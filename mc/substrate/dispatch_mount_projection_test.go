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
