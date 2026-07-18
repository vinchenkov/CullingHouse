package substrate_test

import (
	"strings"
	"testing"
)

func TestAcceptedSealRebuildReceiptIsExactLiveVerifierEvidence(t *testing.T) {
	db := openSpine(t)
	const (
		request = "0011223344556677"
		uuid    = "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	)
	sha := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	manifest := strings.Repeat("c", 64)
	mustExec(t, db, `INSERT INTO tasks (id,title,scope,worksource) VALUES (7,'fixture','task','ws')`)
	mustExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject,ended_at,outcome) VALUES ('worker','pipeline','worker','ws',7,datetime('now'),'completed')`)
	mustExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('setup-worker','pipeline','worker','ws',7)`)
	mustExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('verifier','pipeline','verifier','ws',7)`)
	mustExec(t, db, `INSERT INTO task_setup_receipts (run_id,task_id,root_device,root_inode,root_owner_uid) VALUES ('setup-worker',7,'1','2',501)`)
	mustExec(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,seal_device,seal_inode,seal_owner_uid,state,accepted_at)
		VALUES ('worker',7,?,'sha1',?,?,?,'9','10',501,'accepted',datetime('now'))`, request, sha, digest, manifest)
	mustExec(t, db, `UPDATE tasks SET status='seeded' WHERE id=7`)
	mustExec(t, db, `UPDATE tasks SET status='worked',accepted_completion_run_id='worker',accepted_completion_request_id=? WHERE id=7`, request)
	mustExec(t, db, `UPDATE lock SET run_id='verifier',owner='verifier',subject=7,
		acquired_at=datetime('now'),last_heartbeat_at=datetime('now'),hard_deadline_at=datetime('now','+1 hour') WHERE id=1`)
	insert := `INSERT INTO accepted_seal_rebuild_receipts
		(run_id,task_id,completion_run_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,root_device,root_inode,root_owner_uid,local_repo_uuid,object_count,fsck_clean)
		VALUES ('verifier',7,'worker',?,'sha1',?,?,?,'1','2',501,?,3,1)`
	mustExec(t, db, insert, request, sha, digest, manifest, uuid)
	wantAbort(t, db, `UPDATE accepted_seal_rebuild_receipts SET object_count=4 WHERE run_id='verifier'`)
	wantAbort(t, db, `DELETE FROM accepted_seal_rebuild_receipts WHERE run_id='verifier'`)

	// Correctly shaped but different accepted receipt material cannot be
	// attached to the same task under a live verifier lease.
	mustExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('verifier-2','pipeline','verifier','ws',7)`)
	mustExec(t, db, `UPDATE lock SET run_id='verifier-2',owner='verifier',subject=7 WHERE id=1`)
	wantAbort(t, db, `INSERT INTO accepted_seal_rebuild_receipts
		(run_id,task_id,completion_run_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,root_device,root_inode,root_owner_uid,local_repo_uuid,object_count,fsck_clean)
		VALUES ('verifier-2',7,'worker','deadbeefdeadbeef','sha1',?,?,?,'1','2',501,?,3,1)`, sha, digest, manifest, uuid)
}

func TestMigrateAddsAcceptedSealRebuildReceiptsMatchingFresh(t *testing.T) {
	migrated, fresh := migratedV1Spine(t), openSpine(t)
	if got, want := columnsOf(t, migrated, "accepted_seal_rebuild_receipts"), columnsOf(t, fresh, "accepted_seal_rebuild_receipts"); got != want {
		t.Errorf("accepted_seal_rebuild_receipts columns after migration:\n got %s\nwant %s", got, want)
	}
	if got, want := triggersOf(t, migrated, "accepted_seal_rebuild_receipts"), triggersOf(t, fresh, "accepted_seal_rebuild_receipts"); got != want {
		t.Errorf("accepted_seal_rebuild_receipts triggers after migration:\n got %s\nwant %s", got, want)
	}
}
