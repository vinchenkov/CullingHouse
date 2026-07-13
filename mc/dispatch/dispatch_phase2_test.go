package dispatch

// Phase 2 wave-1 decision-table coverage — the docs/phase2-contract.md §4.2
// gap list, closed as tests only (builder A; dispatch.go is frozen,
// phase1b-contract §3). Numbering below follows the contract's list.
// Fixture helpers live in dispatch_test.go (the promoted S6 suite).

import (
	"reflect"
	"testing"
	"time"
)

func worksource(name string) tmod { return func(t *Task) { t.Worksource = name } }

// decideCfg mirrors decide() but with an explicit Config and Clock, for the
// console-schedule cases.
func decideCfg(t *testing.T, rec Records, lock Lock, cfg Config, clk Clock) Action {
	t.Helper()
	a := Decide(rec, lock, cfg, clk)
	assertWellFormed(t, a)
	return a
}

func consoleCfg(hour, min int, loc *time.Location) Config {
	cfg := DefaultConfig()
	cfg.ConsoleHour = hour
	cfg.ConsoleMinute = min
	cfg.ConsoleLoc = loc
	return cfg
}

// zoneWest is a fixed UTC-5 zone: deterministic, no tzdata dependence, and
// its calendar day genuinely diverges from UTC's around midnight.
var zoneWest = time.FixedZone("UTC-5", -5*60*60)

// ---------------------------------------------------------------------------
// Gap 1 — reap-reason precedence under simultaneous thresholds. dispatch.go
// fixes the check order watchdog → timeout → hard-deadline; the reap outcome
// is identical either way (assert the payload equal modulo Reason).
// ---------------------------------------------------------------------------

func TestStep0_SimultaneousThresholds_ReasonPrecedence_OutcomeIdentical(t *testing.T) {
	rec := recs([]Task{tk(1, StatusSeeded)}, nil)

	// (a) never-heartbeated + past hard deadline → spawn-watchdog wins.
	wd := Lock{
		Held: true, RunID: "run-x", Owner: "worker", SubjectID: pi64(1),
		AcquiredAt:     base.Add(-2 * time.Hour), // far past spawn_grace_s
		HardDeadlineAt: base.Add(-time.Minute),   // hard deadline also past
	}
	aw := decide(t, rec, wd)
	if aw.Kind != KindReap || aw.Reap.Reason != ReapSpawnWatchdog {
		t.Fatalf("never-heartbeated + past deadline: want reap(spawn-watchdog), got %+v", aw)
	}

	// (b) heartbeated-stale + past hard deadline → lease-timeout wins.
	to := wd
	to.LastHeartbeatAt = pt(base.Add(-2 * time.Hour)) // silent far past timeout+grace
	al := decide(t, rec, to)
	if al.Kind != KindReap || al.Reap.Reason != ReapLeaseTimeout {
		t.Fatalf("stale-heartbeat + past deadline: want reap(lease-timeout), got %+v", al)
	}

	// The reap payload is identical modulo Reason: same run, same charge,
	// same block decision, same stop effect.
	nw, nt := *aw.Reap, *al.Reap
	nw.Reason, nt.Reason = "", ""
	if !reflect.DeepEqual(nw, nt) {
		t.Fatalf("reap outcome must be identical modulo Reason:\nwatchdog: %+v\ntimeout:  %+v", nw, nt)
	}
}

// ---------------------------------------------------------------------------
// Gap 2 — hard-deadline boundary not-taken: one second before
// hard_deadline_at with a fresh heartbeat → idle lease-held.
// ---------------------------------------------------------------------------

func TestStep0_HardDeadline_OneSecondEarly_NotTaken(t *testing.T) {
	lock := Lock{
		Held: true, RunID: "run-x", SubjectID: pi64(1),
		AcquiredAt:      base.Add(-3 * time.Hour),
		LastHeartbeatAt: pt(base.Add(-30 * time.Second)), // fresh
		HardDeadlineAt:  base.Add(time.Second),           // now is 1s before it
	}
	a := decide(t, recs([]Task{tk(1, StatusSeeded)}, nil), lock)
	if a.Kind != KindIdle || a.Idle != IdleLeaseHeld {
		t.Fatalf("1s before hard_deadline_at, heartbeat fresh: want idle(lease-held), got %+v", a)
	}
}

// ---------------------------------------------------------------------------
// Gap 3 — reap with DispatchRetries already 0 → BlockSubject still set
// (the -1 <= 0 clamp edge; §10 "never a silent loop").
// ---------------------------------------------------------------------------

func TestStep0_ReapWithRetriesAlreadyZero_StillBlocks(t *testing.T) {
	lock := Lock{
		Held: true, RunID: "run-x", SubjectID: pi64(1),
		AcquiredAt:     base.Add(-2 * time.Minute), // never heartbeated → watchdog
		HardDeadlineAt: base.Add(2 * time.Hour),
	}
	a := decide(t, recs([]Task{tk(1, StatusSeeded, retries(0))}, nil), lock)
	if a.Kind != KindReap {
		t.Fatalf("want reap, got %+v", a)
	}
	if !a.Reap.ChargeRetries || !a.Reap.BlockSubject {
		t.Fatalf("retries 0 → -1 clamps to blocked, never a silent loop: %+v", a.Reap)
	}
}

// ---------------------------------------------------------------------------
// Gap 4 — the console schedule in a non-UTC ConsoleLoc: delivery is computed
// in the stored zone while Clock.Now is UTC; same-day suppression keys on the
// *local* calendar day (§16.3 "delivery time + IANA timezone").
// ---------------------------------------------------------------------------

func TestStep0b_NonUTCZone_DueAfterLocalDelivery(t *testing.T) {
	rec := Records{Tasks: []Task{tk(1, StatusSeeded)}, LastBriefingAt: nil}
	// 13:30 UTC = 08:30 in UTC-5 → past the 08:00 local delivery → due.
	a := decideCfg(t, rec, freeLock(), consoleCfg(8, 0, zoneWest),
		clock(time.Date(2026, 7, 1, 13, 30, 0, 0, time.UTC)))
	wantSpawn(t, a, RoleStrategistConsole, nil)
}

func TestStep0b_NonUTCZone_NotDueBeforeLocalDelivery(t *testing.T) {
	rec := Records{Tasks: []Task{tk(1, StatusSeeded)}, LastBriefingAt: nil}
	// 12:30 UTC = 07:30 in UTC-5. A UTC-keyed computation would have fired
	// (12:30 > 08:00) — the stored zone says not yet.
	a := decideCfg(t, rec, freeLock(), consoleCfg(8, 0, zoneWest),
		clock(time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)))
	wantSpawn(t, a, RoleWorker, pi64(1))
}

func TestStep0b_NonUTCZone_SameLocalDaySuppression_AcrossUTCDays(t *testing.T) {
	// now: July 2 03:00 UTC = July 1 22:00 local; briefed July 1 14:00 UTC =
	// July 1 09:00 local. Different UTC calendar days, same *local* day →
	// suppressed. (UTC-keyed suppression would re-fire here.)
	rec := Records{
		Tasks:          []Task{tk(1, StatusSeeded)},
		LastBriefingAt: pt(time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC)),
	}
	a := decideCfg(t, rec, freeLock(), consoleCfg(8, 0, zoneWest),
		clock(time.Date(2026, 7, 2, 3, 0, 0, 0, time.UTC)))
	wantSpawn(t, a, RoleWorker, pi64(1))
}

func TestStep0b_NonUTCZone_DifferentLocalDay_SameUTCDay_Due(t *testing.T) {
	// now: July 1 23:00 UTC = July 1 18:00 local; briefed July 1 02:00 UTC =
	// June 30 21:00 local. Same UTC day, *previous* local day → due today.
	rec := Records{
		Tasks:          []Task{tk(1, StatusSeeded)},
		LastBriefingAt: pt(time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC)),
	}
	a := decideCfg(t, rec, freeLock(), consoleCfg(8, 0, zoneWest),
		clock(time.Date(2026, 7, 1, 23, 0, 0, 0, time.UTC)))
	wantSpawn(t, a, RoleStrategistConsole, nil)
}

func TestStep0b_ConsoleMinuteNonZero_BoundaryAtDeliveryMinute(t *testing.T) {
	rec := Records{Tasks: []Task{tk(1, StatusSeeded)}, LastBriefingAt: nil}
	cfg := consoleCfg(8, 30, time.UTC)
	// 08:29:59 → not due.
	a := decideCfg(t, rec, freeLock(), cfg,
		clock(time.Date(2026, 7, 1, 8, 29, 59, 0, time.UTC)))
	wantSpawn(t, a, RoleWorker, pi64(1))
	// 08:30:00 exactly → due (a threshold fires at its boundary instant).
	a = decideCfg(t, rec, freeLock(), cfg,
		clock(time.Date(2026, 7, 1, 8, 30, 0, 0, time.UTC)))
	wantSpawn(t, a, RoleStrategistConsole, nil)
}

func TestStep0b_NilConsoleLocDefaultsToUTC(t *testing.T) {
	rec := Records{Tasks: []Task{tk(1, StatusSeeded)}, LastBriefingAt: nil}
	a := decideCfg(t, rec, freeLock(), consoleCfg(8, 0, nil), noonClock())
	wantSpawn(t, a, RoleStrategistConsole, nil)
}

// ---------------------------------------------------------------------------
// Gap 5 — briefed yesterday *after* delivery time → still due today:
// staleness is calendar-day, not 24 h.
// ---------------------------------------------------------------------------

func TestStep0b_BriefedYesterdayAfterDelivery_DueToday(t *testing.T) {
	rec := Records{
		Tasks:          []Task{tk(1, StatusSeeded)},
		LastBriefingAt: pt(base.Add(-16 * time.Hour)), // yesterday 20:00, well after 08:00
	}
	a := decide(t, rec, freeLock()) // noon today, only 16 h later
	wantSpawn(t, a, RoleStrategistConsole, nil)
}

// Extra (not on the contract's list; same clause): a briefing earlier *today*
// but before today's delivery time still suppresses — suppression is purely
// "a same-day daily.briefing event exists" (§10 step 0b), not
// "delivered since today's delivery instant".
func TestStep0b_BriefedTodayBeforeDelivery_Suppressed(t *testing.T) {
	rec := Records{
		Tasks:          []Task{tk(1, StatusSeeded)},
		LastBriefingAt: pt(time.Date(2026, 7, 1, 7, 0, 0, 0, time.UTC)), // 07:00 < 08:00
	}
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleWorker, pi64(1))
}

// ---------------------------------------------------------------------------
// Gap 6 — landing-pending occupancy composite (§10 step 0c "still counts
// toward queue occupancy"): an approved+branch+blocked task (landing held)
// whose packet is 1 of 3 unarchived produces *no* land effect, yet its packet
// keeps occupancy at cap — the tick goes to (2a)/(2b), not step (3). The
// at-cap mode itself is the assertion: if the held packet were invisible to
// occupancy, the queue would have room and the verified row would win (3).
// ---------------------------------------------------------------------------

func TestOccupancy_BlockedLandingPendingPacketStillCounts(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusPackaged, decided(DecisionApproved, at(-30)), branch("b1", "s1"), blocked()),
		tk(2, StatusPackaged), tk(3, StatusPackaged),
		tk(4, StatusVerified), // would win step (3) if the queue had room
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	// Not KindLand (blocked landing held, NOTE(S6.3)); not Packager(4)
	// (at cap); (2a) empty → (2b) starts refinement on the best undecided
	// packet — packet 2 (task 1's packet is decided, invisible to (2b)).
	wantSpawn(t, a, RoleRefiner, pi64(2))
}

// ---------------------------------------------------------------------------
// Gap 7 — (0c) tie order pinned directly: equal decided_at → lower id lands
// first (NOTE(S6.4) key), independent of slice order.
// ---------------------------------------------------------------------------

func TestStep0c_TieOnDecidedAt_LowerIDLandsFirst(t *testing.T) {
	rec := recs([]Task{
		tk(9, StatusPackaged, decided(DecisionApproved, at(-10)), branch("b9", "s9")),
		tk(4, StatusPackaged, decided(DecisionApproved, at(-10)), branch("b4", "s4")),
	}, []Packet{pk(9), pk(4)})
	a := decide(t, rec, freeLock())
	if a.Kind != KindLand || a.Land.TaskID != 4 {
		t.Fatalf("decided_at tie → lower id lands first: want land(4), got %+v", a)
	}
}

// ---------------------------------------------------------------------------
// Gap 8 — (2a) archived-packet link invisible: a task whose only packet link
// is an *archived* packet would win (2a) by rank — it must be invisible, and
// the lower-ranked row with a live link is selected instead.
// ---------------------------------------------------------------------------

func TestStep2a_ArchivedPacketLinkInvisible_LiveLinkWins(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusWorked), // rank 3 — would win via its (archived) packet link
		tk(2, StatusSeeded), // rank 2 — holds a live packet link
		tk(3, StatusPackaged), tk(4, StatusPackaged),
	}, []Packet{pk(1, pArchived()), pk(2), pk(3), pk(4)}) // occupancy 3 = at cap
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleWorker, pi64(2))
}

// ---------------------------------------------------------------------------
// Gap 9 — (2a) has no expedite partition (NOTE(S6.6)): at cap, a P-1
// low-rank in-flight row loses to a P2 higher-rank one — the expedite prefix
// exists only in the with-room query (3), taken literally.
// ---------------------------------------------------------------------------

func TestStep2a_NoExpeditePartition_HigherRankBeatsExpedite(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusSeeded, prio(-1)), // expedited, rank 2, live packet
		tk(2, StatusWorked, prio(2)),  // P2, rank 3, live packet
		tk(3, StatusPackaged),
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleVerifier, pi64(2))
}

// ---------------------------------------------------------------------------
// Gap 10 — (2a) re-entered *initiative* (drained) at cap: the arc-packet
// round-trip (§8 move 1). An initiative that re-entered seeded, holding its
// own live packet (the p.task_id = t.id arm), with only archived children
// (drained — archived children do not park), spawns Strategist(initiative)
// through (2a)'s role table.
// ---------------------------------------------------------------------------

func TestStep2a_ReenteredDrainedInitiativeAtCap_SpawnsStrategistInitiative(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusSeeded, initiative()), // re-entered arc, own live packet
		tk(10, StatusSeeded, childOf(1), archived(), decided(DecisionCancelled, at(-5))),
		tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleStrategistInitiative, pi64(1))
}

// ---------------------------------------------------------------------------
// Gap 11 — (3) Editor pool excludes archived proposals: a rejected+archived
// proposal must not appear in ProposedPool while a live proposal dispatches
// the Editor (snapshot honesty; ADR-001 D4's coverage check depends on it).
// The archived row sits between the live ids to pin ascending order too.
// ---------------------------------------------------------------------------

func TestStep3_ProposedPoolExcludesArchivedRejectedProposal(t *testing.T) {
	rec := recs([]Task{
		tk(8, StatusProposed),
		tk(2, StatusProposed, archived(), decided(DecisionRejected, at(-10))),
		tk(5, StatusProposed),
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleEditor, pi64(5)) // older created_at (id-correlated) wins (3)
	if want := []int64{5, 8}; !reflect.DeepEqual(a.Spawn.ProposedPool, want) {
		t.Fatalf("pool must exclude the archived proposal and stay ascending: want %v, got %v",
			want, a.Spawn.ProposedPool)
	}
}

// ---------------------------------------------------------------------------
// Gap 12 — (4) dedupe tie order: equal decided_at → id DESC (NOTE(S6.1) pin).
// ---------------------------------------------------------------------------

func TestStep4_DedupeTieOrder_EqualDecidedAt_IDDesc(t *testing.T) {
	rec := recs([]Task{
		tk(3, StatusProposed, archived(), decided(DecisionRejected, at(-10))),
		tk(7, StatusProposed, archived(), decided(DecisionRejected, at(-10))),
		tk(5, StatusProposed, archived(), decided(DecisionRejected, at(-10))),
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleStrategistPropose, nil)
	if want := []string{"task-7", "task-5", "task-3"}; !reflect.DeepEqual(a.Spawn.DedupeTitles, want) {
		t.Fatalf("decided_at tie → id DESC: want %v, got %v", want, a.Spawn.DedupeTitles)
	}
}

// ---------------------------------------------------------------------------
// Gap 13 — occupancy is global across Worksources (Inv. 18): three unarchived
// packets on three different Worksources cap a fourth Worksource's dispatch
// into refinement mode.
// ---------------------------------------------------------------------------

func TestOccupancy_GlobalAcrossWorksources(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusPackaged, worksource("ws-a")),
		tk(2, StatusPackaged, worksource("ws-b")),
		tk(3, StatusPackaged, worksource("ws-c")),
		tk(4, StatusVerified, worksource("ws-d")), // would win (3) with room
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(1)) // at cap globally → refinement mode
}

// ---------------------------------------------------------------------------
// Takeover finding — §10 step (0c) carries no Worksource-status qualifier and
// §7 pins "landing latency is at most one tick". Pause/archive make work
// invisible to NEW selection, but an operator-approved landing-pending row
// must still land: archive is terminal and no unpause verb exists, so a
// gated landing would strand approved work forever while its packet burns an
// Inv. 18 review slot permanently.
// ---------------------------------------------------------------------------

func TestStep0c_LandingProceedsRegardlessOfWorksourceStatus(t *testing.T) {
	for _, status := range []string{"paused", "archived"} {
		one := tk(1, StatusPackaged, decided(DecisionApproved, at(-10)), branch("b1", "s1"))
		one.WorksourceStatus = status
		rec := recs([]Task{one}, []Packet{pk(1)})
		a := decide(t, rec, freeLock())
		if a.Kind != KindLand || a.Land.TaskID != 1 {
			t.Fatalf("%s Worksource held an approved landing: got %+v", status, a)
		}
	}
}

func TestStep0c_StrandedLandingsCannotSaturateTheQueueForever(t *testing.T) {
	held := func(id int64) Task {
		one := tk(id, StatusPackaged, decided(DecisionApproved, at(-10-int(id))),
			branch("b", "s"))
		one.WorksourceStatus = "archived"
		return one
	}
	rec := recs([]Task{
		held(1), held(2), held(3),
		tk(4, StatusVerified), // active Worksource work waiting on the cap
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	// One held row lands per tick (oldest decided_at first); the cap drains
	// instead of wedging the whole system at idle/queue-saturated.
	if a.Kind != KindLand || a.Land.TaskID != 3 {
		t.Fatalf("archived-Worksource landings wedged the queue: got %+v", a)
	}
}
