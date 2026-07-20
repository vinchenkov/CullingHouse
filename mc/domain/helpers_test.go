package domain_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"mc/domain"
	"mc/substrate"
)

// openSpine creates a fresh spine on a real temp file (WAL needs a file,
// Inv. 24 / spike S5) and seeds the fixture worksource.
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

// tx runs fn under BEGIN IMMEDIATE — the production transaction shape domain
// functions are scoped to (contract §1.1). fn's error rolls back and returns.
func tx(t *testing.T, db *sql.DB, fn func(ctx context.Context, q domain.Q) error) error {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("begin immediate: %v", err)
	}
	if err := fn(ctx, conn); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return err
	}
	return nil
}

// mustTx asserts fn commits.
func mustTx(t *testing.T, db *sql.DB, fn func(ctx context.Context, q domain.Q) error) {
	t.Helper()
	if err := tx(t, db, fn); err != nil {
		t.Fatalf("want commit, got: %v", err)
	}
}

// wantCode asserts fn is rejected with the named DomainError code
// (contract §1.1: every domain rejection carries a stable Code slug).
func wantCode(t *testing.T, db *sql.DB, code string, fn func(ctx context.Context, q domain.Q) error) {
	t.Helper()
	err := tx(t, db, fn)
	if err == nil {
		t.Fatalf("want DomainError %q, got commit", code)
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("want DomainError %q, got %T: %v", code, err, err)
	}
	if de.Code != code {
		t.Fatalf("DomainError code = %q (%s), want %q", de.Code, de.Msg, code)
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) sql.Result {
	t.Helper()
	res, err := db.Exec(q, args...)
	if err != nil {
		t.Fatalf("want commit, got abort:\n  %s\n  %v", q, err)
	}
	return res
}

// wantAbort asserts the raw SQL write aborts on the substrate lattice — the
// backstop-agreement half of each rule-class case (contract §1.1).
func wantAbort(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err == nil {
		t.Fatalf("want substrate abort, got commit:\n  %s", q)
	}
}

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

func oneInt(t *testing.T, db *sql.DB, q string, args ...any) int64 {
	t.Helper()
	var v int64
	if err := db.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return v
}

func taskStr(t *testing.T, db *sql.DB, id int64, col string) string {
	t.Helper()
	return oneStr(t, db, `SELECT `+col+` FROM tasks WHERE id = ?`, id)
}

func taskInt(t *testing.T, db *sql.DB, id int64, col string) int64 {
	t.Helper()
	return oneInt(t, db, `SELECT `+col+` FROM tasks WHERE id = ?`, id)
}

var walkOrder = map[string][]string{
	"proposed": {},
	"seeded":   {"seeded"},
	"worked":   {"seeded", "worked"},
	"verified": {"seeded", "worked", "verified"},
	"packaged": {"seeded", "worked", "verified", "packaged"},
}

// mkTask inserts a row and walks it along legal edges to the target status.
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

// mkRun inserts a bare runs row (the verdict record's landing site).
func mkRun(t *testing.T, db *sql.DB, id, role string, subject int64) {
	t.Helper()
	mustExec(t, db,
		`INSERT INTO runs (id, tier, role, worksource, subject) VALUES (?, 'pipeline', ?, 'ws', ?)`,
		id, role, subject)
}

// mkAssignment inserts the ADR-016 D5 first-task closure assignment, the row
// that makes a standalone task "assigned" — i.e. sealed, with its reviewed
// commit in the task-local store and its branch name here rather than in
// tasks.branch.
func mkAssignment(t *testing.T, db *sql.DB, taskID int64) {
	t.Helper()
	mustExec(t, db, `
		INSERT INTO task_assignments
			(task_id, target_ref, branch, task_root_key, object_format,
			 base_sha, local_repo_uuid, closure_digest)
		VALUES (?, 'main', ?, 'task-root', 'sha1', ?, '0a1b2c3d-4e5f', ?)`,
		taskID, fmt.Sprintf("mc/task-%d", taskID),
		strings.Repeat("a", 40), strings.Repeat("b", 64))
}

// mkPacket inserts a packet for a packaged task.
func mkPacket(t *testing.T, db *sql.DB, taskID int64) {
	t.Helper()
	mustExec(t, db,
		`INSERT INTO review_packets (task_id, render_path) VALUES (?, 'packet.html')`, taskID)
}
