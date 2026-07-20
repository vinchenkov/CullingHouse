package verbs

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// The sealed lander (ADR-017:738-761), step 4 of the landing lane.
//
// This file holds the two things every later stage sits on: the fenced git
// wrapper, and the real-repository revalidation.
//
// WHY THE WRAPPER IS A TYPE AND NOT A HELPER. The legacy lander
// (`runner/image/mc-land`) builds a carefully isolated view for its merge, then
// runs a long tail of bare `git` calls outside it — the receipt scan,
// symbolic-ref, show-ref, merge-base, diff-tree, worktree list, and the
// cat-file inside its xargs blocks — with the operator's live config and hooks
// in scope. Every one of those is read-only plumbing, so no hook fires today
// and the isolation looks fine. It is accidental. ADR-017:704-711 asks for a
// "cleared environment plus generated safe Git configuration" as a property of
// the landing PROGRAM, not of one command inside it. Routing every call through
// a type whose only entry point applies the fences makes forgetting one a
// compile-time impossibility rather than a review question.

// landingGitEnv is the complete environment for a landing git call. It is
// CONSTRUCTED, never derived from os.Environ(): that is what makes a hostile
// GIT_DIR, GIT_INDEX_FILE, GIT_ALTERNATE_OBJECT_DIRECTORIES, or forged
// GIT_AUTHOR_*/GIT_COMMITTER_* structurally unable to reach git, rather than
// something each call site has to remember to override.
//
// PATH is the one inherited value — git needs to find its own helpers — and it
// is inherited rather than fixed because the container's git is pinned by the
// image, not by this process.
func landingGitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		// A refs/replace entry must not substitute different bytes for a
		// reviewed object under the lander's feet.
		"GIT_NO_REPLACE_OBJECTS=1",
		// The operator's system and global config are out of scope: they can
		// both mask a forbidden repository state and inject one.
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
		// Landing runs network=none; a prompt would hang to the deadline.
		"GIT_TERMINAL_PROMPT=0",
		// Refuse to take opportunistic index locks while inspecting.
		"GIT_OPTIONAL_LOCKS=0",
		"HOME=" + os.TempDir(),
	}
}

// landingFixedConfig is the `-c` prefix on every landing git call: the controls
// that would otherwise let repository-local config execute something.
//
// core.checkStat and core.trustctime are the stat-cache pair legacy pinned: a
// repository can set them so git's dirty detection skips a same-length edit,
// hiding operator bytes from the fence meant to protect them.
//
// MEASURED TWICE, and NOT load-bearing in either place, contrary to what an
// earlier version of this comment predicted:
//
//   - `git diff --name-only HEAD -- <paths>` (the 4c dirty fence) detects a
//     same-length, mtime-restored edit with `checkStat=minimal` and
//     `trustctime=false` set in the repository and these overrides removed.
//   - so does `read-tree HEAD` + `add -u` + `write-tree` on a disposable
//     index, which this comment previously claimed was the place they WOULD
//     matter.
//
// The reason is structural rather than configurational, and worth knowing
// before relying on either flag: `read-tree` into a FRESH index produces no
// stat cache at all, so `add -u` has nothing to trust and must re-read every
// file. A disposable index is what defeats stat-cache evasion, not these two
// settings.
//
// Retained as cheap defence in depth for any future path that reuses the
// repository's PERSISTENT index, where the cache genuinely is consulted — and
// labelled so the next reader does not count them as live anywhere today.
func landingFixedConfig() []string {
	return []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.attributesFile=/dev/null",
		"-c", "core.excludesFile=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "core.checkStat=default",
		"-c", "core.trustctime=true",
		"-c", "gc.auto=0",
		"-c", "protocol.allow=never",
	}
}

// landingGit is the ONLY way the sealed lander invokes git.
type landingGit struct {
	dir string
}

func (g landingGit) run(args ...string) ([]byte, error) {
	return gitOutput(g.dir, landingGitEnv(), nil, append(landingFixedConfig(), args...)...)
}

// out runs a command and returns its trimmed stdout.
func (g landingGit) out(args ...string) (string, error) {
	body, err := g.run(args...)
	return strings.TrimSpace(string(body)), err
}

// landingRepoFacts is what the repository stage establishes for the stages
// after it. TargetSHA is the pre-merge SHA — the target ref's tip observed
// under the fences — which the envelope also carries frozen from attest, so the
// next stage can prove the target has not moved since the plan was built.
type landingRepoFacts struct {
	Toplevel  string
	TargetSHA string
}

// revalidateLandingRepository proves the RW anchor is the exact real
// repository the plan named, that its local controls cannot execute anything,
// and that no operator operation is already in flight. ADR-017:741-743's "safe
// local controls, real repository/target identity".
//
// It deliberately does NOT apply a dirty fence. The spec's fence is scoped to
// the REVIEWED paths (ADR-017:742), and the reviewed set is not known until the
// closure stage; a global dirty check here would refuse an operator who merely
// has unrelated work in progress, which legacy explicitly permits and which is
// the difference between a usable system and one that only lands on a pristine
// tree.
func revalidateLandingRepository(workspace, target string) (landingRepoFacts, error) {
	if !validLandingTargetBranch(target) {
		return landingRepoFacts{}, Domainf("landing target %q is not a bare local branch name", target)
	}
	g := landingGit{dir: workspace}

	// A configured core.worktree redirects the checkout somewhere the plan
	// never attested. Checked FIRST: every later answer is about the wrong tree
	// if this one is set.
	if value, err := g.out("config", "--local", "--get", "core.worktree"); err == nil && value != "" {
		return landingRepoFacts{}, Domainf("landing repository configures core.worktree; the primary checkout is redirected")
	}
	if bare, err := g.out("rev-parse", "--is-bare-repository"); err != nil {
		return landingRepoFacts{}, Domainf("landing anchor is not a git repository: %v", err)
	} else if bare != "false" {
		return landingRepoFacts{}, Domainf("landing repository is bare; there is no primary checkout to merge in (ADR-017:699)")
	}

	// The anchor must be the repository's OWN toplevel. A subdirectory resolves
	// to a perfectly valid repository, so without this the RW grant would
	// attach to "somewhere inside the Worksource" rather than to the exact
	// registered root. Both sides are symlink-resolved before comparison: the
	// mount may reach the same directory by a different spelling.
	top, err := g.out("rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return landingRepoFacts{}, Domainf("landing repository has no resolvable toplevel: %v", err)
	}
	wantTop, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return landingRepoFacts{}, Domainf("landing anchor does not resolve: %v", err)
	}
	gotTop, err := filepath.EvalSymlinks(top)
	if err != nil {
		return landingRepoFacts{}, Domainf("landing repository toplevel does not resolve: %v", err)
	}
	if gotTop != wantTop {
		return landingRepoFacts{}, Domainf("landing anchor %q is not the repository toplevel %q", workspace, top)
	}

	// Repository-local config must not be able to execute anything. Scope is
	// pinned to local+worktree so the `-c` fences above (which appear in an
	// unscoped listing) cannot mask or manufacture a hit.
	if err := refuseExecutableLandingConfig(g); err != nil {
		return landingRepoFacts{}, err
	}

	// The target must be a real, non-symbolic local branch. A symbolic ref
	// would let a merge into `target` land on whatever branch it aliases —
	// an operator branch the plan never named.
	//
	// REDUNDANT, retained deliberately, and measured rather than assumed. This
	// fence has NO independently reachable scenario: git resolves symref chains
	// transitively, so with `refs/heads/T` symbolic, `symbolic-ref --short HEAD`
	// returns the name at the END of the chain, never T — and the HEAD-on-target
	// fence below therefore refuses every input this one would. Mutating this
	// condition to `false` leaves the suite green, twice over, for two different
	// reasons (verified 2026-07-20). It is kept because it runs FIRST, so it is
	// the fence that actually refuses in production and the one whose message
	// names the real problem; and because it states the invariant directly
	// instead of relying on a coincidence of git's resolution order. The test
	// case for a symbolic target is labelled for what it really proves.
	ref := "refs/heads/" + target
	if _, err := g.out("symbolic-ref", "-q", ref); err == nil {
		return landingRepoFacts{}, Domainf("landing target %q is a symbolic ref", target)
	}
	if _, err := g.out("show-ref", "--verify", "--quiet", ref); err != nil {
		return landingRepoFacts{}, Domainf("landing target branch %q does not exist", target)
	}
	tip, err := g.out("rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return landingRepoFacts{}, Domainf("landing target %q does not resolve to a commit: %v", target, err)
	}

	// The merge happens in the primary checkout, so HEAD must already be on the
	// target. A detached HEAD or a different branch means merging would move
	// something other than the target.
	head, err := g.out("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || head == "" {
		return landingRepoFacts{}, Domainf("landing repository HEAD is detached; it must be on the target branch")
	}
	if head != target {
		return landingRepoFacts{}, Domainf("landing repository HEAD is on %q, not the target %q", head, target)
	}

	// An index that hides paths defeats every later content comparison: an
	// --assume-unchanged or --skip-worktree entry makes git report a modified
	// operator file as clean, so the reviewed-path fence would pass over bytes
	// it is meant to protect.
	if err := refuseHiddenLandingIndexEntries(g); err != nil {
		return landingRepoFacts{}, err
	}

	// An operator merge already in flight is never ours to touch. Landing
	// refuses rather than aborting someone else's work.
	if _, err := os.Lstat(filepath.Join(gitDirOf(g, workspace), "MERGE_HEAD")); err == nil {
		return landingRepoFacts{}, Domainf("landing repository has a merge already in progress; refusing to disturb it")
	}
	return landingRepoFacts{Toplevel: gotTop, TargetSHA: tip}, nil
}

// refuseExecutableLandingConfig rejects repository config that can run a
// program during a merge: custom merge drivers and content filters.
func refuseExecutableLandingConfig(g landingGit) error {
	out, err := g.run("config", "--list", "--name-only", "--show-scope")
	if err != nil {
		// No config to list is not an error condition for an empty repository.
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		scope, key := fields[0], strings.ToLower(fields[1])
		if scope != "local" && scope != "worktree" {
			continue
		}
		if strings.HasPrefix(key, "merge.") && strings.HasSuffix(key, ".driver") {
			return Domainf("landing repository configures an executable git merge driver (%s)", key)
		}
		if strings.HasPrefix(key, "filter.") &&
			(strings.HasSuffix(key, ".clean") || strings.HasSuffix(key, ".smudge") || strings.HasSuffix(key, ".process")) {
			return Domainf("landing repository configures an executable git content filter (%s)", key)
		}
	}
	return nil
}

// refuseHiddenLandingIndexEntries rejects an index carrying visibility flags.
// `ls-files -v` tags each path: lowercase means --assume-unchanged, 'S' means
// --skip-worktree. Either makes git lie about whether a file is modified.
func refuseHiddenLandingIndexEntries(g landingGit) error {
	out, err := g.run("ls-files", "-v")
	if err != nil {
		return Domainf("landing repository index is unreadable: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		tag := line[0]
		if tag == 'S' || (tag >= 'a' && tag <= 'z') {
			return Domainf("landing repository index carries visibility flags; a reviewed path could be hidden from the dirty fence")
		}
	}
	return nil
}

// gitDirOf resolves the repository's administrative directory, falling back to
// the conventional location so a probe never silently succeeds against nothing.
func gitDirOf(g landingGit, workspace string) string {
	if dir, err := g.out("rev-parse", "--path-format=absolute", "--git-dir"); err == nil && dir != "" {
		return dir
	}
	return filepath.Join(workspace, ".git")
}

// landingStorePins are the frozen facts the landing instruction carries. They
// are INPUTS to match the sealed store against, never values read off it.
type landingStorePins struct {
	TaskID        int64
	ObjectFormat  string
	VerifiedSHA   string
	LocalRepoUUID string
}

// landingStoreFacts is what the sealed stage establishes for the stages after
// it: the reviewed commit and its tree, both proven to be the assignment's.
type landingStoreFacts struct {
	Head string
	Tree string
}

// revalidateSealedTaskStore proves the RO task root holds exactly the sealed
// artifact the immutable assignment named (ADR-017:741-743).
//
// NOT `inspectCompletableTaskStore`, deliberately. That function asks whether a
// store is COMPLETABLE — its producer holds it RW and the identity facts are
// OUTPUTS to be recorded. This asks whether a store IS the exact sealed
// artifact already named: held RO, in a different effect class, with the
// identity facts as INPUTS to match. The shapes rhyme and the direction of
// proof is opposite, so sharing one function would mean a parameter that
// selects which of two security questions is being asked. It also runs under
// the LANDING fences rather than `sourceGitEnv()`, which lacks
// GIT_NO_REPLACE_OBJECTS and a hooks override — and the sealed store is
// attacker-shaped input to this class, not our own scratch space.
// [owed: if a third consumer appears, unify the structural half deliberately]
func revalidateSealedTaskStore(taskRoot string, pins landingStorePins) (landingStoreFacts, error) {
	if pins.TaskID < 1 {
		return landingStoreFacts{}, Domainf("sealed store pins name no task")
	}
	if err := validateSetupObjectFormat(pins.ObjectFormat); err != nil {
		return landingStoreFacts{}, err
	}
	source := filepath.Join(taskRoot, "source")
	gitDir := filepath.Join(taskRoot, "git")
	g := landingGit{dir: source}

	// The config must reproduce the generated fixed grammar EXACTLY, byte for
	// byte, for the frozen format and uuid. Exact reproduction is what makes
	// this a closed grammar rather than an allowlist: there is no room for an
	// extra section, so no filter, hook path, or alternate can be declared.
	body, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		return landingStoreFacts{}, Domainf("sealed store config is unreadable: %v", err)
	}
	if err := validateTaskGitConfig(body); err != nil {
		return landingStoreFacts{}, Domainf("sealed store config is not the fixed grammar: %v", err)
	}
	if string(body) != string(generatedTaskGitConfig(pins.ObjectFormat, pins.LocalRepoUUID)) {
		return landingStoreFacts{}, Domainf("sealed store config does not reproduce the frozen object format and repository UUID")
	}
	if fileNonEmpty(filepath.Join(gitDir, "objects", "info", "alternates")) {
		return landingStoreFacts{}, Domainf("sealed store has object alternates; its closure would not be self-contained")
	}

	// NO separate `rev-parse --show-object-format` check here, and that is a
	// deliberate removal rather than an omission. A draft had one. It was
	// TAUTOLOGICAL: git derives the reported object format BY READING
	// `extensions.objectFormat` from this same config file, which the exact
	// reproduction above has already pinned byte for byte. Asking git to read a
	// file we just compared against a generated constant is not a second
	// opinion — it is the same opinion, laundered through a subprocess, and it
	// would read as an independent fence to anyone auditing this list.
	// Mutation confirmed it: disabling it left the suite green.

	// The reviewed artifact must be pristine. Unlike the real repository's
	// path-scoped fence, this side admits NO dirt at all: any modification here
	// is a modification of the thing under review.
	status, err := g.out("status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return landingStoreFacts{}, Domainf("sealed store status is unreadable: %v", err)
	}
	if status != "" {
		return landingStoreFacts{}, Domainf("sealed store worktree is not clean; the reviewed artifact has been modified")
	}

	// Exactly one ref, the managed branch. This is also what keeps
	// `refs/replace/*` out — a replacement could substitute different bytes for
	// a reviewed object, and it lives under refs/ like any other ref.
	branch := taskAssignmentBranch(pins.TaskID)
	refs, err := g.out("for-each-ref", "--format=%(refname)")
	if err != nil {
		return landingStoreFacts{}, Domainf("sealed store refs are unreadable: %v", err)
	}
	if refs != "refs/heads/"+branch {
		return landingStoreFacts{}, Domainf("sealed store does not hold exactly its managed branch %q", branch)
	}
	if head, err := g.out("symbolic-ref", "--quiet", "HEAD"); err != nil || head != "refs/heads/"+branch {
		return landingStoreFacts{}, Domainf("sealed store HEAD is not its managed branch")
	}

	// The reviewed commit must be the exact verified SHA the assignment froze.
	head, err := g.out("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || len(head) != oidLen(pins.ObjectFormat) || !assignmentHex.MatchString(head) {
		return landingStoreFacts{}, Domainf("sealed store HEAD is not a canonical commit")
	}
	if head != pins.VerifiedSHA {
		return landingStoreFacts{}, Domainf("sealed store HEAD %q is not the frozen verified SHA %q", head, pins.VerifiedSHA)
	}
	tree, err := g.out("rev-parse", "--verify", "HEAD^{tree}")
	if err != nil || len(tree) != oidLen(pins.ObjectFormat) || !assignmentHex.MatchString(tree) {
		return landingStoreFacts{}, Domainf("sealed store tree is not canonical")
	}
	if err := fsckClean(gitDir); err != nil {
		return landingStoreFacts{}, err
	}
	return landingStoreFacts{Head: head, Tree: tree}, nil
}

// landingMergeBase returns the single merge base of the target branch and the
// reviewed commit, refusing anything but exactly one.
//
// More than one base means a criss-cross history where "what the merge writes"
// is not a single well-defined diff, so the reviewed-path set the dirty fence
// depends on would be a guess. Zero means the reviewed commit shares no history
// with the target at all. Legacy required uniqueness for the same reason; the
// caller additionally binds the result to the assignment's frozen base, which
// is what proves the target has not been rewritten out from under the review.
func landingMergeBase(g landingGit, target, verifiedSHA string) (string, error) {
	out, err := g.out("merge-base", "--all", "refs/heads/"+target, verifiedSHA)
	if err != nil || out == "" {
		return "", Domainf("reviewed commit shares no history with the landing target %q", target)
	}
	bases := strings.Fields(out)
	if len(bases) != 1 {
		return "", Domainf("landing target and reviewed commit have %d merge bases; the reviewed change is not a single well-defined diff", len(bases))
	}
	return bases[0], nil
}

// reviewedLandingPaths is the set of paths the reviewed change touches: the
// diff from the base to the reviewed commit.
//
// Renames are deliberately NOT detected (`--no-renames`). Rename inference is a
// heuristic, and a heuristic here would silently relocate a reviewed edit onto
// a path the reviewer never saw — legacy pinned exactly that attack.
//
// MEASURED: `--no-renames` is currently INERT on this command. `diff-tree` is
// plumbing and ignores `diff.renames`, so a repository cannot switch rename
// detection on; with the config set to true both the source and destination
// paths are still reported. The flag is retained as an explicit statement of
// intent — it is what stops a future edit from adding `-M` and silently
// shrinking the reviewed set — but it is not a live fence and mutating it away
// leaves the suite green. Recorded so it is not counted as one.
//
// External diff drivers and textconv are off for the reason everything is off
// in this class: they execute.
func reviewedLandingPaths(g landingGit, base, verifiedSHA string) ([]string, error) {
	out, err := g.run("diff-tree", "-r", "--no-commit-id", "--name-only", "--no-renames",
		"--no-ext-diff", "--no-textconv", "-z", base, verifiedSHA)
	if err != nil {
		return nil, Domainf("landing cannot derive the reviewed path set: %v", err)
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil, Domainf("the reviewed change touches no paths")
	}
	return paths, nil
}

// fenceReviewedPathsClean is ADR-017:742's "primary dirty-tree fence", scoped
// to the reviewed paths.
//
// SCOPED, not global, and that is a decision rather than an optimization. A
// global check refuses an operator who merely has unrelated work in progress,
// which makes landing usable only against a pristine tree. The paths the merge
// will write must be pristine; everything else is the operator's business.
func fenceReviewedPathsClean(g landingGit, workspace, target, base, verifiedSHA string) error {
	paths, err := reviewedLandingPaths(g, base, verifiedSHA)
	if err != nil {
		return err
	}
	return fenceReviewedPathsCleanAt(g, workspace, target, paths)
}

// fenceReviewedPathsCleanAt is the fence itself, over an already-derived path
// set. The split exists because the composed lane derives the reviewed set from
// the SEALED store, not from the real repository: the real repository cannot
// name the reviewed commit until the import has happened, so deriving the set
// there would force the dirty fence AFTER the import and invert
// ADR-017:741-743's order. The two-argument form above is retained for the
// single-repository case its own tests cover.
//
// It deliberately does NOT re-check that the set is non-empty. `reviewedLandingPaths`
// is the only producer of a path set and already refuses an empty one, so a
// guard here could never fire — a DEAD fence, which is not a fence.
func fenceReviewedPathsCleanAt(g landingGit, workspace, target string, paths []string) error {

	// Tracked modifications at reviewed paths, worktree AND index against HEAD.
	// The fixed config overrides core.checkStat/core.trustctime, so a repo that
	// configures git to skip content comparison cannot hide an edit here.
	args := append([]string{"diff", "--name-only", "HEAD", "--"}, paths...)
	dirty, err := g.out(args...)
	if err != nil {
		return Domainf("landing cannot compare reviewed paths against HEAD: %v", err)
	}
	if dirty != "" {
		return Domainf("reviewed path(s) are modified in the primary checkout: %s", strings.Join(strings.Fields(dirty), " "))
	}

	// Paths the merge will CREATE but which already exist on disk. Git reports
	// nothing for these — they are untracked, so no diff mentions them — and the
	// merge would overwrite operator bytes without a trace. An IGNORED file is
	// the same hazard and the likelier one: a .env sitting exactly where a
	// reviewed file is about to land. Checked against the filesystem rather than
	// `ls-files --others`, because --exclude-standard would omit precisely the
	// ignored files that matter and dropping it would list every ignored file in
	// the tree.
	for _, p := range paths {
		inTarget := true
		if _, err := g.out("cat-file", "-e", "refs/heads/"+target+"^{tree}:"+p); err != nil {
			inTarget = false
		}
		abs := filepath.Join(workspace, p)
		if !inTarget {
			if _, err := os.Lstat(abs); err == nil {
				return Domainf("reviewed path %q will be created by the merge but already exists untracked in the primary checkout", p)
			}
		}
		// Every ancestor component must be a directory or absent. A regular
		// file where the merge needs a directory fails midway through the merge
		// rather than before it.
		rel := filepath.Dir(p)
		for rel != "." && rel != string(filepath.Separator) && rel != "" {
			info, err := os.Lstat(filepath.Join(workspace, rel))
			if err == nil && !info.IsDir() {
				return Domainf("reviewed path %q needs directory %q, which is occupied by a non-directory", p, rel)
			}
			rel = filepath.Dir(rel)
		}
	}
	return nil
}

// inodeOf extracts an inode number from a FileInfo, for the hardlink fence.
func inodeOf(info os.FileInfo) (uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Ino), true
}

// importSealedClosure imports the reviewed closure into the real object store
// (ADR-017:743-747).
//
// The shape is `pack-objects --revs --stdout` in the SEALED store, piped to
// `index-pack --stdin` in the REAL one. That pairing is what satisfies the
// ADR's three prohibitions structurally rather than by discipline:
//
//   - no HARDLINK, because objects arrive as a byte stream over a pipe; there
//     is no filesystem operation that could link them. A hardlinked import
//     would leave the sealed root's bytes mutable through the real repository
//     and defeat the RO mount.
//   - no ALTERNATE, because nothing writes objects/info/alternates. An
//     alternate would break the real repository the moment the landing
//     container exits and the mount disappears.
//   - no SPECULATIVE DELETION, because index-pack only ever adds. A crash here
//     leaves unreachable but valid objects, which is explicitly acceptable:
//     retry re-imports the same closure and git deduplicates by hash.
//
// The pack is bounded to `verified` minus `base` — the reviewed closure only,
// not the whole history the real repository already has.
//
// It creates NO ref. Ref creation is the next stage's durable import marker
// (ADR-017:748); doing it here would make a crash mid-import indistinguishable
// from a completed one.
func importSealedClosure(sealedRepo, realRepo, base, verifiedSHA string) error {
	sealed := landingGit{dir: sealedRepo}
	// Prove the sealed store actually holds the reviewed commit and the frozen
	// base before building anything.
	//
	// REDUNDANT and retained: `pack-objects --revs` rejects an unknown rev on
	// its own, so mutating these two away leaves the suite green. They are kept
	// because they fail FAST with a message naming which of the two revs is
	// missing, where pack-objects reports a generic bad-revision error from a
	// subprocess. Not counted as a live fence.
	if _, err := sealed.out("cat-file", "-e", verifiedSHA+"^{commit}"); err != nil {
		return Domainf("sealed store does not hold the reviewed commit %s", verifiedSHA)
	}
	if _, err := sealed.out("cat-file", "-e", base+"^{commit}"); err != nil {
		return Domainf("sealed store does not hold the frozen base %s", base)
	}

	// `--revs` reads the rev range from stdin: everything reachable from the
	// reviewed commit, minus everything reachable from the base.
	revs := verifiedSHA + "\n^" + base + "\n"
	pack, err := gitOutput(sealedRepo, landingGitEnv(), []byte(revs),
		append(landingFixedConfig(), "pack-objects", "--revs", "--stdout", "--delta-base-offset")...)
	if err != nil {
		return Domainf("landing could not build the reviewed closure pack: %v", err)
	}
	// The pack must actually CONTAIN objects. Checking `len(pack) == 0` does
	// not establish that and never fires: MEASURED, an empty rev range still
	// produces a 32-byte pack — a 12-byte header plus a 20-byte trailer — so a
	// length test is dead code wearing the look of a fence. The object count is
	// in the header, so read it.
	//
	// This is reachable: a reviewed commit equal to the frozen base has an
	// empty closure, and importing nothing and proceeding to create a ref and
	// merge would land a no-op change while reporting success.
	count, err := packObjectCount(pack)
	if err != nil {
		return err
	}
	if count == 0 {
		return Domainf("the reviewed closure is empty; %s adds nothing to %s", verifiedSHA, base)
	}

	// index-pack fsyncs the pack and its index; core.fsync covers the loose and
	// derived metadata git writes alongside. ADR-017:744 wants the imported
	// objects durable before the ref that points at them exists.
	fsync := []string{"-c", "core.fsync=objects,derived-metadata,index", "-c", "core.fsyncMethod=fsync"}
	args := append(append(landingFixedConfig(), fsync...), "index-pack", "--stdin")
	if _, err := gitOutput(realRepo, landingGitEnv(), pack, args...); err != nil {
		return Domainf("landing could not index the reviewed closure into the real store: %v", err)
	}

	// Verify, in the REAL store, that the whole closure is now readable. The
	// pack having been written is not the same claim, and ADR-017:744 asks for
	// verification explicitly ("then fsyncs and verifies the imported
	// objects").
	//
	// NO REACHABLE FAILURE in the fast lane: index-pack validates the pack it
	// writes, so producing a store where it succeeded yet the objects are
	// unreadable would mean corrupting the pack between write and read.
	// Mutating either check away leaves the suite green. Retained because the
	// ADR mandates the verification, not because a test drives it — recorded
	// here so the next reader does not mistake it for a fence with a scenario.
	real := landingGit{dir: realRepo}
	if _, err := real.out("cat-file", "-e", verifiedSHA+"^{commit}"); err != nil {
		return Domainf("the reviewed commit is not readable in the real store after import: %v", err)
	}
	listed, err := real.out("rev-list", "--objects", verifiedSHA, "--not", base)
	if err != nil {
		return Domainf("the imported closure is not traversable in the real store: %v", err)
	}
	for _, line := range strings.Split(listed, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if _, err := real.out("cat-file", "-e", fields[0]); err != nil {
			return Domainf("imported closure object %s is not readable in the real store", fields[0])
		}
	}
	return nil
}

// packObjectCount reads the object count from a pack header. The format is
// fixed: "PACK" magic, a 4-byte version, then a 4-byte big-endian count.
func packObjectCount(pack []byte) (uint32, error) {
	if len(pack) < 12 || string(pack[0:4]) != "PACK" {
		return 0, Domainf("landing produced something that is not a pack")
	}
	return binary.BigEndian.Uint32(pack[8:12]), nil
}

// The fixed landing identity (ADR-017:752's "fixed metadata"). Pinned so the
// merge commit is byte-reproducible on replay and no environment can forge it.
const (
	landingIdentityName  = "mission control"
	landingIdentityEmail = "mc@mission-control.invalid"
)

// createLandingRefCAS creates `refs/heads/mc/task-<id>` at the reviewed commit,
// compare-and-swap against ABSENCE (ADR-017:748-750).
//
// The zero old-value is the whole mechanism: `update-ref <ref> <new> ""` means
// "create this, and fail if it exists at all". That makes the ref a durable
// stage marker — its existence at the reviewed SHA proves the import completed
// — while making it impossible to clobber a ref someone else put there.
//
// `--no-deref` matters: without it a symbolic ref planted at this name would be
// followed, and the CAS would land on whatever operator branch it aliases.
//
// Retry accepts an existing ref ONLY at the same SHA (ADR-017:749). A crash
// after creation therefore replays into an accept, while any other value
// refuses and is left exactly as found.
func createLandingRefCAS(g landingGit, branch, verifiedSHA string) error {
	ref := "refs/heads/" + branch
	// A symbolic ref here is never ours to write through, and `update-ref
	// --no-deref` would REPLACE it rather than refuse, so it is checked first.
	if _, err := g.out("symbolic-ref", "-q", ref); err == nil {
		return Domainf("landing ref %q is a symbolic ref; refusing to write through it", ref)
	}
	if existing, err := g.out("rev-parse", "--verify", "-q", ref); err == nil && existing != "" {
		if existing == verifiedSHA {
			return nil // Response-loss replay of a completed create.
		}
		return Domainf("landing ref %q already exists at %s, not the reviewed %s", ref, existing, verifiedSHA)
	}
	// The zero old-value is the ATOMIC fence: `update-ref <ref> <new> ""` fails
	// at the ref lock with "reference already exists". The rev-parse check above
	// is a message-improving convenience with a TOCTOU window, NOT the guard —
	// a concurrent creator landing between the two is exactly what this closes.
	// That race is not reachable from the fast lane, so the mechanism is pinned
	// directly by TestUpdateRefZeroOldValueRefusesAnExistingRef.
	if _, err := g.run("update-ref", "--no-deref", ref, verifiedSHA, ""); err != nil {
		return Domainf("landing could not compare-and-create %q: %v", ref, err)
	}
	return nil
}

// mergeSealedLanding performs ADR-017:751's required `git merge --no-ff` in the
// primary checkout, after rechecking the SHA fence.
//
// It STOPS at the merge. Cleanup — deleting the task ref, emptying the task
// root — is ADR-017:757-759's "later trusted action" that may only run after
// the spine records success, and it has neither a mount nor an owner yet.
func mergeSealedLanding(g landingGit, target, verifiedSHA, preMergeSHA, landingID string, taskID int64) error {
	ref := "refs/heads/" + target
	// Recheck the SHA fence immediately before merging (ADR-017:750). Between
	// the repository stage and here the operator may have committed, and the
	// pre-merge SHA the plan froze would no longer describe what we merge into.
	tip, err := g.out("rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return Domainf("landing target %q is unreadable at merge time: %v", target, err)
	}
	if tip != preMergeSHA {
		return Domainf("landing target moved from %s to %s since the fence; refusing to merge", preMergeSHA, tip)
	}

	message := "Land task " + strconv.FormatInt(taskID, 10) + "\n\n" +
		"MC-Landing-Id: " + landingID + "\n" +
		"MC-Landing-Task: task " + strconv.FormatInt(taskID, 10) + "\n" +
		"MC-Landing-Verified-SHA: " + verifiedSHA + "\n" +
		"MC-Landing-Target-Preimage: " + preMergeSHA + "\n"

	// Every merge-behaviour knob is pinned rather than inherited: autoStash
	// would move operator bytes, renames/directoryRenames would relocate a
	// reviewed edit onto a path nobody reviewed, and mergeOptions on the target
	// branch is a repository-controlled argument injection point.
	args := append(landingFixedConfig(),
		"-c", "merge.autoStash=false",
		"-c", "merge.log=false",
		"-c", "merge.branchdesc=false",
		"-c", "merge.renames=false",
		"-c", "merge.directoryRenames=false",
		"-c", "merge.ours.driver=false",
		"-c", "commit.gpgSign=false",
		"-c", "merge.gpgSign=false",
		// Mutually redundant with the GIT_AUTHOR_*/GIT_COMMITTER_* env below —
		// measured: either alone fixes the identity, so mutating one away leaves
		// the suite green. Both are kept because they fail differently: the env
		// is what git actually consults for a merge commit, while these survive
		// a refactor that routes through `g.run()`, which builds its environment
		// from landingGitEnv() alone and would silently drop the identity.
		"-c", "user.name="+landingIdentityName,
		"-c", "user.email="+landingIdentityEmail,
		"merge", "--no-ff", "--no-autostash", "--no-gpg-sign", "--no-log",
		"--cleanup=verbatim", "-s", "ort", "-m", message, verifiedSHA,
	)
	env := append(landingGitEnv(),
		"GIT_AUTHOR_NAME="+landingIdentityName,
		"GIT_AUTHOR_EMAIL="+landingIdentityEmail,
		"GIT_COMMITTER_NAME="+landingIdentityName,
		"GIT_COMMITTER_EMAIL="+landingIdentityEmail,
	)
	if _, err := gitOutput(g.dir, env, nil, args...); err != nil {
		return Domainf("landing merge failed: %v", err)
	}
	return nil
}
