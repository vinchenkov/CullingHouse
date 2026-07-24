package dispatch

import "testing"

// dispatch.go is frozen except where ADR-025 D3 sanctions the InitiativeSetup
// emission. S1.4a lands only the pure predicate + its data fact; the emission is
// wired into Decide by S1.4b, so these tests exercise nextInitiativeSetup
// directly.

func setupDone() tmod { return func(t *Task) { t.InitiativeSetupDone = true } }

func realCfg() Config { c := DefaultConfig(); c.RealRouting = true; return c }

// A promoted (seeded, branch-set) initiative with no setup receipt owes an
// InitiativeSetup under real routing.
func TestNextInitiativeSetupSelectsAPromotedUncutInitiative(t *testing.T) {
	rec := recs([]Task{
		tk(5, StatusSeeded, initiative(), branch("mc/initiative-5", "")),
	}, nil)
	id, ok := nextInitiativeSetup(rec, realCfg())
	if !ok || id != 5 {
		t.Fatalf("nextInitiativeSetup = (%d, %v), want (5, true)", id, ok)
	}
}

// Fake routing never emits: the fake lane's children make the shared worktree
// themselves, and an emission would alter the fake dispatch sequence.
func TestNextInitiativeSetupNeverFiresUnderFakeRouting(t *testing.T) {
	rec := recs([]Task{
		tk(5, StatusSeeded, initiative(), branch("mc/initiative-5", "")),
	}, nil)
	if id, ok := nextInitiativeSetup(rec, DefaultConfig()); ok {
		t.Fatalf("fake routing emitted InitiativeSetup for %d", id)
	}
}

func TestNextInitiativeSetupRespectsTheReceiptAndRowShape(t *testing.T) {
	cases := []struct {
		name string
		task Task
	}{
		{"already cut", tk(5, StatusSeeded, initiative(), branch("mc/initiative-5", ""), setupDone())},
		{"not promoted (no branch)", tk(5, StatusSeeded, initiative())},
		{"not seeded (proposed)", tk(5, StatusProposed, initiative(), branch("mc/initiative-5", ""))},
		{"archived", tk(5, StatusSeeded, initiative(), branch("mc/initiative-5", ""), archived())},
		{"blocked", tk(5, StatusSeeded, initiative(), branch("mc/initiative-5", ""), blocked())},
		{"a standalone task with a branch", tk(5, StatusSeeded, branch("mc/task-5", ""))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if id, ok := nextInitiativeSetup(recs([]Task{tc.task}, nil), realCfg()); ok {
				t.Fatalf("%s: unexpectedly owed setup (%d)", tc.name, id)
			}
		})
	}
}

// One per tick, lowest id first (deterministic).
func TestNextInitiativeSetupPicksTheLowestIDDeterministically(t *testing.T) {
	rec := recs([]Task{
		tk(9, StatusSeeded, initiative(), branch("mc/initiative-9", "")),
		tk(4, StatusSeeded, initiative(), branch("mc/initiative-4", "")),
		tk(7, StatusSeeded, initiative(), branch("mc/initiative-7", ""), setupDone()),
	}, nil)
	id, ok := nextInitiativeSetup(rec, realCfg())
	if !ok || id != 4 {
		t.Fatalf("nextInitiativeSetup = (%d, %v), want the lowest uncut id (4, true)", id, ok)
	}
}
