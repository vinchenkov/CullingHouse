package substrate_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"mc/substrate"
)

// openSpine creates a fresh spine on a real temp file (never :memory: — WAL
// needs a file, spec Inv. 24 / spike S5), applies the schema, and seeds the
// one fixture worksource every task row references.
func openSpine(t *testing.T) *sql.DB {
	t.Helper()
	db, err := substrate.Open(filepath.Join(t.TempDir(), "spine.db"))
	if err != nil {
		t.Fatalf("open spine: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := substrate.Init(db); err != nil {
		t.Fatalf("init spine: %v", err)
	}
	mustExec(t, db, `INSERT INTO worksources (id, title, kind) VALUES ('ws', 'Test Worksource', 'repo')`)
	return db
}

// mustExec asserts the statement commits.
func mustExec(t *testing.T, db *sql.DB, q string, args ...any) sql.Result {
	t.Helper()
	res, err := db.Exec(q, args...)
	if err != nil {
		t.Fatalf("want commit, got abort:\n  %s\n  %v", q, err)
	}
	return res
}

// wantAbort asserts the statement aborts.
func wantAbort(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err == nil {
		t.Fatalf("want abort, got commit:\n  %s", q)
	}
}

// oneStr returns a single scalar as a string; NULL comes back as "<NULL>".
func oneStr(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	var v sql.NullString
	if err := db.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	if !v.Valid {
		return "<NULL>"
	}
	return v.String
}

// oneInt returns a single scalar as an int64.
func oneInt(t *testing.T, db *sql.DB, q string, args ...any) int64 {
	t.Helper()
	var v int64
	if err := db.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return v
}

var statuses = []string{"proposed", "seeded", "worked", "verified", "packaged"}

// walkOrder lists the legal-edge walk from birth (proposed) to each status.
var walkOrder = map[string][]string{
	"proposed": {},
	"seeded":   {"seeded"},
	"worked":   {"seeded", "worked"},
	"verified": {"seeded", "worked", "verified"},
	"packaged": {"seeded", "worked", "verified", "packaged"},
}

// mkTask inserts a row of the given scope and walks it along legal edges to
// the target status.
func mkTask(t *testing.T, db *sql.DB, scope, status string) int64 {
	t.Helper()
	res := mustExec(t, db,
		`INSERT INTO tasks (title, scope, worksource) VALUES ('fixture', ?, 'ws')`, scope)
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	for _, s := range walkOrder[status] {
		mustExec(t, db, `UPDATE tasks SET status = ? WHERE id = ?`, s, id)
	}
	return id
}

// mkChild inserts a wave child (born seeded) into the given initiative.
func mkChild(t *testing.T, db *sql.DB, initiative int64) int64 {
	t.Helper()
	res := mustExec(t, db,
		`INSERT INTO tasks (title, scope, status, initiative_id, worksource)
		 VALUES ('child', 'task', 'seeded', ?, 'ws')`, initiative)
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// walkChild advances a wave child from seeded to the target status.
func walkChild(t *testing.T, db *sql.DB, id int64, status string) {
	t.Helper()
	for _, s := range walkOrder[status] {
		if s == "seeded" {
			continue // children are born seeded
		}
		mustExec(t, db, `UPDATE tasks SET status = ? WHERE id = ?`, s, id)
	}
}

// mkPacket inserts a review packet for the given (packaged) task.
func mkPacket(t *testing.T, db *sql.DB, taskID int64) {
	t.Helper()
	mustExec(t, db,
		`INSERT INTO review_packets (task_id, render_path, thesis) VALUES (?, 'packet.html', 'thesis')`,
		taskID)
}

func mkHomieSession(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	mustExec(t, db, `
		INSERT INTO homie_sessions
			(id, container_name, verb_allowlist, session_path, binding)
		VALUES (?, ?, '["homie.list"]', ?, 'fake/fake')`,
		id, "mc-homie-"+id, "sessions/"+id)
}

// cancelTask applies the operator cancel bookkeeping: decision + timestamp +
// archive, one transaction (§6, §7).
func cancelTask(t *testing.T, db *sql.DB, id int64) {
	t.Helper()
	mustExec(t, db,
		`UPDATE tasks SET decision = 'cancelled', decided_at = datetime('now'), archived = 1 WHERE id = ?`,
		id)
}

func taskStr(t *testing.T, db *sql.DB, id int64, col string) string {
	t.Helper()
	return oneStr(t, db, `SELECT `+col+` FROM tasks WHERE id = ?`, id)
}

func taskInt(t *testing.T, db *sql.DB, id int64, col string) int64 {
	t.Helper()
	return oneInt(t, db, `SELECT `+col+` FROM tasks WHERE id = ?`, id)
}
