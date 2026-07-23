package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mc/boundary"
)

// isBuild creates the exact ADR-025 D1/D2 initiative skeleton for initiative id
// 7 under a fresh workspace root and returns (workspaceRoot, storeRoot,
// worktreeRoot). The store root is left mode 0555 with a cleanup chmod, exactly
// as the standalone task root is.
func isBuild(t *testing.T) (string, string, string) {
	t.Helper()
	ws := grWorkspace(t)
	store, wt := isBuildAt(t, ws)
	return ws, store, wt
}

func isBuildAt(t *testing.T, ws string) (string, string) {
	t.Helper()
	store := filepath.Join(ws, ".mission-control", "initiatives", "initiative-7")
	wt := filepath.Join(ws, ".mc-worktrees", "initiative-7")
	for _, dir := range []string{
		"source",
		"git/hooks", "git/info", "git/objects/info", "git/objects/pack",
		"git/worktrees/mc-initiative-7",
	} {
		if err := os.MkdirAll(filepath.Join(store, filepath.FromSlash(dir)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(wt, ".mission-control"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"git/config":      string(generatedTaskGitConfig("sha1", "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9")),
		"git/packed-refs": "",
		"git/shallow":     "",
		"git/worktrees/mc-initiative-7/commondir":       "../..\n",
		"git/worktrees/mc-initiative-7/gitdir":          "../../../source/.git\n",
		"git/worktrees/mc-initiative-7/config.worktree": "",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(store, filepath.FromSlash(rel)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The shared worktree carries the container-relative pointer bytes verbatim
	// (ADR-025 D1): they resolve in the container, not on the host.
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../git/worktrees/mc-initiative-7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store, 0o700) })
	if err := os.Chmod(store, 0o555); err != nil {
		t.Fatal(err)
	}
	return store, wt
}

// The initiative table is the standalone-task table with the worktree name
// substituted: same 15 destinations, same kinds, same access, same pointer
// bytes modulo the name (ADR-025 D2 — "no destination is added").
func TestInitiativePlanRowsMirrorTheStandaloneTable(t *testing.T) {
	task := taskPlanRows(7)
	init := initiativePlanRows(7)
	if len(init) != len(task) {
		t.Fatalf("initiative table has %d rows, the standalone table has %d", len(init), len(task))
	}
	for i := range task {
		want, got := task[i], init[i].taskPlanRow
		if got.Kind != want.Kind {
			t.Errorf("row %d kind = %v, want %v", i, got.Kind, want.Kind)
		}
		wantDest := strings.Replace(want.Dest, "mc-task-7", "mc-initiative-7", 1)
		if got.Dest != wantDest {
			t.Errorf("row %d destination = %q, want %q", i, got.Dest, wantDest)
		}
		if got.Access != want.Access {
			t.Errorf("row %d (%s) access = %v, want %v", i, got.Dest, got.Access, want.Access)
		}
		if got.IsDir != want.IsDir || got.MustBeEmptyDir != want.MustBeEmptyDir || got.ConfigGrammar != want.ConfigGrammar {
			t.Errorf("row %d (%s) shape discipline differs from the standalone row", i, got.Dest)
		}
		wantBytes := strings.Replace(string(want.WantBytes), "mc-task-7", "mc-initiative-7", 1)
		if (want.WantBytes == nil) != (got.WantBytes == nil) || string(got.WantBytes) != wantBytes {
			t.Errorf("row %d (%s) pinned bytes = %q, want %q", i, got.Dest, got.WantBytes, wantBytes)
		}
		if !validTaskPlanDestination(got.Dest) {
			t.Errorf("row %d destination %q is not a table cell", i, got.Dest)
		}
	}
}

// The shared worktree is the ONE row family whose host base is not the bound
// /workspace root (ADR-025 D2): source and its two covers resolve against the
// worktree, everything else against the store.
func TestInitiativePlanRowsSplitTheTwoHostBases(t *testing.T) {
	want := map[string]planBase{
		"/workspace":                                      baseStoreRoot,
		"/workspace/source":                               baseSharedWorktree,
		"/workspace/source/.git":                          baseSharedWorktree,
		"/workspace/source/.mission-control":              baseSharedWorktree,
		"/workspace/git":                                  baseStoreRoot,
		"/workspace/git/objects/pack":                     baseStoreRoot,
		"/workspace/git/worktrees/mc-initiative-7/gitdir": baseStoreRoot,
	}
	got := map[string]planBase{}
	for _, row := range initiativePlanRows(7) {
		got[row.Dest] = row.Base
	}
	for dest, base := range want {
		if got[dest] != base {
			t.Errorf("row %q base = %v, want %v", dest, got[dest], base)
		}
	}
	// The worktree-based rows are relative to the worktree root itself.
	for _, row := range initiativePlanRows(7) {
		if row.Base != baseSharedWorktree {
			continue
		}
		switch row.Dest {
		case "/workspace/source":
			if row.Rel != "" {
				t.Errorf("the source row must bind the worktree root itself, got rel %q", row.Rel)
			}
		case "/workspace/source/.git":
			if row.Rel != ".git" {
				t.Errorf("the git pointer row rel = %q, want %q", row.Rel, ".git")
			}
		case "/workspace/source/.mission-control":
			if row.Rel != ".mission-control" {
				t.Errorf("the mission-control cover rel = %q, want %q", row.Rel, ".mission-control")
			}
		default:
			t.Errorf("unexpected worktree-based row %q", row.Dest)
		}
	}
}

func TestResolveInitiativeSkeletonResolvesEveryRow(t *testing.T) {
	ws, store, wt := isBuild(t)
	roots, err := resolveInitiativeSkeleton(ws, 7, os.Getuid())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(roots) != 15 {
		t.Fatalf("want 15 typed roots, got %d", len(roots))
	}
	if got := roots[boundary.KindTaskRoot].Canonical; got != store {
		t.Fatalf("store root canonical = %q, want %q", got, store)
	}
	if got := roots[boundary.KindTaskSource].Canonical; got != wt {
		t.Fatalf("source canonical = %q, want the shared worktree %q", got, wt)
	}
	if got := roots[boundary.KindWorkspaceSourceGitCover].Canonical; got != filepath.Join(wt, ".git") {
		t.Fatalf("git pointer canonical = %q, want it inside the shared worktree", got)
	}
	for kind, id := range roots {
		if !id.Present() {
			t.Fatalf("typed root %v resolved absent", kind)
		}
	}
}

func TestResolveInitiativeSkeletonRefusalTable(t *testing.T) {
	cases := []struct {
		name    string
		break_  func(t *testing.T, store, wt string)
		wantErr string
	}{
		{"absent shared worktree", func(t *testing.T, store, wt string) {
			if err := os.RemoveAll(wt); err != nil {
				t.Fatal(err)
			}
		}, boundary.CodeSourceMissing},
		{"absent store git", func(t *testing.T, store, wt string) {
			// The store root is 0555, so the removal needs a temporary chmod;
			// restore it or the root's own mode pin fires first.
			_ = os.Chmod(store, 0o700)
			if err := os.RemoveAll(filepath.Join(store, "git")); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(store, 0o555); err != nil {
				t.Fatal(err)
			}
		}, boundary.CodeSourceMissing},
		{"symlinked shared worktree", func(t *testing.T, store, wt string) {
			if err := os.RemoveAll(wt); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(store, wt); err != nil {
				t.Fatal(err)
			}
		}, boundary.CodeSourceWrongKind},
		{"worktree pointer carries foreign bytes", func(t *testing.T, store, wt string) {
			if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /elsewhere\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, boundary.CodeRuntimeUnappliable},
		{"store root is not mode 0555", func(t *testing.T, store, wt string) {
			if err := os.Chmod(store, 0o755); err != nil {
				t.Fatal(err)
			}
		}, boundary.CodeRuntimeUnappliable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, store, wt := isBuild(t)
			tc.break_(t, store, wt)
			_, err := resolveInitiativeSkeleton(ws, 7, os.Getuid())
			if code := grMountCode(t, err); code != tc.wantErr {
				t.Fatalf("want %q, got %q (%v)", tc.wantErr, code, err)
			}
		})
	}
}

func TestResolveInitiativeSkeletonRefusesNonCanonicalID(t *testing.T) {
	ws, _, _ := isBuild(t)
	for _, id := range []int64{0, -1} {
		if _, err := resolveInitiativeSkeleton(ws, id, os.Getuid()); err == nil {
			t.Fatalf("initiative id %d resolved", id)
		}
	}
}
