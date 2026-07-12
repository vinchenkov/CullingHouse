package verbs

import "database/sql"

// LockGet returns the lock row as JSON — a pure read under §18's
// `mc <record> get/list` surface. The Docker e2e asserts lease state
// (held/free, owner, heartbeat advancement — contract §7 ladder steps 3–5)
// through mc because it cannot open the spine volume (forced faithfulness);
// nothing else needs this verb in the skeleton.
func LockGet(db *sql.DB) (any, error) {
	rows, err := db.Query(`
		SELECT run_id, worksource, subject, owner, acquired_at,
		       last_heartbeat_at, hard_deadline_at, timeout_minutes,
		       grace_minutes, heartbeat_interval_s, spawn_grace_s,
		       hard_deadline_minutes
		FROM lock WHERE id = 1`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out, err := rowsToMaps(rows)
	if err != nil {
		return nil, classify(err)
	}
	if len(out) == 0 {
		return nil, Domainf("no lock row (uninitialized spine)")
	}
	return out[0], nil
}

// RunList returns every runs row as JSON (reads only; §18 `mc <record>
// get/list`). Run rows are never deleted (Inv. 26), so this is the e2e's
// history channel: which roles ran, when they ended, their session locators.
func RunList(db *sql.DB) (any, error) {
	rows, err := db.Query(`
		SELECT id, tier, role, worksource, subject, created_at, ended_at,
		       outcome, session_path, binding, native_session_ref,
		       trace_filename, pool_snapshot
		FROM runs ORDER BY created_at, id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out, err := rowsToMaps(rows)
	if err != nil {
		return nil, classify(err)
	}
	return map[string]any{"runs": out}, nil
}
