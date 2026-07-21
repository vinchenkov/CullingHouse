package verbs

// The sealed landing's own prepare/attest/commit lane (ADR-016:369-379).
//
// A landing-pending row's id, task-store identity, base, verified SHA and
// target "form an attested candidate rather than a bare effect", and commit
// "rechecks the entire pending tuple and inventory before returning the frozen
// landing plan". So a sealed landing goes through the same three-step frame a
// spawn does — but it is NOT a spawn, and this file exists so it never has to
// pretend to be one.
//
// The lane is a SIBLING of the spawn lane, not a widening of it. `preparedDispatch`
// gained a third variant (`landing`); `preparedCandidate` did not gain a landing
// arm. That is deliberate and it is the whole safety argument: the spawn seam
// dereferences `cand.spawn` unguarded in dozens of places, and a landing routed
// through `preparedCandidate` would make every one of them reachable with a nil
// Spawn. As a separate variant they stay unreachable BY TYPE rather than by
// audit — so `preparedCandidate`, `dispatchAttest`, `dispatchCommit`,
// `applySpawn`, `spawnCandidateProjection` and `loadDispatchMountState` keep
// their exact signatures and bodies.
//
// What a landing must never do, and why each is structural rather than
// remembered: it claims no lease (§7; it holds none and frees none), opens no
// Run (`runs.role` has no landing member), and writes nothing to the spine at
// dispatch time — the writes come later, through `mc land report`.

import (
	"mc/dispatch"
)

// landingCandidateProjection puts a landing in the same bounded candidate slot
// a spawn occupies, naming NO run and NO role.
//
// Both emptinesses are load-bearing. A run id here would insert a runs row with
// no heartbeat producer — a reap target with no container — and a role would
// log the landing as that role's spawn. The subject is the only identity a
// landing has, and it is the task it lands.
func landingCandidateProjection(taskID int64) canonicalCandidate {
	id := taskID
	return canonicalCandidate{
		Kind:         "landing",
		RunID:        "",
		Role:         "",
		SubjectID:    &id,
		ProposedPool: []int64{},
		Wave:         []int64{},
		DedupeTitles: []string{},
	}
}

// landingTupleProjection freezes ADR-016:371-373's landing tuple out of the
// selected row and the mount state prepare already loaded.
//
// It refuses rather than defaulting on either missing half. Without the
// assignment there is no branch, base, object format or repo identity to land
// against; without the accepted completion seal the landing id loses its
// approved-run input, and an id derived over empty strings would be identical
// for every task in the deployment — which would collide the container name and
// the MERGE_MSG trailer that the abort path matches on.
func landingTupleProjection(t dispatch.Task, st PrivateDispatchMountState, landingID string) (canonicalLandingCandidate, error) {
	var zero canonicalLandingCandidate
	if t.Sealed == nil {
		return zero, Domainf("landing: task %d has no first-task closure assignment", t.ID)
	}
	seal := st.SubjectAcceptedCompletionSeal
	if seal == nil {
		return zero, Domainf("landing: task %d has no accepted completion seal to identify its approved run", t.ID)
	}
	return canonicalLandingCandidate{
		LandingID: landingID,
		TaskID:    t.ID,
		// The assignment is the sealed lane's branch home: `tasks.branch` is
		// empty for every sealed row by construction, because its only writer is
		// closed to assigned tasks. That is what makes the two lanes partition.
		TaskRootKey:   t.Sealed.TaskRootKey,
		Branch:        t.Sealed.Branch,
		ObjectFormat:  t.Sealed.ObjectFormat,
		PinnedBaseSHA: t.Sealed.BaseSHA,
		ClosureDigest: t.Sealed.ClosureDigest,
		LocalRepoUUID: t.Sealed.LocalRepoUUID,
		VerifiedSHA:   t.VerifiedSHA,
		// Both refs, deliberately. SealedLandingPending admits a row whose
		// assignment-frozen ref has diverged from the task's current one so the
		// seam can refuse it loudly instead of leaving it silently unlandable;
		// freezing both is what makes that refusal reproducible at commit
		// rather than re-observed there.
		TargetRef:         t.TargetRef,
		AssignedTargetRef: t.Sealed.TargetRef,
		ApprovedRunID:     seal.RunID,
		ApprovedRequestID: seal.CompletionRequest,
	}, nil
}

// buildCanonicalLandingPrepare is the spawn builder with the landing sibling
// attached. The shared builder is called unchanged, so everything selection read
// is projected identically on both lanes and only the candidate half differs.
func buildCanonicalLandingPrepare(identity dispatchProtocolIdentity, uuid, requestID string,
	rec dispatch.Records, lk dispatch.Lock, tun tunables, homies []homieCandidateState,
	mounts PrivateDispatchMountState, taskID int64, land canonicalLandingCandidate) canonicalPrepare {
	p := buildCanonicalPrepareWithIdentity(identity, uuid, requestID, rec, lk, tun, homies, mounts,
		landingCandidateProjection(taskID))
	p.Landing = &land
	return p
}

// sealedLandingSubject finds the row the sealed lane owes a landing to.
//
// It re-asserts the WHOLE predicate, not the id: at commit this is the fence
// that catches a row which was landable at prepare and has since been blocked,
// archived, un-approved, or had its decision reversed. Matching on the id alone
// would land a row the tick no longer selects.
func sealedLandingSubject(rec dispatch.Records, taskID int64) (dispatch.Task, bool) {
	for _, t := range rec.Tasks {
		if t.ID == taskID && t.SealedLandingPending() {
			return t, true
		}
	}
	return dispatch.Task{}, false
}
