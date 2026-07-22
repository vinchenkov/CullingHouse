package substrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

// MaxDispatchMountProjectionBytes reserves one quarter of ADR-016 D2's
// 1 MiB private frame for the complete Worksource/profile projection. The
// remaining frame carries selection, tunables, candidate, and protocol
// identity. Admission measures the exact canonical JSON bytes below.
const MaxDispatchMountProjectionBytes = 256 * 1024

type DispatchMountState struct {
	SelectedWorksource  string               `json:"selected_worksource"`
	SubjectInitiativeID *int64               `json:"subject_initiative_id"`
	Worksources         []DispatchWorksource `json:"worksources"`
	// SubjectTaskSetupRoots freezes, under the preparation token, the durable
	// first-task setup receipt identities registered for the spawn subject's
	// task (ADR-016 D5). The host mount attest resolves the on-disk task
	// skeleton and only admits it into an agent plan when the resolved root
	// identity is a member of this frozen set: a materialized skeleton with no
	// receipt is never trusted (the run-keyed InspectFirstTaskSetup fence is
	// unsatisfiable at spawn dispatch-attest, so the identity travels here and
	// rides the commit-time DeepEqual/token/plan_digest fences).
	SubjectTaskSetupRoots []DispatchTaskSetupIdentity `json:"subject_task_setup_roots"`
	// SubjectTaskAssignment freezes the subject task's immutable first-task
	// closure assignment when one exists (ADR-016 D5). Its presence flips the
	// plan's setup instruction to retry mode carrying these exact pins — a
	// retry reuses the recorded closure, never rebases. Absence is the normal
	// fresh-mode first run. omitempty keeps every pre-existing state's
	// canonical bytes (and its preparation token) unchanged.
	SubjectTaskAssignment *DispatchTaskAssignment `json:"subject_task_assignment,omitempty"`
	// SubjectAcceptedCompletionSeal freezes the exact completed Worker receipt
	// for a downstream D6 setup. It is task-pointed at acceptance rather than
	// selected from historical completion_seals rows by time or run ordering.
	SubjectAcceptedCompletionSeal *DispatchAcceptedCompletionSeal `json:"subject_accepted_completion_seal,omitempty"`
	// SubjectAcceptedSealRebuild freezes the completed setup receipt that proves
	// the canonical store was reconstructed from that exact accepted seal.
	SubjectAcceptedSealRebuild *DispatchAcceptedSealRebuild `json:"subject_accepted_seal_rebuild,omitempty"`
	// SubjectTaskTargetRef freezes the subject task's target ref under the
	// token so the spine-free attest leg can pin a fresh-mode first-task
	// closure without re-reading the spine.
	SubjectTaskTargetRef string `json:"subject_task_target_ref,omitempty"`
}

// DispatchTaskAssignment is the git-derived half of a task_assignments row,
// projected at dispatch prepare for the plan's retry-mode setup instruction.
// The logical half (target ref, branch, root key) is derived from the task id
// and never travels: the envelope grammar recomputes it.
type DispatchTaskAssignment struct {
	BaseSHA       string `json:"base_sha"`
	ClosureDigest string `json:"closure_digest"`
	LocalRepoUUID string `json:"local_repo_uuid"`
	ObjectFormat  string `json:"object_format"`
}

type DispatchAcceptedCompletionSeal struct {
	RunID             string `json:"run_id"`
	CompletionRequest string `json:"completion_request_id"`
	ObjectFormat      string `json:"object_format"`
	SealedSHA         string `json:"sealed_sha"`
	ClosureDigest     string `json:"closure_digest"`
	ManifestDigest    string `json:"manifest_digest"`
	Device            string `json:"device"`
	Inode             string `json:"inode"`
	OwnerUID          int    `json:"owner_uid"`
}

type DispatchAcceptedSealRebuild struct {
	RunID             string `json:"run_id"`
	CompletionRunID   string `json:"completion_run_id"`
	CompletionRequest string `json:"completion_request_id"`
	ObjectFormat      string `json:"object_format"`
	SealedSHA         string `json:"sealed_sha"`
	ClosureDigest     string `json:"closure_digest"`
	ManifestDigest    string `json:"manifest_digest"`
}

// DispatchTaskSetupIdentity is the path-free durable identity of a
// resident-created first-task setup root, projected from task_setup_receipts.
// Device/Inode are the canonical decimal encodings the resident registered.
type DispatchTaskSetupIdentity struct {
	Device   string `json:"device"`
	Inode    string `json:"inode"`
	OwnerUID int    `json:"owner_uid"`
}

// MaxDispatchTaskSetupRoots bounds the frozen receipt-identity set. Every
// retry run registers its own durable row (all reusing the same reused task
// root), so the distinct set is normally a single element; the cap is a
// fail-closed backstop against a pathological row count bloating the token.
const MaxDispatchTaskSetupRoots = 64

type DispatchWorksource struct {
	WorksourceID      string   `json:"worksource_id"`
	Kind              string   `json:"kind"`
	Status            string   `json:"status"`
	ProfilePresent    bool     `json:"profile_present"`
	ProfileID         string   `json:"profile_id"`
	WorkspaceRoot     string   `json:"workspace_root"`
	ArtifactRoots     []string `json:"artifact_roots"`
	ReadonlyMounts    []string `json:"readonly_mounts"`
	DeniedPaths       []string `json:"denied_paths"`
	ToolHomeDir       string   `json:"tool_home_dir"`
	RuntimeControlDir string   `json:"runtime_control_dir"`
	// The declared env planes and the ADR-022 credential-delivery class. The
	// planes are carried raw: boundary.BuildEnvPlan is their one validator,
	// and it runs in the attest leg before any lease claim (contract §2.2).
	HarnessEnvPolicy    string `json:"harness_env_policy"`
	ToolEnvPolicy       string `json:"tool_env_policy"`
	RuntimeAuthDelivery string `json:"runtime_auth_delivery"`
}

type dispatchProjectionQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func LoadDispatchWorksourceProjection(ctx context.Context, q dispatchProjectionQuerier) ([]DispatchWorksource, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT w.id, w.kind, w.status, w.sandbox_profile,
		       p.id, p.workspace_root, p.artifact_roots, p.readonly_mounts,
		       p.denied_paths, p.tool_home_dir, p.runtime_control_dir,
		       p.harness_env_policy, p.tool_env_policy, p.runtime_auth_delivery
		FROM worksources w
		LEFT JOIN sandbox_profiles p ON p.id = w.sandbox_profile
		ORDER BY w.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DispatchWorksource{}
	for rows.Next() {
		var ws DispatchWorksource
		var associatedProfile, profileID, workspace sql.NullString
		var artifactRaw, readonlyRaw, deniedRaw sql.NullString
		var toolHome, runtimeControl sql.NullString
		var harnessEnv, toolEnv, authDelivery sql.NullString
		if err := rows.Scan(&ws.WorksourceID, &ws.Kind, &ws.Status, &associatedProfile,
			&profileID, &workspace, &artifactRaw, &readonlyRaw, &deniedRaw,
			&toolHome, &runtimeControl, &harnessEnv, &toolEnv, &authDelivery); err != nil {
			return nil, err
		}
		ws.ProfilePresent = associatedProfile.Valid && profileID.Valid
		if associatedProfile.Valid {
			ws.ProfileID = associatedProfile.String
		}
		if ws.ProfilePresent {
			ws.WorkspaceRoot = workspace.String
			ws.ToolHomeDir = toolHome.String
			ws.RuntimeControlDir = runtimeControl.String
			ws.HarnessEnvPolicy = harnessEnv.String
			ws.ToolEnvPolicy = toolEnv.String
			ws.RuntimeAuthDelivery = authDelivery.String
			if ws.ArtifactRoots, err = normalizedDispatchPaths(artifactRaw, 64); err != nil {
				return nil, fmt.Errorf("Worksource %q artifact_roots: %w", ws.WorksourceID, err)
			}
			if ws.ReadonlyMounts, err = normalizedDispatchPaths(readonlyRaw, 128); err != nil {
				return nil, fmt.Errorf("Worksource %q readonly_mounts: %w", ws.WorksourceID, err)
			}
			if ws.DeniedPaths, err = normalizedDispatchPaths(deniedRaw, 512); err != nil {
				return nil, fmt.Errorf("Worksource %q denied_paths: %w", ws.WorksourceID, err)
			}
		} else {
			ws.ArtifactRoots = []string{}
			ws.ReadonlyMounts = []string{}
			ws.DeniedPaths = []string{}
		}
		if !validDispatchScalar(ws.WorksourceID) || !validDispatchScalar(ws.Kind) || !validDispatchScalar(ws.Status) ||
			(ws.ProfileID != "" && !validDispatchScalar(ws.ProfileID)) ||
			(ws.WorkspaceRoot != "" && !validDispatchScalar(ws.WorkspaceRoot)) ||
			(ws.ToolHomeDir != "" && !validDispatchScalar(ws.ToolHomeDir)) ||
			(ws.RuntimeControlDir != "" && !validDispatchScalar(ws.RuntimeControlDir)) {
			return nil, fmt.Errorf("Worksource %q carries a scalar outside ADR-016 D2 bounds", ws.WorksourceID)
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

// LoadSubjectTaskSetupRoots returns the distinct durable setup-receipt root
// identities registered for one task, sorted for canonical determinism. It
// deliberately omits the live run/lock-lease fence of the run-keyed reader:
// this is a projection consumed at dispatch prepare (frozen into the token,
// re-derived and DeepEqual'd at commit), not the resident's post-claim setup
// consumer. An empty result is normal — a first task whose skeleton has not
// been set up yet has no receipt, and its dispatch arm falls to precreate.
func LoadSubjectTaskSetupRoots(ctx context.Context, q dispatchProjectionQuerier, taskID int64) ([]DispatchTaskSetupIdentity, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT DISTINCT root_device, root_inode, root_owner_uid
		FROM task_setup_receipts WHERE task_id = ?
		ORDER BY root_device, root_inode, root_owner_uid`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DispatchTaskSetupIdentity{}
	for rows.Next() {
		var id DispatchTaskSetupIdentity
		if err := rows.Scan(&id.Device, &id.Inode, &id.OwnerUID); err != nil {
			return nil, err
		}
		if len(out) >= MaxDispatchTaskSetupRoots {
			return nil, fmt.Errorf("task %d carries more than %d distinct setup-receipt identities", taskID, MaxDispatchTaskSetupRoots)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// LoadSubjectTaskAssignment returns the immutable first-task closure
// assignment recorded for one task, or nil when none exists. Like
// LoadSubjectTaskSetupRoots it deliberately omits the live run/lock-lease
// fence: it is a projection consumed at dispatch prepare (frozen into the
// token, re-derived and DeepEqual'd at commit), not the post-claim fenced
// reader. A nil result is normal — the first run of a task has no
// assignment and its setup instruction is fresh mode.
func LoadSubjectTaskAssignment(ctx context.Context, q dispatchProjectionQuerier, taskID int64) (*DispatchTaskAssignment, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT base_sha, closure_digest, local_repo_uuid, object_format
		FROM task_assignments WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var a DispatchTaskAssignment
	if err := rows.Scan(&a.BaseSHA, &a.ClosureDigest, &a.LocalRepoUUID, &a.ObjectFormat); err != nil {
		return nil, err
	}
	return &a, rows.Err()
}

// LoadSubjectAcceptedCompletionSeal projects only the receipt explicitly
// pointed to by the task's D6 acceptance transaction. Older schema history
// has no pointer and remains deliberately non-consumable.
func LoadSubjectAcceptedCompletionSeal(ctx context.Context, q dispatchProjectionQuerier, taskID int64) (*DispatchAcceptedCompletionSeal, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT s.run_id,s.completion_request_id,s.object_format,s.sealed_sha,
		       s.closure_digest,s.manifest_digest,s.seal_device,s.seal_inode,s.seal_owner_uid
		FROM tasks t JOIN completion_seals s
		  ON s.run_id=t.accepted_completion_run_id
		 AND s.completion_request_id=t.accepted_completion_request_id
		WHERE t.id=? AND t.accepted_completion_run_id IS NOT NULL
		  AND s.state='accepted'`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var out DispatchAcceptedCompletionSeal
	if err := rows.Scan(&out.RunID, &out.CompletionRequest, &out.ObjectFormat, &out.SealedSHA,
		&out.ClosureDigest, &out.ManifestDigest, &out.Device, &out.Inode, &out.OwnerUID); err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, fmt.Errorf("task %d accepted completion projection is ambiguous", taskID)
	}
	return &out, rows.Err()
}

// LoadSubjectAcceptedSealRebuild returns only a terminal Verifier setup
// receipt joined to the task's current accepted-completion pointer. A stale
// historical rebuild cannot authorize a later verifier projection.
func LoadSubjectAcceptedSealRebuild(ctx context.Context, q dispatchProjectionQuerier, taskID int64) (*DispatchAcceptedSealRebuild, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT b.run_id,b.completion_run_id,b.completion_request_id,b.object_format,b.sealed_sha,b.closure_digest,b.manifest_digest
		FROM tasks t JOIN accepted_seal_rebuild_receipts b
		 ON b.task_id=t.id AND b.completion_run_id=t.accepted_completion_run_id AND b.completion_request_id=t.accepted_completion_request_id
		JOIN runs r ON r.id=b.run_id
		WHERE t.id=? AND t.status='worked' AND r.outcome='accepted-seal-rebuilt'`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var out DispatchAcceptedSealRebuild
	if err := rows.Scan(&out.RunID, &out.CompletionRunID, &out.CompletionRequest, &out.ObjectFormat, &out.SealedSHA, &out.ClosureDigest, &out.ManifestDigest); err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, fmt.Errorf("multiple accepted seal rebuild receipts for task %d", taskID)
	}
	return &out, rows.Err()
}

func ValidateDispatchMountProjection(ctx context.Context, q dispatchProjectionQuerier) error {
	rows, err := LoadDispatchWorksourceProjection(ctx, q)
	if err != nil {
		return err
	}
	body, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	if len(body) > MaxDispatchMountProjectionBytes {
		return fmt.Errorf("Worksource/profile projection is %d bytes, exceeds %d-byte ADR-016 D2 admission budget",
			len(body), MaxDispatchMountProjectionBytes)
	}
	return nil
}

func normalizedDispatchPaths(raw sql.NullString, max int) ([]string, error) {
	if !raw.Valid || raw.String == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw.String), &values); err != nil || values == nil {
		return nil, fmt.Errorf("must be a JSON array of strings")
	}
	if len(values) > max {
		return nil, fmt.Errorf("contains %d items, exceeds bound %d", len(values), max)
	}
	sort.Strings(values)
	for i, value := range values {
		if !validDispatchScalar(value) {
			return nil, fmt.Errorf("item %d is not a bounded structural string", i)
		}
		if i > 0 && values[i-1] == value {
			return nil, fmt.Errorf("duplicate path %q", value)
		}
	}
	return values, nil
}
