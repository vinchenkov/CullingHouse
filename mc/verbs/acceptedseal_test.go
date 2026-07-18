package verbs

import (
	"mc/dispatch"
	"strings"
	"testing"
)

func TestLoadAcceptedCompletionSealRequiresExactAcceptedManifestBoundTerminal(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, dispatch.ScopeTask, dispatch.StatusWorked, 2))
	dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject,ended_at,outcome) VALUES ('worker','pipeline','worker','ws-test',7,datetime('now'),'completed')`)
	sha, closure, manifest := strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64)
	dvExec(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,seal_device,seal_inode,seal_owner_uid,state,accepted_at) VALUES ('worker',7,'0011223344556677','sha1',?,?,?,?,?,501,'accepted',datetime('now'))`, sha, closure, manifest, "1", "2")
	got, err := LoadAcceptedCompletionSeal(db, "worker", "0011223344556677")
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != 7 || got.SealedSHA != sha || got.ManifestDigest != manifest || got.ClosureDigest != closure {
		t.Fatalf("loaded seal = %+v", got)
	}
}

func TestLoadAcceptedCompletionSealRejectsPublishedReceipt(t *testing.T) {
	db := dvSpine(t)
	dvInsertTask(t, db, dvTask(7, dispatch.ScopeTask, dispatch.StatusWorked, 2))
	dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject,ended_at,outcome) VALUES ('worker','pipeline','worker','ws-test',7,datetime('now'),'completed')`)
	dvExec(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,seal_device,seal_inode,seal_owner_uid) VALUES ('worker',7,'0011223344556677','sha1',?,?,?,'1','2',501)`, strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64))
	if _, err := LoadAcceptedCompletionSeal(db, "worker", "0011223344556677"); err == nil {
		t.Fatal("published seal was consumable")
	}
}
