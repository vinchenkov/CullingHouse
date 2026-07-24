package verbs

import (
	"os"
	"path/filepath"
	"testing"
)

// ipParents creates the two operator-owned mode-0700 precreate parents under ws.
func ipParents(t *testing.T, ws string) {
	t.Helper()
	for _, p := range []string{filepath.Join(ws, ".mission-control", "initiatives"), filepath.Join(ws, ".mc-worktrees")} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func ipGitWorkspace(t *testing.T) string {
	t.Helper()
	ws := grWorkspace(t)
	srcGit(t, ws, "init", "-q")
	srcGit(t, ws, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(ws, "README.md"), "hello\n")
	srcGit(t, ws, "add", "-A")
	srcGit(t, ws, "commit", "-qm", "c1")
	return ws
}

// A first cut: both children absent → a fresh Setup pinned to the target ref,
// two proven parents, no recovery roots.
func TestCaptureInitiativePrecreateFreshCutFromTargetRef(t *testing.T) {
	ws := ipGitWorkspace(t)
	ipParents(t, ws)

	step, err := captureInitiativePrecreate(ws, 7, os.Getuid(), "main")
	if err != nil {
		t.Fatalf("fresh capture: %v", err)
	}
	if step.InitiativeID != 7 || step.ChildMode != 0o700 || step.WorkspaceRoot != ws {
		t.Fatalf("step identity = %+v", step)
	}
	if step.Setup == nil || step.Setup.Mode != "fresh" || step.Setup.TargetRef != "main" || step.Setup.ObjectFormat != "sha1" {
		t.Fatalf("fresh setup = %+v, want fresh/main/sha1", step.Setup)
	}
	if step.RecoverStore != nil || step.RecoverWorktree != nil {
		t.Fatalf("fresh cut carries recovery roots: %+v %+v", step.RecoverStore, step.RecoverWorktree)
	}
	if step.StoreParent.Canonical != filepath.Join(ws, ".mission-control", "initiatives") ||
		!decimalIdentity.MatchString(step.StoreParent.Device) || !decimalIdentity.MatchString(step.StoreParent.Inode) ||
		step.StoreParent.OwnerUID != os.Getuid() {
		t.Fatalf("store parent evidence = %+v", step.StoreParent)
	}
	if step.WorktreeParent.Canonical != filepath.Join(ws, ".mc-worktrees") ||
		!decimalIdentity.MatchString(step.WorktreeParent.Device) || step.WorktreeParent.OwnerUID != os.Getuid() {
		t.Fatalf("worktree parent evidence = %+v", step.WorktreeParent)
	}
}

// A retry over on-disk residue: both children present → a retry Setup whose pins
// are re-derived from the landed store bytes (there is no spine assignment), plus
// the two recovery roots.
func TestCaptureInitiativePrecreateRetryFromResidue(t *testing.T) {
	src, base, objfmt := buildSourceRepo(t)
	ws := grWorkspace(t)
	ipParents(t, ws)
	uuid := "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	store := filepath.Join(ws, ".mission-control", "initiatives", "initiative-7")
	worktree := filepath.Join(ws, ".mc-worktrees", "initiative-7")
	for _, d := range []string{filepath.Join(store, "git"), filepath.Join(store, "source"), worktree} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	res, err := MaterializeInitiativeStore(src, store, worktree, InitiativeSetupSpec{
		InitiativeID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: objfmt, LocalRepoUUID: uuid,
	})
	if err != nil {
		t.Fatalf("seed materialize: %v", err)
	}
	// The store root carries the task-root discipline (0555); the worktree 0700.
	if err := os.Chmod(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store, 0o700) })

	step, err := captureInitiativePrecreate(ws, 7, os.Getuid(), "")
	if err != nil {
		t.Fatalf("retry capture: %v", err)
	}
	if step.Setup == nil || step.Setup.Mode != "retry" || step.Setup.ObjectFormat != objfmt ||
		step.Setup.PinnedBaseSHA != base || step.Setup.PinnedBaseSHA != res.BaseSHA ||
		step.Setup.PinnedClosureDigest != res.ClosureDigest || step.Setup.PinnedLocalRepoUUID != uuid {
		t.Fatalf("retry setup = %+v, want the residue-derived pins (base %s digest %s)", step.Setup, res.BaseSHA, res.ClosureDigest)
	}
	if step.RecoverStore == nil || step.RecoverStore.Canonical != store ||
		step.RecoverWorktree == nil || step.RecoverWorktree.Canonical != worktree {
		t.Fatalf("recovery roots = %+v / %+v, want the residue store/worktree", step.RecoverStore, step.RecoverWorktree)
	}
}

func TestCaptureInitiativePrecreateRefusalTable(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(t *testing.T, ws string)
	}{
		{"partial: only the store child exists", func(t *testing.T, ws string) {
			if err := os.MkdirAll(filepath.Join(ws, ".mission-control", "initiatives", "initiative-7"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"store parent is not mode 0700", func(t *testing.T, ws string) {
			if err := os.Chmod(filepath.Join(ws, ".mission-control", "initiatives"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"worktree parent absent", func(t *testing.T, ws string) {
			if err := os.RemoveAll(filepath.Join(ws, ".mc-worktrees")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := ipGitWorkspace(t)
			ipParents(t, ws)
			tc.break_(t, ws)
			if _, err := captureInitiativePrecreate(ws, 7, os.Getuid(), "main"); err == nil {
				t.Fatalf("%s: capture unexpectedly succeeded", tc.name)
			}
		})
	}
}
