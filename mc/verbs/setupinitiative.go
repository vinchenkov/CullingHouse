package verbs

// The ADR-025 D3 initiative shared-store materializer. It generalizes
// MaterializeFirstTaskStore to the two-base initiative layout (ADR-025 D1): the
// sanitized bare store lives at <workspace>/.mission-control/initiatives/
// initiative-<id> with an EMPTY structural source/ mountpoint, while the shared
// worktree checkout lives at the SEPARATE base <workspace>/.mc-worktrees/
// initiative-<id>. Because the two bases are not siblings on the host, the
// container-relative worktree pointers (.git, gitdir) do not resolve host-side
// (ADR-025 D1) — so the checkout is driven with an explicit GIT_DIR (the linked
// worktree admin, whose commondir ../.. DOES resolve within the store) and
// GIT_WORK_TREE (the separate worktree base), never through the pointers. The
// pointers are written verbatim for the in-container child, which sees both
// bases under /workspace.
//
// Like the first-task materializer it runs inside the network=none setup
// container with no spine; the host records the cut SHA into the durable
// initiative_setup_receipts and re-verifies the landed bytes.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// InitiativeSetupSpec is the closed instruction the in-container executor
// materializes: the initiative id (which fixes the shared branch
// mc/initiative-<id> and worktree mc-initiative-<id>), fresh-vs-retry mode, the
// target ref whose CURRENT tip is the cut point (fresh), the exact pinned cut
// OID to reuse (retry — ADR-025 D3: a retry reuses the recorded cut SHA and
// never re-resolves main), the expected object format, and the local repository
// UUID.
type InitiativeSetupSpec struct {
	InitiativeID  int64
	Mode          string
	TargetRef     string
	PinnedBaseSHA string
	ObjectFormat  string
	LocalRepoUUID string
}

func initiativeSharedBranch(initiativeID int64) string {
	return "mc/initiative-" + strconv.FormatInt(initiativeID, 10)
}

// MaterializeInitiativeStore builds the complete sanitized initiative store
// under storeRoot's empty git/ (leaving source/ the empty structural
// mountpoint) and checks the cut tree out into the separate worktreeRoot. The
// cut is the CURRENT tip of TargetRef (fresh) or the exact recorded OID (retry);
// a retry never re-resolves the ref. It returns the same git-derived SetupResult
// the first-task materializer does, keyed to the recorded cut SHA.
func MaterializeInitiativeStore(sourceRepo, storeRoot, worktreeRoot string, spec InitiativeSetupSpec) (SetupResult, error) {
	if spec.InitiativeID < 1 {
		return SetupResult{}, Domainf("initiative setup id %d is not a canonical positive decimal", spec.InitiativeID)
	}
	if err := validateSetupObjectFormat(spec.ObjectFormat); err != nil {
		return SetupResult{}, err
	}
	if !assignmentUUID.MatchString(spec.LocalRepoUUID) {
		return SetupResult{}, Domainf("initiative setup local repository UUID is malformed")
	}

	sf, err := gitAtSource(sourceRepo, "rev-parse", "--show-object-format")
	if err != nil {
		return SetupResult{}, Domainf("initiative setup could not read the source object format: %v", err)
	}
	if got := strings.TrimSpace(string(sf)); got != spec.ObjectFormat {
		return SetupResult{}, Domainf("initiative setup source object format %q does not match the pinned %q", got, spec.ObjectFormat)
	}

	var cutSHA string
	switch spec.Mode {
	case "fresh":
		cutSHA, err = resolveBaseOID(sourceRepo, spec.TargetRef, spec.ObjectFormat)
		if err != nil {
			return SetupResult{}, err
		}
	case "retry":
		cutSHA = spec.PinnedBaseSHA
		if len(cutSHA) != oidLen(spec.ObjectFormat) || !assignmentHex.MatchString(cutSHA) {
			return SetupResult{}, Domainf("initiative setup retry cut SHA is not a canonical object name")
		}
		if typ, err := gitAtSource(sourceRepo, "cat-file", "-t", cutSHA); err != nil ||
			strings.TrimSpace(string(typ)) != "commit" {
			return SetupResult{}, Domainf("initiative setup retry cut %s is not a commit in the source", cutSHA)
		}
	default:
		return SetupResult{}, Domainf("initiative setup mode %q is neither fresh nor retry", spec.Mode)
	}

	if err := rejectReservedTreeComponent(sourceRepo, cutSHA); err != nil {
		return SetupResult{}, err
	}

	// The resident precreates store root {git, source} and the worktree dir; all
	// three arrive empty. source/ stays the empty structural mountpoint (ADR-025
	// D1): only git/ and the separate worktree are populated.
	gitDir := filepath.Join(storeRoot, "git")
	source := filepath.Join(storeRoot, "source")
	for _, c := range []string{source, gitDir, worktreeRoot} {
		if err := requireEmptyChild(c); err != nil {
			return SetupResult{}, err
		}
	}

	wtName := initiativeWorktreeName(spec.InitiativeID)
	branch := initiativeSharedBranch(spec.InitiativeID)
	wtAdmin := filepath.Join(gitDir, "worktrees", wtName)
	for _, d := range []string{
		filepath.Join(worktreeRoot, ".mission-control"),
		filepath.Join(gitDir, "objects", "pack"),
		filepath.Join(gitDir, "objects", "info"),
		filepath.Join(gitDir, "info"),
		filepath.Join(gitDir, "hooks"),
		filepath.Join(gitDir, "refs", "heads", "mc"),
		wtAdmin,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return SetupResult{}, Domainf("initiative setup could not create %s: %v", d, err)
		}
	}

	refPath := filepath.Join(gitDir, "refs", "heads", "mc", "initiative-"+strconv.FormatInt(spec.InitiativeID, 10))
	headRef := "ref: refs/heads/" + branch + "\n"
	// The worktree pointers carry the SAME container-relative bytes the task
	// grammar uses (ADR-025 D1); they resolve only in-container.
	files := []struct{ path, body string }{
		{filepath.Join(gitDir, "config"), string(generatedTaskGitConfig(spec.ObjectFormat, spec.LocalRepoUUID))},
		{filepath.Join(gitDir, "HEAD"), headRef},
		{refPath, cutSHA + "\n"},
		{filepath.Join(gitDir, "packed-refs"), ""},
		{filepath.Join(gitDir, "shallow"), ""},
		{filepath.Join(wtAdmin, "commondir"), "../..\n"},
		{filepath.Join(wtAdmin, "gitdir"), "../../../source/.git\n"},
		{filepath.Join(wtAdmin, "HEAD"), headRef},
		{filepath.Join(wtAdmin, "config.worktree"), ""},
		{filepath.Join(worktreeRoot, ".git"), "gitdir: ../git/worktrees/" + wtName + "\n"},
	}
	for _, f := range files {
		if err := os.WriteFile(f.path, []byte(f.body), 0o644); err != nil {
			return SetupResult{}, Domainf("initiative setup could not write %s: %v", f.path, err)
		}
	}

	packDir := filepath.Join(gitDir, "objects", "pack")
	count, err := extractClosurePack(sourceRepo, cutSHA, spec.ObjectFormat, packDir)
	if err != nil {
		return SetupResult{}, err
	}

	// Drive the checkout with an explicit GIT_DIR (the linked worktree admin,
	// whose commondir ../.. resolves within the store host-side) and
	// GIT_WORK_TREE (the separate worktree base). The worktree's own .git and
	// the admin gitdir pointers are NOT consulted here — they resolve only in
	// the container — so the cross-base layout builds correctly host-side.
	env := append(sourceGitEnv(), "GIT_DIR="+wtAdmin, "GIT_WORK_TREE="+worktreeRoot)
	if _, err := gitOutput("", env, nil, "read-tree", cutSHA); err != nil {
		return SetupResult{}, Domainf("initiative setup could not build the worktree index: %v", err)
	}
	if _, err := gitOutput("", env, nil, "checkout-index", "-a", "-f"); err != nil {
		return SetupResult{}, Domainf("initiative setup could not materialize the shared worktree: %v", err)
	}
	if err := fsckClean(gitDir); err != nil {
		return SetupResult{}, err
	}

	digest, err := digestLandedPack(packDir)
	if err != nil {
		return SetupResult{}, err
	}
	return SetupResult{
		BaseSHA: cutSHA, ObjectFormat: spec.ObjectFormat, LocalRepoUUID: spec.LocalRepoUUID,
		ClosureDigest: digest, ObjectCount: count, FsckClean: true,
	}, nil
}
