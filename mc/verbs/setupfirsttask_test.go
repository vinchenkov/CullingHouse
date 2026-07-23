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

func TestExtractClosurePackReadsAReadOnlySourceIntoTheTaskObjectStore(t *testing.T) {
	src, base, objfmt := buildSourceRepo(t)
	objects := filepath.Join(src, ".git", "objects")
	packDir := filepath.Join(t.TempDir(), "objects", "pack")
	if err := os.MkdirAll(packDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Match the setup container boundary: source objects are readable but no
	// scratch pack may land there. Restore the mode for t.TempDir cleanup.
	t.Cleanup(func() { _ = os.Chmod(objects, 0o700) })
	if err := os.Chmod(objects, 0o555); err != nil {
		t.Fatal(err)
	}

	if _, err := extractClosurePack(src, base, objfmt, packDir); err != nil {
		t.Fatalf("extract from read-only source: %v", err)
	}
	if _, err := singlePackIdx(packDir); err != nil {
		t.Fatal(err)
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

func mkTaskChildren(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, c := range []string{"source", "git"} {
		if err := os.Mkdir(filepath.Join(root, c), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestMaterializeFirstTaskStoreBuildsAFsckCleanOperableStore(t *testing.T) {
	src, base, objfmt := buildSourceRepo(t)
	root := mkTaskChildren(t)
	uuid := "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	spec := FirstTaskSetupSpec{TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: objfmt, LocalRepoUUID: uuid}

	res, err := MaterializeFirstTaskStore(src, root, spec)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if res.BaseSHA != base || res.ObjectFormat != objfmt || res.LocalRepoUUID != uuid || !res.FsckClean {
		t.Fatalf("result = %+v, want base %s / %s / uuid %s / fsck clean", res, base, objfmt, uuid)
	}
	if res.ObjectCount < 5 || len(res.ClosureDigest) != 64 || !assignmentHex.MatchString(res.ClosureDigest) {
		t.Fatalf("result closure = count %d digest %q", res.ObjectCount, res.ClosureDigest)
	}

	// The sole ref exists at base and HEAD resolves through the worktree to it.
	refBytes, err := os.ReadFile(filepath.Join(root, "git", "refs", "heads", "mc", "task-7"))
	if err != nil || strings.TrimSpace(string(refBytes)) != base {
		t.Fatalf("refs/heads/mc/task-7 = %q (%v), want %s", refBytes, err, base)
	}
	source := filepath.Join(root, "source")
	if got := srcGit(t, source, "rev-parse", "HEAD"); got != base {
		t.Fatalf("worktree HEAD = %s, want %s", got, base)
	}
	if got := srcGit(t, source, "status", "--porcelain"); got != "" {
		t.Fatalf("materialized worktree is dirty:\n%s", got)
	}
	// The empty git/shallow cover (ADR-017:467) makes git report is-shallow=true;
	// harmless for a complete store (status/commit/fsck all pass), and the
	// object-set==closure proof, not is-shallow, is the completeness guard
	// (deviation 2026-07-17). Pinned so a future cover change is noticed.
	if got := srcGit(t, source, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("expected is-shallow=true from the empty shallow cover, got %s", got)
	}
	// The materialized tree matches the base tree (executable + symlink preserved).
	if got := srcGit(t, source, "cat-file", "-p", "HEAD:run.sh"); !strings.Contains(got, "echo hi") {
		t.Fatalf("run.sh content = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "git", "objects", "info", "alternates")); !os.IsNotExist(err) {
		t.Fatal("the task store carries a persistent alternate")
	}
	// No loose objects: everything is in the one pack.
	fanout, _ := filepath.Glob(filepath.Join(root, "git", "objects", "[0-9a-f][0-9a-f]"))
	if len(fanout) != 0 {
		t.Fatalf("the task store holds loose objects: %v", fanout)
	}
	// The generated config carries exactly the closed key set.
	cfg, _ := os.ReadFile(filepath.Join(root, "git", "config"))
	for _, want := range []string{"repositoryformatversion = 1", "relativeWorktrees = true", "localRepoUuid = " + uuid} {
		if !strings.Contains(string(cfg), want) {
			t.Fatalf("generated config missing %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(string(cfg), "hooksPath") || strings.Contains(string(cfg), "[remote") {
		t.Fatalf("generated config carries a forbidden key:\n%s", cfg)
	}
}

func TestMaterializeFirstTaskStoreRetryPinsTheExactBaseSHA(t *testing.T) {
	src, head, objfmt := buildSourceRepo(t)
	older := srcGit(t, src, "rev-parse", "HEAD~1")
	if older == head {
		t.Fatal("fixture needs two commits")
	}
	root := mkTaskChildren(t)
	spec := FirstTaskSetupSpec{TaskID: 7, Mode: "retry", TargetRef: "HEAD", PinnedBaseSHA: older,
		ObjectFormat: objfmt, LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"}

	res, err := MaterializeFirstTaskStore(src, root, spec)
	if err != nil {
		t.Fatalf("retry materialize: %v", err)
	}
	// Retry pins the recorded OID, never rebasing to the moved HEAD (D5).
	if res.BaseSHA != older {
		t.Fatalf("retry base = %s, want the pinned %s (not HEAD %s)", res.BaseSHA, older, head)
	}
	if got := srcGit(t, filepath.Join(root, "source"), "rev-parse", "HEAD"); got != older {
		t.Fatalf("retry worktree HEAD = %s, want pinned %s", got, older)
	}
}

func TestMaterializeFirstTaskStoreRefusesObjectFormatMismatch(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	other := "sha256"
	if objfmt == "sha256" {
		other = "sha1"
	}
	root := mkTaskChildren(t)
	spec := FirstTaskSetupSpec{TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: other,
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"}
	if _, err := MaterializeFirstTaskStore(src, root, spec); err == nil {
		t.Fatal("a store was materialized with a mismatched object format")
	}
}

func TestMaterializeFirstTaskStoreRefusesResidueInAChild(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	root := mkTaskChildren(t)
	writeFile(t, filepath.Join(root, "git", "residue"), "x\n")
	spec := FirstTaskSetupSpec{TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: objfmt,
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"}
	if _, err := MaterializeFirstTaskStore(src, root, spec); err == nil {
		t.Fatal("a store was materialized over residue in the git child")
	}
}

// Every committable reserved root component is refused: .mission-control is the
// task-era cover (ADR-017:433-434) and .mc-worktrees is the shared initiative
// checkout area (ADR-025 D10) — a committed path there would collide with the
// live worktree in the primary checkout at merge time. .git is also reserved in
// the production check but git itself refuses to track such a path, so it has
// no expressible case here.
func TestMaterializeFirstTaskStoreRefusesEveryReservedRootComponent(t *testing.T) {
	for _, name := range []string{".mission-control", ".mc-worktrees"} {
		t.Run(name, func(t *testing.T) {
			src, _, objfmt := buildSourceRepo(t)
			if err := os.MkdirAll(filepath.Join(src, name), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(src, name, "planted"), "x\n")
			srcGit(t, src, "add", "-Af")
			srcGit(t, src, "commit", "-qm", "reserved")
			root := mkTaskChildren(t)
			spec := FirstTaskSetupSpec{TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: objfmt,
				LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"}
			if _, err := MaterializeFirstTaskStore(src, root, spec); err == nil {
				t.Fatalf("a store was materialized over a reserved %q root component", name)
			}
		})
	}
}

func TestValidateTaskGitConfigClosedGrammar(t *testing.T) {
	uuid := "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	if err := validateTaskGitConfig(generatedTaskGitConfig("sha1", uuid)); err != nil {
		t.Fatalf("generated sha1 config rejected: %v", err)
	}
	if err := validateTaskGitConfig(generatedTaskGitConfig("sha256", uuid)); err != nil {
		t.Fatalf("generated sha256 config rejected: %v", err)
	}
	bad := []string{
		"", // no required keys
		"[core]\n\trepositoryformatversion = 1\n\thooksPath = /tmp\n", // foreign key
		"[remote \"origin\"]\n\turl = http://x\n",                     // foreign section + subsection
		"[core]\n\trepositoryformatversion = 0\n\tbare = true\n[extensions]\n\trelativeWorktrees = true\n[mc]\n\tlocalRepoUuid = " + uuid + "\n",                        // v0 hides extensions
		"[core]\n\trepositoryformatversion = 1\n\tbare = true\n[extensions]\n\trelativeWorktrees = true\n\tobjectFormat = sha3\n[mc]\n\tlocalRepoUuid = " + uuid + "\n", // bad format
	}
	for i, cfg := range bad {
		if err := validateTaskGitConfig([]byte(cfg)); err == nil {
			t.Fatalf("bad config %d was accepted:\n%s", i, cfg)
		}
	}
}
