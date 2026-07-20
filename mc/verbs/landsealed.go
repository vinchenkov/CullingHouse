package verbs

import (
	"os"
	"path/filepath"
	"strings"
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
// core.checkStat/core.trustctime are here for a specific attack legacy pinned —
// a repository can set them to make git's dirty detection ignore a same-length
// edit, hiding operator bytes from the very fence meant to protect them.
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
