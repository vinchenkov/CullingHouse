package verbs

// The ADR-025 D6 store-worktree cleanliness fence. Before a next
// initiative-family container for initiative I is prepared, the shared worktree
// must be clean at the managed branch tip: no prior child left uncommitted work,
// HEAD sits on mc/initiative-<id>, and HEAD equals that branch's ref tip (not
// detached, not mid-operation). It asserts STRUCTURAL cleanliness, never a
// spine-pinned verified SHA — that pin is D8/S5's landing concern; this is the
// each-child D6 fence guarding a shared worktree the prior child held RW.
//
// It runs IN-CONTAINER (the host never runs Git, ADR-016 D5) via the same
// explicit GIT_DIR/GIT_WORK_TREE the cross-base materializer uses
// (setupinitiative.go): GIT_DIR is the linked worktree admin (whose commondir
// resolves within the store), GIT_WORK_TREE the separate worktree base. The
// container-relative worktree pointers are never consulted. Read-only: it
// asserts and returns, and mutates nothing.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// InitiativeCleanFenceOperation is the closed operation tag the fence envelope
// must carry, distinct from every setup-class operation.
const InitiativeCleanFenceOperation = "initiative-clean-fence"

// InitiativeCleanFenceEnvelope is the frozen instruction the in-container fence
// executor consumes. StoreRoot and WorktreeRoot are the two container mount
// paths (/repo/store, /repo/worktree); InitiativeID fixes both the linked
// worktree admin name (mc-initiative-<id>) and the managed branch
// (mc/initiative-<id>).
type InitiativeCleanFenceEnvelope struct {
	SchemaVersion int    `json:"schema_version"`
	Operation     string `json:"operation"`
	InitiativeID  int64  `json:"initiative_id"`
	StoreRoot     string `json:"store_root"`
	WorktreeRoot  string `json:"worktree_root"`
}

// ReadInitiativeCleanFenceEnvelope decodes the RO-mounted fence instruction,
// refusing unknown or trailing fields exactly as ReadSetupEnvelope does.
func ReadInitiativeCleanFenceEnvelope(path string) (InitiativeCleanFenceEnvelope, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return InitiativeCleanFenceEnvelope{}, Domainf("initiative clean fence envelope is unreadable: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var env InitiativeCleanFenceEnvelope
	if err := dec.Decode(&env); err != nil {
		return InitiativeCleanFenceEnvelope{}, Domainf("initiative clean fence envelope is malformed: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return InitiativeCleanFenceEnvelope{}, Domainf("initiative clean fence envelope carries trailing data")
	}
	return env, nil
}

// RunInitiativeCleanFence proves the shared worktree is clean at the managed
// branch tip. A non-nil error is a fail-closed refusal: the resident must not
// prepare the next initiative-family container.
func RunInitiativeCleanFence(env InitiativeCleanFenceEnvelope) error {
	if env.SchemaVersion != 1 {
		return Domainf("initiative clean fence schema version %d is unsupported", env.SchemaVersion)
	}
	if env.Operation != InitiativeCleanFenceOperation {
		return Domainf("initiative clean fence envelope is not a clean-fence operation")
	}
	if env.InitiativeID < 1 {
		return Domainf("initiative clean fence id %d is not a canonical positive decimal", env.InitiativeID)
	}
	if env.StoreRoot == "" || env.WorktreeRoot == "" {
		return Domainf("initiative clean fence carries no store/worktree paths")
	}

	wtName := initiativeWorktreeName(env.InitiativeID)
	branch := initiativeSharedBranch(env.InitiativeID)
	wtAdmin := filepath.Join(env.StoreRoot, "git", "worktrees", wtName)
	// The explicit cross-base env, identical to the materializer's checkout leg:
	// GIT_DIR is the linked worktree admin, GIT_WORK_TREE the separate base.
	gitEnv := append(sourceGitEnv(), "GIT_DIR="+wtAdmin, "GIT_WORK_TREE="+env.WorktreeRoot)

	// The worktree admits NO dirt at all: any modification is a prior child's
	// uncommitted work the next container would inherit through the shared base.
	status, err := gitOutput("", gitEnv, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return Domainf("initiative shared worktree status is unreadable: %v", err)
	}
	if strings.TrimRight(string(status), "\n") != "" {
		return Domainf("initiative shared worktree is not clean; a prior child left uncommitted work")
	}

	// --assume-unchanged / --skip-worktree live in the INDEX, not the config, and
	// either makes `status` report a modified tracked file as clean. The prior
	// child held this worktree RW, so it is attacker-shaped input.
	if err := refuseHiddenInitiativeIndexEntries(gitEnv); err != nil {
		return err
	}

	// HEAD sits on the managed branch: not detached (which a rebase-in-progress
	// leaves it), and symbolic to exactly mc/initiative-<id>. No separate
	// "HEAD == branch tip" rev-parse follows, deliberately: with HEAD symbolic to
	// the branch, `rev-parse HEAD` resolves THROUGH the symref to the very tip we
	// would compare it against — a tautology, not a second opinion (the removal
	// landsealed.go:347-355 argues for). Any real divergence (a moved branch, a
	// re-read index, an in-progress merge) already surfaces as staged or unstaged
	// content in the porcelain fence above, so it is caught there, not here.
	head, err := gitOutput("", gitEnv, nil, "symbolic-ref", "--quiet", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != "refs/heads/"+branch {
		return Domainf("initiative shared worktree HEAD is not its managed branch %q", branch)
	}
	if _, err := gitOutput("", gitEnv, nil, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		return Domainf("initiative shared worktree HEAD is not a canonical commit")
	}
	return nil
}

// refuseHiddenInitiativeIndexEntries mirrors refuseHiddenLandingIndexEntries for
// the cross-base initiative worktree: an index carrying skip-worktree ('S') or
// assume-unchanged (lowercase) flags could hide a modified tracked file from the
// porcelain fence above.
func refuseHiddenInitiativeIndexEntries(gitEnv []string) error {
	out, err := gitOutput("", gitEnv, nil, "ls-files", "-v")
	if err != nil {
		return Domainf("initiative shared worktree index is unreadable: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		tag := line[0]
		if tag == 'S' || (tag >= 'a' && tag <= 'z') {
			return Domainf("initiative shared worktree index carries visibility flags; a modified file could be hidden from the clean fence")
		}
	}
	return nil
}
