package verbs

import (
	"database/sql"
	"strings"
	"testing"
)

func gitAssignmentFixture() FirstTaskAssignment {
	return FirstTaskAssignment{
		ObjectFormat:  "sha1",
		BaseSHA:       strings.Repeat("a", 40),
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
		ClosureDigest: strings.Repeat("b", 64),
	}
}

// seedLiveStandaloneWorker installs task 7 and a live standalone Worker run
// ('setup-run') holding the singleton lease for it, the exact post-claim state
// in which a first-task setup records its receipt and assignment.
func seedLiveStandaloneWorker(t *testing.T, db *sql.DB) {
	t.Helper()
	dvInsertTask(t, db, dvTask(7, "task", "seeded", 2))
	dvExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject) VALUES ('setup-run', 'pipeline', 'worker', 'ws-test', 7)`)
	dvExec(t, db, `UPDATE lock SET run_id='setup-run', subject=7, owner='worker', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
}

func TestRegisterFirstTaskAssignmentIsFencedIdempotentAndRejectsDivergence(t *testing.T) {
	db := dvSpine(t)
	seedLiveStandaloneWorker(t, db)

	git := gitAssignmentFixture()
	want := TaskAssignment{
		TaskID: 7, TargetRef: "main", Branch: "mc/task-7",
		TaskRootKey:  ".mission-control/tasks/task-7",
		ObjectFormat: git.ObjectFormat, BaseSHA: git.BaseSHA,
		LocalRepoUUID: git.LocalRepoUUID, ClosureDigest: git.ClosureDigest,
	}
	if got, err := RegisterFirstTaskAssignment(db, "setup-run", git); err != nil || got != want {
		t.Fatalf("first registration = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	// Exact retry is an idempotent lost-response replay.
	if got, err := RegisterFirstTaskAssignment(db, "setup-run", git); err != nil || got != want {
		t.Fatalf("exact retry = (%+v, %v), want idempotent (%+v, nil)", got, err, want)
	}
	// A retry that resolved a moved target refuses rather than rebasing (D5).
	moved := git
	moved.BaseSHA = strings.Repeat("c", 40)
	if _, err := RegisterFirstTaskAssignment(db, "setup-run", moved); err == nil {
		t.Fatal("a first-task retry rebased to a moved base SHA")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_assignments WHERE task_id=7`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("assignment count = (%d, %v), want (1, nil)", count, err)
	}
}

func TestRegisterFirstTaskAssignmentRequiresTheLiveLease(t *testing.T) {
	db := dvSpine(t)
	seedLiveStandaloneWorker(t, db)
	dvExec(t, db, `UPDATE lock SET run_id='other-run' WHERE id=1`)
	if _, err := RegisterFirstTaskAssignment(db, "setup-run", gitAssignmentFixture()); err == nil {
		t.Fatal("a superseded Worker recorded a closure assignment")
	}
	dvExec(t, db, `UPDATE lock SET run_id='setup-run' WHERE id=1`)
	dvExec(t, db, `UPDATE runs SET ended_at=datetime('now') WHERE id='setup-run'`)
	if _, err := RegisterFirstTaskAssignment(db, "setup-run", gitAssignmentFixture()); err == nil {
		t.Fatal("an ended Worker recorded a closure assignment")
	}
}

func TestRegisterFirstTaskAssignmentValidatesGitFields(t *testing.T) {
	db := dvSpine(t)
	seedLiveStandaloneWorker(t, db)
	bad := gitAssignmentFixture()
	bad.ObjectFormat = "sha256" // base_sha is only 40 hex, the sha1 length
	if _, err := RegisterFirstTaskAssignment(db, "setup-run", bad); err == nil {
		t.Fatal("a base SHA whose length contradicts the object format was accepted")
	}
}

func TestReadFirstTaskAssignmentRequiresTheLiveLeaseAndExactRow(t *testing.T) {
	db := dvSpine(t)
	seedLiveStandaloneWorker(t, db)
	git := gitAssignmentFixture()
	if _, err := RegisterFirstTaskAssignment(db, "setup-run", git); err != nil {
		t.Fatalf("register assignment: %v", err)
	}
	if got, err := ReadFirstTaskAssignment(db, "setup-run"); err != nil || got.BaseSHA != git.BaseSHA || got.Branch != "mc/task-7" {
		t.Fatalf("read = (%+v, %v)", got, err)
	}
	dvExec(t, db, `UPDATE runs SET ended_at=datetime('now') WHERE id='setup-run'`)
	if _, err := ReadFirstTaskAssignment(db, "setup-run"); err == nil {
		t.Fatal("an ended run exposed its closure assignment")
	}
}

func TestReadFirstTaskAssignmentRefusesAnUnrecordedRun(t *testing.T) {
	db := dvSpine(t)
	seedLiveStandaloneWorker(t, db)
	if _, err := ReadFirstTaskAssignment(db, "setup-run"); err == nil {
		t.Fatal("a run with no recorded assignment returned one")
	}
}
