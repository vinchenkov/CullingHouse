package verbs

import (
	"context"
	"strings"
	"testing"

	"mc/dispatch"
)

// The SQL↔dispatch differential for the sealed landing lane. The pure
// predicate is only as true as its projection: if loadRecords does not read
// `task_assignments`, every sealed task projects as `Sealed == nil`, the
// sealed lane is empty forever, and an approved sealed task waits on a landing
// that dispatch can never see it is owed — the silent failure this whole slice
// exists to remove.
func TestDispatchLoadRecordsProjectsSealedAssignment(t *testing.T) {
	db := dvSpine(t)

	sha1hex := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	uuid := "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"

	// Task 1 is sealed: it carries an immutable closure assignment and is
	// branchless in `tasks`. Task 2 is an ordinary unassigned row.
	for _, id := range []int64{1, 2} {
		dvExec(t, db, `INSERT INTO tasks (id, title, scope, priority, created_at, status,
			dispatch_retries, origin, worksource, target_ref)
			VALUES (?, 'seal', 'task', 1, ?, 'proposed', 3, 'user', 'ws-test', 'main')`,
			id, dvOld.Format(spineTime))
	}
	dvExec(t, db, `INSERT INTO task_assignments
		(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
		VALUES (1, 'main', 'mc/task-1', '.mission-control/tasks/task-1', 'sha1', ?, ?, ?)`,
		sha1hex, uuid, digest)

	rec, err := loadRecords(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]dispatch.Task{}
	for _, task := range rec.Tasks {
		byID[task.ID] = task
	}

	// An unassigned row must project nil, not a zero-valued struct: nil is the
	// lane discriminator, and an empty non-nil assignment would put every
	// legacy task in the sealed lane.
	if byID[2].Sealed != nil {
		t.Fatalf("unassigned task projected an assignment: %+v", byID[2].Sealed)
	}

	got := byID[1].Sealed
	if got == nil {
		t.Fatal("sealed task projected no assignment; the sealed landing lane would be empty forever")
	}
	want := dispatch.SealedAssignment{
		Branch: "mc/task-1", TargetRef: "main",
		TaskRootKey: ".mission-control/tasks/task-1", ObjectFormat: "sha1",
		BaseSHA: sha1hex, LocalRepoUUID: uuid, ClosureDigest: digest,
	}
	if *got != want {
		t.Fatalf("projected assignment:\n  got  %+v\n  want %+v", *got, want)
	}

	// The sealed row is not yet landing-pending — it is `proposed`, undecided.
	// This pins that carrying an assignment is not by itself enough, so the
	// projection cannot smuggle a row into the lane.
	if byID[1].SealedLandingPending() {
		t.Fatal("a merely-assigned proposed task must not be owed a landing")
	}
}
