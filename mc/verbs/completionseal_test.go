package verbs

import (
	"mc/dispatch"
	"strings"
	"testing"
)

func TestAcceptCompletionSealRequiresLiveMatchingPublishedReceipt(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, dispatch.ScopeTask, dispatch.StatusSeeded, 2))
	dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('worker','pipeline','worker','ws-test',7)`)
	dvExec(t, db, `UPDATE lock SET run_id='worker',subject=7,owner='worker',acquired_at=datetime('now'),hard_deadline_at=datetime('now','+1 hour') WHERE id=1`)
	dvExec(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,seal_device,seal_inode,seal_owner_uid) VALUES ('worker',7,'0011223344556677','sha1',?,?, '1','2',501)`, strings.Repeat("a", 40), strings.Repeat("b", 64))
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
	dvExec(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,seal_device,seal_inode,seal_owner_uid) VALUES ('worker',7,'0011223344556677','sha1',?,?, '1','2',501)`, strings.Repeat("a", 40), strings.Repeat("b", 64))

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
