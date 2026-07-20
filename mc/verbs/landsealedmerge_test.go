package verbs

import (
	"os"
	"path/filepath"
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
