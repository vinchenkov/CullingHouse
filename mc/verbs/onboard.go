package verbs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mc/routing"
	"mc/substrate"
)

// mc onboard (§17, §18, ADR-015): the sectioned wizard at the Phase-2 tier.
// Named sections only, in the §17 order; every section is idempotent —
// re-running skips what is already healthy — and fail-closed: the first
// failing or input-starved section aborts the run with its repairing
// instruction, and completed sections' writes persist (resumable). The
// Docker/host-effect sections (runtime-auth, container, supervision) are
// deferred doubles until Phase 3/5; supervision NEVER loads launchd during
// development (spike S7 is history's one sanctioned exception).

var onboardSections = []string{
	"preflight", "home", "runtime-auth", "routing", "container",
	"worksource", "tunables", "surfaces", "supervision", "verify",
}

// defaultRoutingTable is the §9.1 default: decorrelated producer/judge pairs
// across the two subscription harness families.
const defaultRoutingTable = `# Mission Control routing (§9.1 default)

| role | harness | binding |
| --- | --- | --- |
| strategist | claude-sdk | claude |
| editor | codex | chatgpt |
| worker | claude-sdk | minimax |
| verifier | codex | chatgpt |
| packager | claude-sdk | minimax |
| refiner | codex | chatgpt |
| homie | claude-sdk | claude |
`

type OnboardArgs struct {
	Section string
	Spine   string
	Smoke   bool
	// Dual-input answers (§17): supplied as flags by the shepherding agent.
	Worksource    string
	WorkspaceRoot string
	// Tunables; zero = accept the §16.3 schema defaults.
	TimeoutMinutes      int
	GraceMinutes        int
	HeartbeatIntervalS  int
	SpawnGraceS         int
	HardDeadlineMinutes int
	// Daily Console schedule (surfaces section).
	ConsoleScheduleSet bool
	ConsoleHour        int
	ConsoleMinute      int
	ConsoleTZ          string
}

func Onboard(id *RunIdentity, a OnboardArgs) (any, error) {
	if err := RequireHostScope(id, "mc onboard"); err != nil {
		return nil, err
	}
	if a.Smoke {
		return nil, Domainf("mc onboard --smoke is the full-pipeline ride-through and needs the Phase 4/5 container tier (§17)")
	}
	sections := onboardSections
	if a.Section != "" {
		known := false
		for _, s := range onboardSections {
			if s == a.Section {
				known = true
				break
			}
		}
		if !known {
			return nil, Usagef("unknown onboarding section %q; sections: %s (§17)",
				a.Section, strings.Join(onboardSections, "|"))
		}
		sections = []string{a.Section}
	}
	results := make([]map[string]any, 0, len(sections))
	for _, section := range sections {
		status, detail, err := onboardSection(section, a)
		if err != nil {
			return nil, err
		}
		results = append(results, map[string]any{
			"section": section, "status": status, "detail": detail,
		})
	}
	return map[string]any{"ok": true, "sections": results}, nil
}

func onboardSection(section string, a OnboardArgs) (string, string, error) {
	switch section {
	case "preflight":
		return onboardPreflight()
	case "home":
		return onboardHome(a.Spine)
	case "runtime-auth":
		return "deferred", "subscription auth flows, live no-op turns, and the forbidden-env scan run in Phase 3/5 (§11.4, §17)", nil
	case "routing":
		return onboardRouting()
	case "container":
		return "deferred", "image build, warm helper round-trip, and the two-leg gateway probe run in Phase 3 (§11.4, §17)", nil
	case "worksource":
		return onboardWorksource(a)
	case "tunables":
		return onboardTunables(a)
	case "surfaces":
		return onboardSurfaces(a)
	case "supervision":
		return "deferred", "init-system units load once, operator-present, in Phase 5 — never during development (§12, §17)", nil
	case "verify":
		return onboardVerify(a.Spine)
	}
	return "", "", Usagef("unknown onboarding section %q", section)
}

// onboardPreflight writes nothing (§17: nothing is written until preflight
// passes): MC_HOME resolves outside every git working tree (§16.1) and git
// is present. The container-runtime reachability probe is Phase 3.
func onboardPreflight() (string, string, error) {
	home, err := mcHomeDir()
	if err != nil {
		return "", "", err
	}
	for dir := home; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return "", "", Domainf("MC_HOME %q is inside a git working tree (%q); choose a path outside any repository (§16.1)", home, dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", "", Domainf("git is required and not on PATH (§17 preflight)")
	}
	return "ok", fmt.Sprintf("%s/%s; MC_HOME %s; container-runtime probe deferred to Phase 3", runtime.GOOS, runtime.GOARCH, home), nil
}

// onboardHome scaffolds the MC_HOME tree and provisions the spine under the
// §16.4 identity rules: meta present and current → skip; a spine with tables
// but no meta identity → abort loudly, restore from backup; empty or missing
// → initialize schema, triggers, and the deployment UUID.
func onboardHome(spine string) (string, string, error) {
	home, err := mcHomeDir()
	if err != nil {
		return "", "", err
	}
	if spine == "" {
		return "", "", Usagef("mc onboard home requires MC_SPINE")
	}
	for _, dir := range []string{"", "backups", "sessions", "outputs", "attachments", "workflows"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			return "", "", Domainf("scaffold MC_HOME: %v", err)
		}
	}
	db, err := substrate.Open(spine)
	if err != nil {
		return "", "", Usagef("%v", err)
	}
	defer db.Close()

	var uuid string
	var version int
	metaErr := db.QueryRow(`SELECT deployment_uuid, schema_version FROM meta WHERE id = 1`).Scan(&uuid, &version)
	switch {
	case metaErr == nil && version == 1:
		return "ok", fmt.Sprintf("deployment %s already provisioned (schema %d)", uuid, version), nil
	case metaErr == nil:
		return "", "", Domainf("spine schema_version %d has no defined migration; refusing (§16.4)", version)
	}
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		return "", "", Domainf("inspect spine: %v", err)
	}
	if tables != 0 {
		return "", "", Domainf("spine at %q has %d tables but no meta identity — no onboarding path may re-initialize it; restore from backup (§16.4)", spine, tables)
	}
	if err := substrate.Init(db); err != nil {
		return "", "", Domainf("%v", err)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", Usagef("generate deployment uuid: %v", err)
	}
	uuid = hex.EncodeToString(raw[:])
	if _, err := db.Exec(`INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, ?, 1)`, uuid); err != nil {
		return "", "", Domainf("seed meta identity: %v", err)
	}
	return "done", fmt.Sprintf("deployment %s provisioned", uuid), nil
}

// onboardRouting validates the operator's routing.md, or writes the §9.1
// default when none exists — validated before writing, so an invalid table
// can never come out of onboarding (§17). It never rewrites an existing
// file, valid or not.
func onboardRouting() (string, string, error) {
	home, err := mcHomeDir()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", "", Domainf("scaffold MC_HOME: %v", err)
	}
	path := filepath.Join(home, "routing.md")
	registry, allowFakeDecorrelation := routing.ActiveRegistry()
	data, readErr := os.ReadFile(path)
	if readErr == nil {
		if _, err := routing.Parse(data, registry, allowFakeDecorrelation); err != nil {
			return "", "", Domainf("invalid routing.md at %q: %v — repair it or remove it to accept the default (§17 routing)", path, err)
		}
		return "ok", path, nil
	}
	if !os.IsNotExist(readErr) {
		return "", "", Domainf("read routing.md at %q: %v", path, readErr)
	}
	if _, err := routing.Parse([]byte(defaultRoutingTable), registry, allowFakeDecorrelation); err != nil {
		return "", "", Domainf("default routing table failed validation: %v", err)
	}
	if err := os.WriteFile(path, []byte(defaultRoutingTable), 0o600); err != nil {
		return "", "", Domainf("write routing.md: %v", err)
	}
	return "done", path, nil
}

// onboardWorksource seeds the first Worksource from the dual-input answers
// (§17 section 6). An existing Worksource means the deployment is past this
// section; the full interactive richness (git contract, sandbox review,
// directive authoring) rides on `mc worksource add` and the agent shepherd.
func onboardWorksource(a OnboardArgs) (string, string, error) {
	if err := requireSpineFile(a.Spine); err != nil {
		return "", "", err
	}
	db, err := substrate.Open(a.Spine)
	if err != nil {
		return "", "", Usagef("%v", err)
	}
	defer db.Close()
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worksources`).Scan(&existing); err != nil {
		return "", "", Domainf("inspect worksources (run: mc onboard home first): %v", err)
	}
	if existing > 0 {
		return "ok", fmt.Sprintf("%d Worksources registered", existing), nil
	}
	if a.Worksource == "" || a.WorkspaceRoot == "" {
		return "", "", Domainf("the first Worksource needs operator input: re-run with --worksource <id> --workspace-root </abs/path> (§17 dual-input)")
	}
	if !filepath.IsAbs(a.WorkspaceRoot) {
		return "", "", Usagef("mc onboard worksource --workspace-root must be absolute, got %q", a.WorkspaceRoot)
	}
	err = inTx(db, func(ctx context.Context, q Q) error {
		if _, err := q.ExecContext(ctx, `
			INSERT INTO sandbox_profiles (id, workspace_root, egress_policy)
			VALUES ('default', ?, 'none')
			ON CONFLICT (id) DO NOTHING`, a.WorkspaceRoot); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `
			INSERT INTO worksources (id, title, kind, sandbox_profile)
			VALUES (?, ?, 'repo', 'default')`, a.Worksource, a.Worksource)
		return err
	})
	if err != nil {
		return "", "", err
	}
	return "done", fmt.Sprintf("Worksource %s at %s", a.Worksource, a.WorkspaceRoot), nil
}

func onboardTunables(a OnboardArgs) (string, string, error) {
	changed := map[string]int{}
	for col, v := range map[string]int{
		"timeout_minutes":       a.TimeoutMinutes,
		"grace_minutes":         a.GraceMinutes,
		"heartbeat_interval_s":  a.HeartbeatIntervalS,
		"spawn_grace_s":         a.SpawnGraceS,
		"hard_deadline_minutes": a.HardDeadlineMinutes,
	} {
		if v > 0 {
			changed[col] = v
		}
	}
	if len(changed) == 0 {
		return "ok", "§16.3 defaults accepted (recommended)", nil
	}
	if err := requireSpineFile(a.Spine); err != nil {
		return "", "", err
	}
	db, err := substrate.Open(a.Spine)
	if err != nil {
		return "", "", Usagef("%v", err)
	}
	defer db.Close()
	names := make([]string, 0, len(changed))
	err = inTx(db, func(ctx context.Context, q Q) error {
		for col, v := range changed {
			if _, err := q.ExecContext(ctx,
				fmt.Sprintf(`UPDATE lock SET %s = ? WHERE id = 1`, col), v); err != nil {
				return err
			}
			names = append(names, col)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return "done", "applied: " + strings.Join(names, ", "), nil
}

// onboardSurfaces applies the Daily Console schedule when answered; the
// dashboard is core with loopback defaults, and Discord is skippable
// entirely (§17 section 8) — both land with their Phase 3/5 processes.
func onboardSurfaces(a OnboardArgs) (string, string, error) {
	if !a.ConsoleScheduleSet {
		return "ok", "dashboard defaults accepted; Daily Console schedule unset; Discord skipped (optional, §17)", nil
	}
	if a.ConsoleHour < 0 || a.ConsoleHour > 23 {
		return "", "", Usagef("mc onboard surfaces --console-hour must be 0..23")
	}
	if a.ConsoleMinute < 0 || a.ConsoleMinute > 59 {
		return "", "", Usagef("mc onboard surfaces --console-minute must be 0..59")
	}
	if _, err := time.LoadLocation(a.ConsoleTZ); a.ConsoleTZ == "" || err != nil {
		return "", "", Usagef("mc onboard surfaces --console-tz %q is not a loadable IANA timezone", a.ConsoleTZ)
	}
	if err := requireSpineFile(a.Spine); err != nil {
		return "", "", err
	}
	db, err := substrate.Open(a.Spine)
	if err != nil {
		return "", "", Usagef("%v", err)
	}
	defer db.Close()
	err = inTx(db, func(ctx context.Context, q Q) error {
		_, err := q.ExecContext(ctx, `
			UPDATE lock SET console_hour = ?, console_minute = ?, console_tz = ?
			WHERE id = 1`, a.ConsoleHour, a.ConsoleMinute, a.ConsoleTZ)
		return err
	})
	if err != nil {
		return "", "", err
	}
	return "done", fmt.Sprintf("Daily Console at %02d:%02d %s", a.ConsoleHour, a.ConsoleMinute, a.ConsoleTZ), nil
}

// onboardVerify is the §17 closing section: a full mc doctor, failing the
// run when any check fails, with each failure naming its repairing section.
func onboardVerify(spine string) (string, string, error) {
	res, err := Doctor(nil, spine)
	if err != nil {
		return "", "", err
	}
	report := res.(map[string]any)
	findings := report["findings"].([]doctorFinding)
	okCount, deferredCount := 0, 0
	failing := []string{}
	for _, f := range findings {
		switch f.Status {
		case "ok":
			okCount++
		case "deferred":
			deferredCount++
		case "fail":
			failing = append(failing, fmt.Sprintf("%s (run: mc onboard %s)", f.Check, f.OnboardSection))
		}
	}
	if len(failing) != 0 {
		return "", "", Domainf("mc doctor reports failing checks: %s", strings.Join(failing, "; "))
	}
	return "ok", fmt.Sprintf("mc doctor: %d checks ok, %d deferred to Phase 3/5", okCount, deferredCount), nil
}
