package verbs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// CompletionSealManifest is the immutable sealed-input contract. Its SHA-256
// is stored in completion_seals.manifest_digest; pack bytes are individually
// named and digested so the temporary importer never consults mutable task or
// Worksource objects.
type CompletionSealManifest struct {
	Version           int                  `json:"version"`
	RunID             string               `json:"run_id"`
	TaskID            int64                `json:"task_id"`
	CompletionRequest string               `json:"completion_request_id"`
	ObjectFormat      string               `json:"object_format"`
	SealedSHA         string               `json:"sealed_sha"`
	Tree              string               `json:"tree,omitempty"`
	ObjectCount       int                  `json:"object_count,omitempty"`
	ClosureDigest     string               `json:"closure_digest"`
	LocalRepoUUID     string               `json:"local_repo_uuid"`
	Files             []CompletionSealFile `json:"files"`
}
type CompletionSealFile struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// RebuildAcceptedCompletionSeal reconstructs a task store from only an exact
// accepted seal. The seal root is supplied by the resident's re-attested mount;
// this pure executor verifies its immutable manifest and files before forming a
// throwaway bare source used by the existing closure materializer.
func RebuildAcceptedCompletionSeal(sealDir, taskRoot string, seal AcceptedCompletionSeal) (SetupResult, error) {
	return rebuildAcceptedCompletionSeal(sealDir, taskRoot, seal, true)
}

// rebuildAcceptedCompletionSeal performs the sealed-byte verification common
// to both callers. Host callers can compare the receipt's host filesystem
// identity directly; the setup container cannot, because Docker Desktop bind
// namespaces remap device/inode values. That crossing is re-attested by the
// resident immediately before the exact bind is created, while this executor
// proves the immutable manifest and pack bytes it can observe.
func rebuildAcceptedCompletionSeal(sealDir, taskRoot string, seal AcceptedCompletionSeal, verifyIdentity bool) (SetupResult, error) {
	if verifyIdentity {
		if err := verifyAcceptedSealIdentity(sealDir, seal); err != nil {
			return SetupResult{}, err
		}
	}
	body, err := os.ReadFile(filepath.Join(sealDir, "manifest.json"))
	if err != nil {
		return SetupResult{}, Domainf("accepted seal manifest is unreadable: %v", err)
	}
	h := sha256.Sum256(body)
	if hex.EncodeToString(h[:]) != seal.ManifestDigest {
		return SetupResult{}, Domainf("accepted seal manifest digest does not match the accepted receipt")
	}
	var m CompletionSealManifest
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return SetupResult{}, Domainf("accepted seal manifest is malformed")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return SetupResult{}, Domainf("accepted seal manifest is malformed")
	}
	if m.Version != 1 || m.RunID != seal.RunID || m.TaskID != seal.TaskID || m.CompletionRequest != seal.CompletionRequest || m.ObjectFormat != seal.ObjectFormat || m.SealedSHA != seal.SealedSHA || m.ClosureDigest != seal.ClosureDigest || !assignmentUUID.MatchString(m.LocalRepoUUID) || len(m.Tree) != oidLen(m.ObjectFormat) || !assignmentHex.MatchString(m.Tree) || m.ObjectCount < 1 {
		return SetupResult{}, Domainf("accepted seal manifest does not reproduce its immutable receipt")
	}
	if len(m.Files) != 2 {
		return SetupResult{}, Domainf("accepted seal manifest has no complete pack/index pair")
	}
	tmp, err := os.MkdirTemp("", "mc-accepted-seal-")
	if err != nil {
		return SetupResult{}, Domainf("create sealed importer: %v", err)
	}
	defer os.RemoveAll(tmp)
	packDir := filepath.Join(tmp, "objects", "pack")
	if err := os.MkdirAll(packDir, 0o700); err != nil {
		return SetupResult{}, Domainf("create sealed importer: %v", err)
	}
	seen := map[string]bool{}
	pairStem := ""
	hasPack, hasIndex := false, false
	for _, f := range m.Files {
		if seen[f.Name] || len(f.Digest) != 64 || !assignmentHex.MatchString(f.Digest) {
			return SetupResult{}, Domainf("accepted seal manifest has an invalid pack entry")
		}
		seen[f.Name] = true
		var stem string
		switch {
		case strings.HasPrefix(f.Name, "pack-") && strings.HasSuffix(f.Name, ".pack"):
			stem = strings.TrimSuffix(f.Name, ".pack")
			if hasPack {
				return SetupResult{}, Domainf("accepted seal manifest has an invalid pack entry")
			}
			hasPack = true
		case strings.HasPrefix(f.Name, "pack-") && strings.HasSuffix(f.Name, ".idx"):
			stem = strings.TrimSuffix(f.Name, ".idx")
			if hasIndex {
				return SetupResult{}, Domainf("accepted seal manifest has an invalid pack entry")
			}
			hasIndex = true
		default:
			return SetupResult{}, Domainf("accepted seal manifest has an invalid pack entry")
		}
		if stem == "pack-" || (pairStem != "" && pairStem != stem) {
			return SetupResult{}, Domainf("accepted seal manifest has an invalid pack entry")
		}
		pairStem = stem
	}
	if !hasPack || !hasIndex {
		return SetupResult{}, Domainf("accepted seal manifest has no complete pack/index pair")
	}
	for _, f := range m.Files {
		b, err := os.ReadFile(filepath.Join(sealDir, f.Name))
		if err != nil {
			return SetupResult{}, Domainf("accepted seal pack entry is unreadable: %v", err)
		}
		d := sha256.Sum256(b)
		if hex.EncodeToString(d[:]) != f.Digest {
			return SetupResult{}, Domainf("accepted seal pack entry digest mismatch")
		}
		if err := os.WriteFile(filepath.Join(packDir, f.Name), b, 0o600); err != nil {
			return SetupResult{}, Domainf("write sealed importer: %v", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(tmp, "refs", "heads", "mc"), 0o700); err != nil {
		return SetupResult{}, Domainf("create sealed importer refs: %v", err)
	}
	branch := taskAssignmentBranch(m.TaskID)
	if err := os.WriteFile(filepath.Join(tmp, "config"), generatedTaskGitConfig(m.ObjectFormat, m.LocalRepoUUID), 0o600); err != nil {
		return SetupResult{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0o600); err != nil {
		return SetupResult{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "refs", "heads", "mc", "task-"+strconv.FormatInt(m.TaskID, 10)), []byte(m.SealedSHA+"\n"), 0o600); err != nil {
		return SetupResult{}, err
	}
	sealedTree, err := gitOutput(tmp, sourceGitEnv(), nil, "rev-parse", "--verify", m.SealedSHA+"^{tree}")
	if err != nil || strings.TrimSpace(string(sealedTree)) != m.Tree {
		return SetupResult{}, Domainf("accepted seal manifest tree does not match its sealed commit")
	}
	result, err := MaterializeFirstTaskStore(tmp, taskRoot, FirstTaskSetupSpec{TaskID: m.TaskID, Mode: "retry", PinnedBaseSHA: m.SealedSHA, ObjectFormat: m.ObjectFormat, LocalRepoUUID: m.LocalRepoUUID})
	if err != nil {
		return SetupResult{}, err
	}
	if result.ObjectCount != m.ObjectCount {
		return SetupResult{}, Domainf("accepted seal manifest object count does not match its sealed closure")
	}
	return result, nil
}

func verifyAcceptedSealIdentity(path string, seal AcceptedCompletionSeal) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Domainf("accepted seal root is not a non-symlink directory")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || strconv.FormatUint(uint64(st.Dev), 10) != seal.Device ||
		strconv.FormatUint(st.Ino, 10) != seal.Inode || int64(st.Uid) != seal.OwnerUID {
		return Domainf("accepted seal root is a different filesystem object than its receipt")
	}
	return nil
}
