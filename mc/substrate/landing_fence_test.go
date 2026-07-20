package substrate_test

import (
	"database/sql"
	"strings"
	"testing"
)

// The §7 landing fence exists so that no task can be approved for a merge
// without the two facts the merge needs: the exact verified commit, and the ref
// to merge into. Its original form keys on `tasks.branch`, which was the only
// branch home when it was written.
//
// The seal pipeline introduced a second one. A sealed standalone task is
// branchless in `tasks` by construction — `tasks.branch`'s only writer is the
// `--status worked --branch` terminal that ADR-016 D6 closes to assigned tasks
// — and carries its branch in `task_assignments.branch` instead. So the fence
// as written skips exactly the rows the sealed landing path will consume, and
// the substrate would accept an approved sealed task with no verified SHA at
// all. `domain.Approve` refuses it in Go, but the substrate is the layer that
// still has to hold when a future writer reaches the column another way.
//
// Fence on the ASSIGNMENT as well as the branch: either branch home arms it.
func TestApproveLandingFenceCoversSealedAssignments(t *testing.T) {
	// One spine per case: Inv. 18 caps a spine at three unarchived packets,
	// and every case here needs a live one.
	sealed := func(t *testing.T) *sql.DB {
		t.Helper()
		db := openSpine(t)
		mustExec(t, db, `INSERT INTO tasks (id, title, scope, worksource) VALUES (7, 'fixture', 'task', 'ws')`)
		packageTask(t, db, 7)
		mustExec(t, db, `INSERT INTO review_packets (task_id) VALUES (7)`)
		mustExec(t, db, `INSERT INTO task_assignments
			(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
			VALUES (7, 'main', 'mc/task-7', '.mission-control/tasks/task-7', 'sha1', ?, ?, ?)`,
			strings.Repeat("a", 40), "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9", strings.Repeat("b", 64))
		return db
	}
	sha1hex := strings.Repeat("a", 40)

	t.Run("neither_landing_fact", func(t *testing.T) {
		db := sealed(t)
		wantFenceAbort(t, db)
	})

	// verified_sha alone is not enough — the merge still has no target.
	t.Run("verified_sha_only", func(t *testing.T) {
		db := sealed(t)
		mustExec(t, db, `UPDATE tasks SET verified_sha=? WHERE id=7`, sha1hex)
		wantFenceAbort(t, db)
	})

	// target_ref alone is not enough — the merge still has no commit.
	t.Run("target_ref_only", func(t *testing.T) {
		db := sealed(t)
		mustExec(t, db, `UPDATE tasks SET target_ref='main' WHERE id=7`)
		wantFenceAbort(t, db)
	})

	t.Run("both_present_commits", func(t *testing.T) {
		db := sealed(t)
		mustExec(t, db, `UPDATE tasks SET verified_sha=?, target_ref='main' WHERE id=7`, sha1hex)
		mustExec(t, db, `UPDATE tasks SET decision='approved', decided_at=datetime('now') WHERE id=7`)
	})

	// The fence must not over-reach onto the legacy artifact plane. A branchless
	// task with NO assignment is a deliverable that never merges, so it has no
	// landing facts to require and approve archives it synchronously
	// (domain.Approve). Arming the fence for it would break every Phase-2 row.
	t.Run("unassigned_artifact_plane_row_still_approves", func(t *testing.T) {
		db := openSpine(t)
		mustExec(t, db, `INSERT INTO tasks (id, title, scope, worksource) VALUES (7, 'fixture', 'task', 'ws')`)
		packageTask(t, db, 7)
		mustExec(t, db, `INSERT INTO review_packets (task_id) VALUES (7)`)
		mustExec(t, db, `UPDATE tasks SET decision='approved', decided_at=datetime('now') WHERE id=7`)
	})
}

// A spine migrated in place must carry the widened fence, not the branch-only
// one it was created with: an upgraded deployment is exactly where a sealed
// task and an old trigger meet.
func TestMigrateWidensApproveLandingFence(t *testing.T) {
	migrated := migratedV1Spine(t)
	fresh := openSpine(t)
	if got, want := triggersOf(t, migrated, "tasks"), triggersOf(t, fresh, "tasks"); got != want {
		t.Errorf("tasks triggers after migration:\n  got  %s\n  want %s", got, want)
	}
	if !triggerExists(t, migrated, "tasks_approve_requires_landing_fence") {
		t.Fatal("migrated spine is missing the approve landing fence")
	}
}

// wantFenceAbort approves task 7 and asserts it aborts for the LANDING FENCE
// specifically. Asserting only "aborts" would pass vacuously: the paired
// decision/decided_at CHECK and the live-packet trigger both abort this same
// statement, and an unwidened fence would still look green behind either.
func wantFenceAbort(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`UPDATE tasks SET decision='approved', decided_at=datetime('now') WHERE id=7`)
	if err == nil {
		t.Fatal("want the landing fence to abort the approve, got commit")
	}
	if !strings.Contains(err.Error(), "landing fence") {
		t.Fatalf("aborted for the wrong reason (want the §7 landing fence): %v", err)
	}
}

// packageTask walks the §6 lattice from birth to packaged; there is no direct
// edge, and every hop here is one of the six legal ones.
func packageTask(t *testing.T, db *sql.DB, id int) {
	t.Helper()
	for _, s := range []string{"seeded", "worked", "verified", "packaged"} {
		mustExec(t, db, `UPDATE tasks SET status=? WHERE id=?`, s, id)
	}
}
