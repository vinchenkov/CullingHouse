package verbs

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mc/routing"
	"mc/substrate"
)

// Operational verbs (spec §16.4, §17, §18; wave-2 contract §1.5): doctor,
// backup, reset. All three are strictly host scope — refusal precedes any
// spine open — and Phase 2 proves CLI validation and file/state semantics;
// the container/auth/supervision probes and the named-volume story are
// Phase 3/5 (ADR-014).

// mcHomeDir resolves the deployment root (§16.1): the MC_HOME override or
// the ~/.mission-control default, always absolute.
func mcHomeDir() (string, error) {
	home := os.Getenv("MC_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", Usagef("resolve default MC_HOME: %v", err)
		}
		home = filepath.Join(userHome, ".mission-control")
	}
	if !filepath.IsAbs(home) {
		return "", Usagef("MC_HOME must be absolute, got %q", home)
	}
	return filepath.Clean(home), nil
}

// snapshotSpine writes one consistent spine snapshot into MC_HOME/backups/
// via VACUUM INTO — a fresh copy, never the live locked file (Inv. 24) — to
// a temp name, renamed on completion (§16.4). Retention is the resident tick
// chore's job, not this verb's.
func snapshotSpine(db *sql.DB, home string) (string, int64, error) {
	dir := filepath.Join(home, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, Domainf("create backups dir: %v", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	var final string
	for i := 0; ; i++ {
		name := "spine-" + stamp
		if i > 0 {
			name = fmt.Sprintf("%s-%d", name, i)
		}
		final = filepath.Join(dir, name+".db")
		if _, err := os.Stat(final); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", 0, Domainf("probe snapshot name %q: %v", final, err)
		}
	}
	tmp := fmt.Sprintf("%s.tmp-%d", final, os.Getpid())
	if _, err := db.Exec(`VACUUM INTO ?`, tmp); err != nil {
		os.Remove(tmp)
		return "", 0, Domainf("snapshot spine: %v", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return "", 0, Domainf("finalize snapshot: %v", err)
	}
	st, err := os.Stat(final)
	if err != nil {
		return "", 0, Domainf("stat snapshot: %v", err)
	}
	return final, st.Size(), nil
}

// requireSpineFile refuses before creating bytes: opening a missing path
// would materialize an empty spine, which no operational verb may do
// (§16.4 loss detection).
func requireSpineFile(spine string) error {
	if spine == "" {
		return Usagef("no spine: set MC_SPINE (or MC_HELPER to delegate)")
	}
	if _, err := os.Stat(spine); err != nil {
		return Domainf("no spine at %q: %v", spine, err)
	}
	return nil
}

// Backup is the §16.4 snapshot verb — the resident tick chore's, and manual.
func Backup(id *RunIdentity, spine string) (any, error) {
	if err := RequireHostScope(id, "mc backup"); err != nil {
		return nil, err
	}
	if err := requireSpineFile(spine); err != nil {
		return nil, err
	}
	home, err := mcHomeDir()
	if err != nil {
		return nil, err
	}
	db, err := substrate.Open(spine)
	if err != nil {
		return nil, Usagef("%v", err)
	}
	defer db.Close()
	snapshot, size, err := snapshotSpine(db, home)
	if err != nil {
		return nil, err
	}
	return map[string]any{"snapshot": snapshot, "bytes": size}, nil
}

// Reset is the one destructive verb (§16.4): confirmation-gated, and the
// snapshot must complete before anything is deleted — a failed snapshot
// aborts with the spine untouched. Deleting the spine file (+ WAL/SHM
// siblings) is the dev-tier stand-in for volume teardown; the named-volume
// story is Phase 3+. Output carries paths only, never config or secrets.
func Reset(id *RunIdentity, spine string, confirm bool) (any, error) {
	if err := RequireHostScope(id, "mc reset"); err != nil {
		return nil, err
	}
	if err := requireSpineFile(spine); err != nil {
		return nil, err
	}
	if !confirm {
		return nil, Domainf("mc reset is destructive: it snapshots the spine into MC_HOME/backups/ and then deletes %q; re-run with --confirm (§16.4)", spine)
	}
	home, err := mcHomeDir()
	if err != nil {
		return nil, err
	}
	db, err := substrate.Open(spine)
	if err != nil {
		return nil, Usagef("%v", err)
	}
	snapshot, _, err := snapshotSpine(db, home)
	db.Close()
	if err != nil {
		return nil, err
	}
	if err := os.Remove(spine); err != nil {
		return nil, Domainf("delete spine %q (snapshot %q is safe): %v", spine, snapshot, err)
	}
	for _, sibling := range []string{spine + "-wal", spine + "-shm"} {
		if err := os.Remove(sibling); err != nil && !os.IsNotExist(err) {
			return nil, Domainf("delete %q (snapshot %q is safe): %v", sibling, snapshot, err)
		}
	}
	return map[string]any{"spine": spine, "snapshot": snapshot, "reset": true}, nil
}

type DoctorFinding struct {
	Check          string `json:"check"`
	Status         string `json:"status"` // ok | fail | deferred
	Detail         string `json:"detail"`
	OnboardSection string `json:"onboard_section"` // §17 repairing section
}

type doctorFinding = DoctorFinding

// DoctorRuntimeReport is the path-free helper→host half of production doctor.
// It contains only spine-derived facts and the kernel capability result; no
// MC_HOME path or configuration byte crosses into or out of the helper.
type DoctorRuntimeReport struct {
	ProtocolVersion     int             `json:"protocol_version"`
	ReleaseBuildID      string          `json:"release_build_id"`
	ControlVersion      int             `json:"control_version"`
	SpineSchemaVersion  int             `json:"spine_schema_version"`
	ConfigSchemaVersion int             `json:"config_schema_version"`
	SpineUUID           string          `json:"spine_uuid,omitempty"`
	Findings            []DoctorFinding `json:"findings"`
}

type DoctorRuntimeRequest struct {
	ProtocolVersion     int    `json:"protocol_version"`
	ReleaseBuildID      string `json:"release_build_id"`
	ControlVersion      int    `json:"control_version"`
	SpineSchemaVersion  int    `json:"spine_schema_version"`
	ConfigSchemaVersion int    `json:"config_schema_version"`
}

func DoctorRuntimeUnavailable(req DoctorRuntimeRequest, detail string) DoctorRuntimeReport {
	runes := []rune(detail)
	if len(runes) > 2048 {
		detail = string(runes[:2048]) + "…"
	}
	return DoctorRuntimeReport{Findings: []DoctorFinding{
		{Check: "spine", Status: "fail", Detail: "not checked: " + detail, OnboardSection: "home"},
		{Check: "worksources", Status: "fail", Detail: "not checked: spine unavailable", OnboardSection: "worksource"},
		{Check: "surfaces", Status: "fail", Detail: "not checked: spine unavailable", OnboardSection: "surfaces"},
		{Check: "container-runtime", Status: "fail", Detail: detail, OnboardSection: "container"},
	}, ProtocolVersion: req.ProtocolVersion, ReleaseBuildID: req.ReleaseBuildID,
		ControlVersion: req.ControlVersion, SpineSchemaVersion: req.SpineSchemaVersion,
		ConfigSchemaVersion: req.ConfigSchemaVersion}
}

// DoctorRuntime filters the existing total doctor surface down to facts whose
// authority lives in the runtime kernel. The helper has no MC_HOME bind, so
// host findings computed by Doctor are discarded and recomputed on Darwin.
func DoctorRuntime(spine string, req DoctorRuntimeRequest, releaseBuildID string, controlVersion, configSchemaVersion int) (DoctorRuntimeReport, error) {
	if req.ProtocolVersion != 1 || req.ReleaseBuildID != releaseBuildID ||
		req.ControlVersion != controlVersion || req.SpineSchemaVersion != substrate.CurrentSchemaVersion ||
		req.ConfigSchemaVersion != configSchemaVersion {
		return DoctorRuntimeReport{}, Domainf("private doctor-runtime build/schema identity mismatch")
	}
	res, err := Doctor(nil, spine)
	if err != nil {
		return DoctorRuntimeReport{}, err
	}
	report := res.(map[string]any)
	all := report["findings"].([]doctorFinding)
	keep := map[string]bool{
		"spine": true, "worksources": true, "surfaces": true, "container-runtime": true,
	}
	runtimeFindings := make([]DoctorFinding, 0, len(keep))
	spineOK := false
	for _, finding := range all {
		if keep[finding.Check] {
			runtimeFindings = append(runtimeFindings, finding)
			if finding.Check == "spine" && finding.Status == "ok" {
				spineOK = true
			}
		}
	}
	result := DoctorRuntimeReport{
		ProtocolVersion: req.ProtocolVersion, ReleaseBuildID: req.ReleaseBuildID,
		ControlVersion: req.ControlVersion, SpineSchemaVersion: req.SpineSchemaVersion,
		ConfigSchemaVersion: req.ConfigSchemaVersion, Findings: runtimeFindings,
	}
	if spineOK {
		inspection, err := inspectSpineReadOnly(spine)
		if err != nil {
			return DoctorRuntimeReport{}, err
		}
		result.SpineUUID = inspection.uuid
	}
	return result, nil
}

// DoctorHostFacts computes only Darwin-authoritative facts. Passing no spine
// is deliberate: it makes a host database open impossible, and the filtered
// result contains only MC_HOME/routing and the two host-service findings.
func DoctorHostFacts() ([]DoctorFinding, error) {
	res, err := Doctor(nil, "")
	if err != nil {
		return nil, err
	}
	report := res.(map[string]any)
	all := report["findings"].([]doctorFinding)
	keep := map[string]bool{
		"mc-home": true, "routing": true, "runtime-auth": true, "supervision": true,
	}
	findings := make([]DoctorFinding, 0, len(keep))
	for _, finding := range all {
		if keep[finding.Check] {
			findings = append(findings, finding)
		}
	}
	return findings, nil
}

// ComposeDoctor joins the two authority-local reports and performs the only
// cross-kernel comparison: the small deployment UUID mirror versus the
// helper-proved spine UUID. It preserves Doctor's public finding order.
func ComposeDoctor(host []DoctorFinding, runtimeReport DoctorRuntimeReport) (any, error) {
	wantSection := map[string]string{
		"mc-home": "home", "spine": "home", "worksources": "worksource",
		"surfaces": "surfaces", "routing": "routing", "container-runtime": "container",
		"runtime-auth": "runtime-auth", "supervision": "supervision",
	}
	if len(host) != 4 || len(runtimeReport.Findings) != 4 {
		return nil, Domainf("composed doctor halves have invalid finding counts")
	}
	byCheck := map[string]DoctorFinding{}
	for _, finding := range append(append([]DoctorFinding{}, host...), runtimeReport.Findings...) {
		section, known := wantSection[finding.Check]
		if !known || finding.OnboardSection != section ||
			(finding.Status != "ok" && finding.Status != "fail" && finding.Status != "deferred") {
			return nil, Domainf("invalid composed doctor finding %q", finding.Check)
		}
		if _, duplicate := byCheck[finding.Check]; duplicate {
			return nil, Domainf("duplicate composed doctor finding %q", finding.Check)
		}
		byCheck[finding.Check] = finding
	}
	for _, required := range []string{"mc-home", "spine", "worksources", "surfaces", "routing", "container-runtime", "runtime-auth", "supervision"} {
		if _, ok := byCheck[required]; !ok {
			return nil, Domainf("missing composed doctor finding %q", required)
		}
	}

	identity := DoctorFinding{Check: "deployment-identity", Status: "fail", OnboardSection: "home"}
	switch {
	case byCheck["mc-home"].Status != "ok":
		identity.Detail = "not checked: MC_HOME unresolved"
	case byCheck["spine"].Status != "ok" || runtimeReport.SpineUUID == "":
		identity.Detail = "not checked: spine unavailable"
	default:
		home, err := mcHomeDir()
		if err != nil {
			identity.Detail = err.Error()
			break
		}
		mirrored, exists, mirrorErr := readDeploymentMirror(home)
		switch {
		case mirrorErr != nil:
			identity.Detail = mirrorErr.Error()
		case !exists:
			identity.Detail = fmt.Sprintf("%s is missing; run the home repair section (§16.4)", filepath.Join(home, deploymentUUIDFilename))
		case mirrored != runtimeReport.SpineUUID:
			identity.Detail = fmt.Sprintf("MC_HOME identity %s does not match spine identity %s — restore the matching backup (§16.4)", mirrored, runtimeReport.SpineUUID)
		default:
			identity.Status = "ok"
			identity.Detail = fmt.Sprintf("MC_HOME and spine agree on %s", runtimeReport.SpineUUID)
		}
	}

	order := []string{"mc-home", "spine", "deployment-identity", "worksources", "surfaces", "routing", "container-runtime", "runtime-auth", "supervision"}
	findings := make([]DoctorFinding, 0, len(order))
	for _, check := range order {
		if check == "deployment-identity" {
			findings = append(findings, identity)
		} else {
			findings = append(findings, byCheck[check])
		}
	}
	ok := true
	for _, finding := range findings {
		if finding.Status == "fail" {
			ok = false
		}
	}
	return map[string]any{"ok": ok, "findings": findings}, nil
}

// Doctor validates what Phase 2 can validate — MC_HOME shape, spine
// identity/schema + MC_HOME mirror, routing, configured surfaces, and
// worksource/sandbox references — and reports the
// Phase 3/5 probes (container runtime, gateway, runtime auth, supervision)
// as deferred so the finding surface is total from the start. Every finding
// names its repairing onboarding section (§17). Doctor mutates nothing and
// never creates spine bytes; it always exits 0 with `ok` carrying the
// verdict, so a failing deployment still yields the full findings list.
func Doctor(id *RunIdentity, spine string) (any, error) {
	if err := RequireHostScope(id, "mc doctor"); err != nil {
		return nil, err
	}
	findings := []doctorFinding{}
	add := func(check, status, detail, section string) {
		findings = append(findings, doctorFinding{
			Check: check, Status: status, Detail: detail, OnboardSection: section,
		})
	}

	home, err := mcHomeDir()
	switch {
	case err != nil:
		add("mc-home", "fail", err.Error(), "home")
	default:
		if st, statErr := os.Stat(home); statErr != nil || !st.IsDir() {
			add("mc-home", "fail", fmt.Sprintf("MC_HOME %q is not a directory: %v", home, statErr), "home")
		} else {
			add("mc-home", "ok", home, "home")
		}
	}

	spineOK := false
	spineUUID := ""
	switch {
	case spine == "":
		add("spine", "fail", "MC_SPINE is not set", "home")
	default:
		if _, statErr := os.Stat(spine); statErr != nil {
			add("spine", "fail", fmt.Sprintf("no spine at %q — restore from backup (§16.4), never re-initialize over a deployment", spine), "home")
		} else if db, openErr := substrate.Open(spine); openErr != nil {
			add("spine", "fail", fmt.Sprintf("open spine: %v", openErr), "home")
		} else {
			func() {
				defer db.Close()
				var uuid string
				var version int
				err := db.QueryRow(`SELECT deployment_uuid, schema_version FROM meta WHERE id = 1`).Scan(&uuid, &version)
				switch {
				case err != nil:
					add("spine", "fail", fmt.Sprintf("spine has no meta identity (%v) — restore from backup (§16.4)", err), "home")
				case version > substrate.CurrentSchemaVersion:
					add("spine", "fail", fmt.Sprintf("schema version %d is newer than this build's %d — upgrade mc or restore from backup (§16.4)", version, substrate.CurrentSchemaVersion), "home")
				case version < substrate.CurrentSchemaVersion:
					// §16.4's migrate arm is onboard's; doctor names the repair.
					add("spine", "fail", fmt.Sprintf("schema version %d, expected %d", version, substrate.CurrentSchemaVersion), "home")
				default:
					add("spine", "ok", fmt.Sprintf("deployment %s schema %d", uuid, version), "home")
					spineOK = true
					spineUUID = uuid
				}
			}()
		}
	}

	switch {
	case err != nil:
		add("deployment-identity", "fail", "not checked: MC_HOME unresolved", "home")
	case !spineOK:
		add("deployment-identity", "fail", "not checked: spine unavailable", "home")
	default:
		mirrored, exists, mirrorErr := readDeploymentMirror(home)
		switch {
		case mirrorErr != nil:
			add("deployment-identity", "fail", mirrorErr.Error(), "home")
		case !exists:
			add("deployment-identity", "fail", fmt.Sprintf("%s is missing; run the home repair section (§16.4)", filepath.Join(home, deploymentUUIDFilename)), "home")
		case mirrored != spineUUID:
			add("deployment-identity", "fail", fmt.Sprintf("MC_HOME identity %s does not match spine identity %s — restore the matching backup (§16.4)", mirrored, spineUUID), "home")
		default:
			add("deployment-identity", "ok", fmt.Sprintf("MC_HOME and spine agree on %s", spineUUID), "home")
		}
	}

	if spineOK {
		db, openErr := substrate.Open(spine)
		if openErr != nil {
			add("worksources", "fail", fmt.Sprintf("open spine: %v", openErr), "worksource")
		} else {
			func() {
				defer db.Close()
				var total, broken int
				err := db.QueryRow(`
					SELECT COUNT(*),
					       COUNT(*) FILTER (WHERE p.id IS NULL OR p.workspace_root IS NULL
					                        OR substr(p.workspace_root, 1, 1) <> '/')
					FROM worksources w LEFT JOIN sandbox_profiles p ON p.id = w.sandbox_profile`,
				).Scan(&total, &broken)
				switch {
				case err != nil:
					add("worksources", "fail", fmt.Sprintf("inspect worksources: %v", err), "worksource")
				case total == 0:
					add("worksources", "fail", "no Worksource rows", "worksource")
				case broken != 0:
					add("worksources", "fail", fmt.Sprintf("%d of %d Worksources have a missing sandbox profile or non-absolute workspace_root", broken, total), "worksource")
				default:
					add("worksources", "ok", fmt.Sprintf("%d Worksources, sandbox profiles resolve", total), "worksource")
				}
			}()
		}
	} else {
		add("worksources", "fail", "not checked: spine unavailable", "worksource")
	}

	if spineOK {
		db, openErr := substrate.Open(spine)
		if openErr != nil {
			add("surfaces", "fail", fmt.Sprintf("open spine: %v", openErr), "surfaces")
		} else {
			func() {
				defer db.Close()
				var hour, minute int
				var tz string
				queryErr := db.QueryRow(`SELECT console_hour, console_minute, console_tz FROM lock WHERE id = 1`).Scan(&hour, &minute, &tz)
				switch {
				case queryErr != nil:
					add("surfaces", "fail", fmt.Sprintf("read Daily Console schedule: %v", queryErr), "surfaces")
				case hour == 24:
					add("surfaces", "fail", "Daily Console schedule is not configured", "surfaces")
				default:
					_, tzErr := time.LoadLocation(tz)
					if hour < 0 || hour > 23 || minute < 0 || minute > 59 || tzErr != nil {
						add("surfaces", "fail", fmt.Sprintf("invalid Daily Console schedule %02d:%02d %s", hour, minute, tz), "surfaces")
					} else {
						add("surfaces", "ok", fmt.Sprintf("Daily Console at %02d:%02d %s", hour, minute, tz), "surfaces")
					}
				}
			}()
		}
	} else {
		add("surfaces", "fail", "not checked: spine unavailable", "surfaces")
	}

	if err != nil {
		add("routing", "fail", "not checked: MC_HOME unresolved", "routing")
	} else {
		path := filepath.Join(home, "routing.md")
		registry, allowFakeDecorrelation := routing.ActiveRegistry()
		if routeErr := validateRoutingPath(path, registry, allowFakeDecorrelation); routeErr != nil {
			add("routing", "fail", routeErr.Error(), "routing")
		} else {
			add("routing", "ok", path, "routing")
		}
	}

	// The gateway finding is retired, not deferred: ADR-022 deleted the egress
	// gateway, so there is no probe to defer. Credential-projection health is
	// the runtime-auth row's job.
	{
		status, detail := containerRuntimeFinding(spine)
		add("container-runtime", status, detail, "container")
	}
	// Per-binding runtime-auth health is a live token-validity turn against a
	// real subscription — Phase 5's operator-scheduled acceptance, not a
	// Phase-3-owned deferral (§8).
	add("runtime-auth", "deferred", "per-binding runtime-auth health runs in Phase 5 (§11.4, §17)", "runtime-auth")
	add("supervision", "deferred", "launchd/resident supervision check runs in Phase 5 (§17)", "supervision")

	ok := true
	for _, f := range findings {
		if f.Status == "fail" {
			ok = false
		}
	}
	return map[string]any{"ok": ok, "findings": findings}, nil
}
