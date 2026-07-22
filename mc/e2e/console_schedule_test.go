//go:build docker_e2e

package e2e

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// TestConsoleScheduleDockerBoundary is Phase 4 scenario family (6)'s console
// schedule sub-test: at the configured delivery time the timer-driven tick
// spawns Strategist(console) (dispatch step 0b, consoleDue), which publishes
// the Daily Review Console — recording a daily.briefing and fanning a `console`
// outbox row to the dashboard surface (§14) — and it fires exactly once per day
// (LastBriefingAt is the last daily.briefing event, so the guard re-arms only
// the next day).
//
// The lever: set the delivery time to 00:00 so consoleDue is due on the first
// tick (deterministic, no wall-clock-minute race).
func TestConsoleScheduleDockerBoundary(t *testing.T) {
	f := setup(t, withSeededSpine(func(f *fixture, db *sql.DB) {
		if _, err := db.Exec(`UPDATE lock SET console_hour = 0, console_minute = 0 WHERE id = 1`); err != nil {
			t.Fatal(err)
		}
	}))

	// Strategist(console) terminal: publish the console packet to the dashboard.
	writeBehaviorFile(t, f, "strategist-console.json", behaviorSteps(
		execStep(`mc console publish --run "$MC_RUN_ID" --content "outputs/console.md"`),
		succeedStep("console published"),
	))
	addResidentRoleBehavior(t, f, "strategist(console)", "/mc/behaviors/strategist-console.json")

	f.startResident()

	// The scheduled console fans a `console` row to the dashboard surface.
	f.waitFor(90*time.Second, "the console publishes to the dashboard surface", func() (bool, string) {
		return countDashboardConsoleRows(f) >= 1, "no console outbox row yet"
	})

	// It fires exactly once — the same-day guard holds across further ticks.
	time.Sleep(3 * time.Second)
	if n := countDashboardConsoleRows(f); n != 1 {
		t.Fatalf("console outbox rows = %d, want exactly 1 (once-per-day guard, §14)", n)
	}

	// The row is a real, ackable delivery: poll it, ack it, and it stops
	// appearing (at-least-once until acked, §15.5).
	poll := f.mcOK("", "outbox", "poll", "--surface", "dashboard", "--limit", "100")
	var rowID int64
	for _, r := range poll["rows"].([]any) {
		row := r.(map[string]any)
		if row["kind"] == "console" {
			rowID = int64(row["id"].(float64))
		}
	}
	if rowID == 0 {
		t.Fatalf("no console row in the dashboard poll: %v", poll["rows"])
	}
	if got := f.mcRun("", "outbox", "ack", fmt.Sprint(rowID), "--surface", "dashboard"); got.code != 0 {
		t.Fatalf("mc outbox ack exited %d: %s", got.code, got.stderr)
	}
	if n := countDashboardConsoleRows(f); n != 0 {
		t.Fatalf("acked console row still polls (%d); ack must mark it delivered (§15.5)", n)
	}
}

func countDashboardConsoleRows(f *fixture) int {
	poll := f.mcOK("", "outbox", "poll", "--surface", "dashboard", "--limit", "100")
	n := 0
	for _, r := range poll["rows"].([]any) {
		if r.(map[string]any)["kind"] == "console" {
			n++
		}
	}
	return n
}
