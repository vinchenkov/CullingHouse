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
	"context"
	"os"

	"mc/dispatch"
	"mc/refusal"
)

// preparedLanding is what one landing prepare froze: the tuple under the token,
// the deterministic id, and the workspace root attest will resolve its host
// anchors against.
//
// `inputs` is held as the exact struct captureLandingPlan takes, so attest
// passes it straight through with no field-by-field copy that could drift from
// the tuple the token was computed over.
type preparedLanding struct {
	taskID        int64
	landingID     string
	inputs        landingCaptureInputs
	assignedRef   string
	workspaceRoot string
	tun           tunables
	token         string
	mountState    PrivateDispatchMountState
}

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

// dispatchLandingPrepare is the sealed lane's step 1 (ADR-016 D1). It loads the
// subject's mount state under prepare's transaction, then freezes the tuple.
func dispatchLandingPrepare(ctx context.Context, q Q, identity dispatchProtocolIdentity,
	uuid, requestID string, sel spineSelection, t dispatch.Task) (preparedDispatch, error) {
	mountState, err := loadDispatchLandingMountState(ctx, q, t.ID, sel.rec)
	if err != nil {
		return preparedDispatch{}, err
	}
	return landingPrepareFromState(identity, uuid, requestID, sel, t, mountState)
}

// landingPrepareFromState is the freezing half, pure over already-loaded state.
//
// The landing id is derived HERE rather than at attest, which contradicts the
// original siting in landingid.go and is the stronger reading: ADR-016:371
// names the deterministic id as a member of the candidate TUPLE, and a tuple
// member must be inside the preparation token or commit cannot detect its
// drift. All four of its inputs are in prepare's scope.
func landingPrepareFromState(identity dispatchProtocolIdentity, uuid, requestID string,
	sel spineSelection, t dispatch.Task, mountState PrivateDispatchMountState) (preparedDispatch, error) {
	seal := mountState.SubjectAcceptedCompletionSeal
	if seal == nil {
		return preparedDispatch{}, Domainf("landing: task %d has no accepted completion seal to identify its approved run", t.ID)
	}
	landingID, err := deriveLandingID(canonicalLandingIdentity{
		DeploymentUUID:    uuid,
		SubjectID:         t.ID,
		ApprovedRunID:     seal.RunID,
		ApprovedRequestID: seal.CompletionRequest,
	})
	if err != nil {
		return preparedDispatch{}, err
	}
	tuple, err := landingTupleProjection(t, mountState, landingID)
	if err != nil {
		return preparedDispatch{}, err
	}
	// Resolved at PREPARE, under the transaction, so attest never has to reach
	// back into spine-derived state to find out where on the host it is allowed
	// to look. It refuses rather than yielding "".
	workspaceRoot, err := landingWorkspaceRoot(mountState)
	if err != nil {
		return preparedDispatch{}, err
	}
	canonical, err := buildCanonicalLandingPrepare(identity, uuid, requestID,
		sel.rec, sel.lk, sel.tun, sel.homies, mountState, t.ID, tuple).bytes()
	if err != nil {
		return preparedDispatch{}, err
	}
	return preparedDispatch{
		requestID:      requestID,
		deploymentUUID: uuid,
		identity:       identity,
		landing: &preparedLanding{
			taskID:    t.ID,
			landingID: landingID,
			// Built from the tuple the token was computed over, never
			// re-assembled from the task, so the two cannot drift apart.
			inputs: landingCaptureInputs{
				TaskID:        tuple.TaskID,
				LandingID:     tuple.LandingID,
				ApprovedRunID: tuple.ApprovedRunID,
				TargetRef:     tuple.TargetRef,
				VerifiedSHA:   tuple.VerifiedSHA,
				ObjectFormat:  tuple.ObjectFormat,
				PinnedBaseSHA: tuple.PinnedBaseSHA,
				ClosureDigest: tuple.ClosureDigest,
				LocalRepoUUID: tuple.LocalRepoUUID,
			},
			assignedRef:   tuple.AssignedTargetRef,
			workspaceRoot: workspaceRoot,
			tun:           sel.tun,
			token:         preparationToken(canonical),
			mountState:    mountState,
		},
	}, nil
}

// dispatchAttestLanding is the sealed lane's step 2: the host-authority leg,
// run with the flock and transaction released.
//
// It attests the operator's Git views and NOTHING ELSE. There is no routing.md
// read, no route resolution, and no path from here to CodeRoutingInvalid.
// ADR-016:53-60 is explicit that a land candidate "instead attests ADR-017's
// exact task-store/real-repository Git views ... without a gateway probe", and
// spec §7:231 puts no agent in the landing path, so there is no role to
// resolve. Routing brokenness is a deployment fault that must never suppress an
// approved landing — the pending row would simply retry forever with the
// operator given no signal about the real cause.
//
// `route` and `routingDigest` therefore stay zero. That is the honest encoding
// of "no routing input", the same way an empty PlanDigest encodes "this refusal
// carries no plan"; it is not an omission to be filled in later.
//
// Every failure here is deployment HEALTH, never candidate class. ADR-016:576
// forbids mislabeling runtime failure as a failed reviewed change, and a
// candidate-class refusal against this subject would block the task. Only the
// fixed mc-land program's semantic Git refusal blocks, and it reports through
// `mc land report failure`, never from this leg.
func dispatchAttestLanding(home string, prepared preparedDispatch) (attestedDispatch, error) {
	land := prepared.landing
	if land == nil {
		return attestedDispatch{}, Domainf("dispatch: landing attest requires a prepared landing")
	}
	uuid, err := attestDeploymentPreamble(home, prepared)
	if err != nil {
		return attestedDispatch{}, err
	}
	// The assignment's frozen ref against the task's current one. The lane
	// admits a diverged row deliberately so that it refuses LOUDLY here rather
	// than being filtered out of selection and left silently unlandable
	// forever; both refs are inside the preparation token, so commit reproduces
	// this decision instead of re-observing it.
	if land.inputs.TargetRef != land.assignedRef {
		return landingHealthRefusal(uuid, refusal.FieldProjection, refusal.SummaryMismatch,
			Domainf("task %d: the assignment's frozen target ref %q has diverged from the task's %q",
				land.taskID, land.assignedRef, land.inputs.TargetRef)), nil
	}
	plan, err := captureLandingPlan(land.workspaceRoot, os.Getuid(), land.inputs)
	if err != nil {
		return landingHealthRefusal(uuid, refusal.FieldFilePlane, refusal.SummaryUnappliable, err), nil
	}
	return attestedDispatch{
		deploymentUUID: uuid,
		// Version and Entries are not boilerplate: validatePrivateMountPlan and
		// the resident's own invalidMountPlanReason both hard-refuse a zero
		// Version or a nil Entries, so a landing plan built without them would
		// fail only once the lane was armed. The landing rides as a SIBLING of
		// Entries because every entry destination must be under /workspace,
		// which no landing cell is.
		mountPlan: &PrivateDispatchMountPlan{
			Version: 1,
			Entries: []PrivateDispatchMountEntry{},
			Landing: &plan,
		},
	}, nil
}

// landingHealthRefusal classifies one landing attestation failure as deployment
// health. The raw error text rides only in Message, which DetailFor drops.
func landingHealthRefusal(deploymentUUID string, field refusal.Field, summary refusal.Summary, err error) attestedDispatch {
	return attestedDispatch{deploymentUUID: deploymentUUID, refusal: &refusal.Refusal{
		Code:    refusal.CodeProjectionUnavailable,
		Field:   field,
		Summary: summary,
		Message: err.Error(),
	}}
}
