package substrate_test

import (
	"context"
	"reflect"
	"testing"

	"mc/substrate"
)

// LoadSubjectTaskSetupRoots projects the durable setup-receipt identities for
// one task, distinct and canonically sorted, with no live run/lease fence (it
// feeds the token-frozen dispatch projection, not the resident's consumer).
func TestLoadSubjectTaskSetupRootsDistinctSortedBounded(t *testing.T) {
	db := openSpine(t)
	taskID := mkTask(t, db, "task", "seeded")
	for _, run := range []string{"r1", "r2", "r3"} {
		mustExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject)
			VALUES (?, 'pipeline', 'worker', 'ws', ?)`, run, taskID)
	}
	// Two retry runs reused the same task root (the normal case); a third
	// recorded a lower inode. The projection dedupes and sorts.
	mustExec(t, db, `INSERT INTO task_setup_receipts (run_id, task_id, root_device, root_inode, root_owner_uid)
		VALUES ('r1', ?, '20', '300', 501)`, taskID)
	mustExec(t, db, `INSERT INTO task_setup_receipts (run_id, task_id, root_device, root_inode, root_owner_uid)
		VALUES ('r2', ?, '20', '300', 501)`, taskID)
	mustExec(t, db, `INSERT INTO task_setup_receipts (run_id, task_id, root_device, root_inode, root_owner_uid)
		VALUES ('r3', ?, '20', '299', 501)`, taskID)

	got, err := substrate.LoadSubjectTaskSetupRoots(context.Background(), db, taskID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []substrate.DispatchTaskSetupIdentity{
		{Device: "20", Inode: "299", OwnerUID: 501},
		{Device: "20", Inode: "300", OwnerUID: 501},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want distinct sorted %+v", got, want)
	}

	// A task with no setup receipt is an explicit empty, non-nil set — the
	// first-run precreate path, not an error.
	empty, err := substrate.LoadSubjectTaskSetupRoots(context.Background(), db, taskID+999)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("no-receipt task = (%+v, %v), want empty non-nil", empty, err)
	}
}

// LoadSubjectTaskAssignment projects the immutable first-task closure
// assignment for one task (ADR-016 D5), with no live run/lease fence: it is
// consumed at dispatch prepare, frozen into the token, and re-derived at
// commit. Presence flips the plan's setup instruction to retry mode carrying
// these exact pins; absence is the normal fresh-mode first run.
func TestLoadSubjectTaskAssignmentProjectsRetryPins(t *testing.T) {
	db := openSpine(t)
	taskID := mkTask(t, db, "task", "seeded")
	mustExec(t, db, `INSERT INTO task_assignments
		(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
		VALUES (?, 'main', 'mc/task-1', '.mission-control/tasks/task-1', 'sha1', ?, ?, ?)`,
		taskID,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	got, err := substrate.LoadSubjectTaskAssignment(context.Background(), db, taskID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := &substrate.DispatchTaskAssignment{
		BaseSHA:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ClosureDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
		ObjectFormat:  "sha1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// No assignment row means fresh mode: an explicit nil, not an error.
	absent, err := substrate.LoadSubjectTaskAssignment(context.Background(), db, taskID+999)
	if err != nil || absent != nil {
		t.Fatalf("no-assignment task = (%+v, %v), want nil", absent, err)
	}
}

func TestLoadSubjectAcceptedCompletionSealUsesTheTaskAcceptancePointer(t *testing.T) {
	db := openSpine(t)
	taskID := mkTask(t, db, "task", "worked")
	mustExec(t, db, `INSERT INTO runs (id,tier,role,worksource,subject,ended_at,outcome)
		VALUES ('worker','pipeline','worker','ws',?,datetime('now'),'completed')`, taskID)
	mustExec(t, db, `INSERT INTO completion_seals
		(run_id,task_id,completion_request_id,object_format,sealed_sha,closure_digest,manifest_digest,seal_device,seal_inode,seal_owner_uid,state,accepted_at)
		VALUES ('worker',?,'0011223344556677','sha1',?,?,?,?,?,501,'accepted',datetime('now'))`,
		taskID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "1", "2")
	mustExec(t, db, `UPDATE tasks SET accepted_completion_run_id='worker', accepted_completion_request_id='0011223344556677' WHERE id=?`, taskID)
	got, err := substrate.LoadSubjectAcceptedCompletionSeal(context.Background(), db, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.RunID != "worker" || got.CompletionRequest != "0011223344556677" || got.ManifestDigest != "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" {
		t.Fatalf("accepted projection=%+v", got)
	}
	if absent, err := substrate.LoadSubjectAcceptedCompletionSeal(context.Background(), db, taskID+999); err != nil || absent != nil {
		t.Fatalf("absent accepted projection=(%+v,%v), want nil,nil", absent, err)
	}
}

// LoadInitiativePriorChildRuns freezes the ADR-025 D6 producer-absence set: the
// pipeline-tier runs of every child task of an initiative, id-sorted, so the
// resident can confirm each prior child container is absent before the next one
// is prepared.
func TestLoadInitiativePriorChildRunsCollectsChildPipelineRuns(t *testing.T) {
	db := openSpine(t)
	initiative := mkTask(t, db, "initiative", "seeded")
	childA := mkChild(t, db, initiative)
	childB := mkChild(t, db, initiative)

	// Two pipeline runs under childA (worker then verifier) and one under childB.
	mustExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject)
		VALUES ('run-b1', 'pipeline', 'worker', 'ws', ?)`, childB)
	mustExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject)
		VALUES ('run-a2', 'pipeline', 'verifier', 'ws', ?)`, childA)
	mustExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject)
		VALUES ('run-a1', 'pipeline', 'worker', 'ws', ?)`, childA)

	got, err := substrate.LoadInitiativePriorChildRuns(context.Background(), db, initiative)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"run-a1", "run-a2", "run-b1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want id-sorted %+v", got, want)
	}
}

func TestLoadInitiativePriorChildRunsExcludesUnrelatedAndHomieRuns(t *testing.T) {
	db := openSpine(t)
	initiative := mkTask(t, db, "initiative", "seeded")
	child := mkChild(t, db, initiative)

	otherInit := mkTask(t, db, "initiative", "seeded")
	otherChild := mkChild(t, db, otherInit)
	standalone := mkTask(t, db, "task", "seeded")

	mustExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject)
		VALUES ('mine', 'pipeline', 'worker', 'ws', ?)`, child)
	// A run on a sibling initiative's child, on a standalone task, and a
	// subject-less homie run must all be excluded.
	mustExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject)
		VALUES ('other-init', 'pipeline', 'worker', 'ws', ?)`, otherChild)
	mustExec(t, db, `INSERT INTO runs (id, tier, role, worksource, subject)
		VALUES ('standalone', 'pipeline', 'worker', 'ws', ?)`, standalone)
	mustExec(t, db, `INSERT INTO runs (id, tier, binding)
		VALUES ('h-abc', 'homie', 'claude')`)

	got, err := substrate.LoadInitiativePriorChildRuns(context.Background(), db, initiative)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"mine"}) {
		t.Fatalf("got %+v, want only the initiative's own child pipeline run", got)
	}

	// An initiative with no child runs projects an empty, non-nil set.
	empty, err := substrate.LoadInitiativePriorChildRuns(context.Background(), db, otherInit+999)
	if err != nil || len(empty) != 0 {
		t.Fatalf("no-child initiative = (%+v, %v), want empty", empty, err)
	}
}
