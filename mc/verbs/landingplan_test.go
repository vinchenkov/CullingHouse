package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mc/boundary"
	"mc/refusal"
)

// The landing table is ADR-017:699-702. These tests pin the table, its
// destination grammar, and the host-anchor resolver — and, load-bearingly,
// that NONE of it is reachable from the ADR-016 D5 mount plan. See
// landingplan.go: the plan carrier is an agent-plane mechanism and the
// `/repo` plane is composed by the resident.

func TestLandingMountRowsAreTheClosedADR017Table(t *testing.T) {
	rows := landingMountRows()
	if len(rows) != 4 {
		t.Fatalf("the landing table has exactly 4 rows (ADR-017:699-702), got %d", len(rows))
	}
	want := []landingMountRow{
		{Kind: boundary.KindLandingWorksource, Dest: "/repo/source", Access: boundary.AccessRW, IsDir: true},
		{Kind: boundary.KindLandingMissionControlCover, Dest: "/repo/source/.mission-control", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true, Generated: true},
		{Kind: boundary.KindLandingTaskRoot, Dest: "/repo/task", Access: boundary.AccessRO, IsDir: true},
		{Kind: boundary.KindLandingEnvelope, Dest: "/mc/landing.json", Access: boundary.AccessRO, Generated: true},
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
	for _, row := range landingMountRows() {
		if row.Access == boundary.AccessRW {
			writable = append(writable, row.Dest)
		}
	}
	if len(writable) != 1 || writable[0] != "/repo/source" {
		t.Fatalf("landing writes exactly the real Worksource root, got %v", writable)
	}
}

func TestValidLandingDestinationIsClosed(t *testing.T) {
	for _, row := range landingMountRows() {
		if !validLandingDestination(row.Dest) {
			t.Fatalf("%q is a landing cell", row.Dest)
		}
	}
	for _, dest := range []string{
		"/mc/setup.json", "/mc/session", "/mc",
		"/repo", "/repo/", "/repo/source/", "/repo/source/.git",
		"/repo/task/source", "/repo/task/git", "/repo/seal", "/repo/projection",
		"/repo/source/.mission-control/tasks", "/reposource", "/workspace", "",
	} {
		if validLandingDestination(dest) {
			t.Fatalf("%q is outside the landing table", dest)
		}
	}
}

// THE LOAD-BEARING ONE. The ADR-016 D5 plan is an AGENT-plane carrier:
// resident/src/effects.ts:90-95 refuses any destination outside `/workspace`,
// so a landing row can never be a plan entry. Both Go seams must agree, and
// must keep agreeing — a first draft of this slice taught them the `/repo`
// grammar, which bought no capability (the resident would have refused the
// spawn) and silently widened two guards that share these predicates: the
// task-precreate fabrication guard and the agent/landing class separation.
func TestNoLandingCellIsPlanAddressable(t *testing.T) {
	for _, row := range landingMountRows() {
		if validTaskPlanDestination(row.Dest) {
			t.Fatalf("landing cell %q must never be a plannable destination", row.Dest)
		}
		plan := &PrivateDispatchMountPlan{Version: 1, Entries: []PrivateDispatchMountEntry{{
			LogicalID: row.Kind.String(), Source: "/w/anything", Destination: row.Dest,
			Kind: "dir", Access: string(row.Access), Device: "1", Inode: "2", OwnerUID: 501, Mode: 0o755,
		}}}
		err := validatePrivateMountPlan(plan)
		if err == nil {
			t.Fatalf("the helper boundary must refuse landing cell %q as a plan entry", row.Dest)
		}
		if !strings.Contains(err.Error(), "outside the ordinary namespace") {
			t.Fatalf("landing cell %q refused for the wrong reason: %v", row.Dest, err)
		}
	}
	// The task table is likewise not a landing grammar cell: the two classes
	// partition, which is what lets each guard key off one predicate alone.
	for _, row := range taskPlanRows(7) {
		if validLandingDestination(row.Dest) {
			t.Fatalf("task cell %q must not be a landing-table cell", row.Dest)
		}
	}
}

// planMounts treats a `/repo` cell as a confused planner, which is correct:
// nothing may ask the agent-plane carrier for a resident-composed plane.
func TestPlanMountsRefusesEveryLandingCell(t *testing.T) {
	ws, _ := tsBuild(t)
	in := mtInputs(t, ws, 7)
	source := filepath.Join(ws, "landing-source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, row := range landingMountRows() {
		_, _, err := planMounts([]mountRequest{{
			Source: source, Access: row.Access, Authority: refusal.AuthorityDeployment,
			Kind: row.Kind, Destination: row.Dest,
		}}, in)
		if err == nil {
			t.Fatalf("a %q plan request is a protocol error, not a plannable row", row.Dest)
		}
	}
}

// The precreate fabrication guard keys off validTaskPlanDestination ALONE.
// Its coverage of a landing cell is asserted rather than assumed: the draft
// that widened the shared predicate let a precreate plan — the one class that
// by construction has no materialized task yet — carry RW to the real
// Worksource checkout.
func TestTaskPrecreatePlanCannotCarryALandingCell(t *testing.T) {
	plan := &PrivateDispatchMountPlan{
		Version: 1,
		TaskPrecreate: &PrivateDispatchTaskPrecreate{
			ChildMode: 0o700, TaskID: 7, WorkspaceRoot: "/w",
			TasksParent: PrivateDispatchPathIdentity{Canonical: "/w/.mission-control/tasks", Device: "1", Inode: "9", OwnerUID: 501},
			Setup:       &PrivateDispatchTaskSetup{Mode: "fresh", ObjectFormat: "sha1", TargetRef: "refs/heads/main"},
		},
		Entries: []PrivateDispatchMountEntry{{
			LogicalID: "landing-worksource", Source: "/w", Destination: "/repo/source",
			Kind: "dir", Access: "rw", Device: "1", Inode: "2", OwnerUID: 501, Mode: 0o755,
		}},
	}
	if err := validatePrivateMountPlan(plan); err == nil {
		t.Fatal("a precreate plan must not authorize RW to the real Worksource checkout")
	}
}

// --- the host-anchor resolver ------------------------------------------------

// Only the two rows with a real host source resolve. The cover and the
// envelope are GENERATED per run by the resident (ADR-017:700,:702), so there
// is no identity to resolve for them.
func TestResolveLandingRootsResolvesOnlyTheHostBackedRows(t *testing.T) {
	ws, root := lrBuild(t)
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
	for _, row := range landingMountRows() {
		if !row.Generated {
			continue
		}
		if _, ok := roots[row.Kind]; ok {
			t.Fatalf("%v is resident-generated and has no host anchor", row.Kind)
		}
	}
}

func TestResolveLandingRootsRefusesAbsentTaskRoot(t *testing.T) {
	ws := grWorkspace(t)
	if err := os.Mkdir(filepath.Join(ws, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveLandingRoots(ws, 7, os.Getuid()); err == nil {
		t.Fatal("landing has nothing to land without the sealed task root")
	}
}

// ADR-017:699 grants RW to a "real Git Worksource root". A directory with no
// administrative Git entry is not one, and landing would import a closure and
// create a ref in a non-repository.
func TestResolveLandingRootsRefusesANonRepositoryWorksource(t *testing.T) {
	ws, _ := lrBuild(t)
	if err := os.Remove(filepath.Join(ws, ".git")); err != nil {
		t.Fatal(err)
	}
	_, err := resolveLandingRoots(ws, 7, os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "administrative .git entry") {
		t.Fatalf("want the not-a-real-Git-Worksource refusal, got %v", err)
	}
}

// The RW anchor takes no exact-mode fence (a real repository is commonly
// 0755), but a non-owner able to plant content in the tree about to receive
// the system's only RW repository grant is not a trusted anchor.
func TestResolveLandingRootsRefusesAWritableByOthersWorksource(t *testing.T) {
	for _, mode := range []os.FileMode{0o775, 0o757} {
		ws, _ := lrBuild(t)
		if err := os.Chmod(ws, mode); err != nil {
			t.Fatal(err)
		}
		_, err := resolveLandingRoots(ws, 7, os.Getuid())
		if err == nil || !strings.Contains(err.Error(), "group- or world-writable") {
			t.Fatalf("mode %o: want the writable-anchor refusal, got %v", mode, err)
		}
	}
}

// Each arm is exercised separately so a fence deleted from one is not masked
// by the other refusing first.
func TestResolveLandingRootsFencesTheTaskRootSeparately(t *testing.T) {
	ws, root := lrBuild(t)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := resolveLandingRoots(ws, 7, os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "mode-0555 sealed task root") {
		t.Fatalf("a writable task root is not the sealed reviewed repository, got %v", err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}

	// The ownership fence on the TASK ROOT specifically. Passing a foreign uid
	// would trip the Worksource arm first and prove nothing about this one, so
	// the task root is made foreign-looking by asking for the uid that owns
	// the Worksource while the task root reports another. That is not possible
	// without privileges, so instead the Worksource arm is satisfied and the
	// task root replaced by a file — the next fence down in the same arm.
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = resolveLandingRoots(ws, 7, os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "landing task root is not a directory") {
		t.Fatalf("want the task-root kind refusal, got %v", err)
	}
}

func TestResolveLandingRootsRefusesForeignOwnedAnchors(t *testing.T) {
	ws, _ := lrBuild(t)
	_, err := resolveLandingRoots(ws, 7, os.Getuid()+4242)
	if err == nil || !strings.Contains(err.Error(), "landing Worksource root is not owned by the operator") {
		t.Fatalf("want the Worksource ownership refusal, got %v", err)
	}
}

// `/repo/source` is the ONE real-repository RW grant in the system. An
// aliased path reaching it would put that grant somewhere the operator never
// registered.
func TestResolveLandingRootsRefusesAliasedAnchors(t *testing.T) {
	ws, root := lrBuild(t)
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
	_, err := resolveLandingRoots(ws, 7, os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "landing task root is a symlink") {
		t.Fatalf("a symlinked task root must not be landed from, got %v", err)
	}
}

// The canonical-resolution fence catches what the symlink check cannot: a
// path whose FINAL component is real but whose ancestry resolves elsewhere.
// Without its own case this fence is vacuous — the symlink checks reach every
// other aliased shape first.
func TestResolveLandingRootsRefusesANonCanonicalAncestry(t *testing.T) {
	ws, root := lrBuild(t)
	// Replace the `tasks` PARENT with a symlink to a sibling directory holding
	// a real, correctly-shaped task root. Every component of the constructed
	// path exists and the leaf is not itself a symlink, so only the canonical
	// comparison can refuse it.
	tasks := filepath.Dir(root)
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(tasks); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(ws, "real-tasks")
	if err := os.MkdirAll(filepath.Join(real, "task-7"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(real, "task-7"), 0o700) })
	if err := os.Chmod(filepath.Join(real, "task-7"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, tasks); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := resolveLandingRoots(ws, 7, os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "does not resolve to its constructed path") {
		t.Fatalf("want the canonical-ancestry refusal, got %v", err)
	}
}

func TestResolveLandingRootsRefusesNonCanonicalTaskID(t *testing.T) {
	ws, _ := lrBuild(t)
	for _, id := range []int64{0, -1} {
		if _, err := resolveLandingRoots(ws, id, os.Getuid()); err == nil {
			t.Fatalf("task id %d is not a canonical positive decimal", id)
		}
	}
}

// lrBuild is tsBuild plus the administrative `.git` entry that makes the
// workspace a real Git Worksource root (ADR-017:699).
func lrBuild(t *testing.T) (string, string) {
	t.Helper()
	ws, root := tsBuild(t)
	if err := os.Mkdir(filepath.Join(ws, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return ws, root
}

// ---------------------------------------------------------------------------
// The blocked floor governs the landing's host anchors too (contract §3 row 1).
//
// The agent plane already meets the floor: planMounts calls
// `in.Blocked.Rejects(...)` on every typed source (mountplan.go:336) and passes
// the policy into Authorize (:353). The LANDING plane did not, and landing is
// the one class that binds a real Worksource checkout RW — "the only grant in
// the system that gets a real Worksource repository RW, intentionally including
// its primary checkout" (ADR-017:699).
//
// So the asymmetry was exactly backwards: the strongest grant in the system had
// the weakest source check. A Worksource registered under a blocked address
// component would be refused for every agent spawn and still bound RW into a
// landing container.
//
// This is not hypothetical bookkeeping: `mc init` (init.go:85) writes
// `sandbox_profiles.workspace_root` with no boundary call at all, so nothing
// upstream of here has ever vetted that path either.
// ---------------------------------------------------------------------------

func TestResolveLandingRootsRefusesABlockedWorksourceAddress(t *testing.T) {
	for _, component := range []string{".ssh", "secrets", "credentials", ".aws"} {
		t.Run(component, func(t *testing.T) {
			// A Worksource whose canonical path passes through a blocked
			// component. Everything else about it is a perfectly good repo.
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ws := filepath.Join(base, component, "ws")
			if err := os.MkdirAll(ws, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(ws, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			tsBuildAt(t, ws)

			if _, err := resolveLandingRoots(ws, 7, os.Getuid()); err == nil {
				t.Fatalf("a Worksource under a blocked %q component resolved for a landing; "+
					"the agent plane refuses this address (mountplan.go:336) while landing "+
					"would bind it RW", component)
			}
		})
	}
}

// The floor must not over-reach: an ordinary Worksource still resolves. Without
// this, a rejection bug that refused everything would look like a passing fence.
func TestResolveLandingRootsStillAcceptsAnOrdinaryWorksource(t *testing.T) {
	ws, _ := lrBuild(t)
	if _, err := resolveLandingRoots(ws, 7, os.Getuid()); err != nil {
		t.Fatalf("an ordinary Worksource was refused by the blocked floor: %v", err)
	}
}
