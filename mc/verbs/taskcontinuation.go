package verbs

import (
	"context"
	"database/sql"

	"mc/domain"
)

// FirstTaskSetupContinuation is the one durable handoff from the immutable
// precreate-only effect to a later, separately attested Worker plan.  The
// setup Run never starts an agent: after the closure record has proved the
// 15-row task table and pinned its assignment, it ends normally and frees the
// singleton lease without consuming a dispatch retry.  The next tick can then
// claim a new Worker and attest the receipt-backed task rows.
type FirstTaskSetupContinuation struct {
	RunID            string `json:"run_id"`
	TaskID           int64  `json:"task_id"`
	AlreadyContinued bool   `json:"already_continued"`
}

// ContinueFirstTaskSetup is the host-scope, run-fenced completion of ADR-016
// D6's first-task setup operation.  It is intentionally narrower than a role
// terminal: no agent ran, so it may neither advance nor block the task and
// must never charge dispatch_retries.  Repeating a lost response returns the
// same durable terminal only after re-proving that its receipt and immutable
// task assignment exist.
func ContinueFirstTaskSetup(db *sql.DB, runID string) (FirstTaskSetupContinuation, error) {
	if runID == "" {
		return FirstTaskSetupContinuation{}, Domainf("first-task setup continuation run is absent")
	}
	var out FirstTaskSetupContinuation
	err := inTx(db, func(ctx context.Context, q Q) error {
		var role, tier string
		var subject sql.NullInt64
		var ended, outcome sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT role, tier, subject, ended_at, outcome FROM runs WHERE id=?`, runID).
			Scan(&role, &tier, &subject, &ended, &outcome); err != nil {
			if err == sql.ErrNoRows {
				return Domainf("first-task setup continuation run is absent")
			}
			return err
		}
		if tier != "pipeline" || role != "worker" || !subject.Valid {
			return Domainf("first-task setup continuation does not name a standalone Worker run")
		}
		out = FirstTaskSetupContinuation{RunID: runID, TaskID: subject.Int64}
		if ended.Valid {
			if !outcome.Valid || outcome.String != "setup-complete" {
				return Domainf("first-task setup continuation run is already terminal")
			}
			if err := requireFirstTaskContinuationEvidence(ctx, q, runID, subject.Int64); err != nil {
				return err
			}
			out.AlreadyContinued = true
			return nil
		}

		fenced, err := domain.Fence(ctx, q, runID)
		if err != nil {
			return err
		}
		if fenced == nil || *fenced != subject.Int64 {
			return Domainf("first-task setup continuation lost its run/task lease fence")
		}
		if err := requireFirstTaskContinuationEvidence(ctx, q, runID, subject.Int64); err != nil {
			return err
		}
		if err := endRun(ctx, q, runID, "setup-complete"); err != nil {
			return err
		}
		return domain.Release(ctx, q, runID)
	})
	return out, err
}

func requireFirstTaskContinuationEvidence(ctx context.Context, q Q, runID string, taskID int64) error {
	var receiptTask int64
	if err := q.QueryRowContext(ctx, `SELECT task_id FROM task_setup_receipts WHERE run_id=?`, runID).Scan(&receiptTask); err != nil {
		if err == sql.ErrNoRows {
			return Domainf("first-task setup continuation has no durable root receipt")
		}
		return err
	}
	if receiptTask != taskID {
		return Domainf("first-task setup continuation receipt names a different task")
	}
	var assignmentTask int64
	if err := q.QueryRowContext(ctx, `SELECT task_id FROM task_assignments WHERE task_id=?`, taskID).Scan(&assignmentTask); err != nil {
		if err == sql.ErrNoRows {
			return Domainf("first-task setup continuation has no immutable closure assignment")
		}
		return err
	}
	if assignmentTask != taskID {
		return Domainf("first-task setup continuation assignment names a different task")
	}
	return nil
}
