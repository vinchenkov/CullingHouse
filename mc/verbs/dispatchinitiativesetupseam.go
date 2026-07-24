package verbs

import (
	"context"
	"os"
	"reflect"
	"time"

	"mc/dispatch"
	"mc/domain"
	"mc/refusal"
)

// The route-free InitiativeSetup dispatch lane (ADR-025 D3). It FUSES two
// existing lanes: the sealed-landing lane's route-free, host-attested-for-its-
// mount-plan, subjectless-refusal skeleton, and the spawn lane's lease claim.
// It is the ONLY lane that claims the global execution lease (Inv. 1) and opens
// a run (tier=pipeline, role=worker — "Worker-tier" per D3) while resolving NO
// agent route, NO brief, and NO harness: the resident runs the fixed
// `mc __setup-initiative` on the mount plan alone. The setup receipt is
// registered by the resident (S1.5), NEVER a task_assignments row.

// preparedInitiativeSetup is the fourth mutually-exclusive preparedDispatch
// variant (beside final/candidate/landing). Unlike a landing it carries a run id
// (allocated at prepare, canonical only at commit) because it opens a run.
type preparedInitiativeSetup struct {
	initiativeID  int64
	runID         string
	targetRef     string
	workspaceRoot string
	token         string
	mountState    PrivateDispatchMountState
}

// initiativeSetupCandidateProjection puts the initiative setup in the bounded
// candidate slot: a Worker-tier run keyed on the initiative, with no
// pool/wave/dedupe. The distinct Kind keeps its preparation token separate from
// a spawn's, and its omitempty Landing sibling stays nil so no existing token
// drifts.
func initiativeSetupCandidateProjection(runID string, initiativeID int64) canonicalCandidate {
	id := initiativeID
	return canonicalCandidate{
		Kind: "initiative-setup", RunID: runID, Role: "worker", SubjectID: &id,
		ProposedPool: []int64{}, Wave: []int64{}, DedupeTitles: []string{},
	}
}

// dispatchInitiativeSetupPrepare is the lane's step 1: it allocates the run id,
// loads the arc row's mount state under the transaction, resolves the workspace
// root, and freezes the preparation token. The loader is role-blind, so keying
// it on the arc row (a scope='initiative' tasks row) yields the arc's target ref
// and selected Worksource.
func dispatchInitiativeSetupPrepare(ctx context.Context, q Q, identity dispatchProtocolIdentity,
	uuid, requestID string, sel spineSelection, initiativeID int64) (preparedDispatch, error) {
	runID, err := newRunID()
	if err != nil {
		return preparedDispatch{}, err
	}
	id := initiativeID
	mountState, err := loadDispatchMountState(ctx, q, &dispatch.Spawn{SubjectID: &id}, sel.rec)
	if err != nil {
		return preparedDispatch{}, err
	}
	workspaceRoot, err := landingWorkspaceRoot(mountState)
	if err != nil {
		return preparedDispatch{}, err
	}
	canonical, err := buildCanonicalPrepareWithIdentity(identity, uuid, requestID,
		sel.rec, sel.lk, sel.tun, sel.homies, mountState,
		initiativeSetupCandidateProjection(runID, initiativeID)).bytes()
	if err != nil {
		return preparedDispatch{}, err
	}
	return preparedDispatch{
		requestID: requestID, deploymentUUID: uuid, identity: identity,
		initiativeSetup: &preparedInitiativeSetup{
			initiativeID: initiativeID, runID: runID,
			targetRef: mountState.SubjectTaskTargetRef, workspaceRoot: workspaceRoot,
			token: preparationToken(canonical), mountState: mountState,
		},
	}, nil
}

// dispatchAttestInitiativeSetup is the lane's route-free host-authority leg. Like
// landing it reads NO routing.md and resolves NO route; it attests only the
// shared-store precreate parents (ADR-025 D1) and authors the mount plan. Every
// failure is deployment HEALTH — a broken/absent store must never block the
// initiative — so it reuses the landing health-refusal classifier.
func dispatchAttestInitiativeSetup(home string, prepared preparedDispatch) (attestedDispatch, error) {
	is := prepared.initiativeSetup
	if is == nil {
		return attestedDispatch{}, Domainf("dispatch: initiative-setup attest requires a prepared initiative setup")
	}
	uuid, err := attestDeploymentPreamble(home, prepared)
	if err != nil {
		return attestedDispatch{}, err
	}
	step, err := captureInitiativePrecreate(is.workspaceRoot, is.initiativeID, os.Getuid(), is.targetRef)
	if err != nil {
		return landingHealthRefusal(uuid, refusal.FieldFilePlane, refusal.SummaryUnappliable, err), nil
	}
	return attestedDispatch{
		deploymentUUID: uuid,
		mountPlan: &PrivateDispatchMountPlan{
			Version: 1, Entries: []PrivateDispatchMountEntry{}, InitiativePrecreate: &step,
		},
	}, nil
}

// initiativeSetupCommitFences re-asserts the frozen state at commit, pure over
// reloaded state. Like the landing fences it is stale-class/inert on refusal.
// The re-decision fence catches a concurrent cut: if the receipt appeared
// between prepare and commit, loadRecords sets InitiativeSetupDone and Decide no
// longer selects this initiative, so the commit refuses inertly.
func initiativeSetupCommitFences(prepared preparedDispatch, sel spineSelection,
	current PrivateDispatchMountState) (string, error) {
	is := prepared.initiativeSetup
	if !reflect.DeepEqual(current, is.mountState) {
		return refusal.CodeStale, nil
	}
	canonical, err := buildCanonicalPrepareWithIdentity(prepared.identity, prepared.deploymentUUID,
		prepared.requestID, sel.rec, sel.lk, sel.tun, sel.homies, current,
		initiativeSetupCandidateProjection(is.runID, is.initiativeID)).bytes()
	if err != nil {
		return "", err
	}
	if preparationToken(canonical) != is.token {
		return refusal.CodeStale, nil
	}
	if sel.action.Kind != dispatch.KindInitiativeSetup || sel.action.InitiativeSetup == nil ||
		sel.action.InitiativeSetup.InitiativeID != is.initiativeID {
		return refusal.CodeCandidateMismatch, nil
	}
	return "", nil
}

// dispatchCommitInitiativeSetup is the lane's step 3. Unlike landing it CLAIMS
// the lease, opens the Worker-tier run, and writes an attested receipt.
func dispatchCommitInitiativeSetup(ctx context.Context, q Q, prepared preparedDispatch, attested attestedDispatch) (map[string]any, error) {
	is := prepared.initiativeSetup
	if is == nil {
		return nil, Domainf("dispatch: initiative-setup commit requires a prepared initiative setup")
	}
	if attested.deploymentUUID != prepared.deploymentUUID {
		return nil, Domainf("dispatch: attested deployment identity does not match prepare")
	}
	if err := requireDeploymentUUID(ctx, q, attested.deploymentUUID); err != nil {
		return nil, err
	}
	sel, err := selectFromSpine(ctx, q)
	if err != nil {
		return nil, err
	}
	id := is.initiativeID
	current, err := loadDispatchMountState(ctx, q, &dispatch.Spawn{SubjectID: &id}, sel.rec)
	if err != nil {
		return nil, err
	}
	// Subject-adjacent but subjectless refusal class: a stale/attest refusal must
	// be INERT and never block the arc row (only per-child terminals and arc
	// verification move the initiative). Mirror the landing lane.
	rcand := RefusalCandidate{Kind: RefusalSubjectlessPipeline}
	code, err := initiativeSetupCommitFences(prepared, sel, current)
	if err != nil {
		return nil, err
	}
	if code != "" {
		return commitInertInitiativeSetupRefusal(ctx, q, prepared, rcand, code)
	}
	if attested.refusal != nil {
		key, err := initiativeSetupDispatchKey(prepared, *attested.refusal)
		if err != nil {
			return nil, err
		}
		return applyAttestedRefusal(ctx, q, prepared.requestID, rcand, *attested.refusal, key)
	}
	if attested.mountPlan == nil || attested.mountPlan.InitiativePrecreate == nil {
		return nil, Domainf("dispatch: an initiative-setup attestation carries no precreate plan (ADR-025 D3)")
	}
	planDigest, err := mountPlanDigest(attested.mountPlan)
	if err != nil {
		return nil, err
	}
	action := canonicalAction{
		Version: 1, RequestID: prepared.requestID, Consequence: "initiative-setup",
		RunID: is.runID, Role: "worker", SubjectID: &id, PlanDigest: planDigest,
	}
	key, err := deriveDispatchKey(is.token, action)
	if err != nil {
		return nil, err
	}
	effect, err := applyInitiativeSetup(ctx, q, sel.now, is.initiativeID, sel.tun, is.runID, attested.mountPlan)
	if err != nil {
		return nil, err
	}
	if err := writeAttestedReceipt(ctx, q, prepared.requestID, key, effect,
		"dispatch.initiative-setup", is.initiativeID); err != nil {
		return nil, err
	}
	return effect, nil
}

// initiativeSetupDispatchKey is the refusal-only D2 fence for the lane.
func initiativeSetupDispatchKey(prepared preparedDispatch, r refusal.Refusal) (string, error) {
	is := prepared.initiativeSetup
	id := is.initiativeID
	return deriveDispatchKey(is.token, canonicalAction{
		Version: 1, RequestID: prepared.requestID, Consequence: "refusal",
		RunID: is.runID, Role: "worker", SubjectID: &id,
		Refusal: &canonicalRefusal{
			Code: r.Code, Authority: string(r.Authority),
			Field: string(r.Field), Summary: string(r.Summary), ItemIndex: r.ItemIndex,
		},
	})
}

// commitInertInitiativeSetupRefusal mirrors commitInertLandingRefusal: the
// general commitInertRefusal dereferences prepared.candidate, which this lane
// does not set.
func commitInertInitiativeSetupRefusal(ctx context.Context, q Q, prepared preparedDispatch, rcand RefusalCandidate, code string) (map[string]any, error) {
	r := refusal.Refusal{Code: code, Field: refusal.FieldNone, Summary: refusal.SummaryMismatch}
	key, err := initiativeSetupDispatchKey(prepared, r)
	if err != nil {
		return nil, err
	}
	return applyRefusal(ctx, q, rcand, r, key)
}

// applyInitiativeSetup claims the lease + opens the Worker-tier run and returns
// the route-free effect. There is NO harness/model_binding/brief key — the
// resident runs the fixed `mc __setup-initiative` on the mount plan.
func applyInitiativeSetup(ctx context.Context, q Q, now time.Time, initiativeID int64, tun tunables, runID string, mountPlan *PrivateDispatchMountPlan) (map[string]any, error) {
	id := initiativeID
	sessionPath := "sessions/" + runID
	if _, err := domain.Claim(ctx, q, now, domain.ClaimArgs{
		RunID: runID, Owner: "worker", SubjectID: &id, SessionPath: sessionPath,
		Binding: "", HardDeadlineMinutes: tun.hardDeadlineMinutes,
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"action":               "initiative-setup",
		"run_id":               runID,
		"initiative_id":        initiativeID,
		"subject_id":           initiativeID,
		"session_path":         sessionPath,
		"heartbeat_interval_s": tun.heartbeatIntervalS,
		"mount_plan":           mountPlan,
	}, nil
}
