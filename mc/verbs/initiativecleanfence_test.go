package verbs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func isDomain(err error) bool {
	var de *DomainError
	return errors.As(err, &de)
}

// materializedInitiative builds a real fresh-cut store + shared worktree (the
// output S1's `mc __setup-initiative` leaves on disk) so the D6 clean fence runs
// against genuine cross-base git state, not a fixture stand-in.
func materializedInitiative(t *testing.T) (storeRoot, worktreeRoot string) {
	t.Helper()
	src, _, objfmt := buildSourceRepo(t)
	storeRoot, worktreeRoot = mkInitiativeBases(t)
	spec := initiativeSpec()
	spec.ObjectFormat = objfmt
	if _, err := MaterializeInitiativeStore(src, storeRoot, worktreeRoot, spec); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	return storeRoot, worktreeRoot
}

func cleanFenceEnvelope(storeRoot, worktreeRoot string) InitiativeCleanFenceEnvelope {
	return InitiativeCleanFenceEnvelope{
		SchemaVersion: 1,
		Operation:     InitiativeCleanFenceOperation,
		InitiativeID:  7,
		StoreRoot:     storeRoot,
		WorktreeRoot:  worktreeRoot,
	}
}

func TestInitiativeCleanFenceAdmitsAFreshlyCutWorktree(t *testing.T) {
	store, wt := materializedInitiative(t)
	if err := RunInitiativeCleanFence(cleanFenceEnvelope(store, wt)); err != nil {
		t.Fatalf("a freshly cut worktree must pass the clean fence, got: %v", err)
	}
}

func TestInitiativeCleanFenceRefusesAnUntrackedFile(t *testing.T) {
	store, wt := materializedInitiative(t)
	if err := os.WriteFile(filepath.Join(wt, "stray.txt"), []byte("prior child residue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInitiativeCleanFence(cleanFenceEnvelope(store, wt)); err == nil {
		t.Fatal("an untracked file must fail the clean fence")
	} else if !isDomain(err) {
		t.Fatalf("want a domain refusal, got %v", err)
	}
}

func TestInitiativeCleanFenceRefusesAModifiedTrackedFile(t *testing.T) {
	store, wt := materializedInitiative(t)
	// README is seeded by buildSourceRepo; overwrite it so the working tree
	// diverges from the index.
	entries, _ := os.ReadDir(wt)
	var tracked string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != ".git" {
			tracked = e.Name()
			break
		}
	}
	if tracked == "" {
		t.Fatal("no tracked file to modify in the materialized worktree")
	}
	if err := os.WriteFile(filepath.Join(wt, tracked), []byte("mutated by a prior child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInitiativeCleanFence(cleanFenceEnvelope(store, wt)); err == nil {
		t.Fatal("a modified tracked file must fail the clean fence")
	}
}

func TestInitiativeCleanFenceRefusesADetachedHead(t *testing.T) {
	store, wt := materializedInitiative(t)
	// Detach HEAD in the linked worktree admin — a rebase-in-progress shape.
	wtGit(t, store, wt, "checkout", "--detach", "HEAD")
	if err := RunInitiativeCleanFence(cleanFenceEnvelope(store, wt)); err == nil {
		t.Fatal("a detached HEAD must fail the clean fence")
	} else if !isDomain(err) {
		t.Fatalf("want a domain refusal, got %v", err)
	}
}

func TestInitiativeCleanFenceRefusesASkipWorktreeHiddenEntry(t *testing.T) {
	store, wt := materializedInitiative(t)
	entries, _ := os.ReadDir(wt)
	var tracked string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != ".git" {
			tracked = e.Name()
			break
		}
	}
	if tracked == "" {
		t.Fatal("no tracked file to flag in the materialized worktree")
	}
	// Flag the file skip-worktree, then modify it: status would report clean, but
	// the index-visibility fence must refuse the hidden modification.
	wtGit(t, store, wt, "update-index", "--skip-worktree", tracked)
	if err := os.WriteFile(filepath.Join(wt, tracked), []byte("hidden mutation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunInitiativeCleanFence(cleanFenceEnvelope(store, wt)); err == nil {
		t.Fatal("a skip-worktree hidden modification must fail the clean fence")
	} else if !isDomain(err) {
		t.Fatalf("want a domain refusal, got %v", err)
	}
}

func TestInitiativeCleanFenceValidatesTheEnvelope(t *testing.T) {
	store, wt := materializedInitiative(t)
	cases := []struct {
		name   string
		mutate func(*InitiativeCleanFenceEnvelope)
	}{
		{"bad schema version", func(e *InitiativeCleanFenceEnvelope) { e.SchemaVersion = 2 }},
		{"wrong operation", func(e *InitiativeCleanFenceEnvelope) { e.Operation = "initiative-setup" }},
		{"non-positive id", func(e *InitiativeCleanFenceEnvelope) { e.InitiativeID = 0 }},
		{"empty store root", func(e *InitiativeCleanFenceEnvelope) { e.StoreRoot = "" }},
		{"empty worktree root", func(e *InitiativeCleanFenceEnvelope) { e.WorktreeRoot = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := cleanFenceEnvelope(store, wt)
			tc.mutate(&env)
			if err := RunInitiativeCleanFence(env); err == nil {
				t.Fatalf("%s must refuse", tc.name)
			} else if !isDomain(err) {
				t.Fatalf("want a domain refusal, got %v", err)
			}
		})
	}
}
