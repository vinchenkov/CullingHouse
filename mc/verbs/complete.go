package verbs

import (
	"context"
	"database/sql"
)

// CompleteArgs carries mc complete's skeleton flags (contract §2). The §18
// flags --reason/--needs-operator/--infra/--correction-count parse in cmd/mc
// but are rejected as deferred [P2] before reaching here.
type CompleteArgs struct {
	Task    int64
	Run     string
	Status  string // 'worked' (Worker terminal) | 'packaged' (Packager terminal)
	Branch  string // Worker: records tasks.branch (contract Ambiguity A2)
	Outputs string // Packager: rides into review_packets.render_path
}

// Complete is the run's terminal action (ADR-001 D3): it advances the
// subject's status, releases the lease, and stamps the runs row — one
// transaction (Inv. 10). It never dispatches (Inv. 3). Fenced: --run must
// match the live lease and the lease's subject must be the task (§10, §18
// deny rule 2).
func Complete(db *sql.DB, id *RunIdentity, a CompleteArgs) (any, error) {
	var wantRole string
	switch a.Status {
	case "worked":
		wantRole = "worker"
	case "packaged":
		wantRole = "packager"
	default:
		return nil, Usagef("mc complete --status must be worked|packaged in the skeleton (contract §2)")
	}
	if err := requireRole(id, wantRole); err != nil {
		return nil, err
	}
	if a.Run == "" {
		return nil, Usagef("mc complete requires --run (the fencing token, §10)")
	}

	err := inTx(db, func(ctx context.Context, q Q) error {
		subject, err := fenceRun(ctx, q, a.Run)
		if err != nil {
			return err
		}
		if subject == nil || *subject != a.Task {
			return Domainf("task %d is not the live lease's subject (§10 fencing)", a.Task)
		}
		switch a.Status {
		case "worked":
			// seeded → worked; the substrate's transition trigger backstops.
			if _, err := q.ExecContext(ctx,
				`UPDATE tasks SET status = 'worked' WHERE id = ?`, a.Task); err != nil {
				return err
			}
			if a.Branch != "" {
				if _, err := q.ExecContext(ctx,
					`UPDATE tasks SET branch = ? WHERE id = ?`, a.Branch, a.Task); err != nil {
					return err
				}
			}
		case "packaged":
			// verified → packaged AND packet birth in the same transaction
			// (ADR-001 "not new verbs"; Inv. 11; the WIP-cap trigger fires
			// here).
			if _, err := q.ExecContext(ctx,
				`UPDATE tasks SET status = 'packaged' WHERE id = ?`, a.Task); err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `
				INSERT INTO review_packets (task_id, render_path)
				VALUES (?, ?)`, a.Task, nullIfEmpty(a.Outputs)); err != nil {
				return err
			}
		}
		if err := endRun(ctx, q, a.Run, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, a.Run)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"task_id": a.Task, "status": a.Status}, nil
}
