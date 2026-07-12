package verbs

import (
	"context"
	"database/sql"
)

// TaskAdd files human-seeded work into the proposed pool as origin:user
// (§18, contract §2). target_ref defaults to 'main' at add time (contract
// Ambiguity A4).
func TaskAdd(db *sql.DB, title, worksource, description string, priority *int) (any, error) {
	if title == "" {
		return nil, Usagef("mc task add requires a title")
	}
	if worksource == "" {
		return nil, Usagef("mc task add requires --worksource")
	}
	var taskID int64
	err := inTx(db, func(ctx context.Context, q Q) error {
		pri := 2 // schema default P2
		if priority != nil {
			pri = *priority
		}
		res, err := q.ExecContext(ctx, `
			INSERT INTO tasks (title, description, priority, origin, worksource, target_ref)
			VALUES (?, ?, ?, 'user', ?, 'main')`,
			title, nullIfEmpty(description), pri, worksource)
		if err != nil {
			return err
		}
		taskID, err = res.LastInsertId()
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
		       branch, verified_sha, target_ref
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
