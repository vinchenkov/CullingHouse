package verbs

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Setup operations are a closed union (ADR-016 D6:724). Each arm carries only
// the authority it consumes; arbitrary container commands are never admitted.
const (
	SetupOperationFirstTaskClosure    = "first-task-closure-extraction"
	SetupOperationAcceptedSealRebuild = "accepted-seal-rebuild"
	SetupOperationVerifierProjection  = "verifier-disposable-source"
)

// SetupEnvelope is /mc/setup.json: the frozen, credential-free, host-path-free
// (container-destination) instruction the resident writes RO and the in-
// container executor consumes. Fresh mode resolves the target ref; retry mode
// reuses the exact pinned closure assignment and never re-resolves it.
type SetupEnvelope struct {
	SchemaVersion       int    `json:"schema_version"`
	Operation           string `json:"operation"`
	RunID               string `json:"run_id"`
	TaskID              int64  `json:"task_id"`
	Mode                string `json:"mode"`
	ObjectFormat        string `json:"object_format"`
	TargetRef           string `json:"target_ref,omitempty"`
	PinnedBaseSHA       string `json:"pinned_base_sha,omitempty"`
	PinnedClosureDigest string `json:"pinned_closure_digest,omitempty"`
	PinnedLocalRepoUUID string `json:"pinned_local_repo_uuid,omitempty"`
	Branch              string `json:"branch"`
	WorktreeName        string `json:"worktree_name"`
	SourceRepo          string `json:"source_repo"`
	TaskRoot            string `json:"task_root"`
	CompletionRunID     string `json:"completion_run_id,omitempty"`
	CompletionRequest   string `json:"completion_request_id,omitempty"`
	SealedSHA           string `json:"sealed_sha,omitempty"`
	ClosureDigest       string `json:"closure_digest,omitempty"`
	ManifestDigest      string `json:"manifest_digest,omitempty"`
	SealRoot            string `json:"seal_root,omitempty"`
	SealDevice          string `json:"seal_device,omitempty"`
	SealInode           string `json:"seal_inode,omitempty"`
	SealOwnerUID        int64  `json:"seal_owner_uid,omitempty"`
	ProjectionRoot      string `json:"projection_root,omitempty"`
}

func validateSetupEnvelope(env SetupEnvelope) error {
	if env.SchemaVersion != 1 {
		return Domainf("setup envelope schema version %d is unsupported", env.SchemaVersion)
	}
	if env.RunID == "" || env.TaskID < 1 {
		return Domainf("setup envelope names no live run/task")
	}
	if err := validateSetupObjectFormat(env.ObjectFormat); err != nil {
		return err
	}
	switch env.Operation {
	case SetupOperationFirstTaskClosure:
		if env.CompletionRunID != "" || env.CompletionRequest != "" || env.SealedSHA != "" || env.ClosureDigest != "" ||
			env.ManifestDigest != "" || env.SealRoot != "" || env.SealDevice != "" || env.ProjectionRoot != "" ||
			env.SealInode != "" || env.SealOwnerUID != 0 {
			return Domainf("first-task setup envelope carries accepted-seal authority")
		}
		if env.Branch != taskAssignmentBranch(env.TaskID) || env.WorktreeName != taskWorktreeName(env.TaskID) {
			return Domainf("setup envelope branch/worktree name does not match the task id")
		}
		if env.SourceRepo == "" || env.TaskRoot == "" {
			return Domainf("setup envelope carries no source/task paths")
		}
		switch env.Mode {
		case "fresh":
			if env.TargetRef == "" {
				return Domainf("setup envelope fresh mode has no target ref")
			}
		case "retry":
			if len(env.PinnedBaseSHA) != oidLen(env.ObjectFormat) || !assignmentHex.MatchString(env.PinnedBaseSHA) {
				return Domainf("setup envelope retry base SHA is malformed")
			}
			if len(env.PinnedClosureDigest) != 64 || !assignmentHex.MatchString(env.PinnedClosureDigest) {
				return Domainf("setup envelope retry closure digest is malformed")
			}
			if !assignmentUUID.MatchString(env.PinnedLocalRepoUUID) {
				return Domainf("setup envelope retry local repository UUID is malformed")
			}
		default:
			return Domainf("setup envelope mode %q is neither fresh nor retry", env.Mode)
		}
	case SetupOperationAcceptedSealRebuild:
		if env.Mode != "" || env.TargetRef != "" || env.PinnedBaseSHA != "" ||
			env.PinnedClosureDigest != "" || env.PinnedLocalRepoUUID != "" ||
			env.Branch != "" || env.WorktreeName != "" || env.SourceRepo != "" || env.ProjectionRoot != "" {
			return Domainf("accepted-seal setup envelope carries first-task authority")
		}
		if env.TaskRoot != "/repo/task" || env.SealRoot != "/repo/seal" {
			return Domainf("accepted-seal setup envelope carries paths outside its fixed container destinations")
		}
		if env.CompletionRunID == "" || len(env.CompletionRequest) != 16 || !assignmentHex.MatchString(env.CompletionRequest) ||
			len(env.SealedSHA) != oidLen(env.ObjectFormat) || !assignmentHex.MatchString(env.SealedSHA) ||
			len(env.ClosureDigest) != 64 || !assignmentHex.MatchString(env.ClosureDigest) ||
			len(env.ManifestDigest) != 64 || !assignmentHex.MatchString(env.ManifestDigest) ||
			!decimalIdentity.MatchString(env.SealDevice) || !decimalIdentity.MatchString(env.SealInode) || env.SealOwnerUID < 0 {
			return Domainf("accepted-seal setup envelope does not reproduce an immutable accepted receipt")
		}
	case SetupOperationVerifierProjection:
		if env.Mode != "" || env.TargetRef != "" || env.PinnedBaseSHA != "" ||
			env.PinnedClosureDigest != "" || env.PinnedLocalRepoUUID != "" ||
			env.Branch != "" || env.WorktreeName != "" || env.SourceRepo != "" || env.SealRoot != "" || env.CompletionRunID != "" {
			return Domainf("verifier projection envelope carries unrelated setup authority")
		}
		if env.TaskRoot != "/repo/task" || env.ProjectionRoot != "/repo/projection" {
			return Domainf("verifier projection envelope carries paths outside its fixed container destinations")
		}
		if len(env.CompletionRequest) != 16 || !assignmentHex.MatchString(env.CompletionRequest) ||
			len(env.SealedSHA) != oidLen(env.ObjectFormat) || !assignmentHex.MatchString(env.SealedSHA) ||
			len(env.ClosureDigest) != 64 || !assignmentHex.MatchString(env.ClosureDigest) ||
			len(env.ManifestDigest) != 64 || !assignmentHex.MatchString(env.ManifestDigest) ||
			!decimalIdentity.MatchString(env.SealDevice) || !decimalIdentity.MatchString(env.SealInode) || env.SealOwnerUID < 0 {
			return Domainf("verifier projection envelope does not reproduce an immutable accepted receipt")
		}
	default:
		return Domainf("setup envelope operation %q is outside the closed union", env.Operation)
	}
	return nil
}

// newRepoUUID mints a random v4 UUID for a fresh task-local store's MC identity.
func newRepoUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", Domainf("could not generate a repository UUID: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}

// ReadSetupEnvelope reads and strictly decodes a setup envelope file.
func ReadSetupEnvelope(path string) (SetupEnvelope, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return SetupEnvelope{}, Domainf("setup envelope is unreadable: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var env SetupEnvelope
	if err := dec.Decode(&env); err != nil {
		return SetupEnvelope{}, Domainf("setup envelope is malformed: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return SetupEnvelope{}, Domainf("setup envelope carries trailing data")
	}
	return env, nil
}

// RunFirstTaskSetup is the in-container executor entrypoint: it validates the
// frozen envelope, materializes the store, and in retry mode proves the
// re-extraction reproduced the pinned closure assignment exactly.
func RunFirstTaskSetup(env SetupEnvelope) (SetupResult, error) {
	if err := validateSetupEnvelope(env); err != nil {
		return SetupResult{}, err
	}
	if env.Operation != SetupOperationFirstTaskClosure {
		return SetupResult{}, Domainf("setup envelope is not a first-task closure operation")
	}
	if env.Mode == "retry" {
		// A prior attempt may have already landed the store before its receipt
		// or assignment was recorded. Accept an exact-matching residue
		// idempotently (D5); a divergent one refuses without overwriting.
		if entries, err := os.ReadDir(filepath.Join(env.TaskRoot, "git")); err == nil && len(entries) > 0 {
			return verifyLandedStoreMatches(env.TaskRoot, env.TaskID, env.ObjectFormat,
				env.PinnedBaseSHA, env.PinnedLocalRepoUUID, env.PinnedClosureDigest)
		}
	}
	uuid := env.PinnedLocalRepoUUID
	if env.Mode == "fresh" {
		var err error
		if uuid, err = newRepoUUID(); err != nil {
			return SetupResult{}, err
		}
	}
	result, err := MaterializeFirstTaskStore(env.SourceRepo, env.TaskRoot, FirstTaskSetupSpec{
		TaskID: env.TaskID, Mode: env.Mode, TargetRef: env.TargetRef,
		PinnedBaseSHA: env.PinnedBaseSHA, ObjectFormat: env.ObjectFormat, LocalRepoUUID: uuid,
	})
	if err != nil {
		return SetupResult{}, err
	}
	if env.Mode == "retry" && (result.ClosureDigest != env.PinnedClosureDigest ||
		result.BaseSHA != env.PinnedBaseSHA || result.LocalRepoUUID != env.PinnedLocalRepoUUID) {
		return SetupResult{}, Domainf("first-task retry did not reproduce the pinned closure assignment")
	}
	return result, nil
}

// RunAcceptedSealSetup is the in-container D6 executor arm. Its envelope
// contains only the immutable accepted receipt and fixed container paths; the
// resident is responsible for the separate producer-absence and mount-plan
// attestations before this executor can become reachable.
func RunAcceptedSealSetup(env SetupEnvelope) (SetupResult, error) {
	if err := validateSetupEnvelope(env); err != nil {
		return SetupResult{}, err
	}
	if env.Operation != SetupOperationAcceptedSealRebuild {
		return SetupResult{}, Domainf("setup envelope is not an accepted-seal rebuild operation")
	}
	// The resident re-attests this receipt's host device/inode/owner tuple
	// immediately before creating the exact seal bind. Docker Desktop exposes
	// a namespace-local tuple to the container, so repeating that host-identity
	// comparison here would reject every valid cross-VM bind. The shared
	// rebuild still verifies every immutable manifest and pack byte.
	return rebuildAcceptedCompletionSeal(env.SealRoot, env.TaskRoot, AcceptedCompletionSeal{
		RunID: env.CompletionRunID, TaskID: env.TaskID, CompletionRequest: env.CompletionRequest,
		ObjectFormat: env.ObjectFormat, SealedSHA: env.SealedSHA,
		ClosureDigest: env.ClosureDigest, ManifestDigest: env.ManifestDigest,
		Device: env.SealDevice, Inode: env.SealInode, OwnerUID: env.SealOwnerUID,
	}, false)
}

// RunVerifierProjectionSetup materializes only the sealed tree into the
// execution-scoped projection root; the canonical task store remains mounted
// read-only to the later Verifier container.
func RunVerifierProjectionSetup(env SetupEnvelope) error {
	if err := validateSetupEnvelope(env); err != nil {
		return err
	}
	if env.Operation != SetupOperationVerifierProjection {
		return Domainf("setup envelope is not a verifier projection operation")
	}
	return MaterializeVerifierDisposableSource(env.TaskRoot, env.ProjectionRoot, AcceptedCompletionSeal{
		RunID: env.RunID, TaskID: env.TaskID, CompletionRequest: env.CompletionRequest,
		ObjectFormat: env.ObjectFormat, SealedSHA: env.SealedSHA, ClosureDigest: env.ClosureDigest,
		ManifestDigest: env.ManifestDigest, Device: env.SealDevice, Inode: env.SealInode, OwnerUID: env.SealOwnerUID,
	})
}
