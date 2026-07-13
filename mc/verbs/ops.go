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

type doctorFinding struct {
	Check          string `json:"check"`
	Status         string `json:"status"` // ok | fail | deferred
	Detail         string `json:"detail"`
	OnboardSection string `json:"onboard_section"` // §17 repairing section
}

// Doctor validates what Phase 2 can validate — MC_HOME shape, spine
// identity/schema, routing, worksource/sandbox references — and reports the
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
				case version != 1:
					add("spine", "fail", fmt.Sprintf("schema version %d, expected 1", version), "home")
				default:
					add("spine", "ok", fmt.Sprintf("deployment %s schema %d", uuid, version), "home")
					spineOK = true
				}
			}()
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

	if err != nil {
		add("routing", "fail", "not checked: MC_HOME unresolved", "routing")
	} else {
		path := filepath.Join(home, "routing.md")
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			add("routing", "fail", fmt.Sprintf("read routing.md at %q: %v", path, readErr), "routing")
		} else {
			registry, allowFakeDecorrelation := routing.ActiveRegistry()
			if _, parseErr := routing.Parse(data, registry, allowFakeDecorrelation); parseErr != nil {
				add("routing", "fail", fmt.Sprintf("invalid routing.md at %q: %v", path, parseErr), "routing")
			} else {
				add("routing", "ok", path, "routing")
			}
		}
	}

	add("container-runtime", "deferred", "capability probe (setuid gate, helper exec) runs in Phase 3 (§16.4, §17)", "container")
	add("gateway", "deferred", "egress gateway health probe runs in Phase 3 (§11.4, §17)", "container")
	add("runtime-auth", "deferred", "per-binding runtime-auth health runs in Phase 3/5 (§11.4, §17)", "runtime-auth")
	add("supervision", "deferred", "launchd/resident supervision check runs in Phase 5 (§17)", "supervision")

	ok := true
	for _, f := range findings {
		if f.Status == "fail" {
			ok = false
		}
	}
	return map[string]any{"ok": ok, "findings": findings}, nil
}
