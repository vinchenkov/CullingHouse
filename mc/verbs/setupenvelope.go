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
	// SetupOperationSealedLanding is the landing class (ADR-017:697-702). It
	// shares this envelope's frame — schema, strict decode, fixed container
	// destinations — and nothing else: landing runs a different program, holds
	// no lease, opens no Run, and is the only class that receives a real
	// Worksource repository RW. Its file is `/mc/landing.json`, not
	// `/mc/setup.json`. The union stays closed by refusing cross-arm authority
	// in BOTH directions.
	SetupOperationSealedLanding = "sealed-landing"
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

	// The sealed landing arm (ADR-017:702). Its nine enumerated facts map onto
	// these fields plus the four reused above (TaskID, TargetRef,
	// PinnedBaseSHA, PinnedClosureDigest, PinnedLocalRepoUUID, Branch):
	//
	//   exact task            → TaskID
	//   local/real branch     → Branch
	//   verified SHA          → VerifiedSHA
	//   target ref            → TargetRef
	//   pre-merge SHA         → PreMergeSHA (the target preimage, frozen at
	//                           attest so retry can match ADR-017:755)
	//   closure digest        → PinnedClosureDigest
	//   landing action id     → LandingID (ADR-016:831's 16 lowercase hex)
	//   expected Git topology → carried STRUCTURALLY, not as a blob: the
	//                           branch, verified/pre-merge/base SHAs, closure
	//                           digest and repository UUID above are the
	//                           topology the lander revalidates
	//                           (ADR-017:741-743). The ADR does not specify a
	//                           serialization; inventing an opaque one would
	//                           add a parser without adding a fence.
	//   cleanup path          → NOT carried. ADR-017:757-759 defers cleanup to
	//                           "a later trusted landing/setup action" after
	//                           the spine records success, and its only target
	//                           inside this container is TaskRoot, which is
	//                           already here and mounted RO. Deviation logged.
	//
	// CoverRoot is the obligation, not decoration: without the generated empty
	// RO cover the sealed task bytes are reachable through the RW
	// `/repo/source` alias, which is the single thing ADR-017:700 exists to
	// prevent. It is carried explicitly so the helper boundary can refuse a
	// landing instruction that omits it.
	LandingID   string `json:"landing_id,omitempty"`
	VerifiedSHA string `json:"verified_sha,omitempty"`
	PreMergeSHA string `json:"pre_merge_sha,omitempty"`
	CoverRoot   string `json:"cover_root,omitempty"`
}

func validateSetupEnvelope(env SetupEnvelope) error {
	if env.SchemaVersion != 1 {
		return Domainf("setup envelope schema version %d is unsupported", env.SchemaVersion)
	}
	if env.TaskID < 1 {
		return Domainf("setup envelope names no task")
	}
	// Every setup arm runs under a live claimed run. Landing does not: it holds
	// no lease and opens no Run (§7), so a run id there means some caller
	// conflated the two classes.
	if env.RunID == "" && env.Operation != SetupOperationSealedLanding {
		return Domainf("setup envelope names no live run")
	}
	// Landing authority never bleeds INTO a setup arm. Stated once here so a
	// fifth arm inherits the refusal instead of forgetting it.
	if env.Operation != SetupOperationSealedLanding &&
		(env.LandingID != "" || env.VerifiedSHA != "" || env.PreMergeSHA != "" || env.CoverRoot != "") {
		return Domainf("setup envelope carries sealed landing authority")
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
	case SetupOperationSealedLanding:
		if env.RunID != "" {
			return Domainf("sealed landing envelope names a run; landing holds no lease and opens no Run (§7)")
		}
		if env.Mode != "" || env.WorktreeName != "" || env.SealRoot != "" || env.ProjectionRoot != "" ||
			env.CompletionRunID != "" || env.CompletionRequest != "" || env.SealedSHA != "" ||
			env.ClosureDigest != "" || env.ManifestDigest != "" || env.SealDevice != "" ||
			env.SealInode != "" || env.SealOwnerUID != 0 {
			return Domainf("sealed landing envelope carries setup authority")
		}
		// The destinations are the landing table's own cells, never a host
		// path: the resident composes the `/repo` plane and the envelope only
		// restates where it put things.
		if env.SourceRepo != landingSourceDest || env.TaskRoot != landingTaskRootDest ||
			env.CoverRoot != landingCoverDest {
			return Domainf("sealed landing envelope carries paths outside its fixed container destinations")
		}
		if len(env.LandingID) != 16 || !assignmentHex.MatchString(env.LandingID) {
			return Domainf("sealed landing envelope carries no 16-hex landing action identity (ADR-016:831)")
		}
		if env.Branch != taskAssignmentBranch(env.TaskID) {
			return Domainf("sealed landing envelope branch does not match the task id")
		}
		if env.TargetRef == "" {
			return Domainf("sealed landing envelope has no target ref")
		}
		oid := oidLen(env.ObjectFormat)
		if len(env.VerifiedSHA) != oid || !assignmentHex.MatchString(env.VerifiedSHA) ||
			len(env.PreMergeSHA) != oid || !assignmentHex.MatchString(env.PreMergeSHA) ||
			len(env.PinnedBaseSHA) != oid || !assignmentHex.MatchString(env.PinnedBaseSHA) ||
			len(env.PinnedClosureDigest) != 64 || !assignmentHex.MatchString(env.PinnedClosureDigest) ||
			!assignmentUUID.MatchString(env.PinnedLocalRepoUUID) {
			return Domainf("sealed landing envelope does not reproduce the frozen closure assignment")
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
	// The receipt's device/inode/owner are recorded by the in-container setuid
	// publisher, so the tuple is namespace-local: no comparison against it
	// survives a bind crossing, here or on the host. The resident proves
	// custody instead (derived path, non-symlink directory, host-operator
	// ownership — task-skeleton.ts `recheckAcceptedSeal`, deviation logged
	// 2026-07-19), and the shared rebuild below verifies every immutable
	// manifest and pack byte, which is what actually binds the seal's content.
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
