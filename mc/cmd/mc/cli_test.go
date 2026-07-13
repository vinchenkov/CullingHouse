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
	if res.code == 0 && strings.TrimSpace(res.stdout) != "" {
		if err := json.Unmarshal([]byte(res.stdout), &res.json); err != nil {
			t.Fatalf("mc %v: stdout is not a single JSON object: %q (%v)", args, res.stdout, err)
		}
	}
	return res
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

	// Runner lifecycle: heartbeat advances last_heartbeat_at, never the hard
	// deadline (Inv. 1); register-session records the locators (ADR-001 D5).
	deadlineBefore := queryStr(t, db, `SELECT hard_deadline_at FROM lock WHERE id = 1`)
	hb := runMC(t, spineEnv(spine), "", "heartbeat", workerRun)
	if hb.code != 0 {
		t.Fatalf("heartbeat failed: %s", hb.stderr)
	}
	if got := queryStr(t, db, `SELECT last_heartbeat_at FROM lock WHERE id = 1`); got == "<NULL>" {
		t.Fatalf("heartbeat did not stamp last_heartbeat_at")
	}
	if got := queryStr(t, db, `SELECT hard_deadline_at FROM lock WHERE id = 1`); got != deadlineBefore {
		t.Fatalf("heartbeat moved hard_deadline_at %q → %q (Inv. 1)", deadlineBefore, got)
	}
	if res := runMC(t, spineEnv(spine), "", "heartbeat", "not-the-run"); res.code != 1 {
		t.Fatalf("stale heartbeat exit = %d, want 1", res.code)
	}
	rs := runMC(t, spineEnv(spine), "", "run", "register-session", workerRun,
		"--native-ref", "fake-session", "--file", "native.jsonl")
	if rs.code != 0 {
		t.Fatalf("register-session failed: %s", rs.stderr)
	}
	if got := queryStr(t, db, `SELECT native_session_ref FROM runs WHERE id = ?`, workerRun); got != "fake-session" {
		t.Fatalf("native_session_ref = %q", got)
	}
	if res := runMC(t, spineEnv(spine), "", "run", "register-session", "not-the-run",
		"--native-ref", "x", "--file", "y"); res.code != 1 {
		t.Fatalf("unknown-run register-session exit = %d, want 1", res.code)
	}

	// Worker terminal: seeded → worked, branch recorded (Ambiguity A2), and
	// complete never dispatches (Inv. 3) — no effect data, lease free.
	workerEnv := runJSONEnv(t, spine, workerRun, "pipeline", "worker")
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
	rs = runMC(t, spineEnv(spine), "", "run", "register-session", workerRun,
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
		"complete", fmt.Sprint(taskID), "--run", run, "--status", "packaged")
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
		{"verifier_wrong_role", runJSONEnv(t, spine, run, "pipeline", "editor"),
			[]string{"verifier", "verdict", fmt.Sprint(taskID), "--run", run,
				"--outcome", "pass", "--evidence", "e", "--sha", "s"}, "", "role mismatch"},
		{"strategist_wrong_role", runJSONEnv(t, spine, run, "pipeline", "worker"),
			[]string{"strategist", "propose", "--run", run, "--batch", "-"}, `{"proposals":[]}`, "role mismatch"},
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
