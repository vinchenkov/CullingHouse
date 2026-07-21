package verbs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Step 4e: the CAS ref creation and the merge (ADR-017:748-756).
//
//	"The CAS-created real `refs/heads/mc/task-<id>` at the verified SHA is the
//	 durable import stage marker. Retry accepts it only for the same action/SHA,
//	 rechecks the SHA fence, and runs the required `git merge --no-ff` in the
//	 primary checkout with disabled hooks/external drivers and fixed metadata."
//
// STOPS AT THE MERGE. Cleanup (deleting the task ref, emptying the task root)
// is ADR-017:757-759's "later trusted action" after the spine records success,
// and it has neither a mount nor an owner yet.

func mergeReady(t *testing.T) (repo, sealed, base, verified, branch string) {
	t.Helper()
	repo, sealed, base, verified = importPair(t)
	if err := importSealedClosure(sealed, repo, base, verified); err != nil {
		t.Fatalf("import: %v", err)
	}
	return repo, sealed, base, verified, taskAssignmentBranch(7)
}

func TestCreateLandingRefIsCASAgainstAbsence(t *testing.T) {
	repo, _, _, verified, branch := mergeReady(t)
	g := landingGit{dir: repo}
	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatalf("CAS create: %v", err)
	}
	got, err := g.out("rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil || got != verified {
		t.Fatalf("ref = %q (err %v), want %q", got, err, verified)
	}
}

// Retry accepts the ref only for the SAME SHA. That is what makes the ref a
// durable stage marker: a crash after creation replays into an accept, while a
// ref someone else put there at a different commit refuses rather than being
// overwritten.
func TestCreateLandingRefRetryAcceptsOnlyTheSameSHA(t *testing.T) {
	repo, _, base, verified, branch := mergeReady(t)
	g := landingGit{dir: repo}
	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatalf("retry at the same SHA must be inert, got: %v", err)
	}
	// A ref at a different commit is someone else's, or a stale action's.
	srcGit(t, repo, "update-ref", "refs/heads/"+branch, base)
	if err := createLandingRefCAS(g, branch, verified); err == nil {
		t.Fatal("accepted a pre-existing landing ref at a different commit")
	}
	// ...and it must be left exactly as found.
	if got := srcGit(t, repo, "rev-parse", "refs/heads/"+branch); got != base {
		t.Fatalf("the refused ref was modified: %q", got)
	}
}

// A symbolic ref at the landing branch name must never be followed.
//
// The aliased branch is deliberately placed AT THE REVIEWED SHA, and that is
// the whole point of the case. With it anywhere else, the existence check
// refuses first ("already exists at X, not Y") and this fence proves nothing —
// mutation showed exactly that. But when the alias resolves to the reviewed
// commit, the existence check sees the value it expects and returns "replay
// accepted", so landing would proceed believing it owns a ref that is really
// an alias onto an operator branch. The merge would then run, and the eventual
// cleanup would delete a ref pointing at the operator's own work. This fence,
// running first, is the only thing between that and a real repository.
func TestCreateLandingRefRefusesASymbolicRefAtItsName(t *testing.T) {
	repo, _, _, verified, branch := mergeReady(t)
	srcGit(t, repo, "branch", "operator-feature", verified)
	srcGit(t, repo, "symbolic-ref", "refs/heads/"+branch, "refs/heads/operator-feature")
	before := srcGit(t, repo, "rev-parse", "refs/heads/operator-feature")
	if err := createLandingRefCAS(landingGit{dir: repo}, branch, verified); err == nil {
		t.Fatal("CAS created through a symbolic ref")
	}
	if after := srcGit(t, repo, "rev-parse", "refs/heads/operator-feature"); after != before {
		t.Fatalf("the operator branch moved: %q -> %q", before, after)
	}
}

func TestSealedMergeLandsTheReviewedChange(t *testing.T) {
	repo, _, _, verified, branch := mergeReady(t)
	g := landingGit{dir: repo}
	pre := srcGit(t, repo, "rev-parse", "refs/heads/main")
	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatal(err)
	}
	if err := mergeSealedLanding(g, "main", verified, pre, "0011223344556677", 7); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// A --no-ff merge: exactly two parents, the second being the reviewed SHA.
	parents := srcGit(t, repo, "rev-list", "--parents", "-n", "1", "refs/heads/main")
	fields := strings.Fields(parents)
	if len(fields) != 3 {
		t.Fatalf("landing did not create a two-parent merge: %q", parents)
	}
	if fields[1] != pre || fields[2] != verified {
		t.Fatalf("merge parents = %v, want [%s %s]", fields[1:], pre, verified)
	}
	// The reviewed bytes are actually in the working tree now.
	body := srcGit(t, repo, "show", "refs/heads/main:lib/new.go")
	if !strings.Contains(body, "package lib") {
		t.Fatalf("reviewed file did not land: %q", body)
	}
}

// The SHA fence is rechecked immediately before the merge (ADR-017:750). If the
// target moved between the repository stage and here, the pre-merge SHA the
// plan froze no longer describes what we are about to merge into.
func TestSealedMergeRechecksTheSHAFence(t *testing.T) {
	repo, _, _, verified, branch := mergeReady(t)
	g := landingGit{dir: repo}
	pre := srcGit(t, repo, "rev-parse", "refs/heads/main")
	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatal(err)
	}
	// The operator commits between the fence and the merge.
	writeFile(t, filepath.Join(repo, "operator-late.txt"), "late\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "operator moved the target")
	moved := srcGit(t, repo, "rev-parse", "refs/heads/main")
	if err := mergeSealedLanding(g, "main", verified, pre, "0011223344556677", 7); err == nil {
		t.Fatal("merged against a target that had moved since the fence")
	}
	if now := srcGit(t, repo, "rev-parse", "refs/heads/main"); now != moved {
		t.Fatalf("the refused merge moved the target: %q", now)
	}
}

// Ported from legacy mc-land.test.ts:683. The preflight stage cannot see a
// merge driver inserted AFTER it ran, and an in-tree .gitattributes naming that
// driver survives `core.attributesFile=/dev/null` (which replaces the
// attributes FILE, not in-tree ones) and `GIT_ATTR_NOSYSTEM=1` (system scope).
// The pinned `-c merge.ours.driver=false` covers only `ours`.
//
// So the merge stage rechecks the executable-control fences itself. The window
// is narrowed, not closed — a writer racing the merge invocation still wins —
// and that residual is documented at the call site rather than papered over.
func TestSealedMergeRechecksExecutableConfigInsertedAfterPreflight(t *testing.T) {
	late := map[string]func(t *testing.T, repo string){
		"a merge driver inserted after preflight": func(t *testing.T, repo string) {
			srcGit(t, repo, "config", "--local", "merge.evil.driver", "sh -c 'echo pwned >&2' %A %O %B")
			writeFile(t, filepath.Join(repo, ".gitattributes"), "* merge=evil\n")
		},
		// MEASURED REDUNDANT: this arm survives mutation of the merge-time
		// recheck, because git's own checkout fails when the filter cannot run —
		// so it is git refusing, not our fence. Retained because the OUTCOME is
		// the property that matters (no merge, target unmoved) and it would
		// still hold if git ever succeeded here, but it must not be read as
		// evidence that the recheck works. The other two arms are that evidence.
		"a clean filter inserted after preflight": func(t *testing.T, repo string) {
			srcGit(t, repo, "config", "--local", "filter.evil.clean", "sh -c 'echo pwned >&2'")
			writeFile(t, filepath.Join(repo, ".gitattributes"), "* filter=evil\n")
		},
		"an index visibility flag set after preflight": func(t *testing.T, repo string) {
			srcGit(t, repo, "update-index", "--assume-unchanged", "README.md")
		},
	}
	for name, plant := range late {
		t.Run(name, func(t *testing.T) {
			repo, _, _, verified, branch := mergeReady(t)
			g := landingGit{dir: repo}
			pre := srcGit(t, repo, "rev-parse", "refs/heads/main")
			if err := createLandingRefCAS(g, branch, verified); err != nil {
				t.Fatal(err)
			}
			plant(t, repo)
			if err := mergeSealedLanding(g, "main", verified, pre, "0011223344556677", 7); err == nil {
				t.Fatalf("merged with %s", name)
			}
			if now := srcGit(t, repo, "rev-parse", "refs/heads/main"); now != pre {
				t.Fatalf("the refused merge moved the target: %q", now)
			}
		})
	}
}

// Ported from legacy mc-land.test.ts:763, and it closes an UNPINNED knob.
//
// Mutation found this: deleting `merge.autoStash=false`, `--no-autostash`,
// `merge.renames=false` and `merge.directoryRenames=false` together left the
// whole mc/verbs suite green. They were pinned in code and held by nothing —
// the same invisible rot as 6657541 and 83ed9e9, where an implementation stayed
// correct while no test reached it.
//
// autostash is the dangerous one because it MOVES OPERATOR BYTES. With it on,
// a merge blocked by local changes silently stashes them, merges, and pops —
// so a landing would rewrite the operator's working tree as a side effect. The
// lane's own dirty fence normally refuses this case earlier; this exercises the
// merge stage directly, because defence in depth that nothing tests is not
// defence.
func TestSealedMergeNeverAutostashesOperatorBytes(t *testing.T) {
	repo, _, _, verified, branch := mergeReady(t)
	g := landingGit{dir: repo}
	pre := srcGit(t, repo, "rev-parse", "refs/heads/main")
	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatal(err)
	}
	// The repository asks for autostash, and a reviewed path is dirty — the
	// exact combination where git would otherwise stash and pop.
	srcGit(t, repo, "config", "--local", "merge.autoStash", "true")
	writeFile(t, filepath.Join(repo, "README.md"), "operator uncommitted work\n")

	if err := mergeSealedLanding(g, "main", verified, pre, "0011223344556677", 7); err == nil {
		t.Fatal("merged by stashing the operator's uncommitted work")
	}
	// The operator's bytes are still exactly where they were: not stashed, not
	// popped, not reconstructed.
	body, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil || string(body) != "operator uncommitted work\n" {
		t.Fatalf("operator bytes were moved by the refused merge: %q (err %v)", body, err)
	}
	if now := srcGit(t, repo, "rev-parse", "refs/heads/main"); now != pre {
		t.Fatalf("the refused merge moved the target: %q", now)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".git", "refs", "stash")); err == nil {
		t.Fatal("the refused merge created a stash")
	}
}

// Ported from legacy mc-land.test.ts:289,328, and it closes the last two of the
// four knobs 7364503 found were held by nothing.
//
// RENAME INFERENCE IS A HEURISTIC, and the merge stage is the one place it can
// move bytes. The reviewed side renames `old.txt` to `new.txt` and edits it; the
// operator edits `old.txt` where it still is. With `merge.renames=false` git
// sees a delete against a modify and refuses. With detection ON it "helpfully"
// carries the operator's edit onto `new.txt` and commits — a landing that writes
// a path no reviewer saw and that the reviewed-path dirty fence therefore never
// checked, because that fence derives its set with `--no-renames`. The two
// halves must agree, and this is what makes them agree.
//
// MUTATION-CONFIRMED (2026-07-20): removing `merge.renames=false` alone makes
// this merge SUCCEED and the test fail. `merge.directoryRenames=false` is
// CORRECT AND UNREACHABLE — git disables directory-rename detection whenever
// rename detection is off, so with the first knob pinned no input can reach it.
// It is retained as the explicit statement that neither heuristic is wanted, and
// recorded here so it is not counted as a live fence.
func TestSealedMergeNeverInfersARename(t *testing.T) {
	repo, base, verified, pre, branch := renameMergeReady(t)
	g := landingGit{dir: repo}

	// FIXTURE GUARD. The whole case is vacuous if git would not infer this
	// rename in the first place: a fixture whose similarity drifts below the
	// detection threshold keeps passing while testing nothing.
	status, err := g.out("diff-tree", "-M", "-r", "--no-commit-id", "--name-status", base, verified)
	if err != nil || !strings.HasPrefix(status, "R") {
		t.Fatalf("fixture is not rename-inferable; the case proves nothing: %q (err %v)", status, err)
	}

	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatal(err)
	}
	if err := mergeSealedLanding(g, "main", verified, pre, "0011223344556677", 7); err == nil {
		t.Fatal("the merge inferred a rename and relocated the operator's edit onto an unreviewed path")
	}
	if now := srcGit(t, repo, "rev-parse", "refs/heads/main"); now != pre {
		t.Fatalf("the refused merge moved the target: %q", now)
	}
	// The operator's file is still committed where it was, under its own name,
	// and the reviewed path was never created on the target.
	body := srcGit(t, repo, "show", "refs/heads/main:old.txt")
	if !strings.Contains(body, "operator line 20") {
		t.Fatalf("the operator's bytes at the original path did not survive: %q", body)
	}
	if _, err := g.out("cat-file", "-e", "refs/heads/main^{tree}:new.txt"); err == nil {
		t.Fatal("the refused merge created the renamed path on the target")
	}
}

// renameMergeReady is a bespoke pair, because `mergeReady`'s sealed content is
// fixed and shares no path with the target beyond README.md. Here the two sides
// must touch THE SAME tracked file by different names for the heuristic to have
// anything to infer.
//
// The two edits are deliberately at opposite ends of the file: were they to
// overlap, the merge would conflict with rename detection either on or off, and
// the test would pass without pinning anything.
func renameMergeReady(t *testing.T) (repo, base, verified, pre, branch string) {
	t.Helper()
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	body := func(mutate func([]string)) string {
		out := append([]string{}, lines...)
		mutate(out)
		return strings.Join(out, "\n") + "\n"
	}

	repo = t.TempDir()
	srcGit(t, repo, "init", "-q", "-b", "main")
	srcGit(t, repo, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(repo, "old.txt"), body(func([]string) {}))
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "base")
	base = srcGit(t, repo, "rev-parse", "HEAD")

	// The reviewed side: rename, then edit the head of the file.
	sealed := filepath.Join(t.TempDir(), "sealed")
	srcGit(t, filepath.Dir(sealed), "clone", "-q", "--no-hardlinks", repo, sealed)
	srcGit(t, sealed, "config", "commit.gpgsign", "false")
	srcGit(t, sealed, "checkout", "-q", "-b", taskAssignmentBranch(7))
	srcGit(t, sealed, "mv", "old.txt", "new.txt")
	writeFile(t, filepath.Join(sealed, "new.txt"), body(func(l []string) { l[0] = "reviewed line 1" }))
	srcGit(t, sealed, "add", "-A")
	srcGit(t, sealed, "commit", "-qm", "reviewed renames and edits the file")
	verified = srcGit(t, sealed, "rev-parse", "HEAD")

	// The operator, meanwhile, edits the tail of the file at its original path.
	writeFile(t, filepath.Join(repo, "old.txt"), body(func(l []string) { l[19] = "operator line 20" }))
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "operator edits the original path")
	pre = srcGit(t, repo, "rev-parse", "refs/heads/main")

	if err := importSealedClosure(sealed, repo, base, verified); err != nil {
		t.Fatalf("import: %v", err)
	}
	return repo, base, verified, pre, taskAssignmentBranch(7)
}

// Fixed metadata (ADR-017:752): author and committer are pinned so the merge is
// byte-reproducible on replay, and a hostile environment cannot forge identity.
// The env is constructed, so the hostile values below never reach git.
func TestSealedMergeUsesFixedIdentityAndIgnoresHostileEnv(t *testing.T) {
	for _, hostile := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(hostile, "attacker")
	}
	repo, _, _, verified, branch := mergeReady(t)
	g := landingGit{dir: repo}
	pre := srcGit(t, repo, "rev-parse", "refs/heads/main")
	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatal(err)
	}
	if err := mergeSealedLanding(g, "main", verified, pre, "0011223344556677", 7); err != nil {
		t.Fatalf("merge: %v", err)
	}
	for _, format := range []string{"%an", "%cn", "%ae", "%ce"} {
		got := srcGit(t, repo, "log", "-1", "--format="+format, "refs/heads/main")
		if strings.Contains(got, "attacker") {
			t.Fatalf("hostile identity reached the merge commit (%s = %q)", format, got)
		}
	}
	if got := srcGit(t, repo, "log", "-1", "--format=%an", "refs/heads/main"); got != landingIdentityName {
		t.Fatalf("author = %q, want the fixed %q", got, landingIdentityName)
	}
}

// A repository hook must not fire during the merge — this is the stage where
// legacy's accidental isolation would have mattered most, because a merge
// genuinely runs hooks in normal operation.
func TestSealedMergeRunsNoHook(t *testing.T) {
	repo, _, _, verified, branch := mergeReady(t)
	g := landingGit{dir: repo}
	hooks := filepath.Join(repo, ".git", "hooks")
	marker := filepath.Join(repo, "merge-hook-fired")
	for _, name := range []string{"prepare-commit-msg", "commit-msg", "post-merge", "post-commit", "reference-transaction"} {
		path := filepath.Join(hooks, name)
		writeFile(t, path, "#!/bin/sh\ntouch "+marker+"\n")
		chmodExec(t, path)
	}
	pre := srcGit(t, repo, "rev-parse", "refs/heads/main")
	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatal(err)
	}
	if err := mergeSealedLanding(g, "main", verified, pre, "0011223344556677", 7); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if fileExists(marker) {
		t.Fatal("a repository hook fired during the sealed merge")
	}
}

// The merge carries an action trailer naming the landing and the task
// (ADR-017:755 makes the trailer part of what a retry matches on).
func TestSealedMergeCarriesItsActionTrailer(t *testing.T) {
	repo, _, _, verified, branch := mergeReady(t)
	g := landingGit{dir: repo}
	pre := srcGit(t, repo, "rev-parse", "refs/heads/main")
	if err := createLandingRefCAS(g, branch, verified); err != nil {
		t.Fatal(err)
	}
	if err := mergeSealedLanding(g, "main", verified, pre, "0011223344556677", 7); err != nil {
		t.Fatalf("merge: %v", err)
	}
	body := srcGit(t, repo, "log", "-1", "--format=%B", "refs/heads/main")
	for _, want := range []string{"0011223344556677", verified, "task 7"} {
		if !strings.Contains(body, want) {
			t.Fatalf("merge message does not carry %q:\n%s", want, body)
		}
	}
}

func chmodExec(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// The zero old-value is the ATOMIC half of the CAS, and it is the fence that
// actually matters: `createLandingRefCAS`'s rev-parse pre-check is a
// message-improving convenience with a TOCTOU window, while
// `update-ref <ref> <new> ""` refuses at the ref lock itself.
//
// The race that distinguishes them — a concurrent creator between the
// pre-check and the write — is not reachable from the fast lane without a
// concurrency harness, so this test pins the MECHANISM the fence relies on
// instead. Without it, mutating the `""` away leaves no test failing at all.
func TestUpdateRefZeroOldValueRefusesAnExistingRef(t *testing.T) {
	repo, _, _, verified, branch := mergeReady(t)
	g := landingGit{dir: repo}
	ref := "refs/heads/" + branch
	if _, err := g.run("update-ref", "--no-deref", ref, verified, ""); err != nil {
		t.Fatalf("zero old-value create against absence: %v", err)
	}
	// Same value, but the ref now exists: the zero old-value must still refuse.
	if _, err := g.run("update-ref", "--no-deref", ref, verified, ""); err == nil {
		t.Fatal("update-ref with a zero old-value accepted an existing ref; the CAS is not exclusive")
	}
}
