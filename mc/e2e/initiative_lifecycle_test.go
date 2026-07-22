//go:build docker_e2e

package e2e

import (
	"fmt"
	"testing"
	"time"
)

// TestInitiativeLifecycleDockerBoundary is Phase 4 scenario family (4): a full
// initiative control loop through a REAL merge to main (ADR-023). Charter →
// Editor promotes (Promote cuts the shared branch mc/initiative-<id>) →
// Strategist(initiative) births a wave → Editor plan-review passes it →
// children commit into the shared worktree and archive on approval (branchless,
// no per-child merge) → strict drain → Strategist(initiative) declares done →
// arc verified → arc packet → approve → the legacy land merges the shared
// branch onto main, archives, deletes the worktree.
//
// This is the payoff of ADR-023: children are branchless so approving each one
// touches nothing on main, and only the arc row carries the shared branch and
// lands (Inv. 25).
func TestInitiativeLifecycleDockerBoundary(t *testing.T) {
	f := setup(t)

	// Editor pool review: promote whatever proposals are snapshotted (here,
	// just the initiative). Multi-verdict form handles the comma-separated pool.
	writeBehaviorFile(t, f, "editor.json", behaviorSteps(
		execStep(`set -e
ids=$(printf '%s' "$MC_POOL_IDS" | tr ',' ' ')
verdicts=""
for id in $ids; do
  verdicts="${verdicts}{\"task\":${id},\"decision\":\"promote\",\"reason\":\"init\"},"
done
printf '{"verdicts":[%s]}' "${verdicts%,}" | mc editor decide --run "$MC_RUN_ID" --batch -`),
		succeedStep("promoted"),
	))

	// Strategist(initiative): first spawn births one wave (marker absent); the
	// post-drain spawn declares the charter done (marker present). The marker
	// lives in the gitignored .mc-worktrees so it never collides with the merge.
	writeBehaviorFile(t, f, "strategist-initiative.json", behaviorSteps(
		execStep(`set -e
mkdir -p /workspace/source/.mc-worktrees
marker=/workspace/source/.mc-worktrees/.waved-$MC_SUBJECT_ID
if [ -f "$marker" ]; then
  mc complete "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --status worked --outputs "charter satisfied: the wave drained"
else
  touch "$marker"
  printf '{"children":[{"title":"child one","description":"first slice","priority":2},{"title":"child two","description":"second slice","priority":2}]}' | mc strategist wave --run "$MC_RUN_ID" --initiative "$MC_SUBJECT_ID" --batch -
fi`),
		succeedStep("planned"),
	))
	addResidentRoleBehavior(t, f, "strategist(initiative)", "/mc/behaviors/strategist-initiative.json")

	// Editor plan-review: pass the wave so its children become dispatchable.
	writeBehaviorFile(t, f, "plan-review.json", behaviorSteps(
		execStep(`mc editor plan-review --run "$MC_RUN_ID" --initiative "$MC_SUBJECT_ID" --verdict pass`),
		succeedStep("reviewed"),
	))
	addResidentRoleBehavior(t, f, "editor(plan-review)", "/mc/behaviors/plan-review.json")

	// Child Worker: commit into the ONE shared worktree/branch (created once),
	// then complete on the shared branch (validated, not stored — D3).
	writeBehaviorFile(t, f, "worker.json", behaviorSteps(
		execStep(`set -e
init=$(mc task get "$MC_SUBJECT_ID" | tr -d ' \n' | sed -n 's/.*"initiative_id":\([0-9][0-9]*\).*/\1/p')
: "${init:?no initiative_id on child}"
cd /workspace/source
wt=.mc-worktrees/initiative-$init
if [ ! -d "$wt" ]; then git worktree add "$wt" -b "mc/initiative-$init"; fi
cd "$wt"
echo "work of child $MC_SUBJECT_ID" > "child-$MC_SUBJECT_ID.txt"
git add "child-$MC_SUBJECT_ID.txt"
git -c user.name='mc worker' -c user.email='worker@mc.invalid' commit -q -m "child $MC_SUBJECT_ID"
mc complete "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --status worked --branch "mc/initiative-$init"`),
		succeedStep("worked"),
	))

	// Verifier: passes both a child (verify the shared-branch tip) and the arc
	// (scope=initiative). Neither is a refinement round here, so no --deepening.
	writeBehaviorFile(t, f, "verifier.json", behaviorSteps(
		execStep(`set -e
row=$(mc task get "$MC_SUBJECT_ID" | tr -d ' \n')
scope=$(printf '%s' "$row" | sed -n 's/.*"scope":"\([a-z]*\)".*/\1/p')
if [ "$scope" = "initiative" ]; then
  init=$MC_SUBJECT_ID
else
  init=$(printf '%s' "$row" | sed -n 's/.*"initiative_id":\([0-9][0-9]*\).*/\1/p')
fi
sha=$(git -C /workspace/source rev-parse "refs/heads/mc/initiative-$init")
ev=/tmp/ev-$MC_SUBJECT_ID.txt
printf 'gate pass\n' > "$ev"
mc verifier verdict "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --outcome pass --evidence "$ev" --sha "$sha"`),
		succeedStep("verified"),
	))
	// packager.json (default) renders both the child packets and the arc packet.

	res := f.mcOK("", "initiative", "add", "ship feature X",
		"--worksource", worksource, "--charter", "X works end to end and is documented")
	initID := int64(res["initiative_id"].(float64))
	mainBefore := f.git("rev-parse", "main")

	f.startResident()

	// The wave produces two child packets (task_id != initiative). Each child
	// is branchless, so approving it archives it with no merge (drain).
	var childIDs []int64
	f.waitFor(180*time.Second, "two wave-child packets appear", func() (bool, string) {
		childIDs = nil
		for _, p := range f.packets() {
			tid := int64(p["task_id"].(float64))
			if tid != initID && p["archived"].(float64) == 0 {
				childIDs = append(childIDs, tid)
			}
		}
		return len(childIDs) == 2, fmt.Sprintf("%d child packets so far", len(childIDs))
	})

	if now := f.git("rev-parse", "main"); now != mainBefore {
		t.Fatalf("main moved (%s) while children were only packaged — main must move only on the arc approval (Inv. 25)", now)
	}

	for _, cid := range childIDs {
		if got := f.mcRun("", "packet", "decide", fmt.Sprint(cid), "--approve"); got.code != 0 {
			t.Fatalf("approve child packet %d exited %d: %s", cid, got.code, got.stderr)
		}
	}
	// Approving a branchless child archives it synchronously — nothing merges.
	for _, cid := range childIDs {
		f.waitFor(30*time.Second, fmt.Sprintf("child %d archives on approval", cid), func() (bool, string) {
			return f.mcOK("", "task", "get", fmt.Sprint(cid))["archived"].(float64) == 1, "not archived yet"
		})
	}
	if now := f.git("rev-parse", "main"); now != mainBefore {
		t.Fatalf("main moved (%s) on a child approval — children must not land individually (ADR-023 D3)", now)
	}

	// Drained → done-declaration → arc verified → arc packet.
	f.waitForTaskStatus(initID, "packaged", 180*time.Second)

	// Approve the arc packet: the shared branch lands onto main.
	if got := f.mcRun("", "packet", "decide", fmt.Sprint(initID), "--approve"); got.code != 0 {
		t.Fatalf("approve arc packet exited %d: %s", got.code, got.stderr)
	}
	f.waitFor(90*time.Second, "the arc lands and the initiative archives", func() (bool, string) {
		task := f.mcOK("", "task", "get", fmt.Sprint(initID))
		if task["archived"].(float64) != 1 {
			return false, fmt.Sprintf("archived=%v blocked=%v (%v)", task["archived"], task["blocked"], task["blocked_reason"])
		}
		return true, ""
	})

	// The real merge: main advanced by a --no-ff merge whose second parent is
	// the shared branch, and both children's files are now on main.
	mainAfter := f.git("rev-parse", "main")
	if mainAfter == mainBefore {
		t.Fatal("main did not advance after the arc landing")
	}
	if p1 := f.git("rev-parse", "main^1"); p1 != mainBefore {
		t.Fatalf("arc merge first parent = %s, want prior main %s (--no-ff)", p1, mainBefore)
	}
	for _, cid := range childIDs {
		if _, err := f.gitErr("cat-file", "-e", fmt.Sprintf("main:child-%d.txt", cid)); err != nil {
			t.Fatalf("child %d's file is not on main after the arc landing: %v", cid, err)
		}
	}
	// The shared branch and its worktree are gone (§7 step 3).
	if _, err := f.gitErr("rev-parse", "--verify", fmt.Sprintf("refs/heads/mc/initiative-%d", initID)); err == nil {
		t.Fatalf("shared branch mc/initiative-%d still exists after landing", initID)
	}
}
