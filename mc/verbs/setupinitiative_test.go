package verbs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mkInitiativeBases builds the two empty host bases the resident precreates for
// an initiative cut (ADR-025 D1/D3): the store root with empty {git, source}
// children and the SEPARATE shared-worktree base.
func mkInitiativeBases(t *testing.T) (storeRoot, worktreeRoot string) {
	t.Helper()
	base := t.TempDir()
	storeRoot = filepath.Join(base, "store")
	worktreeRoot = filepath.Join(base, "wt")
	for _, d := range []string{filepath.Join(storeRoot, "git"), filepath.Join(storeRoot, "source"), worktreeRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return storeRoot, worktreeRoot
}

// wtGit runs git against the shared worktree the way the cross-base checkout
// does host-side: an explicit GIT_DIR (the linked worktree admin, whose
// commondir resolves within the store) and GIT_WORK_TREE (the separate base).
// The worktree's own container-relative .git pointer is deliberately not used.
func wtGit(t *testing.T, storeRoot, worktreeRoot string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_DIR="+filepath.Join(storeRoot, "git", "worktrees", "mc-initiative-7"),
		"GIT_WORK_TREE="+worktreeRoot,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func initiativeSpec() InitiativeSetupSpec {
	return InitiativeSetupSpec{
		InitiativeID: 7, Mode: "fresh", TargetRef: "HEAD",
		LocalRepoUUID: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	}
}

func TestMaterializeInitiativeStoreBuildsAFsckCleanOperableStore(t *testing.T) {
	src, base, objfmt := buildSourceRepo(t)
	store, wt := mkInitiativeBases(t)
	spec := initiativeSpec()
	spec.ObjectFormat = objfmt

	res, err := MaterializeInitiativeStore(src, store, wt, spec)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if res.BaseSHA != base || res.ObjectFormat != objfmt || !res.FsckClean {
		t.Fatalf("result = %+v, want cut %s / %s / fsck clean", res, base, objfmt)
	}
	if res.ObjectCount < 5 || len(res.ClosureDigest) != 64 || !assignmentHex.MatchString(res.ClosureDigest) {
		t.Fatalf("result closure = count %d digest %q", res.ObjectCount, res.ClosureDigest)
	}

	// The sole ref exists at the cut, and HEAD names the shared branch.
	refBytes, err := os.ReadFile(filepath.Join(store, "git", "refs", "heads", "mc", "initiative-7"))
	if err != nil || strings.TrimSpace(string(refBytes)) != base {
		t.Fatalf("refs/heads/mc/initiative-7 = %q (%v), want %s", refBytes, err, base)
	}
	head, _ := os.ReadFile(filepath.Join(store, "git", "HEAD"))
	if strings.TrimSpace(string(head)) != "ref: refs/heads/mc/initiative-7" {
		t.Fatalf("store HEAD = %q", head)
	}

	// store/source stays the empty structural mountpoint (ADR-025 D1) — the
	// checkout content is in the separate worktree, never here.
	if entries, _ := os.ReadDir(filepath.Join(store, "source")); len(entries) != 0 {
		t.Fatalf("store/source is not empty: %v", entries)
	}

	// The worktree carries the exact container-relative pointer bytes and an
	// empty .mission-control cover, and the checked-out tree is clean.
	dotGit, _ := os.ReadFile(filepath.Join(wt, ".git"))
	if string(dotGit) != "gitdir: ../git/worktrees/mc-initiative-7\n" {
		t.Fatalf("worktree .git = %q, want the container-relative pointer", dotGit)
	}
	mc, err := os.ReadDir(filepath.Join(wt, ".mission-control"))
	if err != nil || len(mc) != 0 {
		t.Fatalf(".mission-control cover = %v (%v), want an empty dir", mc, err)
	}
	if got := wtGit(t, store, wt, "rev-parse", "HEAD"); got != base {
		t.Fatalf("worktree HEAD = %s, want %s", got, base)
	}
	if got := wtGit(t, store, wt, "status", "--porcelain"); got != "" {
		t.Fatalf("materialized shared worktree is dirty:\n%s", got)
	}
	// Executable bit and symlink are preserved in the checkout.
	if info, err := os.Lstat(filepath.Join(wt, "run.sh")); err != nil || info.Mode()&0o100 == 0 {
		t.Fatalf("run.sh not executable: %v %v", info, err)
	}
	if info, err := os.Lstat(filepath.Join(wt, "link")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link is not a symlink: %v %v", info, err)
	}

	// No loose objects and no persistent alternate in the store.
	fanout, _ := filepath.Glob(filepath.Join(store, "git", "objects", "[0-9a-f][0-9a-f]"))
	if len(fanout) != 0 {
		t.Fatalf("the initiative store holds loose objects: %v", fanout)
	}
	if _, err := os.Lstat(filepath.Join(store, "git", "objects", "info", "alternates")); !os.IsNotExist(err) {
		t.Fatal("the initiative store carries a persistent alternate")
	}
	// The generated config is the same closed key set the task store uses.
	cfg, _ := os.ReadFile(filepath.Join(store, "git", "config"))
	for _, want := range []string{"repositoryformatversion = 1", "relativeWorktrees = true", "localRepoUuid = " + spec.LocalRepoUUID} {
		if !strings.Contains(string(cfg), want) {
			t.Fatalf("generated config missing %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(string(cfg), "hooksPath") || strings.Contains(string(cfg), "[remote") {
		t.Fatalf("generated config carries a forbidden key:\n%s", cfg)
	}
}

func TestMaterializeInitiativeStoreRetryPinsTheExactCutSHA(t *testing.T) {
	src, head, objfmt := buildSourceRepo(t)
	older := srcGit(t, src, "rev-parse", "HEAD~1")
	if older == head {
		t.Fatal("fixture needs two commits")
	}
	store, wt := mkInitiativeBases(t)
	spec := initiativeSpec()
	spec.Mode = "retry"
	spec.PinnedBaseSHA = older
	spec.ObjectFormat = objfmt

	res, err := MaterializeInitiativeStore(src, store, wt, spec)
	if err != nil {
		t.Fatalf("retry materialize: %v", err)
	}
	// A retry pins the recorded cut, never re-resolving to the moved main (D3).
	if res.BaseSHA != older {
		t.Fatalf("retry cut = %s, want the pinned %s (not HEAD %s)", res.BaseSHA, older, head)
	}
	if got := wtGit(t, store, wt, "rev-parse", "HEAD"); got != older {
		t.Fatalf("retry worktree HEAD = %s, want pinned %s", got, older)
	}
}

func TestMaterializeInitiativeStoreRefusesObjectFormatMismatch(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	store, wt := mkInitiativeBases(t)
	spec := initiativeSpec()
	if objfmt == "sha256" {
		spec.ObjectFormat = "sha1"
	} else {
		spec.ObjectFormat = "sha256"
	}
	if _, err := MaterializeInitiativeStore(src, store, wt, spec); err == nil {
		t.Fatal("materialize accepted a source object format mismatch")
	}
}

func TestMaterializeInitiativeStoreRefusesResidueInAWorktree(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	store, wt := mkInitiativeBases(t)
	spec := initiativeSpec()
	spec.ObjectFormat = objfmt
	if err := os.WriteFile(filepath.Join(wt, "residue"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeInitiativeStore(src, store, wt, spec); err == nil {
		t.Fatal("materialize overwrote a non-empty shared worktree")
	}
}

func freshInitiativeEnvelope(src, storeRoot, worktreeRoot, objfmt string) SetupEnvelope {
	return SetupEnvelope{
		SchemaVersion: 1, Operation: SetupOperationInitiativeSetup,
		RunID: "setup-run", TaskID: 7, Mode: "fresh", ObjectFormat: objfmt,
		TargetRef: "HEAD", Branch: "mc/initiative-7", WorktreeName: "mc-initiative-7",
		SourceRepo: src, TaskRoot: storeRoot, WorktreeRoot: worktreeRoot,
	}
}

func TestRunInitiativeSetupFreshMaterializesAndReports(t *testing.T) {
	src, base, objfmt := buildSourceRepo(t)
	store, wt := mkInitiativeBases(t)
	res, err := RunInitiativeSetup(freshInitiativeEnvelope(src, store, wt, objfmt))
	if err != nil {
		t.Fatalf("fresh initiative setup: %v", err)
	}
	if res.BaseSHA != base || !res.FsckClean || res.ObjectCount < 5 {
		t.Fatalf("result = %+v, want cut %s and a clean nonempty closure", res, base)
	}
	if _, err := os.Stat(filepath.Join(store, "git", "refs", "heads", "mc", "initiative-7")); err != nil {
		t.Fatalf("fresh setup left no store ref: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Fatalf("fresh setup did not check out the shared worktree: %v", err)
	}
}

func TestRunInitiativeSetupRetryReproducesThePinnedClosure(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	store0, wt0 := mkInitiativeBases(t)
	fresh, err := RunInitiativeSetup(freshInitiativeEnvelope(src, store0, wt0, objfmt))
	if err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	store, wt := mkInitiativeBases(t)
	env := freshInitiativeEnvelope(src, store, wt, objfmt)
	env.Mode = "retry"
	env.TargetRef = ""
	env.PinnedBaseSHA, env.PinnedClosureDigest, env.PinnedLocalRepoUUID = fresh.BaseSHA, fresh.ClosureDigest, fresh.LocalRepoUUID
	res, err := RunInitiativeSetup(env)
	if err != nil {
		t.Fatalf("retry initiative setup: %v", err)
	}
	if res.ClosureDigest != fresh.ClosureDigest || res.BaseSHA != fresh.BaseSHA || res.LocalRepoUUID != fresh.LocalRepoUUID {
		t.Fatalf("retry result %+v did not reproduce the pinned closure %+v", res, fresh)
	}
}

// A retry that finds its OWN prior store (a prior attempt that materialized
// before its receipt was recorded) is accepted idempotently, not refused as
// residue (ADR-025 D3).
func TestRunInitiativeSetupRetryAcceptsExactResidueIdempotently(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	store, wt := mkInitiativeBases(t)
	fresh, err := RunInitiativeSetup(freshInitiativeEnvelope(src, store, wt, objfmt))
	if err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	env := freshInitiativeEnvelope(src, store, wt, objfmt) // same store — residue present
	env.Mode = "retry"
	env.TargetRef = ""
	env.PinnedBaseSHA, env.PinnedClosureDigest, env.PinnedLocalRepoUUID = fresh.BaseSHA, fresh.ClosureDigest, fresh.LocalRepoUUID
	res, err := RunInitiativeSetup(env)
	if err != nil {
		t.Fatalf("idempotent residue retry: %v", err)
	}
	if res.BaseSHA != fresh.BaseSHA || res.ClosureDigest != fresh.ClosureDigest {
		t.Fatalf("residue verify = %+v, want the recorded cut", res)
	}
	// A divergent pinned digest against the same residue refuses without overwriting.
	env.PinnedClosureDigest = strings.Repeat("0", 64)
	if _, err := RunInitiativeSetup(env); err == nil {
		t.Fatal("residue retry accepted a divergent pinned closure digest")
	}
}

// The initiative arm is a closed union member: its second container root never
// rides another arm, and it carries no accepted-seal authority.
func TestValidateInitiativeSetupEnvelopeIsAClosedArm(t *testing.T) {
	base := freshInitiativeEnvelope("/src", "/store", "/wt", "sha1")
	if err := validateSetupEnvelope(base); err != nil {
		t.Fatalf("a valid initiative envelope was rejected: %v", err)
	}
	bad := map[string]func(e *SetupEnvelope){
		"branch":           func(e *SetupEnvelope) { e.Branch = "mc/initiative-8" },
		"worktree_name":    func(e *SetupEnvelope) { e.WorktreeName = "mc-initiative-8" },
		"no_worktree_root": func(e *SetupEnvelope) { e.WorktreeRoot = "" },
		"no_target":        func(e *SetupEnvelope) { e.TargetRef = "" },
		"seal_authority":   func(e *SetupEnvelope) { e.SealedSHA = strings.Repeat("a", 40) },
	}
	for name, mut := range bad {
		e := base
		mut(&e)
		if err := validateSetupEnvelope(e); err == nil {
			t.Fatalf("%s: a malformed initiative envelope was accepted", name)
		}
	}
	// The WorktreeRoot field never rides a non-initiative arm.
	ft := freshEnvelope("/src", "/task", "sha1")
	ft.WorktreeRoot = "/wt"
	if err := validateSetupEnvelope(ft); err == nil {
		t.Fatal("a first-task envelope carrying a worktree root was accepted")
	}
}

func TestMaterializeInitiativeStoreRefusesReservedRootComponent(t *testing.T) {
	src, _, objfmt := buildSourceRepo(t)
	// .mc-worktrees at the tree top must be refused (ADR-025 D10) — a child could
	// otherwise commit a path that collides with the live worktree at merge.
	if err := os.MkdirAll(filepath.Join(src, ".mc-worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, ".mc-worktrees", "keep"), "x\n")
	srcGit(t, src, "add", "-A")
	srcGit(t, src, "commit", "-qm", "reserved")
	store, wt := mkInitiativeBases(t)
	spec := initiativeSpec()
	spec.ObjectFormat = objfmt
	if _, err := MaterializeInitiativeStore(src, store, wt, spec); err == nil {
		t.Fatal("materialize accepted a base tree with a reserved .mc-worktrees root")
	}
}
