//go:build docker_e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBackpressureDockerBoundary is Phase 4 scenario family (3): with the
// review queue at its cap of 3 unarchived packets, the dispatcher admits NO
// new pipeline work (a fourth proposed task never gets an Editor), while it
// still (a) spawns a Refiner on the best non-saturated packet and (b) advances
// the re-entered task through its pipeline roles at cap. Draining the queue
// (cancelling the three packets) frees the fourth task to advance — proving
// the cap, not some other stall, was the blocker.
//
// The deep saturation arithmetic (three churns → saturated → idle) is owned by
// the exhaustive dispatch/substrate/property unit tests
// (dispatch_test.go:930-990, the packets_saturate triggers, lifecycle_nightly)
// and is not re-driven through real containers here; this E2E proves the parts
// only a live resident/timer/container can: the cap gate, the auto-Refiner
// spawn, and re-entry advancing at cap.
func TestBackpressureDockerBoundary(t *testing.T) {
	f := setup(t)

	// Multi-verdict editor (MC_POOL_IDS is comma-separated).
	writeBehaviorFile(t, f, "editor.json", behaviorSteps(
		execStep(`set -e
ids=$(printf '%s' "$MC_POOL_IDS" | tr ',' ' ')
verdicts=""
for id in $ids; do
  verdicts="${verdicts}{\"task\":${id},\"decision\":\"promote\",\"reason\":\"bp\"},"
done
printf '{"verdicts":[%s]}' "${verdicts%,}" | mc editor decide --run "$MC_RUN_ID" --batch -`),
		succeedStep("promoted"),
	))
	// Re-work-safe worker: the refiner re-enters tasks, so the worktree may
	// already exist.
	writeBehaviorFile(t, f, "worker.json", behaviorSteps(
		execStep(`set -e
cd /workspace/source
wt=.mc-worktrees/task-$MC_SUBJECT_ID
if [ ! -d "$wt" ]; then git worktree add "$wt" -b mc/task-$MC_SUBJECT_ID; fi
cd "$wt"
echo "work" >> work.txt
git add work.txt
git -c user.name='mc worker' -c user.email='worker@mc.invalid' commit -q -m "mc/task-$MC_SUBJECT_ID: work"`),
		execStep(`mc complete "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --status worked --branch "mc/task-$MC_SUBJECT_ID"`),
		succeedStep("worked"),
	))
	// Verifier: always PASS. On a refinement round the task carries non-empty
	// refine_notes, and a PASS refinement must declare --deepening genuine
	// (which resets the streak, so the packet never saturates and the Refiner
	// keeps re-entering — exactly the at-cap churn this test wants to observe).
	writeBehaviorFile(t, f, "verifier.json", behaviorSteps(
		execStep(`set -e
sha=$(git -C /workspace/source rev-parse "refs/heads/mc/task-$MC_SUBJECT_ID")
ev=/tmp/evidence-$MC_SUBJECT_ID.txt
printf 'bp verdict\n' > "$ev"
deepening=""
if mc task get "$MC_SUBJECT_ID" | tr -d ' \n' | grep -q '"refine_notes":"[^"]'; then
  deepening="--deepening genuine"
fi
mc verifier verdict "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --outcome pass --evidence "$ev" --sha "$sha" $deepening`),
		succeedStep("verified"),
	))
	// Refiner terminal: re-enter with a deepening scope (packaged→seeded).
	writeBehaviorFile(t, f, "refiner.json", behaviorSteps(
		execStep(`mc complete "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --status seeded --outputs "deepen the analysis"`),
		succeedStep("refined"),
	))
	addResidentRoleBehavior(t, f, "refiner", "/mc/behaviors/refiner.json")

	// Three tasks fill the queue to its cap of 3.
	capIDs := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		res := f.mcOK("", "task", "add", fmt.Sprintf("queue filler %d", i+1), "--worksource", worksource,
			"--description", "one of three packets that fill the review cap")
		capIDs = append(capIDs, int64(res["task_id"].(float64)))
	}

	f.startResident()

	// Wait until all three reach the queue: three unarchived packets = at cap.
	f.waitFor(180*time.Second, "review queue fills to the cap of 3 packets", func() (bool, string) {
		return countUnarchivedPackets(f) == 3, fmt.Sprintf("%d unarchived packets", countUnarchivedPackets(f))
	})

	// A fourth proposed task added AT cap must never get an Editor: new
	// pipeline dispatch is refused while the queue is full (Inv. 18).
	res := f.mcOK("", "task", "add", "blocked-at-cap task", "--worksource", worksource,
		"--description", "must stay proposed until the queue drains")
	blockedID := int64(res["task_id"].(float64))

	// The auto-Refiner spawns on a packaged non-saturated packet at cap.
	f.waitFor(90*time.Second, "a Refiner spawns at cap", func() (bool, string) {
		for _, r := range f.runs() {
			if r["role"] == "refiner" {
				return true, ""
			}
		}
		return false, "no refiner run yet"
	})

	// Re-entry advances at cap: a re-entered task cycles back through the
	// Worker, so worker runs exceed the three initial passes.
	f.waitFor(90*time.Second, "a re-entered task advances through the Worker at cap", func() (bool, string) {
		workers := 0
		for _, r := range f.runs() {
			if r["role"] == "worker" {
				workers++
			}
		}
		return workers > 3, fmt.Sprintf("%d worker runs", workers)
	})

	// Even after a Refiner has re-entered work at cap, the fourth task is still
	// proposed and no fourth packet was born (the WIP cap holds).
	if st := f.mcOK("", "task", "get", fmt.Sprint(blockedID))["status"]; st != "proposed" {
		t.Fatalf("blocked task %d left proposed (%v) while the queue was at cap — the WIP gate failed", blockedID, st)
	}
	if n := countUnarchivedPackets(f); n != 3 {
		t.Fatalf("unarchived packets = %d at cap, want exactly 3 (a fourth packet was born past the WIP cap)", n)
	}

	// Drain the queue: cancel the three packets (a live packet cancels
	// regardless of where its task sits in the churn cycle).
	for _, id := range capIDs {
		if got := f.mcRun("", "packet", "decide", fmt.Sprint(id), "--cancel", "--reason", "drain the queue"); got.code != 0 {
			t.Fatalf("cancel packet for task %d exited %d: %s", id, got.code, got.stderr)
		}
	}

	// With the queue drained, the previously-blocked task finally advances all
	// the way to packaged — proving the cap, not another stall, was the block.
	f.waitForTaskStatus(blockedID, "packaged", 120*time.Second)
}

func countUnarchivedPackets(f *fixture) int {
	n := 0
	for _, p := range f.packets() {
		if p["archived"].(float64) == 0 {
			n++
		}
	}
	return n
}

// addResidentRoleBehavior patches resident.json to map a role to a behavior
// file (setup() writes no refiner role by default).
func addResidentRoleBehavior(t *testing.T, f *fixture, role, behaviorPath string) {
	t.Helper()
	patchResidentConfig(t, f, func(cfg map[string]any) {
		cfg["roleBehaviors"].(map[string]any)[role] = behaviorPath
	})
}

// patchResidentConfig reads resident.json, applies mutate, and writes it back.
// Must run after setup() (which writes the config) and before startResident().
func patchResidentConfig(t *testing.T, f *fixture, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(f.base, "resident.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	mutate(cfg)
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}
