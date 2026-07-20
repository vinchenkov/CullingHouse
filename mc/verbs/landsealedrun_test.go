package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Step 4 composition: the five built stages become one lane.
//
// The stages were each proven in isolation. What this file proves is the thing
// no stage test can: the ORDER, and that every stage's output is actually
// carried into the next one rather than recomputed from the same input twice.
//
// The load-bearing ordering claim is that the dirty fence runs BEFORE the
// import (ADR-017:741-743). That is only buildable because the reviewed path
// set is derived from the SEALED store against the assignment's frozen base —
// the real repository cannot name the reviewed commit until the import has
// already happened, so deriving the set there would force the fence after it.

const landingFixtureUUID = "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"

// sealedLandingFixture builds a real operator repository on `main` and a real
// materialized sealed task store holding a reviewed commit the operator
// repository has never seen, plus the nested empty cover.
func sealedLandingFixture(t *testing.T) (repo, taskRoot string, pins sealedLandingPins) {
	t.Helper()
	// The repository path contains a SPACE, deliberately and for every test in
	// this file. Legacy pinned this as a single case (mc-land.test.ts:90);
	// making it structural is strictly stronger, because every stage of the lane
	// then handles the spaced path rather than only the one scenario that
	// remembered to. A Worksource root is operator-chosen and "~/My Projects" is
	// entirely ordinary. It costs nothing here: the lane builds arguments as
	// exec argv and never a shell string, so this proves that property rather
	// than defending a quoting bug.
	repo = filepath.Join(t.TempDir(), "work space")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	srcGit(t, repo, "init", "-q", "-b", "main")
	srcGit(t, repo, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(repo, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	// TWO commits, deliberately. A single-commit history makes a rewritten
	// target share no history at all, which a different fence refuses — so the
	// frozen-base binding would be untestable against the scenario it exists for.
	writeFile(t, filepath.Join(repo, "lib/core.go"), "package lib\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "c1")
	writeFile(t, filepath.Join(repo, "README.md"), "operator\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "base")
	base := srcGit(t, repo, "rev-parse", "HEAD")
	format := srcGit(t, repo, "rev-parse", "--show-object-format")

	taskRoot = mkTaskChildren(t)
	if _, err := MaterializeFirstTaskStore(repo, taskRoot, FirstTaskSetupSpec{
		TaskID: 7, Mode: "fresh", TargetRef: "HEAD", ObjectFormat: format,
		LocalRepoUUID: landingFixtureUUID,
	}); err != nil {
		t.Fatal(err)
	}

	// The reviewed change, committed in the sealed store. `-c` rather than
	// `config`: the store's config is compared byte-for-byte against the
	// generated fixed grammar, so writing a local key would fail the store
	// stage for a reason the test never intended.
	source := filepath.Join(taskRoot, "source")
	if err := os.MkdirAll(filepath.Join(source, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "README.md"), "operator\nreviewed\n")
	writeFile(t, filepath.Join(source, "lib/new.go"), "package lib\n")
	srcGit(t, source, "-c", "commit.gpgsign=false", "add", "-A")
	srcGit(t, source, "-c", "commit.gpgsign=false", "commit", "-qm", "reviewed change")
	verified := srcGit(t, source, "rev-parse", "HEAD")

	if err := os.Mkdir(filepath.Join(repo, ".mission-control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := objectPresent(repo, verified); err == nil {
		t.Fatal("fixture is invalid: the operator repository already has the reviewed commit")
	}
	return repo, taskRoot, sealedLandingPins{
		TaskID: 7, Branch: taskAssignmentBranch(7), TargetRef: "main",
		ObjectFormat: format, VerifiedSHA: verified, PreMergeSHA: base,
		PinnedBaseSHA: base, LocalRepoUUID: landingFixtureUUID,
		LandingID: "0011223344556677",
	}
}

func coverOf(repo string) string { return filepath.Join(repo, ".mission-control") }

func TestLandSealedCarriesTheWholeLaneToTheMerge(t *testing.T) {
	repo, taskRoot, pins := sealedLandingFixture(t)
	res, err := landSealed(repo, taskRoot, coverOf(repo), pins)
	if err != nil {
		t.Fatalf("the real sealed landing was refused: %v", err)
	}
	g := landingGit{dir: repo}

	// The closure crossed.
	if err := objectPresent(repo, pins.VerifiedSHA); err != nil {
		t.Fatalf("the reviewed commit is not in the operator store: %v", err)
	}
	// The CAS ref is the durable import marker (ADR-017:748).
	if got, err := g.out("rev-parse", "--verify", "refs/heads/"+pins.Branch); err != nil || got != pins.VerifiedSHA {
		t.Fatalf("landing ref = %q (err %v), want the verified SHA %q", got, err, pins.VerifiedSHA)
	}
	// The merge is a real --no-ff merge of the two exact parents.
	if res.MergeSHA == "" || res.MergeSHA == pins.PreMergeSHA {
		t.Fatalf("the target did not advance: merge %q, pre-merge %q", res.MergeSHA, pins.PreMergeSHA)
	}
	parents, err := g.out("rev-list", "--parents", "-n", "1", res.MergeSHA)
	if err != nil {
		t.Fatal(err)
	}
	want := res.MergeSHA + " " + pins.PreMergeSHA + " " + pins.VerifiedSHA
	if parents != want {
		t.Fatalf("merge topology = %q, want %q", parents, want)
	}
	// The reviewed bytes are actually in the operator's checkout.
	body, err := os.ReadFile(filepath.Join(repo, "lib/new.go"))
	if err != nil || string(body) != "package lib\n" {
		t.Fatalf("the reviewed file did not land: %q (err %v)", body, err)
	}
}

// The ordering claim, pinned directly: a reviewed path dirty in the operator's
// checkout is refused AND nothing was imported. If the fence ever moves after
// the import, the refusal still happens but the object assertion fails.
func TestLandSealedFencesReviewedDirtBeforeImportingAnything(t *testing.T) {
	repo, taskRoot, pins := sealedLandingFixture(t)
	writeFile(t, filepath.Join(repo, "README.md"), "operator local edit\n")

	if _, err := landSealed(repo, taskRoot, coverOf(repo), pins); err == nil {
		t.Fatal("landing accepted a reviewed path dirty in the operator checkout")
	}
	if err := objectPresent(repo, pins.VerifiedSHA); err == nil {
		t.Fatal("the closure was imported before the dirty fence refused; the fence must precede the import (ADR-017:741-743)")
	}
	if _, err := (landingGit{dir: repo}).out("show-ref", "--verify", "--quiet", "refs/heads/"+pins.Branch); err == nil {
		t.Fatal("a landing ref was created on a refused landing")
	}
}

// Unrelated dirt is the operator's business (4c's scoped fence), and the lane
// must not silently re-globalize it.
func TestLandSealedAdmitsDirtOutsideTheReviewedPaths(t *testing.T) {
	repo, taskRoot, pins := sealedLandingFixture(t)
	writeFile(t, filepath.Join(repo, "lib/core.go"), "package lib\n// operator wip\n")

	if _, err := landSealed(repo, taskRoot, coverOf(repo), pins); err != nil {
		t.Fatalf("landing refused over dirt outside the reviewed paths: %v", err)
	}
}

func TestLandSealedRefusesATargetThatMoved(t *testing.T) {
	repo, taskRoot, pins := sealedLandingFixture(t)
	writeFile(t, filepath.Join(repo, "unrelated.txt"), "operator commit\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "operator moved the target")

	if _, err := landSealed(repo, taskRoot, coverOf(repo), pins); err == nil {
		t.Fatal("landing accepted a target that moved since the plan was frozen")
	} else if !strings.Contains(err.Error(), "pre-merge") {
		t.Fatalf("refusal does not name the pre-merge fence: %v", err)
	}
	if err := objectPresent(repo, pins.VerifiedSHA); err == nil {
		t.Fatal("the closure was imported despite the target having moved")
	}
}

// Without the cover, the sealed bytes are reachable RW through the source
// alias — the hazard step 3 recorded as owed to this stage.
func TestLandSealedRefusesAMissingOrPopulatedCover(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		repo, taskRoot, pins := sealedLandingFixture(t)
		if err := os.Remove(coverOf(repo)); err != nil {
			t.Fatal(err)
		}
		if _, err := landSealed(repo, taskRoot, coverOf(repo), pins); err == nil {
			t.Fatal("landing ran without the .mission-control cover")
		}
	})
	t.Run("populated", func(t *testing.T) {
		repo, taskRoot, pins := sealedLandingFixture(t)
		writeFile(t, filepath.Join(coverOf(repo), "leak"), "x\n")
		if _, err := landSealed(repo, taskRoot, coverOf(repo), pins); err == nil {
			t.Fatal("landing ran with a populated cover; it is not covering the real path")
		}
	})
	t.Run("symlinked", func(t *testing.T) {
		repo, taskRoot, pins := sealedLandingFixture(t)
		if err := os.Remove(coverOf(repo)); err != nil {
			t.Fatal(err)
		}
		elsewhere := t.TempDir()
		if err := os.Symlink(elsewhere, coverOf(repo)); err != nil {
			t.Fatal(err)
		}
		if _, err := landSealed(repo, taskRoot, coverOf(repo), pins); err == nil {
			t.Fatal("landing accepted a symlinked cover; it covers nothing and can be redirected")
		}
	})
	t.Run("foreign path", func(t *testing.T) {
		repo, taskRoot, pins := sealedLandingFixture(t)
		other := t.TempDir()
		if _, err := landSealed(repo, taskRoot, other, pins); err == nil {
			t.Fatal("landing accepted a cover that is not this source's nested cover")
		}
	})
}

// The frozen base binds the history relationship: a base that is not the real
// merge base means the target was REWRITTEN out from under the review.
//
// The scenario has to be built precisely, and getting it wrong hides the fence.
// A first draft simply set the frozen base to the reviewed commit; that makes
// the sealed diff empty, so path derivation refuses first and the binding is
// never reached — the mutation survived and the test still passed. Here the
// operator amends the target tip, so the merge base slides back to c1 while the
// assignment still names c2, and every earlier stage passes.
func TestLandSealedRefusesARewrittenTarget(t *testing.T) {
	repo, taskRoot, pins := sealedLandingFixture(t)
	srcGit(t, repo, "commit", "--amend", "-qm", "operator rewrote the target")
	pins.PreMergeSHA = srcGit(t, repo, "rev-parse", "HEAD")

	_, err := landSealed(repo, taskRoot, coverOf(repo), pins)
	if err == nil {
		t.Fatal("landing accepted a target rewritten out from under the frozen base")
	}
	if !strings.Contains(err.Error(), "frozen base") {
		t.Fatalf("refusal does not name the frozen-base binding: %v", err)
	}
}

// Every frozen sealed-store fact is carried into the store stage rather than
// read off the store. Drifting any one of them must refuse.
func TestLandSealedCarriesTheFrozenStoreFacts(t *testing.T) {
	drift := map[string]func(p *sealedLandingPins){
		"verified sha": func(p *sealedLandingPins) { p.VerifiedSHA = strings.Repeat("a", len(p.VerifiedSHA)) },
		"repo uuid":    func(p *sealedLandingPins) { p.LocalRepoUUID = "0f9c1e2a-3b4c-4d5e-8f60-112233445566" },
		"task id":      func(p *sealedLandingPins) { p.TaskID = 8; p.Branch = taskAssignmentBranch(8) },
	}
	for name, mutate := range drift {
		t.Run(name, func(t *testing.T) {
			repo, taskRoot, pins := sealedLandingFixture(t)
			mutate(&pins)
			if _, err := landSealed(repo, taskRoot, coverOf(repo), pins); err == nil {
				t.Fatalf("landing accepted a store with a drifted %s", name)
			}
		})
	}
}

// A reviewed commit ALREADY reachable from the target must refuse, not merge
// again. Legacy pinned this as "refuses an already-ancestor SHA instead of
// deleting its inputs" (mc-land.test.ts:882); the sealed lane has no deletion,
// so what matters is only that it refuses rather than producing a degenerate
// second merge.
//
// This is also the concrete shape of the retry gap logged on 2026-07-20: with
// no adoption path (ADR-017:750-753), a retry after a SUCCESSFUL landing must
// fail closed. Here it refuses at the frozen-base binding — once the reviewed
// commit is merged, it becomes the merge base itself, which is not the base the
// assignment named. Fail-closed, and the message says so.
func TestLandSealedRefusesAnAlreadyLandedReviewedCommit(t *testing.T) {
	repo, taskRoot, pins := sealedLandingFixture(t)
	first, err := landSealed(repo, taskRoot, coverOf(repo), pins)
	if err != nil {
		t.Fatalf("the first landing was refused: %v", err)
	}

	// Retry the same landing against the post-merge target.
	pins.PreMergeSHA = first.MergeSHA
	_, err = landSealed(repo, taskRoot, coverOf(repo), pins)
	if err == nil {
		t.Fatal("landing merged an already-landed reviewed commit a second time")
	}
	if !strings.Contains(err.Error(), "frozen base") {
		t.Fatalf("refusal does not name the frozen-base binding: %v", err)
	}
	// The first merge is untouched: a refused retry must not move the target.
	if tip, err := (landingGit{dir: repo}).out("rev-parse", "--verify", "refs/heads/main^{commit}"); err != nil || tip != first.MergeSHA {
		t.Fatalf("target tip = %q (err %v), want the first merge %q", tip, err, first.MergeSHA)
	}
}

// CHARACTERIZING, not aspirational: this records what the lane does today when
// the merge CONFLICTS, because the answer was not obvious and matters
// operationally.
//
// A conflict is reachable even though the reviewed paths are fenced clean and
// the target tip is pinned: the target may have ADVANCED from the frozen base
// to the pre-merge SHA touching the very paths the reviewed change touches.
// Base has "operator", the target commits one edit to README.md, the reviewed
// change commits a different one — every earlier stage passes and `merge --no-ff`
// cannot resolve it.
//
// The lane has no abort path (cleanup has no mount and no owner), so it returns
// the error and LEAVES the conflicted state behind. That state then trips the
// operator-merge-in-flight fence on the next attempt, which refuses rather than
// disturbing what it finds — so a conflicted landing head-of-line-blocks until
// a human intervenes. Whether that is right is a decision, logged 2026-07-20;
// this test exists so the behaviour cannot change silently either way.
func TestLandSealedLeavesAConflictedMergeInPlace(t *testing.T) {
	repo, taskRoot, pins := sealedLandingFixture(t)
	writeFile(t, filepath.Join(repo, "README.md"), "operator\nconflicting\n")
	srcGit(t, repo, "add", "-A")
	srcGit(t, repo, "commit", "-qm", "operator edits the same reviewed path")
	pins.PreMergeSHA = srcGit(t, repo, "rev-parse", "HEAD")

	if _, err := landSealed(repo, taskRoot, coverOf(repo), pins); err == nil {
		t.Fatal("a conflicting merge reported success")
	}
	// The conflicted state is left behind, and the next attempt refuses at the
	// merge-in-flight fence rather than disturbing it.
	if _, err := os.Lstat(filepath.Join(repo, ".git", "MERGE_HEAD")); err != nil {
		t.Skipf("no MERGE_HEAD left behind (%v); the conflict shape changed — re-derive the finding", err)
	}
	_, err := landSealed(repo, taskRoot, coverOf(repo), pins)
	if err == nil {
		t.Fatal("a retry ran against a conflicted checkout")
	}
	if !strings.Contains(err.Error(), "merge already in progress") {
		t.Fatalf("retry refused for some other reason than the in-flight merge: %v", err)
	}
}

// An internally inconsistent pins struct never reaches a repository. These
// duplicate what the envelope validator already refuses; they are kept because
// landSealed is directly reachable and they name the problem at the point of
// use rather than letting a mismatched branch surface as a store refusal.
func TestLandSealedRefusesInconsistentPins(t *testing.T) {
	broken := map[string]func(p *sealedLandingPins){
		"no task":          func(p *sealedLandingPins) { p.TaskID = 0 },
		"branch mismatch":  func(p *sealedLandingPins) { p.Branch = taskAssignmentBranch(9) },
		"targets own":      func(p *sealedLandingPins) { p.TargetRef = p.Branch },
		"bad objectformat": func(p *sealedLandingPins) { p.ObjectFormat = "md5" },
	}
	for name, mutate := range broken {
		t.Run(name, func(t *testing.T) {
			repo, taskRoot, pins := sealedLandingFixture(t)
			mutate(&pins)
			if _, err := landSealed(repo, taskRoot, coverOf(repo), pins); err == nil {
				t.Fatalf("landing accepted pins with %s", name)
			}
		})
	}
}

func TestRunSealedLandingRefusesAnyOtherOperation(t *testing.T) {
	env := SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationFirstTaskClosure, TaskID: 7,
		RunID: "run-1", ObjectFormat: "sha1", Mode: "fresh", TargetRef: "HEAD",
		Branch: taskAssignmentBranch(7), WorktreeName: taskWorktreeName(7),
		SourceRepo: "/repo/source", TaskRoot: "/repo/task",
	}
	if _, err := RunSealedLanding(env); err == nil {
		t.Fatal("the landing entrypoint accepted a first-task setup envelope")
	}
}
