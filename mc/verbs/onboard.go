package verbs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
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
	if a.Smoke {
		return nil, Domainf("mc onboard --smoke is the full-pipeline ride-through and needs the Phase 4/5 container tier (§17)")
	}
	results := make([]map[string]any, 0, len(sections))
	for _, section := range sections {
		if section != "preflight" && section != "home" {
			if err := requireOnboardIdentity(a.Spine); err != nil {
				return nil, err
			}
		}
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

const deploymentUUIDFilename = "deployment.uuid"

type spineInspection struct {
	exists  bool
	bytes   int64
	tables  int
	hasMeta bool
	uuid    string
	version int
}

// inspectSpineReadOnly is the §16.4 meta-first gate. In particular, it does
// not use substrate.Open: that connection deliberately applies WAL pragmas,
// which would mutate a foreign database before onboarding had accepted its
// identity.
func inspectSpineReadOnly(spine string) (spineInspection, error) {
	if spine == "" {
		return spineInspection{}, Usagef("mc onboard home requires MC_SPINE")
	}
	// This inspection is the one deliberate bypass of substrate.Open (it must
	// not WAL-mutate a foreign database), so it carries the lock-domain guard
	// itself. Inv. 24 binds every open, including read-only ones.
	if err := substrate.GuardLockDomain(spine); err != nil {
		return spineInspection{}, Domainf("%v", err)
	}
	st, err := os.Stat(spine)
	if os.IsNotExist(err) {
		return spineInspection{}, nil
	}
	if err != nil {
		return spineInspection{}, Domainf("inspect spine %q: %v", spine, err)
	}
	if st.IsDir() {
		return spineInspection{}, Domainf("spine path %q is a directory", spine)
	}
	inspection := spineInspection{exists: true, bytes: st.Size()}
	if st.Size() == 0 {
		return inspection, nil
	}
	dsn := "file:" + filepath.ToSlash(spine) + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return spineInspection{}, Domainf("inspect spine %q read-only: %v", spine, err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return spineInspection{}, Domainf("inspect spine %q read-only: %v", spine, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&inspection.tables); err != nil {
		return spineInspection{}, Domainf("inspect spine %q tables: %v", spine, err)
	}
	if inspection.tables == 0 {
		return inspection, nil
	}
	var metaTables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'meta'`).Scan(&metaTables); err != nil {
		return spineInspection{}, Domainf("inspect spine %q meta table: %v", spine, err)
	}
	if metaTables == 0 {
		return inspection, nil
	}
	if err := db.QueryRow(`SELECT deployment_uuid, schema_version FROM meta WHERE id = 1`).Scan(&inspection.uuid, &inspection.version); err != nil {
		return spineInspection{}, Domainf("spine at %q has a meta table but no singleton identity: %v — restore from backup (§16.4)", spine, err)
	}
	inspection.hasMeta = true
	return inspection, nil
}

func readDeploymentMirror(home string) (string, bool, error) {
	path := filepath.Join(home, deploymentUUIDFilename)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, Domainf("read deployment identity mirror %q: %v", path, err)
	}
	uuid := strings.TrimSpace(string(b))
	if uuid == "" {
		return "", false, Domainf("deployment identity mirror %q is empty — restore from backup (§16.4)", path)
	}
	return uuid, true, nil
}

func writeDeploymentMirror(home, uuid string) error {
	path := filepath.Join(home, deploymentUUIDFilename)
	tmp, err := os.CreateTemp(home, ".deployment.uuid.tmp-*")
	if err != nil {
		return Domainf("create deployment identity mirror: %v", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(uuid + "\n"); err != nil {
		tmp.Close()
		return Domainf("write deployment identity mirror: %v", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return Domainf("sync deployment identity mirror: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return Domainf("close deployment identity mirror: %v", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return Domainf("publish deployment identity mirror: %v", err)
	}
	return nil
}

// migrateSpine is §16.4's "present with an older schema → migrate" arm. It
// runs only after validateInspection has vouched for the version, and
// substrate.Migrate is itself idempotent by version, so a current spine is a
// no-op rather than a special case here.
func migrateSpine(spine string) (bool, error) {
	db, err := substrate.Open(spine)
	if err != nil {
		return false, Usagef("%v", err)
	}
	defer db.Close()
	changed, err := substrate.Migrate(db)
	if err != nil {
		return false, Domainf("migrate spine at %q: %v — restore from backup (§16.4)", spine, err)
	}
	return changed, nil
}

func validateInspection(spine string, inspection spineInspection) error {
	if inspection.tables == 0 || !inspection.hasMeta {
		return Domainf("spine at %q has %d tables but no meta identity — no onboarding path may re-initialize it; restore from backup (§16.4)", spine, inspection.tables)
	}
	// §16.4: "present and current → skip; present with an older schema →
	// migrate". A version this build has never heard of is the loud-abort arm:
	// a spine written by a newer mc is refused, never downgraded or guessed at.
	if inspection.version > substrate.CurrentSchemaVersion {
		return Domainf("spine schema_version %d is newer than this build's %d — upgrade mc or restore from backup (§16.4)",
			inspection.version, substrate.CurrentSchemaVersion)
	}
	if inspection.version < 1 {
		return Domainf("spine schema_version %d has no defined migration; refusing (§16.4)", inspection.version)
	}
	if inspection.uuid == "" {
		return Domainf("spine at %q has an empty deployment identity — restore from backup (§16.4)", spine)
	}
	return nil
}

func requireOnboardIdentity(spine string) error {
	home, err := mcHomeDir()
	if err != nil {
		return err
	}
	mirrored, mirrorExists, err := readDeploymentMirror(home)
	if err != nil {
		return err
	}
	if !mirrorExists {
		return Domainf("deployment identity is not provisioned; run mc onboard home first (§16.4)")
	}
	inspection, err := inspectSpineReadOnly(spine)
	if err != nil {
		return err
	}
	if !inspection.exists || inspection.tables == 0 {
		return Domainf("spine lost — restore from backup: MC_HOME identity %s has no non-empty spine at %q (§16.4)", mirrored, spine)
	}
	if err := validateInspection(spine, inspection); err != nil {
		return err
	}
	if mirrored != inspection.uuid {
		return Domainf("deployment identity mismatch: MC_HOME has %s but spine has %s — restore the matching backup (§16.4)", mirrored, inspection.uuid)
	}
	return nil
}

func scaffoldOnboardHome(home string) (bool, error) {
	changed := false
	for _, dir := range []string{"", "backups", "sessions", "outputs", "attachments", "workflows"} {
		if st, err := os.Stat(filepath.Join(home, dir)); os.IsNotExist(err) {
			changed = true
		} else if err != nil {
			return false, Domainf("inspect MC_HOME scaffold: %v", err)
		} else if !st.IsDir() {
			return false, Domainf("MC_HOME scaffold path %q is not a directory", filepath.Join(home, dir))
		}
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			return false, Domainf("scaffold MC_HOME: %v", err)
		}
	}
	return changed, nil
}

// onboardPreflight writes nothing (§17: nothing is written until preflight
// passes): MC_HOME resolves outside every git working tree unless the
// concrete root is ignored there (§16.1), and git is present. The
// container-runtime reachability probe is Phase 3.
func onboardPreflight() (string, string, error) {
	home, err := mcHomeDir()
	if err != nil {
		return "", "", err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", "", Domainf("git is required and not on PATH (§17 preflight)")
	}
	resolved, err := resolveThroughExistingAncestor(home)
	if err != nil {
		return "", "", err
	}
	for dir := resolved; ; {
		_, gitErr := os.Stat(filepath.Join(dir, ".git"))
		if gitErr == nil {
			rel, relErr := filepath.Rel(dir, resolved)
			if relErr != nil || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return "", "", Domainf("MC_HOME %q is inside a git working tree (%q) and is not an ignored child path (§16.1)", home, dir)
			}
			ignored := exec.Command("git", "-C", dir, "check-ignore", "-q", "--", rel).Run() == nil
			if !ignored {
				// A directory-only pattern (for example `.mc/`) needs the
				// trailing slash while the not-yet-created root has no stat
				// information for git to infer that it will be a directory.
				ignored = exec.Command("git", "-C", dir, "check-ignore", "-q", "--", filepath.ToSlash(rel)+"/").Run() == nil
			}
			if !ignored {
				return "", "", Domainf("MC_HOME %q resolves inside git working tree %q but is not covered by its .gitignore (§16.1)", home, dir)
			}
		} else if !os.IsNotExist(gitErr) {
			return "", "", Domainf("inspect git working-tree marker under %q: %v", dir, gitErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "ok", fmt.Sprintf("%s/%s; MC_HOME %s (resolved %s); container-runtime probe deferred to Phase 3", runtime.GOOS, runtime.GOARCH, home, resolved), nil
}

// resolveThroughExistingAncestor resolves symlinks even when the MC_HOME
// leaf does not exist yet: resolve the nearest existing ancestor, then add
// the missing suffix back. This closes the lexical-outside/physical-inside
// repository-fence bypass.
func resolveThroughExistingAncestor(path string) (string, error) {
	cur := path
	suffix := []string{}
	for {
		if _, err := os.Lstat(cur); err == nil {
			resolved, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", Domainf("resolve MC_HOME %q: %v", path, err)
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", Domainf("inspect MC_HOME %q: %v", path, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", Domainf("resolve MC_HOME %q: no existing ancestor", path)
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

// onboardHome scaffolds the MC_HOME tree and provisions the spine under the
// §16.4 identity rules: meta present and current → skip; a spine with tables
// but no meta identity → abort loudly, restore from backup; empty or missing
// → initialize schema, triggers, and the deployment UUID.
func onboardHome(spine string) (string, string, error) {
	if _, _, err := onboardPreflight(); err != nil {
		return "", "", err
	}
	home, err := mcHomeDir()
	if err != nil {
		return "", "", err
	}
	if spine == "" {
		return "", "", Usagef("mc onboard home requires MC_SPINE")
	}
	mirrored, mirrorExists, err := readDeploymentMirror(home)
	if err != nil {
		return "", "", err
	}
	inspection, err := inspectSpineReadOnly(spine)
	if err != nil {
		return "", "", err
	}
	if inspection.exists && inspection.tables > 0 {
		if err := validateInspection(spine, inspection); err != nil {
			return "", "", err
		}
		if mirrorExists && mirrored != inspection.uuid {
			return "", "", Domainf("deployment identity mismatch: MC_HOME has %s but spine has %s — restore the matching backup (§16.4)", mirrored, inspection.uuid)
		}
		migrated, err := migrateSpine(spine)
		if err != nil {
			return "", "", err
		}
		changed, err := scaffoldOnboardHome(home)
		if err != nil {
			return "", "", err
		}
		changed = changed || migrated
		if !mirrorExists {
			if err := writeDeploymentMirror(home, inspection.uuid); err != nil {
				return "", "", err
			}
			changed = true
		}
		status := "ok"
		if changed {
			status = "done"
		}
		return status, fmt.Sprintf("deployment %s already provisioned (schema %d)", inspection.uuid, inspection.version), nil
	}
	if inspection.exists && inspection.bytes > 0 {
		return "", "", Domainf("spine at %q is non-empty (%d bytes) but has no meta identity — no onboarding path may re-initialize it; restore from backup (§16.4)", spine, inspection.bytes)
	}
	if mirrorExists {
		return "", "", Domainf("spine lost — restore from backup: MC_HOME identity %s has no non-empty spine at %q (§16.4)", mirrored, spine)
	}
	if _, err := scaffoldOnboardHome(home); err != nil {
		return "", "", err
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", Usagef("generate deployment uuid: %v", err)
	}
	uuid := hex.EncodeToString(raw[:])
	db, err := substrate.Open(spine)
	if err != nil {
		return awaitConcurrentProvision(home, spine, Usagef("%v", err))
	}
	err = inTx(db, func(ctx context.Context, q Q) error {
		if _, err := q.ExecContext(ctx, substrate.Schema); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx,
			`INSERT INTO meta (id, deployment_uuid, schema_version) VALUES (1, ?, ?)`,
			uuid, substrate.CurrentSchemaVersion)
		return err
	})
	if err != nil {
		db.Close()
		return recoverConcurrentProvision(home, spine, err)
	}
	if err := db.Close(); err != nil {
		return "", "", Domainf("close provisioned spine: %v", err)
	}
	if err := writeDeploymentMirror(home, uuid); err != nil {
		return "", "", err
	}
	return "done", fmt.Sprintf("deployment %s provisioned", uuid), nil
}

func recoverConcurrentProvision(home, spine string, provisionErr error) (string, string, error) {
	// BEGIN failures never prove that this caller owned an empty database;
	// another provisioner may still be live. Preserve every byte and let the
	// ordinary retry path inspect it later.
	if _, ok := provisionErr.(*UsageError); ok {
		return "", "", provisionErr
	}
	inspection, inspectErr := inspectSpineReadOnly(spine)
	if inspectErr != nil {
		return "", "", provisionErr
	}
	if inspection.tables > 0 {
		// The usual concurrent-loser path: another BEGIN IMMEDIATE committed
		// the canonical schema while this caller waited, so our CREATEs failed.
		// Adopt that identity; it is never ours to delete.
		status, detail, err := adoptConcurrentProvision(home, spine, inspection)
		if err != nil {
			return "", "", provisionErr
		}
		return status, detail, nil
	}
	// A failed transaction with no committed tables is the caller's fresh
	// attempt, not a deployment. Remove only those empty SQLite artifacts so
	// the exact same onboarding invocation is retryable. Any unidentified
	// non-empty/table-bearing state above is preserved and refused.
	removeFreshSpineFiles(spine)
	return "", "", provisionErr
}

func awaitConcurrentProvision(home, spine string, openErr error) (string, string, error) {
	if !strings.Contains(openErr.Error(), "database is locked") && !strings.Contains(openErr.Error(), "SQLITE_BUSY") {
		return "", "", openErr
	}
	for range 200 {
		inspection, err := inspectSpineReadOnly(spine)
		if err == nil && inspection.tables > 0 && inspection.hasMeta {
			return adoptConcurrentProvision(home, spine, inspection)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", "", openErr
}

func adoptConcurrentProvision(home, spine string, inspection spineInspection) (string, string, error) {
	if err := validateInspection(spine, inspection); err != nil {
		return "", "", err
	}
	mirrored, mirrorExists, err := readDeploymentMirror(home)
	if err != nil {
		return "", "", err
	}
	if mirrorExists && mirrored != inspection.uuid {
		return "", "", Domainf("deployment identity mismatch: MC_HOME has %s but concurrently provisioned spine has %s (§16.4)", mirrored, inspection.uuid)
	}
	changed, err := scaffoldOnboardHome(home)
	if err != nil {
		return "", "", err
	}
	if !mirrorExists {
		if err := writeDeploymentMirror(home, inspection.uuid); err != nil {
			return "", "", err
		}
		changed = true
	}
	status := "ok"
	if changed {
		status = "done"
	}
	return status, fmt.Sprintf("deployment %s concurrently provisioned and adopted (schema %d)", inspection.uuid, inspection.version), nil
}

func removeFreshSpineFiles(spine string) {
	for _, path := range []string{spine, spine + "-wal", spine + "-shm"} {
		_ = os.Remove(path)
	}
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
	if _, err := os.Lstat(path); err == nil {
		if err := validateRoutingPath(path, registry, allowFakeDecorrelation); err != nil {
			return "", "", err
		}
		return "ok", path, nil
	} else if !os.IsNotExist(err) {
		return "", "", Domainf("inspect routing.md at %q: %v", path, err)
	}
	if _, err := routing.Parse([]byte(defaultRoutingTable), registry, allowFakeDecorrelation); err != nil {
		return "", "", Domainf("default routing table failed validation: %v", err)
	}
	tmp, err := os.CreateTemp(home, ".routing.md.tmp-*")
	if err != nil {
		return "", "", Domainf("create routing.md temp file: %v", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(defaultRoutingTable); err != nil {
		tmp.Close()
		return "", "", Domainf("write routing.md temp file: %v", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", "", Domainf("sync routing.md temp file: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return "", "", Domainf("close routing.md temp file: %v", err)
	}
	// Hard-link publication is atomic and refuses to replace a concurrent
	// creator or a symlink; unlike a direct write, a crash cannot publish a
	// partial routing table.
	if err := os.Link(tmpPath, path); err != nil {
		if os.IsExist(err) {
			if err := validateRoutingPath(path, registry, allowFakeDecorrelation); err != nil {
				return "", "", err
			}
			return "ok", path, nil
		}
		return "", "", Domainf("publish routing.md: %v", err)
	}
	return "done", path, nil
}

func validateRoutingPath(path string, registry routing.Registry, allowFakeDecorrelation bool) error {
	before, err := os.Lstat(path)
	if err != nil {
		return Domainf("inspect routing.md at %q: %v", path, err)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return Domainf("routing.md at %q is a symlink; operator config must be a regular file under MC_HOME (§16.1)", path)
	}
	if !before.Mode().IsRegular() {
		return Domainf("routing.md at %q is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return Domainf("read routing.md at %q: %v", path, err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return Domainf("stat open routing.md at %q: %v", path, err)
	}
	if !os.SameFile(before, opened) {
		return Domainf("routing.md at %q changed during validation; retry (§17 routing)", path)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return Domainf("read routing.md at %q: %v", path, err)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, after) {
		return Domainf("routing.md at %q changed during validation; retry (§17 routing)", path)
	}
	if _, err := routing.Parse(data, registry, allowFakeDecorrelation); err != nil {
		return Domainf("invalid routing.md at %q: %v — repair it or remove it to accept the default (§17 routing)", path, err)
	}
	return nil
}

// onboardWorksource seeds the first Worksource from the dual-input answers
// (§17 section 6). An existing Worksource means the deployment is past this
// section; the full interactive richness (git contract, sandbox review,
// directive authoring) rides on `mc worksource add` and the agent shepherd.
func onboardWorksource(a OnboardArgs) (string, string, error) {
	if err := requireSpineFile(a.Spine); err != nil {
		return "", "", err
	}
	supplied := a.Worksource != "" || a.WorkspaceRoot != ""
	if supplied && (a.Worksource == "" || a.WorkspaceRoot == "") {
		return "", "", Usagef("mc onboard worksource requires --worksource and --workspace-root together")
	}
	if supplied && (!validStructuralText(a.Worksource, maxPrivateScalarBytes) ||
		!validStructuralText(a.WorkspaceRoot, maxPrivateScalarBytes)) {
		return "", "", Usagef("mc onboard Worksource and workspace root must be valid UTF-8 without controls and at most 4096 bytes (ADR-016 D2)")
	}
	workspaceRoot := ""
	if supplied {
		if !filepath.IsAbs(a.WorkspaceRoot) {
			return "", "", Usagef("mc onboard worksource --workspace-root must be absolute, got %q", a.WorkspaceRoot)
		}
		st, err := os.Stat(a.WorkspaceRoot)
		if err != nil {
			return "", "", Domainf("inspect workspace root %q: %v", a.WorkspaceRoot, err)
		}
		if !st.IsDir() {
			return "", "", Usagef("mc onboard worksource --workspace-root must name a directory, got %q", a.WorkspaceRoot)
		}
		workspaceRoot, err = filepath.EvalSymlinks(a.WorkspaceRoot)
		if err != nil {
			return "", "", Domainf("resolve workspace root %q: %v", a.WorkspaceRoot, err)
		}
		workspaceRoot = filepath.Clean(workspaceRoot)
	}
	db, err := substrate.Open(a.Spine)
	if err != nil {
		return "", "", Usagef("%v", err)
	}
	defer db.Close()
	status, detail := "", ""
	err = inTx(db, func(ctx context.Context, q Q) error {
		var existing int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM worksources`).Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			if supplied {
				var storedRoot string
				err := q.QueryRowContext(ctx, `
					SELECT p.workspace_root
					FROM worksources w JOIN sandbox_profiles p ON p.id = w.sandbox_profile
					WHERE w.id = ?`, a.Worksource).Scan(&storedRoot)
				if err == sql.ErrNoRows {
					return Domainf("%d Worksources are already registered; add %q through mc worksource add, not the first-Worksource onboarding section", existing, a.Worksource)
				}
				if err != nil {
					return err
				}
				if storedRoot != workspaceRoot {
					return Domainf("Worksource %s already points at %q, not requested workspace %q; refuse implicit rebinding (§17)", a.Worksource, storedRoot, workspaceRoot)
				}
			} else {
				rows, err := q.QueryContext(ctx, `
					SELECT w.id, p.workspace_root
					FROM worksources w LEFT JOIN sandbox_profiles p ON p.id = w.sandbox_profile
					ORDER BY w.id`)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var id string
					var storedRoot sql.NullString
					if err := rows.Scan(&id, &storedRoot); err != nil {
						return err
					}
					if !storedRoot.Valid {
						return Domainf("Worksource %s has no sandbox profile; repair it before onboarding can call the section healthy", id)
					}
					st, err := os.Stat(storedRoot.String)
					if err != nil || !st.IsDir() {
						return Domainf("Worksource %s workspace root %q is unavailable: %v", id, storedRoot.String, err)
					}
				}
				if err := rows.Err(); err != nil {
					return err
				}
			}
			status = "ok"
			detail = fmt.Sprintf("%d Worksources registered and reachable", existing)
			return nil
		}
		if !supplied {
			return Domainf("the first Worksource needs operator input: re-run with --worksource <id> --workspace-root </abs/path> (§17 dual-input)")
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO sandbox_profiles (id, workspace_root)
			VALUES ('default', ?)
			ON CONFLICT (id) DO NOTHING`, workspaceRoot); err != nil {
			return err
		}
		var storedRoot string
		if err := q.QueryRowContext(ctx, `
			SELECT workspace_root
			FROM sandbox_profiles WHERE id = 'default'`,
		).Scan(&storedRoot); err != nil {
			return err
		}
		if storedRoot != workspaceRoot {
			return Domainf("sandbox profile default already points at %q, not requested workspace %q; refuse implicit rebinding (§17)", storedRoot, workspaceRoot)
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO worksources (id, title, kind, sandbox_profile)
			VALUES (?, ?, 'repo', 'default')`, a.Worksource, a.Worksource); err != nil {
			return err
		}
		if err := substrate.ValidateDispatchMountProjection(ctx, q); err != nil {
			return err
		}
		status = "done"
		detail = fmt.Sprintf("Worksource %s at %s", a.Worksource, workspaceRoot)
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return status, detail, nil
}

func onboardTunables(a OnboardArgs) (string, string, error) {
	type requestedTunable struct {
		column string
		value  int
	}
	requested := []requestedTunable{
		{"timeout_minutes", a.TimeoutMinutes},
		{"grace_minutes", a.GraceMinutes},
		{"heartbeat_interval_s", a.HeartbeatIntervalS},
		{"spawn_grace_s", a.SpawnGraceS},
		{"hard_deadline_minutes", a.HardDeadlineMinutes},
	}
	hasRequested := false
	for _, item := range requested {
		if item.value < 0 {
			return "", "", Usagef("mc onboard tunables --%s must be non-negative", strings.ReplaceAll(item.column, "_", "-"))
		}
		hasRequested = hasRequested || item.value > 0
	}
	if err := requireSpineFile(a.Spine); err != nil {
		return "", "", err
	}
	db, err := substrate.Open(a.Spine)
	if err != nil {
		return "", "", Usagef("%v", err)
	}
	defer db.Close()
	status, detail := "", ""
	err = inTx(db, func(ctx context.Context, q Q) error {
		var timeout, grace, heartbeat, spawn, deadline int
		if err := q.QueryRowContext(ctx, `
			SELECT timeout_minutes, grace_minutes, heartbeat_interval_s,
			       spawn_grace_s, hard_deadline_minutes
			FROM lock WHERE id = 1`,
		).Scan(&timeout, &grace, &heartbeat, &spawn, &deadline); err != nil {
			return err
		}
		current := map[string]int{
			"timeout_minutes": timeout, "grace_minutes": grace,
			"heartbeat_interval_s": heartbeat, "spawn_grace_s": spawn,
			"hard_deadline_minutes": deadline,
		}
		if !hasRequested {
			status = "ok"
			label := "existing"
			if timeout == 60 && grace == 15 && heartbeat == 60 && spawn == 60 && deadline == 240 {
				label = "schema defaults accepted (recommended)"
			}
			detail = fmt.Sprintf("%s: timeout_minutes=%d, grace_minutes=%d, heartbeat_interval_s=%d, spawn_grace_s=%d, hard_deadline_minutes=%d", label, timeout, grace, heartbeat, spawn, deadline)
			return nil
		}
		changed := make([]requestedTunable, 0, len(requested))
		for _, item := range requested {
			if item.value > 0 && current[item.column] != item.value {
				changed = append(changed, item)
			}
		}
		if len(changed) == 0 {
			status = "ok"
			detail = "requested §16.3 tunables already configured"
			return nil
		}
		names := make([]string, 0, len(changed))
		for _, item := range changed {
			if _, err := q.ExecContext(ctx,
				fmt.Sprintf(`UPDATE lock SET %s = ? WHERE id = 1`, item.column), item.value); err != nil {
				return err
			}
			names = append(names, item.column)
		}
		status = "done"
		detail = "applied: " + strings.Join(names, ", ")
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return status, detail, nil
}

// onboardSurfaces applies the Daily Console schedule when answered; the
// dashboard is core with loopback defaults, and Discord is skippable
// entirely (§17 section 8) — both land with their Phase 3/5 processes.
func onboardSurfaces(a OnboardArgs) (string, string, error) {
	if a.ConsoleScheduleSet {
		if a.ConsoleHour < 0 || a.ConsoleHour > 23 {
			return "", "", Usagef("mc onboard surfaces --console-hour must be 0..23")
		}
		if a.ConsoleMinute < 0 || a.ConsoleMinute > 59 {
			return "", "", Usagef("mc onboard surfaces --console-minute must be 0..59")
		}
		if _, err := time.LoadLocation(a.ConsoleTZ); a.ConsoleTZ == "" || err != nil {
			return "", "", Usagef("mc onboard surfaces --console-tz %q is not a loadable IANA timezone", a.ConsoleTZ)
		}
	}
	if err := requireSpineFile(a.Spine); err != nil {
		return "", "", err
	}
	db, err := substrate.Open(a.Spine)
	if err != nil {
		return "", "", Usagef("%v", err)
	}
	defer db.Close()
	status, detail := "", ""
	err = inTx(db, func(ctx context.Context, q Q) error {
		var currentHour, currentMinute int
		var currentTZ string
		if err := q.QueryRowContext(ctx, `SELECT console_hour, console_minute, console_tz FROM lock WHERE id = 1`).Scan(&currentHour, &currentMinute, &currentTZ); err != nil {
			return err
		}
		if !a.ConsoleScheduleSet {
			if currentHour == 24 {
				return Domainf("Daily Console schedule needs operator input: re-run with --console-hour <0..23> --console-minute <0..59> --console-tz <IANA> (§17 dual-input)")
			}
			status = "ok"
			detail = fmt.Sprintf("Daily Console already configured at %02d:%02d %s; dashboard defaults accepted; Discord skipped", currentHour, currentMinute, currentTZ)
			return nil
		}
		if currentHour == a.ConsoleHour && currentMinute == a.ConsoleMinute && currentTZ == a.ConsoleTZ {
			status = "ok"
			detail = fmt.Sprintf("Daily Console already configured at %02d:%02d %s", currentHour, currentMinute, currentTZ)
			return nil
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE lock SET console_hour = ?, console_minute = ?, console_tz = ?
			WHERE id = 1`, a.ConsoleHour, a.ConsoleMinute, a.ConsoleTZ); err != nil {
			return err
		}
		status = "done"
		detail = fmt.Sprintf("Daily Console at %02d:%02d %s", a.ConsoleHour, a.ConsoleMinute, a.ConsoleTZ)
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return status, detail, nil
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
