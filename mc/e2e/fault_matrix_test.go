//go:build docker_e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFaultReapRetryDockerBoundary is Phase 4 scenario family (5)'s core: a
// spawned Worker that dies before establishing a session (the spawn-watchdog
// kill class) is reaped on the spawn-grace threshold, which charges the task's
// infra-death budget (dispatch_retries) and frees the lease; the same task is
// re-selected (its status never advanced, Inv. 10) and completes on the retry.
// The reaped run's trace-only session folder survives the reap (Inv. 26).
//
// Kill lever: a MALFORMED behavior file makes the fake harness exit before it
// emits session-start, so the runner never heartbeats — the never-heartbeated
// path the reaper reaps at spawn_grace_s (5s in the E2E tunables), the exact
// fast-fail case flagged in the family-3 ledger. The test then swaps in a valid
// behavior so the retry completes.
func TestFaultReapRetryDockerBoundary(t *testing.T) {
	f := setup(t)

	// First: a broken Worker harness that dies before session-start.
	brokenWorker := filepath.Join(f.base, "behaviors", "worker.json")
	if err := os.WriteFile(brokenWorker, []byte("{ not valid behavior json"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := f.mcOK("", "task", "add", "fault-retry task", "--worksource", worksource,
		"--description", "worker dies before session, gets reaped, retries green")
	taskID := int64(res["task_id"].(float64))
	branch := fmt.Sprintf("mc/task-%d", taskID)

	f.startResident()

	// The broken Worker is spawned, dies before heartbeating, and is reaped at
	// spawn_grace — charging one infra retry (3 → <3).
	f.waitFor(90*time.Second, "a reap charges the infra budget", func() (bool, string) {
		task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
		if r := task["dispatch_retries"].(float64); r < 3 {
			return true, ""
		}
		return false, fmt.Sprintf("dispatch_retries still %v", task["dispatch_retries"])
	})

	// The reaped Worker run exists with outcome=reaped, and its trace-only
	// session folder survives the reap (Inv. 26).
	var reapedRun string
	for _, r := range f.runs() {
		if r["role"] == "worker" && r["outcome"] == "reaped" {
			reapedRun = r["id"].(string)
		}
	}
	if reapedRun == "" {
		t.Fatalf("no reaped worker run for task %d: %v", taskID, f.runs())
	}
	sessionDir := filepath.Join(f.home, "sessions", reapedRun)
	if _, err := os.Stat(sessionDir); err != nil {
		t.Fatalf("reaped run's session folder %s missing after reap: %v (Inv. 26)", sessionDir, err)
	}

	// The task never advanced (status unchanged, not blocked yet), so it is
	// still re-selectable.
	if st := f.mcOK("", "task", "get", fmt.Sprint(taskID))["status"]; st != "seeded" {
		t.Fatalf("reaped task status = %q, want seeded (a reaped run never advances its task)", st)
	}

	// Now heal the Worker: the retry re-selects the same task and completes.
	writeBehaviorFile(t, f, "worker.json", behaviorSteps(
		execStep(`set -e
cd /workspace/source
git worktree add ".mc-worktrees/task-$MC_SUBJECT_ID" -b "mc/task-$MC_SUBJECT_ID"
cd ".mc-worktrees/task-$MC_SUBJECT_ID"
echo "recovered work" > work.txt
git add work.txt
git -c user.name='mc worker' -c user.email='worker@mc.invalid' commit -q -m "recovered"`),
		execStep(`mc complete "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --status worked --branch "mc/task-$MC_SUBJECT_ID"`),
		succeedStep("worked"),
	))

	// The healed Worker re-selects the same task and drives it to packaged.
	f.waitForTaskStatus(taskID, "packaged", 120*time.Second)
	if got := f.git("rev-parse", "refs/heads/"+branch); got == f.git("rev-parse", "main") {
		t.Fatalf("recovered branch %s has no commit", branch)
	}
	// The infra budget was charged (< 3) but not exhausted — the task was never
	// blocked.
	final := f.mcOK("", "task", "get", fmt.Sprint(taskID))
	if final["blocked"].(float64) != 0 {
		t.Fatalf("recovered task is blocked: %v", final["blocked_reason"])
	}
	if r := final["dispatch_retries"].(float64); r >= 3 {
		t.Fatalf("dispatch_retries = %v after a reap, want < 3 (one infra charge)", r)
	}
}

// TestFaultBudgetExhaustionDockerBoundary is family (5)'s exhaustion arm: a
// Worker that never establishes a session is reaped repeatedly; each reap
// charges one infra retry, and when the budget hits zero the task is BLOCKED
// with a stable reason rather than looping forever (§10 "never a silent loop").
func TestFaultBudgetExhaustionDockerBoundary(t *testing.T) {
	f := setup(t)

	// A permanently broken Worker harness.
	if err := os.WriteFile(filepath.Join(f.base, "behaviors", "worker.json"),
		[]byte("{ permanently broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := f.mcOK("", "task", "add", "fault-exhaustion task", "--worksource", worksource,
		"--description", "worker never establishes a session; the retry budget drains to blocked")
	taskID := int64(res["task_id"].(float64))

	f.startResident()

	// The default budget is 3, each reap charges one (spawn_grace ~5s each), so
	// three reaps → blocked. Give it a generous window.
	f.waitFor(120*time.Second, "the drained infra budget blocks the task", func() (bool, string) {
		task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
		if task["blocked"].(float64) == 1 {
			return true, ""
		}
		return false, fmt.Sprintf("dispatch_retries=%v blocked=%v", task["dispatch_retries"], task["blocked"])
	})

	task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
	if r := task["dispatch_retries"].(float64); r != 0 {
		t.Fatalf("dispatch_retries at block = %v, want 0 (budget fully drained)", r)
	}
	if reason, _ := task["blocked_reason"].(string); reason == "" {
		t.Fatalf("blocked with no reason; want a dispatch-retries-exhausted reason (§10)")
	}
}
