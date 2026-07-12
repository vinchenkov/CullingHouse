package verbs

import (
	"context"
	"database/sql"
)

// PacketDecide is the operator decision verb (§7, contract §2). Skeleton
// implements --approve: a pure state write (decision='approved', decided_at)
// — no dispatch, no filesystem effect (Inv. 2). Landing-pending is derived,
// never a column. A branchless task (artifact-plane deliverable) archives
// synchronously (§7). --revise/--cancel parse fully but are deferred [P2];
// the reason asymmetry (§7: required for revise/cancel, forbidden for
// approve) is validated now.
func PacketDecide(db *sql.DB, task int64, decision, reason string) (any, error) {
	switch decision {
	case "approve":
		if reason != "" {
			return nil, Domainf("--reason is forbidden for approve (§7: asymmetric by design)")
		}
	case "revise", "cancel":
		if reason == "" {
			return nil, Domainf("--reason is required for %s (§7: asymmetric by design)", decision)
		}
		return nil, Domainf("%s is deferred to Phase 2 (contract §2 [P2])", decision)
	default:
		return nil, Usagef("mc packet decide requires exactly one of --approve, --revise, --cancel")
	}

	var archived bool
	err := inTx(db, func(ctx context.Context, q Q) error {
		var status string
		var branch, prior sql.NullString
		err := q.QueryRowContext(ctx,
			`SELECT status, branch, decision FROM tasks WHERE id = ?`, task).
			Scan(&status, &branch, &prior)
		if err == sql.ErrNoRows {
			return Domainf("no task %d", task)
		}
		if err != nil {
			return err
		}
		if prior.Valid {
			return Domainf("task %d is already decided (%s); decisions never rewrite (§5)", task, prior.String)
		}
		// The substrate CHECK (approved requires packaged) backstops this;
		// surfacing it here names the rule.
		if status != "packaged" {
			return Domainf("only packaged work can be approved (§4, §6); task %d is %q", task, status)
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE tasks SET decision = 'approved', decided_at = datetime('now')
			WHERE id = ?`, task); err != nil {
			return err
		}
		// Branchless: nothing staged against the workspace — approve
		// completes and archives synchronously (§7); the packet-archive
		// cascade trigger fires with it.
		if !branch.Valid || branch.String == "" {
			if _, err := q.ExecContext(ctx,
				`UPDATE tasks SET archived = 1 WHERE id = ?`, task); err != nil {
				return err
			}
			archived = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"task_id": task, "decision": "approved", "archived": archived}, nil
}

// PacketList returns every review_packets row as JSON (contract §2: the
// e2e's assertion channel; reads take no transaction).
func PacketList(db *sql.DB) (any, error) {
	rows, err := db.Query(`
		SELECT task_id, render_path, thesis, refine_streak, saturated,
		       archived, created_at
		FROM review_packets ORDER BY task_id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out, err := rowsToMaps(rows)
	if err != nil {
		return nil, classify(err)
	}
	return map[string]any{"packets": out}, nil
}
