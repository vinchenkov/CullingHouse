package verbs

import (
	"context"
	"database/sql"

	"mc/domain"
)

// TaskAdd files human-seeded work into the proposed pool as origin:user
// (§18, contract §2). target_ref defaults to 'main' at add time (contract
// Ambiguity A4).
func TaskAdd(db *sql.DB, id *RunIdentity, title, worksource, description string, priority *int) (any, error) {
	if err := RequireOperatorVerb(id, "task.add"); err != nil {
		return nil, err
	}
	if title == "" {
		return nil, Usagef("mc task add requires a title")
	}
	if worksource == "" {
		return nil, Usagef("mc task add requires --worksource")
	}
	var taskID int64
	err := inTx(db, func(ctx context.Context, q Q) error {
		if err := requireOperatorVerbTx(ctx, q, id, "task.add"); err != nil {
			return err
		}
		var err error
		taskID, err = domain.BirthProposal(ctx, q, domain.ProposalArgs{
			Title:       title,
			Description: description,
			Priority:    priority,
			Origin:      "user",
			Worksource:  worksource,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"task_id": taskID}, nil
}

// TaskGet returns one task row as JSON — the e2e's assertion channel into
// the spine (contract §2).
func TaskGet(db *sql.DB, id int64) (any, error) {
	rows, err := db.Query(`
		SELECT id, title, description, scope, initiative_id, priority,
		       created_at, status, stage_rank, stage_entered_at,
		       correction_count, blocked, blocked_reason, dispatch_retries,
		       decision, decided_at, archived, origin, worksource,
		       branch, verified_sha, target_ref, refine_notes
		FROM tasks WHERE id = ?`, id)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out, err := rowsToMaps(rows)
	if err != nil {
		return nil, classify(err)
	}
	if len(out) == 0 {
		return nil, Domainf("no task %d", id)
	}
	return out[0], nil
}

// TaskBlock is `mc task block <task> --reason …` (§18; ADR-001 D6): host
// scope, or a pipeline run for its **own subject only** — fenced through the
// run's identity (run.json run_id against the live lease; deny rule 2). It
// never touches the lease: blocking mid-run is not a terminal.
func TaskBlock(db *sql.DB, id *RunIdentity, task int64, reason string) (any, error) {
	if reason == "" {
		return nil, Usagef("mc task block requires --reason (§4)")
	}
	pipeline := id != nil && id.Tier == "pipeline"
	if !pipeline {
		if err := RequireOperatorVerb(id, "task.block"); err != nil {
			return nil, err
		}
	}
	err := inTx(db, func(ctx context.Context, q Q) error {
		if pipeline {
			// Pipeline caller: own subject only, fenced to the live lease.
			subject, err := fenceRun(ctx, q, id.RunID)
			if err != nil {
				return err
			}
			if subject == nil || *subject != task {
				return Domainf("a pipeline run blocks only its own subject (ADR-001 D6); task %d is not it", task)
			}
		} else if err := requireOperatorVerbTx(ctx, q, id, "task.block"); err != nil {
			return err
		}
		return domain.Block(ctx, q, task, reason)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"task_id": task, "blocked": true}, nil
}

// TaskUnblock is `mc task unblock <task>` (§18; ADR-001 D6): an operator
// verb, denied to pipeline runs (deny rule 1) — resuming work is the
// operator's judgment.
func TaskUnblock(db *sql.DB, id *RunIdentity, task int64) (any, error) {
	if err := RequireOperatorVerb(id, "task.unblock"); err != nil {
		return nil, err
	}
	err := inTx(db, func(ctx context.Context, q Q) error {
		if err := requireOperatorVerbTx(ctx, q, id, "task.unblock"); err != nil {
			return err
		}
		return domain.Unblock(ctx, q, task)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"task_id": task, "blocked": false}, nil
}

// TaskInterrupt is the operator's in-flight "scratch that" (§15.3): cancel
// the exact live subject, end its Run, and clear its lease in one transaction.
// The returned effect is only the host instruction to stop that exact
// container; no successor is dispatched.
func TaskInterrupt(db *sql.DB, id *RunIdentity, task int64) (any, error) {
	if err := RequireOperatorVerb(id, "task.interrupt"); err != nil {
		return nil, err
	}
	var runID string
	err := inTx(db, func(ctx context.Context, q Q) error {
		if err := requireOperatorVerbTx(ctx, q, id, "task.interrupt"); err != nil {
			return err
		}
		var lockRun sql.NullString
		var subject sql.NullInt64
		if err := q.QueryRowContext(ctx,
			`SELECT run_id, subject FROM lock WHERE id = 1`,
		).Scan(&lockRun, &subject); err != nil {
			return err
		}
		if !lockRun.Valid || !subject.Valid || subject.Int64 != task {
			return Domainf("task %d is not the live lease subject; nothing to interrupt", task)
		}
		runID = lockRun.String
		if err := domain.Cancel(ctx, q, task, "operator_interrupt"); err != nil {
			return err
		}
		ended, err := q.ExecContext(ctx, `
			UPDATE runs SET ended_at = datetime('now'), outcome = 'interrupted'
			WHERE id = ? AND ended_at IS NULL`, runID)
		if err != nil {
			return err
		}
		n, err := ended.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return Domainf("live lease Run %q was already ended", runID)
		}
		return domain.Release(ctx, q, runID)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"action": "interrupt", "task_id": task, "run_id": runID,
		"stop_container": true,
	}, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// rowsToMaps renders generic rows as JSON-ready maps (reads only; contract
// §2 "row(s) as JSON").
func rowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				m[c] = string(b)
			} else {
				m[c] = vals[i]
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
