//go:build docker_e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLandingFailureAndRecoveryDockerBoundary is Phase 4 scenario family (1)'s
// landing-FAILURE variant: an approved task whose landing REFUSES leaves the
// task in a recoverable pending state (blocked, still packaged/approved,
// unmerged), and an operator unblock after the obstruction is cleared re-lands
// it. TestWalkingSkeleton covers the landing SUCCESS job; this covers the
// refusal-and-recovery half of the approve/land contract (§7 failure protocol).
//
// The refusal is deterministic and meaningful: mc-land protects operator bytes,
// so an untracked file in the primary checkout that the reviewed merge would
// add is refused before any merge is attempted (exit 77). The legacy land
// reports any nonzero mc-land exit as a failure, which blocks the task.
func TestLandingFailureAndRecoveryDockerBoundary(t *testing.T) {
	f := setup(t)

	res := f.mcOK("", "task", "add", "landing failure task", "--worksource", worksource,
		"--description", "drive to approved, refuse the landing, then recover")
	taskID := int64(res["task_id"].(float64))
	branch := fmt.Sprintf("mc/task-%d", taskID)
	worktreeDir := filepath.Join(f.ws, ".mc-worktrees", fmt.Sprintf("task-%d", taskID))
	mainBefore := f.git("rev-parse", "main")

	f.startResident()

	// The pipeline runs itself under the timer; the intermediate ladders are
	// TestWalkingSkeleton's to assert — here we only need it at packaged.
	f.waitForTaskStatus(taskID, "packaged", 120*time.Second)
	workedSHA := f.git("rev-parse", "refs/heads/"+branch)

	// The obstruction: an untracked file in the primary checkout at the exact
	// path the reviewed branch adds. mc-land refuses to clobber it (exit 77).
	collision := filepath.Join(f.ws, "skeleton.txt")
	if err := os.WriteFile(collision, []byte("operator bytes mc-land must not erase\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := f.mcRun("", "packet", "decide", fmt.Sprint(taskID), "--approve"); got.code != 0 {
		t.Fatalf("mc packet decide --approve exited %d: %s", got.code, got.stderr)
	}

	// The landing must refuse and BLOCK the task — not archive it, not merge.
	f.waitFor(60*time.Second, "landing refusal blocks the task", func() (bool, string) {
		task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
		if task["archived"].(float64) == 1 {
			return false, "task ARCHIVED — the obstructed landing should have refused, not merged"
		}
		if task["blocked"].(float64) != 1 {
			return false, fmt.Sprintf("not blocked yet: %v", task["blocked_reason"])
		}
		return true, ""
	})

	task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
	if reason, _ := task["blocked_reason"].(string); reason == "" || !strings.Contains(reason, "mc-land exited") {
		t.Fatalf("blocked_reason = %q, want the mc-land failure reason (§7 failure protocol)", task["blocked_reason"])
	}
	if task["status"] != "packaged" || task["decision"] != "approved" {
		t.Fatalf("a refused landing must retain status=packaged decision=approved, got status=%v decision=%v", task["status"], task["decision"])
	}
	if now := f.git("rev-parse", "main"); now != mainBefore {
		t.Fatalf("main advanced to %s despite the refused landing (want unmoved %s)", now, mainBefore)
	}
	// Recoverable: the branch and worktree survive a refusal, so the retry has
	// the reviewed commit to land.
	if got := f.git("rev-parse", "refs/heads/"+branch); got != workedSHA {
		t.Fatalf("reviewed branch %s changed/vanished after refusal (%s)", branch, got)
	}
	if _, err := os.Stat(worktreeDir); err != nil {
		t.Fatalf("worktree %s removed after a refused landing: %v (not recoverable)", worktreeDir, err)
	}
	f.waitFor(10*time.Second, "lock free after the refused landing", func() (bool, string) {
		if lock := f.mcOK("", "lock", "get"); lock["run_id"] != nil {
			return false, fmt.Sprintf("held by %v", lock["owner"])
		}
		return true, ""
	})

	// A refused landing does not auto-retry (blocked gates the land effect);
	// the trigger is the ordinary operator unblock, once the obstruction is
	// cleared.
	time.Sleep(2 * time.Second)
	if task := f.mcOK("", "task", "get", fmt.Sprint(taskID)); task["archived"].(float64) == 1 {
		t.Fatal("a blocked landing auto-retried and archived; it must wait for an operator unblock")
	}

	// ── Recovery: clear the obstruction, unblock, and the next tick lands ──
	if err := os.Remove(collision); err != nil {
		t.Fatal(err)
	}
	if got := f.mcRun("", "task", "unblock", fmt.Sprint(taskID)); got.code != 0 {
		t.Fatalf("mc task unblock exited %d: %s", got.code, got.stderr)
	}
	f.waitFor(60*time.Second, "the unblocked task lands and archives", func() (bool, string) {
		task := f.mcOK("", "task", "get", fmt.Sprint(taskID))
		if task["archived"].(float64) != 1 {
			return false, fmt.Sprintf("archived=%v blocked=%v (%v)", task["archived"], task["blocked"], task["blocked_reason"])
		}
		return true, ""
	})
	mainAfter := f.git("rev-parse", "main")
	if mainAfter == mainBefore {
		t.Fatal("main did not advance after the recovery landing")
	}
	if p1 := f.git("rev-parse", "main^1"); p1 != mainBefore {
		t.Fatalf("recovery merge first parent = %s, want prior main %s (--no-ff)", p1, mainBefore)
	}
	if p2 := f.git("rev-parse", "main^2"); p2 != workedSHA {
		t.Fatalf("recovery merge second parent = %s, want verified_sha %s (§7)", p2, workedSHA)
	}
	if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
		t.Fatalf("worktree %s still present after the recovery landing (§7 step 3)", worktreeDir)
	}
}
