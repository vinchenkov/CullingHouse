package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The attest-side producer of `PrivateDispatchLanding`. It is the piece that
// turns spine facts plus the live host into the frozen carrier the lander is
// later handed — the "attested candidate rather than a bare effect" of
// ADR-016:369-379.
//
// STILL INERT: nothing routes a landing action into attestation yet (that is
// the seam work). This assembles the carrier; it does not arm the lane.

// lcInputs is a coherent set of spine facts for task 7.
func lcInputs(landingID string) landingCaptureInputs {
	return landingCaptureInputs{
		TaskID:        7,
		LandingID:     landingID,
		ApprovedRunID: "a1b2c3d4e5f60718",
		TargetRef:     "main",
		VerifiedSHA:   strings.Repeat("a", 40),
		ObjectFormat:  "sha1",
		PinnedBaseSHA: strings.Repeat("c", 40),
		ClosureDigest: strings.Repeat("d", 64),
		LocalRepoUUID: "0f9c1e2a-3b4c-4d5e-8f60-112233445566",
	}
}

// lcRepo is a REAL git Worksource carrying the sealed task root: the two
// things the capture needs live, since one anchor is proved by git and the
// other by its 0555 operator-owned shape.
//
// Built beneath a SYMLINK-RESOLVED root, the same as grWorkspace: the RW
// grant's anchor must be its own exact canonical path, and on macOS a raw
// t.TempDir() hands back /var/... aliasing /private/var/.... Production
// registers the resolved form, so a fixture that did not would be testing a
// path the system never produces.
func lcRepo(t *testing.T, taskID string) (workspace, head string) {
	t.Helper()
	workspace, head = buildLandingRepoAt(t, grWorkspace(t))
	root := filepath.Join(workspace, ".mission-control", "tasks", "task-"+taskID)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	return workspace, head
}

func TestCaptureLandingPlanProducesAValidCarrier(t *testing.T) {
	ws, _ := lcRepo(t, "7")
	id, err := deriveLandingID(landingIdentityFixture())
	if err != nil {
		t.Fatal(err)
	}
	got, err := captureLandingPlan(ws, os.Getuid(), lcInputs(id))
	if err != nil {
		t.Fatalf("capture over a real Worksource: %v", err)
	}

	// The carrier must satisfy the same validator the commit side applies. This
	// is the assertion that matters: a producer that emits something the
	// validator rejects would surface only once the lane is armed.
	plan := &PrivateDispatchMountPlan{Version: 1, Entries: []PrivateDispatchMountEntry{}, Landing: &got}
	if err := validatePrivateMountPlan(plan); err != nil {
		t.Fatalf("the captured landing carrier does not satisfy validatePrivateMountPlan: %v", err)
	}
}

// PreMergeSHA is the whole reason attest observes the target at all: it is the
// target preimage FROZEN at attest, which `landsealedrun.go:142-145` later
// refuses divergence from.
func TestCaptureLandingPlanFreezesTheObservedTargetTip(t *testing.T) {
	ws, head := lcRepo(t, "7")
	got, err := captureLandingPlan(ws, os.Getuid(), lcInputs("0011223344556677"))
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got.PreMergeSHA != head {
		t.Fatalf("pre-merge SHA = %q, want the observed target tip %q", got.PreMergeSHA, head)
	}
	// And it is an OBSERVATION, not an echo of a carried input: nothing in
	// lcInputs equals the tip.
	in := lcInputs("0011223344556677")
	for name, carried := range map[string]string{
		"verified": in.VerifiedSHA, "base": in.PinnedBaseSHA, "closure": in.ClosureDigest,
	} {
		if got.PreMergeSHA == carried {
			t.Fatalf("pre-merge SHA echoes the carried %s rather than observing the target", name)
		}
	}
}

// Every carried spine fact must arrive on the carrier unmodified — the lander
// treats all of them as inputs to MATCH against, never values to read off a
// repository (landsealedrun.go:28-30).
func TestCaptureLandingPlanCarriesTheSpineFactsVerbatim(t *testing.T) {
	ws, _ := lcRepo(t, "7")
	in := lcInputs("0011223344556677")
	got, err := captureLandingPlan(ws, os.Getuid(), in)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	for name, pair := range map[string][2]string{
		"landing id":    {got.LandingID, in.LandingID},
		"target ref":    {got.TargetRef, in.TargetRef},
		"verified sha":  {got.VerifiedSHA, in.VerifiedSHA},
		"object format": {got.ObjectFormat, in.ObjectFormat},
		"base sha":      {got.PinnedBaseSHA, in.PinnedBaseSHA},
		"closure":       {got.ClosureDigest, in.ClosureDigest},
		"repo uuid":     {got.LocalRepoUUID, in.LocalRepoUUID},
		"approved run":  {got.ApprovedRunID, in.ApprovedRunID},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("%s = %q, want the carried %q", name, pair[0], pair[1])
		}
	}
	if got.TaskID != in.TaskID {
		t.Fatalf("task id = %d, want %d", got.TaskID, in.TaskID)
	}
	// Branch is DERIVED, never carried: complete.go:163 is tasks.branch's only
	// writer and it is closed to assigned tasks, which is what partitions the
	// two landing lanes.
	if got.Branch != taskAssignmentBranch(in.TaskID) {
		t.Fatalf("branch = %q, want the derived %q", got.Branch, taskAssignmentBranch(in.TaskID))
	}
	// The cover obligation is non-negotiable: without it the sealed task root
	// is reachable RW through the source alias.
	if got.CoverDest != landingCoverDest {
		t.Fatalf("cover dest = %q, want %q", got.CoverDest, landingCoverDest)
	}
}

func TestCaptureLandingPlanRefusesWhatTheLanderWouldRefuse(t *testing.T) {
	cases := map[string]func(t *testing.T) (workspace string, in landingCaptureInputs){
		// No sealed task root: there is nothing to land from. resolveLandingRoots
		// owns this, and it must run BEFORE the git work.
		"sealed task root is absent": func(t *testing.T) (string, landingCaptureInputs) {
			ws, _ := buildLandingRepoAt(t, grWorkspace(t))
			return ws, lcInputs("0011223344556677")
		},
		// A task root belonging to a DIFFERENT task is not this landing's root.
		"sealed task root belongs to another task": func(t *testing.T) (string, landingCaptureInputs) {
			ws, _ := lcRepo(t, "8")
			return ws, lcInputs("0011223344556677")
		},
		// The target must exist as a real local branch before it can be a merge
		// destination; the lander refuses this and so must the attester.
		"target branch does not exist": func(t *testing.T) (string, landingCaptureInputs) {
			ws, _ := lcRepo(t, "7")
			in := lcInputs("0011223344556677")
			in.TargetRef = "nonexistent"
			return ws, in
		},
		// An operator merge already in flight is never ours to disturb, and a
		// plan frozen over it would name a preimage about to move.
		"an operator merge is already in progress": func(t *testing.T) (string, landingCaptureInputs) {
			ws, _ := lcRepo(t, "7")
			gitDir := filepath.Join(ws, ".git")
			if err := os.WriteFile(filepath.Join(gitDir, "MERGE_HEAD"), []byte(strings.Repeat("e", 40)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return ws, lcInputs("0011223344556677")
		},
		// A landing id the container name and abort trailer cannot carry.
		"landing id is not 16 lowercase hex": func(t *testing.T) (string, landingCaptureInputs) {
			ws, _ := lcRepo(t, "7")
			in := lcInputs("not-hex")
			return ws, in
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			ws, in := build(t)
			if _, err := captureLandingPlan(ws, os.Getuid(), in); err == nil {
				t.Fatal("capture accepted a landing the lander would refuse")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Workspace-root resolution.
//
// This is its own fence for one reason: captureLandingPlan resolves every host
// anchor RELATIVE to the root it is handed, so an empty root resolves them
// against the process working directory. That is the single place in this lane
// where a landing could interrogate — and then write into — a repository that
// is not the operator's. A refusal is the only safe answer; returning "" and
// letting a downstream Lstat fail is not, because the paths it would probe are
// real paths on the host.
// ---------------------------------------------------------------------------

func lcState(selected, root string) PrivateDispatchMountState {
	return PrivateDispatchMountState{
		SelectedWorksource: selected,
		Worksources: []PrivateDispatchWorksource{{
			WorksourceID: "ws-test", Kind: "repo", Status: "active",
			ProfilePresent: true, ProfileID: "default", WorkspaceRoot: root,
		}},
	}
}

func TestLandingWorkspaceRootResolvesTheSelectedWorksource(t *testing.T) {
	got, err := landingWorkspaceRoot(lcState("ws-test", "/srv/ws-test"))
	if err != nil {
		t.Fatalf("landingWorkspaceRoot: %v", err)
	}
	if got != "/srv/ws-test" {
		t.Fatalf("workspace root = %q, want /srv/ws-test", got)
	}
}

func TestLandingWorkspaceRootRefusesUnresolvedWorksource(t *testing.T) {
	for name, state := range map[string]PrivateDispatchMountState{
		"no selected worksource":    lcState("", "/srv/ws-test"),
		"selection names no row":    lcState("ws-absent", "/srv/ws-test"),
		"selected row has no root":  lcState("ws-test", ""),
		"no worksource rows at all": {SelectedWorksource: "ws-test"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := landingWorkspaceRoot(state)
			if err == nil {
				t.Fatalf("resolved %q instead of refusing; an unresolved root resolves the landing's host anchors against the process working directory", got)
			}
			if got != "" {
				t.Fatalf("refusal still yielded a root %q", got)
			}
		})
	}
}
