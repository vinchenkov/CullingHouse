package verbs

import (
	"database/sql"
	"os"
	"strings"
	"testing"
)

// isrSeed inserts a promoted scope='initiative' arc row (id 7) plus a live
// Worker-tier setup run holding the lease keyed on it — the state the dispatch
// InitiativeSetup lane leaves for the resident.
func isrSeed(t *testing.T, db *sql.DB) {
	t.Helper()
	dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
		dispatch_retries, origin, worksource, target_ref)
		VALUES (7, 'arc', 'initiative', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`, dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET status='seeded', branch='mc/initiative-7' WHERE id=7`)
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
}

func initiativeReceipt(runID string, id int64) InitiativeSetupReceipt {
	return InitiativeSetupReceipt{
		RunID: runID, InitiativeID: id,
		StoreRoot:    TaskSetupIdentity{Device: "17", Inode: "42", OwnerUID: os.Getuid()},
		WorktreeRoot: TaskSetupIdentity{Device: "17", Inode: "99", OwnerUID: os.Getuid()},
		CutSHA:       strings.Repeat("a", 40),
	}
}

func TestRegisterInitiativeSetupIsFencedAndIdempotent(t *testing.T) {
	db := dvSpine(t)
	dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
		dispatch_retries, origin, worksource, target_ref)
		VALUES (7, 'arc', 'initiative', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`, dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET status='seeded', branch='mc/initiative-7' WHERE id=7`)
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)

	want := initiativeReceipt("setup-run", 7)
	if got, err := RegisterInitiativeSetup(db, want); err != nil || got != want {
		t.Fatalf("first registration = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	// An exact retry is idempotent (lost-response replay).
	if got, err := RegisterInitiativeSetup(db, want); err != nil || got != want {
		t.Fatalf("exact retry = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	// A different cut is refused — the store is never re-cut (D3).
	recut := want
	recut.CutSHA = strings.Repeat("b", 40)
	if _, err := RegisterInitiativeSetup(db, recut); err == nil {
		t.Fatal("a re-cut (changed cut SHA) was accepted")
	}
	// A different store root is refused.
	moved := want
	moved.StoreRoot.Inode = "777"
	if _, err := RegisterInitiativeSetup(db, moved); err == nil {
		t.Fatal("a changed store root identity was accepted")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM initiative_setup_receipts WHERE initiative_id=7`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipt count = (%d, %v), want (1, nil)", count, err)
	}
}

func TestRegisterInitiativeSetupRejectsStaleOrWrongFence(t *testing.T) {
	db := dvSpine(t)
	dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
		dispatch_retries, origin, worksource, target_ref)
		VALUES (7, 'arc', 'initiative', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`, dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET status='seeded', branch='mc/initiative-7' WHERE id=7`)
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	// The lease names a different run: not fenced to this setup.
	dvExec(t, db, `UPDATE lock SET run_id='other-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	if _, err := RegisterInitiativeSetup(db, initiativeReceipt("setup-run", 7)); err == nil {
		t.Fatal("a stale lease registration was accepted")
	}
	// The lease is this run but keyed on a different initiative than the receipt.
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7 WHERE id=1`)
	if _, err := RegisterInitiativeSetup(db, initiativeReceipt("setup-run", 8)); err == nil {
		t.Fatal("a wrong-initiative registration was accepted")
	}
}

func TestRegisterInitiativeSetupRejectsForeignOwner(t *testing.T) {
	db := dvSpine(t)
	dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
		dispatch_retries, origin, worksource, target_ref)
		VALUES (7, 'arc', 'initiative', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`, dvOld.Format(spineTime))
	dvExec(t, db, `UPDATE tasks SET status='seeded', branch='mc/initiative-7' WHERE id=7`)
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	foreign := initiativeReceipt("setup-run", 7)
	foreign.WorktreeRoot.OwnerUID = os.Getuid() + 1
	if _, err := RegisterInitiativeSetup(db, foreign); err == nil {
		t.Fatal("a foreign-owned worktree root was accepted")
	}
}

// The seal-free lease terminal ends the setup run and frees the singleton lease
// without charging a dispatch retry, and a lost-response replay is idempotent.
func TestContinueInitiativeSetupFencesReleasesWithoutCharging(t *testing.T) {
	db := dvSpine(t)
	isrSeed(t, db)
	if _, err := RegisterInitiativeSetup(db, initiativeReceipt("setup-run", 7)); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := ContinueInitiativeSetup(db, "setup-run")
	if err != nil || got.RunID != "setup-run" || got.InitiativeID != 7 || got.AlreadyContinued {
		t.Fatalf("continue = (%+v, %v), want a fresh completion for setup-run/initiative 7", got, err)
	}
	var outcome sql.NullString
	if err := db.QueryRow(`SELECT outcome FROM runs WHERE id='setup-run'`).Scan(&outcome); err != nil || !outcome.Valid || outcome.String != "setup-complete" {
		t.Fatalf("setup run outcome = (%v, %v), want setup-complete", outcome, err)
	}
	var retries int
	if err := db.QueryRow(`SELECT dispatch_retries FROM tasks WHERE id=7`).Scan(&retries); err != nil || retries != 3 {
		t.Fatalf("continuation charged dispatch retries = (%d, %v), want 3", retries, err)
	}
	var lockRun sql.NullString
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id=1`).Scan(&lockRun); err != nil || lockRun.Valid {
		t.Fatalf("continuation did not release the setup lease: (%v, %v)", lockRun, err)
	}

	replay, err := ContinueInitiativeSetup(db, "setup-run")
	if err != nil || !replay.AlreadyContinued || replay.InitiativeID != 7 {
		t.Fatalf("lost-response replay = (%+v, %v), want the same continued initiative", replay, err)
	}
}

func TestContinueInitiativeSetupRejectsWithoutReceiptWithoutMutation(t *testing.T) {
	db := dvSpine(t)
	isrSeed(t, db)
	// No receipt registered yet: the continuation must refuse and mutate nothing.
	if _, err := ContinueInitiativeSetup(db, "setup-run"); err == nil {
		t.Fatal("continuation accepted before the cut receipt existed")
	}
	var ended, lockRun sql.NullString
	if err := db.QueryRow(`SELECT ended_at FROM runs WHERE id='setup-run'`).Scan(&ended); err != nil || ended.Valid {
		t.Fatalf("unreceipted continuation ended the run: (%v, %v)", ended, err)
	}
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id=1`).Scan(&lockRun); err != nil || !lockRun.Valid || lockRun.String != "setup-run" {
		t.Fatalf("unreceipted continuation changed the lease: (%v, %v)", lockRun, err)
	}
}
