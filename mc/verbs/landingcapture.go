package verbs

// The attest-side producer of the landing carrier. ADR-016:369-379 puts a
// landing-pending row through the same prepare/attest/commit shape as a spawn —
// "an attested candidate rather than a bare effect" whose frozen landing plan
// commit returns. This file builds that plan.
//
// It is the assembly only. Nothing routes a landing action into attestation
// yet, so this stays inert until the seam carries a landing candidate.

import (
	"strconv"
	"syscall"

	"mc/boundary"
)

// landingCaptureInputs are the SPINE facts of one landing, held by the caller
// and carried onto the plan unmodified.
//
// Every one is an input for the lander to MATCH against, never a value it may
// read off a repository (landsealedrun.go:28-30) — which is what makes the
// carrier a fence rather than a hint. The two facts NOT here are the ones only
// the host can answer: the observed target preimage, and the identity of the
// two host anchors.
type landingCaptureInputs struct {
	TaskID        int64
	LandingID     string
	ApprovedRunID string
	TargetRef     string
	VerifiedSHA   string
	ObjectFormat  string
	PinnedBaseSHA string
	ClosureDigest string
	LocalRepoUUID string
}

// captureLandingPlan assembles the frozen landing carrier over a live host.
//
// Order is deliberate and fail-closed: the structural anchors resolve BEFORE
// any git work, so a task with no sealed root refuses on its own evidence
// rather than after the attester has already interrogated the operator's
// repository. The repository revalidation reuses the lander's own
// `revalidateLandingRepository`, so attest refuses exactly what the lander
// would refuse — a landing that could not succeed is never planned, and the
// preimage frozen here is the tip of a repository already proved safe.
func captureLandingPlan(workspaceRoot string, ownerUID int, in landingCaptureInputs) (PrivateDispatchLanding, error) {
	var zero PrivateDispatchLanding
	// The abort path matches MERGE_MSG on this id and the container is
	// `mc-landing-<id>`; neither can carry anything else. Checked first because
	// it is the cheapest and because a plan without it is unusable however
	// sound the rest is.
	if !validLowercaseHex(in.LandingID, landingIDHexLen) {
		return zero, Domainf("landing id is not %d lowercase hex; the abort path could not identify its own merge", landingIDHexLen)
	}
	roots, err := resolveLandingRoots(workspaceRoot, in.TaskID, ownerUID)
	if err != nil {
		return zero, err
	}
	ws, err := landingPathIdentity(roots[boundary.KindLandingWorksource])
	if err != nil {
		return zero, err
	}
	taskRoot, err := landingPathIdentity(roots[boundary.KindLandingTaskRoot])
	if err != nil {
		return zero, err
	}
	// The target preimage, observed AT ATTEST and carried frozen. This is the
	// whole reason attest touches the target at all: `landsealedrun.go:142-145`
	// refuses if the tip has moved since, so an operator commit landing between
	// plan and merge is caught rather than merged over.
	repo, err := revalidateLandingRepository(workspaceRoot, in.TargetRef)
	if err != nil {
		return zero, err
	}
	return PrivateDispatchLanding{
		TaskID:        in.TaskID,
		ApprovedRunID: in.ApprovedRunID,
		// Derived, never carried. `complete.go:163` is tasks.branch's only
		// writer and it is closed to assigned tasks; deriving here is what keeps
		// the sealed lane from ever reading that column.
		Branch:         taskAssignmentBranch(in.TaskID),
		CoverDest:      landingCoverDest,
		LandingID:      in.LandingID,
		TargetRef:      in.TargetRef,
		ObjectFormat:   in.ObjectFormat,
		VerifiedSHA:    in.VerifiedSHA,
		PreMergeSHA:    repo.TargetSHA,
		PinnedBaseSHA:  in.PinnedBaseSHA,
		ClosureDigest:  in.ClosureDigest,
		LocalRepoUUID:  in.LocalRepoUUID,
		WorksourceRoot: ws,
		TaskRoot:       taskRoot,
	}, nil
}

// landingPathIdentity converts a resolved host anchor into the decimal-string
// identity evidence the plan carries.
//
// The type assertion is CHECKED, unlike the inline conversions in
// mountattest.go: this runs on a path the resident later rechecks against, so a
// platform where Sys() is not a *syscall.Stat_t must refuse rather than panic
// mid-attestation or, worse, carry a zero device/inode that would compare equal
// to an unpopulated field.
func landingPathIdentity(id boundary.ProtectedID) (PrivateDispatchPathIdentity, error) {
	if id.Info == nil {
		return PrivateDispatchPathIdentity{}, Domainf("landing anchor %q carries no stat evidence", id.Canonical)
	}
	st, ok := id.Info.Sys().(*syscall.Stat_t)
	if !ok {
		return PrivateDispatchPathIdentity{}, Domainf("landing anchor %q exposes no host identity", id.Canonical)
	}
	return PrivateDispatchPathIdentity{
		Canonical: id.Canonical,
		Device:    strconv.FormatUint(uint64(st.Dev), 10),
		Inode:     strconv.FormatUint(st.Ino, 10),
		OwnerUID:  int(st.Uid),
	}, nil
}
