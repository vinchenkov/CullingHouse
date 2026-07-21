package verbs

import (
	"path"
	"strconv"
	"strings"
	"testing"
)

// The `Landing` instruction on the private mount plan (step 3 of the sealed
// landing lane). It is the carrier for the two HOST-backed anchors of the
// ADR-017:699-702 table plus the cover obligation — never the `/repo` mount
// rows themselves, which stay resident-composed (landingplan.go).
//
// INERT: no attester produces this yet. These tests are the grammar step 5
// must satisfy, not evidence of a live path.

func landingPlanFixture() *PrivateDispatchMountPlan {
	ws := "/Users/op/repo"
	return &PrivateDispatchMountPlan{
		Version: 1, Entries: []PrivateDispatchMountEntry{},
		Landing: &PrivateDispatchLanding{
			ApprovedRunID: "a1b2c3d4e5f60718",
			Branch:        taskAssignmentBranch(7),
			ClosureDigest: strings.Repeat("d", 64),
			CoverDest:     "/repo/source/.mission-control",
			LandingID:     "0011223344556677",
			LocalRepoUUID: "0f9c1e2a-3b4c-4d5e-8f60-112233445566",
			ObjectFormat:  "sha1",
			PinnedBaseSHA: strings.Repeat("c", 40),
			PreMergeSHA:   strings.Repeat("b", 40),
			TargetRef:     "main",
			TaskID:        7,
			TaskRoot: PrivateDispatchPathIdentity{
				Canonical: path.Join(ws, ".mission-control", "tasks", "task-7"),
				Device:    "16777232", Inode: "9001", OwnerUID: 501,
			},
			VerifiedSHA: strings.Repeat("a", 40),
			WorksourceRoot: PrivateDispatchPathIdentity{
				Canonical: ws, Device: "16777232", Inode: "42", OwnerUID: 501,
			},
		},
	}
}

func TestPrivateMountPlanLandingEvidenceIsClosed(t *testing.T) {
	if err := validatePrivateMountPlan(landingPlanFixture()); err != nil {
		t.Fatalf("valid landing plan: %v", err)
	}
	bad := map[string]func(*PrivateDispatchLanding){
		"task":       func(l *PrivateDispatchLanding) { l.TaskID = 0 },
		"landing-id": func(l *PrivateDispatchLanding) { l.LandingID = "not-hex" },
		// ADR-016:846 puts the approved-run identity on the landing container's
		// labels. A carrier without it cannot be labelled as the ADR requires.
		"approved-run":  func(l *PrivateDispatchLanding) { l.ApprovedRunID = "" },
		"landing-len":   func(l *PrivateDispatchLanding) { l.LandingID = strings.Repeat("a", 15) },
		"branch-drift":  func(l *PrivateDispatchLanding) { l.Branch = "mc/task-8" },
		"target":        func(l *PrivateDispatchLanding) { l.TargetRef = "" },
		"format":        func(l *PrivateDispatchLanding) { l.ObjectFormat = "sha3" },
		"verified":      func(l *PrivateDispatchLanding) { l.VerifiedSHA = "bad" },
		"premerge":      func(l *PrivateDispatchLanding) { l.PreMergeSHA = "bad" },
		"base":          func(l *PrivateDispatchLanding) { l.PinnedBaseSHA = "bad" },
		"digest":        func(l *PrivateDispatchLanding) { l.ClosureDigest = "bad" },
		"uuid":          func(l *PrivateDispatchLanding) { l.LocalRepoUUID = "not-a-uuid" },
		"cover-missing": func(l *PrivateDispatchLanding) { l.CoverDest = "" },
		"cover-outside": func(l *PrivateDispatchLanding) { l.CoverDest = "/repo/task/.mission-control" },
		// The cover cell is fixed. Any other landing table cell here would
		// place the generated empty directory over a row that must carry real
		// bytes — /repo/source RW most dangerously of all.
		"cover-is-source":  func(l *PrivateDispatchLanding) { l.CoverDest = "/repo/source" },
		"ws-relative":      func(l *PrivateDispatchLanding) { l.WorksourceRoot.Canonical = "repo" },
		"ws-uncanonical":   func(l *PrivateDispatchLanding) { l.WorksourceRoot.Canonical = "/Users/op/repo/" },
		"ws-device":        func(l *PrivateDispatchLanding) { l.WorksourceRoot.Device = "0x10" },
		"ws-owner":         func(l *PrivateDispatchLanding) { l.WorksourceRoot.OwnerUID = -1 },
		"root-inode":       func(l *PrivateDispatchLanding) { l.TaskRoot.Inode = "9001x" },
		"root-owner-drift": func(l *PrivateDispatchLanding) { l.TaskRoot.OwnerUID = 502 },
	}
	for name, mutate := range bad {
		plan := landingPlanFixture()
		mutate(plan.Landing)
		if err := validatePrivateMountPlan(plan); err == nil {
			t.Fatalf("%s: invalid landing evidence was accepted", name)
		}
	}
}

// The two sides of this crossing disagree about canonical decimals, and the
// disagreement is PRE-EXISTING and SHARED: the envelope side's
// `decimalIdentity` refuses a leading zero, the helper boundary's
// `validDecimalText` accepts one. Landing inherits the looser predicate rather
// than tightening it, because `validDecimalText` also gates task precreate,
// the completion seal, the accepted-seal rebuild and the verifier projection —
// changing it here would move four unrelated guards, which is exactly the
// mistake 55c2949's review reversed. It is not a hole at this layer: the
// resident compares these against `strconv.FormatUint` output, which never
// emits a leading zero, so "01" fails the later identity comparison instead.
// Pinned here so the asymmetry is visible rather than silent.
// Owner: whoever next unifies the private-scalar grammar.
func TestPrivateMountPlanLandingInheritsTheSharedDecimalGrammar(t *testing.T) {
	plan := landingPlanFixture()
	plan.Landing.TaskRoot.Inode = "09001"
	if err := validatePrivateMountPlan(plan); err != nil {
		t.Fatalf("landing did not inherit the shared decimal predicate: %v", err)
	}
}

// The sealed task root is DERIVED from the Worksource root and the task id —
// the same construction resolveLandingRoots uses. A carrier that could name a
// root anywhere would hand the reviewed-repository RO row to an arbitrary
// directory, and with it the closure the lander imports into the real repo.
func TestPrivateMountPlanLandingTaskRootIsDerivedNotNamed(t *testing.T) {
	for _, elsewhere := range []string{
		"/Users/op/repo/.mission-control/tasks/task-8",
		"/Users/op/repo/.mission-control/tasks",
		"/Users/op/other/.mission-control/tasks/task-7",
		"/Users/op/repo/task-7",
	} {
		plan := landingPlanFixture()
		plan.Landing.TaskRoot.Canonical = elsewhere
		if err := validatePrivateMountPlan(plan); err == nil {
			t.Fatalf("landing accepted an underived task root %q", elsewhere)
		}
	}
	// The derivation follows the task id, not a constant.
	plan := landingPlanFixture()
	plan.Landing.TaskID = 12
	plan.Landing.Branch = taskAssignmentBranch(12)
	plan.Landing.TaskRoot.Canonical = path.Join(
		plan.Landing.WorksourceRoot.Canonical, ".mission-control", "tasks",
		"task-"+strconv.FormatInt(12, 10))
	if err := validatePrivateMountPlan(plan); err != nil {
		t.Fatalf("a coherent landing plan for another task: %v", err)
	}
}

// Landing is not a setup step and carries no agent plane. Both fences matter:
// sharing a plan with a setup step would let one attestation authorize a
// mutating setup container AND the system's only RW real-repository grant,
// and any plan entry would put an agent-plane mount in a class ADR-017:711
// says has "no agent/model process present".
func TestPrivateMountPlanLandingExcludesEverySetupStepAndEveryEntry(t *testing.T) {
	steps := map[string]func(*PrivateDispatchMountPlan){
		"task-precreate": func(p *PrivateDispatchMountPlan) {
			p.TaskPrecreate = &PrivateDispatchTaskPrecreate{
				ChildMode: taskSkeletonChildMode, TaskID: 7, WorkspaceRoot: "/Users/op/repo",
				TasksParent: PrivateDispatchPathIdentity{
					Canonical: "/Users/op/repo/.mission-control/tasks",
					Device:    "16777232", Inode: "77", OwnerUID: 501},
				Setup: &PrivateDispatchTaskSetup{
					Mode: "fresh", ObjectFormat: "sha1", TargetRef: "refs/heads/main"},
			}
		},
		"completion-seal": func(p *PrivateDispatchMountPlan) {
			p.CompletionSeal = &PrivateDispatchCompletionSeal{
				RunID: "worker-1", TaskID: 7,
				SealsParent: PrivateDispatchPathIdentity{
					Canonical: "/mc/home/seals", Device: "1", Inode: "2", OwnerUID: 501},
			}
		},
		"accepted-seal-rebuild": func(p *PrivateDispatchMountPlan) {
			p.AcceptedSealRebuild = &PrivateDispatchAcceptedSealRebuild{
				ClosureDigest: strings.Repeat("b", 64), CompletionRequest: "0011223344556677",
				Device: "1", Inode: "2", ManifestDigest: strings.Repeat("c", 64),
				ObjectFormat: "sha1", OwnerUID: 501, RunID: "worker-1",
				SealedSHA: strings.Repeat("a", 40), TaskID: 7,
			}
		},
		"verifier-projection": func(p *PrivateDispatchMountPlan) {
			p.VerifierProjection = &PrivateDispatchVerifierProjection{
				ClosureDigest: strings.Repeat("b", 64), CompletionRequest: "0011223344556677",
				ManifestDigest: strings.Repeat("c", 64), ObjectFormat: "sha1",
				RebuildRunID: "verifier-1", SealDevice: "1", SealInode: "2", SealOwnerUID: 501,
				SealedSHA: strings.Repeat("a", 40), TaskID: 7,
			}
		},
	}
	for name, add := range steps {
		plan := landingPlanFixture()
		add(plan)
		if err := validatePrivateMountPlan(plan); err == nil {
			t.Fatalf("a landing plan shared its attestation with the %s step", name)
		}
	}

	plan := landingPlanFixture()
	plan.Entries = []PrivateDispatchMountEntry{{
		LogicalID: "artifact-0", Source: "/Users/op/artifacts", Destination: "/workspace/artifacts/a",
		Access: "ro", Kind: "dir", Device: "1", Inode: "3", OwnerUID: 501, Mode: 0o755,
	}}
	if err := validatePrivateMountPlan(plan); err == nil {
		t.Fatal("a landing plan carried an agent-plane mount entry")
	}
}
