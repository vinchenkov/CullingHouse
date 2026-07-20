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
		{Kind: boundary.KindLandingMissionControlCover, Dest: "/repo/source/.mission-control", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true, ResidentMaterialized: true},
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
	for _, dest := range []string{"/repo/source", "/repo/task"} {
		if !validLandingPlanDestination(dest) {
			t.Fatalf("%q is a bindable landing cell", dest)
		}
	}
	// The two GENERATED rows are resident-materialized and never plan entries:
	// the cover is a per-run empty directory and the envelope a per-run file,
	// neither of which exists when attest captures host identity evidence.
	// Admitting the envelope would also make /mc plan-addressable.
	for _, dest := range []string{
		"/repo/source/.mission-control",
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

// --- the producer (step 2 of the lane) --------------------------------------

// Only the two rows with a real host source resolve. The cover and the
// envelope are GENERATED per run by the resident, so they have no identity to
// capture at attest and deliberately get no authorized root: requesting one
// still denies. That is what keeps `/repo` containment a step-6 Docker
// obligation rather than a plan-level claim the plan cannot back.
func TestResolveLandingRootsResolvesOnlyTheHostBackedRows(t *testing.T) {
	ws, root := tsBuild(t)
	roots, err := resolveLandingRoots(ws, 7, os.Getuid())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("want the 2 host-backed landing rows, got %d: %v", len(roots), roots)
	}
	if got := roots[boundary.KindLandingWorksource].Canonical; got != ws {
		t.Fatalf("landing worksource = %q, want the real repo root %q", got, ws)
	}
	if got := roots[boundary.KindLandingTaskRoot].Canonical; got != root {
		t.Fatalf("landing task root = %q, want %q", got, root)
	}
	for _, kind := range []boundary.TypedKind{
		boundary.KindLandingMissionControlCover, boundary.KindLandingEnvelope,
	} {
		if _, ok := roots[kind]; ok {
			t.Fatalf("%v is resident-generated and has no attest-time root", kind)
		}
	}
}

func TestResolveLandingRootsRefusesAbsentTaskRoot(t *testing.T) {
	ws := grWorkspace(t)
	if _, err := resolveLandingRoots(ws, 7, os.Getuid()); err == nil {
		t.Fatal("landing has nothing to land without the sealed task root")
	}
}

// The sealed task root is the reviewed repository. Landing reads it RO, so
// its 0555 operator-owned shape is the same fence the agent plan applies —
// a writable or foreign-owned root is never landed from.
func TestResolveLandingRootsRefusesUntrustedTaskRoot(t *testing.T) {
	ws, root := tsBuild(t)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveLandingRoots(ws, 7, os.Getuid()); err == nil {
		t.Fatal("a writable task root is not the sealed reviewed repository")
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveLandingRoots(ws, 7, os.Getuid()+4242); err == nil {
		t.Fatal("a task root owned by someone else is refused")
	}
}

// `/repo/source` is the ONE real-repository RW grant in the system. An
// aliased path reaching it would put that grant somewhere the operator never
// registered, so both anchors must be their own exact canonical path.
func TestResolveLandingRootsRefusesAliasedAnchors(t *testing.T) {
	ws, root := tsBuild(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(ws, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := resolveLandingRoots(alias, 7, os.Getuid()); err == nil {
		t.Fatal("a symlinked Worksource must not receive the RW landing grant")
	}

	// ...and the task root may not be an alias either.
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Mkdir(elsewhere, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, root); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := resolveLandingRoots(ws, 7, os.Getuid()); err == nil {
		t.Fatal("a symlinked task root must not be landed from")
	}
}

func TestResolveLandingRootsRefusesNonCanonicalTaskID(t *testing.T) {
	ws, _ := tsBuild(t)
	for _, id := range []int64{0, -1} {
		if _, err := resolveLandingRoots(ws, id, os.Getuid()); err == nil {
			t.Fatalf("task id %d is not a canonical positive decimal", id)
		}
	}
}

// The producer is what turns the inert grammar into a plannable cell: with
// the landing roots in jurisdiction, the RW `/repo/source` request that
// TestPlanMountsLandingCellIsDeniedNotAProtocolError sees denied now plans.
// Both tests must hold at once — that is the difference between the lane
// existing and the lane being ON.
func TestPlanMountsAuthorizesLandingRowsOnceTheProducerSupplies(t *testing.T) {
	ws, root := tsBuild(t)
	in := mtInputs(t, ws, 7)
	roots, err := resolveLandingRoots(ws, 7, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	typedRoots := map[boundary.TypedKind][]boundary.ProtectedID{}
	for kind, id := range roots {
		typedRoots[kind] = []boundary.ProtectedID{id}
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o750); err != nil {
		t.Fatal(err)
	}
	j, err := boundary.ResolveJurisdiction(boundary.JurisdictionInput{
		Home: home, TypedRoots: typedRoots,
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	in.Jurisdiction = j

	entries, r, err := planMounts([]mountRequest{
		{Source: ws, Access: boundary.AccessRW, Authority: refusal.AuthorityDeployment,
			Kind: boundary.KindLandingWorksource, Destination: "/repo/source"},
		{Source: root, Access: boundary.AccessRO, Authority: refusal.AuthorityDeployment,
			Kind: boundary.KindLandingTaskRoot, Destination: "/repo/task"},
	}, in)
	if err != nil || r != nil {
		t.Fatalf("plan: refusal=%+v err=%v", r, err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 landing entries, got %d", len(entries))
	}
	if entries[0].Destination != "/repo/source" || entries[0].Access != "rw" {
		t.Fatalf("the real repository is the one RW grant: %+v", entries[0])
	}
	if entries[1].Destination != "/repo/task" || entries[1].Access != "ro" {
		t.Fatalf("the sealed task root is read-only: %+v", entries[1])
	}

	// The kinds do not cross: the RW worksource grant may not be claimed by
	// the task-root kind, nor may a landing kind reach a task-table cell.
	_, r, err = planMounts([]mountRequest{
		{Source: ws, Access: boundary.AccessRW, Authority: refusal.AuthorityDeployment,
			Kind: boundary.KindLandingTaskRoot, Destination: "/repo/task"},
	}, in)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Code != boundary.CodeDeniedRoot {
		t.Fatalf("a landing kind claiming the other row's root is denied, got %+v", r)
	}
}

func TestValidatePrivateMountPlanAcceptsLandingDestinations(t *testing.T) {
	plan := &PrivateDispatchMountPlan{Version: 1, Entries: []PrivateDispatchMountEntry{
		{LogicalID: "landing-worksource", Source: "/w/repo", Destination: "/repo/source",
			Kind: "dir", Access: "rw", Device: "1", Inode: "2", OwnerUID: 501, Mode: 0o755},
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
