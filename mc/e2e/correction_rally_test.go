//go:build docker_e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCorrectionRallyDockerBoundary is Phase 4 scenario family (2): a Verifier
// returns CORRECT three times, sending the task back through the Worker each
// time (worked→seeded, correction_count++), then ships the fourth pass as
// BUDGET-SPENT (worked→verified, exception-labeled) at correction_count == 3.
//
// The domain makes the end-state self-proving: a fourth CORRECT is rejected
// (CodeBudgetExhausted), so the only way to leave `worked` at cc == 3 is an
// explicit budget-spent — reaching `packaged` with correction_count == 3
// therefore proves the rally spent its budget and shipped anyway.
//
// Role behavior is static, so verifier.json branches on correction_count read
// via `mc task get`, and the worker is re-work-safe (it reuses its worktree).
func TestCorrectionRallyDockerBoundary(t *testing.T) {
	f := setup(t)

	// A re-work-safe worker: create the worktree once, then append a commit on
	// every pass so each rework advances the reviewed branch. No sleep.
	writeBehaviorFile(t, f, "worker.json", behaviorSteps(
		execStep(`set -e
cd /workspace/source
wt=.mc-worktrees/task-$MC_SUBJECT_ID
if [ ! -d "$wt" ]; then git worktree add "$wt" -b mc/task-$MC_SUBJECT_ID; fi
cd "$wt"
echo "rework" >> work.txt
git add work.txt
git -c user.name='mc worker' -c user.email='worker@mc.invalid' commit -q -m "mc/task-$MC_SUBJECT_ID: rework"`),
		execStep(`mc complete "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --status worked --branch "mc/task-$MC_SUBJECT_ID"`),
		succeedStep("worked"),
	))

	// A count-aware verifier: CORRECT while correction_count < 3 (re-seeds the
	// task), BUDGET-SPENT at 3 (ships it). CORRECT forbids --sha and requires
	// --correction; budget-spent is the inverse.
	writeBehaviorFile(t, f, "verifier.json", behaviorSteps(
		execStep(`set -e
cc=$(mc task get "$MC_SUBJECT_ID" | tr -d ' \n' | sed -n 's/.*"correction_count":\([0-9][0-9]*\).*/\1/p')
: "${cc:?could not read correction_count}"
sha=$(git -C /workspace/source rev-parse "refs/heads/mc/task-$MC_SUBJECT_ID")
ev=/tmp/evidence-$MC_SUBJECT_ID.txt
printf 'rally verdict at cc=%s\n' "$cc" > "$ev"
if [ "$cc" -lt 3 ]; then
  corr=/tmp/correction-$MC_SUBJECT_ID.txt
  printf 'please revise (cc=%s)\n' "$cc" > "$corr"
  mc verifier verdict "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --outcome correct --evidence "$ev" --correction "$corr"
else
  mc verifier verdict "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --outcome budget-spent --evidence "$ev" --sha "$sha"
fi`),
		succeedStep("verified"),
	))

	res := f.mcOK("", "task", "add", "correction rally task", "--worksource", worksource,
		"--description", "three corrects then budget-spent")
	taskID := int64(res["task_id"].(float64))

	f.startResident()

	// The rally cycles worker↔verifier four times; wait for the shipped state.
	f.waitForTaskStatus(taskID, "packaged", 240*time.Second)

	task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
	if cc := task["correction_count"].(float64); cc != 3 {
		t.Fatalf("correction_count = %v, want 3 (three CORRECTs before the ship)", cc)
	}
	// Reaching packaged at correction_count==3 is only possible via a
	// budget-spent verdict — a fourth CORRECT is rejected by the domain.

	// Corroboration: the rally cycled four verifier passes and four worker
	// passes (initial + three reworks), all completed.
	verifiers, workers := 0, 0
	for _, r := range f.runs() {
		if r["outcome"] != "completed" || r["subject"] == nil || int64(r["subject"].(float64)) != taskID {
			continue
		}
		switch r["role"] {
		case "verifier":
			verifiers++
		case "worker":
			workers++
		}
	}
	if verifiers != 4 {
		t.Fatalf("completed verifier runs for task %d = %d, want 4 (3 correct + 1 budget-spent)", taskID, verifiers)
	}
	if workers != 4 {
		t.Fatalf("completed worker runs for task %d = %d, want 4 (initial + 3 reworks)", taskID, workers)
	}

	// The exception-labeled ship still produces a packet like any PASS.
	packets := f.packets()
	if len(packets) != 1 || int64(packets[0]["task_id"].(float64)) != taskID {
		t.Fatalf("packet = %v, want one unarchived packet for task %d", packets, taskID)
	}

	// The reviewed branch carries all four worker commits above the base.
	commits := f.git("rev-list", "--count", "main.."+fmt.Sprintf("refs/heads/mc/task-%d", taskID))
	if commits != "4" {
		t.Fatalf("reviewed branch has %s commits, want 4 (initial + 3 reworks)", commits)
	}

	// Cleanup residue check: the worktree still exists (not landed here).
	worktreeDir := filepath.Join(f.ws, ".mc-worktrees", fmt.Sprintf("task-%d", taskID))
	if _, err := os.Stat(worktreeDir); err != nil {
		t.Fatalf("worktree %s missing after the rally: %v", worktreeDir, err)
	}
}
