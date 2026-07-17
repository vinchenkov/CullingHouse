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
	response.Kind = "candidate"
	response.Candidate = privateCandidateFromPrepared(prepared.candidate)
	return response, nil
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
	if len(plan.Entries) > maxPlanMounts {
		return Domainf("dispatch: private mount plan exceeds %d entries (ADR-016 D2)", maxPlanMounts)
	}
	body, err := json.Marshal(plan)
	if err != nil || len(body) > maxDispatchMountPlanBytes {
		return Domainf("dispatch: private mount plan exceeds its byte budget")
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
		// never plan-addressable, whatever the broker claims.
		if !strings.HasPrefix(e.Destination, "/workspace/artifacts/") &&
			!strings.HasPrefix(e.Destination, "/workspace/references/") &&
			!validTaskPlanDestination(e.Destination) {
			return Domainf("dispatch: private mount entry %d destination is outside the ordinary namespace", i)
		}
		if e.Kind != "dir" && e.Kind != "file" {
			return Domainf("dispatch: private mount entry %d kind is invalid", i)
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
		if i > 0 && (e.Destination <= prior ||
			(strings.HasPrefix(e.Destination, prior+"/") && !mountOverlapPermitted(prior, e.Destination))) {
			return Domainf("dispatch: private mount destinations are unsorted or overlapping")
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
	return nil
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
