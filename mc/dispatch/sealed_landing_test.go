package dispatch

import "testing"

// The landing lane has two branch homes, and they are mutually exclusive by
// CONSTRUCTION rather than by convention: `tasks.branch`'s only writer is the
// `--status worked --branch` terminal, which ADR-016 D6 closes to assigned
// tasks, so no row can ever carry both. `LandingPending` selects the legacy
// worktree lane; `SealedLandingPending` selects the sealed lane. Both must
// never be true at once — the whole reason one `KindLand` step can serve both
// is that the predicates partition rather than overlap.
func TestSealedLandingPendingIsTheAssignmentLane(t *testing.T) {
	assigned := func(mods ...tmod) Task {
		base := tk(2, StatusPackaged, decided(DecisionApproved, at(-30)))
		base.VerifiedSHA = "sha-2"
		base.TargetRef = "main"
		base.Sealed = &SealedAssignment{
			Branch: "mc/task-2", TargetRef: "main",
			TaskRootKey: ".mission-control/tasks/task-2", ObjectFormat: "sha1",
			BaseSHA: "base-2", LocalRepoUUID: "uuid-2", ClosureDigest: "digest-2",
		}
		for _, m := range mods {
			m(&base)
		}
		return base
	}

	t.Run("the_whole_shape_is_pending", func(t *testing.T) {
		if !assigned().SealedLandingPending() {
			t.Fatal("a sealed, approved, packaged, unblocked row is owed a landing")
		}
	})

	// One conjunct per case: each of these alone must take the row out of the
	// lane, and for a different reason.
	for _, c := range []struct {
		name string
		mod  tmod
	}{
		{"not_approved", func(t *Task) { t.Decision = ""; t.DecidedAt = nil }},
		{"rejected", func(t *Task) { t.Decision = DecisionRejected }},
		{"archived", func(t *Task) { t.Archived = true }},
		// NOTE(S6.3) applies to the sealed lane identically: a failed landing
		// blocks, and the retry trigger is the operator unblock, not the tick.
		{"blocked", func(t *Task) { t.Blocked = true }},
		{"not_packaged", func(t *Task) { t.Status = StatusVerified }},
		{"no_assignment", func(t *Task) { t.Sealed = nil }},
		{"no_verified_sha", func(t *Task) { t.VerifiedSHA = "" }},
		{"no_target_ref", func(t *Task) { t.TargetRef = "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			if assigned(c.mod).SealedLandingPending() {
				t.Fatalf("%s must not be owed a sealed landing", c.name)
			}
		})
	}

	// The partition. A legacy row is never in the sealed lane, a sealed row is
	// never in the legacy lane, and the impossible both-homes row — which the
	// substrate forbids, but which a projection bug could still synthesize —
	// is refused by BOTH rather than served twice.
	t.Run("the_two_lanes_partition", func(t *testing.T) {
		legacy := tk(3, StatusPackaged, decided(DecisionApproved, at(-30)), branch("mc/task-3", "sha-3"))
		if !legacy.LandingPending() || legacy.SealedLandingPending() {
			t.Fatal("a legacy branch-carrying row belongs to the legacy lane only")
		}
		s := assigned()
		if s.LandingPending() || !s.SealedLandingPending() {
			t.Fatal("a sealed assigned row belongs to the sealed lane only")
		}
		both := assigned(func(t *Task) { t.Branch = "mc/task-2" })
		if both.SealedLandingPending() {
			t.Fatal("a row carrying BOTH branch homes is incoherent and must not be landed sealed")
		}
	})

	// A frozen assignment target_ref that has drifted from the task's current
	// one does NOT leave the lane. Excluding it would make the row silently
	// unlandable forever; the landing step refuses it loudly instead, which is
	// what surfaces an operator retarget (ADR-016 D5: a retry reuses the
	// recorded assignment and never rebases).
	t.Run("diverged_target_ref_stays_in_the_lane", func(t *testing.T) {
		drifted := assigned(func(t *Task) { t.TargetRef = "release" })
		if !drifted.SealedLandingPending() {
			t.Fatal("target_ref drift must surface as a loud landing refusal, not a silent exclusion")
		}
	})
}
