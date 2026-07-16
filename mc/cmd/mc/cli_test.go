// CLI tests for the Phase 1b skeleton verb surface (docs/phase1b-contract.md
// §2), driven through the real built binary against a temp spine — exit
// codes, stdout JSON, and spine state are all asserted at the os/exec
// boundary. Docker-free by construction (contract §1: the e2e alone is
// behind the docker_e2e tag).
package main_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mc/substrate"
)

var mcBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mc-cli-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mcBin = filepath.Join(dir, "mc")
	// The fast CLI tier explicitly registers the deterministic fake routing
	// family; production builds omit this build tag and cannot resolve it.
	build := exec.Command("go", "build", "-tags", "test_fake_routing", "-o", mcBin, ".")
	out, err := build.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build mc binary: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// mcResult is one CLI invocation's observable surface.
type mcResult struct {
	code   int
	stdout string
	stderr string
	json   map[string]any
}

// runMC invokes the built binary. env entries are KEY=VALUE overrides; the
// ambient MC_* variables are stripped so tests are hermetic.
func runMC(t *testing.T, env []string, stdin string, args ...string) mcResult {
	t.Helper()
	cmd := exec.Command(mcBin, args...)
	base := []string{}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "MC_SPINE=") || strings.HasPrefix(e, "MC_HELPER=") ||
			strings.HasPrefix(e, "MC_RUN_JSON=") || strings.HasPrefix(e, "MC_HOME=") {
			continue
		}
		base = append(base, e)
	}
	cmd.Env = append(base, env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	res := mcResult{stdout: outBuf.String(), stderr: errBuf.String()}
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run mc %v: %v", args, err)
		}
		res.code = exit.ExitCode()
	}
	if strings.TrimSpace(res.stdout) != "" {
		if err := json.Unmarshal([]byte(res.stdout), &res.json); err != nil {
			t.Fatalf("mc %v: stdout is not a single JSON object: %q (%v)", args, res.stdout, err)
		}
	}
	return res
}

func TestStructuredErrorJSON(t *testing.T) {
	spine := initSpine(t)

	domainRes := runMC(t, spineEnv(spine), "", "packet", "decide", "999", "--approve")
	if domainRes.code != 1 {
		t.Fatalf("domain exit = %d stderr=%q", domainRes.code, domainRes.stderr)
	}
	domainErr, ok := domainRes.json["error"].(map[string]any)
	if !ok || domainErr["code"] != "not-found" || !strings.Contains(domainErr["message"].(string), "no task") {
		t.Fatalf("domain error JSON = %v", domainRes.json)
	}

	usageRes := runMC(t, spineEnv(spine), "", "task", "add")
	if usageRes.code != 2 {
		t.Fatalf("usage exit = %d stderr=%q", usageRes.code, usageRes.stderr)
	}
	usageErr, ok := usageRes.json["error"].(map[string]any)
	if !ok || usageErr["code"] != "usage" {
		t.Fatalf("usage error JSON = %v", usageRes.json)
	}

	// CLI-plane domain refusals without a narrower aggregate code still have
	// one stable public slug rather than an empty/missing field.
	eff := dispatchExpect(t, spine, "spawn")
	run := eff["run_id"].(string)
	scopeRes := runMC(t, runJSONEnv(t, spine, run, "pipeline", "editor"), "", "dispatch")
	if scopeRes.code != 1 {
		t.Fatalf("scope exit = %d stderr=%q", scopeRes.code, scopeRes.stderr)
	}
	scopeErr, ok := scopeRes.json["error"].(map[string]any)
	if !ok || scopeErr["code"] != "domain-rejection" {
		t.Fatalf("scope error JSON = %v", scopeRes.json)
	}
	if !strings.Contains(scopeRes.stderr, scopeErr["message"].(string)) {
		t.Fatalf("stderr %q does not preserve JSON diagnostic %v", scopeRes.stderr, scopeErr)
	}

	// Zero-args and wrapper-only delegation failures are local rejections
	// too: one stdout JSON envelope each, not bare stderr.
	bare := runMC(t, spineEnv(spine), "")
	if bare.code != 2 {
		t.Fatalf("zero-args exit = %d stderr=%q", bare.code, bare.stderr)
	}
	bareErr, ok := bare.json["error"].(map[string]any)
	if !ok || bareErr["code"] != "usage" {
		t.Fatalf("zero-args error JSON = %v", bare.json)
	}

	broken := runMC(t, []string{"MC_HELPER=phantom", "PATH=/nonexistent"}, "", "task", "list")
	if broken.code != 2 {
		t.Fatalf("delegation-failure exit = %d stderr=%q", broken.code, broken.stderr)
	}
	brokenErr, ok := broken.json["error"].(map[string]any)
	if !ok || brokenErr["code"] != "usage" {
		t.Fatalf("delegation-failure error JSON = %v stdout=%q", broken.json, broken.stdout)
	}
}

const fakeRoutingMarkdown = `# Mission Control routing

| role | harness | binding |
| --- | --- | --- |
| strategist | fake | fake |
| editor | fake | fake |
| worker | fake | fake |
| verifier | fake | fake |
| packager | fake | fake |
| refiner | fake | fake |
| homie | fake | fake |
`

const defaultRoutingMarkdown = `# Mission Control routing

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

var defaultHomieAllowlist = []string{
	"homie.end", "homie.history", "homie.list", "initiative.add",
	"packet.decide", "task.add", "task.block", "task.interrupt", "task.unblock",
	"worksource.add", "worksource.archive", "worksource.pause",
}

func spineEnv(spine string) []string {
	return []string{
		"MC_SPINE=" + spine,
		"MC_HOME=" + filepath.Dir(spine),
	}
}

// initSpine provisions a fresh temp spine and returns its path.
func initSpine(t *testing.T, extra ...string) string {
	t.Helper()
	spine := filepath.Join(t.TempDir(), "spine.db")
	args := append([]string{
		"init", "--spine", spine,
		"--worksource", "ws-test",
		"--workspace-root", "/tmp/ws-test",
	}, extra...)
	res := runMC(t, nil, "", args...)
	if res.code != 0 {
		t.Fatalf("mc init failed (%d): %s", res.code, res.stderr)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(spine), "routing.md"), []byte(fakeRoutingMarkdown), 0o600); err != nil {
		t.Fatalf("write test routing.md: %v", err)
	}
	// The ADR-016 D1 deployment identity mirror: dispatch refuses to prepare
	// without it matching meta.deployment_uuid.
	uuid, _ := res.json["deployment_uuid"].(string)
	if uuid == "" {
		t.Fatalf("mc init effect carries no deployment_uuid: %v", res.json)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(spine), "deployment.uuid"), []byte(uuid+"\n"), 0o600); err != nil {
		t.Fatalf("write deployment mirror: %v", err)
	}
	return spine
}

// openDB opens the spine directly for state assertions and test fixture
// surgery (the test is the operator here; the volume-forced faithfulness of
// contract §2 applies to the Docker e2e, not this tier).
func openDB(t *testing.T, spine string) *sql.DB {
	t.Helper()
	db, err := substrate.Open(spine)
	if err != nil {
		t.Fatalf("open spine: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func queryStr(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	var s sql.NullString
	if err := db.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	if !s.Valid {
		return "<NULL>"
	}
	return s.String
}

func queryInt(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return n
}

// runJSONEnv materializes a run.json identity file and returns the env pair
// selecting it (MC_RUN_JSON is the CLI test tier's stand-in for the fixed
// /mc/run.json mount).
func runJSONEnv(t *testing.T, spine, runID, tier, role string) []string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "run.json")
	b, _ := json.Marshal(map[string]any{"run_id": runID, "tier": tier, "role": role})
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return append(spineEnv(spine), "MC_RUN_JSON="+p)
}

func homieJSONEnv(t *testing.T, spine, sessionID string, allowlist []string) []string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "run.json")
	b, _ := json.Marshal(map[string]any{
		"run_id": sessionID, "tier": "homie", "role": "homie",
		"verb_allowlist": allowlist,
	})
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return append(spineEnv(spine), "MC_RUN_JSON="+p)
}

// taskAdd files one origin:user task and returns its id.
func taskAdd(t *testing.T, spine, title string) int64 {
	t.Helper()
	res := runMC(t, spineEnv(spine), "", "task", "add", title, "--worksource", "ws-test")
	if res.code != 0 {
		t.Fatalf("task add failed (%d): %s", res.code, res.stderr)
	}
	return int64(res.json["task_id"].(float64))
}

// dispatch invokes mc dispatch and returns the effect JSON.
func dispatch(t *testing.T, spine string) map[string]any {
	t.Helper()
	res := runMC(t, spineEnv(spine), "", "dispatch")
	if res.code != 0 {
		t.Fatalf("dispatch failed (%d): %s", res.code, res.stderr)
	}
	return res.json
}

// dispatchExpect asserts the effect's action and returns the effect.
func dispatchExpect(t *testing.T, spine, action string) map[string]any {
	t.Helper()
	eff := dispatch(t, spine)
	if eff["action"] != action {
		t.Fatalf("dispatch action = %v, want %q (effect %v)", eff["action"], action, eff)
	}
	return eff
}

// ---------------------------------------------------------------------------
// mc init
// ---------------------------------------------------------------------------

func TestInit(t *testing.T) {
	t.Run("provisions_spine_and_tunables", func(t *testing.T) {
		spine := initSpine(t,
			"--timeout-minutes", "5", "--grace-minutes", "1",
			"--heartbeat-interval-s", "1", "--spawn-grace-s", "5",
			"--hard-deadline-minutes", "120",
			"--console-hour", "9", "--console-minute", "30",
			"--console-tz", "America/Los_Angeles")
		db := openDB(t, spine)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM meta`); n != 1 {
			t.Fatalf("meta rows = %d, want 1", n)
		}
		if ws := queryStr(t, db, `SELECT id FROM worksources`); ws != "ws-test" {
			t.Fatalf("worksource = %q", ws)
		}
		if p := queryStr(t, db, `SELECT egress_policy FROM sandbox_profiles`); p != "none" {
			t.Fatalf("egress_policy = %q, want none (fake family, contract §1)", p)
		}
		for col, want := range map[string]int{
			"timeout_minutes": 5, "grace_minutes": 1,
			"heartbeat_interval_s": 1, "spawn_grace_s": 5,
			"hard_deadline_minutes": 120,
		} {
			if got := queryInt(t, db, `SELECT `+col+` FROM lock WHERE id = 1`); got != want {
				t.Fatalf("lock.%s = %d, want %d", col, got, want)
			}
		}
		if got := queryStr(t, db, `SELECT console_hour || ':' || console_minute || '/' || console_tz FROM lock WHERE id = 1`); got != "9:30/America/Los_Angeles" {
			t.Fatalf("console schedule = %q", got)
		}
	})

	t.Run("console_schedule_validates_before_provisioning", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			flags []string
		}{
			{"partial", []string{"--console-hour", "9"}},
			{"hour", []string{"--console-hour", "24", "--console-minute", "0", "--console-tz", "UTC"}},
			{"minute", []string{"--console-hour", "9", "--console-minute", "60", "--console-tz", "UTC"}},
			{"timezone", []string{"--console-hour", "9", "--console-minute", "0", "--console-tz", "Mars/Olympus"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				spine := filepath.Join(t.TempDir(), "spine.db")
				args := []string{"init", "--spine", spine, "--worksource", "ws-test", "--workspace-root", "/tmp/ws-test"}
				args = append(args, tc.flags...)
				res := runMC(t, nil, "", args...)
				if res.code != 2 {
					t.Fatalf("exit=%d stderr=%q, want usage error before provisioning", res.code, res.stderr)
				}
				if _, err := os.Stat(spine); !os.IsNotExist(err) {
					t.Fatalf("invalid schedule created spine: stat err=%v", err)
				}
			})
		}
	})

	t.Run("midnight_is_an_explicit_schedule_not_the_unset_sentinel", func(t *testing.T) {
		spine := initSpine(t, "--console-hour", "0", "--console-minute", "0", "--console-tz", "UTC")
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT console_hour || ':' || console_minute || '/' || console_tz FROM lock WHERE id = 1`); got != "0:0/UTC" {
			t.Fatalf("midnight schedule = %q", got)
		}
	})

	t.Run("reinit_fails_loudly", func(t *testing.T) {
		spine := initSpine(t)
		res := runMC(t, nil, "", "init", "--spine", spine,
			"--worksource", "ws-test", "--workspace-root", "/tmp/x")
		if res.code != 1 {
			t.Fatalf("re-init exit = %d, want 1 (provisioning happens exactly once, §16.4)", res.code)
		}
	})

	t.Run("missing_flags_are_usage_errors", func(t *testing.T) {
		res := runMC(t, nil, "", "init", "--spine", filepath.Join(t.TempDir(), "s.db"))
		if res.code != 2 {
			t.Fatalf("exit = %d, want 2", res.code)
		}
	})
}

// ---------------------------------------------------------------------------
// mc task add / get, mc packet list
// ---------------------------------------------------------------------------

func TestTaskAddAndGet(t *testing.T) {
	spine := initSpine(t)

	t.Run("files_origin_user_proposed", func(t *testing.T) {
		id := taskAdd(t, spine, "first user task")
		res := runMC(t, spineEnv(spine), "", "task", "get", fmt.Sprint(id))
		if res.code != 0 {
			t.Fatalf("task get failed: %s", res.stderr)
		}
		for k, want := range map[string]any{
			"status": "proposed", "origin": "user", "target_ref": "main",
			"priority": float64(2), "worksource": "ws-test",
		} {
			if got := res.json[k]; got != want {
				t.Fatalf("task get %s = %v, want %v", k, got, want)
			}
		}
	})

	t.Run("priority_flag_recorded", func(t *testing.T) {
		res := runMC(t, spineEnv(spine), "", "task", "add", "urgent",
			"--worksource", "ws-test", "--priority", "-1")
		if res.code != 0 {
			t.Fatalf("task add failed: %s", res.stderr)
		}
		id := int64(res.json["task_id"].(float64))
		got := runMC(t, spineEnv(spine), "", "task", "get", fmt.Sprint(id))
		if got.json["priority"] != float64(-1) {
			t.Fatalf("priority = %v, want -1", got.json["priority"])
		}
	})

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"unknown_worksource_is_domain", []string{"task", "add", "x", "--worksource", "nope"}, 1},
		{"missing_worksource_is_usage", []string{"task", "add", "x"}, 2},
		{"missing_title_is_usage", []string{"task", "add", "--worksource", "ws-test"}, 2},
		{"get_missing_task_is_domain", []string{"task", "get", "999"}, 1},
		{"get_bad_id_is_usage", []string{"task", "get", "abc"}, 2},
		{"unknown_verb_is_usage", []string{"frobnicate"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runMC(t, spineEnv(spine), "", tc.args...)
			if res.code != tc.want {
				t.Fatalf("mc %v exit = %d, want %d (stderr %q)", tc.args, res.code, tc.want, res.stderr)
			}
		})
	}
}

func TestInitiativeAdd(t *testing.T) {
	t.Run("host_files_user_initiative_with_charter", func(t *testing.T) {
		spine := initSpine(t)
		res := runMC(t, spineEnv(spine), "", "initiative", "add", "raise system leverage",
			"--worksource", "ws-test", "--charter", "criterion: acceptance doubles",
			"--priority", "-1")
		if res.code != 0 {
			t.Fatalf("initiative add failed: %s", res.stderr)
		}
		id := int64(res.json["initiative_id"].(float64))
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT scope || '/' || origin || '/' || priority || '/' || description FROM tasks WHERE id=?`, id); got != "initiative/user/-1/criterion: acceptance doubles" {
			t.Fatalf("initiative row = %q", got)
		}
	})

	t.Run("charter_required_and_pipeline_denied", func(t *testing.T) {
		spine := initSpine(t)
		missing := runMC(t, spineEnv(spine), "", "initiative", "add", "missing charter",
			"--worksource", "ws-test")
		if missing.code != 2 || !strings.Contains(missing.stderr, "--charter") {
			t.Fatalf("missing charter exit=%d stderr=%q", missing.code, missing.stderr)
		}
		taskAdd(t, spine, "claim Editor")
		eff := dispatchExpect(t, spine, "spawn")
		run := eff["run_id"].(string)
		denied := runMC(t, runJSONEnv(t, spine, run, "pipeline", "editor"), "",
			"initiative", "add", "forged", "--worksource", "ws-test", "--charter", "fake")
		if denied.code != 1 || !strings.Contains(denied.stderr, "operator verb") {
			t.Fatalf("pipeline initiative add exit=%d stderr=%q", denied.code, denied.stderr)
		}
		db := openDB(t, spine)
		if got := queryInt(t, db, `SELECT COUNT(*) FROM tasks WHERE scope='initiative'`); got != 0 {
			t.Fatalf("pipeline forged %d initiatives", got)
		}
	})
}

func TestWorksourceLifecycle(t *testing.T) {
	spine := initSpine(t)
	add := runMC(t, spineEnv(spine), "", "worksource", "add", "ws-two",
		"--title", "Second Worksource", "--kind", "repo",
		"--sandbox-profile", "default", "--directive", "raise leverage")
	if add.code != 0 {
		t.Fatalf("worksource add failed: %s", add.stderr)
	}
	bad := runMC(t, spineEnv(spine), "", "worksource", "add", "bad-profile",
		"--title", "Bad", "--kind", "repo", "--sandbox-profile", "missing")
	if bad.code != 1 {
		t.Fatalf("missing profile exit=%d stderr=%q", bad.code, bad.stderr)
	}

	pausedTask := runMC(t, spineEnv(spine), "", "task", "add", "paused task",
		"--worksource", "ws-two", "--priority", "-1")
	if pausedTask.code != 0 {
		t.Fatalf("paused fixture add: %s", pausedTask.stderr)
	}
	activeID := taskAdd(t, spine, "active task")
	pause := runMC(t, spineEnv(spine), "", "worksource", "pause", "ws-two")
	if pause.code != 0 {
		t.Fatalf("pause failed: %s", pause.stderr)
	}
	eff := dispatchExpect(t, spine, "spawn")
	pool := eff["pool_ids"].([]any)
	if len(pool) != 1 || int64(pool[0].(float64)) != activeID {
		t.Fatalf("paused Worksource leaked into Editor pool: %v", pool)
	}

	run := eff["run_id"].(string)
	pipelineEnv := runJSONEnv(t, spine, run, "pipeline", "editor")
	listed := runMC(t, pipelineEnv, "", "worksource", "list")
	if listed.code != 0 || len(listed.json["worksources"].([]any)) != 2 {
		t.Fatalf("pipeline list code=%d json=%v stderr=%q", listed.code, listed.json, listed.stderr)
	}
	for _, args := range [][]string{
		{"worksource", "pause", "ws-test"},
		{"worksource", "archive", "ws-test"},
		{"worksource", "add", "forged", "--title", "Forged", "--kind", "repo"},
	} {
		denied := runMC(t, pipelineEnv, "", args...)
		if denied.code != 1 || !strings.Contains(denied.stderr, "operator verb") {
			t.Fatalf("pipeline %v exit=%d stderr=%q", args, denied.code, denied.stderr)
		}
	}

	archive := runMC(t, spineEnv(spine), "", "worksource", "archive", "ws-two")
	if archive.code != 0 {
		t.Fatalf("archive failed: %s", archive.stderr)
	}
	db := openDB(t, spine)
	if got := queryStr(t, db, `SELECT status FROM worksources WHERE id='ws-two'`); got != "archived" {
		t.Fatalf("worksource status = %q", got)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM worksources WHERE id='bad-profile'`); got != 0 {
		t.Fatalf("invalid profile left %d worksource rows", got)
	}
}

func TestScopeRefusalPrecedesSpineOpen(t *testing.T) {
	// Wave-2 contract §1: every refusal precedes spine mutation. A pipeline
	// caller pointed at a fresh MC_SPINE path must be refused before the
	// database file (or -wal/-shm) is created.
	for _, args := range [][]string{
		{"land", "report", "1", "--status", "success"},
		{"outbox", "poll", "--surface", "dashboard"},
		{"outbox", "ack", "1", "--surface", "dashboard"},
	} {
		probe := filepath.Join(t.TempDir(), "probe.db")
		res := runMC(t, runJSONEnv(t, probe, "r-probe", "pipeline", "editor"), "", args...)
		if res.code != 1 {
			t.Fatalf("%v pipeline exit = %d stderr=%q", args, res.code, res.stderr)
		}
		if _, err := os.Stat(probe); !os.IsNotExist(err) {
			t.Fatalf("%v created spine bytes before the scope refusal (stat err=%v)", args, err)
		}
	}
}

func TestInactiveWorksourceRefusesNewWork(t *testing.T) {
	spine := initSpine(t)
	add := runMC(t, spineEnv(spine), "", "worksource", "add", "ws-cold",
		"--title", "Cold", "--kind", "repo")
	if add.code != 0 {
		t.Fatalf("worksource add failed: %s", add.stderr)
	}
	db := openDB(t, spine)

	for _, status := range []string{"pause", "archive"} {
		if res := runMC(t, spineEnv(spine), "", "worksource", status, "ws-cold"); res.code != 0 {
			t.Fatalf("worksource %s failed: %s", status, res.stderr)
		}
		before := queryInt(t, db, `SELECT COUNT(*) FROM tasks`)
		for _, args := range [][]string{
			{"task", "add", "swallowed", "--worksource", "ws-cold"},
			{"initiative", "add", "swallowed arc", "--worksource", "ws-cold",
				"--charter", "criterion: never visible"},
		} {
			res := runMC(t, spineEnv(spine), "", args...)
			if res.code != 1 {
				t.Fatalf("after %s, %v exit = %d stderr=%q", status, args, res.code, res.stderr)
			}
			errObj, ok := res.json["error"].(map[string]any)
			if !ok || errObj["code"] != "worksource-inactive" {
				t.Fatalf("after %s, %v error JSON = %v", status, args, res.json)
			}
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM tasks`); got != before {
			t.Fatalf("after %s, inactive Worksource swallowed %d rows", status, got-before)
		}
	}
}

func TestHomieWorksourcePauseArchive(t *testing.T) {
	spine := initSpine(t)
	add := runMC(t, spineEnv(spine), "", "worksource", "add", "ws-homie",
		"--title", "Homie-managed", "--kind", "repo")
	if add.code != 0 {
		t.Fatalf("worksource add failed: %s", add.stderr)
	}
	started := runMC(t, spineEnv(spine), "", "homie", "start", "--from", "dashboard:ops")
	if started.code != 0 {
		t.Fatalf("homie start failed: %s", started.stderr)
	}
	session := started.json["session_id"].(string)
	env := homieJSONEnv(t, spine, session, defaultHomieAllowlist)

	pause := runMC(t, env, "", "worksource", "pause", "ws-homie")
	if pause.code != 0 {
		t.Fatalf("allowlisted Homie worksource pause failed (%d): %s", pause.code, pause.stderr)
	}
	db := openDB(t, spine)
	if got := queryStr(t, db, `SELECT status FROM worksources WHERE id='ws-homie'`); got != "paused" {
		t.Fatalf("after Homie pause status = %q", got)
	}
	archive := runMC(t, env, "", "worksource", "archive", "ws-homie")
	if archive.code != 0 {
		t.Fatalf("allowlisted Homie worksource archive failed (%d): %s", archive.code, archive.stderr)
	}
	if got := queryStr(t, db, `SELECT status FROM worksources WHERE id='ws-homie'`); got != "archived" {
		t.Fatalf("after Homie archive status = %q", got)
	}

	narrow := runMC(t, spineEnv(spine), "", "homie", "start", "--from", "cli:narrow", "--allow", "task.add")
	if narrow.code != 0 {
		t.Fatalf("narrow homie start failed: %s", narrow.stderr)
	}
	narrowEnv := homieJSONEnv(t, spine, narrow.json["session_id"].(string), []string{"task.add"})
	denied := runMC(t, narrowEnv, "", "worksource", "pause", "ws-test")
	if denied.code != 1 || !strings.Contains(denied.stderr, "not allowed") {
		t.Fatalf("narrow Homie pause = code %d stderr %q", denied.code, denied.stderr)
	}
	if got := queryStr(t, db, `SELECT status FROM worksources WHERE id='ws-test'`); got != "active" {
		t.Fatalf("denied pause mutated ws-test to %q", got)
	}
}

func TestPacketListEmpty(t *testing.T) {
	spine := initSpine(t)
	res := runMC(t, spineEnv(spine), "", "packet", "list")
	if res.code != 0 {
		t.Fatalf("packet list failed: %s", res.stderr)
	}
	if got := res.json["packets"].([]any); len(got) != 0 {
		t.Fatalf("packets = %v, want empty", got)
	}
}

func TestTaskBlockUnblock(t *testing.T) {
	t.Run("host_blocks_and_unblocks_without_moving_status", func(t *testing.T) {
		spine := initSpine(t)
		id := taskAdd(t, spine, "needs a decision")
		res := runMC(t, spineEnv(spine), "", "task", "block", fmt.Sprint(id),
			"--reason", "choose a target")
		if res.code != 0 {
			t.Fatalf("block failed: %s", res.stderr)
		}
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT status || '/' || blocked || '/' || blocked_reason FROM tasks WHERE id = ?`, id); got != "proposed/1/choose a target" {
			t.Fatalf("blocked row = %q", got)
		}
		res = runMC(t, spineEnv(spine), "", "task", "unblock", fmt.Sprint(id))
		if res.code != 0 {
			t.Fatalf("unblock failed: %s", res.stderr)
		}
		if got := queryStr(t, db, `SELECT status || '/' || blocked || '/' || COALESCE(blocked_reason, 'none') FROM tasks WHERE id = ?`, id); got != "proposed/0/none" {
			t.Fatalf("unblocked row = %q", got)
		}
	})

	t.Run("pipeline_may_block_only_its_live_subject_and_never_unblock", func(t *testing.T) {
		spine, id, run, env := workerFixture(t, "pipeline block")
		other := taskAdd(t, spine, "not this run's subject")
		res := runMC(t, env, "", "task", "block", fmt.Sprint(other), "--reason", "no")
		if res.code != 1 || !strings.Contains(res.stderr, "own subject") {
			t.Fatalf("cross-subject block exit=%d stderr=%q", res.code, res.stderr)
		}
		res = runMC(t, env, "", "task", "block", fmt.Sprint(id), "--reason", "operator choice")
		if res.code != 0 {
			t.Fatalf("own-subject block failed: %s", res.stderr)
		}
		res = runMC(t, env, "", "task", "unblock", fmt.Sprint(id))
		if res.code != 1 || !strings.Contains(res.stderr, "operator verb") {
			t.Fatalf("pipeline unblock exit=%d stderr=%q", res.code, res.stderr)
		}
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != run {
			t.Fatalf("nonterminal block disturbed lease: %q", got)
		}
		if got := queryInt(t, db, `SELECT blocked FROM tasks WHERE id = ?`, other); got != 0 {
			t.Fatalf("cross-subject refusal blocked other task")
		}
	})

	t.Run("validation", func(t *testing.T) {
		spine := initSpine(t)
		id := taskAdd(t, spine, "validation")
		for _, tc := range []struct {
			args []string
			want int
		}{
			{[]string{"task", "block", fmt.Sprint(id)}, 2},
			{[]string{"task", "unblock", fmt.Sprint(id), "extra"}, 2},
			{[]string{"task", "block", "bad", "--reason", "x"}, 2},
		} {
			if res := runMC(t, spineEnv(spine), "", tc.args...); res.code != tc.want {
				t.Fatalf("mc %v exit=%d stderr=%q, want %d", tc.args, res.code, res.stderr, tc.want)
			}
		}
	})
}

func TestTaskInterrupt(t *testing.T) {
	t.Run("host_cancels_live_subject_and_returns_exact_stop", func(t *testing.T) {
		spine, taskID, run, _ := workerFixture(t, "interrupt live run")
		res := runMC(t, spineEnv(spine), "", "task", "interrupt", fmt.Sprint(taskID))
		if res.code != 0 || res.json["action"] != "interrupt" ||
			res.json["run_id"] != run || res.json["stop_container"] != true {
			t.Fatalf("interrupt code=%d json=%v stderr=%q", res.code, res.json, res.stderr)
		}
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT decision || '/' || archived FROM tasks WHERE id=?`, taskID); got != "cancelled/1" {
			t.Fatalf("interrupted task = %q", got)
		}
		if got := queryStr(t, db, `SELECT outcome FROM runs WHERE id=?`, run); got != "interrupted" {
			t.Fatalf("interrupted run outcome = %q", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id=1`); got != "<NULL>" {
			t.Fatalf("interrupt left lease held: %q", got)
		}
		replay := runMC(t, spineEnv(spine), "", "task", "interrupt", fmt.Sprint(taskID))
		if replay.code != 1 {
			t.Fatalf("interrupt replay exit=%d stderr=%q", replay.code, replay.stderr)
		}
	})

	t.Run("wrong_subject_and_pipeline_provenance_are_inert", func(t *testing.T) {
		spine, taskID, run, env := workerFixture(t, "protected live run")
		other := taskAdd(t, spine, "not in flight")
		wrong := runMC(t, spineEnv(spine), "", "task", "interrupt", fmt.Sprint(other))
		if wrong.code != 1 || !strings.Contains(wrong.stderr, "live lease") {
			t.Fatalf("wrong-subject exit=%d stderr=%q", wrong.code, wrong.stderr)
		}
		denied := runMC(t, env, "", "task", "interrupt", fmt.Sprint(taskID))
		if denied.code != 1 || !strings.Contains(denied.stderr, "operator verb") {
			t.Fatalf("pipeline interrupt exit=%d stderr=%q", denied.code, denied.stderr)
		}
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id=1`); got != run {
			t.Fatalf("refusals disturbed lease: %q", got)
		}
		if got := queryStr(t, db, `SELECT decision FROM tasks WHERE id=?`, other); got != "<NULL>" {
			t.Fatalf("wrong-subject interrupt decided other task: %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// The pipeline walk: the contract §7 ladder minus Docker — every stage
// advanced by a fresh `mc dispatch` against a temp spine, every terminal
// written by the role verb through the real CLI.
// ---------------------------------------------------------------------------

func TestPipelineWalk(t *testing.T) {
	spine := initSpine(t)
	db := openDB(t, spine)
	taskID := taskAdd(t, spine, "walk the skeleton")

	// Stage: Editor spawn — pool snapshot, lease held, runs row committed
	// before any process exists (Inv. 4).
	eff := dispatchExpect(t, spine, "spawn")
	if eff["role"] != "editor" {
		t.Fatalf("first spawn role = %v, want editor", eff["role"])
	}
	editorRun := eff["run_id"].(string)
	if pool := eff["pool_ids"].([]any); len(pool) != 1 || pool[0] != float64(taskID) {
		t.Fatalf("pool_ids = %v, want [%d]", pool, taskID)
	}
	if eff["session_path"] != "sessions/"+editorRun {
		t.Fatalf("session_path = %v", eff["session_path"])
	}
	if hb := eff["heartbeat_interval_s"]; hb != float64(60) {
		t.Fatalf("heartbeat_interval_s = %v, want the lock default 60", hb)
	}
	if got := queryStr(t, db, `SELECT owner FROM lock WHERE id = 1`); got != "editor" {
		t.Fatalf("lock.owner = %q", got)
	}
	if got := queryStr(t, db, `SELECT pool_snapshot FROM runs WHERE id = ?`, editorRun); got != fmt.Sprintf("[%d]", taskID) {
		t.Fatalf("pool_snapshot = %q", got)
	}

	// A held, fresh lease idles the next tick (§10 step 0).
	if eff := dispatch(t, spine); eff["action"] != "idle" || eff["reason"] != "lease-held" {
		t.Fatalf("second dispatch = %v, want idle lease-held", eff)
	}

	// Editor terminal: batch promote over exactly the snapshotted pool.
	editorEnv := runJSONEnv(t, spine, editorRun, "pipeline", "editor")
	res := runMC(t, editorEnv, fmt.Sprintf(`{"verdicts":[{"task":%d,"decision":"promote","reason":"solid"}]}`, taskID),
		"editor", "decide", "--run", editorRun, "--batch", "-")
	if res.code != 0 {
		t.Fatalf("editor decide failed (%d): %s", res.code, res.stderr)
	}
	if got := queryStr(t, db, `SELECT status FROM tasks WHERE id = ?`, taskID); got != "seeded" {
		t.Fatalf("task status = %q, want seeded", got)
	}
	if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != "<NULL>" {
		t.Fatalf("lease not released after editor terminal: %q", got)
	}
	if got := queryStr(t, db, `SELECT ended_at FROM runs WHERE id = ?`, editorRun); got == "<NULL>" {
		t.Fatalf("runs.ended_at not stamped")
	}

	// Stage: Worker spawn.
	eff = dispatchExpect(t, spine, "spawn")
	if eff["role"] != "worker" || eff["subject_id"] != float64(taskID) {
		t.Fatalf("spawn = %v, want worker on task %d", eff, taskID)
	}
	workerRun := eff["run_id"].(string)
	workerEnv := runJSONEnv(t, spine, workerRun, "pipeline", "worker")

	// Runner lifecycle: heartbeat advances last_heartbeat_at, never the hard
	// deadline (Inv. 1); register-session records the locators (ADR-001 D5).
	deadlineBefore := queryStr(t, db, `SELECT hard_deadline_at FROM lock WHERE id = 1`)
	hb := runMC(t, workerEnv, "", "heartbeat", workerRun)
	if hb.code != 0 {
		t.Fatalf("heartbeat failed: %s", hb.stderr)
	}
	if got := queryStr(t, db, `SELECT last_heartbeat_at FROM lock WHERE id = 1`); got == "<NULL>" {
		t.Fatalf("heartbeat did not stamp last_heartbeat_at")
	}
	if got := queryStr(t, db, `SELECT hard_deadline_at FROM lock WHERE id = 1`); got != deadlineBefore {
		t.Fatalf("heartbeat moved hard_deadline_at %q → %q (Inv. 1)", deadlineBefore, got)
	}
	if res := runMC(t, workerEnv, "", "heartbeat", "not-the-run"); res.code != 1 {
		t.Fatalf("stale heartbeat exit = %d, want 1", res.code)
	}
	rs := runMC(t, workerEnv, "", "run", "register-session", workerRun,
		"--native-ref", "fake-session", "--file", "native.jsonl")
	if rs.code != 0 {
		t.Fatalf("register-session failed: %s", rs.stderr)
	}
	if got := queryStr(t, db, `SELECT native_session_ref FROM runs WHERE id = ?`, workerRun); got != "fake-session" {
		t.Fatalf("native_session_ref = %q", got)
	}
	if replay := runMC(t, workerEnv, "", "run", "register-session", workerRun,
		"--native-ref", "fake-session", "--file", "native.jsonl"); replay.code != 0 {
		t.Fatalf("same-value locator replay exit=%d stderr=%q", replay.code, replay.stderr)
	}
	if conflict := runMC(t, workerEnv, "", "run", "register-session", workerRun,
		"--native-ref", "different-session", "--file", "other.jsonl"); conflict.code != 1 {
		t.Fatalf("conflicting locator rewrite exit=%d stderr=%q", conflict.code, conflict.stderr)
	}
	if got := queryStr(t, db, `SELECT native_session_ref || '/' || trace_filename FROM runs WHERE id = ?`, workerRun); got != "fake-session/native.jsonl" {
		t.Fatalf("conflicting registration rewrote locators to %q", got)
	}
	if res := runMC(t, workerEnv, "", "run", "register-session", "not-the-run",
		"--native-ref", "x", "--file", "y"); res.code != 1 {
		t.Fatalf("unknown-run register-session exit = %d, want 1", res.code)
	}

	// Worker terminal: seeded → worked, branch recorded (Ambiguity A2), and
	// complete never dispatches (Inv. 3) — no effect data, lease free.
	branch := fmt.Sprintf("mc/task-%d", taskID)
	runsBefore := queryInt(t, db, `SELECT COUNT(*) FROM runs`)
	res = runMC(t, workerEnv, "", "complete", fmt.Sprint(taskID),
		"--run", workerRun, "--status", "worked", "--branch", branch)
	if res.code != 0 {
		t.Fatalf("complete worked failed (%d): %s", res.code, res.stderr)
	}
	if _, has := res.json["action"]; has {
		t.Fatalf("complete returned effect data %v — it must never dispatch (Inv. 3)", res.json)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM runs`); got != runsBefore {
		t.Fatalf("complete changed runs count %d → %d — it dispatched (Inv. 3)", runsBefore, got)
	}
	if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != "<NULL>" {
		t.Fatalf("lease not released by complete: %q", got)
	}
	if got := queryStr(t, db, `SELECT status || '/' || branch FROM tasks WHERE id = ?`, taskID); got != "worked/"+branch {
		t.Fatalf("task = %q, want worked/%s", got, branch)
	}

	// Replay of the same terminal is fenced off (§10): the lease is gone.
	res = runMC(t, workerEnv, "", "complete", fmt.Sprint(taskID),
		"--run", workerRun, "--status", "worked")
	if res.code != 1 {
		t.Fatalf("replayed complete exit = %d, want 1 (stale-run fencing)", res.code)
	}

	// register-session is NOT lease-fenced (ADR-001 D6 "(own run)"): the
	// runner fires it at session-start, which can lose the race against the
	// behavior's terminal verb releasing the lease — the own-row write must
	// still land after release, or the locators are silently lost forever.
	rs = runMC(t, workerEnv, "", "run", "register-session", workerRun,
		"--native-ref", "fake-session", "--file", "native.jsonl")
	if rs.code != 0 {
		t.Fatalf("register-session after lease release exit = %d, want 0 (own-row identity, not fencing): %s", rs.code, rs.stderr)
	}

	// Stage: Verifier.
	eff = dispatchExpect(t, spine, "spawn")
	if eff["role"] != "verifier" {
		t.Fatalf("spawn role = %v, want verifier", eff["role"])
	}
	verifierRun := eff["run_id"].(string)
	verifierEnv := runJSONEnv(t, spine, verifierRun, "pipeline", "verifier")
	res = runMC(t, verifierEnv, "", "verifier", "verdict", fmt.Sprint(taskID),
		"--run", verifierRun, "--outcome", "pass",
		"--evidence", "/mc/session/evidence.md", "--sha", "abc1234")
	if res.code != 0 {
		t.Fatalf("verifier verdict failed (%d): %s", res.code, res.stderr)
	}
	if got := queryStr(t, db, `SELECT status || '/' || verified_sha FROM tasks WHERE id = ?`, taskID); got != "verified/abc1234" {
		t.Fatalf("task = %q, want verified/abc1234", got)
	}

	// Stage: Packager — verified → packaged AND packet birth in the same
	// transaction (Inv. 10/11).
	eff = dispatchExpect(t, spine, "spawn")
	if eff["role"] != "packager" {
		t.Fatalf("spawn role = %v, want packager", eff["role"])
	}
	packagerRun := eff["run_id"].(string)
	packagerEnv := runJSONEnv(t, spine, packagerRun, "pipeline", "packager")
	res = runMC(t, packagerEnv, "", "complete", fmt.Sprint(taskID),
		"--run", packagerRun, "--status", "packaged", "--outputs", "packet/render.html")
	if res.code != 0 {
		t.Fatalf("complete packaged failed (%d): %s", res.code, res.stderr)
	}
	pl := runMC(t, spineEnv(spine), "", "packet", "list")
	packets := pl.json["packets"].([]any)
	if len(packets) != 1 {
		t.Fatalf("packets = %v, want exactly one (born with packaging)", packets)
	}
	packet := packets[0].(map[string]any)
	if packet["task_id"] != float64(taskID) || packet["archived"] != float64(0) {
		t.Fatalf("packet = %v", packet)
	}
	if packet["render_path"] != "packet/render.html" {
		t.Fatalf("render_path = %v", packet["render_path"])
	}

	// Board drained: §10 step (4) legitimately spawns Strategist(propose);
	// the fake terminal is the empty batch (contract §7 liveness note).
	eff = dispatchExpect(t, spine, "spawn")
	if eff["role"] != "strategist(propose)" {
		t.Fatalf("drained-board spawn role = %v, want strategist(propose)", eff["role"])
	}
	if eff["subject_id"] != nil {
		t.Fatalf("strategist(propose) subject_id = %v, want null (subjectless lease)", eff["subject_id"])
	}
	strategistRun := eff["run_id"].(string)
	strategistEnv := runJSONEnv(t, spine, strategistRun, "pipeline", "strategist(propose)")
	tasksBefore := queryInt(t, db, `SELECT COUNT(*) FROM tasks`)
	res = runMC(t, strategistEnv, `{"proposals":[]}`,
		"strategist", "propose", "--run", strategistRun, "--batch", "-")
	if res.code != 0 {
		t.Fatalf("strategist propose failed (%d): %s", res.code, res.stderr)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM tasks`); got != tasksBefore {
		t.Fatalf("empty batch grew the board %d → %d", tasksBefore, got)
	}
	if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != "<NULL>" {
		t.Fatalf("subjectless lease not released: %q", got)
	}

	// The approve/land split, first half (§7): approve is a pure state
	// write — decision set, status still packaged, nothing archived.
	res = runMC(t, spineEnv(spine), "", "packet", "decide", fmt.Sprint(taskID), "--approve")
	if res.code != 0 {
		t.Fatalf("packet decide --approve failed (%d): %s", res.code, res.stderr)
	}
	if res.json["archived"] != false {
		t.Fatalf("branch-carrying approve archived synchronously: %v", res.json)
	}
	tg := runMC(t, spineEnv(spine), "", "task", "get", fmt.Sprint(taskID))
	if tg.json["decision"] != "approved" || tg.json["status"] != "packaged" || tg.json["archived"] != float64(0) {
		t.Fatalf("post-approve task = %v", tg.json)
	}

	// Second half: the next tick returns the land effect — pure effect data,
	// no writes (a re-dispatch returns it again).
	for i := 0; i < 2; i++ {
		eff = dispatchExpect(t, spine, "land")
		if eff["task_id"] != float64(taskID) || eff["branch"] != branch ||
			eff["verified_sha"] != "abc1234" || eff["target_ref"] != "main" {
			t.Fatalf("land effect = %v", eff)
		}
	}
	if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != "<NULL>" {
		t.Fatalf("land effect claimed the lease: %q (landing holds no lease, §7)", got)
	}

	// Landing success: archived, packet cascaded, slot freed.
	res = runMC(t, spineEnv(spine), "", "land", "report", fmt.Sprint(taskID), "--status", "success")
	if res.code != 0 {
		t.Fatalf("land report failed (%d): %s", res.code, res.stderr)
	}
	tg = runMC(t, spineEnv(spine), "", "task", "get", fmt.Sprint(taskID))
	if tg.json["archived"] != float64(1) {
		t.Fatalf("post-land task not archived: %v", tg.json)
	}
	pl = runMC(t, spineEnv(spine), "", "packet", "list")
	if got := pl.json["packets"].([]any)[0].(map[string]any)["archived"]; got != float64(1) {
		t.Fatalf("packet not cascaded to archived: %v", got)
	}
}

// ---------------------------------------------------------------------------
// CAS claim: two concurrent claimants, exactly one winner (§10 fencing).
// ---------------------------------------------------------------------------

func TestDispatchWaitsForProcessFlock(t *testing.T) {
	spine := initSpine(t)
	taskAdd(t, spine, "flock-serialized task")
	lockPath := filepath.Join(filepath.Dir(spine), "mc.dispatch.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		}
	}()

	cmd := exec.Command(mcBin, "dispatch")
	cmd.Env = append(os.Environ(), spineEnv(spine)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		t.Fatalf("dispatch exited while mc.dispatch.lock was held: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	case <-time.After(150 * time.Millisecond):
		// Still blocked before evaluation: no Run row can have opened.
	}
	db := openDB(t, spine)
	if n := queryInt(t, db, `SELECT COUNT(*) FROM runs`); n != 0 {
		t.Fatalf("dispatch evaluated while process flock was held: runs=%d", n)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	locked = false

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("dispatch after flock release: %v stderr=%q", err, stderr.String())
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("dispatch did not resume after process flock release")
	}
	var effect map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &effect); err != nil {
		t.Fatalf("dispatch output %q: %v", stdout.String(), err)
	}
	if effect["action"] != "spawn" {
		t.Fatalf("post-flock effect = %v, want spawn", effect)
	}
}

func TestDispatchRoutingResolution(t *testing.T) {
	writeRouting := func(t *testing.T, spine, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(filepath.Dir(spine), "routing.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("canonical_route_stamps_resolved_harness_and_binding", func(t *testing.T) {
		spine := initSpine(t)
		writeRouting(t, spine, defaultRoutingMarkdown)
		id := taskAdd(t, spine, "already edited")
		db := openDB(t, spine)
		if _, err := db.Exec(`UPDATE tasks SET status='seeded' WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, spineEnv(spine), "", "dispatch")
		if res.code != 0 {
			t.Fatalf("dispatch failed: %s", res.stderr)
		}
		if res.json["role"] != "worker" || res.json["harness"] != "claude-sdk" || res.json["model_binding"] != "minimax" {
			t.Fatalf("resolved spawn = %v", res.json)
		}
		run := res.json["run_id"].(string)
		if got := queryStr(t, db, `SELECT binding FROM runs WHERE id=?`, run); got != "claude-sdk/minimax" {
			t.Fatalf("runs.binding = %q", got)
		}
	})

	// A routing failure at attest is ADR-016 D4's deployment-health refusal
	// (health.routing_invalid; phase3-contract row 174): one dispatch.health
	// action under a derived dispatch_key, no Run, no claim, no task blame,
	// exit 0 with the terminal refused effect. It stopped being a command
	// error when the D1 seam landed (2026-07-16).
	assertRoutingHealthRefusal := func(t *testing.T, spine string, res mcResult) {
		t.Helper()
		if res.code != 0 {
			t.Fatalf("a routing health refusal is a terminal effect, not an error: exit=%d stderr=%q", res.code, res.stderr)
		}
		if res.json["action"] != "refused" || res.json["class"] != "health" ||
			res.json["code"] != "health.routing_invalid" || res.json["consequence"] != "health" {
			t.Fatalf("effect = %v, want refused/health/health.routing_invalid", res.json)
		}
		db := openDB(t, spine)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM runs`); n != 0 {
			t.Fatalf("a routing refusal opened %d runs", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM tasks WHERE blocked = 1`); n != 0 {
			t.Fatalf("a deployment health refusal blocked %d tasks", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind='dispatch.health' AND dispatch_key IS NOT NULL`); n != 1 {
			t.Fatalf("want exactly one keyed dispatch.health action, got %d", n)
		}
	}

	t.Run("missing_file_is_health_refusal_before_claim", func(t *testing.T) {
		spine := initSpine(t)
		if err := os.Remove(filepath.Join(filepath.Dir(spine), "routing.md")); err != nil {
			t.Fatal(err)
		}
		taskAdd(t, spine, "must not dispatch")
		assertRoutingHealthRefusal(t, spine, runMC(t, spineEnv(spine), "", "dispatch"))
	})

	t.Run("unresolved_binding_is_health_refusal_before_claim", func(t *testing.T) {
		spine := initSpine(t)
		writeRouting(t, spine, strings.Replace(defaultRoutingMarkdown,
			"| worker | claude-sdk | minimax |", "| worker | claude-sdk | missing |", 1))
		taskAdd(t, spine, "must not dispatch")
		assertRoutingHealthRefusal(t, spine, runMC(t, spineEnv(spine), "", "dispatch"))
	})

	t.Run("producer_judge_same_family_is_health_refusal_before_claim", func(t *testing.T) {
		spine := initSpine(t)
		writeRouting(t, spine, strings.Replace(defaultRoutingMarkdown,
			"| worker | claude-sdk | minimax |", "| worker | codex | chatgpt |", 1))
		taskAdd(t, spine, "must not dispatch")
		assertRoutingHealthRefusal(t, spine, runMC(t, spineEnv(spine), "", "dispatch"))
	})

	t.Run("explicit_test_fake_route_propagates_without_fallback", func(t *testing.T) {
		spine := initSpine(t)
		taskAdd(t, spine, "fake route")
		res := runMC(t, spineEnv(spine), "", "dispatch")
		if res.code != 0 || res.json["harness"] != "fake" || res.json["model_binding"] != "fake" {
			t.Fatalf("fake dispatch code=%d json=%v stderr=%q", res.code, res.json, res.stderr)
		}
	})

	t.Run("invalid_routing_does_not_block_pending_land", func(t *testing.T) {
		spine := initSpine(t)
		id := taskAdd(t, spine, "already verified")
		packageTask(t, spine, id, fmt.Sprintf("mc/task-%d", id))
		res := runMC(t, spineEnv(spine), "", "packet", "decide", fmt.Sprint(id), "--approve")
		if res.code != 0 {
			t.Fatalf("approve fixture: %s", res.stderr)
		}
		writeRouting(t, spine, "not a routing table\n")
		res = runMC(t, spineEnv(spine), "", "dispatch")
		if res.code != 0 || res.json["action"] != "land" {
			t.Fatalf("pending land under broken routing code=%d json=%v stderr=%q", res.code, res.json, res.stderr)
		}
	})

	t.Run("relative_MC_HOME_is_refused", func(t *testing.T) {
		spine := initSpine(t)
		taskAdd(t, spine, "absolute home only")
		res := runMC(t, []string{"MC_SPINE=" + spine, "MC_HOME=relative/home"}, "", "dispatch")
		if res.code != 2 || !strings.Contains(res.stderr, "must be absolute") {
			t.Fatalf("exit=%d stderr=%q", res.code, res.stderr)
		}
	})

	t.Run("unset_MC_HOME_uses_only_default_home", func(t *testing.T) {
		spine := initSpine(t)
		taskAdd(t, spine, "default home")
		home := t.TempDir()
		root := filepath.Join(home, ".mission-control")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "routing.md"), []byte(defaultRoutingMarkdown), 0o600); err != nil {
			t.Fatal(err)
		}
		// The deployment mirror lives in whichever MC_HOME dispatch resolves.
		db := openDB(t, spine)
		uuid := queryStr(t, db, `SELECT deployment_uuid FROM meta WHERE id = 1`)
		if err := os.WriteFile(filepath.Join(root, "deployment.uuid"), []byte(uuid+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, []string{"MC_SPINE=" + spine, "HOME=" + home}, "", "dispatch")
		if res.code != 0 || res.json["harness"] != "codex" || res.json["model_binding"] != "chatgpt" {
			t.Fatalf("default-home route code=%d json=%v stderr=%q", res.code, res.json, res.stderr)
		}
	})
}

func TestDispatchCASSingleWinner(t *testing.T) {
	spine := initSpine(t)
	taskAdd(t, spine, "contended task")

	const claimants = 4
	results := make([]mcResult, claimants)
	var wg sync.WaitGroup
	for i := 0; i < claimants; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(mcBin, "dispatch")
			cmd.Env = append(os.Environ(), spineEnv(spine)...)
			var outBuf, errBuf bytes.Buffer
			cmd.Stdout = &outBuf
			cmd.Stderr = &errBuf
			err := cmd.Run()
			res := mcResult{stdout: outBuf.String(), stderr: errBuf.String()}
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					res.code = exit.ExitCode()
				} else {
					res.code = -1
				}
			}
			results[i] = res
		}(i)
	}
	wg.Wait()

	spawns, losers := 0, 0
	for i, res := range results {
		if res.code != 0 {
			t.Fatalf("claimant %d exit = %d: %s", i, res.code, res.stderr)
		}
		var eff map[string]any
		if err := json.Unmarshal([]byte(res.stdout), &eff); err != nil {
			t.Fatalf("claimant %d output: %v", i, err)
		}
		switch eff["action"] {
		case "spawn":
			spawns++
		case "idle":
			// Prepared after the winner committed: §10's held-lease return.
			if eff["reason"] != "lease-held" {
				t.Fatalf("claimant %d idle reason = %v", i, eff["reason"])
			}
			losers++
		case "refused":
			// Prepared before the winner committed: the ADR-016 D1 commit
			// fence — only one commit can match current state; the loser
			// stales inertly and the next tick re-decides.
			if eff["code"] != "preflight.stale" || eff["consequence"] != "none" {
				t.Fatalf("claimant %d refused with %v/%v, want preflight.stale/none", i, eff["code"], eff["consequence"])
			}
			losers++
		default:
			t.Fatalf("claimant %d action = %v", i, eff["action"])
		}
	}
	if spawns != 1 || losers != claimants-1 {
		t.Fatalf("spawns = %d, losers = %d; want exactly one winner among %d", spawns, losers, claimants)
	}

	db := openDB(t, spine)
	if n := queryInt(t, db, `SELECT COUNT(*) FROM runs`); n != 1 {
		t.Fatalf("runs rows = %d, want 1 (losers must not open runs)", n)
	}
}

// ---------------------------------------------------------------------------
// Reaping through the CLI (contract §2 mc dispatch reap writes).
// ---------------------------------------------------------------------------

func TestDispatchReap(t *testing.T) {
	t.Run("spawn_watchdog_charges_retries", func(t *testing.T) {
		spine := initSpine(t, "--spawn-grace-s", "5")
		taskID := taskAdd(t, spine, "doomed spawn")
		eff := dispatchExpect(t, spine, "spawn")
		runID := eff["run_id"].(string)

		// Fixture surgery: age the stamped-but-never-heartbeated claim past
		// spawn_grace_s (the lock domain's clock cannot be injected — A6).
		db := openDB(t, spine)
		if _, err := db.Exec(`UPDATE lock SET acquired_at = datetime('now', '-60 seconds') WHERE id = 1`); err != nil {
			t.Fatal(err)
		}

		reap := dispatchExpect(t, spine, "reap")
		if reap["run_id"] != runID || reap["reason"] != "spawn-watchdog" || reap["stop_container"] != true {
			t.Fatalf("reap effect = %v", reap)
		}
		if got := queryStr(t, db, `SELECT outcome FROM runs WHERE id = ?`, runID); got != "reaped" {
			t.Fatalf("runs.outcome = %q, want reaped", got)
		}
		if got := queryInt(t, db, `SELECT dispatch_retries FROM tasks WHERE id = ?`, taskID); got != 2 {
			t.Fatalf("dispatch_retries = %d, want 2 (charged once)", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != "<NULL>" {
			t.Fatalf("lease not freed by reap: %q", got)
		}
		// The next tick simply re-selects the subject (§10: recovery is
		// mostly the absence of a problem).
		if eff := dispatchExpect(t, spine, "spawn"); eff["role"] != "editor" {
			t.Fatalf("post-reap re-select role = %v", eff["role"])
		}
	})

	t.Run("exhausted_budget_blocks_subject", func(t *testing.T) {
		spine := initSpine(t, "--spawn-grace-s", "5")
		taskID := taskAdd(t, spine, "always dying")
		db := openDB(t, spine)
		if _, err := db.Exec(`UPDATE tasks SET dispatch_retries = 1 WHERE id = ?`, taskID); err != nil {
			t.Fatal(err)
		}
		dispatchExpect(t, spine, "spawn")
		if _, err := db.Exec(`UPDATE lock SET acquired_at = datetime('now', '-60 seconds') WHERE id = 1`); err != nil {
			t.Fatal(err)
		}
		dispatchExpect(t, spine, "reap")
		if got := queryInt(t, db, `SELECT blocked FROM tasks WHERE id = ?`, taskID); got != 1 {
			t.Fatalf("subject not blocked at budget exhaustion (§10 step 0)")
		}
		if got := queryStr(t, db, `SELECT blocked_reason FROM tasks WHERE id = ?`, taskID); !strings.Contains(got, "dispatch retries exhausted") {
			t.Fatalf("blocked_reason = %q", got)
		}
		// Blocked and nothing else on the board → step (4).
		if eff := dispatchExpect(t, spine, "spawn"); eff["role"] != "strategist(propose)" {
			t.Fatalf("post-block spawn role = %v", eff["role"])
		}
	})
}

func TestZombieCLILeavesNewHolderUntouched(t *testing.T) {
	spine, taskID, zombieRun, zombieEnv := workerFixture(t, "zombie CLI fence")
	db := openDB(t, spine)
	if _, err := db.Exec(`UPDATE lock SET acquired_at=datetime('now', '-10 minutes'),
		last_heartbeat_at=NULL, hard_deadline_at=datetime('now', '+1 hour') WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if got := dispatchExpect(t, spine, "reap"); got["run_id"] != zombieRun {
		t.Fatalf("reaped run = %v, want %s", got["run_id"], zombieRun)
	}
	fresh := dispatchExpect(t, spine, "spawn")
	freshRun := fresh["run_id"].(string)
	if freshRun == zombieRun {
		t.Fatal("re-claim reused fencing token")
	}
	snapshot := func() string {
		return queryStr(t, db, `SELECT run_id || '|' || owner || '|' || subject || '|' ||
			acquired_at || '|' || COALESCE(last_heartbeat_at, 'null') || '|' || hard_deadline_at
			FROM lock WHERE id=1`)
	}
	before := snapshot()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "old_complete", args: []string{"complete", fmt.Sprint(taskID), "--run", zombieRun, "--status", "worked"}},
		{name: "old_heartbeat", args: []string{"heartbeat", zombieRun}},
		{name: "zombie_supplies_new_token", args: []string{"complete", fmt.Sprint(taskID), "--run", freshRun, "--status", "worked"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runMC(t, zombieEnv, "", tc.args...)
			if res.code != 1 {
				t.Fatalf("exit = %d stderr=%q, want fenced refusal", res.code, res.stderr)
			}
		})
	}
	if after := snapshot(); after != before {
		t.Fatalf("zombie CLI disturbed new lease:\n before %s\n after  %s", before, after)
	}
}

// ---------------------------------------------------------------------------
// mc packet decide — validation + Phase 2 revise/cancel arms (§7).
// ---------------------------------------------------------------------------

func TestPacketDecideValidation(t *testing.T) {
	spine := initSpine(t)
	taskID := taskAdd(t, spine, "not yet packaged")

	tests := []struct {
		name string
		args []string
		want int
		msg  string
	}{
		{"approve_with_reason_forbidden",
			[]string{"packet", "decide", fmt.Sprint(taskID), "--approve", "--reason", "no"}, 1, "forbidden"},
		{"revise_without_reason",
			[]string{"packet", "decide", fmt.Sprint(taskID), "--revise"}, 1, "required"},
		{"cancel_without_reason",
			[]string{"packet", "decide", fmt.Sprint(taskID), "--cancel"}, 1, "required"},
		{"no_flag_is_usage",
			[]string{"packet", "decide", fmt.Sprint(taskID)}, 2, ""},
		{"two_flags_is_usage",
			[]string{"packet", "decide", fmt.Sprint(taskID), "--approve", "--cancel"}, 2, ""},
		{"approve_unpackaged_is_domain",
			[]string{"packet", "decide", fmt.Sprint(taskID), "--approve"}, 1, "packaged"},
		{"approve_missing_task",
			[]string{"packet", "decide", "999", "--approve"}, 1, "no task"},
		{"cancel_without_packet_is_domain",
			[]string{"packet", "decide", fmt.Sprint(taskID), "--cancel", "--reason", "invisible"}, 1, "Review Packet"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runMC(t, spineEnv(spine), "", tc.args...)
			if res.code != tc.want {
				t.Fatalf("exit = %d, want %d (stderr %q)", res.code, tc.want, res.stderr)
			}
			if tc.msg != "" && !strings.Contains(res.stderr, tc.msg) {
				t.Fatalf("stderr %q missing %q", res.stderr, tc.msg)
			}
		})
	}

	packaged := func(t *testing.T, title string) (string, int64, *sql.DB) {
		t.Helper()
		s := initSpine(t)
		id := taskAdd(t, s, title)
		packageTask(t, s, id, "")
		db := openDB(t, s)
		return s, id, db
	}

	t.Run("revise_reenters_same_task_and_keeps_packet_live", func(t *testing.T) {
		s, id, db := packaged(t, "revise me")
		res := runMC(t, spineEnv(s), "", "packet", "decide", fmt.Sprint(id),
			"--revise", "--reason", "tighten evidence")
		if res.code != 0 {
			t.Fatalf("revise failed: %s", res.stderr)
		}
		if got := queryStr(t, db, `SELECT status || '/' || refine_notes FROM tasks WHERE id = ?`, id); got != "seeded/tighten evidence" {
			t.Fatalf("re-entered task = %q", got)
		}
		if got := queryInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, id); got != 0 {
			t.Fatalf("revise freed packet slot: archived=%d", got)
		}
	})

	t.Run("cancel_archives_task_and_packet", func(t *testing.T) {
		s, id, db := packaged(t, "cancel me")
		res := runMC(t, spineEnv(s), "", "packet", "decide", fmt.Sprint(id),
			"--cancel", "--reason", "no longer useful")
		if res.code != 0 {
			t.Fatalf("cancel failed: %s", res.stderr)
		}
		if got := queryStr(t, db, `SELECT decision || '/' || archived FROM tasks WHERE id = ?`, id); got != "cancelled/1" {
			t.Fatalf("cancelled task = %q", got)
		}
		if got := queryInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, id); got != 1 {
			t.Fatalf("cancel did not archive packet: %d", got)
		}
	})
}

// packageTask drives a fresh task through editor→worker→verifier→packager via
// the real CLI, returning once it is packaged. branch == "" leaves the task
// branchless (an artifact-plane deliverable).
func packageTask(t *testing.T, spine string, taskID int64, branch string) {
	t.Helper()
	eff := dispatchExpect(t, spine, "spawn") // editor over the pool
	run := eff["run_id"].(string)
	pool := eff["pool_ids"].([]any)
	verdicts := make([]string, 0, len(pool))
	for _, p := range pool {
		verdicts = append(verdicts, fmt.Sprintf(`{"task":%v,"decision":"promote","reason":"ok"}`, p))
	}
	res := runMC(t, runJSONEnv(t, spine, run, "pipeline", "editor"),
		`{"verdicts":[`+strings.Join(verdicts, ",")+`]}`,
		"editor", "decide", "--run", run, "--batch", "-")
	if res.code != 0 {
		t.Fatalf("editor decide failed: %s", res.stderr)
	}

	eff = dispatchExpect(t, spine, "spawn") // worker
	run = eff["run_id"].(string)
	args := []string{"complete", fmt.Sprint(taskID), "--run", run, "--status", "worked"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	res = runMC(t, runJSONEnv(t, spine, run, "pipeline", "worker"), "", args...)
	if res.code != 0 {
		t.Fatalf("complete worked failed: %s", res.stderr)
	}

	eff = dispatchExpect(t, spine, "spawn") // verifier
	run = eff["run_id"].(string)
	res = runMC(t, runJSONEnv(t, spine, run, "pipeline", "verifier"), "",
		"verifier", "verdict", fmt.Sprint(taskID), "--run", run,
		"--outcome", "pass", "--evidence", "e.md", "--sha", "cafe123")
	if res.code != 0 {
		t.Fatalf("verifier verdict failed: %s", res.stderr)
	}

	eff = dispatchExpect(t, spine, "spawn") // packager
	run = eff["run_id"].(string)
	res = runMC(t, runJSONEnv(t, spine, run, "pipeline", "packager"), "",
		"complete", fmt.Sprint(taskID), "--run", run, "--status", "packaged",
		"--outputs", fmt.Sprintf("packets/%d.html", taskID))
	if res.code != 0 {
		t.Fatalf("complete packaged failed: %s", res.stderr)
	}
}

func TestApproveBranchlessArchivesSynchronously(t *testing.T) {
	spine := initSpine(t)
	taskID := taskAdd(t, spine, "artifact-plane deliverable")
	packageTask(t, spine, taskID, "") // no branch

	res := runMC(t, spineEnv(spine), "", "packet", "decide", fmt.Sprint(taskID), "--approve")
	if res.code != 0 {
		t.Fatalf("approve failed: %s", res.stderr)
	}
	if res.json["archived"] != true {
		t.Fatalf("branchless approve did not archive synchronously (§7): %v", res.json)
	}
	db := openDB(t, spine)
	if got := queryInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, taskID); got != 1 {
		t.Fatalf("packet not cascaded")
	}
	// No landing pending: the next tick moves on (step 4 on the drained board).
	if eff := dispatch(t, spine); eff["action"] == "land" {
		t.Fatalf("branchless approve left a landing pending: %v", eff)
	}
	// A host cannot counterfeit a Git landing for an artifact-only packet.
	if got := runMC(t, spineEnv(spine), "", "land", "report", fmt.Sprint(taskID), "--status", "success"); got.code != 1 || !strings.Contains(got.stderr, "no branch") {
		t.Fatalf("branchless land report exit=%d stderr=%q, want inert refusal", got.code, got.stderr)
	}
}

func TestLandReportSuccessIsTerminalAndReplaySafe(t *testing.T) {
	spine := initSpine(t)
	taskID := taskAdd(t, spine, "landing report truth")
	packageTask(t, spine, taskID, fmt.Sprintf("mc/task-%d", taskID))
	if got := runMC(t, spineEnv(spine), "", "packet", "decide", fmt.Sprint(taskID), "--approve"); got.code != 0 {
		t.Fatalf("approve failed: %s", got.stderr)
	}
	for i := 0; i < 2; i++ {
		if got := runMC(t, spineEnv(spine), "", "land", "report", fmt.Sprint(taskID), "--status", "success"); got.code != 0 {
			t.Fatalf("success report %d failed: %s", i+1, got.stderr)
		}
	}

	stale := runMC(t, spineEnv(spine), "", "land", "report", fmt.Sprint(taskID),
		"--status", "failure", "--reason", "late stale failure")
	if stale.code != 1 || !strings.Contains(stale.stderr, "already landed") {
		t.Fatalf("stale failure exit=%d stderr=%q, want inert refusal", stale.code, stale.stderr)
	}
	db := openDB(t, spine)
	if got := queryStr(t, db, `SELECT archived || '/' || blocked || '/' || COALESCE(blocked_reason, 'null') FROM tasks WHERE id=?`, taskID); got != "1/0/null" {
		t.Fatalf("stale failure regressed landing truth: %q", got)
	}
	if got := queryInt(t, db, `SELECT archived FROM review_packets WHERE task_id=?`, taskID); got != 1 {
		t.Fatalf("stale failure regressed packet archive: %d", got)
	}
}

func TestDoubleApproveRejected(t *testing.T) {
	spine := initSpine(t)
	taskID := taskAdd(t, spine, "decided once")
	packageTask(t, spine, taskID, fmt.Sprintf("mc/task-%d", taskID))
	if res := runMC(t, spineEnv(spine), "", "packet", "decide", fmt.Sprint(taskID), "--approve"); res.code != 0 {
		t.Fatalf("first approve failed: %s", res.stderr)
	}
	res := runMC(t, spineEnv(spine), "", "packet", "decide", fmt.Sprint(taskID), "--approve")
	if res.code != 1 || !strings.Contains(res.stderr, "already decided") {
		t.Fatalf("double approve exit = %d (%q), want 1 already-decided", res.code, res.stderr)
	}
}

func TestLandReportFailureBlocks(t *testing.T) {
	spine := initSpine(t)
	taskID := taskAdd(t, spine, "will fail landing")
	packageTask(t, spine, taskID, fmt.Sprintf("mc/task-%d", taskID))
	runMC(t, spineEnv(spine), "", "packet", "decide", fmt.Sprint(taskID), "--approve")

	res := runMC(t, spineEnv(spine), "", "land", "report", fmt.Sprint(taskID),
		"--status", "failure", "--reason", "dirty tree overlap")
	if res.code != 0 {
		t.Fatalf("land report failure errored: %s", res.stderr)
	}
	db := openDB(t, spine)
	if got := queryStr(t, db, `SELECT blocked_reason FROM tasks WHERE id = ?`, taskID); got != "dirty tree overlap" {
		t.Fatalf("blocked_reason = %q", got)
	}
	if got := queryInt(t, db, `SELECT archived FROM tasks WHERE id = ?`, taskID); got != 0 {
		t.Fatalf("failed landing archived the task")
	}
	// Blocked gates the retry (NOTE(S6.3)): no land effect while blocked —
	// the drained board falls through to step (4) instead.
	eff := dispatch(t, spine)
	if eff["action"] == "land" {
		t.Fatalf("blocked landing still returned a land effect: %v", eff)
	}
	if eff["action"] == "spawn" && eff["role"] == "strategist(propose)" {
		// Terminate the legitimately spawned Strategist with the empty batch
		// (contract §7 liveness note) to free the lease for the retry tick.
		run := eff["run_id"].(string)
		res := runMC(t, runJSONEnv(t, spine, run, "pipeline", "strategist(propose)"),
			`{"proposals":[]}`, "strategist", "propose", "--run", run, "--batch", "-")
		if res.code != 0 {
			t.Fatalf("strategist terminal failed: %s", res.stderr)
		}
	}
	// The ordinary operator unblock re-arms the landing.
	if _, err := db.Exec(`UPDATE tasks SET blocked = 0 WHERE id = ?`, taskID); err != nil {
		t.Fatal(err)
	}
	dispatchExpect(t, spine, "land")

	t.Run("unapproved_task_rejected", func(t *testing.T) {
		other := taskAdd(t, spine, "never approved")
		res := runMC(t, spineEnv(spine), "", "land", "report", fmt.Sprint(other), "--status", "success")
		if res.code != 1 {
			t.Fatalf("exit = %d, want 1", res.code)
		}
	})
	t.Run("bad_status_is_usage", func(t *testing.T) {
		res := runMC(t, spineEnv(spine), "", "land", "report", fmt.Sprint(taskID), "--status", "meh")
		if res.code != 2 {
			t.Fatalf("exit = %d, want 2", res.code)
		}
	})
}

// ---------------------------------------------------------------------------
// Pipeline-role scope: identity, role match, fencing (ADR-001 D2).
// ---------------------------------------------------------------------------

func TestRoleScopeEnforcement(t *testing.T) {
	spine := initSpine(t)
	taskID := taskAdd(t, spine, "scoped work")
	eff := dispatchExpect(t, spine, "spawn") // editor
	run := eff["run_id"].(string)
	editorEnv := runJSONEnv(t, spine, run, "pipeline", "editor")
	forbiddenInit := filepath.Join(t.TempDir(), "pipeline-must-not-init.db")

	batch := fmt.Sprintf(`{"verdicts":[{"task":%d,"decision":"promote","reason":"ok"}]}`, taskID)
	tests := []struct {
		name string
		env  []string
		args []string
		in   string
		msg  string
	}{
		{"no_run_json_refused", spineEnv(spine),
			[]string{"editor", "decide", "--run", run, "--batch", "-"}, batch, "pipeline run identity"},
		{"wrong_role_refused", runJSONEnv(t, spine, run, "pipeline", "worker"),
			[]string{"editor", "decide", "--run", run, "--batch", "-"}, batch, "role mismatch"},
		{"wrong_tier_refused", runJSONEnv(t, spine, run, "homie", "editor"),
			[]string{"editor", "decide", "--run", run, "--batch", "-"}, batch, "pipeline-tier"},
		{"stale_run_refused", runJSONEnv(t, spine, run, "pipeline", "editor"),
			[]string{"editor", "decide", "--run", "old-run", "--batch", "-"}, batch, "stale run"},
		{"caller_run_id_mismatch_refused", runJSONEnv(t, spine, "old-run", "pipeline", "editor"),
			[]string{"editor", "decide", "--run", run, "--batch", "-"}, batch, "caller run mismatch"},
		{"complete_without_identity", spineEnv(spine),
			[]string{"complete", fmt.Sprint(taskID), "--run", run, "--status", "worked"}, "", "pipeline run identity"},
		{"heartbeat_without_identity", spineEnv(spine),
			[]string{"heartbeat", run}, "", "pipeline run identity"},
		{"register_session_without_identity", spineEnv(spine),
			[]string{"run", "register-session", run, "--native-ref", "x", "--file", "native.jsonl"}, "", "pipeline run identity"},
		{"verifier_wrong_role", runJSONEnv(t, spine, run, "pipeline", "editor"),
			[]string{"verifier", "verdict", fmt.Sprint(taskID), "--run", run,
				"--outcome", "pass", "--evidence", "e", "--sha", "s"}, "", "role mismatch"},
		{"strategist_wrong_role", runJSONEnv(t, spine, run, "pipeline", "worker"),
			[]string{"strategist", "propose", "--run", run, "--batch", "-"}, `{"proposals":[]}`, "role mismatch"},
		{"pipeline_init_denied", editorEnv,
			[]string{"init", "--spine", forbiddenInit, "--worksource", "forged"}, "", "host"},
		{"pipeline_dispatch_denied", editorEnv,
			[]string{"dispatch"}, "", "host"},
		{"pipeline_task_add_denied", editorEnv,
			[]string{"task", "add", "forged operator task", "--worksource", "ws-test"}, "", "operator verb"},
		{"pipeline_packet_decide_denied", editorEnv,
			[]string{"packet", "decide", fmt.Sprint(taskID), "--cancel", "--reason", "forged"}, "", "operator verb"},
		{"pipeline_land_report_denied", editorEnv,
			[]string{"land", "report", fmt.Sprint(taskID), "--status", "success"}, "", "host"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runMC(t, tc.env, tc.in, tc.args...)
			if res.code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr %q)", res.code, res.stderr)
			}
			if !strings.Contains(res.stderr, tc.msg) {
				t.Fatalf("stderr %q missing %q", res.stderr, tc.msg)
			}
		})
	}

	// None of the refusals touched the state: the pool is intact and the
	// lease still held by the editor run.
	db := openDB(t, spine)
	if got := queryStr(t, db, `SELECT status FROM tasks WHERE id = ?`, taskID); got != "proposed" {
		t.Fatalf("refused calls moved the task to %q", got)
	}
	if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != run {
		t.Fatalf("refused calls disturbed the lease: %q", got)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM tasks`); got != 1 {
		t.Fatalf("pipeline operator-verb attempts changed task count to %d", got)
	}
	if _, err := os.Stat(forbiddenInit); !os.IsNotExist(err) {
		t.Fatalf("pipeline init created %s", forbiddenInit)
	}
}

func TestEditorDecideCoverage(t *testing.T) {
	spine := initSpine(t)
	t1 := taskAdd(t, spine, "proposal one")
	t2 := taskAdd(t, spine, "proposal two")
	eff := dispatchExpect(t, spine, "spawn")
	run := eff["run_id"].(string)
	env := runJSONEnv(t, spine, run, "pipeline", "editor")

	tests := []struct {
		name string
		in   string
		want int
		msg  string
	}{
		{"subset_rejected",
			fmt.Sprintf(`{"verdicts":[{"task":%d,"decision":"promote","reason":"r"}]}`, t1), 1, "exactly"},
		{"superset_rejected",
			fmt.Sprintf(`{"verdicts":[{"task":%d,"decision":"promote","reason":"r"},{"task":%d,"decision":"promote","reason":"r"},{"task":99,"decision":"promote","reason":"r"}]}`, t1, t2), 1, "exactly"},
		{"unknown_decision",
			fmt.Sprintf(`{"verdicts":[{"task":%d,"decision":"defer","reason":"r"},{"task":%d,"decision":"promote","reason":"r"}]}`, t1, t2), 1, "unknown decision"},
		{"bad_json", `{"verdicts": nope}`, 1, "bad batch payload"},
		{"unknown_field_rejected",
			fmt.Sprintf(`{"verdicts":[{"task":%d,"decision":"promote","reason":"r"},{"task":%d,"decision":"reject","reason":"r"}],"surprise":true}`, t1, t2), 1, "bad batch payload"},
		{"trailing_json_rejected",
			fmt.Sprintf(`{"verdicts":[{"task":%d,"decision":"promote","reason":"r"},{"task":%d,"decision":"reject","reason":"r"}]} {}`, t1, t2), 1, "bad batch payload"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runMC(t, env, tc.in, "editor", "decide", "--run", run, "--batch", "-")
			if res.code != tc.want || !strings.Contains(res.stderr, tc.msg) {
				t.Fatalf("exit = %d stderr %q, want %d containing %q", res.code, res.stderr, tc.want, tc.msg)
			}
		})
	}

	// Atomicity (ADR-001 D1): the failed batches promoted nothing.
	db := openDB(t, spine)
	if n := queryInt(t, db, `SELECT COUNT(*) FROM tasks WHERE status = 'seeded'`); n != 0 {
		t.Fatalf("failed batches half-applied: %d seeded", n)
	}

	// The exact mixed batch promotes and rejects atomically.
	res := runMC(t, env,
		fmt.Sprintf(`{"verdicts":[{"task":%d,"decision":"promote","reason":"r"},{"task":%d,"decision":"reject","reason":"weak"}]}`, t1, t2),
		"editor", "decide", "--run", run, "--batch", "-")
	if res.code != 0 {
		t.Fatalf("exact batch failed: %s", res.stderr)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM tasks WHERE status = 'seeded'`); n != 1 {
		t.Fatalf("seeded = %d, want 1", n)
	}
	if got := queryStr(t, db, `SELECT decision || '/' || archived FROM tasks WHERE id = ?`, t2); got != "rejected/1" {
		t.Fatalf("rejected task = %q", got)
	}
}

func TestStrategistProposeInserts(t *testing.T) {
	spine := initSpine(t)
	eff := dispatchExpect(t, spine, "spawn") // empty board → strategist(propose)
	if eff["role"] != "strategist(propose)" {
		t.Fatalf("role = %v", eff["role"])
	}
	run := eff["run_id"].(string)
	env := runJSONEnv(t, spine, run, "pipeline", "strategist(propose)")

	res := runMC(t, env, `{"proposals":[
		{"worksource":"ws-test","title":"agent idea one","description":"criteria"},
		{"worksource":"ws-test","title":"agent idea two","priority":1}]}`,
		"strategist", "propose", "--run", run, "--batch", "-")
	if res.code != 0 {
		t.Fatalf("strategist propose failed: %s", res.stderr)
	}
	ids := res.json["task_ids"].([]any)
	if len(ids) != 2 {
		t.Fatalf("task_ids = %v", ids)
	}
	db := openDB(t, spine)
	if n := queryInt(t, db, `SELECT COUNT(*) FROM tasks WHERE origin = 'autonomous' AND status = 'proposed'`); n != 2 {
		t.Fatalf("autonomous proposals = %d, want 2", n)
	}
	if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != "<NULL>" {
		t.Fatalf("lease not released: %q", got)
	}

	t.Run("invalid_proposal_aborts_whole_batch", func(t *testing.T) {
		eff := dispatchExpect(t, spine, "spawn") // editor now owns the pool
		if eff["role"] != "editor" {
			t.Skipf("board state moved on: %v", eff["role"])
		}
		run := eff["run_id"].(string)
		env := runJSONEnv(t, spine, run, "pipeline", "strategist(propose)")
		// Wrong role for this lease's owner is not what we test here — use
		// the fencing-neutral validation failure instead: title missing.
		res := runMC(t, env, `{"proposals":[{"worksource":"ws-test"}]}`,
			"strategist", "propose", "--run", run, "--batch", "-")
		if res.code != 1 {
			t.Fatalf("exit = %d, want 1", res.code)
		}
	})
}

func TestStrategistProposeStrictBatchJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		batch string
	}{
		{name: "unknown_nested_field", batch: `{"proposals":[{"worksource":"ws-test","title":"idea","surprise":true}]}`},
		{name: "trailing_value", batch: `{"proposals":[]} {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spine := initSpine(t)
			eff := dispatchExpect(t, spine, "spawn")
			run := eff["run_id"].(string)
			env := runJSONEnv(t, spine, run, "pipeline", "strategist(propose)")
			res := runMC(t, env, tc.batch,
				"strategist", "propose", "--run", run, "--batch", "-")
			if res.code != 1 || !strings.Contains(res.stderr, "bad batch payload") {
				t.Fatalf("exit = %d stderr %q", res.code, res.stderr)
			}
			db := openDB(t, spine)
			if got := queryInt(t, db, `SELECT COUNT(*) FROM tasks`); got != 0 {
				t.Fatalf("invalid JSON inserted %d tasks", got)
			}
			if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != run {
				t.Fatalf("invalid JSON disturbed lease: %q", got)
			}
		})
	}
}

func TestStrategistProposeModeAndSubjectFence(t *testing.T) {
	for _, role := range []string{"strategist(initiative)", "strategist(console)"} {
		t.Run(role, func(t *testing.T) {
			spine := initSpine(t)
			eff := dispatchExpect(t, spine, "spawn")
			run := eff["run_id"].(string)
			res := runMC(t, runJSONEnv(t, spine, run, "pipeline", role),
				`{"proposals":[]}`, "strategist", "propose", "--run", run, "--batch", "-")
			if res.code != 1 || !strings.Contains(res.stderr, "role mismatch") {
				t.Fatalf("exit = %d stderr %q", res.code, res.stderr)
			}
			db := openDB(t, spine)
			if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != run {
				t.Fatalf("wrong mode disturbed lease: %q", got)
			}
		})
	}

	t.Run("subject_carrying_lease_refused", func(t *testing.T) {
		spine := initSpine(t)
		eff := dispatchExpect(t, spine, "spawn")
		run := eff["run_id"].(string)
		db := openDB(t, spine)
		res, err := db.Exec(`INSERT INTO tasks (title, worksource, target_ref) VALUES ('fixture', 'ws-test', 'main')`)
		if err != nil {
			t.Fatal(err)
		}
		taskID, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE lock SET subject = ? WHERE id = 1`, taskID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE runs SET subject = ? WHERE id = ?`, taskID, run); err != nil {
			t.Fatal(err)
		}
		got := runMC(t, runJSONEnv(t, spine, run, "pipeline", "strategist(propose)"),
			`{"proposals":[]}`, "strategist", "propose", "--run", run, "--batch", "-")
		if got.code != 1 || !strings.Contains(got.stderr, "subjectless") {
			t.Fatalf("exit = %d stderr %q", got.code, got.stderr)
		}
		if lockRun := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); lockRun != run {
			t.Fatalf("subject-shape refusal disturbed lease: %q", lockRun)
		}
	})
}

func TestConsolePublish(t *testing.T) {
	newConsoleRun := func(t *testing.T) (string, string, []string, *sql.DB) {
		t.Helper()
		spine := initSpine(t,
			"--console-hour", "0", "--console-minute", "0", "--console-tz", "UTC")
		eff := dispatchExpect(t, spine, "spawn")
		if eff["role"] != "strategist(console)" || eff["subject_id"] != nil {
			t.Fatalf("console dispatch = %v, want subjectless strategist(console)", eff)
		}
		run := eff["run_id"].(string)
		return spine, run,
			runJSONEnv(t, spine, run, "pipeline", "strategist(console)"),
			openDB(t, spine)
	}

	t.Run("publishes_event_and_dashboard_outbox_atomically", func(t *testing.T) {
		spine, run, env, db := newConsoleRun(t)
		content := "outputs/daily-console.html"
		res := runMC(t, env, "", "console", "publish", "--run", run, "--content", content)
		if res.code != 0 {
			t.Fatalf("console publish failed: %s", res.stderr)
		}
		if res.json["content_path"] != content || res.json["destinations"] != float64(1) {
			t.Fatalf("console publish result = %v", res.json)
		}
		if got := queryStr(t, db, `SELECT actor || '/' || kind || '/' || subject || '/' || detail
			FROM activity WHERE kind = 'daily.briefing'`); got != "strategist(console)/daily.briefing/"+run+"/"+content {
			t.Fatalf("daily briefing event = %q", got)
		}
		if got := queryStr(t, db, `SELECT kind || '/' || surface || '/' || COALESCE(channel_ref, '<null>') || '/' || payload
			FROM outbox`); got != `console/dashboard/<null>/{"content_path":"`+content+`"}` {
			t.Fatalf("console outbox row = %q", got)
		}
		if got := queryStr(t, db, `SELECT outcome FROM runs WHERE id = ?`, run); got != "completed" {
			t.Fatalf("console run outcome = %q", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != "<NULL>" {
			t.Fatalf("console lease not released: %q", got)
		}

		// The event is the same-day suppression record: the next tick must not
		// dispatch a second Console even though the schedule remains due.
		if eff := dispatch(t, spine); eff["role"] == "strategist(console)" {
			t.Fatalf("same-day event did not suppress a second Console: %v", eff)
		}
	})

	t.Run("scope_subject_and_content_fail_closed", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			role    string
			content string
			subject bool
			host    bool
			stale   bool
			want    int
			msg     string
		}{
			{name: "wrong_mode", role: "strategist(propose)", content: "outputs/c.html", want: 1, msg: "role mismatch"},
			{name: "host_scope", content: "outputs/c.html", host: true, want: 1, msg: "pipeline run identity"},
			{name: "caller_run_mismatch", role: "strategist(console)", content: "outputs/c.html", stale: true, want: 1, msg: "stale run"},
			{name: "missing_content", role: "strategist(console)", want: 2, msg: "requires --content"},
			{name: "content_traversal", role: "strategist(console)", content: "outputs/../outside.html", want: 1, msg: "beneath outputs"},
			{name: "subject_carrying", role: "strategist(console)", content: "outputs/c.html", subject: true, want: 1, msg: "subjectless"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				spine, run, _, db := newConsoleRun(t)
				if tc.subject {
					res, err := db.Exec(`INSERT INTO tasks (title, worksource, target_ref) VALUES ('fixture', 'ws-test', 'main')`)
					if err != nil {
						t.Fatal(err)
					}
					taskID, err := res.LastInsertId()
					if err != nil {
						t.Fatal(err)
					}
					if _, err := db.Exec(`UPDATE lock SET subject = ? WHERE id = 1`, taskID); err != nil {
						t.Fatal(err)
					}
					if _, err := db.Exec(`UPDATE runs SET subject = ? WHERE id = ?`, taskID, run); err != nil {
						t.Fatal(err)
					}
				}
				token := run
				if tc.stale {
					token = "different-run"
				}
				args := []string{"console", "publish", "--run", token}
				if tc.content != "" {
					args = append(args, "--content", tc.content)
				}
				env := runJSONEnv(t, spine, run, "pipeline", tc.role)
				if tc.host {
					env = spineEnv(spine)
				}
				got := runMC(t, env, "", args...)
				if got.code != tc.want || !strings.Contains(got.stderr, tc.msg) {
					t.Fatalf("exit = %d stderr %q, want %d containing %q", got.code, got.stderr, tc.want, tc.msg)
				}
				if n := queryInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind = 'daily.briefing'`); n != 0 {
					t.Fatalf("failed Console wrote %d activity rows", n)
				}
				if n := queryInt(t, db, `SELECT COUNT(*) FROM outbox`); n != 0 {
					t.Fatalf("failed Console wrote %d outbox rows", n)
				}
				if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != run {
					t.Fatalf("failed Console disturbed lease: %q", got)
				}
			})
		}
	})

	t.Run("outbox_failure_rolls_back_event_and_terminal", func(t *testing.T) {
		_, run, env, db := newConsoleRun(t)
		if _, err := db.Exec(`CREATE TRIGGER test_fail_console_outbox
			BEFORE INSERT ON outbox WHEN NEW.kind = 'console'
			BEGIN SELECT RAISE(ABORT, 'injected outbox failure'); END`); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, env, "", "console", "publish", "--run", run, "--content", "outputs/c.html")
		if res.code != 1 || !strings.Contains(res.stderr, "injected outbox failure") {
			t.Fatalf("exit = %d stderr %q", res.code, res.stderr)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind = 'daily.briefing'`); n != 0 {
			t.Fatalf("failed atomic publish left %d events", n)
		}
		if got := queryStr(t, db, `SELECT COALESCE(outcome, '<null>') FROM runs WHERE id = ?`, run); got != "<null>" {
			t.Fatalf("failed atomic publish ended run: %q", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != run {
			t.Fatalf("failed atomic publish released lease: %q", got)
		}
	})

	// Injecting at the transaction's LAST write (the lease release) proves
	// the event, outbox fan-out, and Run end all roll back with it — the
	// outbox-insert injection above leaves the later writes unexercised.
	t.Run("release_failure_rolls_back_event_outbox_and_run_end", func(t *testing.T) {
		_, run, env, db := newConsoleRun(t)
		if _, err := db.Exec(`CREATE TRIGGER test_fail_console_release
			BEFORE UPDATE OF run_id ON lock
			WHEN OLD.run_id IS NOT NULL AND NEW.run_id IS NULL
			BEGIN SELECT RAISE(ABORT, 'injected release failure'); END`); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, env, "", "console", "publish", "--run", run, "--content", "outputs/c.html")
		if res.code != 1 || !strings.Contains(res.stderr, "injected release failure") {
			t.Fatalf("exit = %d stderr %q", res.code, res.stderr)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind = 'daily.briefing'`); n != 0 {
			t.Fatalf("failed atomic publish left %d events", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM outbox`); n != 0 {
			t.Fatalf("failed atomic publish left %d outbox rows", n)
		}
		if got := queryStr(t, db, `SELECT COALESCE(outcome, '<null>') FROM runs WHERE id = ?`, run); got != "<null>" {
			t.Fatalf("failed atomic publish ended run: %q", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != run {
			t.Fatalf("failed atomic publish released lease: %q", got)
		}
	})
}

func TestHomieStartBindList(t *testing.T) {
	start := func(t *testing.T, spine, from string, extra ...string) mcResult {
		t.Helper()
		args := []string{"homie", "start", "--from", from}
		args = append(args, extra...)
		res := runMC(t, spineEnv(spine), "", args...)
		if res.code != 0 {
			t.Fatalf("homie start failed (%d): %s", res.code, res.stderr)
		}
		return res
	}

	t.Run("start_is_atomic_registry_state_and_never_touches_pipeline_lease", func(t *testing.T) {
		spine := initSpine(t)
		taskAdd(t, spine, "keep the pipeline lease live")
		pipeline := dispatchExpect(t, spine, "spawn")
		db := openDB(t, spine)
		lockSnapshot := func() string {
			return queryStr(t, db, `SELECT
				COALESCE(run_id, '') || '|' || COALESCE(owner, '') || '|' ||
				COALESCE(subject, '') || '|' || COALESCE(worksource, '') || '|' ||
				COALESCE(acquired_at, '') || '|' || COALESCE(last_heartbeat_at, '') || '|' ||
				COALESCE(hard_deadline_at, '') FROM lock WHERE id = 1`)
		}
		before := lockSnapshot()

		res := start(t, spine, "dashboard:console-1")
		if _, exists := res.json["action"]; exists {
			t.Fatalf("homie start returned a host effect: %v", res.json)
		}
		session := res.json["session_id"].(string)
		if !strings.HasPrefix(session, "h-") || res.json["status"] != "active" ||
			res.json["session_path"] != "sessions/"+session || res.json["binding"] != "fake/fake" {
			t.Fatalf("homie start result = %v", res.json)
		}
		if got := lockSnapshot(); got != before {
			t.Fatalf("homie start disturbed the pipeline lease:\n before %s\n after  %s", before, got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM runs`); n != 1 {
			t.Fatalf("homie start opened a Runs row: %d", n)
		}
		if got := queryStr(t, db, `SELECT id FROM runs`); got != pipeline["run_id"] {
			t.Fatalf("pipeline Run changed: %q", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM outbox`); n != 0 {
			t.Fatalf("homie start emitted %d outbox rows", n)
		}
		if got := queryStr(t, db, `SELECT status || '|' || container_name || '|' ||
			session_path || '|' || binding FROM homie_sessions WHERE id = ?`, session); got != "active|mc-homie-"+session+"|sessions/"+session+"|fake/fake" {
			t.Fatalf("registry row = %q", got)
		}
		allowJSON, _ := json.Marshal(defaultHomieAllowlist)
		if got := queryStr(t, db, `SELECT verb_allowlist FROM homie_sessions WHERE id = ?`, session); got != string(allowJSON) {
			t.Fatalf("frozen default allowlist = %q, want %s", got, allowJSON)
		}
		if got := queryStr(t, db, `SELECT surface || ':' || channel_ref FROM homie_bindings WHERE active = 1`); got != "dashboard:console-1" {
			t.Fatalf("initial binding = %q", got)
		}
		if got := queryStr(t, db, `SELECT native_session_ref || '/' || trace_filename FROM homie_sessions WHERE id = ?`, session); got != "<NULL>" {
			t.Fatalf("start guessed runner locators: %q", got)
		}

		listed := runMC(t, spineEnv(spine), "", "homie", "list")
		if listed.code != 0 {
			t.Fatalf("homie list failed: %s", listed.stderr)
		}
		sessions := listed.json["sessions"].([]any)
		if len(sessions) != 1 {
			t.Fatalf("homie list = %v", listed.json)
		}
		row := sessions[0].(map[string]any)
		if row["id"] != session || row["binding"] != "fake/fake" || row["status"] != "active" {
			t.Fatalf("listed session = %v", row)
		}
		bindings := row["active_bindings"].([]any)
		if len(bindings) != 1 || bindings[0].(map[string]any)["channel_ref"] != "console-1" {
			t.Fatalf("listed active bindings = %v", bindings)
		}
	})

	t.Run("canonical_active_session_fences_Homie_operator_mutations", func(t *testing.T) {
		spine := initSpine(t)
		session := start(t, spine, "dashboard:operator").json["session_id"].(string)
		env := homieJSONEnv(t, spine, session, defaultHomieAllowlist)

		added := runMC(t, env, "", "task", "add", "Homie relays operator intent", "--worksource", "ws-test")
		if added.code != 0 {
			t.Fatalf("allowlisted Homie task add failed: %s", added.stderr)
		}
		taskID := int64(added.json["task_id"].(float64))
		blocked := runMC(t, env, "", "task", "block", fmt.Sprint(taskID), "--reason", "operator needs to decide")
		if blocked.code != 0 {
			t.Fatalf("allowlisted Homie task block failed: %s", blocked.stderr)
		}

		db := openDB(t, spine)
		beforeTasks := queryInt(t, db, `SELECT COUNT(*) FROM tasks`)
		forged := runMC(t, homieJSONEnv(t, spine, session, []string{"task.add"}), "",
			"task", "add", "forged envelope", "--worksource", "ws-test")
		if forged.code != 1 || !strings.Contains(forged.stderr, "does not match") {
			t.Fatalf("forged allowlist = code %d stderr %q", forged.code, forged.stderr)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM tasks`) != beforeTasks {
			t.Fatal("forged Homie allowlist inserted a task")
		}

		if _, err := db.Exec(`UPDATE homie_sessions SET status = 'ended' WHERE id = ?`, session); err != nil {
			t.Fatal(err)
		}
		stale := runMC(t, env, "", "task", "add", "zombie write", "--worksource", "ws-test")
		if stale.code != 1 || !strings.Contains(stale.stderr, "not active") {
			t.Fatalf("ended Homie write = code %d stderr %q", stale.code, stale.stderr)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM tasks`) != beforeTasks {
			t.Fatal("ended Homie inserted a task")
		}

		narrow := start(t, spine, "cli:narrow", "--allow", "task.add").json["session_id"].(string)
		narrowEnv := homieJSONEnv(t, spine, narrow, []string{"task.add"})
		if got := runMC(t, narrowEnv, "", "task", "add", "narrow allowed", "--worksource", "ws-test"); got.code != 0 {
			t.Fatalf("narrow canonical Homie task add failed: %s", got.stderr)
		}
		if got := runMC(t, narrowEnv, "", "task", "unblock", fmt.Sprint(taskID)); got.code != 1 || !strings.Contains(got.stderr, "not allowed") {
			t.Fatalf("narrow forbidden verb = code %d stderr %q", got.code, got.stderr)
		}
	})

	t.Run("bind_is_idempotent_but_never_transfers_a_live_place", func(t *testing.T) {
		spine := initSpine(t)
		a := start(t, spine, "dashboard:a").json["session_id"].(string)
		b := start(t, spine, "cli:b").json["session_id"].(string)
		db := openDB(t, spine)

		bind := runMC(t, spineEnv(spine), "", "homie", "bind", a, "--from", "discord:shared")
		if bind.code != 0 || bind.json["bound"] != true {
			t.Fatalf("first bind = code %d json %v stderr %q", bind.code, bind.json, bind.stderr)
		}
		replay := runMC(t, spineEnv(spine), "", "homie", "bind", a, "--from", "discord:shared")
		if replay.code != 0 || replay.json["bound"] != false {
			t.Fatalf("bind replay = code %d json %v stderr %q", replay.code, replay.json, replay.stderr)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings
			WHERE session_id = ? AND surface = 'discord' AND channel_ref = 'shared'`, a); n != 1 {
			t.Fatalf("bind replay appended history: %d", n)
		}

		conflict := runMC(t, spineEnv(spine), "", "homie", "bind", b, "--from", "discord:shared")
		if conflict.code != 1 || !strings.Contains(conflict.stderr, "already bound") {
			t.Fatalf("cross-session bind = code %d stderr %q", conflict.code, conflict.stderr)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings WHERE surface = 'discord' AND channel_ref = 'shared'`); n != 1 {
			t.Fatalf("conflicting bind changed history: %d", n)
		}

		unknown := runMC(t, spineEnv(spine), "", "homie", "bind", "h-missing", "--from", "dashboard:x")
		if unknown.code != 1 || !strings.Contains(unknown.stderr, "unknown Homie session") {
			t.Fatalf("unknown bind = code %d stderr %q", unknown.code, unknown.stderr)
		}
		if _, err := db.Exec(`UPDATE homie_sessions SET status = 'ended' WHERE id = ?`, a); err != nil {
			t.Fatal(err)
		}
		ended := runMC(t, spineEnv(spine), "", "homie", "bind", a, "--from", "dashboard:new")
		if ended.code != 1 || !strings.Contains(ended.stderr, "requires an active") {
			t.Fatalf("ended bind = code %d stderr %q", ended.code, ended.stderr)
		}
	})

	t.Run("scope_is_host_for_start_bind_and_own_session_for_homie_list", func(t *testing.T) {
		spine := initSpine(t)
		a := start(t, spine, "dashboard:a").json["session_id"].(string)
		b := start(t, spine, "cli:b", "--allow", "task.add").json["session_id"].(string)
		db := openDB(t, spine)
		beforeSessions := queryInt(t, db, `SELECT COUNT(*) FROM homie_sessions`)
		beforeBindings := queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings`)

		pipelineEnv := runJSONEnv(t, spine, "pipeline-forgery", "pipeline", "worker")
		for _, args := range [][]string{
			{"homie", "start", "--from", "dashboard:pipeline"},
			{"homie", "bind", a, "--from", "dashboard:pipeline"},
			{"homie", "list"},
		} {
			got := runMC(t, pipelineEnv, "", args...)
			if got.code != 1 {
				t.Fatalf("pipeline %v exit = %d stderr %q", args, got.code, got.stderr)
			}
		}

		// Structural host-only rules win even over a forged/overbroad envelope.
		overbroad := homieJSONEnv(t, spine, a, []string{"homie.start", "homie.bind", "homie.list"})
		for _, args := range [][]string{
			{"homie", "start", "--from", "dashboard:forged"},
			{"homie", "bind", a, "--from", "dashboard:forged"},
		} {
			got := runMC(t, overbroad, "", args...)
			if got.code != 1 {
				t.Fatalf("Homie %v exit = %d stderr %q", args, got.code, got.stderr)
			}
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM homie_sessions`) != beforeSessions ||
			queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings`) != beforeBindings {
			t.Fatal("scope refusal mutated Homie registry")
		}

		own := runMC(t, homieJSONEnv(t, spine, a, defaultHomieAllowlist), "", "homie", "list")
		if own.code != 0 {
			t.Fatalf("own Homie list failed: %s", own.stderr)
		}
		rows := own.json["sessions"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["id"] != a {
			t.Fatalf("Homie list escaped own session: %v", own.json)
		}
		denied := runMC(t, homieJSONEnv(t, spine, b, []string{"task.add"}), "", "homie", "list")
		if denied.code != 1 || !strings.Contains(denied.stderr, "not allowed") {
			t.Fatalf("narrow Homie list = code %d stderr %q", denied.code, denied.stderr)
		}
		if _, err := db.Exec(`UPDATE homie_sessions SET status = 'ended' WHERE id = ?`, a); err != nil {
			t.Fatal(err)
		}
		stale := runMC(t, homieJSONEnv(t, spine, a, defaultHomieAllowlist), "", "homie", "list")
		if stale.code != 1 || !strings.Contains(stale.stderr, "not active") {
			t.Fatalf("ended Homie list = code %d stderr %q", stale.code, stale.stderr)
		}

		host := runMC(t, spineEnv(spine), "", "homie", "list")
		if host.code != 0 || len(host.json["sessions"].([]any)) != 2 {
			t.Fatalf("host list omitted resumable rows: code %d json %v stderr %q", host.code, host.json, host.stderr)
		}
	})

	t.Run("invalid_inputs_routing_and_binding_abort_leave_no_partial_session", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			args []string
			msg  string
		}{
			{name: "missing_separator", args: []string{"--from", "dashboard"}, msg: "surface:channel_ref"},
			{name: "unknown_surface", args: []string{"--from", "email:x"}, msg: "discord|dashboard|cli"},
			{name: "empty_channel", args: []string{"--from", "dashboard:"}, msg: "channel_ref"},
			{name: "unknown_allow", args: []string{"--from", "dashboard:x", "--allow", "dispatch"}, msg: "not a Homie-agent verb"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				spine := initSpine(t)
				got := runMC(t, spineEnv(spine), "", append([]string{"homie", "start"}, tc.args...)...)
				if got.code == 0 || !strings.Contains(got.stderr, tc.msg) {
					t.Fatalf("exit = %d stderr %q, want failure containing %q", got.code, got.stderr, tc.msg)
				}
				db := openDB(t, spine)
				if queryInt(t, db, `SELECT COUNT(*) FROM homie_sessions`) != 0 ||
					queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings`) != 0 {
					t.Fatal("invalid start left partial registry state")
				}
			})
		}

		t.Run("bad_route", func(t *testing.T) {
			spine := initSpine(t)
			if err := os.WriteFile(filepath.Join(filepath.Dir(spine), "routing.md"), []byte("broken\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			got := runMC(t, spineEnv(spine), "", "homie", "start", "--from", "dashboard:x")
			if got.code == 0 || !strings.Contains(got.stderr, "routing.md") {
				t.Fatalf("bad-route start = code %d stderr %q", got.code, got.stderr)
			}
			db := openDB(t, spine)
			if queryInt(t, db, `SELECT COUNT(*) FROM homie_sessions`) != 0 {
				t.Fatal("bad route opened a Homie session")
			}
		})

		t.Run("binding_insert_failure_rolls_back_registry", func(t *testing.T) {
			spine := initSpine(t)
			db := openDB(t, spine)
			if _, err := db.Exec(`CREATE TRIGGER test_fail_homie_binding
				BEFORE INSERT ON homie_bindings
				BEGIN SELECT RAISE(ABORT, 'injected binding failure'); END`); err != nil {
				t.Fatal(err)
			}
			got := runMC(t, spineEnv(spine), "", "homie", "start", "--from", "dashboard:x")
			if got.code != 1 || !strings.Contains(got.stderr, "injected binding failure") {
				t.Fatalf("atomic start = code %d stderr %q", got.code, got.stderr)
			}
			if queryInt(t, db, `SELECT COUNT(*) FROM homie_sessions`) != 0 ||
				queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings`) != 0 {
				t.Fatal("failed initial binding left an orphan Homie session")
			}
		})

		t.Run("already_bound_origin_does_not_start_another_session", func(t *testing.T) {
			spine := initSpine(t)
			first := start(t, spine, "dashboard:x").json["session_id"]
			got := runMC(t, spineEnv(spine), "", "homie", "start", "--from", "dashboard:x")
			if got.code != 1 || !strings.Contains(got.stderr, "already bound") {
				t.Fatalf("duplicate-origin start = code %d stderr %q", got.code, got.stderr)
			}
			db := openDB(t, spine)
			if queryInt(t, db, `SELECT COUNT(*) FROM homie_sessions`) != 1 ||
				queryStr(t, db, `SELECT id FROM homie_sessions`) != first {
				t.Fatal("duplicate-origin start created a second session")
			}
		})
	})
}

func TestHomieSendHistoryEnd(t *testing.T) {
	start := func(t *testing.T, spine, from string, allow ...string) string {
		t.Helper()
		args := []string{"homie", "start", "--from", from}
		if len(allow) != 0 {
			args = append(args, "--allow", strings.Join(allow, ","))
		}
		res := runMC(t, spineEnv(spine), "", args...)
		if res.code != 0 {
			t.Fatalf("homie start failed: %s", res.stderr)
		}
		return res.json["session_id"].(string)
	}
	bind := func(t *testing.T, spine, session, from string) {
		t.Helper()
		res := runMC(t, spineEnv(spine), "", "homie", "bind", session, "--from", from)
		if res.code != 0 {
			t.Fatalf("homie bind failed: %s", res.stderr)
		}
	}

	t.Run("send_binds_origin_appends_stable_history_and_echoes_other_surfaces", func(t *testing.T) {
		spine := initSpine(t)
		session := start(t, spine, "dashboard:dash")
		bind(t, spine, session, "discord:disc")

		// Conversation traffic is lease-free and must coexist with a live
		// leased pipeline run.
		taskAdd(t, spine, "pipeline stays live during chat")
		dispatchExpect(t, spine, "spawn")
		db := openDB(t, spine)
		lockBefore := queryStr(t, db, `SELECT run_id || '|' || owner || '|' || subject FROM lock WHERE id = 1`)
		if _, err := db.Exec(`UPDATE homie_sessions SET last_activity_at = datetime('now', '-1 day') WHERE id = ?`, session); err != nil {
			t.Fatal(err)
		}
		oldActivity := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session)

		first := runMC(t, spineEnv(spine), "", "homie", "send", session,
			"--from", "cli:terminal", "--body", "hello from CLI",
			"--attachments", `["attachments/screenshot.png"]`)
		if first.code != 0 {
			t.Fatalf("homie send failed: %s", first.stderr)
		}
		if first.json["seq"] != float64(1) || first.json["echoes"] != float64(2) {
			t.Fatalf("first send result = %v", first.json)
		}
		if got := queryStr(t, db, `SELECT direction || '|' || surface || '|' || channel_ref || '|' || body || '|' || attachments
			FROM conversation_messages WHERE session_id = ? AND seq = 1`, session); got != `inbound|cli|terminal|hello from CLI|["attachments/screenshot.png"]` {
			t.Fatalf("first conversation row = %q", got)
		}
		if got := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session); got == oldActivity {
			t.Fatalf("send did not advance last_activity_at from %q", oldActivity)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings WHERE session_id = ? AND active = 1`, session); n != 3 {
			t.Fatalf("origin traffic did not bind CLI place: %d bindings", n)
		}
		if got := queryStr(t, db, `SELECT group_concat(surface || ':' || channel_ref, ',') FROM
			(SELECT surface, channel_ref FROM outbox WHERE kind = 'homie_echo' ORDER BY surface, channel_ref)`); got != "dashboard:dash,discord:disc" {
			t.Fatalf("echo destinations = %q", got)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(queryStr(t, db, `SELECT payload FROM outbox ORDER BY id LIMIT 1`)), &payload); err != nil {
			t.Fatal(err)
		}
		origin := payload["origin"].(map[string]any)
		if payload["body"] != "hello from CLI" || payload["seq"] != float64(1) ||
			origin["surface"] != "cli" || origin["channel_ref"] != "terminal" {
			t.Fatalf("echo payload = %v", payload)
		}
		if got := queryStr(t, db, `SELECT run_id || '|' || owner || '|' || subject FROM lock WHERE id = 1`); got != lockBefore {
			t.Fatalf("send disturbed pipeline lease: before %q after %q", lockBefore, got)
		}

		second := runMC(t, spineEnv(spine), "", "homie", "send", session,
			"--from", "dashboard:dash", "--body", "second turn")
		if second.code != 0 || second.json["seq"] != float64(2) || second.json["echoes"] != float64(2) {
			t.Fatalf("second send = code %d json %v stderr %q", second.code, second.json, second.stderr)
		}

		history := runMC(t, spineEnv(spine), "", "homie", "history", session)
		if history.code != 0 {
			t.Fatalf("homie history failed: %s", history.stderr)
		}
		messages := history.json["messages"].([]any)
		if len(messages) != 2 {
			t.Fatalf("history = %v", history.json)
		}
		m1, m2 := messages[0].(map[string]any), messages[1].(map[string]any)
		if m1["seq"] != float64(1) || m2["seq"] != float64(2) || m2["body"] != "second turn" {
			t.Fatalf("history order/content = %v", messages)
		}
		attachments := m1["attachments"].([]any)
		if len(attachments) != 1 || attachments[0] != "attachments/screenshot.png" {
			t.Fatalf("history attachments = %v", attachments)
		}
		if got := m2["attachments"].([]any); len(got) != 0 {
			t.Fatalf("attachment-free history row = %v", got)
		}
	})

	t.Run("send_outbox_failure_rolls_back_binding_message_and_activity", func(t *testing.T) {
		spine := initSpine(t)
		session := start(t, spine, "dashboard:dash")
		bind(t, spine, session, "discord:disc")
		db := openDB(t, spine)
		beforeActivity := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session)
		if _, err := db.Exec(`CREATE TRIGGER test_fail_homie_echo
			BEFORE INSERT ON outbox WHEN NEW.kind = 'homie_echo'
			BEGIN SELECT RAISE(ABORT, 'injected echo failure'); END`); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, spineEnv(spine), "", "homie", "send", session,
			"--from", "cli:new-origin", "--body", "must roll back")
		if res.code != 1 || !strings.Contains(res.stderr, "injected echo failure") {
			t.Fatalf("send atomic failure = code %d stderr %q", res.code, res.stderr)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages`) != 0 ||
			queryInt(t, db, `SELECT COUNT(*) FROM outbox`) != 0 {
			t.Fatal("failed send left a message or outbox row")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings WHERE channel_ref = 'new-origin'`) != 0 {
			t.Fatal("failed send left the implicit origin binding")
		}
		if got := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session); got != beforeActivity {
			t.Fatalf("failed send changed last_activity_at: before %q after %q", beforeActivity, got)
		}
	})

	t.Run("end_activity_failure_rolls_back_status_and_binding_deactivation", func(t *testing.T) {
		spine := initSpine(t)
		session := start(t, spine, "dashboard:dash")
		db := openDB(t, spine)
		if _, err := db.Exec(`CREATE TRIGGER test_fail_homie_end_activity
			BEFORE INSERT ON activity WHEN NEW.kind = 'homie.ended'
			BEGIN SELECT RAISE(ABORT, 'injected end activity failure'); END`); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, spineEnv(spine), "", "homie", "end", session, "--reason", "must roll back")
		if res.code != 1 || !strings.Contains(res.stderr, "injected end activity failure") {
			t.Fatalf("end atomic failure = code %d stderr %q", res.code, res.stderr)
		}
		if got := queryStr(t, db, `SELECT status FROM homie_sessions WHERE id = ?`, session); got != "active" {
			t.Fatalf("failed end changed status: %q", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings WHERE session_id = ? AND active = 1`, session); n != 1 {
			t.Fatalf("failed end deactivated bindings: %d active", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind = 'homie.ended'`); n != 0 {
			t.Fatalf("failed end left %d activity rows", n)
		}
	})

	t.Run("scope_history_and_end_are_own_session_fenced", func(t *testing.T) {
		spine := initSpine(t)
		a := start(t, spine, "dashboard:a")
		b := start(t, spine, "cli:b")
		if got := runMC(t, spineEnv(spine), "", "homie", "send", a,
			"--from", "dashboard:a", "--body", "visible only in A"); got.code != 0 {
			t.Fatalf("send fixture failed: %s", got.stderr)
		}
		db := openDB(t, spine)
		beforeMessages := queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages`)

		pipeline := runJSONEnv(t, spine, "pipeline", "pipeline", "worker")
		for _, args := range [][]string{
			{"homie", "send", a, "--from", "dashboard:a", "--body", "forged"},
			{"homie", "history", a},
			{"homie", "end", a, "--reason", "forged"},
		} {
			got := runMC(t, pipeline, "", args...)
			if got.code != 1 {
				t.Fatalf("pipeline %v exit = %d stderr %q", args, got.code, got.stderr)
			}
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages`) != beforeMessages ||
			queryStr(t, db, `SELECT status FROM homie_sessions WHERE id = ?`, a) != "active" {
			t.Fatal("pipeline Homie verb mutated state")
		}

		homieA := homieJSONEnv(t, spine, a, defaultHomieAllowlist)
		forgedSend := runMC(t, homieA, "", "homie", "send", a,
			"--from", "dashboard:a", "--body", "agent cannot inject transport")
		if forgedSend.code != 1 {
			t.Fatalf("Homie-agent send exit = %d stderr %q", forgedSend.code, forgedSend.stderr)
		}
		ownHistory := runMC(t, homieA, "", "homie", "history", a)
		if ownHistory.code != 0 || len(ownHistory.json["messages"].([]any)) != 1 {
			t.Fatalf("own history = code %d json %v stderr %q", ownHistory.code, ownHistory.json, ownHistory.stderr)
		}
		otherHistory := runMC(t, homieA, "", "homie", "history", b)
		if otherHistory.code != 1 || !strings.Contains(otherHistory.stderr, "own session") {
			t.Fatalf("cross-session history = code %d stderr %q", otherHistory.code, otherHistory.stderr)
		}
		otherEnd := runMC(t, homieA, "", "homie", "end", b, "--reason", "hijack")
		if otherEnd.code != 1 || !strings.Contains(otherEnd.stderr, "own session") {
			t.Fatalf("cross-session end = code %d stderr %q", otherEnd.code, otherEnd.stderr)
		}

		lockBefore := queryStr(t, db, `SELECT COALESCE(run_id, '<free>') FROM lock WHERE id = 1`)
		ended := runMC(t, homieA, "", "homie", "end", a, "--reason", "operator done")
		if ended.code != 0 || ended.json["status"] != "ended" || ended.json["ended"] != true {
			t.Fatalf("own end = code %d json %v stderr %q", ended.code, ended.json, ended.stderr)
		}
		if got := queryStr(t, db, `SELECT status FROM homie_sessions WHERE id = ?`, a); got != "ended" {
			t.Fatalf("ended session status = %q", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings WHERE session_id = ? AND active = 1`, a); n != 0 {
			t.Fatalf("end left %d active bindings", n)
		}
		if got := queryStr(t, db, `SELECT kind || '|' || subject || '|' || detail FROM activity WHERE kind = 'homie.ended'`); got != "homie.ended|"+a+"|operator done" {
			t.Fatalf("end activity = %q", got)
		}
		if got := queryStr(t, db, `SELECT COALESCE(run_id, '<free>') FROM lock WHERE id = 1`); got != lockBefore {
			t.Fatalf("end disturbed pipeline lease: before %q after %q", lockBefore, got)
		}
		stale := runMC(t, homieA, "", "homie", "history", a)
		if stale.code != 1 || !strings.Contains(stale.stderr, "not active") {
			t.Fatalf("ended Homie history = code %d stderr %q", stale.code, stale.stderr)
		}
		hostHistory := runMC(t, spineEnv(spine), "", "homie", "history", a)
		if hostHistory.code != 0 || len(hostHistory.json["messages"].([]any)) != 1 {
			t.Fatalf("host lost ended-session history: code %d json %v", hostHistory.code, hostHistory.json)
		}
		replay := runMC(t, spineEnv(spine), "", "homie", "end", a, "--reason", "operator done")
		if replay.code != 0 || replay.json["ended"] != false || queryInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind = 'homie.ended'`) != 1 {
			t.Fatalf("host end replay = code %d json %v stderr %q", replay.code, replay.json, replay.stderr)
		}
	})

	t.Run("validation_and_cross_session_origin_conflict_are_inert", func(t *testing.T) {
		spine := initSpine(t)
		a := start(t, spine, "dashboard:a")
		b := start(t, spine, "cli:b")
		db := openDB(t, spine)
		for _, tc := range []struct {
			name string
			args []string
			msg  string
		}{
			{name: "missing_body", args: []string{"homie", "send", a, "--from", "dashboard:a"}, msg: "body or attachment"},
			{name: "bad_attachments_json", args: []string{"homie", "send", a, "--from", "dashboard:a", "--attachments", `{}`}, msg: "JSON array"},
			{name: "attachment_traversal", args: []string{"homie", "send", a, "--from", "dashboard:a", "--attachments", `["../secret"]`}, msg: "normalized relative"},
			{name: "unknown_session", args: []string{"homie", "send", "h-missing", "--from", "dashboard:x", "--body", "x"}, msg: "unknown Homie session"},
			{name: "origin_owned_by_other", args: []string{"homie", "send", a, "--from", "cli:b", "--body", "x"}, msg: "already bound"},
			{name: "history_unknown", args: []string{"homie", "history", "h-missing"}, msg: "unknown Homie session"},
			{name: "end_missing_reason", args: []string{"homie", "end", a}, msg: "requires --reason"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				beforeMessages := queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages`)
				beforeOutbox := queryInt(t, db, `SELECT COUNT(*) FROM outbox`)
				got := runMC(t, spineEnv(spine), "", tc.args...)
				if got.code == 0 || !strings.Contains(got.stderr, tc.msg) {
					t.Fatalf("exit = %d stderr %q, want failure containing %q", got.code, got.stderr, tc.msg)
				}
				if queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages`) != beforeMessages ||
					queryInt(t, db, `SELECT COUNT(*) FROM outbox`) != beforeOutbox {
					t.Fatal("invalid Homie message verb mutated state")
				}
			})
		}
		if _, err := db.Exec(`UPDATE homie_sessions SET status = 'ended' WHERE id = ?`, b); err != nil {
			t.Fatal(err)
		}
		endedSend := runMC(t, spineEnv(spine), "", "homie", "send", b,
			"--from", "cli:b", "--body", "implicit resume comes later")
		if endedSend.code != 1 || !strings.Contains(endedSend.stderr, "use resume") {
			t.Fatalf("ended send = code %d stderr %q", endedSend.code, endedSend.stderr)
		}
	})
}

func TestHomieRunnerRegistersSessionLocators(t *testing.T) {
	spine := initSpine(t)
	started := runMC(t, spineEnv(spine), "", "homie", "start", "--from", "dashboard:reg")
	if started.code != 0 {
		t.Fatalf("homie start failed: %s", started.stderr)
	}
	session := started.json["session_id"].(string)
	runner := runJSONEnv(t, spine, session, "homie", "homie")
	db := openDB(t, spine)

	reg := runMC(t, runner, "", "run", "register-session", session,
		"--native-ref", "native-abc", "--file", "trace.jsonl")
	if reg.code != 0 {
		t.Fatalf("homie runner register-session failed (%d): %s", reg.code, reg.stderr)
	}
	locators := func() string {
		return queryStr(t, db, `SELECT COALESCE(native_session_ref, '<null>') || '|' ||
			COALESCE(trace_filename, '<null>') FROM homie_sessions WHERE id = ?`, session)
	}
	if got := locators(); got != "native-abc|trace.jsonl" {
		t.Fatalf("locators = %q", got)
	}

	replay := runMC(t, runner, "", "run", "register-session", session,
		"--native-ref", "native-abc", "--file", "trace.jsonl")
	if replay.code != 0 {
		t.Fatalf("same-value locator retry rejected: %s", replay.stderr)
	}
	conflict := runMC(t, runner, "", "run", "register-session", session,
		"--native-ref", "native-other", "--file", "other.jsonl")
	if conflict.code != 1 || !strings.Contains(conflict.stderr, "immutable") {
		t.Fatalf("conflicting locators = code %d stderr %q", conflict.code, conflict.stderr)
	}
	if got := locators(); got != "native-abc|trace.jsonl" {
		t.Fatalf("conflict mutated locators: %q", got)
	}

	// Cross-session, pipeline-identity, and host attempts are all inert.
	other := runMC(t, spineEnv(spine), "", "homie", "start", "--from", "cli:reg2")
	if other.code != 0 {
		t.Fatalf("second homie start failed: %s", other.stderr)
	}
	otherSession := other.json["session_id"].(string)
	for name, attempt := range map[string]mcResult{
		"cross-session": runMC(t, runner, "", "run", "register-session", otherSession,
			"--native-ref", "n", "--file", "f"),
		"pipeline-identity": runMC(t, runJSONEnv(t, spine, session, "pipeline", "worker"), "",
			"run", "register-session", session, "--native-ref", "n", "--file", "f"),
		"host": runMC(t, spineEnv(spine), "",
			"run", "register-session", otherSession, "--native-ref", "n", "--file", "f"),
	} {
		if attempt.code != 1 {
			t.Fatalf("%s register = %d stderr %q", name, attempt.code, attempt.stderr)
		}
	}
	if got := queryStr(t, db, `SELECT COALESCE(native_session_ref, '<null>')
		FROM homie_sessions WHERE id = ?`, otherSession); got != "<null>" {
		t.Fatalf("forged registration reached %q: %q", otherSession, got)
	}

	// Registration survives session end: identity, not liveness (Inv. 26).
	ended := runMC(t, spineEnv(spine), "", "homie", "end", otherSession, "--reason", "test")
	if ended.code != 0 {
		t.Fatalf("homie end failed: %s", ended.stderr)
	}
	late := runMC(t, runJSONEnv(t, spine, otherSession, "homie", "homie"), "",
		"run", "register-session", otherSession, "--native-ref", "late", "--file", "late.jsonl")
	if late.code != 0 {
		t.Fatalf("post-end locator registration refused: %s", late.stderr)
	}
}

func TestHomieResume(t *testing.T) {
	begin := func(t *testing.T, spine, from string) string {
		t.Helper()
		res := runMC(t, spineEnv(spine), "", "homie", "start", "--from", from)
		if res.code != 0 {
			t.Fatalf("homie start failed: %s", res.stderr)
		}
		return res.json["session_id"].(string)
	}
	register := func(t *testing.T, spine, session string) {
		t.Helper()
		res := runMC(t, runJSONEnv(t, spine, session, "homie", "homie"), "",
			"run", "register-session", session, "--native-ref", "n-"+session, "--file", session+".jsonl")
		if res.code != 0 {
			t.Fatalf("register locators failed: %s", res.stderr)
		}
	}
	end := func(t *testing.T, spine, session string) {
		t.Helper()
		res := runMC(t, spineEnv(spine), "", "homie", "end", session, "--reason", "test drill")
		if res.code != 0 {
			t.Fatalf("homie end failed: %s", res.stderr)
		}
	}

	t.Run("record_only_transition_rebinds_the_requesting_surface", func(t *testing.T) {
		spine := initSpine(t)
		session := begin(t, spine, "dashboard:main")
		register(t, spine, session)
		end(t, spine, session)
		db := openDB(t, spine)

		res := runMC(t, spineEnv(spine), "", "homie", "resume", session, "--from", "discord:ops")
		if res.code != 0 || res.json["resumed"] != true {
			t.Fatalf("resume = code %d json %v stderr %q", res.code, res.json, res.stderr)
		}
		if got := queryStr(t, db, `SELECT status FROM homie_sessions WHERE id = ?`, session); got != "active" {
			t.Fatalf("resumed status = %q", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings
			WHERE session_id = ? AND surface = 'discord' AND channel_ref = 'ops' AND active = 1`, session); n != 1 {
			t.Fatalf("resume bound %d active discord:ops rows", n)
		}
		// Pre-end binding history is not resurrected.
		if n := queryInt(t, db, `SELECT COUNT(*) FROM homie_bindings
			WHERE session_id = ? AND surface = 'dashboard' AND active = 1`, session); n != 0 {
			t.Fatalf("resume reactivated %d historical bindings", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM activity
			WHERE kind = 'homie.resumed' AND subject = ?`, session); n != 1 {
			t.Fatalf("homie.resumed activity rows = %d", n)
		}

		// Crash-after-commit retry is idempotent; any other active-session
		// resume is bind's job.
		retry := runMC(t, spineEnv(spine), "", "homie", "resume", session, "--from", "discord:ops")
		if retry.code != 0 || retry.json["resumed"] != false {
			t.Fatalf("resume retry = code %d json %v stderr %q", retry.code, retry.json, retry.stderr)
		}
		elsewhere := runMC(t, spineEnv(spine), "", "homie", "resume", session, "--from", "cli:new")
		if elsewhere.code != 1 || !strings.Contains(elsewhere.stderr, "use bind") {
			t.Fatalf("active resume elsewhere = code %d stderr %q", elsewhere.code, elsewhere.stderr)
		}
	})

	t.Run("reaped_sessions_are_resumable", func(t *testing.T) {
		spine := initSpine(t)
		session := begin(t, spine, "dashboard:reap")
		register(t, spine, session)
		db := openDB(t, spine)
		if _, err := db.Exec(`UPDATE homie_sessions SET status = 'reaped' WHERE id = ?`, session); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, spineEnv(spine), "", "homie", "resume", session, "--from", "dashboard:reap")
		if res.code != 0 || res.json["resumed"] != true {
			t.Fatalf("reaped resume = code %d json %v stderr %q", res.code, res.json, res.stderr)
		}
	})

	t.Run("native_continue_requires_the_registered_locator_pair", func(t *testing.T) {
		spine := initSpine(t)
		session := begin(t, spine, "cli:bare")
		end(t, spine, session)
		db := openDB(t, spine)
		res := runMC(t, spineEnv(spine), "", "homie", "resume", session, "--from", "cli:bare")
		if res.code != 1 || !strings.Contains(res.stderr, "locator") {
			t.Fatalf("locator-less resume = code %d stderr %q", res.code, res.stderr)
		}
		if got := queryStr(t, db, `SELECT status FROM homie_sessions WHERE id = ?`, session); got != "ended" {
			t.Fatalf("refused resume moved status to %q", got)
		}
	})

	t.Run("occupied_place_aborts_the_whole_transition", func(t *testing.T) {
		spine := initSpine(t)
		first := begin(t, spine, "dashboard:one")
		register(t, spine, first)
		end(t, spine, first)
		second := begin(t, spine, "cli:two")
		_ = second
		db := openDB(t, spine)

		res := runMC(t, spineEnv(spine), "", "homie", "resume", first, "--from", "cli:two")
		if res.code != 1 || !strings.Contains(res.stderr, "already bound") {
			t.Fatalf("occupied resume = code %d stderr %q", res.code, res.stderr)
		}
		if got := queryStr(t, db, `SELECT status FROM homie_sessions WHERE id = ?`, first); got != "ended" {
			t.Fatalf("aborted resume left status %q", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM activity
			WHERE kind = 'homie.resumed' AND subject = ?`, first); n != 0 {
			t.Fatalf("aborted resume left %d activity rows", n)
		}
	})

	t.Run("resume_is_host_scope_only", func(t *testing.T) {
		spine := initSpine(t)
		session := begin(t, spine, "dashboard:scope")
		register(t, spine, session)
		end(t, spine, session)
		db := openDB(t, spine)
		for name, env := range map[string][]string{
			"pipeline":    runJSONEnv(t, spine, "r-x", "pipeline", "worker"),
			"homie-agent": homieJSONEnv(t, spine, session, defaultHomieAllowlist),
		} {
			res := runMC(t, env, "", "homie", "resume", session, "--from", "dashboard:scope")
			if res.code != 1 {
				t.Fatalf("%s resume exit = %d stderr %q", name, res.code, res.stderr)
			}
		}
		if got := queryStr(t, db, `SELECT status FROM homie_sessions WHERE id = ?`, session); got != "ended" {
			t.Fatalf("denied resume moved status to %q", got)
		}
		unknown := runMC(t, spineEnv(spine), "", "homie", "resume", "h-missing", "--from", "cli:x")
		if unknown.code != 1 || !strings.Contains(unknown.stderr, "unknown Homie session") {
			t.Fatalf("unknown resume = code %d stderr %q", unknown.code, unknown.stderr)
		}
	})
}

// The ADR-013 runner transport: fenced idempotent claims of pending inbound
// turns and the atomic reply append + completion + homie_reply fan-out. Both
// are the runner's private own-session scope (§11.5) — never the model's
// frozen operator allowlist, never host, never pipeline.
func TestHomieClaimReply(t *testing.T) {
	start := func(t *testing.T, spine, from string) string {
		t.Helper()
		res := runMC(t, spineEnv(spine), "", "homie", "start", "--from", from)
		if res.code != 0 {
			t.Fatalf("homie start failed: %s", res.stderr)
		}
		return res.json["session_id"].(string)
	}
	send := func(t *testing.T, spine, session, from, body string) int64 {
		t.Helper()
		res := runMC(t, spineEnv(spine), "", "homie", "send", session, "--from", from, "--body", body)
		if res.code != 0 {
			t.Fatalf("homie send failed: %s", res.stderr)
		}
		return int64(res.json["message_id"].(float64))
	}
	// The runner identity is run.json presence alone — deliberately no
	// verb_allowlist: transport must never depend on the model's frozen
	// operator surface.
	runner := func(t *testing.T, spine, session string) []string {
		t.Helper()
		return runJSONEnv(t, spine, session, "homie", "homie")
	}

	t.Run("claim_stamps_and_returns_the_next_pending_turn_idempotently", func(t *testing.T) {
		spine := initSpine(t)
		session := start(t, spine, "dashboard:dash")
		m1 := send(t, spine, session, "cli:term", "first question")
		m2 := send(t, spine, session, "cli:term", "second question")
		db := openDB(t, spine)
		if _, err := db.Exec(`UPDATE homie_sessions SET last_activity_at = datetime('now', '-1 day') WHERE id = ?`, session); err != nil {
			t.Fatal(err)
		}
		beforeActivity := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session)

		claim := runMC(t, runner(t, spine, session), "", "homie", "claim", session)
		if claim.code != 0 {
			t.Fatalf("claim failed (%d): %s", claim.code, claim.stderr)
		}
		msg, ok := claim.json["message"].(map[string]any)
		if !ok {
			t.Fatalf("claim result = %v", claim.json)
		}
		if msg["id"] != float64(m1) || msg["seq"] != float64(1) || msg["body"] != "first question" ||
			msg["surface"] != "cli" || msg["channel_ref"] != "term" || msg["reclaimed"] != false {
			t.Fatalf("claimed message = %v", msg)
		}
		claimState := func(id int64) string {
			return queryStr(t, db, `SELECT COALESCE(claimed_by, '<null>') || '|' ||
				COALESCE(claimed_at, '<null>') FROM conversation_messages WHERE id = ?`, id)
		}
		firstClaim := claimState(m1)
		if !strings.HasPrefix(firstClaim, session+"|") || strings.HasSuffix(firstClaim, "|<null>") {
			t.Fatalf("claim state = %q", firstClaim)
		}
		if got := claimState(m2); got != "<null>|<null>" {
			t.Fatalf("claim touched the queued turn: %q", got)
		}

		// A fresh runner resumes the durable claim: same turn, same stamp.
		reclaim := runMC(t, runner(t, spine, session), "", "homie", "claim", session)
		if reclaim.code != 0 {
			t.Fatalf("reclaim failed: %s", reclaim.stderr)
		}
		remsg := reclaim.json["message"].(map[string]any)
		if remsg["id"] != float64(m1) || remsg["reclaimed"] != true {
			t.Fatalf("reclaimed message = %v", remsg)
		}
		if got := claimState(m1); got != firstClaim {
			t.Fatalf("reclaim rewrote claim state: before %q after %q", firstClaim, got)
		}
		// Claiming is bookkeeping, not conversation traffic.
		if got := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session); got != beforeActivity {
			t.Fatalf("claim advanced last_activity_at: before %q after %q", beforeActivity, got)
		}

		empty := start(t, spine, "cli:empty")
		nothing := runMC(t, runner(t, spine, empty), "", "homie", "claim", empty)
		if nothing.code != 0 || nothing.json["message"] != nil {
			t.Fatalf("empty claim = code %d json %v stderr %q", nothing.code, nothing.json, nothing.stderr)
		}
	})

	t.Run("reply_appends_completes_and_fans_out_to_every_binding", func(t *testing.T) {
		spine := initSpine(t)
		session := start(t, spine, "dashboard:dash")
		if res := runMC(t, spineEnv(spine), "", "homie", "bind", session, "--from", "discord:disc"); res.code != 0 {
			t.Fatalf("bind failed: %s", res.stderr)
		}
		m1 := send(t, spine, session, "cli:term", "please do X")
		if res := runMC(t, runner(t, spine, session), "", "homie", "claim", session); res.code != 0 {
			t.Fatalf("claim failed: %s", res.stderr)
		}

		// Conversation transport is lease-free and coexists with a live
		// leased pipeline run.
		taskAdd(t, spine, "pipeline stays live during reply")
		dispatchExpect(t, spine, "spawn")
		db := openDB(t, spine)
		lockBefore := queryStr(t, db, `SELECT run_id || '|' || owner || '|' || subject FROM lock WHERE id = 1`)
		if _, err := db.Exec(`UPDATE homie_sessions SET last_activity_at = datetime('now', '-1 day') WHERE id = ?`, session); err != nil {
			t.Fatal(err)
		}
		beforeActivity := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session)

		reply := runMC(t, runner(t, spine, session), "", "homie", "reply", session,
			"--to", strconv.FormatInt(m1, 10), "--body", "done: X",
			"--attachments", `["outputs/x.png"]`)
		if reply.code != 0 {
			t.Fatalf("reply failed (%d): %s", reply.code, reply.stderr)
		}
		if reply.json["replied"] != true || reply.json["seq"] != float64(2) ||
			reply.json["reply_to"] != float64(m1) || reply.json["deliveries"] != float64(3) {
			t.Fatalf("reply result = %v", reply.json)
		}
		replyID := int64(reply.json["message_id"].(float64))

		if got := queryStr(t, db, `SELECT direction || '|' || surface || '|' ||
			COALESCE(channel_ref, '<null>') || '|' || body || '|' || attachments || '|' || reply_to
			FROM conversation_messages WHERE id = ?`, replyID); got != `reply|homie|<null>|done: X|["outputs/x.png"]|`+strconv.FormatInt(m1, 10) {
			t.Fatalf("reply row = %q", got)
		}
		if got := queryStr(t, db, `SELECT COALESCE(completed_at, '<null>') FROM conversation_messages WHERE id = ?`, m1); got == "<null>" {
			t.Fatalf("reply did not complete the claimed turn")
		}
		if got := queryStr(t, db, `SELECT group_concat(surface || ':' || channel_ref, ',') FROM
			(SELECT surface, channel_ref FROM outbox WHERE kind = 'homie_reply' ORDER BY surface, channel_ref)`); got != "cli:term,dashboard:dash,discord:disc" {
			t.Fatalf("reply destinations = %q", got)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(queryStr(t, db,
			`SELECT payload FROM outbox WHERE kind = 'homie_reply' ORDER BY id LIMIT 1`)), &payload); err != nil {
			t.Fatal(err)
		}
		attachments, _ := payload["attachments"].([]any)
		if payload["message_id"] != float64(replyID) || payload["seq"] != float64(2) ||
			payload["body"] != "done: X" || payload["reply_to"] != float64(m1) ||
			len(attachments) != 1 || attachments[0] != "outputs/x.png" {
			t.Fatalf("reply payload = %v", payload)
		}
		if got := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session); got == beforeActivity {
			t.Fatalf("reply did not advance last_activity_at from %q", beforeActivity)
		}
		if got := queryStr(t, db, `SELECT run_id || '|' || owner || '|' || subject FROM lock WHERE id = 1`); got != lockBefore {
			t.Fatalf("reply disturbed pipeline lease: before %q after %q", lockBefore, got)
		}

		// The completed turn leaves the pending queue; new traffic re-enters it.
		drained := runMC(t, runner(t, spine, session), "", "homie", "claim", session)
		if drained.code != 0 || drained.json["message"] != nil {
			t.Fatalf("post-reply claim = code %d json %v", drained.code, drained.json)
		}
		m3 := send(t, spine, session, "cli:term", "follow-up")
		next := runMC(t, runner(t, spine, session), "", "homie", "claim", session)
		if next.code != 0 {
			t.Fatalf("follow-up claim failed: %s", next.stderr)
		}
		if msg := next.json["message"].(map[string]any); msg["id"] != float64(m3) || msg["seq"] != float64(3) {
			t.Fatalf("follow-up claim = %v", next.json)
		}
	})

	t.Run("reply_replay_is_idempotent_and_a_second_logical_reply_is_refused", func(t *testing.T) {
		spine := initSpine(t)
		session := start(t, spine, "dashboard:dash")
		m1 := send(t, spine, session, "dashboard:dash", "one")
		if res := runMC(t, runner(t, spine, session), "", "homie", "claim", session); res.code != 0 {
			t.Fatalf("claim failed: %s", res.stderr)
		}
		first := runMC(t, runner(t, spine, session), "", "homie", "reply", session,
			"--to", strconv.FormatInt(m1, 10), "--body", "answer")
		if first.code != 0 {
			t.Fatalf("reply failed: %s", first.stderr)
		}
		db := openDB(t, spine)
		beforeMessages := queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages`)
		beforeOutbox := queryInt(t, db, `SELECT COUNT(*) FROM outbox`)

		// Crash-after-commit retry: same logical reply, nothing re-appended.
		replay := runMC(t, runner(t, spine, session), "", "homie", "reply", session,
			"--to", strconv.FormatInt(m1, 10), "--body", "answer")
		if replay.code != 0 || replay.json["replied"] != false ||
			replay.json["message_id"] != first.json["message_id"] {
			t.Fatalf("reply replay = code %d json %v stderr %q", replay.code, replay.json, replay.stderr)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages`) != beforeMessages ||
			queryInt(t, db, `SELECT COUNT(*) FROM outbox`) != beforeOutbox {
			t.Fatal("idempotent replay appended rows")
		}
		// A different body against a completed turn is a second logical reply.
		second := runMC(t, runner(t, spine, session), "", "homie", "reply", session,
			"--to", strconv.FormatInt(m1, 10), "--body", "different answer")
		if second.code != 1 || !strings.Contains(second.stderr, "one logical reply") {
			t.Fatalf("second reply = code %d stderr %q", second.code, second.stderr)
		}

		// Reply protocol violations are inert.
		m2 := send(t, spine, session, "dashboard:dash", "two")
		other := start(t, spine, "cli:other")
		m3 := send(t, spine, other, "cli:other", "other conversation")
		replyID := int64(first.json["message_id"].(float64))
		for name, tc := range map[string]struct {
			to  int64
			msg string
		}{
			"unclaimed_turn":       {to: m2, msg: "unclaimed"},
			"reply_to_a_reply":     {to: replyID, msg: "inbound turn"},
			"unknown_message":      {to: 9999, msg: "unknown conversation message"},
			"another_conversation": {to: m3, msg: "another conversation"},
		} {
			t.Run(name, func(t *testing.T) {
				res := runMC(t, runner(t, spine, session), "", "homie", "reply", session,
					"--to", strconv.FormatInt(tc.to, 10), "--body", "x")
				if res.code != 1 || !strings.Contains(res.stderr, tc.msg) {
					t.Fatalf("exit = %d stderr %q, want failure containing %q", res.code, res.stderr, tc.msg)
				}
			})
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages WHERE direction = 'reply'`) != 1 {
			t.Fatal("refused replies appended rows")
		}
	})

	t.Run("reply_outbox_failure_rolls_back_the_whole_turn", func(t *testing.T) {
		spine := initSpine(t)
		session := start(t, spine, "dashboard:dash")
		m1 := send(t, spine, session, "dashboard:dash", "roll me back")
		if res := runMC(t, runner(t, spine, session), "", "homie", "claim", session); res.code != 0 {
			t.Fatalf("claim failed: %s", res.stderr)
		}
		db := openDB(t, spine)
		beforeActivity := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session)
		if _, err := db.Exec(`CREATE TRIGGER test_fail_homie_reply
			BEFORE INSERT ON outbox WHEN NEW.kind = 'homie_reply'
			BEGIN SELECT RAISE(ABORT, 'injected reply failure'); END`); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, runner(t, spine, session), "", "homie", "reply", session,
			"--to", strconv.FormatInt(m1, 10), "--body", "must roll back")
		if res.code != 1 || !strings.Contains(res.stderr, "injected reply failure") {
			t.Fatalf("reply atomic failure = code %d stderr %q", res.code, res.stderr)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages WHERE direction = 'reply'`) != 0 ||
			queryInt(t, db, `SELECT COUNT(*) FROM outbox`) != 0 {
			t.Fatal("failed reply left a reply or outbox row")
		}
		if got := queryStr(t, db, `SELECT COALESCE(completed_at, '<null>') FROM conversation_messages WHERE id = ?`, m1); got != "<null>" {
			t.Fatalf("failed reply completed the turn: %q", got)
		}
		if got := queryStr(t, db, `SELECT last_activity_at FROM homie_sessions WHERE id = ?`, session); got != beforeActivity {
			t.Fatalf("failed reply changed last_activity_at: before %q after %q", beforeActivity, got)
		}
	})

	t.Run("claim_and_reply_are_own_session_runner_scope", func(t *testing.T) {
		spine := initSpine(t)
		session := start(t, spine, "dashboard:dash")
		m1 := send(t, spine, session, "dashboard:dash", "fence me")
		db := openDB(t, spine)
		to := strconv.FormatInt(m1, 10)

		for name, env := range map[string][]string{
			"host":          spineEnv(spine),
			"pipeline":      runJSONEnv(t, spine, "r-x", "pipeline", "worker"),
			"cross-session": runner(t, spine, "h-other"),
		} {
			for _, args := range [][]string{
				{"homie", "claim", session},
				{"homie", "reply", session, "--to", to, "--body", "forged"},
			} {
				res := runMC(t, env, "", args...)
				if res.code != 1 {
					t.Fatalf("%s %v exit = %d stderr %q", name, args, res.code, res.stderr)
				}
			}
		}
		if got := queryStr(t, db, `SELECT COALESCE(claimed_by, '<null>') || '|' ||
			COALESCE(completed_at, '<null>') FROM conversation_messages WHERE id = ?`, m1); got != "<null>|<null>" {
			t.Fatalf("forged transport touched the turn: %q", got)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM conversation_messages`) != 1 ||
			queryInt(t, db, `SELECT COUNT(*) FROM outbox`) != 0 {
			t.Fatal("forged transport appended rows")
		}

		// An ended session's runner has no transport left.
		if res := runMC(t, runner(t, spine, session), "", "homie", "claim", session); res.code != 0 {
			t.Fatalf("claim failed: %s", res.stderr)
		}
		if res := runMC(t, spineEnv(spine), "", "homie", "end", session, "--reason", "test"); res.code != 0 {
			t.Fatalf("end failed: %s", res.stderr)
		}
		endedClaim := runMC(t, runner(t, spine, session), "", "homie", "claim", session)
		if endedClaim.code != 1 || !strings.Contains(endedClaim.stderr, "active") {
			t.Fatalf("ended claim = code %d stderr %q", endedClaim.code, endedClaim.stderr)
		}
		endedReply := runMC(t, runner(t, spine, session), "", "homie", "reply", session,
			"--to", to, "--body", "ghost turn")
		if endedReply.code != 1 || !strings.Contains(endedReply.stderr, "active") {
			t.Fatalf("ended reply = code %d stderr %q", endedReply.code, endedReply.stderr)
		}
		if got := queryStr(t, db, `SELECT COALESCE(completed_at, '<null>') FROM conversation_messages WHERE id = ?`, m1); got != "<null>" {
			t.Fatalf("ended-session transport completed the turn: %q", got)
		}
	})
}

// outboxSnapshot captures every outbox column in deterministic id order, so
// "read-only" means the whole table byte-identical, not a two-column proxy.
func outboxSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	return queryStr(t, db, `SELECT COALESCE(group_concat(row, ';'), '<empty>') FROM (
		SELECT id || '|' || kind || '|' || COALESCE(session_id, '<null>') || '|' ||
		       surface || '|' || COALESCE(channel_ref, '<null>') || '|' ||
		       payload || '|' || created_at || '|' ||
		       COALESCE(delivered_at, '<null>') AS row
		FROM outbox ORDER BY id)`)
}

func TestOutboxPollAck(t *testing.T) {
	fixture := func(t *testing.T) (string, *sql.DB) {
		t.Helper()
		spine := initSpine(t)
		started := runMC(t, spineEnv(spine), "", "homie", "start", "--from", "dashboard:dash")
		if started.code != 0 {
			t.Fatalf("homie start failed: %s", started.stderr)
		}
		session := started.json["session_id"].(string)
		if got := runMC(t, spineEnv(spine), "", "homie", "bind", session, "--from", "discord:disc"); got.code != 0 {
			t.Fatalf("homie bind failed: %s", got.stderr)
		}
		if got := runMC(t, spineEnv(spine), "", "homie", "send", session,
			"--from", "cli:origin", "--body", "deliver me"); got.code != 0 {
			t.Fatalf("homie send failed: %s", got.stderr)
		}
		return spine, openDB(t, spine)
	}

	t.Run("poll_is_surface_scoped_stable_and_read_only", func(t *testing.T) {
		spine, db := fixture(t)
		before := outboxSnapshot(t, db)

		dashboard := runMC(t, spineEnv(spine), "", "outbox", "poll", "--surface", "dashboard")
		if dashboard.code != 0 {
			t.Fatalf("dashboard poll failed: %s", dashboard.stderr)
		}
		rows := dashboard.json["rows"].([]any)
		if len(rows) != 1 {
			t.Fatalf("dashboard poll = %v", dashboard.json)
		}
		row := rows[0].(map[string]any)
		if row["surface"] != "dashboard" || row["channel_ref"] != "dash" || row["kind"] != "homie_echo" {
			t.Fatalf("dashboard outbox row = %v", row)
		}
		payload := row["payload"].(map[string]any)
		if payload["body"] != "deliver me" || payload["seq"] != float64(1) {
			t.Fatalf("structured outbox payload = %v", payload)
		}
		discord := runMC(t, spineEnv(spine), "", "outbox", "poll", "--surface", "discord")
		if discord.code != 0 || len(discord.json["rows"].([]any)) != 1 {
			t.Fatalf("discord poll = code %d json %v stderr %q", discord.code, discord.json, discord.stderr)
		}
		if got := outboxSnapshot(t, db); got != before {
			t.Fatalf("poll mutated the outbox: before %q after %q", before, got)
		}

		// Stable id order and limit are part of the delivery cursor contract.
		if _, err := db.Exec(`INSERT INTO outbox (kind, surface, channel_ref, payload)
			VALUES ('health', 'dashboard', 'dash', '{"n":2}'),
			       ('health', 'dashboard', 'dash', '{"n":3}')`); err != nil {
			t.Fatal(err)
		}
		limited := runMC(t, spineEnv(spine), "", "outbox", "poll", "--surface", "dashboard", "--limit", "2")
		if limited.code != 0 {
			t.Fatalf("limited poll failed: %s", limited.stderr)
		}
		limitedRows := limited.json["rows"].([]any)
		if len(limitedRows) != 2 || limitedRows[0].(map[string]any)["id"].(float64) >= limitedRows[1].(map[string]any)["id"].(float64) {
			t.Fatalf("limited poll is not ascending: %v", limitedRows)
		}
	})

	t.Run("ack_only_owns_its_surface_and_replay_is_idempotent", func(t *testing.T) {
		spine, db := fixture(t)
		dashboardID := queryInt(t, db, `SELECT id FROM outbox WHERE surface = 'dashboard'`)
		discordID := queryInt(t, db, `SELECT id FROM outbox WHERE surface = 'discord'`)

		wrong := runMC(t, spineEnv(spine), "", "outbox", "ack", fmt.Sprint(dashboardID), "--surface", "discord")
		if wrong.code != 1 || !strings.Contains(wrong.stderr, "belongs to surface") {
			t.Fatalf("wrong-surface ack = code %d stderr %q", wrong.code, wrong.stderr)
		}
		if got := queryStr(t, db, `SELECT delivered_at FROM outbox WHERE id = ?`, dashboardID); got != "<NULL>" {
			t.Fatalf("wrong-surface ack delivered row: %q", got)
		}

		acked := runMC(t, spineEnv(spine), "", "outbox", "ack", fmt.Sprint(dashboardID), "--surface", "dashboard")
		if acked.code != 0 || acked.json["acked"] != true {
			t.Fatalf("ack = code %d json %v stderr %q", acked.code, acked.json, acked.stderr)
		}
		deliveredAt := acked.json["delivered_at"].(string)
		if deliveredAt == "" || queryStr(t, db, `SELECT delivered_at FROM outbox WHERE id = ?`, dashboardID) != deliveredAt {
			t.Fatalf("ack timestamp mismatch: %v", acked.json)
		}
		if got := runMC(t, spineEnv(spine), "", "outbox", "poll", "--surface", "dashboard"); got.code != 0 || len(got.json["rows"].([]any)) != 0 {
			t.Fatalf("delivered row still polled: code %d json %v", got.code, got.json)
		}
		if got := queryStr(t, db, `SELECT delivered_at FROM outbox WHERE id = ?`, discordID); got != "<NULL>" {
			t.Fatalf("dashboard ack touched discord row: %q", got)
		}

		replay := runMC(t, spineEnv(spine), "", "outbox", "ack", fmt.Sprint(dashboardID), "--surface", "dashboard")
		if replay.code != 0 || replay.json["acked"] != false || replay.json["delivered_at"] != deliveredAt {
			t.Fatalf("ack replay = code %d json %v stderr %q", replay.code, replay.json, replay.stderr)
		}
		missing := runMC(t, spineEnv(spine), "", "outbox", "ack", "99999", "--surface", "dashboard")
		if missing.code != 1 || !strings.Contains(missing.stderr, "unknown outbox row") {
			t.Fatalf("missing ack = code %d stderr %q", missing.code, missing.stderr)
		}
	})

	t.Run("scope_and_usage_fail_before_delivery_mutation", func(t *testing.T) {
		spine, db := fixture(t)
		before := outboxSnapshot(t, db)
		pipeline := runJSONEnv(t, spine, "pipeline", "pipeline", "worker")
		homie := homieJSONEnv(t, spine, "h-forged", defaultHomieAllowlist)
		for name, env := range map[string][]string{"pipeline": pipeline, "homie": homie} {
			t.Run(name, func(t *testing.T) {
				for _, args := range [][]string{
					{"outbox", "poll", "--surface", "dashboard"},
					{"outbox", "ack", "1", "--surface", "dashboard"},
				} {
					got := runMC(t, env, "", args...)
					if got.code != 1 {
						t.Fatalf("%v exit = %d stderr %q", args, got.code, got.stderr)
					}
				}
			})
		}
		for _, tc := range []struct {
			args []string
			msg  string
		}{
			{args: []string{"outbox", "poll"}, msg: "requires --surface"},
			{args: []string{"outbox", "poll", "--surface", "email"}, msg: "discord|dashboard|cli"},
			{args: []string{"outbox", "poll", "--surface", "dashboard", "--limit", "0"}, msg: "limit"},
			{args: []string{"outbox", "ack", "1"}, msg: "requires --surface"},
		} {
			got := runMC(t, spineEnv(spine), "", tc.args...)
			if got.code != 2 || !strings.Contains(got.stderr, tc.msg) {
				t.Fatalf("%v exit = %d stderr %q", tc.args, got.code, got.stderr)
			}
			errObj, ok := got.json["error"].(map[string]any)
			if !ok || errObj["code"] != "usage" {
				t.Fatalf("%v error JSON = %v", tc.args, got.json)
			}
		}
		if got := outboxSnapshot(t, db); got != before {
			t.Fatalf("scope/usage failure mutated the outbox: before %q after %q", before, got)
		}
	})
}

func workerFixture(t *testing.T, title string) (string, int64, string, []string) {
	t.Helper()
	spine := initSpine(t)
	taskID := taskAdd(t, spine, title)

	eff := dispatchExpect(t, spine, "spawn")
	editorRun := eff["run_id"].(string)
	res := runMC(t, runJSONEnv(t, spine, editorRun, "pipeline", "editor"),
		fmt.Sprintf(`{"verdicts":[{"task":%d,"decision":"promote","reason":"ok"}]}`, taskID),
		"editor", "decide", "--run", editorRun, "--batch", "-")
	if res.code != 0 {
		t.Fatalf("promote fixture failed: %s", res.stderr)
	}

	eff = dispatchExpect(t, spine, "spawn")
	workerRun := eff["run_id"].(string)
	return spine, taskID, workerRun,
		runJSONEnv(t, spine, workerRun, "pipeline", "worker")
}

func verifierFixture(t *testing.T, title string) (string, int64, string, []string) {
	t.Helper()
	spine, taskID, workerRun, workerEnv := workerFixture(t, title)
	res := runMC(t, workerEnv, "",
		"complete", fmt.Sprint(taskID), "--run", workerRun, "--status", "worked")
	if res.code != 0 {
		t.Fatalf("worker fixture failed: %s", res.stderr)
	}

	eff := dispatchExpect(t, spine, "spawn")
	verifierRun := eff["run_id"].(string)
	return spine, taskID, verifierRun,
		runJSONEnv(t, spine, verifierRun, "pipeline", "verifier")
}

func TestVerifierVerdictValidation(t *testing.T) {
	t.Run("correct_requires_correction_file", func(t *testing.T) {
		spine, taskID, run, env := verifierFixture(t, "correct needs feedback")
		_ = spine
		res := runMC(t, env, "", "verifier", "verdict", fmt.Sprint(taskID), "--run", run,
			"--outcome", "correct", "--evidence", "e")
		if res.code != 1 || !strings.Contains(res.stderr, "--correction") {
			t.Fatalf("exit = %d stderr %q", res.code, res.stderr)
		}
	})

	t.Run("correct_reenters_and_records_verdict", func(t *testing.T) {
		spine, taskID, run, env := verifierFixture(t, "correct once")
		res := runMC(t, env, "", "verifier", "verdict", fmt.Sprint(taskID), "--run", run,
			"--outcome", "correct", "--evidence", "e.md",
			"--correction", "corrections/c1.md")
		if res.code != 0 {
			t.Fatalf("correct failed: %s", res.stderr)
		}
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT status || '/' || correction_count FROM tasks WHERE id = ?`, taskID); got != "seeded/1" {
			t.Fatalf("correct result = %q", got)
		}
		if got := queryStr(t, db, `SELECT verdict_outcome || '/' || correction_path FROM runs WHERE id = ?`, run); got != "correct/corrections/c1.md" {
			t.Fatalf("verdict record = %q", got)
		}
	})

	for _, tc := range []struct {
		name string
		args []string
		msg  string
	}{
		{name: "pass_forbids_correction", args: []string{"--outcome", "pass", "--evidence", "e", "--sha", "s", "--correction", "c"}, msg: "--correction"},
		{name: "budget_spent_forbids_correction", args: []string{"--outcome", "budget-spent", "--evidence", "e", "--sha", "s", "--correction", "c"}, msg: "--correction"},
		{name: "correct_forbids_sha", args: []string{"--outcome", "correct", "--evidence", "e", "--sha", "s", "--correction", "c"}, msg: "--sha"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spine, taskID, run, env := verifierFixture(t, tc.name)
			args := []string{"verifier", "verdict", fmt.Sprint(taskID), "--run", run}
			args = append(args, tc.args...)
			res := runMC(t, env, "", args...)
			if res.code != 2 || !strings.Contains(res.stderr, tc.msg) {
				t.Fatalf("exit = %d stderr %q, want usage containing %q", res.code, res.stderr, tc.msg)
			}
			db := openDB(t, spine)
			if got := queryStr(t, db, `SELECT status FROM tasks WHERE id = ?`, taskID); got != "worked" {
				t.Fatalf("invalid carrier moved task to %q", got)
			}
			if got := queryStr(t, db, `SELECT ended_at FROM runs WHERE id = ?`, run); got != "<NULL>" {
				t.Fatalf("invalid carrier ended run at %q", got)
			}
			if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != run {
				t.Fatalf("invalid carrier released lease to %q", got)
			}
		})
	}

	t.Run("budget_spent_requires_exhausted_rally", func(t *testing.T) {
		_, taskID, run, env := verifierFixture(t, "budget remains")
		res := runMC(t, env, "", "verifier", "verdict", fmt.Sprint(taskID), "--run", run,
			"--outcome", "budget-spent", "--evidence", "e", "--sha", "s")
		if res.code != 1 || !strings.Contains(res.stderr, "correction_count = 3") {
			t.Fatalf("exit = %d stderr %q", res.code, res.stderr)
		}
	})

	t.Run("budget_spent_ships_exception_labeled", func(t *testing.T) {
		spine, taskID, run, env := verifierFixture(t, "budget exhausted")
		db := openDB(t, spine)
		if _, err := db.Exec(`UPDATE tasks SET correction_count = 3 WHERE id = ?`, taskID); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, env, "", "verifier", "verdict", fmt.Sprint(taskID), "--run", run,
			"--outcome", "budget-spent", "--evidence", "e", "--sha", "verified")
		if res.code != 0 || res.json["exception_labeled"] != true {
			t.Fatalf("budget-spent result code=%d json=%v stderr=%q", res.code, res.json, res.stderr)
		}
		if got := queryStr(t, db, `SELECT status || '/' || verified_sha FROM tasks WHERE id = ?`, taskID); got != "verified/verified" {
			t.Fatalf("budget-spent task = %q", got)
		}
	})

	t.Run("deepening_forbidden_outside_refinement", func(t *testing.T) {
		_, taskID, run, env := verifierFixture(t, "not a refinement")
		res := runMC(t, env, "", "verifier", "verdict", fmt.Sprint(taskID), "--run", run,
			"--outcome", "pass", "--evidence", "e", "--sha", "s", "--deepening", "genuine")
		if res.code != 1 || !strings.Contains(res.stderr, "only legal") {
			t.Fatalf("exit = %d stderr %q", res.code, res.stderr)
		}
	})

	for _, tc := range []struct {
		name string
		args func(taskID int64, run string) []string
		msg  string
	}{
		{"missing_sha_usage", func(id int64, run string) []string {
			return []string{"verifier", "verdict", fmt.Sprint(id), "--run", run, "--outcome", "pass", "--evidence", "e"}
		}, "--sha"},
		{"missing_evidence_usage", func(id int64, run string) []string {
			return []string{"verifier", "verdict", fmt.Sprint(id), "--run", run, "--outcome", "pass", "--sha", "s"}
		}, "--evidence"},
		{"bad_outcome_usage", func(id int64, run string) []string {
			return []string{"verifier", "verdict", fmt.Sprint(id), "--run", run, "--outcome", "maybe", "--evidence", "e", "--sha", "s"}
		}, "--outcome"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, taskID, run, env := verifierFixture(t, tc.name)
			res := runMC(t, env, "", tc.args(taskID, run)...)
			if res.code != 2 || !strings.Contains(res.stderr, tc.msg) {
				t.Fatalf("exit = %d stderr %q, want usage containing %q", res.code, res.stderr, tc.msg)
			}
		})
	}
}

func TestCompletePhase2Terminals(t *testing.T) {
	t.Run("needs_operator_blocks_without_moving_status", func(t *testing.T) {
		spine, taskID, run, env := workerFixture(t, "operator decision")
		res := runMC(t, env, "", "complete", fmt.Sprint(taskID), "--run", run,
			"--needs-operator", "--reason", "choose a deployment target")
		if res.code != 0 {
			t.Fatalf("needs-operator failed: %s", res.stderr)
		}
		if _, has := res.json["action"]; has {
			t.Fatalf("terminal returned dispatch effect: %v", res.json)
		}
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT status || '/' || blocked || '/' || blocked_reason FROM tasks WHERE id = ?`, taskID); got != "seeded/1/choose a deployment target" {
			t.Fatalf("blocked task = %q", got)
		}
		if got := queryStr(t, db, `SELECT outcome FROM runs WHERE id = ?`, run); got != "blocked" {
			t.Fatalf("run outcome = %q", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != "<NULL>" {
			t.Fatalf("terminal did not release lease: %q", got)
		}
	})

	t.Run("infra_charges_only_dispatch_budget", func(t *testing.T) {
		spine, taskID, run, env := workerFixture(t, "infra failure")
		res := runMC(t, env, "", "complete", fmt.Sprint(taskID), "--run", run,
			"--infra", "--reason", "adapter exited")
		if res.code != 0 {
			t.Fatalf("infra terminal failed: %s", res.stderr)
		}
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT status || '/' || dispatch_retries || '/' || correction_count FROM tasks WHERE id = ?`, taskID); got != "seeded/2/0" {
			t.Fatalf("infra-charged task = %q", got)
		}
		if got := queryStr(t, db, `SELECT outcome FROM runs WHERE id = ?`, run); got != "infra-failed" {
			t.Fatalf("run outcome = %q", got)
		}
	})

	t.Run("worked_rejects_noncanonical_standalone_branch_before_terminal", func(t *testing.T) {
		spine, taskID, run, env := workerFixture(t, "branch provenance")
		res := runMC(t, env, "", "complete", fmt.Sprint(taskID), "--run", run,
			"--status", "worked", "--branch", "mc/task-999999")
		if res.code != 1 || !strings.Contains(res.stderr, fmt.Sprintf("mc/task-%d", taskID)) {
			t.Fatalf("noncanonical branch exit=%d stderr=%q", res.code, res.stderr)
		}
		db := openDB(t, spine)
		if got := queryStr(t, db, `SELECT status || '/' || COALESCE(branch, 'null') FROM tasks WHERE id=?`, taskID); got != "seeded/null" {
			t.Fatalf("rejected branch changed task: %q", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id=1`); got != run {
			t.Fatalf("rejected branch released lease: %q", got)
		}
		if got := queryStr(t, db, `SELECT ended_at FROM runs WHERE id=?`, run); got != "<NULL>" {
			t.Fatalf("rejected branch ended Run: %q", got)
		}
	})

	t.Run("initiative_child_requires_the_parent_shared_branch", func(t *testing.T) {
		spine := initSpine(t)
		db := openDB(t, spine)
		parentResult, err := db.Exec(`INSERT INTO tasks
			(title, scope, worksource, target_ref, branch)
			VALUES ('parent', 'initiative', 'ws-test', 'main', 'mc/shared-parent')`)
		if err != nil {
			t.Fatal(err)
		}
		parentID, err := parentResult.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE tasks SET status='seeded' WHERE id=?`, parentID); err != nil {
			t.Fatal(err)
		}
		childResult, err := db.Exec(`INSERT INTO tasks
			(title, scope, status, initiative_id, worksource, target_ref)
			VALUES ('child', 'task', 'seeded', ?, 'ws-test', 'main')`, parentID)
		if err != nil {
			t.Fatal(err)
		}
		childID, err := childResult.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		// A child reaches worked only after the Editor's plan review (ADR-020
		// D1); this case is about the shared-branch rule, so it builds the
		// legal order.
		if _, err := db.Exec(`UPDATE tasks SET plan_reviewed = 1 WHERE id=?`, childID); err != nil {
			t.Fatal(err)
		}
		run := "initiative-child-worker"
		if _, err := db.Exec(`INSERT INTO runs
			(id, tier, role, worksource, subject)
			VALUES (?, 'pipeline', 'worker', 'ws-test', ?)`, run, childID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE lock SET
			run_id=?, worksource='ws-test', subject=?, owner='worker',
			acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+4 hours')
			WHERE id=1`, run, childID); err != nil {
			t.Fatal(err)
		}
		env := runJSONEnv(t, spine, run, "pipeline", "worker")

		wrong := runMC(t, env, "", "complete", fmt.Sprint(childID), "--run", run,
			"--status", "worked", "--branch", fmt.Sprintf("mc/task-%d", childID))
		if wrong.code != 1 || !strings.Contains(wrong.stderr, "shared branch") {
			t.Fatalf("wrong shared branch exit=%d stderr=%q", wrong.code, wrong.stderr)
		}
		if got := queryStr(t, db, `SELECT status || '/' || COALESCE(branch, 'null') FROM tasks WHERE id=?`, childID); got != "seeded/null" {
			t.Fatalf("wrong branch changed child: %q", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id=1`); got != run {
			t.Fatalf("wrong branch released lease: %q", got)
		}

		correct := runMC(t, env, "", "complete", fmt.Sprint(childID), "--run", run,
			"--status", "worked", "--branch", "mc/shared-parent")
		if correct.code != 0 {
			t.Fatalf("assigned shared branch failed: %s", correct.stderr)
		}
		if got := queryStr(t, db, `SELECT status || '/' || branch FROM tasks WHERE id=?`, childID); got != "worked/mc/shared-parent" {
			t.Fatalf("completed child = %q", got)
		}
	})

	t.Run("refiner_reenters_packaged_task_and_keeps_slot", func(t *testing.T) {
		spine := initSpine(t)
		db := openDB(t, spine)
		ids := make([]int64, 0, 3)
		for i := 0; i < 3; i++ {
			id := taskAdd(t, spine, fmt.Sprintf("queued %d", i))
			ids = append(ids, id)
			for _, status := range []string{"seeded", "worked", "verified", "packaged"} {
				if _, err := db.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, status, id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`UPDATE tasks SET priority = ? WHERE id = ?`, i, id); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO review_packets (task_id) VALUES (?)`, id); err != nil {
				t.Fatal(err)
			}
		}
		eff := dispatchExpect(t, spine, "spawn")
		if eff["role"] != "refiner" || eff["subject_id"] != float64(ids[0]) {
			t.Fatalf("at-cap dispatch = %v, want refiner on %d", eff, ids[0])
		}
		run := eff["run_id"].(string)
		env := runJSONEnv(t, spine, run, "pipeline", "refiner")
		res := runMC(t, env, "", "complete", fmt.Sprint(ids[0]), "--run", run,
			"--status", "seeded", "--outputs", "deepen sources and rerun checks")
		if res.code != 0 {
			t.Fatalf("refiner terminal failed: %s", res.stderr)
		}
		if got := queryStr(t, db, `SELECT status || '/' || refine_notes FROM tasks WHERE id = ?`, ids[0]); got != "seeded/deepen sources and rerun checks" {
			t.Fatalf("re-entered task = %q", got)
		}
		if got := queryInt(t, db, `SELECT archived FROM review_packets WHERE task_id = ?`, ids[0]); got != 0 {
			t.Fatalf("refiner freed packet slot: %d", got)
		}
	})

	t.Run("validation_and_correction_count_ownership", func(t *testing.T) {
		_, taskID, run, env := workerFixture(t, "complete validation")
		for _, tc := range []struct {
			name string
			args []string
			want int
			msg  string
		}{
			{"missing_reason", []string{"complete", fmt.Sprint(taskID), "--run", run, "--needs-operator"}, 2, "--reason"},
			{"two_arms", []string{"complete", fmt.Sprint(taskID), "--run", run, "--status", "worked", "--infra", "--reason", "x"}, 2, "exactly one"},
			{"correction_count_owned_by_verdict", []string{"complete", fmt.Sprint(taskID), "--run", run, "--status", "worked", "--correction-count", "1"}, 1, "verifier verdict"},
			{"bad_status", []string{"complete", fmt.Sprint(taskID), "--run", run, "--status", "done"}, 2, "worked|packaged|seeded"},
			{"missing_run", []string{"complete", fmt.Sprint(taskID), "--status", "worked"}, 2, "--run"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				res := runMC(t, env, "", tc.args...)
				if res.code != tc.want || !strings.Contains(res.stderr, tc.msg) {
					t.Fatalf("exit=%d stderr=%q, want %d containing %q", res.code, res.stderr, tc.want, tc.msg)
				}
			})
		}
	})
}

// ---------------------------------------------------------------------------
// Self-delegation (§11.5, contract §1): MC_HELPER set + MC_SPINE unset →
// mc re-invokes itself through `docker exec -i <helper> mc <argv…>`,
// passing argv, stdout, and the exit code through untouched. A stub docker
// on PATH keeps the fast lane Docker-free.
// ---------------------------------------------------------------------------

func TestCompleteRejectsCrossArmFields(t *testing.T) {
	tests := []struct {
		name string
		args []string
		msg  string
	}{
		{name: "packaged_branch", args: []string{"--status", "packaged", "--outputs", "packet.html", "--branch", "mc/x"}, msg: "--branch"},
		{name: "packaged_missing_outputs", args: []string{"--status", "packaged"}, msg: "--outputs"},
		{name: "seeded_branch", args: []string{"--status", "seeded", "--outputs", "scope.md", "--branch", "mc/x"}, msg: "--branch"},
		{name: "seeded_reason", args: []string{"--status", "seeded", "--outputs", "scope.md", "--reason", "ignored"}, msg: "--reason"},
		{name: "worked_reason", args: []string{"--status", "worked", "--reason", "ignored"}, msg: "--reason"},
		{name: "needs_operator_branch", args: []string{"--needs-operator", "--reason", "need", "--branch", "mc/x"}, msg: "--branch"},
		{name: "needs_operator_outputs", args: []string{"--needs-operator", "--reason", "need", "--outputs", "ignored"}, msg: "--outputs"},
		{name: "infra_branch", args: []string{"--infra", "--reason", "boom", "--branch", "mc/x"}, msg: "--branch"},
		{name: "infra_outputs", args: []string{"--infra", "--reason", "boom", "--outputs", "ignored"}, msg: "--outputs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spine, taskID, run, env := workerFixture(t, tc.name)
			args := []string{"complete", fmt.Sprint(taskID), "--run", run}
			args = append(args, tc.args...)
			res := runMC(t, env, "", args...)
			if res.code != 2 || !strings.Contains(res.stderr, tc.msg) {
				t.Fatalf("exit = %d stderr %q, want usage containing %q", res.code, res.stderr, tc.msg)
			}
			db := openDB(t, spine)
			if got := queryStr(t, db, `SELECT status FROM tasks WHERE id = ?`, taskID); got != "seeded" {
				t.Fatalf("invalid field matrix moved task to %q", got)
			}
			if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id = 1`); got != run {
				t.Fatalf("invalid field matrix disturbed lease: %q", got)
			}
		})
	}
}

func TestInitiativeDoneRequiresCompletionReport(t *testing.T) {
	spine := initSpine(t)
	db := openDB(t, spine)
	res, err := db.Exec(`INSERT INTO tasks
		(title, description, scope, worksource, target_ref)
		VALUES ('initiative', 'charter', 'initiative', 'ws-test', 'main')`)
	if err != nil {
		t.Fatal(err)
	}
	initiative, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE tasks SET status='seeded' WHERE id=?`, initiative); err != nil {
		t.Fatal(err)
	}
	run := "initiative-run"
	if _, err := db.Exec(`INSERT INTO runs
		(id, tier, role, worksource, subject)
		VALUES (?, 'pipeline', 'strategist', 'ws-test', ?)`, run, initiative); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET
		run_id=?, worksource='ws-test', subject=?, owner='strategist',
		acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+4 hours')
		WHERE id=1`, run, initiative); err != nil {
		t.Fatal(err)
	}
	env := runJSONEnv(t, spine, run, "pipeline", "strategist(initiative)")
	got := runMC(t, env, "", "complete", fmt.Sprint(initiative),
		"--run", run, "--status", "worked")
	if got.code != 2 || !strings.Contains(got.stderr, "--outputs") {
		t.Fatalf("missing report exit=%d stderr=%q", got.code, got.stderr)
	}
	if status := queryStr(t, db, `SELECT status FROM tasks WHERE id=?`, initiative); status != "seeded" {
		t.Fatalf("missing report moved initiative to %q", status)
	}
	if lockRun := queryStr(t, db, `SELECT run_id FROM lock WHERE id=1`); lockRun != run {
		t.Fatalf("missing report disturbed lease: %q", lockRun)
	}

	got = runMC(t, env, "", "complete", fmt.Sprint(initiative),
		"--run", run, "--status", "worked", "--outputs", "outputs/completion.md")
	if got.code != 0 {
		t.Fatalf("completion report rejected: %s", got.stderr)
	}
	if output := queryStr(t, db, `SELECT output_path FROM runs WHERE id=?`, run); output != "outputs/completion.md" {
		t.Fatalf("completion report path = %q", output)
	}
}

func TestPhase2TerminalTransactionsRollback(t *testing.T) {
	t.Run("strategist_valid_first_invalid_second", func(t *testing.T) {
		spine := initSpine(t)
		eff := dispatchExpect(t, spine, "spawn")
		run := eff["run_id"].(string)
		env := runJSONEnv(t, spine, run, "pipeline", "strategist(propose)")
		batch := `{"proposals":[
			{"worksource":"ws-test","title":"must roll back"},
			{"worksource":"ws-test","title":"invalid priority","priority":99}]}`
		res := runMC(t, env, batch, "strategist", "propose", "--run", run, "--batch", "-")
		if res.code != 1 {
			t.Fatalf("exit = %d stderr=%q, want domain rollback", res.code, res.stderr)
		}
		db := openDB(t, spine)
		if got := queryInt(t, db, `SELECT COUNT(*) FROM tasks`); got != 0 {
			t.Fatalf("half Strategist batch committed %d tasks", got)
		}
		if got := queryStr(t, db, `SELECT ended_at FROM runs WHERE id=?`, run); got != "<NULL>" {
			t.Fatalf("failed Strategist batch ended run at %q", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id=1`); got != run {
			t.Fatalf("failed Strategist batch released lease: %q", got)
		}
	})

	t.Run("editor_reject_first_blocked_promote_second", func(t *testing.T) {
		spine := initSpine(t)
		t1 := taskAdd(t, spine, "reject must roll back")
		t2 := taskAdd(t, spine, "blocked after snapshot")
		eff := dispatchExpect(t, spine, "spawn")
		run := eff["run_id"].(string)
		db := openDB(t, spine)
		if _, err := db.Exec(`UPDATE tasks SET blocked=1, blocked_reason='concurrent hold' WHERE id=?`, t2); err != nil {
			t.Fatal(err)
		}
		batch := fmt.Sprintf(`{"verdicts":[
			{"task":%d,"decision":"reject","reason":"weak"},
			{"task":%d,"decision":"promote","reason":"strong"}]}`, t1, t2)
		res := runMC(t, runJSONEnv(t, spine, run, "pipeline", "editor"), batch,
			"editor", "decide", "--run", run, "--batch", "-")
		if res.code != 1 {
			t.Fatalf("exit = %d stderr=%q, want atomic refusal", res.code, res.stderr)
		}
		if got := queryStr(t, db, `SELECT status || '/' || COALESCE(decision, 'null') || '/' || archived FROM tasks WHERE id=?`, t1); got != "proposed/null/0" {
			t.Fatalf("first Editor verdict half-committed: %q", got)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind='task.rejected' AND subject=?`, t1); got != 0 {
			t.Fatalf("rolled-back rejection left %d activity rows", got)
		}
		if got := queryStr(t, db, `SELECT run_id FROM lock WHERE id=1`); got != run {
			t.Fatalf("failed Editor batch released lease: %q", got)
		}
	})

	t.Run("packager_wip_cap_rolls_back_stage", func(t *testing.T) {
		spine := initSpine(t)
		db := openDB(t, spine)
		for i := 0; i < 3; i++ {
			res, err := db.Exec(`INSERT INTO tasks (title, worksource, target_ref) VALUES (?, 'ws-test', 'main')`, fmt.Sprintf("packet-%d", i))
			if err != nil {
				t.Fatal(err)
			}
			id, _ := res.LastInsertId()
			for _, status := range []string{"seeded", "worked", "verified", "packaged"} {
				if _, err := db.Exec(`UPDATE tasks SET status=? WHERE id=?`, status, id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO review_packets (task_id, render_path) VALUES (?, 'packet.html')`, id); err != nil {
				t.Fatal(err)
			}
		}
		res, err := db.Exec(`INSERT INTO tasks (title, worksource, target_ref) VALUES ('fourth', 'ws-test', 'main')`)
		if err != nil {
			t.Fatal(err)
		}
		taskID, _ := res.LastInsertId()
		for _, status := range []string{"seeded", "worked", "verified"} {
			if _, err := db.Exec(`UPDATE tasks SET status=? WHERE id=?`, status, taskID); err != nil {
				t.Fatal(err)
			}
		}
		run := "packager-cap-run"
		if _, err := db.Exec(`INSERT INTO runs (id, tier, role, worksource, subject) VALUES (?, 'pipeline', 'packager', 'ws-test', ?)`, run, taskID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource='ws-test', subject=?, owner='packager', acquired_at=datetime('now'), hard_deadline_at=datetime('now', '+4 hours') WHERE id=1`, run, taskID); err != nil {
			t.Fatal(err)
		}
		got := runMC(t, runJSONEnv(t, spine, run, "pipeline", "packager"), "",
			"complete", fmt.Sprint(taskID), "--run", run, "--status", "packaged", "--outputs", "fourth.html")
		if got.code != 1 || !strings.Contains(got.stderr, "WIP cap") {
			t.Fatalf("exit=%d stderr=%q, want WIP-cap rollback", got.code, got.stderr)
		}
		if status := queryStr(t, db, `SELECT status FROM tasks WHERE id=?`, taskID); status != "verified" {
			t.Fatalf("failed packet birth left task %q, want verified", status)
		}
		if packets := queryInt(t, db, `SELECT COUNT(*) FROM review_packets WHERE task_id=?`, taskID); packets != 0 {
			t.Fatalf("failed birth left %d packets", packets)
		}
		if lockRun := queryStr(t, db, `SELECT run_id FROM lock WHERE id=1`); lockRun != run {
			t.Fatalf("failed Packager terminal released lease: %q", lockRun)
		}
	})
}

func TestSelfDelegation(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "docker")
	script := "#!/bin/sh\necho \"{\\\"stub\\\":\\\"$*\\\"}\"\nexit 7\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"MC_HELPER=mc-helper-1",
		"PATH=" + stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	res := runMC(t, env, "", "task", "get", "1")
	if res.code != 7 {
		t.Fatalf("exit = %d, want the delegate's 7 passed through", res.code)
	}
	want := "exec -i mc-helper-1 mc task get 1"
	if !strings.Contains(res.stdout, want) {
		t.Fatalf("stdout %q missing delegated argv %q", res.stdout, want)
	}

	t.Run("direct_spine_wins_over_helper", func(t *testing.T) {
		spine := initSpine(t)
		env := append([]string{
			"MC_HELPER=mc-helper-1",
			"PATH=" + stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		}, spineEnv(spine)...)
		res := runMC(t, env, "", "packet", "list")
		if res.code != 0 || strings.Contains(res.stdout, "stub") {
			t.Fatalf("MC_SPINE set must not delegate: code %d, out %q", res.code, res.stdout)
		}
	})
}

// Missing MC_SPINE with no helper is an environment error, not a crash.
func TestMissingSpineIsUsageError(t *testing.T) {
	res := runMC(t, nil, "", "dispatch")
	if res.code != 2 || !strings.Contains(res.stderr, "MC_SPINE") {
		t.Fatalf("exit = %d stderr %q, want 2 naming MC_SPINE", res.code, res.stderr)
	}
}

// The read verbs added for the Docker e2e's assertion channel (contract §7
// ladder steps 3-5 assert lease/runs state; the e2e cannot open the spine
// volume, so mc lock get / mc run list are its only window — §18 `mc
// <record> get/list`).
func TestLockGetAndRunList(t *testing.T) {
	spine := initSpine(t, "--heartbeat-interval-s", "7")

	lock := runMC(t, spineEnv(spine), "", "lock", "get")
	if lock.code != 0 {
		t.Fatalf("lock get failed (%d): %s", lock.code, lock.stderr)
	}
	if lock.json["run_id"] != nil || lock.json["owner"] != nil {
		t.Fatalf("fresh lock must be free: %v", lock.json)
	}
	if got := lock.json["heartbeat_interval_s"].(float64); got != 7 {
		t.Fatalf("lock get heartbeat_interval_s = %v, want the init tunable 7", got)
	}

	runs := runMC(t, spineEnv(spine), "", "run", "list")
	if runs.code != 0 {
		t.Fatalf("run list failed (%d): %s", runs.code, runs.stderr)
	}
	if n := len(runs.json["runs"].([]any)); n != 0 {
		t.Fatalf("fresh spine has %d runs rows, want 0", n)
	}

	// One claim: lock reflects the lease, run list gains the row (Inv. 4).
	id := taskAdd(t, spine, "read-verbs task")
	eff := dispatchExpect(t, spine, "spawn")
	runID := eff["run_id"].(string)

	lock = runMC(t, spineEnv(spine), "", "lock", "get")
	if lock.json["run_id"] != runID || lock.json["owner"] != "editor" {
		t.Fatalf("lock after claim = %v, want run %s owner editor", lock.json, runID)
	}
	if lock.json["last_heartbeat_at"] != nil {
		t.Fatalf("fresh claim must have NULL last_heartbeat_at: %v", lock.json)
	}

	runs = runMC(t, spineEnv(spine), "", "run", "list")
	rows := runs.json["runs"].([]any)
	if len(rows) != 1 {
		t.Fatalf("run list has %d rows, want 1", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["id"] != runID || row["role"] != "editor" || row["ended_at"] != nil {
		t.Fatalf("run row = %v, want live editor run %s", row, runID)
	}
	_ = id
}

// ---------------------------------------------------------------------------
// Operational verbs (wave-2 contract §1.5, ADR-014): doctor / backup / reset
// ---------------------------------------------------------------------------

// backupFiles lists MC_HOME/backups in name order.
func backupFiles(t *testing.T, spine string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(spine), "backups"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		// Ignore WAL/SHM siblings materialized by the test's own snapshot
		// inspection; the snapshot set is the .db files.
		if strings.HasSuffix(e.Name(), "-wal") || strings.HasSuffix(e.Name(), "-shm") {
			continue
		}
		names = append(names, e.Name())
	}
	return names
}

func TestBackup(t *testing.T) {
	t.Run("snapshot_is_consistent_and_source_stays_usable", func(t *testing.T) {
		spine := initSpine(t)
		taskAdd(t, spine, "survives the snapshot")

		res := runMC(t, spineEnv(spine), "", "backup")
		if res.code != 0 {
			t.Fatalf("backup failed (%d): %s", res.code, res.stderr)
		}
		snapshot, _ := res.json["snapshot"].(string)
		if !strings.HasPrefix(snapshot, filepath.Join(filepath.Dir(spine), "backups")+string(filepath.Separator)) {
			t.Fatalf("snapshot path = %q, want under MC_HOME/backups/", snapshot)
		}
		if res.json["bytes"].(float64) <= 0 {
			t.Fatalf("snapshot bytes = %v", res.json["bytes"])
		}
		// The snapshot is a complete, consistent spine: identity and records.
		snapDB, err := substrate.Open(snapshot)
		if err != nil {
			t.Fatalf("open snapshot: %v", err)
		}
		defer snapDB.Close()
		if n := queryInt(t, snapDB, `SELECT COUNT(*) FROM tasks`); n != 1 {
			t.Fatalf("snapshot task rows = %d, want 1", n)
		}
		if got := queryStr(t, snapDB, `SELECT deployment_uuid FROM meta WHERE id = 1`); got == "" {
			t.Fatal("snapshot lost the deployment identity")
		}
		// No .tmp residue and the source keeps accepting writes.
		for _, name := range backupFiles(t, spine) {
			if strings.Contains(name, ".tmp-") {
				t.Fatalf("temp snapshot residue: %q", name)
			}
		}
		taskAdd(t, spine, "post-backup write")

		// A same-second second snapshot lands as its own file.
		again := runMC(t, spineEnv(spine), "", "backup")
		if again.code != 0 || again.json["snapshot"] == snapshot {
			t.Fatalf("second backup = code %d snapshot %v", again.code, again.json["snapshot"])
		}
		if n := len(backupFiles(t, spine)); n != 2 {
			t.Fatalf("backups dir has %d files, want 2", n)
		}
	})

	t.Run("backup_is_host_scope_and_refuses_before_touching_anything", func(t *testing.T) {
		spine := initSpine(t)
		for name, env := range map[string][]string{
			"pipeline": runJSONEnv(t, spine, "r-x", "pipeline", "worker"),
			"homie":    homieJSONEnv(t, spine, "h-x", defaultHomieAllowlist),
		} {
			res := runMC(t, env, "", "backup")
			if res.code != 1 {
				t.Fatalf("%s backup exit = %d stderr %q", name, res.code, res.stderr)
			}
		}
		if n := len(backupFiles(t, spine)); n != 0 {
			t.Fatalf("denied backup wrote %d snapshot files", n)
		}
		missing := runMC(t, []string{"MC_SPINE=" + filepath.Join(t.TempDir(), "absent.db"), "MC_HOME=" + t.TempDir()}, "", "backup")
		if missing.code != 1 || !strings.Contains(missing.stderr, "no spine") {
			t.Fatalf("missing-spine backup = code %d stderr %q", missing.code, missing.stderr)
		}
	})
}

func TestDoctor(t *testing.T) {
	findingByCheck := func(t *testing.T, res mcResult) map[string]map[string]any {
		t.Helper()
		out := map[string]map[string]any{}
		findings, ok := res.json["findings"].([]any)
		if !ok {
			t.Fatalf("doctor result = %v", res.json)
		}
		for _, raw := range findings {
			f := raw.(map[string]any)
			check, _ := f["check"].(string)
			status, _ := f["status"].(string)
			section, _ := f["onboard_section"].(string)
			if check == "" || status == "" || section == "" {
				t.Fatalf("finding missing check/status/onboard_section: %v", f)
			}
			out[check] = f
		}
		return out
	}
	prepareIdentityAndSurfaces := func(t *testing.T, spine string) string {
		t.Helper()
		db := openDB(t, spine)
		uuid := queryStr(t, db, `SELECT deployment_uuid FROM meta WHERE id = 1`)
		if _, err := db.Exec(`UPDATE lock SET console_hour = 8, console_minute = 0, console_tz = 'UTC' WHERE id = 1`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		db.Close()
		if err := os.WriteFile(filepath.Join(filepath.Dir(spine), "deployment.uuid"), []byte(uuid+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return uuid
	}

	t.Run("healthy_deployment_reports_ok_and_defers_phase3_probes", func(t *testing.T) {
		spine := initSpine(t)
		prepareIdentityAndSurfaces(t, spine)
		res := runMC(t, spineEnv(spine), "", "doctor")
		if res.code != 0 || res.json["ok"] != true {
			t.Fatalf("doctor = code %d json %v stderr %q", res.code, res.json, res.stderr)
		}
		findings := findingByCheck(t, res)
		for check, wantStatus := range map[string]string{
			"mc-home": "ok", "spine": "ok", "deployment-identity": "ok",
			"routing": "ok", "worksources": "ok", "surfaces": "ok",
			"container-runtime": "deferred", "gateway": "deferred",
			"runtime-auth": "deferred", "supervision": "deferred",
		} {
			f, present := findings[check]
			if !present || f["status"] != wantStatus {
				t.Fatalf("finding %q = %v, want status %q", check, f, wantStatus)
			}
		}
		if findings["routing"]["onboard_section"] != "routing" ||
			findings["spine"]["onboard_section"] != "home" ||
			findings["runtime-auth"]["onboard_section"] != "runtime-auth" {
			t.Fatalf("repairing sections = %v", findings)
		}
	})

	t.Run("failures_name_their_repairing_section_without_mutation", func(t *testing.T) {
		spine := initSpine(t)
		prepareIdentityAndSurfaces(t, spine)
		taskAdd(t, spine, "doctor must not touch me")
		db := openDB(t, spine)
		if err := os.WriteFile(filepath.Join(filepath.Dir(spine), "routing.md"),
			[]byte("| role | harness |\n|---|---|\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, spineEnv(spine), "", "doctor")
		if res.code != 0 || res.json["ok"] != false {
			t.Fatalf("doctor = code %d json %v", res.code, res.json)
		}
		findings := findingByCheck(t, res)
		if findings["routing"]["status"] != "fail" || findings["routing"]["onboard_section"] != "routing" {
			t.Fatalf("routing finding = %v", findings["routing"])
		}
		if findings["spine"]["status"] != "ok" {
			t.Fatalf("spine finding = %v", findings["spine"])
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM tasks`); n != 1 {
			t.Fatalf("doctor mutated tasks: %d rows", n)
		}
	})

	t.Run("identity_mismatch_and_disabled_console_are_failures", func(t *testing.T) {
		spine := initSpine(t)
		uuid := prepareIdentityAndSurfaces(t, spine)
		mirror := filepath.Join(filepath.Dir(spine), "deployment.uuid")
		if err := os.WriteFile(mirror, []byte("wrong\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mismatch := runMC(t, spineEnv(spine), "", "doctor")
		findings := findingByCheck(t, mismatch)
		if mismatch.code != 0 || mismatch.json["ok"] != false || findings["deployment-identity"]["status"] != "fail" {
			t.Fatalf("identity mismatch doctor = code %d json %v", mismatch.code, mismatch.json)
		}
		if err := os.WriteFile(mirror, []byte(uuid+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		db := openDB(t, spine)
		if _, err := db.Exec(`UPDATE lock SET console_hour = 24, console_minute = 0, console_tz = 'UTC' WHERE id = 1`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		db.Close()
		disabled := runMC(t, spineEnv(spine), "", "doctor")
		findings = findingByCheck(t, disabled)
		if disabled.code != 0 || disabled.json["ok"] != false || findings["surfaces"]["status"] != "fail" || findings["surfaces"]["onboard_section"] != "surfaces" {
			t.Fatalf("disabled Console doctor = code %d json %v", disabled.code, disabled.json)
		}
	})

	t.Run("missing_spine_reports_loss_without_creating_bytes", func(t *testing.T) {
		home := t.TempDir()
		absent := filepath.Join(home, "spine.db")
		if err := os.WriteFile(filepath.Join(home, "routing.md"), []byte(fakeRoutingMarkdown), 0o600); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, []string{"MC_SPINE=" + absent, "MC_HOME=" + home}, "", "doctor")
		if res.code != 0 || res.json["ok"] != false {
			t.Fatalf("doctor = code %d json %v stderr %q", res.code, res.json, res.stderr)
		}
		findings := findingByCheck(t, res)
		if findings["spine"]["status"] != "fail" ||
			!strings.Contains(findings["spine"]["detail"].(string), "restore from backup") {
			t.Fatalf("spine loss finding = %v", findings["spine"])
		}
		if _, err := os.Stat(absent); !os.IsNotExist(err) {
			t.Fatalf("doctor created spine bytes at %q", absent)
		}
	})

	t.Run("doctor_is_host_scope", func(t *testing.T) {
		spine := initSpine(t)
		for name, env := range map[string][]string{
			"pipeline": runJSONEnv(t, spine, "r-x", "pipeline", "worker"),
			"homie":    homieJSONEnv(t, spine, "h-x", defaultHomieAllowlist),
		} {
			res := runMC(t, env, "", "doctor")
			if res.code != 1 {
				t.Fatalf("%s doctor exit = %d stderr %q", name, res.code, res.stderr)
			}
		}
	})
}

func TestReset(t *testing.T) {
	t.Run("refuses_without_confirmation_and_takes_no_snapshot", func(t *testing.T) {
		spine := initSpine(t)
		taskAdd(t, spine, "still here")
		res := runMC(t, spineEnv(spine), "", "reset")
		if res.code != 1 || !strings.Contains(res.stderr, "--confirm") {
			t.Fatalf("unconfirmed reset = code %d stderr %q", res.code, res.stderr)
		}
		if _, err := os.Stat(spine); err != nil {
			t.Fatalf("unconfirmed reset touched the spine: %v", err)
		}
		if n := len(backupFiles(t, spine)); n != 0 {
			t.Fatalf("unconfirmed reset wrote %d snapshots", n)
		}
	})

	t.Run("confirmed_reset_snapshots_first_then_deletes_the_spine", func(t *testing.T) {
		spine := initSpine(t)
		taskAdd(t, spine, "must survive in the snapshot")
		res := runMC(t, spineEnv(spine), "", "reset", "--confirm")
		if res.code != 0 || res.json["reset"] != true {
			t.Fatalf("reset = code %d json %v stderr %q", res.code, res.json, res.stderr)
		}
		// Output carries only paths and the flag — never config/secret values.
		for key := range res.json {
			switch key {
			case "spine", "snapshot", "reset":
			default:
				t.Fatalf("unexpected reset output key %q in %v", key, res.json)
			}
		}
		snapshot := res.json["snapshot"].(string)
		snapDB, err := substrate.Open(snapshot)
		if err != nil {
			t.Fatalf("open pre-reset snapshot: %v", err)
		}
		defer snapDB.Close()
		if n := queryInt(t, snapDB, `SELECT COUNT(*) FROM tasks`); n != 1 {
			t.Fatalf("snapshot task rows = %d, want 1", n)
		}
		for _, residue := range []string{spine, spine + "-wal", spine + "-shm"} {
			if _, err := os.Stat(residue); !os.IsNotExist(err) {
				t.Fatalf("reset left %q behind (err %v)", residue, err)
			}
		}
	})

	t.Run("failed_snapshot_aborts_the_reset", func(t *testing.T) {
		spine := initSpine(t)
		// A file where the backups directory must go makes the snapshot fail.
		if err := os.WriteFile(filepath.Join(filepath.Dir(spine), "backups"), []byte("in the way"), 0o600); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, spineEnv(spine), "", "reset", "--confirm")
		if res.code != 1 {
			t.Fatalf("snapshot-blocked reset = code %d stderr %q", res.code, res.stderr)
		}
		if _, err := os.Stat(spine); err != nil {
			t.Fatalf("aborted reset deleted the spine: %v", err)
		}
	})

	t.Run("reset_is_host_scope", func(t *testing.T) {
		spine := initSpine(t)
		for name, env := range map[string][]string{
			"pipeline": runJSONEnv(t, spine, "r-x", "pipeline", "worker"),
			"homie":    homieJSONEnv(t, spine, "h-x", defaultHomieAllowlist),
		} {
			res := runMC(t, env, "", "reset", "--confirm")
			if res.code != 1 {
				t.Fatalf("%s reset exit = %d stderr %q", name, res.code, res.stderr)
			}
		}
		if _, err := os.Stat(spine); err != nil {
			t.Fatalf("denied reset touched the spine: %v", err)
		}
	})
}

// mc onboard (§17, ADR-015): the section dispatcher at the Phase-2 tier —
// named sections only, resumable/idempotent, §16.4 spine identity rules,
// deferred doubles for the Docker/host-effect sections, no launchd ever.
func TestOnboard(t *testing.T) {
	freshHome := func(t *testing.T) (string, string, []string) {
		t.Helper()
		home := filepath.Join(t.TempDir(), "mc-home")
		spine := filepath.Join(home, "spine.db")
		return home, spine, []string{"MC_HOME=" + home, "MC_SPINE=" + spine}
	}
	sectionStatus := func(t *testing.T, res mcResult) map[string]map[string]any {
		t.Helper()
		out := map[string]map[string]any{}
		sections, ok := res.json["sections"].([]any)
		if !ok {
			t.Fatalf("onboard result = %v", res.json)
		}
		for _, raw := range sections {
			s := raw.(map[string]any)
			out[s["section"].(string)] = s
		}
		return out
	}

	t.Run("named_sections_only_and_host_scope", func(t *testing.T) {
		home, _, env := freshHome(t)
		res := runMC(t, env, "", "onboard", "bogus-section")
		if res.code != 2 || !strings.Contains(res.stderr, "preflight|home|runtime-auth|routing|container|worksource|tunables|surfaces|supervision|verify") {
			t.Fatalf("unknown section = code %d stderr %q", res.code, res.stderr)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("refused onboard created MC_HOME (err %v)", err)
		}
		for name, identityEnv := range map[string]func(string) []string{
			"pipeline": func(spine string) []string {
				return runJSONEnv(t, spine, "r-x", "pipeline", "worker")
			},
			"homie": func(spine string) []string {
				return homieJSONEnv(t, spine, "h-x", defaultHomieAllowlist)
			},
		} {
			deniedHome, deniedSpine, _ := freshHome(t)
			envDenied := identityEnv(deniedSpine)
			envDenied = append(envDenied, "MC_HOME="+deniedHome)
			res := runMC(t, envDenied, "", "onboard", "home")
			if res.code != 1 {
				t.Fatalf("%s onboard exit = %d stderr %q", name, res.code, res.stderr)
			}
			if _, err := os.Stat(deniedHome); !os.IsNotExist(err) {
				t.Fatalf("%s denial created MC_HOME: %v", name, err)
			}
			if _, err := os.Stat(deniedSpine); !os.IsNotExist(err) {
				t.Fatalf("%s denial created the spine: %v", name, err)
			}
		}
		smoke := runMC(t, env, "", "onboard", "--smoke")
		if smoke.code != 1 || !strings.Contains(smoke.stderr, "Phase") {
			t.Fatalf("smoke = code %d stderr %q", smoke.code, smoke.stderr)
		}
		bogusSmoke := runMC(t, env, "", "onboard", "bogus-section", "--smoke")
		if bogusSmoke.code != 2 || !strings.Contains(bogusSmoke.stderr, "unknown onboarding section") {
			t.Fatalf("bogus smoke section = code %d stderr %q", bogusSmoke.code, bogusSmoke.stderr)
		}
	})

	t.Run("home_provisions_the_deployment_idempotently", func(t *testing.T) {
		home, spine, env := freshHome(t)
		res := runMC(t, env, "", "onboard", "home")
		if res.code != 0 {
			t.Fatalf("onboard home failed (%d): %s", res.code, res.stderr)
		}
		if sectionStatus(t, res)["home"]["status"] != "done" {
			t.Fatalf("fresh home = %v", res.json)
		}
		for _, dir := range []string{"backups", "sessions", "outputs", "attachments", "workflows"} {
			if st, err := os.Stat(filepath.Join(home, dir)); err != nil || !st.IsDir() {
				t.Fatalf("home scaffold missing %s/: %v", dir, err)
			}
		}
		db, err := substrate.Open(spine)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		uuid := queryStr(t, db, `SELECT deployment_uuid FROM meta WHERE id = 1`)
		if uuid == "" {
			t.Fatal("home did not seed the meta identity")
		}
		mirror := filepath.Join(home, "deployment.uuid")
		mirrored, err := os.ReadFile(mirror)
		if err != nil || strings.TrimSpace(string(mirrored)) != uuid {
			t.Fatalf("deployment UUID mirror = %q, err %v; want %q", mirrored, err, uuid)
		}
		// Re-running skips; the deployment identity never changes (§16.4).
		again := runMC(t, env, "", "onboard", "home")
		if again.code != 0 || sectionStatus(t, again)["home"]["status"] != "ok" {
			t.Fatalf("re-run home = code %d json %v", again.code, again.json)
		}
		if got := queryStr(t, db, `SELECT deployment_uuid FROM meta WHERE id = 1`); got != uuid {
			t.Fatalf("re-run changed deployment identity %q -> %q", uuid, got)
		}
		after, err := os.ReadFile(mirror)
		if err != nil || string(after) != string(mirrored) {
			t.Fatalf("re-run changed UUID mirror from %q to %q (err %v)", mirrored, after, err)
		}
		if err := os.WriteFile(mirror, []byte("different-deployment\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mismatch := runMC(t, env, "", "onboard", "home")
		if mismatch.code != 1 || !strings.Contains(mismatch.stderr, "identity mismatch") {
			t.Fatalf("UUID mismatch = code %d stderr %q", mismatch.code, mismatch.stderr)
		}
	})

	t.Run("home_adoption_and_scaffold_repairs_report_done", func(t *testing.T) {
		spine := initSpine(t)
		home := filepath.Dir(spine)
		env := spineEnv(spine)
		adopt := runMC(t, env, "", "onboard", "home")
		if adopt.code != 0 || sectionStatus(t, adopt)["home"]["status"] != "done" {
			t.Fatalf("pre-mirror adoption = code %d json %v stderr %q", adopt.code, adopt.json, adopt.stderr)
		}
		if err := os.RemoveAll(filepath.Join(home, "sessions")); err != nil {
			t.Fatal(err)
		}
		repair := runMC(t, env, "", "onboard", "home")
		if repair.code != 0 || sectionStatus(t, repair)["home"]["status"] != "done" {
			t.Fatalf("scaffold repair = code %d json %v stderr %q", repair.code, repair.json, repair.stderr)
		}
	})

	t.Run("home_never_reinitializes_a_nonempty_spine", func(t *testing.T) {
		root := t.TempDir()
		home := filepath.Join(root, "mc-home")
		spine := filepath.Join(root, "foreign.db")
		env := []string{"MC_HOME=" + home, "MC_SPINE=" + spine}
		raw, err := sql.Open("sqlite", "file:"+spine)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(`CREATE TABLE stranger (x); INSERT INTO stranger VALUES (42)`); err != nil {
			t.Fatal(err)
		}
		raw.Close()
		before, err := os.ReadFile(spine)
		if err != nil {
			t.Fatal(err)
		}
		res := runMC(t, env, "", "onboard", "home")
		if res.code != 1 || !strings.Contains(res.stderr, "restore from backup") {
			t.Fatalf("nonempty-spine home = code %d stderr %q", res.code, res.stderr)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("meta refusal scaffolded MC_HOME: %v", err)
		}
		after, err := os.ReadFile(spine)
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("meta inspection mutated foreign spine (err %v)", err)
		}
		check, err := sql.Open("sqlite", "file:"+spine+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer check.Close()
		if n := queryInt(t, check, `SELECT COUNT(*) FROM stranger`); n != 1 {
			t.Fatalf("onboard touched the foreign spine: %d rows", n)
		}
		if mode := queryStr(t, check, `PRAGMA journal_mode`); mode != "delete" {
			t.Fatalf("read-only inspection changed journal mode to %q", mode)
		}
	})

	t.Run("home_detects_a_lost_spine_from_its_uuid_mirror", func(t *testing.T) {
		_, spine, env := freshHome(t)
		if res := runMC(t, env, "", "onboard", "home"); res.code != 0 {
			t.Fatalf("home failed: %s", res.stderr)
		}
		for _, path := range []string{spine, spine + "-wal", spine + "-shm"} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		lost := runMC(t, env, "", "onboard", "home")
		if lost.code != 1 || !strings.Contains(lost.stderr, "spine lost") || !strings.Contains(lost.stderr, "restore from backup") {
			t.Fatalf("lost spine = code %d stderr %q", lost.code, lost.stderr)
		}
		if _, err := os.Stat(spine); !os.IsNotExist(err) {
			t.Fatalf("loss path recreated the spine: %v", err)
		}
	})

	t.Run("home_refuses_a_nonempty_zero_table_sqlite_file", func(t *testing.T) {
		root := t.TempDir()
		home := filepath.Join(root, "mc-home")
		spine := filepath.Join(root, "zero-table.db")
		db, err := sql.Open("sqlite", "file:"+spine)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`PRAGMA user_version = 7`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		db.Close()
		before, err := os.ReadFile(spine)
		if err != nil || len(before) == 0 {
			t.Fatalf("zero-table fixture = %d bytes, err %v", len(before), err)
		}
		res := runMC(t, []string{"MC_HOME=" + home, "MC_SPINE=" + spine}, "", "onboard", "home")
		if res.code != 1 || !strings.Contains(res.stderr, "non-empty") || !strings.Contains(res.stderr, "restore from backup") {
			t.Fatalf("zero-table spine = code %d stderr %q", res.code, res.stderr)
		}
		after, err := os.ReadFile(spine)
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("zero-table refusal mutated spine: %v", err)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("zero-table refusal scaffolded home: %v", err)
		}
	})

	t.Run("preflight_fences_git_working_trees", func(t *testing.T) {
		repo := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		home := filepath.Join(repo, "deep", "mc-home")
		res := runMC(t, []string{"MC_HOME=" + home, "MC_SPINE=" + filepath.Join(home, "s.db")}, "", "onboard", "preflight")
		if res.code != 1 || !strings.Contains(res.stderr, "git working tree") {
			t.Fatalf("git-tree preflight = code %d stderr %q", res.code, res.stderr)
		}
		directHome := runMC(t, []string{"MC_HOME=" + home, "MC_SPINE=" + filepath.Join(home, "s.db")}, "", "onboard", "home")
		if directHome.code != 1 || !strings.Contains(directHome.stderr, "git working tree") {
			t.Fatalf("named home bypassed git fence = code %d stderr %q", directHome.code, directHome.stderr)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("denied named home wrote MC_HOME: %v", err)
		}
		ignoredRepo := filepath.Join(t.TempDir(), "ignored-repo")
		if out, err := exec.Command("git", "init", "-q", ignoredRepo).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, out)
		}
		if err := os.WriteFile(filepath.Join(ignoredRepo, ".gitignore"), []byte(".mc/\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		ignoredHome := filepath.Join(ignoredRepo, ".mc")
		ignored := runMC(t, []string{"MC_HOME=" + ignoredHome, "MC_SPINE=" + filepath.Join(ignoredHome, "s.db")}, "", "onboard", "preflight")
		if ignored.code != 0 || sectionStatus(t, ignored)["preflight"]["status"] != "ok" {
			t.Fatalf("ignored in-tree home = code %d stderr %q json %v", ignored.code, ignored.stderr, ignored.json)
		}
		if _, err := os.Stat(ignoredHome); !os.IsNotExist(err) {
			t.Fatalf("preflight wrote ignored MC_HOME: %v", err)
		}

		targetRepo := filepath.Join(t.TempDir(), "target-repo")
		if out, err := exec.Command("git", "init", "-q", targetRepo).CombinedOutput(); err != nil {
			t.Fatalf("git init target: %v: %s", err, out)
		}
		aliasRoot := filepath.Join(t.TempDir(), "repo-alias")
		if err := os.Symlink(targetRepo, aliasRoot); err != nil {
			t.Fatal(err)
		}
		aliasedHome := filepath.Join(aliasRoot, "operator-state")
		aliased := runMC(t, []string{"MC_HOME=" + aliasedHome, "MC_SPINE=" + filepath.Join(aliasedHome, "s.db")}, "", "onboard", "preflight")
		if aliased.code != 1 || !strings.Contains(aliased.stderr, "git working tree") {
			t.Fatalf("symlinked git-tree home = code %d stderr %q", aliased.code, aliased.stderr)
		}
		outside, _, env := freshHome(t)
		ok := runMC(t, env, "", "onboard", "preflight")
		if ok.code != 0 || sectionStatus(t, ok)["preflight"]["status"] != "ok" {
			t.Fatalf("clean preflight = code %d json %v stderr %q", ok.code, ok.json, ok.stderr)
		}
		_ = outside
	})

	t.Run("routing_writes_a_valid_default_once_and_validates_existing", func(t *testing.T) {
		home, _, env := freshHome(t)
		tooEarly := runMC(t, env, "", "onboard", "routing")
		if tooEarly.code != 1 || !strings.Contains(tooEarly.stderr, "onboard home") {
			t.Fatalf("routing before home = code %d stderr %q", tooEarly.code, tooEarly.stderr)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("routing before identity created MC_HOME: %v", err)
		}
		if homeRes := runMC(t, env, "", "onboard", "home"); homeRes.code != 0 {
			t.Fatalf("home failed: %s", homeRes.stderr)
		}
		path := filepath.Join(home, "routing.md")
		outside := filepath.Join(t.TempDir(), "outside-routing.md")
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
		redirected := runMC(t, env, "", "onboard", "routing")
		if redirected.code != 1 || !strings.Contains(redirected.stderr, "symlink") {
			t.Fatalf("routing symlink = code %d stderr %q", redirected.code, redirected.stderr)
		}
		if _, err := os.Stat(outside); !os.IsNotExist(err) {
			t.Fatalf("routing symlink wrote outside MC_HOME: %v", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, env, "", "onboard", "routing")
		if res.code != 0 || sectionStatus(t, res)["routing"]["status"] != "done" {
			t.Fatalf("fresh routing = code %d json %v stderr %q", res.code, res.json, res.stderr)
		}
		written, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		again := runMC(t, env, "", "onboard", "routing")
		if again.code != 0 || sectionStatus(t, again)["routing"]["status"] != "ok" {
			t.Fatalf("re-run routing = code %d json %v", again.code, again.json)
		}
		unchanged, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(unchanged) != string(written) {
			t.Fatal("re-run rewrote routing.md")
		}
		// An existing invalid table fails the section and is never repaired
		// silently (§17: an invalid routing.md can never come out of onboarding).
		if err := os.WriteFile(path, []byte("| role | harness |\n|---|---|\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		invalid := runMC(t, env, "", "onboard", "routing")
		if invalid.code != 1 || !strings.Contains(invalid.stderr, "routing") {
			t.Fatalf("invalid routing = code %d stderr %q", invalid.code, invalid.stderr)
		}
		kept, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(kept) != "| role | harness |\n|---|---|\n" {
			t.Fatal("failed section rewrote the operator's routing.md")
		}
	})

	t.Run("worksource_is_dual_input_and_skips_once_seeded", func(t *testing.T) {
		_, spine, env := freshHome(t)
		if res := runMC(t, env, "", "onboard", "home"); res.code != 0 {
			t.Fatalf("home failed: %s", res.stderr)
		}
		missing := runMC(t, env, "", "onboard", "worksource")
		if missing.code != 1 || !strings.Contains(missing.stderr, "--worksource") {
			t.Fatalf("inputless worksource = code %d stderr %q", missing.code, missing.stderr)
		}
		relative := runMC(t, env, "", "onboard", "worksource",
			"--worksource", "ws-main", "--workspace-root", "relative/path")
		if relative.code != 2 {
			t.Fatalf("relative workspace = code %d stderr %q", relative.code, relative.stderr)
		}
		workspace := filepath.Join(t.TempDir(), "ws-main")
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			t.Fatal(err)
		}
		canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
		if err != nil {
			t.Fatal(err)
		}
		seeded := runMC(t, env, "", "onboard", "worksource",
			"--worksource", "ws-main", "--workspace-root", workspace)
		if seeded.code != 0 || sectionStatus(t, seeded)["worksource"]["status"] != "done" {
			t.Fatalf("seeding worksource = code %d json %v stderr %q", seeded.code, seeded.json, seeded.stderr)
		}
		db, err := substrate.Open(spine)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if got := queryStr(t, db, `SELECT sandbox_profile FROM worksources WHERE id = 'ws-main'`); got != "default" {
			t.Fatalf("worksource row = %q", got)
		}
		if got := queryStr(t, db, `SELECT workspace_root FROM sandbox_profiles WHERE id = 'default'`); got != canonicalWorkspace {
			t.Fatalf("canonical workspace root = %q, want %q", got, canonicalWorkspace)
		}
		exact := runMC(t, env, "", "onboard", "worksource",
			"--worksource", "ws-main", "--workspace-root", workspace)
		if exact.code != 0 || sectionStatus(t, exact)["worksource"]["status"] != "ok" {
			t.Fatalf("exact Worksource replay = code %d json %v stderr %q", exact.code, exact.json, exact.stderr)
		}
		otherWorkspace := filepath.Join(t.TempDir(), "other")
		if err := os.MkdirAll(otherWorkspace, 0o700); err != nil {
			t.Fatal(err)
		}
		moved := runMC(t, env, "", "onboard", "worksource",
			"--worksource", "ws-main", "--workspace-root", otherWorkspace)
		if moved.code != 1 || !strings.Contains(moved.stderr, "already") {
			t.Fatalf("moved Worksource replay = code %d stderr %q", moved.code, moved.stderr)
		}
		second := runMC(t, env, "", "onboard", "worksource",
			"--worksource", "ws-other", "--workspace-root", otherWorkspace)
		if second.code != 1 || !strings.Contains(second.stderr, "worksource add") {
			t.Fatalf("second first-Worksource replay = code %d stderr %q", second.code, second.stderr)
		}
		skip := runMC(t, env, "", "onboard", "worksource")
		if skip.code != 0 || sectionStatus(t, skip)["worksource"]["status"] != "ok" {
			t.Fatalf("seeded re-run = code %d json %v", skip.code, skip.json)
		}
		if err := os.RemoveAll(workspace); err != nil {
			t.Fatal(err)
		}
		missingRoot := runMC(t, env, "", "onboard", "worksource")
		if missingRoot.code != 1 || !strings.Contains(missingRoot.stderr, "workspace root") {
			t.Fatalf("missing stored workspace = code %d stderr %q", missingRoot.code, missingRoot.stderr)
		}
	})

	t.Run("worksource_refuses_a_conflicting_default_profile", func(t *testing.T) {
		_, spine, env := freshHome(t)
		if res := runMC(t, env, "", "onboard", "home"); res.code != 0 {
			t.Fatalf("home failed: %s", res.stderr)
		}
		db, err := substrate.Open(spine)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO sandbox_profiles (id, workspace_root) VALUES ('default', '/different')`); err != nil {
			db.Close()
			t.Fatal(err)
		}
		db.Close()
		workspace := filepath.Join(t.TempDir(), "requested")
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, env, "", "onboard", "worksource",
			"--worksource", "ws-main", "--workspace-root", workspace)
		if res.code != 1 || !strings.Contains(res.stderr, "default") || !strings.Contains(res.stderr, "different") {
			t.Fatalf("conflicting profile = code %d stderr %q", res.code, res.stderr)
		}
		check, err := substrate.Open(spine)
		if err != nil {
			t.Fatal(err)
		}
		defer check.Close()
		if got := queryInt(t, check, `SELECT COUNT(*) FROM worksources`); got != 0 {
			t.Fatalf("conflicting profile left %d Worksources", got)
		}
	})

	t.Run("worksource_refuses_a_permissive_default_profile", func(t *testing.T) {
		_, spine, env := freshHome(t)
		if res := runMC(t, env, "", "onboard", "home"); res.code != 0 {
			t.Fatalf("home failed: %s", res.stderr)
		}
		workspace := filepath.Join(t.TempDir(), "requested")
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			t.Fatal(err)
		}
		canonical, err := filepath.EvalSymlinks(workspace)
		if err != nil {
			t.Fatal(err)
		}
		db, err := substrate.Open(spine)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO sandbox_profiles (id, workspace_root, egress_policy) VALUES ('default', ?, 'open+audit')`, canonical); err != nil {
			db.Close()
			t.Fatal(err)
		}
		db.Close()
		res := runMC(t, env, "", "onboard", "worksource",
			"--worksource", "ws-main", "--workspace-root", workspace)
		if res.code != 1 || !strings.Contains(res.stderr, "deny-by-default") {
			t.Fatalf("permissive profile = code %d stderr %q", res.code, res.stderr)
		}
	})

	t.Run("full_run_is_ordered_resumable_and_verified", func(t *testing.T) {
		_, spine, env := freshHome(t)
		workspace := filepath.Join(t.TempDir(), "ws-full")
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, env, "", "onboard",
			"--worksource", "ws-full", "--workspace-root", workspace,
			"--console-hour", "8", "--console-minute", "0", "--console-tz", "America/Los_Angeles")
		if res.code != 0 || res.json["ok"] != true {
			t.Fatalf("full onboard = code %d json %v stderr %q", res.code, res.json, res.stderr)
		}
		sections := res.json["sections"].([]any)
		order := []string{}
		for _, raw := range sections {
			order = append(order, raw.(map[string]any)["section"].(string))
		}
		if strings.Join(order, "|") != "preflight|home|runtime-auth|routing|container|worksource|tunables|surfaces|supervision|verify" {
			t.Fatalf("section order = %v", order)
		}
		byName := sectionStatus(t, res)
		for section, want := range map[string]string{
			"preflight": "ok", "home": "done", "runtime-auth": "deferred",
			"routing": "done", "container": "deferred", "worksource": "done",
			"tunables": "ok", "surfaces": "done", "supervision": "deferred",
			"verify": "ok",
		} {
			if byName[section]["status"] != want {
				t.Fatalf("section %q = %v, want %q", section, byName[section], want)
			}
		}
		db, err := substrate.Open(spine)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		uuid := queryStr(t, db, `SELECT deployment_uuid FROM meta WHERE id = 1`)

		// Resumable: the identical re-run performs nothing and changes nothing.
		again := runMC(t, env, "", "onboard",
			"--worksource", "ws-full", "--workspace-root", workspace,
			"--console-hour", "8", "--console-minute", "0", "--console-tz", "America/Los_Angeles")
		if again.code != 0 || again.json["ok"] != true {
			t.Fatalf("onboard re-run = code %d stderr %q", again.code, again.stderr)
		}
		reByName := sectionStatus(t, again)
		for _, section := range []string{"home", "routing", "worksource", "surfaces"} {
			if reByName[section]["status"] != "ok" {
				t.Fatalf("re-run section %q = %v, want ok (skip)", section, reByName[section])
			}
		}
		if got := queryStr(t, db, `SELECT deployment_uuid FROM meta WHERE id = 1`); got != uuid {
			t.Fatalf("re-run changed deployment identity %q -> %q", uuid, got)
		}
		// The provisioned deployment passes its own doctor.
		doctor := runMC(t, env, "", "doctor")
		if doctor.code != 0 || doctor.json["ok"] != true {
			t.Fatalf("post-onboard doctor = code %d json %v", doctor.code, doctor.json)
		}
	})

	t.Run("verify_refuses_an_unconfigured_console", func(t *testing.T) {
		_, _, env := freshHome(t)
		if res := runMC(t, env, "", "onboard", "home"); res.code != 0 {
			t.Fatalf("home failed: %s", res.stderr)
		}
		if res := runMC(t, env, "", "onboard", "routing"); res.code != 0 {
			t.Fatalf("routing failed: %s", res.stderr)
		}
		verify := runMC(t, env, "", "onboard", "verify")
		if verify.code != 1 || !strings.Contains(verify.stderr, "surfaces") {
			t.Fatalf("incomplete verify = code %d stderr %q", verify.code, verify.stderr)
		}
	})

	t.Run("supervision_double_never_invokes_launchctl", func(t *testing.T) {
		_, _, env := freshHome(t)
		if res := runMC(t, env, "", "onboard", "home"); res.code != 0 {
			t.Fatalf("home failed: %s", res.stderr)
		}
		binDir := t.TempDir()
		marker := filepath.Join(t.TempDir(), "launchctl-ran")
		fake := filepath.Join(binDir, "launchctl")
		if err := os.WriteFile(fake, []byte("#!/bin/sh\n: > \""+marker+"\"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		env = append(env, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		res := runMC(t, env, "", "onboard", "supervision")
		if res.code != 0 || sectionStatus(t, res)["supervision"]["status"] != "deferred" {
			t.Fatalf("supervision double = code %d json %v stderr %q", res.code, res.json, res.stderr)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("supervision double invoked launchctl: %v", err)
		}
	})

	t.Run("tunables_and_surfaces_apply_operator_answers", func(t *testing.T) {
		_, spine, env := freshHome(t)
		if res := runMC(t, env, "", "onboard", "home"); res.code != 0 {
			t.Fatalf("home failed: %s", res.stderr)
		}
		negative := runMC(t, env, "", "onboard", "tunables", "--timeout-minutes", "-1")
		if negative.code != 2 {
			t.Fatalf("negative tunable = code %d stderr %q", negative.code, negative.stderr)
		}
		tun := runMC(t, env, "", "onboard", "tunables", "--timeout-minutes", "7")
		if tun.code != 0 || sectionStatus(t, tun)["tunables"]["status"] != "done" {
			t.Fatalf("tunables = code %d json %v stderr %q", tun.code, tun.json, tun.stderr)
		}
		tunAgain := runMC(t, env, "", "onboard", "tunables", "--timeout-minutes", "7")
		if tunAgain.code != 0 || sectionStatus(t, tunAgain)["tunables"]["status"] != "ok" {
			t.Fatalf("tunable replay = code %d json %v stderr %q", tunAgain.code, tunAgain.json, tunAgain.stderr)
		}
		inspectTunables := runMC(t, env, "", "onboard", "tunables")
		tunableDetail := sectionStatus(t, inspectTunables)["tunables"]["detail"].(string)
		if inspectTunables.code != 0 || !strings.Contains(tunableDetail, "timeout_minutes=7") || strings.Contains(tunableDetail, "defaults accepted") {
			t.Fatalf("tunable inspection = code %d detail %q", inspectTunables.code, tunableDetail)
		}
		missingSchedule := runMC(t, env, "", "onboard", "surfaces")
		if missingSchedule.code != 1 || !strings.Contains(missingSchedule.stderr, "--console-hour") {
			t.Fatalf("unconfigured surfaces = code %d stderr %q", missingSchedule.code, missingSchedule.stderr)
		}
		surf := runMC(t, env, "", "onboard", "surfaces",
			"--console-hour", "8", "--console-minute", "15", "--console-tz", "America/Los_Angeles")
		if surf.code != 0 || sectionStatus(t, surf)["surfaces"]["status"] != "done" {
			t.Fatalf("surfaces = code %d json %v stderr %q", surf.code, surf.json, surf.stderr)
		}
		surfAgain := runMC(t, env, "", "onboard", "surfaces",
			"--console-hour", "8", "--console-minute", "15", "--console-tz", "America/Los_Angeles")
		if surfAgain.code != 0 || sectionStatus(t, surfAgain)["surfaces"]["status"] != "ok" {
			t.Fatalf("surface replay = code %d json %v stderr %q", surfAgain.code, surfAgain.json, surfAgain.stderr)
		}
		db, err := substrate.Open(spine)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if got := queryStr(t, db, `SELECT timeout_minutes || '|' || console_hour || '|' || console_minute || '|' || console_tz FROM lock WHERE id = 1`); got != "7|8|15|America/Los_Angeles" {
			t.Fatalf("applied answers = %q", got)
		}
		partial := runMC(t, env, "", "onboard", "surfaces", "--console-hour", "8")
		if partial.code != 2 {
			t.Fatalf("partial console triple = code %d stderr %q", partial.code, partial.stderr)
		}
		for name, flags := range map[string][]string{
			"explicit-negative-hour": {"--console-hour=-1", "--console-minute", "0", "--console-tz", "UTC"},
			"bad-hour":               {"--console-hour", "24", "--console-minute", "0", "--console-tz", "UTC"},
			"bad-minute":             {"--console-hour", "8", "--console-minute", "60", "--console-tz", "UTC"},
			"bad-timezone":           {"--console-hour", "8", "--console-minute", "0", "--console-tz", "Mars/Olympus"},
		} {
			args := append([]string{"onboard", "surfaces"}, flags...)
			bad := runMC(t, env, "", args...)
			if bad.code != 2 {
				t.Fatalf("%s = code %d stderr %q", name, bad.code, bad.stderr)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// ADR-020 D5: mc editor plan-review — the Editor's holistic wave terminal.
// ---------------------------------------------------------------------------

// seedUnreviewedWave files an initiative and births an unreviewed wave under
// it via raw SQL, returning the initiative and child ids.
func seedUnreviewedWave(t *testing.T, db *sql.DB) (int64, []int64) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO tasks (title, description, scope, worksource, target_ref)
		VALUES ('arc', 'charter', 'initiative', 'ws-test', 'main')`)
	if err != nil {
		t.Fatal(err)
	}
	ini, _ := res.LastInsertId()
	if _, err := db.Exec(`UPDATE tasks SET status='seeded' WHERE id=?`, ini); err != nil {
		t.Fatal(err)
	}
	var kids []int64
	for _, title := range []string{"child-a", "child-b"} {
		r, err := db.Exec(`INSERT INTO tasks (title, description, scope, status,
			initiative_id, worksource, target_ref)
			VALUES (?, 'criterion', 'task', 'seeded', ?, 'ws-test', 'main')`, title, ini)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		kids = append(kids, id)
	}
	return ini, kids
}

// claimPlanReview fakes the claim transaction: a live editor lease on the
// initiative whose run snapshot is the wave.
func claimPlanReview(t *testing.T, db *sql.DB, ini int64, snapshot []int64) string {
	t.Helper()
	run := fmt.Sprintf("plan-review-%d", ini)
	pool, _ := json.Marshal(snapshot)
	if _, err := db.Exec(`INSERT INTO runs (id, tier, role, worksource, subject, pool_snapshot)
		VALUES (?, 'pipeline', 'editor', 'ws-test', ?, ?)`, run, ini, string(pool)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE lock SET run_id=?, worksource='ws-test', subject=?,
		owner='editor', acquired_at=datetime('now'),
		hard_deadline_at=datetime('now', '+4 hours') WHERE id=1`, run, ini); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestEditorPlanReview(t *testing.T) {
	t.Run("pass_marks_every_snapshotted_child_and_leaves_the_initiative", func(t *testing.T) {
		spine := initSpine(t)
		db := openDB(t, spine)
		ini, kids := seedUnreviewedWave(t, db)
		run := claimPlanReview(t, db, ini, kids)
		env := runJSONEnv(t, spine, run, "pipeline", "editor(plan-review)")

		res := runMC(t, env, "", "editor", "plan-review", "--run", run,
			"--initiative", fmt.Sprint(ini), "--verdict", "pass")
		if res.code != 0 {
			t.Fatalf("pass failed (%d): %s", res.code, res.stderr)
		}
		for _, k := range kids {
			if got := queryInt(t, db, `SELECT plan_reviewed FROM tasks WHERE id=?`, k); got != 1 {
				t.Fatalf("child %d plan_reviewed = %d, want 1", k, got)
			}
		}
		// Inv. 10: the plan review completes no stage of the initiative.
		if got := queryStr(t, db, `SELECT status || '/' || COALESCE(decision,'null') || '/' || blocked
			FROM tasks WHERE id=?`, ini); got != "seeded/null/0" {
			t.Fatalf("initiative moved: %q", got)
		}
		if got := queryStr(t, db, `SELECT COALESCE(run_id,'free') FROM lock WHERE id=1`); got != "free" {
			t.Fatalf("lease not released: %q", got)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM activity WHERE kind='wave.passed' AND subject=?`, ini); got != 1 {
			t.Fatalf("wave.passed rows = %d, want 1", got)
		}
		// The released lease rejects a second call (D3-standard terminal).
		again := runMC(t, env, "", "editor", "plan-review", "--run", run,
			"--initiative", fmt.Sprint(ini), "--verdict", "pass")
		if again.code == 0 {
			t.Fatalf("second terminal accepted")
		}
	})

	// The exact-set rule: a holistic verdict asserts a property OF A SET, so a
	// verdict rendered over a set that no longer exists is stale and refused
	// outright — never partially applied.
	t.Run("pass_refuses_a_stale_snapshot", func(t *testing.T) {
		spine := initSpine(t)
		db := openDB(t, spine)
		ini, kids := seedUnreviewedWave(t, db)
		run := claimPlanReview(t, db, ini, kids)
		env := runJSONEnv(t, spine, run, "pipeline", "editor(plan-review)")
		// The operator cancels one child mid-review.
		if _, err := db.Exec(`UPDATE tasks SET decision='cancelled',
			decided_at=datetime('now'), archived=1 WHERE id=?`, kids[0]); err != nil {
			t.Fatal(err)
		}
		res := runMC(t, env, "", "editor", "plan-review", "--run", run,
			"--initiative", fmt.Sprint(ini), "--verdict", "pass")
		if res.code == 0 {
			t.Fatalf("stale pass accepted")
		}
		if !strings.Contains(res.stdout, "pool-mismatch") {
			t.Fatalf("want pool-mismatch code, got stdout=%q stderr=%q", res.stdout, res.stderr)
		}
		if got := queryInt(t, db, `SELECT plan_reviewed FROM tasks WHERE id=?`, kids[1]); got != 0 {
			t.Fatalf("stale pass partially applied: survivor marked")
		}
	})

	t.Run("send_back_cancels_the_wave_and_drains_the_initiative", func(t *testing.T) {
		spine := initSpine(t)
		db := openDB(t, spine)
		ini, kids := seedUnreviewedWave(t, db)
		run := claimPlanReview(t, db, ini, kids)
		env := runJSONEnv(t, spine, run, "pipeline", "editor(plan-review)")

		res := runMC(t, env, "", "editor", "plan-review", "--run", run,
			"--initiative", fmt.Sprint(ini), "--verdict", "send-back",
			"--reason", "child-a's criterion cannot fail")
		if res.code != 0 {
			t.Fatalf("send-back failed (%d): %s", res.code, res.stderr)
		}
		for _, k := range kids {
			if got := queryStr(t, db, `SELECT decision || '/' || archived FROM tasks WHERE id=?`, k); got != "cancelled/1" {
				t.Fatalf("child %d = %q, want cancelled/1", k, got)
			}
		}
		// Inv. 7: the actor is the logical originator, not the operator.
		if got := queryStr(t, db, `SELECT actor FROM activity WHERE kind='task.cancelled'
			AND subject=? LIMIT 1`, kids[0]); got != "editor" {
			t.Fatalf("cancel actor = %q, want editor", got)
		}
		if got := queryStr(t, db, `SELECT detail FROM activity WHERE kind='wave.sent_back'
			AND subject=?`, ini); got != "child-a's criterion cannot fail" {
			t.Fatalf("send-back reason = %q", got)
		}
		// Drained ⇒ the next tick owes Strategist(initiative) a replan.
		if got := queryInt(t, db, `SELECT COUNT(*) FROM tasks WHERE initiative_id=? AND archived=0`, ini); got != 0 {
			t.Fatalf("initiative not drained: %d open children", got)
		}
		if got := queryStr(t, db, `SELECT status FROM tasks WHERE id=?`, ini); got != "seeded" {
			t.Fatalf("initiative status = %q, want seeded", got)
		}
	})

	// Asymmetric by design (§7's precedent): a pass needs no prose because the
	// work itself is what happens next; a send-back is worthless without it.
	t.Run("reason_asymmetry", func(t *testing.T) {
		spine := initSpine(t)
		db := openDB(t, spine)
		ini, kids := seedUnreviewedWave(t, db)
		run := claimPlanReview(t, db, ini, kids)
		env := runJSONEnv(t, spine, run, "pipeline", "editor(plan-review)")

		bare := runMC(t, env, "", "editor", "plan-review", "--run", run,
			"--initiative", fmt.Sprint(ini), "--verdict", "send-back")
		if bare.code == 0 {
			t.Fatalf("send-back without a reason accepted")
		}
		withReason := runMC(t, env, "", "editor", "plan-review", "--run", run,
			"--initiative", fmt.Sprint(ini), "--verdict", "pass", "--reason", "why?")
		if withReason.code == 0 {
			t.Fatalf("pass with a reason accepted")
		}
		if got := queryInt(t, db, `SELECT plan_reviewed FROM tasks WHERE id=?`, kids[0]); got != 1-1 {
			t.Fatalf("a rejected terminal still marked the wave")
		}
	})

	t.Run("fences", func(t *testing.T) {
		spine := initSpine(t)
		db := openDB(t, spine)
		ini, kids := seedUnreviewedWave(t, db)
		run := claimPlanReview(t, db, ini, kids)

		// Wrong mode: an Editor pool run cannot invoke the plan review.
		pool := runMC(t, runJSONEnv(t, spine, run, "pipeline", "editor"), "",
			"editor", "plan-review", "--run", run, "--initiative", fmt.Sprint(ini), "--verdict", "pass")
		if pool.code == 0 {
			t.Fatalf("an editor pool identity reached plan-review")
		}
		// Host scope: no run.json at all.
		host := runMC(t, spineEnv(spine), "", "editor", "plan-review", "--run", run,
			"--initiative", fmt.Sprint(ini), "--verdict", "pass")
		if host.code == 0 {
			t.Fatalf("host reached the pipeline terminal")
		}
		// Wrong run: the --run token must equal run.json's own run_id.
		other := runJSONEnv(t, spine, "someone-elses-run", "pipeline", "editor(plan-review)")
		crossed := runMC(t, other, "", "editor", "plan-review", "--run", run,
			"--initiative", fmt.Sprint(ini), "--verdict", "pass")
		if crossed.code == 0 {
			t.Fatalf("a foreign run token was accepted")
		}
		// Wrong subject: --initiative must equal the fenced lease's subject.
		env := runJSONEnv(t, spine, run, "pipeline", "editor(plan-review)")
		wrong := runMC(t, env, "", "editor", "plan-review", "--run", run,
			"--initiative", fmt.Sprint(kids[0]), "--verdict", "pass")
		if wrong.code == 0 {
			t.Fatalf("a non-subject initiative was accepted")
		}
		for _, k := range kids {
			if got := queryInt(t, db, `SELECT plan_reviewed FROM tasks WHERE id=?`, k); got != 0 {
				t.Fatalf("a fenced-out call still marked child %d", k)
			}
		}
	})

	// The reciprocal tightening: without requireExactRole on decide, an
	// editor(plan-review) identity passes base-role matching and reaches it.
	t.Run("plan_review_identity_cannot_reach_editor_decide", func(t *testing.T) {
		spine := initSpine(t)
		db := openDB(t, spine)
		ini, kids := seedUnreviewedWave(t, db)
		run := claimPlanReview(t, db, ini, kids)
		env := runJSONEnv(t, spine, run, "pipeline", "editor(plan-review)")
		res := runMC(t, env, `{"verdicts":[]}`, "editor", "decide", "--run", run, "--batch", "-")
		if res.code == 0 {
			t.Fatalf("a plan-review identity reached editor decide")
		}
		_ = db
	})
}

// ADR-020 D5's row in the ADR-001 D6 verb-by-scope table: mc editor
// plan-review is pipeline-role only (editor/plan-review). Host is proven in
// TestEditorPlanReview/fences; this covers the remaining scopes.
//
// Note on what is NOT asserted: role terminals load identity and open the
// spine before the role check, so a refusal here still creates an empty spine
// file at MC_SPINE. That is the established shape for every role terminal
// (mc editor decide behaves identically) and no rows are written; the
// wave-2 contract's refusal-precedes-spine-bytes rule is asserted in
// TestScopeRefusalPrecedesSpineOpen for the host/operator verbs it governs.
func TestEditorPlanReviewScopeMatrix(t *testing.T) {
	args := []string{"editor", "plan-review", "--run", "r-probe",
		"--initiative", "1", "--verdict", "pass"}

	for _, tc := range []struct {
		name string
		env  func(spine string) []string
	}{
		{
			// The runner tier owns lifecycle verbs, never terminals — even
			// carrying the exact role string.
			"pipeline_runner",
			func(spine string) []string {
				return runJSONEnv(t, spine, "r-probe", "pipeline-runner", "editor(plan-review)")
			},
		},
		{
			// Even with the verb forged into its allowlist: the Homie tier is
			// not a pipeline role and holds no lease.
			"homie_agent",
			func(spine string) []string {
				return homieJSONEnv(t, spine, "h-1", []string{"editor.plan-review"})
			},
		},
		{
			// No other pipeline role can borrow the Editor's terminal.
			"other_pipeline_role",
			func(spine string) []string {
				return runJSONEnv(t, spine, "r-probe", "pipeline", "worker")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spine := initSpine(t)
			db := openDB(t, spine)
			before := queryInt(t, db, `SELECT COUNT(*) FROM activity`)
			res := runMC(t, tc.env(spine), "", args...)
			if res.code == 0 {
				t.Fatalf("%s reached the Editor plan-review terminal", tc.name)
			}
			if got := queryInt(t, db, `SELECT COUNT(*) FROM activity`); got != before {
				t.Fatalf("%s wrote %d activity rows on a refused terminal", tc.name, got-before)
			}
		})
	}
}
