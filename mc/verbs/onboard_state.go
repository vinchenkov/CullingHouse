package verbs

import (
	"os"
	"path/filepath"

	"mc/substrate"
)

// OnboardStateRequest is the closed host→helper frame for the three
// spine-backed onboarding sections. WorkspaceRoot is schema data attested by
// Darwin; the helper stores or compares it but never treats it as a path.
type OnboardStateRequest struct {
	ProtocolVersion     int    `json:"protocol_version"`
	ReleaseBuildID      string `json:"release_build_id"`
	ControlVersion      int    `json:"control_version"`
	SpineSchemaVersion  int    `json:"spine_schema_version"`
	ConfigSchemaVersion int    `json:"config_schema_version"`
	DeploymentUUID      string `json:"deployment_uuid"`
	Section             string `json:"section"`

	Worksource    string `json:"worksource,omitempty"`
	WorkspaceRoot string `json:"workspace_root,omitempty"`

	TimeoutMinutes      int `json:"timeout_minutes,omitempty"`
	GraceMinutes        int `json:"grace_minutes,omitempty"`
	HeartbeatIntervalS  int `json:"heartbeat_interval_s,omitempty"`
	SpawnGraceS         int `json:"spawn_grace_s,omitempty"`
	HardDeadlineMinutes int `json:"hard_deadline_minutes,omitempty"`

	ConsoleScheduleSet bool   `json:"console_schedule_set,omitempty"`
	ConsoleHour        int    `json:"console_hour,omitempty"`
	ConsoleMinute      int    `json:"console_minute,omitempty"`
	ConsoleTZ          string `json:"console_tz,omitempty"`
}

type OnboardStateResult struct {
	ProtocolVersion     int                    `json:"protocol_version"`
	ReleaseBuildID      string                 `json:"release_build_id"`
	ControlVersion      int                    `json:"control_version"`
	SpineSchemaVersion  int                    `json:"spine_schema_version"`
	ConfigSchemaVersion int                    `json:"config_schema_version"`
	DeploymentUUID      string                 `json:"deployment_uuid"`
	Section             string                 `json:"section"`
	Status              string                 `json:"status"`
	Detail              string                 `json:"detail"`
	WorkspaceRoots      []OnboardWorkspaceRoot `json:"workspace_roots,omitempty"`
}

func onboardStateBase(a OnboardArgs, releaseBuildID string, controlVersion, configSchemaVersion int) OnboardStateRequest {
	return OnboardStateRequest{
		ProtocolVersion: 1, ReleaseBuildID: releaseBuildID,
		ControlVersion: controlVersion, SpineSchemaVersion: substrate.CurrentSchemaVersion,
		ConfigSchemaVersion: configSchemaVersion, Section: a.Section,
		Worksource: a.Worksource, WorkspaceRoot: a.WorkspaceRoot,
		TimeoutMinutes: a.TimeoutMinutes, GraceMinutes: a.GraceMinutes,
		HeartbeatIntervalS: a.HeartbeatIntervalS, SpawnGraceS: a.SpawnGraceS,
		HardDeadlineMinutes: a.HardDeadlineMinutes,
		ConsoleScheduleSet:  a.ConsoleScheduleSet, ConsoleHour: a.ConsoleHour,
		ConsoleMinute: a.ConsoleMinute, ConsoleTZ: a.ConsoleTZ,
	}
}

// PrepareOnboardState performs every host-authoritative check and binds the
// request to the deployment mirror. It deliberately refuses host-only
// sections so no routing/config bytes can enter this frame.
func PrepareOnboardState(a OnboardArgs, releaseBuildID string, controlVersion, configSchemaVersion int) (OnboardStateRequest, error) {
	if !validStructuralText(releaseBuildID, maxPrivateScalarBytes) || releaseBuildID == "" {
		return OnboardStateRequest{}, Usagef("private onboard-state release identity is invalid")
	}
	var err error
	switch a.Section {
	case "routing":
	case "supervision":
	case "worksource":
		a, err = prepareOnboardWorksource(a)
		if err != nil {
			return OnboardStateRequest{}, err
		}
	case "tunables":
		for _, value := range []int{a.TimeoutMinutes, a.GraceMinutes, a.HeartbeatIntervalS, a.SpawnGraceS, a.HardDeadlineMinutes} {
			if value < 0 {
				return OnboardStateRequest{}, Usagef("mc onboard tunables values must be non-negative")
			}
		}
	case "surfaces":
		if err := validateOnboardSurfaceAnswers(a); err != nil {
			return OnboardStateRequest{}, err
		}
	default:
		return OnboardStateRequest{}, Usagef("section %q is not a private onboarding state section", a.Section)
	}
	home, err := mcHomeDir()
	if err != nil {
		return OnboardStateRequest{}, err
	}
	uuid, exists, err := readDeploymentMirror(home)
	if err != nil {
		return OnboardStateRequest{}, err
	}
	if !exists {
		return OnboardStateRequest{}, Domainf("deployment identity is not provisioned; run mc onboard home first (§16.4)")
	}
	req := onboardStateBase(a, releaseBuildID, controlVersion, configSchemaVersion)
	req.DeploymentUUID = uuid
	return req, nil
}

// RecheckOnboardStateHost narrows the unavoidable pathname race immediately
// before the helper call. The finalizer checks the returned roots again after
// the transaction; at no point does the helper receive filesystem authority.
func RecheckOnboardStateHost(req OnboardStateRequest) error {
	if req.Section != "worksource" || req.WorkspaceRoot == "" {
		return nil
	}
	st, err := os.Stat(req.WorkspaceRoot)
	if err != nil {
		return Domainf("recheck workspace root %q: %v", req.WorkspaceRoot, err)
	}
	if !st.IsDir() {
		return Domainf("recheck workspace root %q: not a directory", req.WorkspaceRoot)
	}
	resolved, err := filepath.EvalSymlinks(req.WorkspaceRoot)
	if err != nil {
		return Domainf("recheck workspace root %q: %v", req.WorkspaceRoot, err)
	}
	if filepath.Clean(resolved) != req.WorkspaceRoot {
		return Domainf("workspace root %q changed after host attestation; retry onboarding", req.WorkspaceRoot)
	}
	return nil
}

func validateOnboardStateIdentity(req OnboardStateRequest, releaseBuildID string, controlVersion, configSchemaVersion int) error {
	if req.ProtocolVersion != 1 || req.ReleaseBuildID != releaseBuildID ||
		req.ControlVersion != controlVersion || req.SpineSchemaVersion != substrate.CurrentSchemaVersion ||
		req.ConfigSchemaVersion != configSchemaVersion || req.DeploymentUUID == "" {
		return Domainf("private onboard-state build/schema identity mismatch")
	}
	worksourceFields := req.Worksource != "" || req.WorkspaceRoot != ""
	tunableFields := req.TimeoutMinutes != 0 || req.GraceMinutes != 0 || req.HeartbeatIntervalS != 0 ||
		req.SpawnGraceS != 0 || req.HardDeadlineMinutes != 0
	surfaceFields := req.ConsoleScheduleSet || req.ConsoleHour != 0 || req.ConsoleMinute != 0 || req.ConsoleTZ != ""
	switch req.Section {
	case "routing":
		if worksourceFields || tunableFields || surfaceFields {
			return Domainf("private onboard-state routing frame carries another section's fields")
		}
	case "supervision":
		if worksourceFields || tunableFields || surfaceFields {
			return Domainf("private onboard-state supervision frame carries another section's fields")
		}
	case "worksource":
		if tunableFields || surfaceFields {
			return Domainf("private onboard-state worksource frame carries another section's fields")
		}
	case "tunables":
		if worksourceFields || surfaceFields {
			return Domainf("private onboard-state tunables frame carries another section's fields")
		}
	case "surfaces":
		if worksourceFields || tunableFields {
			return Domainf("private onboard-state surfaces frame carries another section's fields")
		}
	default:
		return Usagef("unknown private onboard-state section %q", req.Section)
	}
	return nil
}

func onboardArgsFromState(spine string, req OnboardStateRequest) OnboardArgs {
	return OnboardArgs{
		Section: req.Section, Spine: spine, Worksource: req.Worksource, WorkspaceRoot: req.WorkspaceRoot,
		TimeoutMinutes: req.TimeoutMinutes, GraceMinutes: req.GraceMinutes,
		HeartbeatIntervalS: req.HeartbeatIntervalS, SpawnGraceS: req.SpawnGraceS,
		HardDeadlineMinutes: req.HardDeadlineMinutes,
		ConsoleScheduleSet:  req.ConsoleScheduleSet, ConsoleHour: req.ConsoleHour,
		ConsoleMinute: req.ConsoleMinute, ConsoleTZ: req.ConsoleTZ,
	}
}

// OnboardState runs only inside the fixed helper scope. It checks the mirror
// identity against the spine before any section mutation.
func OnboardState(spine string, req OnboardStateRequest, releaseBuildID string, controlVersion, configSchemaVersion int) (OnboardStateResult, error) {
	if err := validateOnboardStateIdentity(req, releaseBuildID, controlVersion, configSchemaVersion); err != nil {
		return OnboardStateResult{}, err
	}
	inspection, err := inspectSpineReadOnly(spine)
	if err != nil {
		return OnboardStateResult{}, err
	}
	if !inspection.exists {
		return OnboardStateResult{}, Domainf("private onboard-state deployment identity mismatch")
	}
	if err := validateInspection(spine, inspection); err != nil {
		return OnboardStateResult{}, err
	}
	if inspection.version != substrate.CurrentSchemaVersion || inspection.uuid != req.DeploymentUUID {
		return OnboardStateResult{}, Domainf("private onboard-state deployment identity mismatch")
	}
	a := onboardArgsFromState(spine, req)
	var status, detail string
	var roots []OnboardWorkspaceRoot
	switch req.Section {
	case "routing":
		status, detail = "ok", "deployment identity verified"
	case "worksource":
		if _, err := prepareOnboardWorksourceFrame(a); err != nil {
			return OnboardStateResult{}, err
		}
		status, detail, roots, err = onboardWorksourceDB(a)
	case "tunables":
		status, detail, err = onboardTunables(a)
	case "surfaces":
		status, detail, err = onboardSurfaces(a)
	case "supervision":
		status, detail, roots, err = onboardWorksourceDB(OnboardArgs{Section: "worksource", Spine: spine})
	}
	if err != nil {
		return OnboardStateResult{}, err
	}
	return OnboardStateResult{
		ProtocolVersion: req.ProtocolVersion, ReleaseBuildID: req.ReleaseBuildID,
		ControlVersion: req.ControlVersion, SpineSchemaVersion: req.SpineSchemaVersion,
		ConfigSchemaVersion: req.ConfigSchemaVersion, DeploymentUUID: req.DeploymentUUID,
		Section: req.Section, Status: status, Detail: detail, WorkspaceRoots: roots,
	}, nil
}

func prepareOnboardWorksourceFrame(a OnboardArgs) (OnboardArgs, error) {
	supplied := a.Worksource != "" || a.WorkspaceRoot != ""
	if supplied && (a.Worksource == "" || a.WorkspaceRoot == "") {
		return OnboardArgs{}, Usagef("mc onboard worksource requires --worksource and --workspace-root together")
	}
	if supplied && (!validStructuralText(a.Worksource, maxPrivateScalarBytes) ||
		!validStructuralText(a.WorkspaceRoot, maxPrivateScalarBytes) || !filepathIsCleanAbsolute(a.WorkspaceRoot)) {
		return OnboardArgs{}, Usagef("private onboard worksource frame is invalid")
	}
	return a, nil
}

func filepathIsCleanAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && path == filepath.Clean(path)
}

func FinalizeOnboardState(req OnboardStateRequest, result OnboardStateResult) (string, string, error) {
	if result.ProtocolVersion != req.ProtocolVersion || result.ReleaseBuildID != req.ReleaseBuildID ||
		result.ControlVersion != req.ControlVersion || result.SpineSchemaVersion != req.SpineSchemaVersion ||
		result.ConfigSchemaVersion != req.ConfigSchemaVersion || result.DeploymentUUID != req.DeploymentUUID ||
		result.Section != req.Section || (result.Status != "ok" && result.Status != "done") ||
		!validStructuralText(result.Detail, maxPrivateScalarBytes) {
		return "", "", Domainf("private onboard-state response identity mismatch")
	}
	if req.Section == "worksource" || req.Section == "supervision" {
		if len(result.WorkspaceRoots) == 0 {
			return "", "", Domainf("private onboard-state response omitted workspace roots")
		}
		for _, root := range result.WorkspaceRoots {
			if !validStructuralText(root.ID, maxPrivateScalarBytes) || !validStructuralText(root.Root, maxPrivateScalarBytes) ||
				!filepathIsCleanAbsolute(root.Root) {
				return "", "", Domainf("private onboard-state response carries an invalid workspace root")
			}
		}
		if req.Section == "worksource" && req.Worksource != "" && (len(result.WorkspaceRoots) != 1 ||
			result.WorkspaceRoots[0].ID != req.Worksource || result.WorkspaceRoots[0].Root != req.WorkspaceRoot) {
			return "", "", Domainf("private onboard-state response changed the requested Worksource")
		}
		if err := validateOnboardWorkspaceRoots(result.WorkspaceRoots); err != nil {
			return "", "", err
		}
	} else if len(result.WorkspaceRoots) != 0 {
		return "", "", Domainf("private onboard-state response carries unexpected workspace roots")
	}
	return result.Status, result.Detail, nil
}

func OnboardRoutingHost() (string, string, error) { return onboardRouting() }
