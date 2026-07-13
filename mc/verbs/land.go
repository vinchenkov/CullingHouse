package verbs

import (
	"context"
	"database/sql"
)

// LandReport is the resident's report of a landing result (§7, ADR-001 D6).
// success: archived=1 (the trigger cascades to the packet); the lease is
// untouched — landing holds no lease (§7). failure: blocked=1 with the
// landing reason, nothing archived, the slot stays held (write implemented;
// broader tests are [P2] per contract §2).
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
		var decision sql.NullString
		err := q.QueryRowContext(ctx,
			`SELECT status, decision FROM tasks WHERE id = ?`, task).
			Scan(&st, &decision)
		if err == sql.ErrNoRows {
			return Domainf("no task %d", task)
		}
		if err != nil {
			return err
		}
		if !decision.Valid || decision.String != "approved" {
			return Domainf("task %d is not approved; nothing was landing (§7)", task)
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
