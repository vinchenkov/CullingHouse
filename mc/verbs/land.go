package verbs

import (
	"context"
	"database/sql"
)

// LandReport is the resident's report of a landing result (§7, ADR-001 D6).
// success: archived=1 (the trigger cascades to the packet), with response-loss
// replay idempotent; the lease is untouched — landing holds no lease (§7).
// A stale failure can never regress that success truth. First-time failure:
// blocked=1 with the landing reason, nothing archived, the slot stays held.
func LandReport(db *sql.DB, id *RunIdentity, task int64, status, reason string) (any, error) {
	if err := RequireHostScope(id, "mc land report"); err != nil {
		return nil, err
	}
	if status != "success" && status != "failure" {
		return nil, Usagef("mc land report requires --status success|failure")
	}
	var blocked, archived bool
	err := inTx(db, func(ctx context.Context, q Q) error {
		var st string
		var decision, branch sql.NullString
		var wasArchived bool
		err := q.QueryRowContext(ctx,
			`SELECT status, decision, branch, archived FROM tasks WHERE id = ?`, task).
			Scan(&st, &decision, &branch, &wasArchived)
		if err == sql.ErrNoRows {
			return Domainf("no task %d", task)
		}
		if err != nil {
			return err
		}
		if !decision.Valid || decision.String != "approved" {
			return Domainf("task %d is not approved; nothing was landing (§7)", task)
		}
		if !branch.Valid || branch.String == "" {
			return Domainf("task %d has no branch; nothing was landing (§7)", task)
		}
		if wasArchived {
			if status == "success" {
				// Response-loss replay after the success transaction committed.
				// Preserve the one-way truth without rewriting the row or packet.
				archived = true
				return nil
			}
			return Domainf("task %d already landed successfully; refusing stale failure report (§7)", task)
		}
		switch status {
		case "success":
			if _, err := q.ExecContext(ctx,
				`UPDATE tasks SET archived = 1 WHERE id = ?`, task); err != nil {
				return err
			}
			archived = true
		case "failure":
			if reason == "" {
				reason = "landing failed"
			}
			if _, err := q.ExecContext(ctx, `
				UPDATE tasks SET blocked = 1, blocked_reason = ? WHERE id = ?`,
				reason, task); err != nil {
				return err
			}
			blocked = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"task_id": task, "archived": archived, "blocked": blocked}, nil
}
