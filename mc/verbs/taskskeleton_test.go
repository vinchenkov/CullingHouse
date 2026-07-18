package verbs

import (
	"os"
	"path/filepath"
	"testing"

	"mc/boundary"
)

// tsBuild creates the exact ADR-017 D6 standalone-task skeleton for task id 7
// under a fresh workspace root and returns (workspaceRoot, taskRoot). The
// task root is left mode 0555 (ADR-017:418) with a cleanup chmod so the temp
// tree can be removed.
func tsBuild(t *testing.T) (string, string) {
	t.Helper()
	ws := grWorkspace(t)
	return ws, tsBuildAt(t, ws)
}

// tsBuildAt builds the same skeleton beneath an existing workspace root, so a
// test can rebuild it in place at the same path with a fresh identity.
func tsBuildAt(t *testing.T, ws string) string {
	t.Helper()
	root := filepath.Join(ws, ".mission-control", "tasks", "task-7")
	for _, dir := range []string{
		"source/.mission-control",
		"git/hooks", "git/info", "git/objects/info", "git/objects/pack",
		"git/worktrees/mc-task-7",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"source/.git":                             "gitdir: ../git/worktrees/mc-task-7\n",
		"git/config":                              "",
		"git/packed-refs":                         "",
		"git/shallow":                             "",
		"git/worktrees/mc-task-7/commondir":       "../..\n",
		"git/worktrees/mc-task-7/gitdir":          "../../../source/.git\n",
		"git/worktrees/mc-task-7/config.worktree": "",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestTaskPlanRowsAreAClosedCollisionFreeTable(t *testing.T) {
	rows := taskPlanRows(7)
	if len(rows) != 15 {
		t.Fatalf("the D6 standalone-task table has 15 host-bind rows, got %d", len(rows))
	}
	kinds := map[boundary.TypedKind]bool{}
	dests := map[string]bool{}
	for _, row := range rows {
		if kinds[row.Kind] {
			t.Fatalf("kind %v appears twice", row.Kind)
		}
		kinds[row.Kind] = true
		if dests[row.Dest] {
			t.Fatalf("destination %q appears twice", row.Dest)
		}
		dests[row.Dest] = true
		if row.Dest != "/workspace" && !filepath.IsAbs(row.Dest) {
			t.Fatalf("destination %q is not absolute", row.Dest)
		}
	}
	for _, want := range []string{
		"/workspace", "/workspace/source", "/workspace/git",
		"/workspace/source/.git", "/workspace/git/objects/pack",
		"/workspace/git/worktrees/mc-task-7/commondir",
	} {
		if !dests[want] {
			t.Fatalf("table misses destination %q", want)
		}
	}
}

func TestResolveTaskLocalSkeletonResolvesEveryRow(t *testing.T) {
	ws, root := tsBuild(t)
	roots, err := resolveTaskLocalSkeleton(ws, 7, os.Getuid())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(roots) != 15 {
		t.Fatalf("want 15 typed roots, got %d", len(roots))
	}
	if got := roots[boundary.KindTaskRoot].Canonical; got != root {
		t.Fatalf("task root canonical = %q, want %q", got, root)
	}
	if got := roots[boundary.KindTaskSource].Canonical; got != filepath.Join(root, "source") {
		t.Fatalf("task source canonical = %q", got)
	}
	for kind, id := range roots {
		if !id.Present() {
			t.Fatalf("typed root %v resolved absent", kind)
		}
	}
}

func TestResolveTaskLocalSkeletonRefusalTable(t *testing.T) {
	cases := []struct {
		name string
		muta func(t *testing.T, ws, root string)
		code string
	}{
		{"absent-root", func(t *testing.T, ws, root string) {
			_ = os.Chmod(root, 0o700)
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
		}, boundary.CodeSourceMissing},
		{"missing-source", func(t *testing.T, ws, root string) {
			_ = os.Chmod(root, 0o700)
			if err := os.RemoveAll(filepath.Join(root, "source")); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(root, 0o555)
		}, boundary.CodeSourceMissing},
		{"root-not-0555", func(t *testing.T, ws, root string) {
			if err := os.Chmod(root, 0o755); err != nil {
				t.Fatal(err)
			}
		}, boundary.CodeRuntimeUnappliable},
		{"config-cover-is-a-dir", func(t *testing.T, ws, root string) {
			_ = os.Chmod(root, 0o700)
			path := filepath.Join(root, "git", "config")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(root, 0o555)
		}, boundary.CodeSourceWrongKind},
		{"hooks-not-empty", func(t *testing.T, ws, root string) {
			_ = os.Chmod(root, 0o700)
			if err := os.WriteFile(filepath.Join(root, "git", "hooks", "pre-commit"), []byte("#!/bin/sh\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(root, 0o555)
		}, boundary.CodeRuntimeUnappliable},
		{"config-not-empty", func(t *testing.T, ws, root string) {
			_ = os.Chmod(root, 0o700)
			if err := os.WriteFile(filepath.Join(root, "git", "config"), []byte("[core]\n\thooksPath = /tmp\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(root, 0o555)
		}, boundary.CodeRuntimeUnappliable},
		{"wrong-pointer-content", func(t *testing.T, ws, root string) {
			_ = os.Chmod(root, 0o700)
			if err := os.WriteFile(filepath.Join(root, "source", ".git"), []byte("gitdir: ../../.git\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(root, 0o555)
		}, boundary.CodeRuntimeUnappliable},
		{"symlinked-source", func(t *testing.T, ws, root string) {
			_ = os.Chmod(root, 0o700)
			real := filepath.Join(ws, "elsewhere")
			if err := os.Mkdir(real, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(filepath.Join(root, "source")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, filepath.Join(root, "source")); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(root, 0o555)
		}, boundary.CodeSourceWrongKind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, root := tsBuild(t)
			tc.muta(t, ws, root)
			_, err := resolveTaskLocalSkeleton(ws, 7, os.Getuid())
			if code := grMountCode(t, err); code != tc.code {
				t.Fatalf("want %q, got %q (%v)", tc.code, code, err)
			}
		})
	}
}

func TestResolveTaskLocalSkeletonRefusesForeignOwner(t *testing.T) {
	ws, _ := tsBuild(t)
	_, err := resolveTaskLocalSkeleton(ws, 7, os.Getuid()+1)
	if code := grMountCode(t, err); code != boundary.CodeRuntimeUnappliable {
		t.Fatalf("a skeleton not owned by the operator must refuse, got %q", code)
	}
}

func TestResolveTaskLocalSkeletonRefusesNonCanonicalTaskID(t *testing.T) {
	ws, _ := tsBuild(t)
	for _, id := range []int64{0, -3} {
		if _, err := resolveTaskLocalSkeleton(ws, id, os.Getuid()); err == nil {
			t.Fatalf("task id %d must refuse, not resolve", id)
		}
	}
}
