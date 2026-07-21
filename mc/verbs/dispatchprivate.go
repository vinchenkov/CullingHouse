package verbs

// ADR-016 D1's private same-binary carrier. These structs are the closed,
// map-free frames between the Darwin broker and the Linux helper. RawMessage
// appears only for an already-final ordinary effect object; the helper never
// interprets caller-supplied RawMessage during commit.

import (
	"context"
	"database/sql"
	"encoding/json"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"mc/boundary"
	"mc/dispatch"
	"mc/refusal"
	"mc/routing"
	"mc/substrate"
)

// ADR-016 D2 bounds every ordinary scalar at 4 KiB. Domain cardinality is
// separately governed at admission and by the 1 MiB outer carrier.
const maxPrivateScalarBytes = 4 * 1024

func DispatchPreparePrivateDB(db *sql.DB, req PrivateDispatchPrepareRequest) (PrivateDispatchPrepareResponse, error) {
	var response PrivateDispatchPrepareResponse
	err := underDispatchLock(db, func(ctx context.Context, q Q) error {
		var err error
		response, err = DispatchPreparePrivate(ctx, q, req)
		return err
	})
	return response, err
}

func DispatchCommitPrivateDB(db *sql.DB, req PrivateDispatchCommitRequest) (PrivateDispatchResult, error) {
	var response PrivateDispatchResult
	err := underDispatchLock(db, func(ctx context.Context, q Q) error {
		var err error
		response, err = DispatchCommitPrivate(ctx, q, req)
		return err
	})
	return response, err
}

type PrivateDispatchPrepareRequest struct {
	Version             int    `json:"version"`
	ReleaseBuildID      string `json:"release_build_id"`
	ControlVersion      int    `json:"gateway_control_version"`
	SpineSchemaVersion  int    `json:"spine_schema_version"`
	ConfigSchemaVersion int    `json:"config_schema_version"`
	DeploymentUUID      string `json:"deployment_uuid"`
	DispatchRequestID   string `json:"dispatch_request_id"`
}

type PrivateDispatchCandidate struct {
	RunID               string                    `json:"run_id"`
	Role                string                    `json:"role"`
	SubjectID           *int64                    `json:"subject_id"`
	ProposedPool        []int64                   `json:"proposed_pool"`
	Wave                []int64                   `json:"wave"`
	DedupeTitles        []string                  `json:"dedupe_titles"`
	Token               string                    `json:"preparation_token"`
	TimeoutMinutes      int                       `json:"timeout_minutes"`
	GraceMinutes        int                       `json:"grace_minutes"`
	HeartbeatIntervalS  int                       `json:"heartbeat_interval_s"`
	SpawnGraceS         int                       `json:"spawn_grace_s"`
	HardDeadlineMinutes int                       `json:"hard_deadline_minutes"`
	ConsoleHour         int                       `json:"console_hour"`
	ConsoleMinute       int                       `json:"console_minute"`
	ConsoleTZ           string                    `json:"console_tz"`
	MountState          PrivateDispatchMountState `json:"mount_state"`
}

type PrivateDispatchPrepareResponse struct {
	Version             int                       `json:"version"`
	Kind                string                    `json:"kind"`
	ReleaseBuildID      string                    `json:"release_build_id"`
	ControlVersion      int                       `json:"gateway_control_version"`
	SpineSchemaVersion  int                       `json:"spine_schema_version"`
	ConfigSchemaVersion int                       `json:"config_schema_version"`
	DeploymentUUID      string                    `json:"deployment_uuid"`
	DispatchRequestID   string                    `json:"dispatch_request_id"`
	Final               *json.RawMessage          `json:"final"`
	Candidate           *PrivateDispatchCandidate `json:"candidate"`
}

type PrivateDispatchRefusal struct {
	Code      string `json:"code"`
	Authority string `json:"authority"`
	Field     string `json:"field"`
	Summary   string `json:"summary"`
	ItemIndex *int   `json:"item_index"`
}

type PrivateDispatchAttestation struct {
	RoutingDigest string                    `json:"routing_digest"`
	Harness       string                    `json:"harness"`
	Binding       string                    `json:"binding"`
	MountPlan     *PrivateDispatchMountPlan `json:"mount_plan"`
	Refusal       *PrivateDispatchRefusal   `json:"refusal"`
}

type PrivateDispatchCommitRequest struct {
	Version             int                        `json:"version"`
	ReleaseBuildID      string                     `json:"release_build_id"`
	ControlVersion      int                        `json:"gateway_control_version"`
	SpineSchemaVersion  int                        `json:"spine_schema_version"`
	ConfigSchemaVersion int                        `json:"config_schema_version"`
	DeploymentUUID      string                     `json:"deployment_uuid"`
	DispatchRequestID   string                     `json:"dispatch_request_id"`
	Candidate           PrivateDispatchCandidate   `json:"candidate"`
	Attestation         PrivateDispatchAttestation `json:"attestation"`
}

type PrivateDispatchResult struct {
	Version             int             `json:"version"`
	ReleaseBuildID      string          `json:"release_build_id"`
	ControlVersion      int             `json:"gateway_control_version"`
	SpineSchemaVersion  int             `json:"spine_schema_version"`
	ConfigSchemaVersion int             `json:"config_schema_version"`
	DeploymentUUID      string          `json:"deployment_uuid"`
	DispatchRequestID   string          `json:"dispatch_request_id"`
	Result              json.RawMessage `json:"result"`
}

func NewPrivateDispatchPrepareRequest(releaseBuildID, deploymentUUID string, controlVersion, configVersion int) (PrivateDispatchPrepareRequest, error) {
	requestID, err := newDispatchRequestID()
	if err != nil {
		return PrivateDispatchPrepareRequest{}, err
	}
	return PrivateDispatchPrepareRequest{
		Version: 1, ReleaseBuildID: releaseBuildID,
		ControlVersion: controlVersion, SpineSchemaVersion: substrate.CurrentSchemaVersion,
		ConfigSchemaVersion: configVersion, DeploymentUUID: deploymentUUID,
		DispatchRequestID: requestID,
	}, nil
}

func DispatchPreparePrivate(ctx context.Context, q Q, req PrivateDispatchPrepareRequest) (PrivateDispatchPrepareResponse, error) {
	if err := validatePrivateIdentity(req.Version, req.ReleaseBuildID, req.ControlVersion,
		req.SpineSchemaVersion, req.ConfigSchemaVersion, req.DeploymentUUID, req.DispatchRequestID); err != nil {
		return PrivateDispatchPrepareResponse{}, err
	}
	prepared, err := dispatchPrepareWithIdentity(ctx, q, privateIdentity(req.ReleaseBuildID, req.ControlVersion,
		req.SpineSchemaVersion, req.ConfigSchemaVersion), req.DeploymentUUID, req.DispatchRequestID)
	if err != nil {
		return PrivateDispatchPrepareResponse{}, err
	}
	response := PrivateDispatchPrepareResponse{
		Version: 1, ReleaseBuildID: req.ReleaseBuildID,
		ControlVersion: req.ControlVersion, SpineSchemaVersion: req.SpineSchemaVersion,
		ConfigSchemaVersion: req.ConfigSchemaVersion, DeploymentUUID: req.DeploymentUUID,
		DispatchRequestID: req.DispatchRequestID,
	}
	if prepared.final != nil {
		final, err := json.Marshal(prepared.final)
		if err != nil {
			return PrivateDispatchPrepareResponse{}, err
		}
		rawFinal := json.RawMessage(final)
		response.Kind = "final"
		response.Final = &rawFinal
		return response, nil
	}
	if err := privateFrameRefusesLanding(prepared); err != nil {
		return PrivateDispatchPrepareResponse{}, err
	}
	response.Kind = "candidate"
	response.Candidate = privateCandidateFromPrepared(prepared.candidate)
	return response, nil
}

// privateFrameRefusesLanding is the fail-closed guard on the Darwin
// broker/helper split, which has no landing carrier yet.
//
// There is no safe fallback here. privateCandidateFromPrepared is only ever
// handed prepared.candidate, which a landing leaves nil, so serializing one
// through the candidate frame would hand the far side a candidate whose Spawn
// is nil — and every consumer on that side dereferences it unguarded.
//
// It is also the standing guard on this lane's whole safety argument: if a
// future edit ever routes a landing into preparedCandidate, this is where it
// should fail first and loudly.
func privateFrameRefusesLanding(prepared preparedDispatch) error {
	if prepared.landing != nil {
		return Domainf("dispatch: the private frame does not yet carry a landing candidate")
	}
	return nil
}

func DispatchAttestPrivate(home string, prepared PrivateDispatchPrepareResponse) (PrivateDispatchCommitRequest, error) {
	internal, err := preparedFromPrivate(prepared)
	if err != nil {
		return PrivateDispatchCommitRequest{}, err
	}
	if prepared.Kind != "candidate" {
		return PrivateDispatchCommitRequest{}, Domainf("dispatch: only a candidate private frame may attest")
	}
	attested, err := dispatchAttest(home, internal)
	if err != nil {
		return PrivateDispatchCommitRequest{}, err
	}
	frame := canonicalPrivateAttestation(attested)
	return PrivateDispatchCommitRequest{
		Version: 1, ReleaseBuildID: prepared.ReleaseBuildID,
		ControlVersion: prepared.ControlVersion, SpineSchemaVersion: prepared.SpineSchemaVersion,
		ConfigSchemaVersion: prepared.ConfigSchemaVersion, DeploymentUUID: prepared.DeploymentUUID,
		DispatchRequestID: prepared.DispatchRequestID, Candidate: *prepared.Candidate,
		Attestation: frame,
	}, nil
}

// DispatchRecheckPrivate performs the second host-file read immediately
// before __dispatch-commit. Drift becomes a closed stale attestation; the
// helper then redecides under a fresh lock and applies no consequence.
func DispatchRecheckPrivate(home string, prepared PrivateDispatchPrepareResponse, commit PrivateDispatchCommitRequest) PrivateDispatchCommitRequest {
	internal, err := preparedFromPrivate(prepared)
	if err != nil {
		commit.Attestation = stalePrivateAttestation()
		return commit
	}
	first := attestedFromPrivate(commit.Attestation, prepared.DeploymentUUID)
	commit.Attestation = canonicalPrivateAttestation(dispatchRecheckAttestation(home, internal, first))
	return commit
}

func canonicalPrivateAttestation(attested attestedDispatch) PrivateDispatchAttestation {
	frame := PrivateDispatchAttestation{
		RoutingDigest: attested.routingDigest,
		Harness:       attested.route.Harness, Binding: attested.route.Binding,
		MountPlan: attested.mountPlan,
	}
	if attested.refusal != nil {
		frame.Refusal = &PrivateDispatchRefusal{
			Code: attested.refusal.Code, Authority: string(attested.refusal.Authority),
			Field: string(attested.refusal.Field), Summary: string(attested.refusal.Summary),
			ItemIndex: attested.refusal.ItemIndex,
		}
	}
	return frame
}

func attestedFromPrivate(frame PrivateDispatchAttestation, deploymentUUID string) attestedDispatch {
	attested := attestedDispatch{
		deploymentUUID: deploymentUUID,
		route:          routing.Route{Harness: frame.Harness, Binding: frame.Binding},
		routingDigest:  frame.RoutingDigest,
		mountPlan:      frame.MountPlan,
	}
	if frame.Refusal != nil {
		r := frame.Refusal
		attested.refusal = &refusal.Refusal{
			Code: r.Code, Authority: refusal.Authority(r.Authority), Field: refusal.Field(r.Field),
			Summary: refusal.Summary(r.Summary), ItemIndex: r.ItemIndex,
		}
	}
	return attested
}

func stalePrivateAttestation() PrivateDispatchAttestation {
	return PrivateDispatchAttestation{Refusal: &PrivateDispatchRefusal{
		Code: refusal.CodeStale, Field: string(refusal.FieldNone), Summary: string(refusal.SummaryMismatch),
	}}
}

func DispatchCommitPrivate(ctx context.Context, q Q, req PrivateDispatchCommitRequest) (PrivateDispatchResult, error) {
	if err := validatePrivateIdentity(req.Version, req.ReleaseBuildID, req.ControlVersion,
		req.SpineSchemaVersion, req.ConfigSchemaVersion, req.DeploymentUUID, req.DispatchRequestID); err != nil {
		return PrivateDispatchResult{}, err
	}
	prepared, err := preparedFromCandidate(
		privateIdentity(req.ReleaseBuildID, req.ControlVersion, req.SpineSchemaVersion, req.ConfigSchemaVersion),
		req.DeploymentUUID, req.DispatchRequestID, &req.Candidate)
	if err != nil {
		return PrivateDispatchResult{}, err
	}
	if err := validatePrivateAttestation(req.Attestation); err != nil {
		return PrivateDispatchResult{}, err
	}
	if err := validatePrivateTaskPrecreateCandidate(prepared.candidate, req.Attestation.MountPlan); err != nil {
		return PrivateDispatchResult{}, err
	}
	attested := attestedFromPrivate(req.Attestation, req.DeploymentUUID)
	effect, err := dispatchCommit(ctx, q, prepared, attested)
	if err != nil {
		return PrivateDispatchResult{}, err
	}
	result, err := json.Marshal(effect)
	if err != nil {
		return PrivateDispatchResult{}, err
	}
	return PrivateDispatchResult{
		Version: 1, ReleaseBuildID: req.ReleaseBuildID,
		ControlVersion: req.ControlVersion, SpineSchemaVersion: req.SpineSchemaVersion,
		ConfigSchemaVersion: req.ConfigSchemaVersion, DeploymentUUID: req.DeploymentUUID,
		DispatchRequestID: req.DispatchRequestID, Result: result,
	}, nil
}

func validatePrivateTaskPrecreateCandidate(cand *preparedCandidate, plan *PrivateDispatchMountPlan) error {
	if plan == nil || plan.TaskPrecreate == nil {
		return nil
	}
	step := plan.TaskPrecreate
	if cand == nil || cand.spawn == nil || baseRole(string(cand.spawn.Role)) != "worker" ||
		cand.spawn.SubjectID == nil || *cand.spawn.SubjectID != step.TaskID ||
		cand.mountState.SubjectInitiativeID != nil {
		return Domainf("dispatch: private task precreate does not match a standalone Worker candidate")
	}
	selected, err := selectedDispatchWorksource(cand.mountState)
	if err != nil || selected.Kind != "repo" || selected.WorkspaceRoot != step.WorkspaceRoot {
		return Domainf("dispatch: private task precreate does not match the selected repo Worksource")
	}
	// The setup instruction must restate the token-frozen spine state, never
	// invent it: with a frozen assignment the step is retry mode carrying its
	// exact pins (ADR-016 D5); without one it is fresh mode pinned to the
	// frozen target ref. (validatePrivateTaskSetup already proved the shape.)
	if assignment := cand.mountState.SubjectTaskAssignment; assignment != nil {
		if step.Setup == nil || step.Setup.Mode != "retry" ||
			step.Setup.ObjectFormat != assignment.ObjectFormat ||
			step.Setup.PinnedBaseSHA != assignment.BaseSHA ||
			step.Setup.PinnedClosureDigest != assignment.ClosureDigest ||
			step.Setup.PinnedLocalRepoUUID != assignment.LocalRepoUUID {
			return Domainf("dispatch: private task setup does not restate the frozen closure assignment")
		}
	} else if step.Setup == nil || step.Setup.Mode != "fresh" ||
		step.Setup.TargetRef != cand.mountState.SubjectTaskTargetRef {
		return Domainf("dispatch: private task setup does not restate the frozen target ref")
	}
	if root := step.RecoverRoot; root != nil {
		if cand.mountState.SubjectTaskAssignment != nil {
			return Domainf("dispatch: private task recovery must not replace an assigned closure")
		}
		matched := false
		for _, receipt := range cand.mountState.SubjectTaskSetupRoots {
			if root.Device == receipt.Device && root.Inode == receipt.Inode && root.OwnerUID == receipt.OwnerUID {
				matched = true
				break
			}
		}
		if !matched {
			return Domainf("dispatch: private task recovery root is not receipt-vouched by the prepared candidate")
		}
	}
	return nil
}

// validatePrivateTaskSetup keeps the helper boundary strict about the plan's
// setup instruction: a closed mode pair, a closed object-format set, and
// pins shaped exactly as the task_assignments CHECKs would have produced
// them. Fresh instructions carry a target and no pins; retry instructions
// carry the pins and no target.
func validatePrivateTaskSetup(setup *PrivateDispatchTaskSetup) error {
	if setup == nil {
		return Domainf("dispatch: private task precreate carries no setup instruction")
	}
	switch setup.Mode {
	case "fresh":
		if !validStructuralText(setup.TargetRef, maxPrivateScalarBytes) ||
			setup.PinnedBaseSHA != "" || setup.PinnedClosureDigest != "" || setup.PinnedLocalRepoUUID != "" {
			return Domainf("dispatch: private fresh task setup instruction is invalid")
		}
		if setup.ObjectFormat != "sha1" && setup.ObjectFormat != "sha256" {
			return Domainf("dispatch: private task setup object format is outside the closed set")
		}
	case "retry":
		if setup.TargetRef != "" {
			return Domainf("dispatch: private retry task setup re-resolves a target ref (ADR-016 D5)")
		}
		if err := validateFirstTaskAssignment(FirstTaskAssignment{
			ObjectFormat: setup.ObjectFormat, BaseSHA: setup.PinnedBaseSHA,
			LocalRepoUUID: setup.PinnedLocalRepoUUID, ClosureDigest: setup.PinnedClosureDigest,
		}); err != nil {
			return Domainf("dispatch: private retry task setup pins are invalid")
		}
	default:
		return Domainf("dispatch: private task setup mode is outside the closed pair")
	}
	return nil
}

func validatePrivateAttestation(a PrivateDispatchAttestation) error {
	if a.Refusal != nil {
		if a.RoutingDigest != "" || a.Harness != "" || a.Binding != "" || a.MountPlan != nil {
			return Domainf("dispatch: private attestation carries both a route and refusal")
		}
		r := a.Refusal
		if _, err := refusal.DetailFor(refusal.Refusal{
			Code: r.Code, Authority: refusal.Authority(r.Authority), Field: refusal.Field(r.Field),
			Summary: refusal.Summary(r.Summary), ItemIndex: r.ItemIndex,
		}); err != nil {
			return Domainf("dispatch: invalid private refusal attestation")
		}
		return nil
	}
	if !validLowercaseHex(a.RoutingDigest, 64) || !validStructuralText(a.Harness, 4096) || !validStructuralText(a.Binding, 4096) {
		return Domainf("dispatch: private route attestation is incomplete")
	}
	registry, _ := routing.ActiveRegistry()
	if want, ok := registry[a.Binding]; !ok || want != a.Harness {
		return Domainf("dispatch: private route attestation is unresolved")
	}
	return validatePrivateMountPlan(a.MountPlan)
}

// validatePrivateMountPlan re-validates the carrier at the helper boundary: a
// route attestation always carries an explicit plan (possibly empty), whose
// entries are bounded, evidence-complete, and strictly ordered by unique
// non-overlapping absolute destinations. The helper never trusts the broker's
// shape.
func validatePrivateMountPlan(plan *PrivateDispatchMountPlan) error {
	if plan == nil || plan.Version != 1 || plan.Entries == nil {
		return Domainf("dispatch: private route attestation carries no explicit mount plan")
	}
	if plan.JurisdictionDigest != "" && !validLowercaseHex(plan.JurisdictionDigest, 64) {
		return Domainf("dispatch: private mount plan jurisdiction digest is invalid")
	}
	if len(plan.Entries) > maxPlanMounts {
		return Domainf("dispatch: private mount plan exceeds %d entries (ADR-016 D2)", maxPlanMounts)
	}
	body, err := json.Marshal(plan)
	if err != nil || len(body) > maxDispatchMountPlanBytes {
		return Domainf("dispatch: private mount plan exceeds its byte budget")
	}
	if step := plan.TaskPrecreate; step != nil {
		if step.TaskID < 1 || step.TaskID > maxJavaScriptSafeInteger || step.ChildMode != taskSkeletonChildMode {
			return Domainf("dispatch: private task precreate identity/mode is invalid")
		}
		parent := step.TasksParent
		if !validStructuralText(step.WorkspaceRoot, maxPrivateScalarBytes) ||
			!validStructuralText(parent.Canonical, maxPrivateScalarBytes) ||
			!path.IsAbs(step.WorkspaceRoot) || path.Clean(step.WorkspaceRoot) != step.WorkspaceRoot ||
			parent.Canonical != path.Join(step.WorkspaceRoot, ".mission-control", "tasks") ||
			!validDecimalText(parent.Device) || !validDecimalText(parent.Inode) || parent.OwnerUID < 0 {
			return Domainf("dispatch: private task precreate parent evidence is invalid")
		}
		if err := validatePrivateTaskSetup(step.Setup); err != nil {
			return err
		}
		if root := step.RecoverRoot; root != nil {
			if !validStructuralText(root.Canonical, maxPrivateScalarBytes) ||
				root.Canonical != path.Join(parent.Canonical, "task-"+strconv.FormatInt(step.TaskID, 10)) ||
				!validDecimalText(root.Device) || !validDecimalText(root.Inode) ||
				root.OwnerUID != parent.OwnerUID {
				return Domainf("dispatch: private task recovery root evidence is invalid")
			}
		}
	}
	if step := plan.CompletionSeal; step != nil {
		parent := step.SealsParent
		if step.TaskID < 1 || !validStructuralText(step.RunID, maxPrivateScalarBytes) ||
			!validStructuralText(parent.Canonical, maxPrivateScalarBytes) ||
			!path.IsAbs(parent.Canonical) || path.Clean(parent.Canonical) != parent.Canonical ||
			!validDecimalText(parent.Device) || !validDecimalText(parent.Inode) || parent.OwnerUID < 0 {
			return Domainf("dispatch: private completion seal precreate evidence is invalid")
		}
		if plan.TaskPrecreate != nil || plan.AcceptedSealRebuild != nil || plan.VerifierProjection != nil {
			return Domainf("dispatch: private completion seal cannot share a setup-only plan")
		}
	}
	if step := plan.AcceptedSealRebuild; step != nil {
		if step.TaskID < 1 || !validStructuralText(step.RunID, maxPrivateScalarBytes) ||
			!validLowercaseHex(step.CompletionRequest, 16) ||
			(step.ObjectFormat != "sha1" && step.ObjectFormat != "sha256") ||
			!validLowercaseHex(step.SealedSHA, oidLen(step.ObjectFormat)) ||
			!validLowercaseHex(step.ClosureDigest, 64) || !validLowercaseHex(step.ManifestDigest, 64) ||
			!validDecimalText(step.Device) || !validDecimalText(step.Inode) || step.OwnerUID < 0 {
			return Domainf("dispatch: private accepted seal rebuild evidence is invalid")
		}
	}
	if step := plan.VerifierProjection; step != nil {
		if plan.AcceptedSealRebuild != nil || step.TaskID < 1 || !validStructuralText(step.RebuildRunID, maxPrivateScalarBytes) ||
			!validLowercaseHex(step.CompletionRequest, 16) || (step.ObjectFormat != "sha1" && step.ObjectFormat != "sha256") || !validLowercaseHex(step.SealedSHA, oidLen(step.ObjectFormat)) || !validLowercaseHex(step.ClosureDigest, 64) ||
			!validLowercaseHex(step.ManifestDigest, 64) || !validDecimalText(step.SealDevice) || !validDecimalText(step.SealInode) || step.SealOwnerUID < 0 {
			return Domainf("dispatch: private verifier projection evidence is invalid")
		}
	}
	if step := plan.Landing; step != nil {
		// Landing is its own effect class. Sharing an attestation with a setup
		// step would authorize a mutating setup container AND the system's only
		// RW real-repository grant from one token; carrying an entry would put
		// an agent-plane mount in a class with no agent process (ADR-017:711).
		if plan.TaskPrecreate != nil || plan.CompletionSeal != nil ||
			plan.AcceptedSealRebuild != nil || plan.VerifierProjection != nil {
			return Domainf("dispatch: private landing cannot share a plan with a setup step")
		}
		if len(plan.Entries) != 0 {
			return Domainf("dispatch: private landing carries agent-plane mount entries (ADR-017:711)")
		}
		if step.TaskID < 1 || step.TaskID > maxJavaScriptSafeInteger ||
			!validLowercaseHex(step.LandingID, 16) ||
			!validStructuralText(step.ApprovedRunID, maxPrivateScalarBytes) || step.ApprovedRunID == "" ||
			step.Branch != taskAssignmentBranch(step.TaskID) ||
			!validLandingTargetBranch(step.TargetRef) || step.TargetRef == step.Branch ||
			(step.ObjectFormat != "sha1" && step.ObjectFormat != "sha256") ||
			!validLowercaseHex(step.VerifiedSHA, oidLen(step.ObjectFormat)) ||
			!validLowercaseHex(step.PreMergeSHA, oidLen(step.ObjectFormat)) ||
			!validLowercaseHex(step.PinnedBaseSHA, oidLen(step.ObjectFormat)) ||
			!validLowercaseHex(step.ClosureDigest, 64) ||
			!assignmentUUID.MatchString(step.LocalRepoUUID) {
			return Domainf("dispatch: private landing identity evidence is invalid")
		}
		if step.CoverDest != landingCoverDest {
			return Domainf("dispatch: private landing carries no `.mission-control` cover obligation (ADR-017:700)")
		}
		ws := step.WorksourceRoot
		if !validStructuralText(ws.Canonical, maxPrivateScalarBytes) ||
			!path.IsAbs(ws.Canonical) || path.Clean(ws.Canonical) != ws.Canonical ||
			!validDecimalText(ws.Device) || !validDecimalText(ws.Inode) || ws.OwnerUID < 0 {
			return Domainf("dispatch: private landing Worksource evidence is invalid")
		}
		root := step.TaskRoot
		if !validStructuralText(root.Canonical, maxPrivateScalarBytes) ||
			root.Canonical != path.Join(ws.Canonical, ".mission-control", "tasks",
				"task-"+strconv.FormatInt(step.TaskID, 10)) ||
			!validDecimalText(root.Device) || !validDecimalText(root.Inode) ||
			root.OwnerUID != ws.OwnerUID {
			return Domainf("dispatch: private landing task root evidence is invalid")
		}
	}
	prior := ""
	logicalIDs := map[string]bool{}
	for i, e := range plan.Entries {
		if !validStructuralText(e.LogicalID, maxPrivateScalarBytes) ||
			!validStructuralText(e.Source, maxPrivateScalarBytes) ||
			!validStructuralText(e.Destination, maxPrivateScalarBytes) {
			return Domainf("dispatch: private mount entry %d text is invalid", i)
		}
		if logicalIDs[e.LogicalID] {
			return Domainf("dispatch: private mount logical ids must be unique (ADR-016 D2)")
		}
		logicalIDs[e.LogicalID] = true
		if !strings.HasPrefix(e.Source, "/") || path.Clean(e.Destination) != e.Destination ||
			strings.Contains(e.Source, ":") || strings.Contains(e.Destination, ":") {
			return Domainf("dispatch: private mount entry %d path shape is invalid", i)
		}
		// Plan destinations are a closed set: the derived artifact/reference
		// class namespaces plus ADR-017 D6's task-table cells (which include
		// the legacy fake /workspace/source and the /workspace task root).
		// The runtime/system planes (/mc, /app/src, /home/agent, /etc) are
		// never plan-addressable, whatever the broker claims — and neither are
		// the `/repo` setup and landing planes, which the resident composes
		// itself (effects.ts:90-95 admits only /workspace). Widening this set
		// to a resident-composed plane spends a defence-in-depth layer for
		// zero realized capability; see landingplan.go.
		if !strings.HasPrefix(e.Destination, "/workspace/artifacts/") &&
			!strings.HasPrefix(e.Destination, "/workspace/references/") &&
			!validTaskPlanDestination(e.Destination) {
			return Domainf("dispatch: private mount entry %d destination is outside the ordinary namespace", i)
		}
		if plan.TaskPrecreate != nil && validTaskPlanDestination(e.Destination) {
			return Domainf("dispatch: task precreate plan fabricates a not-yet-existing task mount row")
		}
		if e.Kind != "dir" && e.Kind != "file" {
			return Domainf("dispatch: private mount entry %d kind is invalid", i)
		}
		if e.ContentSHA256 != "" && (!validLowercaseHex(e.ContentSHA256, 64) || e.Kind != "file") {
			return Domainf("dispatch: private mount entry %d content evidence is invalid", i)
		}
		if e.RequireEmptyDir && e.Kind != "dir" {
			return Domainf("dispatch: private mount entry %d empty-directory evidence is invalid", i)
		}
		if e.ContentSHA256 != "" && e.RequireEmptyDir {
			return Domainf("dispatch: private mount entry %d fixed-shape evidence is incoherent", i)
		}
		if e.Access != string(boundary.AccessRO) && e.Access != string(boundary.AccessRW) {
			return Domainf("dispatch: private mount entry %d access is invalid", i)
		}
		if !validDecimalText(e.Device) || !validDecimalText(e.Inode) {
			return Domainf("dispatch: private mount entry %d identity evidence is invalid", i)
		}
		if e.OwnerUID < 0 || e.Mode < 0 || e.Mode > 0o777 {
			return Domainf("dispatch: private mount entry %d owner/mode evidence is invalid", i)
		}
		if i > 0 && e.Destination <= prior {
			return Domainf("dispatch: private mount destinations are unsorted or overlapping")
		}
		// Sorted order puts an ancestor before every descendant but NOT
		// adjacent to it ('-' sorts before '/', so a sibling can interleave):
		// the overlap scan must walk every prior entry, not just the last.
		for _, p := range plan.Entries[:i] {
			if strings.HasPrefix(e.Destination, p.Destination+"/") && !mountOverlapPermitted(p.Destination, e.Destination) {
				return Domainf("dispatch: private mount destinations are unsorted or overlapping")
			}
		}
		prior = e.Destination
	}
	return nil
}

func validDecimalText(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func privateCandidateFromPrepared(c *preparedCandidate) *PrivateDispatchCandidate {
	return &PrivateDispatchCandidate{
		RunID: c.runID, Role: string(c.spawn.Role), SubjectID: c.spawn.SubjectID,
		ProposedPool: nonNilInt64s(c.spawn.ProposedPool), Wave: nonNilInt64s(c.spawn.Wave),
		DedupeTitles: nonNilStrings(c.spawn.DedupeTitles), Token: c.token,
		TimeoutMinutes: c.tun.timeoutMinutes, GraceMinutes: c.tun.graceMinutes,
		HeartbeatIntervalS: c.tun.heartbeatIntervalS, SpawnGraceS: c.tun.spawnGraceS,
		HardDeadlineMinutes: c.tun.hardDeadlineMinutes, ConsoleHour: c.tun.consoleHour,
		ConsoleMinute: c.tun.consoleMinute, ConsoleTZ: c.tun.consoleTZ,
		MountState: c.mountState,
	}
}

func preparedFromPrivate(frame PrivateDispatchPrepareResponse) (preparedDispatch, error) {
	if err := validatePrivateIdentity(frame.Version, frame.ReleaseBuildID, frame.ControlVersion,
		frame.SpineSchemaVersion, frame.ConfigSchemaVersion, frame.DeploymentUUID, frame.DispatchRequestID); err != nil {
		return preparedDispatch{}, err
	}
	switch frame.Kind {
	case "candidate":
		if frame.Candidate == nil || frame.Final != nil {
			return preparedDispatch{}, Domainf("dispatch: malformed private candidate response")
		}
		return preparedFromCandidate(
			privateIdentity(frame.ReleaseBuildID, frame.ControlVersion, frame.SpineSchemaVersion, frame.ConfigSchemaVersion),
			frame.DeploymentUUID, frame.DispatchRequestID, frame.Candidate)
	case "final":
		if frame.Candidate != nil || frame.Final == nil {
			return preparedDispatch{}, Domainf("dispatch: malformed private final response")
		}
		return preparedDispatch{requestID: frame.DispatchRequestID, deploymentUUID: frame.DeploymentUUID,
			identity: privateIdentity(frame.ReleaseBuildID, frame.ControlVersion, frame.SpineSchemaVersion, frame.ConfigSchemaVersion)}, nil
	default:
		return preparedDispatch{}, Domainf("dispatch: unknown private prepare response kind %q", frame.Kind)
	}
}

func preparedFromCandidate(identity dispatchProtocolIdentity, deploymentUUID, requestID string, c *PrivateDispatchCandidate) (preparedDispatch, error) {
	if c == nil || !validLowercaseHex(c.RunID, 16) || !validLowercaseHex(c.Token, 64) {
		return preparedDispatch{}, Domainf("dispatch: malformed private candidate identity")
	}
	if c.ProposedPool == nil || c.Wave == nil || c.DedupeTitles == nil {
		return preparedDispatch{}, Domainf("dispatch: private candidate collections must be explicit")
	}
	if err := validatePrivateMountState(c.MountState); err != nil {
		return preparedDispatch{}, err
	}
	if !validPrivateRole(c.Role) {
		return preparedDispatch{}, Domainf("dispatch: private candidate role is invalid")
	}
	if !strictPositiveIDs(c.ProposedPool) || !strictPositiveIDs(c.Wave) {
		return preparedDispatch{}, Domainf("dispatch: private candidate ids are not sorted unique positive values")
	}
	if !validStructuralTexts(c.DedupeTitles, maxPrivateScalarBytes) || !validPrivateConsole(c.ConsoleHour, c.ConsoleMinute, c.ConsoleTZ) {
		return preparedDispatch{}, Domainf("dispatch: private candidate structural text is invalid")
	}
	if (c.SubjectID != nil && *c.SubjectID <= 0) || c.TimeoutMinutes <= 0 || c.GraceMinutes < 0 ||
		c.HeartbeatIntervalS <= 0 || c.SpawnGraceS <= 0 || c.HardDeadlineMinutes <= 0 ||
		c.ConsoleMinute < 0 || c.ConsoleMinute > 59 {
		return preparedDispatch{}, Domainf("dispatch: private candidate scalar is outside its bound")
	}
	sp := &dispatch.Spawn{
		Role: dispatch.Role(c.Role), SubjectID: c.SubjectID,
		ProposedPool: c.ProposedPool, Wave: c.Wave, DedupeTitles: c.DedupeTitles,
	}
	tun := tunables{
		timeoutMinutes: c.TimeoutMinutes, graceMinutes: c.GraceMinutes,
		heartbeatIntervalS: c.HeartbeatIntervalS, spawnGraceS: c.SpawnGraceS,
		hardDeadlineMinutes: c.HardDeadlineMinutes, consoleHour: c.ConsoleHour,
		consoleMinute: c.ConsoleMinute, consoleTZ: c.ConsoleTZ,
	}
	return preparedDispatch{
		requestID: requestID, deploymentUUID: deploymentUUID,
		identity:  identity,
		candidate: &preparedCandidate{spawn: sp, runID: c.RunID, tun: tun, token: c.Token, mountState: c.MountState},
	}, nil
}

func validatePrivateMountState(state PrivateDispatchMountState) error {
	if state.Worksources == nil {
		return Domainf("dispatch: private mount-state Worksources must be explicit")
	}
	if state.SubjectInitiativeID != nil && *state.SubjectInitiativeID <= 0 {
		return Domainf("dispatch: private mount-state initiative id must be positive")
	}
	projection, err := json.Marshal(state.Worksources)
	if err != nil || len(projection) > substrate.MaxDispatchMountProjectionBytes {
		return Domainf("dispatch: private Worksource projection exceeds its admitted byte budget")
	}
	foundSelected := state.SelectedWorksource == ""
	prior := ""
	for i, ws := range state.Worksources {
		if !validStructuralText(ws.WorksourceID, maxPrivateScalarBytes) ||
			!validStructuralText(ws.Kind, maxPrivateScalarBytes) ||
			!validStructuralText(ws.Status, maxPrivateScalarBytes) ||
			(i > 0 && ws.WorksourceID <= prior) {
			return Domainf("dispatch: private Worksource projection is invalid or unsorted")
		}
		prior = ws.WorksourceID
		if ws.WorksourceID == state.SelectedWorksource {
			foundSelected = true
		}
		if ws.ArtifactRoots == nil || ws.ReadonlyMounts == nil || ws.DeniedPaths == nil ||
			!strictStructuralTexts(ws.ArtifactRoots) || !strictStructuralTexts(ws.ReadonlyMounts) ||
			!strictStructuralTexts(ws.DeniedPaths) {
			return Domainf("dispatch: private Worksource path projection is invalid")
		}
		for _, value := range []string{ws.ProfileID, ws.WorkspaceRoot, ws.ToolHomeDir, ws.RuntimeControlDir} {
			if value != "" && !validStructuralText(value, maxPrivateScalarBytes) {
				return Domainf("dispatch: private Worksource scalar is invalid")
			}
		}
		if ws.ProfilePresent && ws.ProfileID == "" {
			return Domainf("dispatch: private Worksource profile presence is incoherent")
		}
		if !ws.ProfilePresent && (ws.WorkspaceRoot != "" || ws.ToolHomeDir != "" || ws.RuntimeControlDir != "" ||
			len(ws.ArtifactRoots) != 0 || len(ws.ReadonlyMounts) != 0 || len(ws.DeniedPaths) != 0) {
			return Domainf("dispatch: absent private Worksource profile carries state")
		}
	}
	if !foundSelected {
		return Domainf("dispatch: selected Worksource is absent from the private projection")
	}
	if err := validatePrivateTaskSetupRoots(state.SubjectTaskSetupRoots); err != nil {
		return err
	}
	if state.SubjectTaskTargetRef != "" && !validStructuralText(state.SubjectTaskTargetRef, maxPrivateScalarBytes) {
		return Domainf("dispatch: private subject task target ref is invalid")
	}
	if assignment := state.SubjectTaskAssignment; assignment != nil {
		// Mirror the task_assignments CHECKs at the helper boundary, exactly
		// as the setup-receipt roots mirror theirs.
		if err := validateFirstTaskAssignment(FirstTaskAssignment{
			ObjectFormat: assignment.ObjectFormat, BaseSHA: assignment.BaseSHA,
			LocalRepoUUID: assignment.LocalRepoUUID, ClosureDigest: assignment.ClosureDigest,
		}); err != nil {
			return Domainf("dispatch: private subject task assignment projection is invalid")
		}
	}
	return nil
}

// validatePrivateTaskSetupRoots keeps the helper boundary strict about the
// frozen setup-receipt identities: they mirror the task_setup_receipts CHECK
// constraints (canonical decimal device/inode within 20 bytes, non-negative
// owner uid), are bounded, and arrive sorted+deduped so a hostile frame cannot
// smuggle an unordered or oversized set past the token.
func validatePrivateTaskSetupRoots(roots []PrivateDispatchTaskSetupIdentity) error {
	if len(roots) > substrate.MaxDispatchTaskSetupRoots {
		return Domainf("dispatch: private task setup roots exceed their bound")
	}
	for i, id := range roots {
		if !decimalIdentity.MatchString(id.Device) || !decimalIdentity.MatchString(id.Inode) ||
			len(id.Device) > 20 || len(id.Inode) > 20 || id.OwnerUID < 0 {
			return Domainf("dispatch: private task setup root identity is malformed")
		}
		if i > 0 && !taskSetupIdentityLess(roots[i-1], id) {
			return Domainf("dispatch: private task setup roots are unsorted or duplicated")
		}
	}
	return nil
}

func taskSetupIdentityLess(a, b PrivateDispatchTaskSetupIdentity) bool {
	if a.Device != b.Device {
		return a.Device < b.Device
	}
	if a.Inode != b.Inode {
		return a.Inode < b.Inode
	}
	return a.OwnerUID < b.OwnerUID
}

func strictStructuralTexts(values []string) bool {
	for i, value := range values {
		if !validStructuralText(value, maxPrivateScalarBytes) || (i > 0 && values[i-1] >= value) {
			return false
		}
	}
	return true
}

func validPrivateConsole(hour, minute int, timezone string) bool {
	if hour == 24 {
		return minute == 0 && validStructuralText(timezone, maxPrivateScalarBytes)
	}
	return hour >= 0 && hour <= 23 && validStructuralText(timezone, maxPrivateScalarBytes)
}

func privateIdentity(build string, control, schema, config int) dispatchProtocolIdentity {
	return dispatchProtocolIdentity{
		releaseBuildID: build, controlVersion: control,
		spineSchemaVersion: schema, configSchemaVersion: config,
	}
}

func validatePrivateIdentity(version int, build string, control, schema, config int, deployment, request string) error {
	if version != 1 || !validStructuralText(build, maxPrivateScalarBytes) || control != 1 || schema != substrate.CurrentSchemaVersion || config != 1 {
		return Domainf("dispatch: private frame version identity mismatch")
	}
	if !validStructuralText(deployment, maxPrivateScalarBytes) || !validLowercaseHex(request, 16) {
		return Domainf("dispatch: private frame deployment/request identity is invalid")
	}
	return nil
}

func validPrivateRole(role string) bool {
	switch dispatch.Role(role) {
	case dispatch.RoleEditor, dispatch.RoleEditorPlanReview, dispatch.RoleWorker,
		dispatch.RoleVerifier, dispatch.RolePackager, dispatch.RoleRefiner,
		dispatch.RoleStrategistPropose, dispatch.RoleStrategistInitiative, dispatch.RoleStrategistConsole:
		return true
	default:
		return false
	}
}

func strictPositiveIDs(ids []int64) bool {
	for i, id := range ids {
		if id <= 0 || (i > 0 && ids[i-1] >= id) {
			return false
		}
	}
	return true
}

func validStructuralTexts(values []string, maxBytes int) bool {
	for _, value := range values {
		if !validStructuralText(value, maxBytes) {
			return false
		}
	}
	return true
}

func validStructuralText(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func nonNilInt64s(in []int64) []int64 {
	if in == nil {
		return []int64{}
	}
	return in
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
