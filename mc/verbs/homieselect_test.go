package verbs

import (
	"testing"
	"time"
)

func TestSelectHomieWake(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	idle := 30 * time.Minute
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	t.Run("a non-idle no-launch session with pending inbound gets a fresh wake", func(t *testing.T) {
		d := selectHomieWake([]homieSchedRow{
			{SessionID: "s1", LastActivityAt: ago(time.Minute), PendingInput: true},
		}, now, idle)
		if d.Kind != homieWakeSpawn || d.SessionID != "s1" || d.LaunchMode != "fresh" {
			t.Fatalf("decision = %+v, want fresh spawn of s1", d)
		}
	})

	t.Run("resume debt (no inbound) wakes in the recorded resume mode", func(t *testing.T) {
		d := selectHomieWake([]homieSchedRow{
			{SessionID: "s1", LastActivityAt: ago(time.Minute), ResumeOwed: true, ResumeMode: "rows"},
		}, now, idle)
		if d.Kind != homieWakeSpawn || d.LaunchMode != "rows" {
			t.Fatalf("decision = %+v, want a rows-mode resume wake", d)
		}
	})

	t.Run("a session started but never messaged is not eligible", func(t *testing.T) {
		d := selectHomieWake([]homieSchedRow{
			{SessionID: "s1", LastActivityAt: ago(time.Minute)},
		}, now, idle)
		if d.Kind != homieWakeNone {
			t.Fatalf("decision = %+v, want none (no traffic)", d)
		}
	})

	t.Run("a launched session is skipped by the happy-case selector", func(t *testing.T) {
		d := selectHomieWake([]homieSchedRow{
			{SessionID: "s1", LastActivityAt: ago(time.Minute), PendingInput: true, HasLaunch: true, LaunchMode: "fresh"},
		}, now, idle)
		if d.Kind != homieWakeNone {
			t.Fatalf("decision = %+v, want none (already launched — recovery is deferred)", d)
		}
	})

	t.Run("idle end wins over a spawn and picks the oldest idle row", func(t *testing.T) {
		d := selectHomieWake([]homieSchedRow{
			{SessionID: "old-idle", LastActivityAt: ago(2 * time.Hour)},
			{SessionID: "new-idle", LastActivityAt: ago(40 * time.Minute)},
			{SessionID: "live", LastActivityAt: ago(time.Minute), PendingInput: true},
		}, now, idle)
		if d.Kind != homieWakeIdle || d.SessionID != "old-idle" {
			t.Fatalf("decision = %+v, want idle-end of old-idle", d)
		}
	})

	t.Run("threshold equality is idle (belongs to branch 6)", func(t *testing.T) {
		d := selectHomieWake([]homieSchedRow{
			{SessionID: "s1", LastActivityAt: ago(idle)},
		}, now, idle)
		if d.Kind != homieWakeIdle {
			t.Fatalf("decision = %+v, want idle-end at exact threshold", d)
		}
	})

	t.Run("among eligible spawns the oldest by activity wins", func(t *testing.T) {
		d := selectHomieWake([]homieSchedRow{
			{SessionID: "older", LastActivityAt: ago(10 * time.Minute), PendingInput: true},
			{SessionID: "newer", LastActivityAt: ago(2 * time.Minute), PendingInput: true},
		}, now, idle)
		if d.SessionID != "older" {
			t.Fatalf("decision = %+v, want the oldest eligible spawn", d)
		}
	})

	t.Run("nothing to do returns none", func(t *testing.T) {
		if d := selectHomieWake(nil, now, idle); d.Kind != homieWakeNone {
			t.Fatalf("empty projection = %+v, want none", d)
		}
	})
}
