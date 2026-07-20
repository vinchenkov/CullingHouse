package verbs

import (
	"os"
	"path/filepath"
	"testing"
)

// Step 4c: the reviewed path set, and the dirty fence SCOPED to it.
//
// The scoping is the whole point and it is not a convenience. A global dirty
// check would refuse an operator who merely has unrelated work in progress,
// which makes landing usable only against a pristine tree; legacy permits
// unrelated dirt explicitly and so must this. But the paths the merge will
// actually write must be pristine, because those are the ones where operator
// bytes would be destroyed without a trace.

// landingPair builds a real operator repo plus a sealed task store cut from it,
// wired so the sealed branch is a genuine descendant of the target's base. That
// shared history is what makes merge-base meaningful; a fixture without it
// would let every fence below pass for the wrong reason.
func landingPair(t *testing.T) (repo string, pins landingStorePins, base string) {
	t.Helper()
	repo = t.TempDir()
	srcGit(t, repo, "init", "-q", "-b", "main")
	srcGit(t, repo, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(repo, "README.md"), "operator\n")
	writeFile(t, filepath.Join(repo, "keep.txt"), "untouched\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "base")
	base = srcGit(t, repo, "rev-parse", "HEAD")

	// The reviewed change: touches README.md and adds lib/new.go.
	srcGit(t, repo, "checkout", "-q", "-b", "mc/task-7")
	if err := os.MkdirAll(filepath.Join(repo, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "README.md"), "operator\nreviewed\n")
	writeFile(t, filepath.Join(repo, "lib/new.go"), "package lib\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "reviewed change")
	verified := srcGit(t, repo, "rev-parse", "HEAD")
	srcGit(t, repo, "checkout", "-q", "main")
	srcGit(t, repo, "branch", "-D", "mc/task-7")

	format := srcGit(t, repo, "rev-parse", "--show-object-format")
	return repo, landingStorePins{
		TaskID: 7, ObjectFormat: format, VerifiedSHA: verified,
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	}, base
}

func TestReviewedPathsAreTheDiffFromTheFrozenBase(t *testing.T) {
	repo, pins, base := landingPair(t)
	paths, err := reviewedLandingPaths(landingGit{dir: repo}, base, pins.VerifiedSHA)
	if err != nil {
		t.Fatalf("reviewed paths: %v", err)
	}
	want := map[string]bool{"README.md": true, "lib/new.go": true}
	if len(paths) != len(want) {
		t.Fatalf("reviewed paths = %v, want %v", paths, want)
	}
	for _, p := range paths {
		if !want[p] {
			t.Fatalf("unexpected reviewed path %q (got %v)", p, paths)
		}
	}
}

// The merge base must be unique AND must be the base the assignment froze.
// Uniqueness alone is legacy's check; binding it to the frozen base is what
// proves the target has not been rewritten out from under the review.
func TestLandingMergeBaseMustBeUniqueAndTheFrozenBase(t *testing.T) {
	repo, pins, base := landingPair(t)
	g := landingGit{dir: repo}
	got, err := landingMergeBase(g, "main", pins.VerifiedSHA)
	if err != nil {
		t.Fatalf("merge base: %v", err)
	}
	if got != base {
		t.Fatalf("merge base = %q, want the frozen base %q", got, base)
	}
	// The target advancing does NOT change the merge base: the reviewed branch
	// reaches main only through that base. This is why an advanced target is
	// landable and a rewritten one is not.
	writeFile(t, filepath.Join(repo, "operator-later.txt"), "later\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "operator moved on")
	after, err := landingMergeBase(g, "main", pins.VerifiedSHA)
	if err != nil {
		t.Fatalf("merge base after the target advanced: %v", err)
	}
	if after != base {
		t.Fatalf("merge base moved to %q when the target advanced; want %q", after, base)
	}
}

// A criss-cross history has TWO merge bases, so "what the merge writes" is not
// a single well-defined diff and the reviewed path set the dirty fence depends
// on would be a guess. Built explicitly because nothing else in the suite
// produces one — the uniqueness check was vacuous until this existed.
//
//	base -- B ---- M1 (main)     M1 merges C into B
//	     \     \ /
//	      \     X
//	       \   / \
//	        -- C -- M2 (reviewed) M2 merges B into C
func TestLandingRefusesACrissCrossHistoryWithTwoMergeBases(t *testing.T) {
	repo := t.TempDir()
	srcGit(t, repo, "init", "-q", "-b", "main")
	srcGit(t, repo, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(repo, "root.txt"), "root\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "root")

	srcGit(t, repo, "checkout", "-q", "-b", "sideB")
	writeFile(t, filepath.Join(repo, "b.txt"), "b\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "B")
	b := srcGit(t, repo, "rev-parse", "HEAD")

	srcGit(t, repo, "checkout", "-q", "-b", "sideC", "main")
	writeFile(t, filepath.Join(repo, "c.txt"), "c\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "C")
	c := srcGit(t, repo, "rev-parse", "HEAD")

	// main = merge C into B; reviewed = merge B into C. Neither B nor C is an
	// ancestor of the other, so both are merge bases of the two merges.
	srcGit(t, repo, "checkout", "-q", "main")
	srcGit(t, repo, "merge", "-q", "--no-ff", "-m", "M1", b)
	srcGit(t, repo, "merge", "-q", "--no-ff", "-m", "M1c", c)
	srcGit(t, repo, "checkout", "-q", "sideC")
	srcGit(t, repo, "merge", "-q", "--no-ff", "-m", "M2", b)
	reviewed := srcGit(t, repo, "rev-parse", "HEAD")
	srcGit(t, repo, "checkout", "-q", "main")

	bases := srcGit(t, repo, "merge-base", "--all", "refs/heads/main", reviewed)
	if len(splitFields(bases)) < 2 {
		t.Skipf("fixture did not produce a criss-cross (bases: %q); the uniqueness fence needs one", bases)
	}
	if _, err := landingMergeBase(landingGit{dir: repo}, "main", reviewed); err == nil {
		t.Fatal("accepted a criss-cross history with more than one merge base")
	}
}

func splitFields(s string) []string {
	var out []string
	for _, f := range sortedLines(s) {
		out = append(out, f)
	}
	return out
}

func TestLandingRefusesAnUnrelatedHistory(t *testing.T) {
	repo, pins, _ := landingPair(t)
	// An orphan commit shares no history with main, so there is no merge base.
	srcGit(t, repo, "checkout", "-q", "--orphan", "unrelated")
	writeFile(t, filepath.Join(repo, "other.txt"), "other\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "unrelated root")
	orphan := srcGit(t, repo, "rev-parse", "HEAD")
	srcGit(t, repo, "checkout", "-q", "main")
	if _, err := landingMergeBase(landingGit{dir: repo}, "main", orphan); err == nil {
		t.Fatal("accepted a reviewed commit sharing no history with the target")
	}
	_ = pins
}

// The fence permits unrelated dirt and refuses dirt at reviewed paths. Both
// directions are load-bearing: over-refusing makes landing unusable, and
// under-refusing destroys operator bytes.
func TestLandingDirtyFenceIsScopedToReviewedPaths(t *testing.T) {
	t.Run("unrelated dirty and untracked state is permitted", func(t *testing.T) {
		repo, pins, base := landingPair(t)
		writeFile(t, filepath.Join(repo, "keep.txt"), "operator work in progress\n")
		writeFile(t, filepath.Join(repo, "scratch.txt"), "untracked scratch\n")
		if err := fenceReviewedPathsClean(landingGit{dir: repo}, repo, "main", base, pins.VerifiedSHA); err != nil {
			t.Fatalf("unrelated dirt blocked landing: %v", err)
		}
	})
	t.Run("a modified tracked reviewed path is refused", func(t *testing.T) {
		repo, pins, base := landingPair(t)
		writeFile(t, filepath.Join(repo, "README.md"), "operator\nlocal edit\n")
		if err := fenceReviewedPathsClean(landingGit{dir: repo}, repo, "main", base, pins.VerifiedSHA); err == nil {
			t.Fatal("landing accepted a locally modified reviewed path")
		}
	})
	t.Run("a staged-but-uncommitted reviewed path is refused", func(t *testing.T) {
		repo, pins, base := landingPair(t)
		writeFile(t, filepath.Join(repo, "README.md"), "operator\nstaged edit\n")
		srcGit(t, repo, "add", "README.md")
		if err := fenceReviewedPathsClean(landingGit{dir: repo}, repo, "main", base, pins.VerifiedSHA); err == nil {
			t.Fatal("landing accepted a staged reviewed path")
		}
	})
}

// The sharpest case legacy pinned: a path the merge will CREATE already exists
// on disk untracked. Git would overwrite it without ever reporting it as
// modified, because it is not tracked. An IGNORED file is the same hazard and
// the more likely one — a .env sitting where a reviewed file is about to land.
func TestLandingRefusesUntrackedCollisionAtACreatedPath(t *testing.T) {
	for _, ignored := range []bool{false, true} {
		name := "untracked"
		if ignored {
			name = "ignored"
		}
		t.Run(name+" file where a reviewed path will be created", func(t *testing.T) {
			repo, pins, base := landingPair(t)
			if err := os.MkdirAll(filepath.Join(repo, "lib"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(repo, "lib/new.go"), "operator's own file\n")
			if ignored {
				writeFile(t, filepath.Join(repo, ".git", "info", "exclude"), "lib/new.go\n")
			}
			err := fenceReviewedPathsClean(landingGit{dir: repo}, repo, "main", base, pins.VerifiedSHA)
			if err == nil {
				t.Fatalf("landing accepted an %s collision at a path the merge creates", name)
			}
			// The operator's bytes must still be there — the fence refuses, it
			// never "resolves" a collision by moving anything.
			body, rerr := os.ReadFile(filepath.Join(repo, "lib/new.go"))
			if rerr != nil || string(body) != "operator's own file\n" {
				t.Fatalf("operator bytes were disturbed: %q %v", string(body), rerr)
			}
		})
	}
}

// An ancestor component occupied by a FILE where the merge needs a DIRECTORY.
// git cannot create lib/ when lib is a regular file, and this fails in the
// middle of a merge rather than before it.
func TestLandingRefusesAnAncestorComponentOccupiedByAFile(t *testing.T) {
	repo, pins, base := landingPair(t)
	writeFile(t, filepath.Join(repo, "lib"), "not a directory\n")
	if err := fenceReviewedPathsClean(landingGit{dir: repo}, repo, "main", base, pins.VerifiedSHA); err == nil {
		t.Fatal("landing accepted an ancestor component occupied by a file")
	}
}

// A same-length edit with the mtime restored, in a repository that has set
// core.checkStat=minimal and core.trustctime=false to hide exactly that. The
// fence must see it.
//
// An earlier version of this comment claimed the fixed config's checkStat and
// trustctime overrides are what defeat the evasion. MEASURED, and they are
// not: git 2.50 detects this edit through `git diff --name-only HEAD` even with
// the hostile repo config in force and our overrides removed. The property
// under test is real and worth pinning — hostile stat config does not hide a
// reviewed-path edit — but it holds because of how this command works, not
// because of those two flags. See landingFixedConfig, where they are retained
// for the merge stage and labelled as not load-bearing here.
func TestLandingDirtyFenceSeesAStatCacheHiddenEdit(t *testing.T) {
	repo, pins, base := landingPair(t)
	srcGit(t, repo, "config", "core.trustctime", "false")
	srcGit(t, repo, "config", "core.checkStat", "minimal")
	// Same length as "operator\nreviewed\n" is not required; what matters is
	// that the repo config would otherwise let git skip the content check.
	info, err := os.Stat(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "README.md"), "OPERATOR\n")
	// Restore the original timestamps so only content differs.
	if err := os.Chtimes(filepath.Join(repo, "README.md"), info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := fenceReviewedPathsClean(landingGit{dir: repo}, repo, "main", base, pins.VerifiedSHA); err == nil {
		t.Fatal("a stat-cache-hidden edit at a reviewed path passed the dirty fence")
	}
}
