package domain

import "context"

// ---------------------------------------------------------------------------
// Dispatch-retry budget (spec §10 "Two budgets, distinct", §16.3, §18
// --infra) — the infra/dispatch-death budget. Two callers only: the reap
// applier (lease.ApplyReap) and `mc complete --infra`.
//
// Structural separation (contract §2): this file owns dispatch_retries and
// has no access to correction_count.
// ---------------------------------------------------------------------------

// ChargeResult reports one infra charge.
type ChargeResult struct {
	Remaining int  // dispatch_retries after the decrement (clamped at 0)
	Blocked   bool // this charge exhausted the budget and blocked the subject
}

// ChargeInfra decrements the subject's dispatch_retries; when the charge
// lands the budget at 0 the same transaction sets blocked with the reason —
// never a silent loop (§10 step 0). A subject already blocked keeps its
// original reason.
func ChargeInfra(ctx context.Context, q Q, taskID int64, reason string) (ChargeResult, error) {
	var res ChargeResult
	var retries, blocked int
	err := q.QueryRowContext(ctx,
		`SELECT dispatch_retries, blocked FROM tasks WHERE id = ?`, taskID).
		Scan(&retries, &blocked)
	if err != nil {
		return res, Errf(CodeNotFound, "no task %d", taskID)
	}
	remaining := retries - 1
	if remaining < 0 {
		remaining = 0 // the CHECK (dispatch_retries >= 0) backstop's clamp edge
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE tasks SET dispatch_retries = ? WHERE id = ?`, remaining, taskID); err != nil {
		return res, err
	}
	res.Remaining = remaining
	if remaining == 0 && blocked == 0 {
		if _, err := q.ExecContext(ctx, `
			UPDATE tasks SET blocked = 1,
				blocked_reason = 'dispatch retries exhausted (' || ? || ')'
			WHERE id = ? AND blocked = 0`, reason, taskID); err != nil {
			return res, err
		}
		res.Blocked = true
	}
	return res, nil
}
