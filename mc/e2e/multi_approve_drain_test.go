//go:build docker_e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMultiApproveDrainDockerBoundary is Phase 4 scenario family (1)'s
// multi-approve-drain variant: several approved tasks land in sequence, one
// per tick, in decision order — never collapsed into a single batch merge.
// nextLanding sorts pending rows by (decided_at, id) and emits one KindLand
// per tick; Inv. 1 serializes the pipeline, so the observable end-state is a
// linear chain of --no-ff merges on the trunk, one per approved task.
//
// The default editor promotes a single-id pool and the default worker writes a
// fixed filename (whose second landing would add/add-conflict), so this test
// overrides both: a multi-verdict editor and a per-task worker filename.
func TestMultiApproveDrainDockerBoundary(t *testing.T) {
	f := setup(t)

	// A multi-verdict editor: promote every id in the snapshotted pool.
	writeBehaviorFile(t, f, "editor.json", behaviorSteps(
		// MC_POOL_IDS is comma-separated (agent-runner main.ts:60); split it to
		// promote every task in the run's snapshotted pool. EditorDecide
		// refuses a batch that does not cover the pool exactly (ADR-001 D4).
		execStep(`set -e
ids=$(printf '%s' "$MC_POOL_IDS" | tr ',' ' ')
verdicts=""
for id in $ids; do
  verdicts="${verdicts}{\"task\":${id},\"decision\":\"promote\",\"reason\":\"drain\"},"
done
printf '{"verdicts":[%s]}' "${verdicts%,}" | mc editor decide --run "$MC_RUN_ID" --batch -`),
		succeedStep("promoted"),
	))
	// A per-task worker: a task-scoped filename so distinct landings never
	// collide, and no sleep (heartbeat advancement is TestWalkingSkeleton's).
	writeBehaviorFile(t, f, "worker.json", behaviorSteps(
		execStep(`set -e; cd /workspace/source; git worktree add .mc-worktrees/task-$MC_SUBJECT_ID -b mc/task-$MC_SUBJECT_ID; cd .mc-worktrees/task-$MC_SUBJECT_ID; echo "drain work for task $MC_SUBJECT_ID" > work-$MC_SUBJECT_ID.txt; git add work-$MC_SUBJECT_ID.txt; git -c user.name='mc worker' -c user.email='worker@mc.invalid' commit -q -m "mc/task-$MC_SUBJECT_ID: drain work"`),
		execStep(`mc complete "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --status worked --branch "mc/task-$MC_SUBJECT_ID"`),
		succeedStep("worked"),
	))

	const nTasks = 3
	ids := make([]int64, 0, nTasks)
	for i := 0; i < nTasks; i++ {
		res := f.mcOK("", "task", "add", fmt.Sprintf("drain task %d", i+1), "--worksource", worksource,
			"--description", "one of several tasks approved together")
		ids = append(ids, int64(res["task_id"].(float64)))
	}
	mainBefore := f.git("rev-parse", "main")

	f.startResident()

	// Inv. 1 serializes the pipeline, so the tasks reach packaged one at a
	// time; wait for all of them before approving any.
	for _, id := range ids {
		f.waitForTaskStatus(id, "packaged", 180*time.Second)
	}
	workedSHA := map[int64]string{}
	for _, id := range ids {
		workedSHA[id] = f.git("rev-parse", fmt.Sprintf("refs/heads/mc/task-%d", id))
	}

	// Approve all of them, spaced so decided_at is strictly increasing and the
	// drain order is deterministic (decided_at, id).
	for _, id := range ids {
		if got := f.mcRun("", "packet", "decide", fmt.Sprint(id), "--approve"); got.code != 0 {
			t.Fatalf("approve task %d exited %d: %s", id, got.code, got.stderr)
		}
		time.Sleep(1100 * time.Millisecond)
	}

	// Every approved task drains and archives.
	for _, id := range ids {
		f.waitFor(90*time.Second, fmt.Sprintf("task %d drains and archives", id), func() (bool, string) {
			task := f.mcOK("", "task", "get", fmt.Sprint(id))
			if task["archived"].(float64) != 1 {
				return false, fmt.Sprintf("archived=%v blocked=%v (%v)", task["archived"], task["blocked"], task["blocked_reason"])
			}
			return true, ""
		})
	}

	// The drain is SEQUENTIAL, not a collapsed batch: exactly nTasks --no-ff
	// merges, all on the first-parent trunk (one landing per task), each
	// carrying one task's reviewed SHA as its second parent.
	span := mainBefore + "..main"
	if got := f.git("rev-list", "--count", "--merges", "--first-parent", span); got != fmt.Sprint(nTasks) {
		t.Fatalf("trunk merge count = %q, want %d sequential --no-ff landings", got, nTasks)
	}
	secondParents := map[string]bool{}
	for _, sha := range strings.Fields(f.git("rev-list", "--merges", "--first-parent", span)) {
		secondParents[f.git("rev-parse", sha+"^2")] = true
	}
	for _, id := range ids {
		if !secondParents[workedSHA[id]] {
			t.Fatalf("task %d verified_sha %s is not a merge second parent; it did not land as its own merge", id, workedSHA[id])
		}
		worktreeDir := filepath.Join(f.ws, ".mc-worktrees", fmt.Sprintf("task-%d", id))
		if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
			t.Fatalf("worktree for task %d still present after landing (§7 step 3)", id)
		}
		if _, err := f.gitErr("rev-parse", "--verify", fmt.Sprintf("refs/heads/mc/task-%d", id)); err == nil {
			t.Fatalf("branch mc/task-%d still exists after landing (§7 step 3)", id)
		}
	}

	f.waitFor(10*time.Second, "lock free after the drain", func() (bool, string) {
		if lock := f.mcOK("", "lock", "get"); lock["run_id"] != nil {
			return false, fmt.Sprintf("held by %v", lock["owner"])
		}
		return true, ""
	})
}

// --- behavior-file helpers (build via json.Marshal to avoid escaping) ---

type behaviorStep struct {
	Do      string `json:"do"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
}

func execStep(command string) behaviorStep    { return behaviorStep{Do: "exec", Command: command} }
func succeedStep(output string) behaviorStep  { return behaviorStep{Do: "succeed", Output: output} }
func behaviorSteps(steps ...behaviorStep) any { return map[string]any{"steps": steps} }

func writeBehaviorFile(t *testing.T, f *fixture, name string, body any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.base, "behaviors", name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
