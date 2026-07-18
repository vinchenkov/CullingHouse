package verbs

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// srcGit runs a git command in dir and fails the test on error. Test-only: the
// production extractor drives git through the same os/exec seam under the
// setup container's pinned git.
func srcGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// buildSourceRepo builds a small source repo with a subdir, an executable, and
// a symlink over two commits, then plants UNREACHABLE objects (a dangling blob
// and a stash) that must never enter the extracted closure. Returns the repo
// dir, the base commit OID, and the object format.
func buildSourceRepo(t *testing.T) (dir, baseSHA, objectFormat string) {
	t.Helper()
	dir = t.TempDir()
	srcGit(t, dir, "init", "-q")
	srcGit(t, dir, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "README.md"), "hello world\n")
	writeFile(t, filepath.Join(dir, "lib/core.go"), "package lib\n")
	writeFile(t, filepath.Join(dir, "run.sh"), "#!/bin/sh\necho hi\n")
	if err := os.Chmod(filepath.Join(dir, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	srcGit(t, dir, "add", "-A")
	srcGit(t, dir, "commit", "-qm", "c1")
	if err := os.Symlink("README.md", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "README.md"), "hello world\nsecond\n")
	srcGit(t, dir, "add", "-A")
	srcGit(t, dir, "commit", "-qm", "c2")
	baseSHA = srcGit(t, dir, "rev-parse", "HEAD")
	objectFormat = srcGit(t, dir, "rev-parse", "--show-object-format")

	// Unreachable objects that must be excluded from the closure.
	danglingCmd := exec.Command("git", "-C", dir, "hash-object", "-w", "--stdin")
	danglingCmd.Stdin = strings.NewReader("unreachable dangling blob\n")
	if out, err := danglingCmd.CombinedOutput(); err != nil {
		t.Fatalf("plant dangling blob: %v\n%s", err, out)
	}
	writeFile(t, filepath.Join(dir, "lib/core.go"), "package lib\nuncommitted\n")
	srcGit(t, dir, "stash", "push", "-q", "-m", "wip")
	return dir, baseSHA, objectFormat
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sortedLines(s string) []string {
	fields := strings.Fields(s)
	sort.Strings(fields)
	return fields
}

func TestExtractClosurePackProducesTheExactReachableClosure(t *testing.T) {
	src, base, objfmt := buildSourceRepo(t)
	packDir := t.TempDir()

	count, err := extractClosurePack(src, base, objfmt, packDir)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Exactly one pack + its index, no loose objects, no persistent alternate.
	entries, _ := os.ReadDir(packDir)
	var packs, idxs int
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".pack"):
			packs++
		case strings.HasSuffix(e.Name(), ".idx"):
			idxs++
		default:
			t.Fatalf("closure pack dir holds an unexpected entry %q", e.Name())
		}
	}
	if packs != 1 || idxs != 1 {
		t.Fatalf("want exactly one pack+idx, got %d pack / %d idx", packs, idxs)
	}

	// The pack's object set equals the source's reachable closure of base.
	wantOIDs := sortedLines(strings.NewReplacer().Replace(revListOIDs(t, src, base)))
	if count != len(wantOIDs) {
		t.Fatalf("closure count = %d, want %d", count, len(wantOIDs))
	}

	// The unreachable dangling blob is absent from the closure.
	dangling := hashStdin(t, "unreachable dangling blob\n")
	for _, oid := range wantOIDs {
		if oid == dangling {
			t.Fatal("the reachable closure unexpectedly contains the dangling blob")
		}
	}
}

func TestExtractClosurePackRefusesAForbiddenSource(t *testing.T) {
	cases := []struct {
		name string
		muta func(t *testing.T, dir string)
	}{
		{"alternates", func(t *testing.T, dir string) {
			p := filepath.Join(dir, ".git", "objects", "info")
			_ = os.MkdirAll(p, 0o755)
			writeFile(t, filepath.Join(p, "alternates"), "/some/other/objects\n")
		}},
		{"grafts", func(t *testing.T, dir string) {
			p := filepath.Join(dir, ".git", "info")
			_ = os.MkdirAll(p, 0o755)
			writeFile(t, filepath.Join(p, "grafts"), strings.Repeat("a", 40)+"\n")
		}},
		{"replace", func(t *testing.T, dir string) {
			base := srcGit(t, dir, "rev-parse", "HEAD")
			// A replace ref pointing HEAD at its parent rewrites history.
			parent := srcGit(t, dir, "rev-parse", "HEAD~1")
			srcGit(t, dir, "replace", base, parent)
		}},
		{"shallow", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, ".git", "shallow"), srcGit(t, dir, "rev-parse", "HEAD")+"\n")
		}},
		{"promisor", func(t *testing.T, dir string) {
			srcGit(t, dir, "config", "extensions.partialClone", "origin")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, base, objfmt := buildSourceRepo(t)
			tc.muta(t, src)
			packDir := t.TempDir()
			if _, err := extractClosurePack(src, base, objfmt, packDir); err == nil {
				t.Fatalf("a %s source was extracted without refusal", tc.name)
			}
		})
	}
}

// revListOIDs returns the space-joined reachable OIDs of base in dir.
func revListOIDs(t *testing.T, dir, base string) string {
	t.Helper()
	out := srcGit(t, dir, "rev-list", "--objects", base)
	var oids []string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			oids = append(oids, f[0])
		}
	}
	return strings.Join(oids, " ")
}

func hashStdin(t *testing.T, body string) string {
	t.Helper()
	cmd := exec.Command("git", "hash-object", "--stdin")
	cmd.Stdin = strings.NewReader(body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hash-object: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}
