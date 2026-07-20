package verbs

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// asrReady makes a materialized task store, gives its original setup run a
// completed outcome, and arms the exact accepted Worker seal plus a live
// Verifier lease that may rebuild only that seal.
func asrReady(t *testing.T) (*sql.DB, string, SetupResult) {
	t.Helper()
	db := dvSpine(t)
	ws, _, result := tcMaterialized(t, db)
	if _, _, err := RecordFirstTaskSetupClosure(db, "setup-run", ws, result); err != nil {
		t.Fatalf("record initial task store: %v", err)
	}
	dvExec(t, db, `UPDATE runs SET ended_at=datetime('now'), outcome='setup-complete' WHERE id='setup-run'`)
	dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject,ended_at,outcome) VALUES ('worker','pipeline','worker','ws-test',7,datetime('now'),'completed')`)
	manifest := strings.Repeat("d", 64)
	dvExec(t, db, `INSERT INTO completion_seals (run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,seal_device,seal_inode,seal_owner_uid,state,accepted_at)
		VALUES ('worker',7,'0011223344556677',?,?,?,?,'1','2',501,'accepted',datetime('now'))`,
		result.ObjectFormat, result.BaseSHA, result.ClosureDigest, manifest)
	dvExec(t, db, `UPDATE tasks SET status='worked', accepted_completion_run_id='worker', accepted_completion_request_id='0011223344556677' WHERE id=7`)
	dvExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject) VALUES ('verify-run','pipeline','verifier','ws-test',7)`)
	dvExec(t, db, `UPDATE lock SET run_id='verify-run', subject=7, owner='verifier', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`)
	return db, ws, result
}

func TestRecordAcceptedSealRebuildBindsVerifierAcceptedSealAndTaskRoot(t *testing.T) {
	db, ws, result := asrReady(t)

	got, err := RecordAcceptedSealRebuild(db, "verify-run", ws, result)
	if err != nil {
		t.Fatalf("record accepted rebuild: %v", err)
	}
	if got.RunID != "verify-run" || got.TaskID != 7 || got.CompletionRunID != "worker" ||
		got.CompletionRequestID != "0011223344556677" || got.ManifestDigest != strings.Repeat("d", 64) ||
		got.BaseSHA != result.BaseSHA || got.ClosureDigest != result.ClosureDigest {
		t.Fatalf("receipt = %+v", got)
	}
	if count := dfInt(t, db, `SELECT COUNT(*) FROM accepted_seal_rebuild_receipts WHERE run_id='verify-run'`); count != 1 {
		t.Fatalf("receipt rows=%d, want 1", count)
	}

	replay, err := RecordAcceptedSealRebuild(db, "verify-run", ws, result)
	if err != nil || replay != got {
		t.Fatalf("exact record replay = (%+v, %v), want %+v", replay, err, got)
	}
	result.BaseSHA = strings.Repeat("e", len(result.BaseSHA))
	if _, err := RecordAcceptedSealRebuild(db, "verify-run", ws, result); err == nil {
		t.Fatal("changed lost-response result was accepted")
	}
}

func TestRecordAcceptedSealRebuildRefusesMismatchedSealOrLease(t *testing.T) {
	for name, mutate := range map[string]func(*sql.DB, *SetupResult){
		"result does not equal sealed SHA": func(_ *sql.DB, result *SetupResult) {
			result.BaseSHA = strings.Repeat("e", len(result.BaseSHA))
		},
		"lost verifier lease": func(db *sql.DB, _ *SetupResult) {
			dvExec(t, db, `UPDATE lock SET run_id='other-run' WHERE id=1`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			db, ws, result := asrReady(t)
			mutate(db, &result)
			if _, err := RecordAcceptedSealRebuild(db, "verify-run", ws, result); err == nil {
				t.Fatal("unfenced accepted-seal rebuild was recorded")
			}
			if count := dfInt(t, db, `SELECT COUNT(*) FROM accepted_seal_rebuild_receipts`); count != 0 {
				t.Fatalf("refused record persisted %d receipt rows", count)
			}
		})
	}
}

func TestRecordAcceptedSealRebuildReattestsTheDerivedTaskRoot(t *testing.T) {
	db, ws, result := asrReady(t)
	root := filepath.Join(ws, ".mission-control", "tasks", "task-7")
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o555) })
	if _, err := RecordAcceptedSealRebuild(db, "verify-run", ws, result); err == nil {
		t.Fatal("a mode-changed task root was accepted without re-attestation")
	}
	if count := dfInt(t, db, `SELECT COUNT(*) FROM accepted_seal_rebuild_receipts`); count != 0 {
		t.Fatalf("re-attestation refusal persisted %d receipt rows", count)
	}
}

func TestFenceVerifierProjectionTreeRequiresTheAcceptedSealedTrackedTree(t *testing.T) {
	db, ws, result := asrReady(t)
	// The projection is addressed by its fixed path. This test used to Chdir
	// into it, which hid the production defect: an agent container's CWD is
	// "/", so every git call failed as "not a repository" and the fence
	// reported that as drift — refusing a CLEAN projection exactly like a
	// dirty one, making the sealed verdict unreachable.
	projection := filepath.Join(ws, ".mission-control", "tasks", "task-7", "source")
	if err := fenceVerifierProjectionTree(db, 7, result.BaseSHA, projection); err != nil {
		t.Fatalf("clean accepted tree refused: %v", err)
	}
	// A path that is not the projection is named as such, never as drift.
	notARepo := t.TempDir()
	err := fenceVerifierProjectionTree(db, 7, result.BaseSHA, notARepo)
	if err == nil || !strings.Contains(err.Error(), "not a readable Git work tree") {
		t.Fatalf("non-repository projection path = %v, want the work-tree refusal", err)
	}
	if err := fenceVerifierProjectionTree(db, 7, result.BaseSHA, ""); err == nil {
		t.Fatal("an absent projection path reached the verifier terminal fence")
	}
	if err := os.WriteFile(filepath.Join(projection, "README.md"), []byte("projection drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fenceVerifierProjectionTree(db, 7, result.BaseSHA, projection); err == nil {
		t.Fatal("tracked projection drift reached the verifier terminal fence")
	}
}

func TestContinueAcceptedSealRebuildRequiresReceiptThenReleasesAndReplays(t *testing.T) {
	db, ws, result := asrReady(t)
	if _, err := ContinueAcceptedSealRebuild(db, "verify-run"); err == nil {
		t.Fatal("continuation accepted before its durable receipt")
	}
	if _, err := RecordAcceptedSealRebuild(db, "verify-run", ws, result); err != nil {
		t.Fatalf("record accepted rebuild: %v", err)
	}

	got, err := ContinueAcceptedSealRebuild(db, "verify-run")
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if got.RunID != "verify-run" || got.TaskID != 7 || got.AlreadyContinued {
		t.Fatalf("continuation = %+v", got)
	}
	if outcome := dfStr(t, db, `SELECT outcome FROM runs WHERE id='verify-run'`); outcome != "accepted-seal-rebuilt" {
		t.Fatalf("outcome=%q", outcome)
	}
	if holder := dfStr(t, db, `SELECT COALESCE(run_id, '<free>') FROM lock WHERE id=1`); holder != "<free>" {
		t.Fatalf("lease holder=%q, want free", holder)
	}
	replay, err := ContinueAcceptedSealRebuild(db, "verify-run")
	if err != nil || !replay.AlreadyContinued || replay.TaskID != 7 {
		t.Fatalf("lost-response continuation = (%+v, %v)", replay, err)
	}
}
