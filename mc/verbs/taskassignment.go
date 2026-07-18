package verbs

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
)

// FirstTaskAssignment is the git-derived half of a first-task closure
// assignment (ADR-016 D5): the fields the host re-verifies from the landed
// task-local store after the setup container extracts the reachable closure.
// The logical half (target ref, branch, task-root key) is derived from the
// task, never supplied by a caller.
type FirstTaskAssignment struct {
	ObjectFormat  string
	BaseSHA       string
	LocalRepoUUID string
	ClosureDigest string
}

// TaskAssignment is the full immutable first-task assignment row. It is what a
// retry reuses: the pinned base SHA and closure digest fence a re-extraction to
// the exact original object set rather than a moved target.
type TaskAssignment struct {
	TaskID        int64
	TargetRef     string
	Branch        string
	TaskRootKey   string
	ObjectFormat  string
	BaseSHA       string
	LocalRepoUUID string
	ClosureDigest string
}

var (
	assignmentUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	assignmentHex  = regexp.MustCompile(`^[0-9a-f]+$`)
)

// taskAssignmentBranch is the sole managed branch a first-task setup may
// create (ADR-017:469), matching the repo's other managed-name grammars.
func taskAssignmentBranch(taskID int64) string {
	return "mc/task-" + strconv.FormatInt(taskID, 10)
}

// taskAssignmentRootKey is the deterministic, path-free key of a task's
// retry-safe store: workspace-relative, never an absolute host path (D5).
func taskAssignmentRootKey(taskID int64) string {
	return ".mission-control/tasks/task-" + strconv.FormatInt(taskID, 10)
}

func validateFirstTaskAssignment(git FirstTaskAssignment) error {
	switch git.ObjectFormat {
	case "sha1":
		if len(git.BaseSHA) != 40 || !assignmentHex.MatchString(git.BaseSHA) {
			return Domainf("first-task assignment base SHA is not a sha1 object name")
		}
	case "sha256":
		if len(git.BaseSHA) != 64 || !assignmentHex.MatchString(git.BaseSHA) {
			return Domainf("first-task assignment base SHA is not a sha256 object name")
		}
	default:
		return Domainf("first-task assignment object format %q is outside the closed set", git.ObjectFormat)
	}
	if len(git.ClosureDigest) != 64 || !assignmentHex.MatchString(git.ClosureDigest) {
		return Domainf("first-task assignment closure digest is not a canonical sha256")
	}
	if !assignmentUUID.MatchString(git.LocalRepoUUID) {
		return Domainf("first-task assignment local repository UUID is malformed")
	}
	return nil
}

// liveStandaloneWorkerSubject is the shared post-claim fence for the first-task
// setup receipt and the closure assignment: it returns runID's subject task id
// iff runID is a live (non-ended) standalone pipeline Worker that still holds
// the singleton lease for that same run and subject. An abandoned or superseded
// Worker can donate neither a skeleton nor an assignment to a later claim. It
// intentionally mirrors the inline fence in RegisterFirstTaskSetup /
// ReadFirstTaskSetup; the two must not drift.
func liveStandaloneWorkerSubject(ctx context.Context, q Q, runID string) (int64, error) {
	var role, tier string
	var subject sql.NullInt64
	var ended sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT role, tier, subject, ended_at FROM runs WHERE id=?`, runID).
		Scan(&role, &tier, &subject, &ended); err != nil {
		return 0, Domainf("first-task setup run is absent")
	}
	if tier != "pipeline" || role != "worker" || !subject.Valid || ended.Valid {
		return 0, Domainf("first-task setup does not name a live standalone Worker run")
	}
	var lockRun sql.NullString
	var lockSubject sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT run_id, subject FROM lock WHERE id=1`).Scan(&lockRun, &lockSubject); err != nil {
		return 0, err
	}
	if !lockRun.Valid || lockRun.String != runID || !lockSubject.Valid || lockSubject.Int64 != subject.Int64 {
		return 0, Domainf("first-task setup lost its run/task lease fence")
	}
	return subject.Int64, nil
}

// RegisterFirstTaskAssignment records the immutable closure assignment for the
// live Worker's subject task. The logical fields are derived from the task; the
// git-derived fields are supplied by the host after it re-verifies the landed
// store. Repeating the exact assignment is an idempotent lost-response retry; a
// divergent base SHA, closure digest, object format, or UUID refuses rather
// than rebasing a moved target (ADR-016 D5).
func RegisterFirstTaskAssignment(db *sql.DB, runID string, git FirstTaskAssignment) (TaskAssignment, error) {
	if err := validateFirstTaskAssignment(git); err != nil {
		return TaskAssignment{}, err
	}
	var out TaskAssignment
	err := inTx(db, func(ctx context.Context, q Q) error {
		taskID, err := liveStandaloneWorkerSubject(ctx, q, runID)
		if err != nil {
			return err
		}
		var targetRef sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT target_ref FROM tasks WHERE id=?`, taskID).Scan(&targetRef); err != nil {
			return Domainf("first-task assignment task is absent")
		}
		if !targetRef.Valid || targetRef.String == "" {
			return Domainf("first-task assignment task has no target ref to pin")
		}
		want := TaskAssignment{
			TaskID: taskID, TargetRef: targetRef.String,
			Branch: taskAssignmentBranch(taskID), TaskRootKey: taskAssignmentRootKey(taskID),
			ObjectFormat: git.ObjectFormat, BaseSHA: git.BaseSHA,
			LocalRepoUUID: git.LocalRepoUUID, ClosureDigest: git.ClosureDigest,
		}
		existing, err := readAssignmentRow(ctx, q, taskID)
		if err == sql.ErrNoRows {
			if _, err := q.ExecContext(ctx, `INSERT INTO task_assignments
				(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				want.TaskID, want.TargetRef, want.Branch, want.TaskRootKey,
				want.ObjectFormat, want.BaseSHA, want.LocalRepoUUID, want.ClosureDigest); err != nil {
				return err
			}
			out = want
			return nil
		}
		if err != nil {
			return err
		}
		if existing != want {
			return Domainf("first-task assignment retry diverges from the recorded assignment; a retry reuses it, never rebases (ADR-016 D5)")
		}
		out = existing
		return nil
	})
	return out, err
}

// ReadFirstTaskAssignment returns the recorded assignment for the live Worker's
// subject task under the same run/task/lease fence as registration.
func ReadFirstTaskAssignment(db *sql.DB, runID string) (TaskAssignment, error) {
	var out TaskAssignment
	err := inTx(db, func(ctx context.Context, q Q) error {
		taskID, err := liveStandaloneWorkerSubject(ctx, q, runID)
		if err != nil {
			return err
		}
		row, err := readAssignmentRow(ctx, q, taskID)
		if err == sql.ErrNoRows {
			return Domainf("first-task assignment is absent")
		}
		if err != nil {
			return err
		}
		out = row
		return nil
	})
	return out, err
}

func readAssignmentRow(ctx context.Context, q Q, taskID int64) (TaskAssignment, error) {
	var a TaskAssignment
	err := q.QueryRowContext(ctx, `SELECT task_id, target_ref, branch, task_root_key,
		object_format, base_sha, local_repo_uuid, closure_digest FROM task_assignments WHERE task_id=?`, taskID).
		Scan(&a.TaskID, &a.TargetRef, &a.Branch, &a.TaskRootKey, &a.ObjectFormat, &a.BaseSHA, &a.LocalRepoUUID, &a.ClosureDigest)
	return a, err
}
