package verbs

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func setupReceipt(run string, task int64) TaskSetupReceipt {
	return TaskSetupReceipt{RunID: run, TaskID: task, Root: TaskSetupIdentity{Device: "16777220", Inode: "123456", OwnerUID: 501}}
}

func TestRegisterFirstTaskSetupIsRunTaskFencedAndIdempotent(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)

	want := setupReceipt("setup-run", 7)
	if got, err := RegisterFirstTaskSetup(db, want); err != nil || got != want {
		t.Fatalf("first registration = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	if got, err := RegisterFirstTaskSetup(db, want); err != nil || got != want {
		t.Fatalf("exact retry = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	changed := want
	changed.Root.Inode = "123457"
	if _, err := RegisterFirstTaskSetup(db, changed); err == nil {
		t.Fatal("changed retry identity was accepted")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_setup_receipts WHERE run_id='setup-run'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipt count = (%d, %v), want (1, nil)", count, err)
	}
}

func TestRegisterFirstTaskSetupRejectsStaleOrWrongRunTask(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='other-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	if _, err := RegisterFirstTaskSetup(db, setupReceipt("setup-run", 7)); err == nil {
		t.Fatal("stale lease registration was accepted")
	}
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7 WHERE id=1`)
	if _, err := RegisterFirstTaskSetup(db, setupReceipt("setup-run", 8)); err == nil {
		t.Fatal("wrong task registration was accepted")
	}
}

func TestRegisterFirstTaskSetupRejectsForeignOwner(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	receipt := setupReceipt("setup-run", 7)
	receipt.Root.OwnerUID = os.Getuid() + 1
	if _, err := RegisterFirstTaskSetup(db, receipt); err == nil {
		t.Fatal("foreign-owned task root receipt was accepted")
	}
}

func TestReadFirstTaskSetupRequiresTheLiveRunTaskLeaseAndExactReceipt(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	want := setupReceipt("setup-run", 7)
	if _, err := RegisterFirstTaskSetup(db, want); err != nil {
		t.Fatalf("register receipt: %v", err)
	}

	if got, err := ReadFirstTaskSetup(db, "setup-run"); err != nil || got != want {
		t.Fatalf("read = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	dvExec(t, db, `UPDATE lock SET run_id='other-run' WHERE id=1`)
	if _, err := ReadFirstTaskSetup(db, "setup-run"); err == nil {
		t.Fatal("stale run lease exposed its setup receipt")
	}
	dvExec(t, db, `UPDATE lock SET run_id='setup-run' WHERE id=1`)
	dvExec(t, db, `UPDATE runs SET ended_at=datetime('now') WHERE id='setup-run'`)
	if _, err := ReadFirstTaskSetup(db, "setup-run"); err == nil {
		t.Fatal("ended run exposed its setup receipt")
	}
}

func TestReadFirstTaskSetupRefusesAnUnregisteredRun(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	if _, err := ReadFirstTaskSetup(db, "setup-run"); err == nil {
		t.Fatal("missing setup receipt was accepted")
	}
}

func TestAttestFirstTaskSetupRootDerivesAndMatchesOnlyTheRegisteredRoot(t *testing.T) {
	db := dvSpine(t)
	ws, root := tsBuild(t)
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	want := setupReceipt("setup-run", 7)
	want.Root.Device = strconv.FormatUint(uint64(st.Dev), 10)
	want.Root.Inode = strconv.FormatUint(st.Ino, 10)
	want.Root.OwnerUID = int(st.Uid)
	if _, err := RegisterFirstTaskSetup(db, want); err != nil {
		t.Fatalf("register receipt: %v", err)
	}

	got, err := AttestFirstTaskSetupRoot(db, "setup-run", ws)
	if err != nil {
		t.Fatalf("attest root: %v", err)
	}
	if got.Receipt != want || got.Canonical != root {
		t.Fatalf("attestation = %+v, want receipt %+v at %q", got, want, root)
	}

	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := AttestFirstTaskSetupRoot(db, "setup-run", ws); err == nil {
		t.Fatal("non-0555 registered root was accepted for setup")
	}
}

func TestInspectFirstTaskSetupRequiresTheReceiptAttestedCompleteTaskTable(t *testing.T) {
	db := dvSpine(t)
	ws, root := tsBuild(t)
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	receipt := setupReceipt("setup-run", 7)
	receipt.Root.Device = strconv.FormatUint(uint64(st.Dev), 10)
	receipt.Root.Inode = strconv.FormatUint(st.Ino, 10)
	receipt.Root.OwnerUID = int(st.Uid)
	if _, err := RegisterFirstTaskSetup(db, receipt); err != nil {
		t.Fatalf("register receipt: %v", err)
	}

	got, rows, err := InspectFirstTaskSetup(db, "setup-run", ws)
	if err != nil {
		t.Fatalf("inspect setup: %v", err)
	}
	if got.Receipt != receipt || got.Canonical != root {
		t.Fatalf("attested root = %+v, want receipt %+v at %q", got, receipt, root)
	}
	if len(rows) != len(taskPlanRows(7)) {
		t.Fatalf("inspected rows = %d, want %d", len(rows), len(taskPlanRows(7)))
	}

	if err := os.Remove(filepath.Join(root, "git", "shallow")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InspectFirstTaskSetup(db, "setup-run", ws); err == nil {
		t.Fatal("incomplete task table was accepted after receipt attestation")
	}
}

func TestInspectFirstTaskTableBindsTheWalkedRootToTheReceipt(t *testing.T) {
	ws, root := tsBuild(t)
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	fr := FirstTaskSetupRoot{
		Receipt: TaskSetupReceipt{RunID: "setup-run", TaskID: 7, Root: TaskSetupIdentity{
			Device:   strconv.FormatUint(uint64(st.Dev), 10),
			Inode:    strconv.FormatUint(st.Ino, 10),
			OwnerUID: int(st.Uid),
		}},
		Canonical: root,
	}
	if _, err := inspectFirstTaskTable(fr, ws); err != nil {
		t.Fatalf("the receipt-identical root failed inspection: %v", err)
	}

	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if rebuilt := tsBuildAt(t, ws); rebuilt != root {
		t.Fatalf("rebuilt skeleton moved: %q", rebuilt)
	}
	if _, err := inspectFirstTaskTable(fr, ws); err == nil {
		t.Fatal("a same-path swapped task root passed inspection against the stale receipt")
	}
}
