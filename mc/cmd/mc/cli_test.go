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

	t.Run("missing_file_fails_before_claim", func(t *testing.T) {
		spine := initSpine(t)
		if err := os.Remove(filepath.Join(filepath.Dir(spine), "routing.md")); err != nil {
			t.Fatal(err)
		}
		taskAdd(t, spine, "must not dispatch")
		res := runMC(t, spineEnv(spine), "", "dispatch")
		if res.code != 2 || !strings.Contains(res.stderr, "routing.md") {
			t.Fatalf("exit=%d stderr=%q", res.code, res.stderr)
		}
		db := openDB(t, spine)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM runs`); n != 0 {
			t.Fatalf("missing routing opened %d runs", n)
		}
	})

	t.Run("unresolved_binding_fails_before_claim", func(t *testing.T) {
		spine := initSpine(t)
		writeRouting(t, spine, strings.Replace(defaultRoutingMarkdown,
			"| worker | claude-sdk | minimax |", "| worker | claude-sdk | missing |", 1))
		taskAdd(t, spine, "must not dispatch")
		res := runMC(t, spineEnv(spine), "", "dispatch")
		if res.code != 1 || !strings.Contains(res.stderr, "unresolved binding") {
			t.Fatalf("exit=%d stderr=%q", res.code, res.stderr)
		}
		db := openDB(t, spine)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM runs`); n != 0 {
			t.Fatalf("unresolved routing opened %d runs", n)
		}
	})

	t.Run("producer_judge_same_family_fails_before_claim", func(t *testing.T) {
		spine := initSpine(t)
		writeRouting(t, spine, strings.Replace(defaultRoutingMarkdown,
			"| worker | claude-sdk | minimax |", "| worker | codex | chatgpt |", 1))
		taskAdd(t, spine, "must not dispatch")
		res := runMC(t, spineEnv(spine), "", "dispatch")
		if res.code != 1 || !strings.Contains(res.stderr, "decorrelated") {
			t.Fatalf("exit=%d stderr=%q", res.code, res.stderr)
		}
		db := openDB(t, spine)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM runs`); n != 0 {
			t.Fatalf("invalid decorrelation opened %d runs", n)
		}
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
		packageTask(t, spine, id, "mc/task-routing-land")
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

	spawns, idles := 0, 0
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
			if eff["reason"] != "lease-held" {
				t.Fatalf("claimant %d idle reason = %v", i, eff["reason"])
			}
			idles++
		default:
			t.Fatalf("claimant %d action = %v", i, eff["action"])
		}
	}
	if spawns != 1 || idles != claimants-1 {
		t.Fatalf("spawns = %d, idles = %d; want exactly one winner among %d", spawns, idles, claimants)
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
}

func TestDoubleApproveRejected(t *testing.T) {
	spine := initSpine(t)
	taskID := taskAdd(t, spine, "decided once")
	packageTask(t, spine, taskID, "mc/task-x")
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
	packageTask(t, spine, taskID, "mc/task-y")
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
		before := queryStr(t, db, `SELECT group_concat(id || ':' || COALESCE(delivered_at, '<null>'), ',') FROM outbox ORDER BY id`)

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
		if got := queryStr(t, db, `SELECT group_concat(id || ':' || COALESCE(delivered_at, '<null>'), ',') FROM outbox ORDER BY id`); got != before {
			t.Fatalf("poll mutated delivery state: before %q after %q", before, got)
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
		before := queryStr(t, db, `SELECT group_concat(id || ':' || COALESCE(delivered_at, '<null>'), ',') FROM outbox ORDER BY id`)
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
			if got.code == 0 || !strings.Contains(got.stderr, tc.msg) {
				t.Fatalf("%v exit = %d stderr %q", tc.args, got.code, got.stderr)
			}
		}
		if got := queryStr(t, db, `SELECT group_concat(id || ':' || COALESCE(delivered_at, '<null>'), ',') FROM outbox ORDER BY id`); got != before {
			t.Fatalf("scope/usage failure mutated delivery: before %q after %q", before, got)
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
