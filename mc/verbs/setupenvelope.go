package verbs

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// SetupOperationFirstTaskClosure is the sole closed-union operation this
// executor performs (ADR-016 D6:724). Any other value fails closed.
const SetupOperationFirstTaskClosure = "first-task-closure-extraction"

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
}

func validateSetupEnvelope(env SetupEnvelope) error {
	if env.SchemaVersion != 1 {
		return Domainf("setup envelope schema version %d is unsupported", env.SchemaVersion)
	}
	if env.Operation != SetupOperationFirstTaskClosure {
		return Domainf("setup envelope operation %q is outside the closed union", env.Operation)
	}
	if env.RunID == "" || env.TaskID < 1 {
		return Domainf("setup envelope names no live run/task")
	}
	if err := validateSetupObjectFormat(env.ObjectFormat); err != nil {
		return err
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
	if dec.More() {
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
