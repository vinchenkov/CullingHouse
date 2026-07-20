package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Step 4a: the fenced git wrapper every sealed-landing git call runs through,
// and the real-repository revalidation stage (ADR-017:741-743's "safe local
// controls, real repository/target identity").
//
// The wrapper exists because of a concrete finding about the legacy lander: it
// isolates its MERGE carefully but runs a long tail of bare `git` calls with
// the operator's live config and hooks in scope (the receipt scan, symbolic-ref,
// merge-base, diff-tree, worktree list...). Those are read-only plumbing so no
// hook fires today, which makes the isolation accidental rather than
// structural. ADR-017:704-711's "cleared environment plus generated safe Git
// configuration" reads as a whole-program property. Here it is one.

// buildLandingRepo builds a real operator repository with a `main` branch and
// two commits. It is the RW landing anchor — the real thing, primary checkout
// included, which is what makes landing the strongest grant in the system.
func buildLandingRepo(t *testing.T) (dir, targetSHA string) {
	t.Helper()
	dir = t.TempDir()
	srcGit(t, dir, "init", "-q", "-b", "main")
	srcGit(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(dir, "README.md"), "operator\n")
	srcGit(t, dir, "add", "-A")
	srcGit(t, dir, "commit", "-qm", "operator c1")
	writeFile(t, filepath.Join(dir, "app.txt"), "v1\n")
	srcGit(t, dir, "add", "-A")
	srcGit(t, dir, "commit", "-qm", "operator c2")
	return dir, srcGit(t, dir, "rev-parse", "HEAD")
}

// The env is CONSTRUCTED, never inherited. That single property is what makes
// hostile GIT_* in the caller's environment structurally unable to reach git —
// author/committer identity forging, GIT_DIR redirection, index redirection.
// Legacy got the author/committer half by overriding those four vars at the
// merge; building the environment from scratch gets all of it, everywhere.
func TestLandingGitEnvIsConstructedNotInherited(t *testing.T) {
	for _, hostile := range []string{
		"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_COUNT",
		"GIT_EXEC_PATH", "GIT_ATTR_NOSYSTEM", "GIT_REPLACE_REF_BASE",
	} {
		t.Setenv(hostile, "hostile-value")
	}
	env := landingGitEnv()
	for _, entry := range env {
		if strings.Contains(entry, "hostile-value") {
			t.Fatalf("the landing git environment inherited %q", entry)
		}
	}
	// And the fences it must positively assert.
	want := []string{
		"GIT_NO_REPLACE_OBJECTS=1", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	}
	for _, w := range want {
		found := false
		for _, entry := range env {
			if entry == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("the landing git environment does not set %s", w)
		}
	}
}

// A repository hook must never run. This is behavioral, not a config read: the
// hook below would create a file, so its absence is proof it did not fire.
func TestLandingGitNeverRunsARepositoryHook(t *testing.T) {
	dir, _ := buildLandingRepo(t)
	hooks := filepath.Join(dir, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "hook-fired")
	hook := "#!/bin/sh\ntouch " + marker + "\n"
	for _, name := range []string{"post-checkout", "reference-transaction", "post-index-change"} {
		path := filepath.Join(hooks, name)
		writeFile(t, path, hook)
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	g := landingGit{dir: dir}
	if _, err := g.run("status", "--porcelain"); err != nil {
		t.Fatalf("fenced status: %v", err)
	}
	if _, err := g.run("rev-parse", "HEAD"); err != nil {
		t.Fatalf("fenced rev-parse: %v", err)
	}
	if _, err := os.Lstat(marker); err == nil {
		t.Fatal("a repository hook fired through the fenced landing wrapper")
	}
}

// GIT_NO_REPLACE_OBJECTS: a refs/replace entry must not be able to substitute
// different bytes under the lander's feet. Legacy pinned this and it carries.
func TestLandingGitIgnoresReplacementObjects(t *testing.T) {
	dir, head := buildLandingRepo(t)
	// Rewrite the tip's message to get a second, different commit, then install
	// it as a replacement for the real one.
	other := srcGit(t, dir, "commit-tree", head+"^{tree}", "-p", head+"^", "-m", "replacement")
	srcGit(t, dir, "replace", "-f", head, other)
	// Unfenced git honors the replacement...
	if got := srcGit(t, dir, "log", "-1", "--format=%s", head); got != "replacement" {
		t.Fatalf("fixture did not install a replacement (unfenced git said %q)", got)
	}
	// ...the fenced wrapper must not.
	out, err := landingGit{dir: dir}.run("log", "-1", "--format=%s", head)
	if err != nil {
		t.Fatalf("fenced log: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got == "replacement" {
		t.Fatal("the fenced landing wrapper honored a refs/replace substitution")
	}
}

func TestRevalidateLandingRepositoryAcceptsTheRealRepository(t *testing.T) {
	dir, head := buildLandingRepo(t)
	facts, err := revalidateLandingRepository(dir, "main")
	if err != nil {
		t.Fatalf("the real operator repository was refused: %v", err)
	}
	if facts.TargetSHA != head {
		t.Fatalf("target tip = %q, want %q", facts.TargetSHA, head)
	}
	// Toplevel is the SYMLINK-RESOLVED path, deliberately: on macOS t.TempDir()
	// hands back /var/... which is a symlink to /private/var/..., and the same
	// aliasing is exactly what a bind mount can produce in production. Later
	// stages compare paths, so they must be handed the resolved form.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Toplevel != resolved {
		t.Fatalf("toplevel = %q, want the resolved %q", facts.Toplevel, resolved)
	}
}

func TestRevalidateLandingRepositoryFences(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string) (workspace, target string){
		"configured core.worktree redirects the checkout": func(t *testing.T, dir string) (string, string) {
			other := t.TempDir()
			srcGit(t, dir, "config", "core.worktree", other)
			return dir, "main"
		},
		"bare repository has no primary checkout to merge in": func(t *testing.T, dir string) (string, string) {
			bare := t.TempDir()
			srcGit(t, bare, "init", "-q", "--bare")
			return bare, "main"
		},
		"target branch does not exist": func(t *testing.T, dir string) (string, string) {
			return dir, "nonexistent"
		},
		// A symbolic target IS refused — but by the HEAD-on-target fence, not by
		// the symbolic-ref fence that appears to own this case. Git resolves
		// symref chains transitively, so `symbolic-ref --short HEAD` reports the
		// name at the end of the chain and can never equal a symbolic target's
		// own name. Two drafts of this case were mutation-tested and both left
		// the suite green with the symbolic fence disabled. The case is kept
		// (the outcome it asserts is the one that matters) and labelled for what
		// it actually exercises; see the fence's comment in landsealed.go.
		"target is a symbolic ref [refused by the HEAD fence]": func(t *testing.T, dir string) (string, string) {
			srcGit(t, dir, "branch", "operator-feature")
			srcGit(t, dir, "symbolic-ref", "refs/heads/aliased", "refs/heads/operator-feature")
			srcGit(t, dir, "symbolic-ref", "HEAD", "refs/heads/aliased")
			return dir, "aliased"
		},
		"HEAD is not on the target branch": func(t *testing.T, dir string) (string, string) {
			srcGit(t, dir, "checkout", "-q", "-b", "elsewhere")
			return dir, "main"
		},
		"HEAD is detached": func(t *testing.T, dir string) (string, string) {
			srcGit(t, dir, "checkout", "-q", "--detach")
			return dir, "main"
		},
		"an executable merge driver is configured": func(t *testing.T, dir string) (string, string) {
			srcGit(t, dir, "config", "merge.evil.driver", "sh -c 'rm -rf /'")
			return dir, "main"
		},
		"a content filter is configured": func(t *testing.T, dir string) (string, string) {
			srcGit(t, dir, "config", "filter.evil.clean", "sh -c 'cat /etc/passwd'")
			return dir, "main"
		},
		"a filter process is configured": func(t *testing.T, dir string) (string, string) {
			srcGit(t, dir, "config", "filter.evil.process", "sh -c 'true'")
			return dir, "main"
		},
		"the index hides a path with --assume-unchanged": func(t *testing.T, dir string) (string, string) {
			srcGit(t, dir, "update-index", "--assume-unchanged", "app.txt")
			return dir, "main"
		},
		"the index hides a path with --skip-worktree": func(t *testing.T, dir string) (string, string) {
			srcGit(t, dir, "update-index", "--skip-worktree", "app.txt")
			return dir, "main"
		},
		"an operator merge is already in progress": func(t *testing.T, dir string) (string, string) {
			writeFile(t, filepath.Join(dir, ".git", "MERGE_HEAD"), srcGit(t, dir, "rev-parse", "HEAD")+"\n")
			return dir, "main"
		},
		"the workspace is not a git repository at all": func(t *testing.T, dir string) (string, string) {
			return t.TempDir(), "main"
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := buildLandingRepo(t)
			workspace, target := setup(t, dir)
			if _, err := revalidateLandingRepository(workspace, target); err == nil {
				t.Fatalf("accepted a repository where %s", name)
			}
		})
	}
}

// The workspace must be the repository's own toplevel, not a subdirectory of
// it. A subdirectory would still resolve to a valid repository, so this fence
// is what ties the RW grant to the exact registered Worksource root rather than
// to "somewhere inside it".
func TestRevalidateLandingRepositoryRefusesASubdirectoryOfTheRepository(t *testing.T) {
	dir, _ := buildLandingRepo(t)
	sub := filepath.Join(dir, "lib")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := revalidateLandingRepository(sub, "main"); err == nil {
		t.Fatal("a subdirectory of the repository was accepted as the landing anchor")
	}
}

// Unrelated dirty state is NOT this stage's business. The dirty fence is
// path-scoped to the reviewed set (legacy proved that distinction matters: an
// operator with unrelated work in progress must still be able to land), and the
// reviewed set is not known until the closure stage. Pinned so a later slice
// does not "helpfully" add a global dirty check here.
func TestRevalidateLandingRepositoryIgnoresUnrelatedDirtyState(t *testing.T) {
	dir, _ := buildLandingRepo(t)
	writeFile(t, filepath.Join(dir, "app.txt"), "operator work in progress\n")
	writeFile(t, filepath.Join(dir, "untracked.txt"), "scratch\n")
	if _, err := revalidateLandingRepository(dir, "main"); err != nil {
		t.Fatalf("unrelated dirty state blocked the repository stage: %v", err)
	}
}
