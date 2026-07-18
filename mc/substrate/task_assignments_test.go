package substrate_test

import (
	"strings"
	"testing"
)

// A first-task closure assignment is durable retry evidence: once recorded it
// is immutable and undeletable (a retry reuses it, never rebases a moved
// target — ADR-016 D5), its object_format is a closed set, its base_sha length
// must agree with that format, and its hex identity columns are typeof-fenced
// so a BLOB forgery cannot bypass the GLOB checks (the D2 hazard).
func TestTaskAssignmentsAreImmutableTypedAndClosed(t *testing.T) {
	db := openSpine(t)
	mustExec(t, db, `INSERT INTO tasks (id, title, scope, worksource) VALUES (7, 'fixture', 'task', 'ws')`)
	mustExec(t, db, `INSERT INTO tasks (id, title, scope, worksource) VALUES (8, 'fixture', 'task', 'ws')`)

	sha1hex := strings.Repeat("a", 40)
	sha256hex := strings.Repeat("a", 64)
	digest := strings.Repeat("b", 64)
	uuid := "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"

	// A well-formed sha1 assignment commits.
	mustExec(t, db, `INSERT INTO task_assignments
		(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
		VALUES (7, 'main', 'mc/task-7', '.mission-control/tasks/task-7', 'sha1', ?, ?, ?)`,
		sha1hex, uuid, digest)

	// Immutable and undeletable.
	wantAbort(t, db, `UPDATE task_assignments SET base_sha=? WHERE task_id=7`, strings.Repeat("c", 40))
	wantAbort(t, db, `DELETE FROM task_assignments WHERE task_id=7`)

	// object_format is a closed set.
	wantAbort(t, db, `INSERT INTO task_assignments
		(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
		VALUES (8, 'main', 'mc/task-8', 'k', 'sha384', ?, ?, ?)`, sha1hex, uuid, digest)

	// base_sha length must match the declared object_format (sha256 needs 64).
	wantAbort(t, db, `INSERT INTO task_assignments
		(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
		VALUES (8, 'main', 'mc/task-8', 'k', 'sha256', ?, ?, ?)`, sha1hex, uuid, digest)
	mustExec(t, db, `INSERT INTO task_assignments
		(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
		VALUES (8, 'main', 'mc/task-8', 'k', 'sha256', ?, ?, ?)`, sha256hex, uuid, digest)

	// A BLOB base_sha bypasses affinity conversion; the typeof fence refuses it.
	mustExec(t, db, `INSERT INTO tasks (id, title, scope, worksource) VALUES (9, 'fixture', 'task', 'ws')`)
	wantAbort(t, db, `INSERT INTO task_assignments
		(task_id, target_ref, branch, task_root_key, object_format, base_sha, local_repo_uuid, closure_digest)
		VALUES (9, 'main', 'mc/task-9', 'k', 'sha1',
		        x'61616161616161616161616161616161616161616161616161616161616161616161616161616161', ?, ?)`, uuid, digest)
}

// A migrated spine must gain the task_assignments table with exactly the same
// columns and durable-evidence triggers a fresh spine ships — otherwise a
// deployment upgraded in place could record a rebaseable or mutable assignment
// the fresh schema refuses.
func TestMigrateAddsTaskAssignmentsMatchingFresh(t *testing.T) {
	migrated := migratedV1Spine(t)
	fresh := openSpine(t)
	if got, want := columnsOf(t, migrated, "task_assignments"), columnsOf(t, fresh, "task_assignments"); got != want {
		t.Errorf("task_assignments columns after migration:\n  got  %s\n  want %s", got, want)
	}
	if got, want := triggersOf(t, migrated, "task_assignments"), triggersOf(t, fresh, "task_assignments"); got != want {
		t.Errorf("task_assignments triggers after migration:\n  got  %s\n  want %s", got, want)
	}
	if !triggerExists(t, migrated, "task_assignments_immutable") || !triggerExists(t, migrated, "task_assignments_no_delete") {
		t.Fatal("migrated spine is missing the task_assignments durable-evidence fences")
	}
}
