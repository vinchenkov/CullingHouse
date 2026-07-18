package verbs

import (
	"database/sql"
	"mc/dispatch"
	"strings"
	"testing"
)

func TestPublishCompletionSealFencesAndMakesExactReplayInert(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, dispatch.ScopeTask, dispatch.StatusSeeded, 2))
	dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('worker','pipeline','worker','ws-test',7)`)
	dvExec(t, db, `UPDATE lock SET run_id='worker',subject=7,owner='worker',acquired_at=datetime('now'),hard_deadline_at=datetime('now','+1 hour') WHERE id=1`)
	p := CompletionSealPublication{RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: "sha1", SealedSHA: strings.Repeat("a", 40), ClosureDigest: strings.Repeat("b", 64), ManifestDigest: strings.Repeat("c", 64), Device: "1", Inode: "2", OwnerUID: 501}
	if err := PublishCompletionSeal(db, p); err != nil {
		t.Fatal(err)
	}
	if got := dfStr(t, db, `SELECT state FROM completion_seals WHERE run_id='worker'`); got != "published" {
		t.Fatalf("state=%q, want published", got)
	}
	if err := PublishCompletionSeal(db, p); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if err := AcceptCompletionSeal(db, p.RunID, p.CompletionRequest); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := PublishCompletionSeal(db, p); err != nil {
		t.Fatalf("post-terminal exact replay: %v", err)
	}
	p.ManifestDigest = strings.Repeat("d", 64)
	if err := PublishCompletionSeal(db, p); err == nil {
		t.Fatal("conflicting replay rewrote an immutable receipt")
	}
	if got := dfInt(t, db, `SELECT COUNT(*) FROM completion_seals WHERE run_id='worker'`); got != 1 {
		t.Fatalf("publication count=%d, want one", got)
	}
	if got := dfStr(t, db, `SELECT status FROM tasks WHERE id=7`); got != "worked" {
		t.Fatalf("publication replay changed the accepted task to %q", got)
	}
}

func TestPublishCompletionSealRefusesWrongProducerOrLease(t *testing.T) {
	base := func(t *testing.T) (*sql.DB, CompletionSealPublication) {
		t.Helper()
		db := dvSpine(t)
		dvInsertTask(t, db, dvTask(7, dispatch.ScopeTask, dispatch.StatusSeeded, 2))
		dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('worker','pipeline','worker','ws-test',7)`)
		dvExec(t, db, `UPDATE lock SET run_id='worker',subject=7,owner='worker',acquired_at=datetime('now'),hard_deadline_at=datetime('now','+1 hour') WHERE id=1`)
		return db, CompletionSealPublication{RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: "sha1", SealedSHA: strings.Repeat("a", 40), ClosureDigest: strings.Repeat("b", 64), ManifestDigest: strings.Repeat("c", 64), Device: "1", Inode: "2", OwnerUID: 501}
	}
	for name, mutate := range map[string]func(*sql.DB){
		"wrong-role":  func(db *sql.DB) { dvExec(t, db, `UPDATE runs SET role='verifier' WHERE id='worker'`) },
		"lost-lease":  func(db *sql.DB) { dvExec(t, db, `UPDATE lock SET run_id='other',subject=7 WHERE id=1`) },
		"wrong-stage": func(db *sql.DB) { dvExec(t, db, `UPDATE tasks SET status='worked' WHERE id=7`) },
	} {
		t.Run(name, func(t *testing.T) {
			db, p := base(t)
			mutate(db)
			if err := PublishCompletionSeal(db, p); err == nil {
				t.Fatal("invalid publication was accepted")
			}
			if got := dfInt(t, db, `SELECT COUNT(*) FROM completion_seals`); got != 0 {
				t.Fatalf("invalid publication wrote %d rows", got)
			}
		})
	}
}

func TestAcceptCompletionSealRequiresLiveMatchingPublishedReceipt(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, dispatch.ScopeTask, dispatch.StatusSeeded, 2))
	dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('worker','pipeline','worker','ws-test',7)`)
	dvExec(t, db, `UPDATE lock SET run_id='worker',subject=7,owner='worker',acquired_at=datetime('now'),hard_deadline_at=datetime('now','+1 hour') WHERE id=1`)
	dvExec(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,seal_device,seal_inode,seal_owner_uid) VALUES ('worker',7,'0011223344556677','sha1',?,?,?,'1','2',501)`, strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64))
	if err := AcceptCompletionSeal(db, "worker", "0011223344556677"); err != nil {
		t.Fatal(err)
	}
	if got := dfStr(t, db, `SELECT state FROM completion_seals WHERE run_id='worker'`); got != "accepted" {
		t.Fatalf("state=%q", got)
	}
	if got := dfStr(t, db, `SELECT status FROM tasks WHERE id=7`); got != "worked" {
		t.Fatalf("task status=%q, want worked", got)
	}
	if got := dfStr(t, db, `SELECT outcome FROM runs WHERE id='worker'`); got != "completed" {
		t.Fatalf("run outcome=%q, want completed", got)
	}
	if got := dfStr(t, db, `SELECT COALESCE(run_id, '<free>') FROM lock WHERE id=1`); got != "<free>" {
		t.Fatalf("accepted completion left lease=%q", got)
	}

	// A lost response observes the same durable terminal, rather than trying to
	// fence a lease that was correctly released by the accepted completion.
	if err := AcceptCompletionSeal(db, "worker", "0011223344556677"); err != nil {
		t.Fatalf("accepted replay: %v", err)
	}
	if got := dfInt(t, db, `SELECT COUNT(*) FROM completion_seals WHERE run_id='worker'`); got != 1 {
		t.Fatalf("replay changed seal history count to %d", got)
	}
}

func TestAcceptCompletionSealRefusesWrongProducerOrRequestWithoutTerminalMutation(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, dispatch.ScopeTask, dispatch.StatusSeeded, 2))
	dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('worker','pipeline','worker','ws-test',7)`)
	dvExec(t, db, `UPDATE lock SET run_id='worker',subject=7,owner='worker',acquired_at=datetime('now'),hard_deadline_at=datetime('now','+1 hour') WHERE id=1`)
	dvExec(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,seal_device,seal_inode,seal_owner_uid) VALUES ('worker',7,'0011223344556677','sha1',?,?,?,'1','2',501)`, strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64))

	if err := AcceptCompletionSeal(db, "worker", "0011223344556678"); err == nil {
		t.Fatal("different request accepted")
	}
	if got := dfStr(t, db, `SELECT status FROM tasks WHERE id=7`); got != "seeded" {
		t.Fatalf("wrong request advanced task to %q", got)
	}
	if got := dfStr(t, db, `SELECT COALESCE(run_id, '<free>') FROM lock WHERE id=1`); got != "worker" {
		t.Fatalf("wrong request changed lease to %q", got)
	}

	dvExec(t, db, `UPDATE runs SET role='verifier' WHERE id='worker'`)
	if err := AcceptCompletionSeal(db, "worker", "0011223344556677"); err == nil {
		t.Fatal("non-Worker producer accepted")
	}
	if got := dfStr(t, db, `SELECT state FROM completion_seals WHERE run_id='worker'`); got != "published" {
		t.Fatalf("wrong producer changed seal state to %q", got)
	}
}

func TestCompleteSealedWorkerBindsCallerAndRejectsTheUnsealedAssignedBypass(t *testing.T) {
	publication := func() CompletionSealPublication {
		return CompletionSealPublication{RunID: "worker", TaskID: 7, CompletionRequest: "0011223344556677", ObjectFormat: "sha1", SealedSHA: strings.Repeat("a", 40), ClosureDigest: strings.Repeat("b", 64), ManifestDigest: strings.Repeat("c", 64), Device: "1", Inode: "2", OwnerUID: 501}
	}
	seed := func(t *testing.T) *sql.DB {
		t.Helper()
		db := dvSpine(t)
		dvInsertTask(t, db, dvTask(7, dispatch.ScopeTask, dispatch.StatusSeeded, 2))
		dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('worker','pipeline','worker','ws-test',7)`)
		dvExec(t, db, `UPDATE lock SET run_id='worker',subject=7,owner='worker',acquired_at=datetime('now'),hard_deadline_at=datetime('now','+1 hour') WHERE id=1`)
		return db
	}

	t.Run("sealed terminal", func(t *testing.T) {
		db := seed(t)
		got, err := CompleteSealedWorker(db, &RunIdentity{RunID: "worker", Tier: "pipeline", Role: "worker"}, publication())
		if err != nil {
			t.Fatal(err)
		}
		if got["status"] != "worked" || got["completion_request_id"] != "0011223344556677" {
			t.Fatalf("completion result=%v", got)
		}
		if state := dfStr(t, db, `SELECT state FROM completion_seals WHERE run_id='worker'`); state != "accepted" {
			t.Fatalf("seal state=%q, want accepted", state)
		}
	})

	t.Run("ordinary terminal cannot bypass an assigned task seal", func(t *testing.T) {
		db := seed(t)
		dvExec(t, db, `INSERT INTO task_assignments (task_id,target_ref,branch,task_root_key,object_format,base_sha,local_repo_uuid,closure_digest)
			VALUES (7,'main','mc/task-7','.mission-control/tasks/task-7','sha1',?,?,?)`, strings.Repeat("a", 40), "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9", strings.Repeat("b", 64))
		_, err := Complete(db, &RunIdentity{RunID: "worker", Tier: "pipeline", Role: "worker"}, CompleteArgs{Task: 7, Run: "worker", Status: "worked"})
		if err == nil || !strings.Contains(err.Error(), "sealed completion receipt") {
			t.Fatalf("unsealed assigned completion error=%v", err)
		}
		if status := dfStr(t, db, `SELECT status FROM tasks WHERE id=7`); status != "seeded" {
			t.Fatalf("bypass advanced task to %q", status)
		}
		if holder := dfStr(t, db, `SELECT run_id FROM lock WHERE id=1`); holder != "worker" {
			t.Fatalf("bypass released lease to %q", holder)
		}
	})
}
