package verbs

import (
	"os"
	"path/filepath"
)

// The composed sealed-landing lane (ADR-017:740-756). The five stages built in
// steps 4a-4e exist as functions with their own tests; this is what orders them
// and carries each one's output into the next.
//
// ORDER IS THE WHOLE CONTRIBUTION, and one detail of it is load-bearing. The
// ADR revalidates — including "the spec's primary dirty-tree fence" — BEFORE
// importing. That fence is scoped to the reviewed paths (ADR-017:742), and the
// reviewed set is a diff between the frozen base and the reviewed commit. The
// real repository does not have the reviewed commit until the import, so
// deriving the set THERE would force the fence after it, leaving a refused
// landing having already written objects into the operator's store. Deriving it
// from the SEALED store instead — which holds base..verified by construction —
// keeps the spec's order and means a refusal writes nothing at all.
//
// It STOPS at the merge. Cleanup (deleting the real task ref, exact-emptying
// the task-local root) may only happen after the spine records success, by a
// later trusted action (ADR-017:756-758) — this lane has no mount and no owner
// for it.

// sealedLandingPins are the frozen facts the landing instruction carries. Every
// one is an INPUT to match against, never a value read off the repositories.
type sealedLandingPins struct {
	TaskID        int64
	Branch        string
	TargetRef     string
	ObjectFormat  string
	VerifiedSHA   string
	PreMergeSHA   string
	PinnedBaseSHA string
	LocalRepoUUID string
	LandingID     string
}

// SealedLandingResult is the durable merge topology the resident reports into
// the spine (ADR-017:753-755). It is derived, never a third receipt file.
type SealedLandingResult struct {
	TaskID      int64  `json:"task_id"`
	LandingID   string `json:"landing_id"`
	Branch      string `json:"branch"`
	TargetRef   string `json:"target_ref"`
	VerifiedSHA string `json:"verified_sha"`
	PreMergeSHA string `json:"pre_merge_sha"`
	BaseSHA     string `json:"base_sha"`
	MergeSHA    string `json:"merge_sha"`
}

// fenceLandingCover proves the nested cover is really covering.
//
// `/repo/source/.mission-control` is a generated empty RO directory shadowing
// the real one (ADR-017:700). Without it the sealed task bytes are reachable
// through the RW source alias, which is the exact aliasing the separate RO
// `/repo/task` row exists to prevent. The plan declares the cover; this proves
// the declaration was realized, because a bind that silently did not happen
// looks identical from inside except for what is visible underneath.
//
// os.Lstat rather than os.Stat: a symlinked cover fails IsDir here instead of
// being followed to some directory elsewhere.
func fenceLandingCover(sourceRepo, coverRoot string) error {
	want := filepath.Join(sourceRepo, ".mission-control")
	if coverRoot != want {
		return Domainf("landing cover %q is not this source's nested cover %q", coverRoot, want)
	}
	info, err := os.Lstat(coverRoot)
	if err != nil {
		return Domainf("landing cover is absent; the sealed bytes would be reachable through the source alias: %v", err)
	}
	if !info.IsDir() {
		return Domainf("landing cover is not a directory")
	}
	entries, err := os.ReadDir(coverRoot)
	if err != nil {
		return Domainf("landing cover is unreadable: %v", err)
	}
	if len(entries) != 0 {
		return Domainf("landing cover is not empty; it is not shadowing the real .mission-control directory")
	}
	return nil
}

// landSealed runs the lane over real paths. The entrypoint below binds it to
// the envelope's fixed container destinations; this form takes the roots
// explicitly so it is exercisable against real temporary repositories.
func landSealed(sourceRepo, taskRoot, coverRoot string, pins sealedLandingPins) (SealedLandingResult, error) {
	var zero SealedLandingResult
	// Three of these four are REDUNDANT, measured by mutation rather than
	// assumed, and retained under the taxonomy's "fails faster and names the
	// problem better" rule. Only the branch binding is independently load-bearing.
	//
	//   - TaskID < 1: with it disabled the branch binding below still refuses,
	//     because taskAssignmentBranch(0) is "mc/task-0" and the pins carry a
	//     real task's branch.
	//   - TargetRef == Branch: with it disabled the repository stage refuses,
	//     because the task branch does not exist in the operator repository —
	//     creating it is what stage (7) does, later.
	//   - ObjectFormat: with it disabled revalidateSealedTaskStore validates the
	//     same format again at its own entry.
	//
	// The envelope validator refuses all four upstream as well; these exist
	// because landSealed is directly reachable with a hand-built pins struct.
	if pins.TaskID < 1 {
		return zero, Domainf("sealed landing names no task")
	}
	if pins.Branch != taskAssignmentBranch(pins.TaskID) {
		return zero, Domainf("sealed landing branch does not match the task id")
	}
	if pins.TargetRef == pins.Branch {
		return zero, Domainf("sealed landing targets its own task branch")
	}
	if err := validateSetupObjectFormat(pins.ObjectFormat); err != nil {
		return zero, err
	}
	// The one guard here that is NOT redundant, and it was the one missing. The
	// abort path matches MERGE_MSG against `MC-Landing-Id: <LandingID>`; an empty
	// id degenerates that into a match on the trailer NAME, which every landing's
	// message carries — so the lane would abort another action's merge, and would
	// write its own merge commit with an empty trailer for a later retry to match
	// on. Nothing downstream re-derives this, unlike the three above.
	if !validLowercaseHex(pins.LandingID, 16) {
		return zero, Domainf("sealed landing id is not 16 lowercase hex; the abort path could not identify its own merge")
	}

	if err := fenceLandingCover(sourceRepo, coverRoot); err != nil {
		return zero, err
	}

	// (1) The real repository and its target, under the safe-control fences.
	repo, err := revalidateLandingRepository(sourceRepo, pins.TargetRef)
	if err != nil {
		return zero, err
	}
	// The target must still be exactly where the plan froze it. mergeSealedLanding
	// rechecks this immediately before merging (ADR-017:750); checking it HERE too
	// is what makes a moved target cost nothing — it refuses before the import
	// rather than after writing objects the operator never asked for.
	if repo.TargetSHA != pins.PreMergeSHA {
		return zero, Domainf("landing target %q is at %q, not the frozen pre-merge SHA %q; the target moved since the plan was built",
			pins.TargetRef, repo.TargetSHA, pins.PreMergeSHA)
	}

	// (2) The sealed store is the exact artifact the assignment named.
	if _, err := revalidateSealedTaskStore(taskRoot, landingStorePins{
		TaskID: pins.TaskID, ObjectFormat: pins.ObjectFormat,
		VerifiedSHA: pins.VerifiedSHA, LocalRepoUUID: pins.LocalRepoUUID,
	}); err != nil {
		return zero, err
	}

	// (3) The reviewed path set, derived in the SEALED store against the frozen
	// base — see this file's header for why not in the real one.
	sealed := landingGit{dir: filepath.Join(taskRoot, "source")}
	paths, err := reviewedLandingPaths(sealed, pins.PinnedBaseSHA, pins.VerifiedSHA)
	if err != nil {
		return zero, err
	}

	// (4) The primary dirty-tree fence, scoped to those paths, in the real
	// checkout. Nothing has been written to the operator's store yet.
	live := landingGit{dir: sourceRepo}
	if err := fenceReviewedPathsCleanAt(live, sourceRepo, pins.TargetRef, paths); err != nil {
		return zero, err
	}

	// (5) The import. From here a crash can leave unreachable but valid objects,
	// which the ADR explicitly accepts: retry re-imports the same closure and git
	// deduplicates by hash (ADR-017:745-747).
	if err := importSealedClosure(sealed.dir, sourceRepo, pins.PinnedBaseSHA, pins.VerifiedSHA); err != nil {
		return zero, err
	}

	// (6) Only now can the real repository answer the history question. Binding
	// the single merge base to the assignment's frozen base is what proves the
	// target was not rewritten out from under the review; the target is allowed
	// to have ADVANCED beyond the base, which is why this is not a base==tip
	// check.
	base, err := landingMergeBase(live, pins.TargetRef, pins.VerifiedSHA)
	if err != nil {
		return zero, err
	}
	if base != pins.PinnedBaseSHA {
		return zero, Domainf("landing merge base %q is not the assignment's frozen base %q; the target has been rewritten", base, pins.PinnedBaseSHA)
	}

	// (7) The CAS-created ref is the durable import marker (ADR-017:748).
	if err := createLandingRefCAS(live, pins.Branch, pins.VerifiedSHA); err != nil {
		return zero, err
	}

	// (8) The merge, which rechecks the SHA fence itself.
	if err := mergeSealedLanding(live, pins.TargetRef, pins.VerifiedSHA, pins.PreMergeSHA, pins.LandingID, pins.TaskID); err != nil {
		return zero, err
	}
	merge, err := live.out("rev-parse", "--verify", "refs/heads/"+pins.TargetRef+"^{commit}")
	if err != nil {
		return zero, Domainf("landing cannot read the merged target tip: %v", err)
	}
	return SealedLandingResult{
		TaskID: pins.TaskID, LandingID: pins.LandingID, Branch: pins.Branch,
		TargetRef: pins.TargetRef, VerifiedSHA: pins.VerifiedSHA,
		PreMergeSHA: pins.PreMergeSHA, BaseSHA: base, MergeSHA: merge,
	}, nil
}

// RunSealedLanding is the in-container executor entrypoint for the landing
// class, the analogue of RunFirstTaskSetup. The envelope's paths are the
// landing table's own fixed destinations, already pinned by validation.
func RunSealedLanding(env SetupEnvelope) (SealedLandingResult, error) {
	if err := validateSetupEnvelope(env); err != nil {
		return SealedLandingResult{}, err
	}
	if env.Operation != SetupOperationSealedLanding {
		return SealedLandingResult{}, Domainf("setup envelope is not a sealed landing operation")
	}
	return landSealed(env.SourceRepo, env.TaskRoot, env.CoverRoot, sealedLandingPins{
		TaskID: env.TaskID, Branch: env.Branch, TargetRef: env.TargetRef,
		ObjectFormat: env.ObjectFormat, VerifiedSHA: env.VerifiedSHA,
		PreMergeSHA: env.PreMergeSHA, PinnedBaseSHA: env.PinnedBaseSHA,
		LocalRepoUUID: env.PinnedLocalRepoUUID, LandingID: env.LandingID,
	})
}
