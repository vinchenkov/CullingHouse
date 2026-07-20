package verbs

import (
	"os"
	"path/filepath"
	"testing"

	"mc/boundary"
	"mc/refusal"
)

// The landing table is ADR-017:699-702. These tests pin the GRAMMAR only —
// no producer resolves a landing kind to an authorized typed root yet, so
// every landing request must still DENY rather than plan. That inertness is
// asserted here too, so turning the lane on cannot happen by accident.

func TestLandingPlanRowsAreTheClosedADR017Table(t *testing.T) {
	rows := landingPlanRows()
	if len(rows) != 4 {
		t.Fatalf("the landing table has exactly 4 rows (ADR-017:699-702), got %d", len(rows))
	}
	want := []landingPlanRow{
		{Kind: boundary.KindLandingWorksource, Dest: "/repo/source", Access: boundary.AccessRW, IsDir: true},
		{Kind: boundary.KindLandingMissionControlCover, Dest: "/repo/source/.mission-control", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true},
		{Kind: boundary.KindLandingTaskRoot, Dest: "/repo/task", Access: boundary.AccessRO, IsDir: true},
		{Kind: boundary.KindLandingEnvelope, Dest: "/mc/landing.json", Access: boundary.AccessRO, ResidentMaterialized: true},
	}
	for i, w := range want {
		if rows[i] != w {
			t.Fatalf("landing row %d = %+v, want %+v", i, rows[i], w)
		}
	}
}

// The whole reason landing is a separate class: setup's `/repo/source` is RO
// (ADR-017:691), landing's is RW (:699) — "the only grant in the system that
// gets a real Worksource repository RW". Every other landing row is RO.
func TestLandingGrantsExactlyOneWritableRow(t *testing.T) {
	var writable []string
	for _, row := range landingPlanRows() {
		if row.Access == boundary.AccessRW {
			writable = append(writable, row.Dest)
		}
	}
	if len(writable) != 1 || writable[0] != "/repo/source" {
		t.Fatalf("landing writes exactly the real Worksource root, got %v", writable)
	}
}

func TestValidLandingPlanDestinationIsClosed(t *testing.T) {
	for _, dest := range []string{"/repo/source", "/repo/source/.mission-control", "/repo/task"} {
		if !validLandingPlanDestination(dest) {
			t.Fatalf("%q is a bindable landing cell", dest)
		}
	}
	// The envelope is resident-materialized, exactly like /mc/setup.json: the
	// plan carries the instruction, never the bind. Admitting it here would
	// make /mc plan-addressable for the first time.
	for _, dest := range []string{
		"/mc/landing.json", "/mc/setup.json", "/mc/session", "/mc",
		"/repo", "/repo/", "/repo/source/", "/repo/source/.git",
		"/repo/task/source", "/repo/task/git", "/repo/seal", "/repo/projection",
		"/repo/source/.mission-control/tasks", "/reposource", "",
	} {
		if validLandingPlanDestination(dest) {
			t.Fatalf("%q is outside the bindable landing table", dest)
		}
	}
}

// Step 5 of the sealed-landing lane turns two lanes on that must partition by
// construction. The destination grammars are the first place that can rot:
// dispatchprivate.go keys the task-precreate fabrication guard off
// validTaskPlanDestination alone, so a landing cell leaking into the task
// grammar would silently widen it.
func TestLandingAndTaskDestinationGrammarsArePartitioned(t *testing.T) {
	for _, row := range landingPlanRows() {
		if validTaskPlanDestination(row.Dest) {
			t.Fatalf("landing cell %q must not be a task-table cell", row.Dest)
		}
	}
	for _, row := range taskPlanRows(7) {
		if validLandingPlanDestination(row.Dest) {
			t.Fatalf("task cell %q must not be a landing-table cell", row.Dest)
		}
	}
}

// `/repo/source/.mission-control` is the ONE named landing edge: the cover
// shadows the real path so the sealed task bytes stay reachable only through
// RO `/repo/task`, never through the RW source alias (ADR-017:700).
func TestLandingNamedEdgeIsOnlyTheMissionControlCover(t *testing.T) {
	if !mountOverlapPermitted("/repo/source", "/repo/source/.mission-control") {
		t.Fatal("the landing cover is a named parent-before-child edge")
	}
	for _, child := range []string{
		"/repo/source/.git", "/repo/source/inner",
		"/repo/source/.mission-control/tasks",
	} {
		if mountOverlapPermitted("/repo/source", child) {
			t.Fatalf("%q is not a named landing edge", child)
		}
	}
	// The task root admits no children in this table, and `/repo` is not a
	// bind at all, so it grants no root edge the way `/workspace` does.
	if mountOverlapPermitted("/repo/task", "/repo/task/source") {
		t.Fatal("the sealed task root is bound whole and RO; it opens no child edge")
	}
	if mountOverlapPermitted("/repo", "/repo/source") {
		t.Fatal("/repo is not a bind and grants no root edge")
	}
}

// The grammar change's whole point: `/repo/...` stops being a PROTOCOL error
// (a confused planner) and becomes an ordinary jurisdiction DENIAL, because
// no landing kind has an authorized typed root yet. Fail-closed, and the
// refusal is the recoverable kind rather than a wedged dispatch.
func TestPlanMountsLandingCellIsDeniedNotAProtocolError(t *testing.T) {
	ws, _ := tsBuild(t)
	in := mtInputs(t, ws, 7)
	source := filepath.Join(ws, "landing-source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	_, r, err := planMounts([]mountRequest{{
		Source: source, Access: boundary.AccessRW, Authority: refusal.AuthorityDeployment,
		Kind: boundary.KindLandingWorksource, Destination: "/repo/source",
	}}, in)
	if err != nil {
		t.Fatalf("a landing cell is inside the closed table, not a protocol error: %v", err)
	}
	if r == nil || r.Code != boundary.CodeDeniedRoot {
		t.Fatalf("the landing lane is inert: want denied_root, got %+v", r)
	}
}

// The envelope stays unplannable at the seam as well as in the grammar.
func TestPlanMountsRejectsTheLandingEnvelopeAsABind(t *testing.T) {
	ws, _ := tsBuild(t)
	in := mtInputs(t, ws, 7)
	envelope := filepath.Join(ws, "landing.json")
	if err := os.WriteFile(envelope, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := planMounts([]mountRequest{{
		Source: envelope, Access: boundary.AccessRO, Authority: refusal.AuthorityDeployment,
		Kind: boundary.KindLandingEnvelope, Destination: "/mc/landing.json",
	}}, in)
	if err == nil {
		t.Fatal("the resident materializes /mc/landing.json; dispatch may not plan it as a bind")
	}
}

func TestValidatePrivateMountPlanAcceptsLandingDestinations(t *testing.T) {
	plan := &PrivateDispatchMountPlan{Version: 1, Entries: []PrivateDispatchMountEntry{
		{LogicalID: "landing-worksource", Source: "/w/repo", Destination: "/repo/source",
			Kind: "dir", Access: "rw", Device: "1", Inode: "2", OwnerUID: 501, Mode: 0o755},
		{LogicalID: "landing-mission-control-cover", Source: "/w/cover", Destination: "/repo/source/.mission-control",
			Kind: "dir", Access: "ro", Device: "1", Inode: "3", OwnerUID: 501, Mode: 0o755, RequireEmptyDir: true},
		{LogicalID: "landing-task-root", Source: "/w/task", Destination: "/repo/task",
			Kind: "dir", Access: "ro", Device: "1", Inode: "4", OwnerUID: 501, Mode: 0o555},
	}}
	if err := validatePrivateMountPlan(plan); err != nil {
		t.Fatalf("the helper boundary must accept the landing cells: %v", err)
	}
	// ...but not any other /mc or /repo path a broker invents.
	plan.Entries = append(plan.Entries, PrivateDispatchMountEntry{
		LogicalID: "landing-envelope", Source: "/w/landing.json", Destination: "/repo/task/source",
		Kind: "dir", Access: "rw", Device: "1", Inode: "5", OwnerUID: 501, Mode: 0o755,
	})
	if err := validatePrivateMountPlan(plan); err == nil {
		t.Fatal("a fabricated landing child must be refused at the helper boundary")
	}
}
