package verbs

// The sealed landing lane's own prepare/attest/commit (ADR-016:369-379). These
// tests drive the lane's functions DIRECTLY: nothing selects a sealed row yet,
// and nothing will until the activation switch, so a test that went through
// `mc dispatch` would assert on the legacy lane and quietly prove nothing.

import (
	"testing"

	"mc/dispatch"
	"mc/substrate"
)

// dlsTask builds a row that satisfies SealedLandingPending: approved, packaged,
// unarchived, unblocked, branchless in `tasks`, and carrying its branch on the
// immutable assignment instead.
func dlsTask() dispatch.Task {
	return dispatch.Task{
		ID: 7, Title: "fix the gate", Scope: dispatch.ScopeTask,
		Status: dispatch.StatusPackaged, Decision: dispatch.DecisionApproved,
		Archived: false, Blocked: false, Worksource: "ws-test",
		Branch:      "", // the sealed lane is branchless in `tasks` by construction
		VerifiedSHA: "3333333333333333333333333333333333333333",
		TargetRef:   "refs/heads/main",
		Sealed: &dispatch.SealedAssignment{
			Branch:        "mc/task-7",
			TargetRef:     "refs/heads/main",
			TaskRootKey:   "16777220:424242",
			ObjectFormat:  "sha1",
			BaseSHA:       "1111111111111111111111111111111111111111",
			LocalRepoUUID: "de41cafe-0000-4000-8000-00000000beef",
			ClosureDigest: "2222222222222222222222222222222222222222222222222222222222222222",
		},
	}
}

func dlsMountState() PrivateDispatchMountState {
	return PrivateDispatchMountState{
		SelectedWorksource: "ws-test",
		Worksources: []PrivateDispatchWorksource{{
			WorksourceID: "ws-test", Kind: "repo", Status: "active",
			ProfilePresent: true, ProfileID: "default", WorkspaceRoot: "/srv/ws-test",
		}},
		SubjectAcceptedCompletionSeal: &substrate.DispatchAcceptedCompletionSeal{
			RunID:             "0123456789abcdef",
			CompletionRequest: "fedcba9876543210",
			ObjectFormat:      "sha1",
		},
	}
}

// ---------------------------------------------------------------------------
// The candidate projection.
//
// A landing occupies the same canonicalCandidate slot a spawn does, but it has
// no run and no role — it claims no lease and opens no Run, and `runs.role` has
// no landing member. Asserting the two fields are EMPTY is the point: a landing
// that ever acquired a run id here would be a reap target with no container,
// and one that named a role would log as that role's spawn.
// ---------------------------------------------------------------------------

func TestLandingCandidateProjectionNamesNoRunAndNoRole(t *testing.T) {
	c := landingCandidateProjection(7)
	if c.Kind != "landing" {
		t.Fatalf("candidate kind = %q, want landing", c.Kind)
	}
	if c.RunID != "" {
		t.Fatalf("landing candidate allocated run id %q; it opens no Run and would become a reap target with no container", c.RunID)
	}
	if c.Role != "" {
		t.Fatalf("landing candidate named role %q; runs.role has no landing member", c.Role)
	}
	if c.SubjectID == nil || *c.SubjectID != 7 {
		t.Fatalf("landing candidate subject = %v, want 7", c.SubjectID)
	}
	// Normalized like every other candidate so the canonical bytes never carry
	// a null where an empty collection belongs.
	if c.ProposedPool == nil || c.Wave == nil || c.DedupeTitles == nil {
		t.Fatalf("landing candidate left a nil collection: %+v", c)
	}
	if len(c.ProposedPool) != 0 || len(c.Wave) != 0 || len(c.DedupeTitles) != 0 {
		t.Fatalf("landing candidate carried spawn-only pool state: %+v", c)
	}
}

// ---------------------------------------------------------------------------
// The frozen tuple.
// ---------------------------------------------------------------------------

func TestLandingTupleProjectionCarriesBothTargetRefs(t *testing.T) {
	task := dlsTask()
	task.TargetRef = "refs/heads/main"
	task.Sealed.TargetRef = "refs/heads/release" // diverged on purpose

	got, err := landingTupleProjection(task, dlsMountState(), "0f1e2d3c4b5a6978")
	if err != nil {
		t.Fatalf("landingTupleProjection: %v", err)
	}
	if got.TargetRef != "refs/heads/main" {
		t.Fatalf("tuple target ref = %q, want the task's current ref", got.TargetRef)
	}
	if got.AssignedTargetRef != "refs/heads/release" {
		t.Fatalf("tuple assigned target ref = %q, want the assignment's frozen ref", got.AssignedTargetRef)
	}
}

func TestLandingTupleProjectionTakesTheBranchFromTheAssignment(t *testing.T) {
	// `tasks.branch` is empty for every sealed row by construction — its only
	// writer is closed to assigned tasks, which is what makes the two landing
	// lanes partition. The assignment is the sealed lane's branch home.
	got, err := landingTupleProjection(dlsTask(), dlsMountState(), "0f1e2d3c4b5a6978")
	if err != nil {
		t.Fatalf("landingTupleProjection: %v", err)
	}
	if got.Branch != "mc/task-7" {
		t.Fatalf("tuple branch = %q, want the assignment's branch", got.Branch)
	}
	if got.TaskRootKey != "16777220:424242" ||
		got.ObjectFormat != "sha1" ||
		got.PinnedBaseSHA != "1111111111111111111111111111111111111111" ||
		got.LocalRepoUUID != "de41cafe-0000-4000-8000-00000000beef" {
		t.Fatalf("tuple dropped an assignment pin: %+v", got)
	}
	if got.ApprovedRunID != "0123456789abcdef" || got.ApprovedRequestID != "fedcba9876543210" {
		t.Fatalf("tuple dropped the accepted seal's identity: %+v", got)
	}
	if got.VerifiedSHA != "3333333333333333333333333333333333333333" {
		t.Fatalf("tuple verified sha = %q", got.VerifiedSHA)
	}
	if got.LandingID != "0f1e2d3c4b5a6978" {
		t.Fatalf("tuple landing id = %q", got.LandingID)
	}
}

func TestLandingTupleProjectionRefusesMissingEvidence(t *testing.T) {
	// Both halves are load-bearing and neither has a safe default. Without the
	// assignment there is no branch, base, or repo identity to land against;
	// without the accepted seal the landing id has no approved-run input, and an
	// id derived over empty strings would collide across every task in the
	// deployment.
	noAssignment := dlsTask()
	noAssignment.Sealed = nil
	if _, err := landingTupleProjection(noAssignment, dlsMountState(), "0f1e2d3c4b5a6978"); err == nil {
		t.Fatal("projected a landing tuple with no closure assignment")
	}

	noSeal := dlsMountState()
	noSeal.SubjectAcceptedCompletionSeal = nil
	if _, err := landingTupleProjection(dlsTask(), noSeal, "0f1e2d3c4b5a6978"); err == nil {
		t.Fatal("projected a landing tuple with no accepted completion seal")
	}
}

// ---------------------------------------------------------------------------
// Lane membership.
//
// sealedLandingSubject is the commit-side re-assertion that the row prepare
// froze is still the row the sealed lane owes a landing to. It re-checks the
// whole predicate rather than the id alone, so a row that was approved at
// prepare and blocked by the time commit runs is a candidate mismatch, not a
// landing.
// ---------------------------------------------------------------------------

func TestSealedLandingSubjectSelectsOnlyPendingSealedRows(t *testing.T) {
	base := dlsTask()
	rec := dispatch.Records{Tasks: []dispatch.Task{base}}
	if got, ok := sealedLandingSubject(rec, 7); !ok || got.ID != 7 {
		t.Fatalf("sealed landing subject = %+v ok=%v, want task 7", got, ok)
	}
	if _, ok := sealedLandingSubject(rec, 8); ok {
		t.Fatal("selected a task id that is not in the records")
	}

	for name, mutate := range map[string]func(*dispatch.Task){
		"blocked":            func(t *dispatch.Task) { t.Blocked = true },
		"archived":           func(t *dispatch.Task) { t.Archived = true },
		"not approved":       func(t *dispatch.Task) { t.Decision = "" },
		"not packaged":       func(t *dispatch.Task) { t.Status = dispatch.StatusSeeded },
		"no assignment":      func(t *dispatch.Task) { t.Sealed = nil },
		"no verified sha":    func(t *dispatch.Task) { t.VerifiedSHA = "" },
		"no target ref":      func(t *dispatch.Task) { t.TargetRef = "" },
		"carries both homes": func(t *dispatch.Task) { t.Branch = "mc/task-7" },
	} {
		t.Run(name, func(t2 *testing.T) {
			task := dlsTask()
			mutate(&task)
			if _, ok := sealedLandingSubject(dispatch.Records{Tasks: []dispatch.Task{task}}, 7); ok {
				t2.Fatalf("%s row was admitted to the sealed landing lane", name)
			}
		})
	}
}
