package verbs

// The host half of first-task setup closure recording (ADR-016 D5). The
// sanitized closure extraction and full-store materialization run in the
// resident's network=none setup container (MaterializeFirstTaskStore); the
// host, holding the spine, re-attests the receipt-fenced root, re-verifies the
// landed store against the container's returned SetupResult, records the
// durable task assignment, and returns the joined receipt-plus-15-row
// inspection. No caller supplies the pin any more: it is the git-derived
// SetupResult, cross-checked against the bytes on disk.

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"mc/boundary"
)

// FirstTaskClosureFile is one landed pack-store file of the reachable object
// closure. firstTaskClosureDigest reproduces the closure identity over the
// landed set; MaterializeFirstTaskStore and RecordFirstTaskSetupClosure both
// digest the same files, so agreement is set equality.
type FirstTaskClosureFile struct {
	Name string
	Body []byte
}

// firstTaskClosureDigest is the canonical closure identity: sha256 over the
// version header then every file in name order as name NUL decimal-length NUL
// body. Covering the names makes set equality follow from digest equality.
func firstTaskClosureDigest(files []FirstTaskClosureFile) string {
	sorted := make([]FirstTaskClosureFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	h := sha256.New()
	h.Write([]byte("mc-first-task-closure/v1\n"))
	for _, f := range sorted {
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write([]byte(strconv.Itoa(len(f.Body))))
		h.Write([]byte{0})
		h.Write(f.Body)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validateSetupResult(result SetupResult) error {
	if err := validateSetupObjectFormat(result.ObjectFormat); err != nil {
		return err
	}
	if len(result.BaseSHA) != oidLen(result.ObjectFormat) || !assignmentHex.MatchString(result.BaseSHA) {
		return Domainf("first-task setup result base SHA is not a canonical object name")
	}
	if len(result.ClosureDigest) != 64 || !assignmentHex.MatchString(result.ClosureDigest) {
		return Domainf("first-task setup result closure digest is not a canonical sha256")
	}
	if !assignmentUUID.MatchString(result.LocalRepoUUID) {
		return Domainf("first-task setup result local repository UUID is malformed")
	}
	if result.ObjectCount < 1 {
		return Domainf("first-task setup result reports an empty closure")
	}
	if !result.FsckClean {
		return Domainf("first-task setup result was not proven fsck-clean")
	}
	return nil
}

// crossCheckLandedStore proves the store on disk beneath the receipt-attested
// root is exactly the one the setup container reported: the sole ref at the
// pinned base SHA, the generated config reproducing the recorded object
// format/UUID byte-for-byte, and a pack reproducing the recorded closure
// digest. This is the spine-present exact check the dispatch-attest resolver's
// grammar-only pass cannot make.
func crossCheckLandedStore(root FirstTaskSetupRoot, result SetupResult) error {
	refPath := filepath.Join(root.Canonical, "git", "refs", "heads", "mc",
		"task-"+strconv.FormatInt(root.Receipt.TaskID, 10))
	refBytes, err := os.ReadFile(refPath)
	if err != nil {
		return Domainf("first-task setup ref is unreadable: %v", err)
	}
	if string(bytes.TrimSpace(refBytes)) != result.BaseSHA {
		return Domainf("first-task setup landed ref does not equal the recorded base SHA")
	}
	cfg, err := os.ReadFile(filepath.Join(root.Canonical, "git", "config"))
	if err != nil {
		return Domainf("first-task setup config is unreadable: %v", err)
	}
	if !bytes.Equal(cfg, generatedTaskGitConfig(result.ObjectFormat, result.LocalRepoUUID)) {
		return Domainf("first-task setup landed config does not reproduce the recorded object format and UUID")
	}
	digest, err := digestLandedPack(filepath.Join(root.Canonical, "git", "objects", "pack"))
	if err != nil {
		return err
	}
	if digest != result.ClosureDigest {
		return Domainf("first-task setup landed pack does not reproduce the recorded closure digest")
	}
	return nil
}

// RecordFirstTaskSetupClosure is the host-side commit of a materialized
// first-task store. It re-attests the durable receipt's root, proves the landed
// store reproduces the container's SetupResult, joins the receipt-plus-15-row
// inspection, and records the immutable task assignment last so a retry reuses
// the exact pinned closure. It is the first (and only) production caller of the
// closure machinery.
func RecordFirstTaskSetupClosure(db *sql.DB, runID, workspaceRoot string, result SetupResult) (FirstTaskSetupRoot, map[boundary.TypedKind]boundary.ProtectedID, error) {
	// Validate the result before touching the filesystem, as this composition
	// always has: a malformed result is the caller's error and should be named
	// as such, not masked by whatever the task root happens to look like.
	if err := validateSetupResult(result); err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	root, err := AttestFirstTaskSetupRoot(db, runID, workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	hostRoot, rows, err := HostAttestFirstTaskSetupClosure(root.Receipt.TaskID, workspaceRoot, result)
	if err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	receipt, err := RecordFirstTaskSetupClosureAttested(db, runID, hostRoot.Receipt.TaskID, hostRoot.Receipt.Root, result)
	if err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	return FirstTaskSetupRoot{Receipt: receipt, Canonical: hostRoot.Canonical}, rows, nil
}
