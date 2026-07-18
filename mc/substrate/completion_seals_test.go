package substrate_test

import (
	"strings"
	"testing"
)

func TestCompletionSealsAreTypedImmutableAndStateFenced(t *testing.T) {
	db := openSpine(t)
	mustExec(t, db, `INSERT INTO tasks (id,title,scope,worksource) VALUES (7,'fixture','task','ws')`)
	mustExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('run-7','pipeline','worker','ws',7)`)
	sha := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	mustExec(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,seal_device,seal_inode,seal_owner_uid) VALUES ('run-7',7,'0011223344556677','sha1',? ,?,'1','2',501)`, sha, digest)
	wantAbort(t, db, `UPDATE completion_seals SET sealed_sha=? WHERE run_id='run-7'`, strings.Repeat("c", 40))
	wantAbort(t, db, `UPDATE completion_seals SET state='removed', removed_at=datetime('now') WHERE run_id='run-7'`)
	mustExec(t, db, `UPDATE completion_seals SET state='accepted', accepted_at=datetime('now') WHERE run_id='run-7'`)
	wantAbort(t, db, `DELETE FROM completion_seals WHERE run_id='run-7'`)
	mustExec(t, db, `INSERT INTO tasks (id,title,scope,worksource) VALUES (8,'fixture','task','ws')`)
	mustExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('run-8','pipeline','worker','ws',8)`)
	wantAbort(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,seal_device,seal_inode,seal_owner_uid) VALUES ('run-8',8,'0011223344556678','sha1',x'6161',?,'1','2',501)`, digest)
}

func TestMigrateAddsCompletionSealsMatchingFresh(t *testing.T) {
	migrated, fresh := migratedV1Spine(t), openSpine(t)
	if got, want := columnsOf(t, migrated, "completion_seals"), columnsOf(t, fresh, "completion_seals"); got != want {
		t.Errorf("completion_seals columns after migration:\n got %s\nwant %s", got, want)
	}
	if !triggerExists(t, migrated, "completion_seals_transition_only") || !triggerExists(t, migrated, "completion_seals_no_delete") {
		t.Fatal("migrated spine is missing completion-seal durable fences")
	}
}
