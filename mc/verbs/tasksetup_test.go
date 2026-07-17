package verbs

import "testing"

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
