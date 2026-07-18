package verbs

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// tcRegistered arms the spine with the live standalone Worker run/lease for
// task 7 and registers the durable receipt from the real root identity.
func tcRegistered(t *testing.T, db *sql.DB, root string) TaskSetupReceipt {
	t.Helper()
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	receipt := TaskSetupReceipt{RunID: "setup-run", TaskID: 7, Root: TaskSetupIdentity{
		Device:   strconv.FormatUint(uint64(st.Dev), 10),
		Inode:    strconv.FormatUint(st.Ino, 10),
		OwnerUID: int(st.Uid),
	}}
	if _, err := RegisterFirstTaskSetup(db, receipt); err != nil {
		t.Fatalf("register receipt: %v", err)
	}
	return receipt
}

// tcMaterialized builds a real materialized first-task store for task 7 under a
// fresh workspace, arms the spine with the live Worker + receipt, and returns
// (workspaceRoot, taskRoot, SetupResult) — the exact host-side input to
// RecordFirstTaskSetupClosure.
func tcMaterialized(t *testing.T, db *sql.DB) (string, string, SetupResult) {
	t.Helper()
	ws := grWorkspace(t)
	root, res := tcMaterializedAt(t, db, ws)
	return ws, root, res
}

// tcMaterializedAt is tcMaterialized with the Worksource root supplied by an
// integration caller that also needs dispatch to attest the same tree.
func tcMaterializedAt(t *testing.T, db *sql.DB, ws string) (string, SetupResult) {
	t.Helper()
	src, _, objfmt := buildSourceRepo(t)
	root := filepath.Join(ws, ".mission-control", "tasks", "task-7")
	for _, d := range []string{"source", "git"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	res, err := MaterializeFirstTaskStore(src, root, FirstTaskSetupSpec{
		TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: objfmt,
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	tcRegistered(t, db, root)
	return root, res
}

func TestRecordFirstTaskSetupClosureRecordsTheAssignmentAndInspects(t *testing.T) {
	db := dvSpine(t)
	ws, root, res := tcMaterialized(t, db)

	got, rows, err := RecordFirstTaskSetupClosure(db, "setup-run", ws, res)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if got.Canonical != root || got.Receipt.TaskID != 7 {
		t.Fatalf("attested root = %+v, want %q for task 7", got, root)
	}
	if len(rows) != len(taskPlanRows(7)) {
		t.Fatalf("inspected rows = %d, want %d", len(rows), len(taskPlanRows(7)))
	}
	a, err := ReadFirstTaskAssignment(db, "setup-run")
	if err != nil || a.BaseSHA != res.BaseSHA || a.ClosureDigest != res.ClosureDigest ||
		a.ObjectFormat != res.ObjectFormat || a.Branch != "mc/task-7" || a.TaskRootKey != ".mission-control/tasks/task-7" {
		t.Fatalf("recorded assignment = (%+v, %v), want it to mirror the result", a, err)
	}
}

func TestRecordFirstTaskSetupClosureRefusesADigestMismatch(t *testing.T) {
	db := dvSpine(t)
	ws, _, res := tcMaterialized(t, db)
	res.ClosureDigest = strings.Repeat("0", 64)
	if _, _, err := RecordFirstTaskSetupClosure(db, "setup-run", ws, res); err == nil {
		t.Fatal("a store whose pack disagrees with the recorded closure digest was recorded")
	}
	if _, err := ReadFirstTaskAssignment(db, "setup-run"); err == nil {
		t.Fatal("a refused record still left a durable assignment")
	}
}

func TestRecordFirstTaskSetupClosureRefusesABaseSHAMismatch(t *testing.T) {
	db := dvSpine(t)
	ws, _, res := tcMaterialized(t, db)
	res.BaseSHA = strings.Repeat("b", len(res.BaseSHA))
	if _, _, err := RecordFirstTaskSetupClosure(db, "setup-run", ws, res); err == nil {
		t.Fatal("a store whose ref disagrees with the recorded base SHA was recorded")
	}
}

func TestRecordFirstTaskSetupClosureRequiresTheLiveReceipt(t *testing.T) {
	db := dvSpine(t)
	ws, _, res := tcMaterialized(t, db)
	dvExec(t, db, `UPDATE runs SET ended_at=datetime('now') WHERE id='setup-run'`)
	if _, _, err := RecordFirstTaskSetupClosure(db, "setup-run", ws, res); err == nil {
		t.Fatal("an ended run recorded a closure")
	}
}

func TestContinueFirstTaskSetupFencesThenReleasesWithoutChargingTheTask(t *testing.T) {
	db := dvSpine(t)
	ws, _, res := tcMaterialized(t, db)
	if _, _, err := RecordFirstTaskSetupClosure(db, "setup-run", ws, res); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := ContinueFirstTaskSetup(db, "setup-run")
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if got.RunID != "setup-run" || got.TaskID != 7 || got.AlreadyContinued {
		t.Fatalf("continue = %+v, want fresh completion for setup-run/task 7", got)
	}
	var outcome sql.NullString
	var retries int
	if err := db.QueryRow(`SELECT outcome FROM runs WHERE id='setup-run'`).Scan(&outcome); err != nil || !outcome.Valid || outcome.String != "setup-complete" {
		t.Fatalf("setup run outcome = (%v, %v), want setup-complete", outcome, err)
	}
	if err := db.QueryRow(`SELECT dispatch_retries FROM tasks WHERE id=7`).Scan(&retries); err != nil || retries != 3 {
		t.Fatalf("continuation charged dispatch retries = (%d, %v), want 3", retries, err)
	}
	var lockRun sql.NullString
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id=1`).Scan(&lockRun); err != nil || lockRun.Valid {
		t.Fatalf("continuation did not release the setup lease: (%v, %v)", lockRun, err)
	}

	replay, err := ContinueFirstTaskSetup(db, "setup-run")
	if err != nil || !replay.AlreadyContinued || replay.TaskID != 7 {
		t.Fatalf("lost-response replay = (%+v, %v), want the same continued task", replay, err)
	}
}

func TestContinueFirstTaskSetupRejectsUnrecordedOrStaleRunsWithoutMutation(t *testing.T) {
	db := dvSpine(t)
	ws, _, res := tcMaterialized(t, db)
	if _, err := ContinueFirstTaskSetup(db, "setup-run"); err == nil {
		t.Fatal("continuation accepted before the closure record")
	}
	var ended sql.NullString
	var lockRun sql.NullString
	if err := db.QueryRow(`SELECT ended_at FROM runs WHERE id='setup-run'`).Scan(&ended); err != nil || ended.Valid {
		t.Fatalf("unrecorded continuation ended run = (%v, %v)", ended, err)
	}
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id=1`).Scan(&lockRun); err != nil || !lockRun.Valid || lockRun.String != "setup-run" {
		t.Fatalf("unrecorded continuation changed lease = (%v, %v)", lockRun, err)
	}

	if _, _, err := RecordFirstTaskSetupClosure(db, "setup-run", ws, res); err != nil {
		t.Fatalf("record: %v", err)
	}
	dvExec(t, db, `UPDATE lock SET run_id='other-run' WHERE id=1`)
	if _, err := ContinueFirstTaskSetup(db, "setup-run"); err == nil {
		t.Fatal("stale continuation released a different lease holder")
	}
}
