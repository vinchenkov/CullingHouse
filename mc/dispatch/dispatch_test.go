package dispatch

// The exhaustive decision-table test (spike S6; promoted whole into Phase 2).
//
// Coverage contract (implementation-handoff.md Part 2, S6):
//   A. every tick-walk step taken AND not-taken
//      (reconcile, console-if-due, landing, queue-at-cap, queue-with-room);
//   B. every invisibility rule exercised with a would-otherwise-win row;
//   C. deterministic tie-breaking asserted;
//   D. exactly zero-or-one action per evaluation, always (property suite);
//   E. the many-obligation precedence state asserted (the cascade test).

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fixture helpers.
// ---------------------------------------------------------------------------

// base: a fixed instant well after the 08:00 console delivery time.
var base = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

// briefedToday: a same-day daily.briefing event, so the console branch stays
// quiet unless a test wants it.
var briefedToday = base.Add(-3 * time.Hour) // 09:00 same day

func pi64(v int64) *int64       { return &v }
func pt(t time.Time) *time.Time { return &t }
func at(min int) time.Time      { return base.Add(time.Duration(min) * time.Minute) }
func clock(t time.Time) Clock   { return Clock{Now: t} }
func noonClock() Clock          { return clock(base) }

type tmod func(*Task)

func tk(id int64, st Status, mods ...tmod) Task {
	t := Task{
		ID:              id,
		Title:           fmt.Sprintf("task-%d", id),
		Scope:           ScopeTask,
		Priority:        1,
		CreatedAt:       base.Add(time.Duration(id) * time.Second), // deterministic, id-correlated default
		Status:          st,
		DispatchRetries: 3,
		Worksource:      "ws",
		TargetRef:       "main",
	}
	for _, m := range mods {
		m(&t)
	}
	return t
}

func initiative() tmod          { return func(t *Task) { t.Scope = ScopeInitiative } }
func childOf(id int64) tmod     { return func(t *Task) { t.InitiativeID = pi64(id) } }
func prio(p int) tmod           { return func(t *Task) { t.Priority = p } }
func created(tm time.Time) tmod { return func(t *Task) { t.CreatedAt = tm } }
func blocked() tmod             { return func(t *Task) { t.Blocked = true } }
func archived() tmod            { return func(t *Task) { t.Archived = true } }
func retries(n int) tmod        { return func(t *Task) { t.DispatchRetries = n } }
func decided(d TaskDecision, when time.Time) tmod {
	return func(t *Task) { t.Decision = d; t.DecidedAt = pt(when) }
}
func branch(name, sha string) tmod {
	return func(t *Task) { t.Branch = name; t.VerifiedSHA = sha }
}

type pmod func(*Packet)

func pk(taskID int64, mods ...pmod) Packet {
	p := Packet{TaskID: taskID, CreatedAt: base.Add(time.Duration(taskID) * time.Second)}
	for _, m := range mods {
		m(&p)
	}
	return p
}

func saturated() pmod            { return func(p *Packet) { p.Saturated = true } }
func pArchived() pmod            { return func(p *Packet) { p.Archived = true } }
func pCreated(tm time.Time) pmod { return func(p *Packet) { p.CreatedAt = tm } }

// recs builds Records with the console already delivered today, so tests
// exercise the branch they mean to.
func recs(tasks []Task, pkts []Packet) Records {
	return Records{Tasks: tasks, Packets: pkts, LastBriefingAt: pt(briefedToday)}
}

func freeLock() Lock { return Lock{} }

// freshLock: held, heartbeated 30s ago, hard deadline 2h out — not reapable.
func freshLock(subject *int64) Lock {
	return Lock{
		Held:            true,
		RunID:           "run-live",
		Owner:           "worker",
		SubjectID:       subject,
		AcquiredAt:      base.Add(-10 * time.Minute),
		LastHeartbeatAt: pt(base.Add(-30 * time.Second)),
		HardDeadlineAt:  base.Add(2 * time.Hour),
	}
}

func decide(t *testing.T, rec Records, lock Lock) Action {
	t.Helper()
	a := Decide(rec, lock, DefaultConfig(), noonClock())
	assertWellFormed(t, a)
	return a
}

// assertWellFormed asserts requirement D locally: every evaluation yields
// exactly one action or idle — exactly one payload, matching the kind.
func assertWellFormed(t *testing.T, a Action) {
	t.Helper()
	n := 0
	if a.Reap != nil {
		n++
	}
	if a.Spawn != nil {
		n++
	}
	if a.Land != nil {
		n++
	}
	if a.Reenter != nil {
		n++
	}
	switch a.Kind {
	case KindIdle:
		if n != 0 || a.Idle == "" {
			t.Fatalf("malformed idle action: %+v", a)
		}
	case KindReap:
		if n != 1 || a.Reap == nil {
			t.Fatalf("malformed reap action: %+v", a)
		}
	case KindSpawn:
		if n != 1 || a.Spawn == nil {
			t.Fatalf("malformed spawn action: %+v", a)
		}
	case KindLand:
		if n != 1 || a.Land == nil {
			t.Fatalf("malformed land action: %+v", a)
		}
	case KindReenter:
		if n != 1 || a.Reenter == nil {
			t.Fatalf("malformed reenter action: %+v", a)
		}
	default:
		t.Fatalf("unknown action kind %q", a.Kind)
	}
}

func wantSpawn(t *testing.T, a Action, role Role, subject *int64) {
	t.Helper()
	if a.Kind != KindSpawn {
		t.Fatalf("want spawn %s, got %+v", role, a)
	}
	if a.Spawn.Role != role {
		t.Fatalf("want role %s, got %s (%+v)", role, a.Spawn.Role, a)
	}
	switch {
	case subject == nil && a.Spawn.SubjectID != nil:
		t.Fatalf("want subjectless spawn, got subject %d", *a.Spawn.SubjectID)
	case subject != nil && (a.Spawn.SubjectID == nil || *a.Spawn.SubjectID != *subject):
		t.Fatalf("want subject %d, got %+v", *subject, a.Spawn)
	}
}

// ---------------------------------------------------------------------------
// A/0 — Step (0): reconcile the lease. Taken (three reap thresholds, budget
// outcomes, subjectless) and not-taken (fresh → idle; free → walk continues).
// ---------------------------------------------------------------------------

func TestStep0_FreshLeaseIdles_EvenWithEveryObligationWaiting(t *testing.T) {
	// Held-fresh lease returns idle even when console is due, a landing is
	// pending, and dispatchable rows exist: dispatch runs only when the lease
	// is free.
	rec := Records{
		Tasks: []Task{
			tk(1, StatusSeeded),
			tk(2, StatusPackaged, decided(DecisionApproved, at(-30)), branch("mc/task-2", "sha2")),
		},
		Packets:        []Packet{pk(2)},
		LastBriefingAt: nil, // console never delivered → due
	}
	a := decide(t, rec, freshLock(pi64(1)))
	if a.Kind != KindIdle || a.Idle != IdleLeaseHeld {
		t.Fatalf("want idle(lease-held), got %+v", a)
	}
}

func TestStep0_SpawnWatchdog_NeverHeartbeated(t *testing.T) {
	lock := Lock{
		Held: true, RunID: "run-x", SubjectID: pi64(1),
		AcquiredAt:     base.Add(-61 * time.Second), // spawn_grace_s = 60 → past
		HardDeadlineAt: base.Add(2 * time.Hour),
	}
	a := decide(t, recs([]Task{tk(1, StatusSeeded)}, nil), lock)
	if a.Kind != KindReap || a.Reap.Reason != ReapSpawnWatchdog {
		t.Fatalf("want reap(spawn-watchdog), got %+v", a)
	}
	if !a.Reap.StopContainer || a.Reap.RunID != "run-x" {
		t.Fatalf("reap must return the stop effect for the reaped run: %+v", a.Reap)
	}
	if !a.Reap.ChargeRetries || a.Reap.SubjectID == nil || *a.Reap.SubjectID != 1 {
		t.Fatalf("reap must charge the subject's dispatch_retries: %+v", a.Reap)
	}
	if a.Reap.BlockSubject {
		t.Fatalf("retries 3 → 2 must not block: %+v", a.Reap)
	}
}

func TestStep0_SpawnWatchdog_BoundaryOneSecondEarly_NotTaken(t *testing.T) {
	lock := Lock{
		Held: true, RunID: "run-x", SubjectID: pi64(1),
		AcquiredAt:     base.Add(-59 * time.Second),
		HardDeadlineAt: base.Add(2 * time.Hour),
	}
	a := decide(t, recs([]Task{tk(1, StatusSeeded)}, nil), lock)
	if a.Kind != KindIdle || a.Idle != IdleLeaseHeld {
		t.Fatalf("59s < spawn_grace_s: want idle(lease-held), got %+v", a)
	}
}

func TestStep0_LeaseTimeout_HeartbeatedThenSilent(t *testing.T) {
	lock := Lock{
		Held: true, RunID: "run-x", SubjectID: pi64(1),
		AcquiredAt:      base.Add(-3 * time.Hour),
		LastHeartbeatAt: pt(base.Add(-(75*time.Minute + time.Second))), // timeout 60 + grace 15 → past
		HardDeadlineAt:  base.Add(2 * time.Hour),
	}
	a := decide(t, recs([]Task{tk(1, StatusSeeded)}, nil), lock)
	if a.Kind != KindReap || a.Reap.Reason != ReapLeaseTimeout {
		t.Fatalf("want reap(lease-timeout), got %+v", a)
	}
}

func TestStep0_LeaseTimeout_HeartbeatJustInsideFullLease_NotTaken(t *testing.T) {
	lock := Lock{
		Held: true, RunID: "run-x", SubjectID: pi64(1),
		AcquiredAt:      base.Add(-3 * time.Hour),
		LastHeartbeatAt: pt(base.Add(-74 * time.Minute)),
		HardDeadlineAt:  base.Add(2 * time.Hour),
	}
	a := decide(t, recs([]Task{tk(1, StatusSeeded)}, nil), lock)
	if a.Kind != KindIdle || a.Idle != IdleLeaseHeld {
		t.Fatalf("silent 74m < 75m full lease: want idle, got %+v", a)
	}
}

func TestStep0_HardDeadline_ReapedWhileStillHeartbeating(t *testing.T) {
	lock := Lock{
		Held: true, RunID: "run-x", SubjectID: pi64(1),
		AcquiredAt:      base.Add(-3 * time.Hour),
		LastHeartbeatAt: pt(base.Add(-5 * time.Second)), // alive!
		HardDeadlineAt:  base.Add(-time.Second),         // but past the hard deadline
	}
	a := decide(t, recs([]Task{tk(1, StatusSeeded)}, nil), lock)
	if a.Kind != KindReap || a.Reap.Reason != ReapHardDeadline {
		t.Fatalf("liveness is not progress: want reap(hard-deadline), got %+v", a)
	}
}

func TestStep0_ReapExhaustsBudget_BlocksInsteadOfSilentLoop(t *testing.T) {
	lock := Lock{
		Held: true, RunID: "run-x", SubjectID: pi64(1),
		AcquiredAt:     base.Add(-2 * time.Minute),
		HardDeadlineAt: base.Add(2 * time.Hour),
	}
	// retries 1 → decrement lands at 0 → block with reason.
	a := decide(t, recs([]Task{tk(1, StatusSeeded, retries(1))}, nil), lock)
	if a.Kind != KindReap || !a.Reap.BlockSubject {
		t.Fatalf("dispatch_retries 1→0 must block the subject: %+v", a)
	}
	// retries 2 → lands at 1 → no block.
	a = decide(t, recs([]Task{tk(1, StatusSeeded, retries(2))}, nil), lock)
	if a.Kind != KindReap || a.Reap.BlockSubject {
		t.Fatalf("dispatch_retries 2→1 must not block: %+v", a)
	}
}

func TestStep0_ReapSubjectlessRun_ChargesNothing(t *testing.T) {
	lock := Lock{
		Held: true, RunID: "run-console", Owner: "strategist", SubjectID: nil,
		AcquiredAt:     base.Add(-2 * time.Minute),
		HardDeadlineAt: base.Add(2 * time.Hour),
	}
	a := decide(t, recs([]Task{tk(1, StatusSeeded)}, nil), lock)
	if a.Kind != KindReap {
		t.Fatalf("want reap, got %+v", a)
	}
	if a.Reap.ChargeRetries || a.Reap.SubjectID != nil || a.Reap.BlockSubject {
		t.Fatalf("subjectless reap must charge nothing: %+v", a.Reap)
	}
	if !a.Reap.StopContainer {
		t.Fatalf("subjectless reap still emits the stop effect: %+v", a.Reap)
	}
}

func TestStep0_NotTaken_FreeLockWalksOn(t *testing.T) {
	a := decide(t, recs([]Task{tk(1, StatusSeeded)}, nil), freeLock())
	wantSpawn(t, a, RoleWorker, pi64(1)) // reached step (3)
}

// ---------------------------------------------------------------------------
// A/0b — Step (0b): console-if-due. Taken and not-taken, and its precedence
// over landing / queue selection.
// ---------------------------------------------------------------------------

func TestStep0b_ConsoleDue_BeatsLandingAndQueue(t *testing.T) {
	rec := Records{
		Tasks: []Task{
			tk(1, StatusVerified), // dispatchable
			tk(2, StatusPackaged, decided(DecisionApproved, at(-30)), branch("mc/task-2", "sha2")),
		},
		Packets:        []Packet{pk(2)},
		LastBriefingAt: pt(base.Add(-24 * time.Hour)), // yesterday → due today
	}
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleStrategistConsole, nil)
}

func TestStep0b_ConsoleDue_NeverBriefed(t *testing.T) {
	rec := Records{Tasks: []Task{tk(1, StatusSeeded)}, LastBriefingAt: nil}
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleStrategistConsole, nil)
}

func TestStep0b_NotTaken_BeforeDeliveryTime(t *testing.T) {
	rec := Records{Tasks: []Task{tk(1, StatusSeeded)}, LastBriefingAt: nil}
	early := clock(time.Date(2026, 7, 1, 7, 59, 0, 0, time.UTC)) // 07:59 < 08:00
	a := Decide(rec, freeLock(), DefaultConfig(), early)
	assertWellFormed(t, a)
	wantSpawn(t, a, RoleWorker, pi64(1))
}

func TestStep0b_NotTaken_AlreadyDeliveredToday(t *testing.T) {
	a := decide(t, recs([]Task{tk(1, StatusSeeded)}, nil), freeLock())
	wantSpawn(t, a, RoleWorker, pi64(1))
}

func TestStep0b_ConsoleDueAtCap_StillFires(t *testing.T) {
	// 0b precedes the occupancy check: the console is not pipeline work and
	// is owed even when the queue is at cap.
	rec := Records{
		Tasks:          []Task{tk(1, StatusPackaged), tk(2, StatusPackaged), tk(3, StatusPackaged)},
		Packets:        []Packet{pk(1), pk(2), pk(3)},
		LastBriefingAt: nil,
	}
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleStrategistConsole, nil)
}

// ---------------------------------------------------------------------------
// A/0c — Step (0c): landing. Taken (payload, drain order) and not-taken.
// ---------------------------------------------------------------------------

func TestStep0c_LandingPending_ReturnsLandEffectAheadOfQueueSelection(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusVerified), // would win step (3) — must not be reached
		tk(2, StatusPackaged, decided(DecisionApproved, at(-30)), branch("mc/task-2", "sha-2")),
	}, []Packet{pk(2)})
	a := decide(t, rec, freeLock())
	if a.Kind != KindLand {
		t.Fatalf("want land effect ahead of queue selection, got %+v", a)
	}
	want := Land{TaskID: 2, Branch: "mc/task-2", VerifiedSHA: "sha-2", TargetRef: "main"}
	if *a.Land != want {
		t.Fatalf("land payload: want %+v, got %+v", want, *a.Land)
	}
}

func TestStep0c_MultiplePending_OnePerTickInFixedOrder(t *testing.T) {
	// Approval order: decided_at, then id (documented choice, NOTE(S6.4)).
	rec := recs([]Task{
		tk(2, StatusPackaged, decided(DecisionApproved, at(-10)), branch("b2", "s2")),
		tk(3, StatusPackaged, decided(DecisionApproved, at(-30)), branch("b3", "s3")), // earliest decision
		tk(4, StatusPackaged, decided(DecisionApproved, at(-10)), branch("b4", "s4")), // ties task 2 on decided_at
	}, []Packet{pk(2), pk(3), pk(4)})
	a := decide(t, rec, freeLock())
	if a.Kind != KindLand || a.Land.TaskID != 3 {
		t.Fatalf("earliest decided_at lands first: want task 3, got %+v", a)
	}
	// Drain task 3 → decided_at tie between 2 and 4 → lower id.
	rec.Tasks[1].Archived = true
	rec.Packets[1].Archived = true
	a = decide(t, rec, freeLock())
	if a.Kind != KindLand || a.Land.TaskID != 2 {
		t.Fatalf("decided_at tie → lower id: want task 2, got %+v", a)
	}
}

func TestStep0c_NotTaken_NoPendingLanding(t *testing.T) {
	a := decide(t, recs([]Task{tk(1, StatusVerified)}, nil), freeLock())
	wantSpawn(t, a, RolePackager, pi64(1))
}

func TestStep0c_BlockedLandingPending_DoesNotRetryUntilUnblocked(t *testing.T) {
	// NOTE(S6.3): a failed landing blocks the task; the blocked row is
	// invisible to (0c) — the would-otherwise-win row here is the only
	// landing-pending row, so without the rule this test would see KindLand.
	blockedLanding := tk(2, StatusPackaged, decided(DecisionApproved, at(-30)),
		branch("b2", "s2"), blocked())
	rec := recs([]Task{tk(1, StatusVerified), blockedLanding}, []Packet{pk(2)})
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RolePackager, pi64(1)) // falls through to step (3)
}

func TestStep0c_ApprovedBranchlessRow_NeverLands(t *testing.T) {
	// Defensive: an artifact-plane deliverable is archived synchronously at
	// approve and carries no branch; no land effect may exist for it.
	rec := recs([]Task{
		tk(1, StatusSeeded),
		tk(2, StatusPackaged, decided(DecisionApproved, at(-30))), // no branch
	}, []Packet{pk(2)})
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleWorker, pi64(1))
}

// ---------------------------------------------------------------------------
// A/1+2 — Queue at cap: occupancy, (2a) advance-in-flight, (2b) start-one,
// idle-on-saturation. Taken and not-taken for each.
// ---------------------------------------------------------------------------

// atCapPackets: three unarchived packets on the given (packaged) task ids.
func atCapPackets(ids ...int64) []Packet {
	var ps []Packet
	for _, id := range ids {
		ps = append(ps, pk(id))
	}
	return ps
}

func TestAtCap_NoNewPipelineWorkEver(t *testing.T) {
	// Queue at cap + a fresh proposal and a verified row → neither is
	// dispatched (queue-with-room branch not-taken); the tick starts a
	// refinement instead.
	rec := recs([]Task{
		tk(1, StatusPackaged), tk(2, StatusPackaged), tk(3, StatusPackaged),
		tk(4, StatusProposed),           // would win nothing here; pipeline is gated
		tk(5, StatusVerified, prio(-1)), // even expedited pipeline work is gated at cap
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(1))
}

func TestOccupancy_ArchivedPacketFreesTheQueue(t *testing.T) {
	// Invisibility: an archived packet does not count toward occupancy.
	// Would-otherwise: 3 packets = at cap → Refiner; with one archived the
	// queue has room and step (3) dispatches the verified row.
	pkts := []Packet{pk(1), pk(2), pk(3, pArchived())}
	rec := recs([]Task{
		tk(1, StatusPackaged), tk(2, StatusPackaged), tk(3, StatusPackaged),
		tk(4, StatusVerified),
	}, pkts)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RolePackager, pi64(4))
}

func TestStep2a_AdvancesReenteredTask(t *testing.T) {
	// Task 1's packet holds its slot while the task rides the pipeline again
	// (Inv. 11); at cap its next stage keeps dispatching.
	rec := recs([]Task{
		tk(1, StatusWorked), // re-entered, now owed verification
		tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleVerifier, pi64(1))
}

func TestStep2a_RoleMapping_SameTableAsStep3(t *testing.T) {
	cases := []struct {
		name string
		task Task
		role Role
	}{
		{"seeded task → Worker", tk(1, StatusSeeded), RoleWorker},
		{"worked task → Verifier", tk(1, StatusWorked), RoleVerifier},
		{"verified task → Packager", tk(1, StatusVerified), RolePackager},
		{"drained seeded initiative → Strategist(initiative)", tk(1, StatusSeeded, initiative()), RoleStrategistInitiative},
		{"worked initiative → Verifier (arc)", tk(1, StatusWorked, initiative()), RoleVerifier},
		{"verified initiative → Packager (arc)", tk(1, StatusVerified, initiative()), RolePackager},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := recs([]Task{
				c.task,
				tk(2, StatusPackaged), tk(3, StatusPackaged),
			}, atCapPackets(1, 2, 3))
			a := decide(t, rec, freeLock())
			wantSpawn(t, a, c.role, pi64(1))
		})
	}
}

func TestStep2a_WaveChildOfPacketHoldingInitiative_AdvancesAtCap(t *testing.T) {
	// The join's second arm: p.task_id = t.initiative_id. Initiative 1 holds
	// a packet and re-entered (seeded) with an open child; the child — linked
	// only through its initiative — must keep dispatching at cap, or an
	// initiative bounced back from review could never advance its waves.
	rec := recs([]Task{
		tk(1, StatusSeeded, initiative()), // re-entered arc; parked (open child)
		tk(10, StatusSeeded, childOf(1)),  // the wave child: no packet of its own
		tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleWorker, pi64(10))
}

func TestStep2a_ParkedInitiativeInvisible_ChildWins(t *testing.T) {
	// The re-entered initiative itself would win (created earlier, same
	// rank) but is parked while a child is open — the child is selected.
	init := tk(1, StatusSeeded, initiative(), created(at(-100)))
	child := tk(10, StatusSeeded, childOf(1), created(at(-1)))
	rec := recs([]Task{init, child, tk(2, StatusPackaged), tk(3, StatusPackaged)},
		atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleWorker, pi64(10))
}

func TestStep2a_BlockedInFlightRowInvisible(t *testing.T) {
	// Invisibility with a would-otherwise-win row: the blocked re-entered
	// task out-ranks everything (worked, rank 3); with it visible the action
	// would be Verifier(1). It must be skipped → nothing in flight → (2b)
	// starts a refinement on the best packet instead.
	rec := recs([]Task{
		tk(1, StatusWorked, blocked()),
		tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(2))
}

func TestStep2a_Ordering_FurthestFirstThenPriorityThenAgeThenID(t *testing.T) {
	// Every candidate is a re-entered task holding its own live packet
	// (Inv. 11); occupancy is padded to cap with packaged fillers.
	mk := func(tasks ...Task) Records {
		all := append([]Task{}, tasks...)
		var pkts []Packet
		for _, x := range tasks {
			pkts = append(pkts, pk(x.ID))
		}
		for len(pkts) < 3 {
			filler := int64(90 + len(pkts))
			all = append(all, tk(filler, StatusPackaged, prio(3)))
			pkts = append(pkts, pk(filler, saturated()))
		}
		return recs(all, pkts)
	}

	// furthest-first: worked (rank 3) beats seeded (rank 2), even at worse priority.
	a := decide(t, mk(tk(1, StatusSeeded, prio(0)), tk(2, StatusWorked, prio(3))), freeLock())
	wantSpawn(t, a, RoleVerifier, pi64(2))

	// same rank → priority.
	a = decide(t, mk(tk(1, StatusSeeded, prio(2)), tk(2, StatusSeeded, prio(0))), freeLock())
	wantSpawn(t, a, RoleWorker, pi64(2))

	// same rank+priority → older created_at.
	a = decide(t, mk(
		tk(1, StatusSeeded, created(at(-5))),
		tk(2, StatusSeeded, created(at(-50))),
	), freeLock())
	wantSpawn(t, a, RoleWorker, pi64(2))

	// full tie → lower id (added tiebreak, NOTE(S6.1)).
	a = decide(t, mk(
		tk(7, StatusSeeded, created(at(-5))),
		tk(4, StatusSeeded, created(at(-5))),
	), freeLock())
	wantSpawn(t, a, RoleWorker, pi64(4))
}

func TestStep2a_NotTaken_NothingInFlight(t *testing.T) {
	// All packet tasks sit at packaged (no round-trip in flight) → (2b).
	rec := recs([]Task{
		tk(1, StatusPackaged), tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(1))
}

func TestStep2b_TaskPacket_SpawnsRefiner(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusPackaged), tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(1))
}

func TestStep2b_InitiativePacket_ReentersDirectly_NoSpawn(t *testing.T) {
	// Scope initiative → pure mutation packaged → seeded; Strategist(initiative)
	// carries the deepening as waves, which (2a) then picks up on later ticks.
	rec := recs([]Task{
		tk(1, StatusPackaged, initiative(), created(at(-100))),
		tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, []Packet{pk(1, pCreated(at(-100))), pk(2), pk(3)})
	a := decide(t, rec, freeLock())
	if a.Kind != KindReenter || a.Reenter.TaskID != 1 {
		t.Fatalf("initiative packet must re-enter, not spawn: %+v", a)
	}
}

func TestStep2b_Ordering_PriorityThenPacketAgeThenID(t *testing.T) {
	// priority first.
	rec := recs([]Task{
		tk(1, StatusPackaged, prio(2)), tk(2, StatusPackaged, prio(0)), tk(3, StatusPackaged, prio(1)),
	}, atCapPackets(1, 2, 3))
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(2))

	// tie priority → older packet created_at.
	rec = recs([]Task{
		tk(1, StatusPackaged), tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, []Packet{pk(1, pCreated(at(-5))), pk(2, pCreated(at(-50))), pk(3, pCreated(at(-20)))})
	a = decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(2))

	// full tie → lower task id (added tiebreak, NOTE(S6.1)).
	rec = recs([]Task{
		tk(5, StatusPackaged), tk(4, StatusPackaged), tk(6, StatusPackaged),
	}, []Packet{pk(5, pCreated(at(-5))), pk(4, pCreated(at(-5))), pk(6, pCreated(at(-5)))})
	a = decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(4))
}

func TestStep2b_SaturatedPacketInvisible(t *testing.T) {
	// Would-otherwise-win: packet 1 is oldest and would be chosen, but it is
	// saturated → packet 2 selected.
	rec := recs([]Task{
		tk(1, StatusPackaged), tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, []Packet{
		pk(1, pCreated(at(-100)), saturated()),
		pk(2, pCreated(at(-50))),
		pk(3, pCreated(at(-20))),
	})
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(2))
}

func TestStep2b_DecidedPacketInvisible(t *testing.T) {
	// A decided packet is out of the refinement pool (§10 step 0c), or
	// dispatch would spawn a Refiner on already-approved work. Task 1 is
	// approved-and-blocked (failed landing: slot held, retry awaits the
	// operator) and its packet is oldest — would otherwise win.
	rec := recs([]Task{
		tk(1, StatusPackaged, decided(DecisionApproved, at(-30)), branch("b1", "s1"), blocked()),
		tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, []Packet{pk(1, pCreated(at(-100))), pk(2, pCreated(at(-50))), pk(3, pCreated(at(-20)))})
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(2))
}

func TestStep2b_BlockedPackagedTaskInvisible(t *testing.T) {
	// NOTE(S6.2): §6's blanket rule — blocked rows are invisible to dispatch —
	// applied to (2b). Task 1's packet is oldest and undecided; blocked ⇒ skipped.
	rec := recs([]Task{
		tk(1, StatusPackaged, blocked()),
		tk(2, StatusPackaged), tk(3, StatusPackaged),
	}, []Packet{pk(1, pCreated(at(-100))), pk(2, pCreated(at(-50))), pk(3, pCreated(at(-20)))})
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleRefiner, pi64(2))
}

func TestStep2b_NotTaken_AllSaturated_Idle(t *testing.T) {
	// At cap, nothing in flight, every packet saturated → idle — even with
	// dispatchable pipeline rows waiting (they are gated at cap, Inv. 19).
	rec := recs([]Task{
		tk(1, StatusPackaged), tk(2, StatusPackaged), tk(3, StatusPackaged),
		tk(4, StatusVerified), tk(5, StatusProposed),
	}, []Packet{pk(1, saturated()), pk(2, saturated()), pk(3, saturated())})
	a := decide(t, rec, freeLock())
	if a.Kind != KindIdle || a.Idle != IdleQueueSaturated {
		t.Fatalf("all saturated at cap must idle, got %+v", a)
	}
}

// ---------------------------------------------------------------------------
// A/3 — Queue with room: the single next dispatch. Role mapping, ordering,
// invisibility rules, each with a would-otherwise-win row.
// ---------------------------------------------------------------------------

func TestStep3_RoleMapping(t *testing.T) {
	cases := []struct {
		name string
		task Task
		role Role
	}{
		{"proposed task → Editor", tk(1, StatusProposed), RoleEditor},
		{"proposed initiative → Editor", tk(1, StatusProposed, initiative()), RoleEditor},
		{"seeded task → Worker", tk(1, StatusSeeded), RoleWorker},
		{"seeded initiative (drained) → Strategist(initiative)", tk(1, StatusSeeded, initiative()), RoleStrategistInitiative},
		{"worked task → Verifier", tk(1, StatusWorked), RoleVerifier},
		{"worked initiative → Verifier (arc)", tk(1, StatusWorked, initiative()), RoleVerifier},
		{"verified task → Packager", tk(1, StatusVerified), RolePackager},
		{"verified initiative → Packager (arc)", tk(1, StatusVerified, initiative()), RolePackager},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := decide(t, recs([]Task{c.task}, nil), freeLock())
			wantSpawn(t, a, c.role, pi64(1))
		})
	}
}

func TestStep3_EditorBriefSnapshotsEntirePool(t *testing.T) {
	// Batch-wise: the Editor sees the whole proposed pool, never the subject
	// row alone; archived (already-rejected) proposals are excluded.
	rec := recs([]Task{
		tk(1, StatusProposed),
		tk(2, StatusProposed, initiative()),
		tk(3, StatusProposed, archived(), decided(DecisionRejected, at(-10))),
		tk(4, StatusProposed),
		tk(5, StatusProposed, blocked()),
	}, nil)
	a := decide(t, rec, freeLock())
	if a.Kind != KindSpawn || a.Spawn.Role != RoleEditor {
		t.Fatalf("want editor spawn, got %+v", a)
	}
	if want := []int64{1, 2, 4}; !reflect.DeepEqual(a.Spawn.ProposedPool, want) {
		t.Fatalf("pool snapshot: want %v, got %v", want, a.Spawn.ProposedPool)
	}
}

func TestStep3_FurthestFirst(t *testing.T) {
	// verified (4) > worked (3) > seeded (2) > proposed (1).
	rec := recs([]Task{
		tk(1, StatusProposed), tk(2, StatusSeeded), tk(3, StatusWorked), tk(4, StatusVerified),
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RolePackager, pi64(4))
}

func TestStep3_ExpediteLanePartitionsAheadOfStageOrder(t *testing.T) {
	// A P-1 proposal pulls the Editor ahead of a verified P0 row.
	rec := recs([]Task{
		tk(1, StatusVerified, prio(0)),
		tk(2, StatusProposed, prio(-1)),
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleEditor, pi64(2))
}

func TestStep3_WithinExpeditePartition_FurthestFirstStillApplies(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusProposed, prio(-1)),
		tk(2, StatusWorked, prio(-1)),
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleVerifier, pi64(2))
}

func TestStep3_PriorityThenAgeThenID(t *testing.T) {
	// same rank → priority.
	a := decide(t, recs([]Task{
		tk(1, StatusSeeded, prio(2)), tk(2, StatusSeeded, prio(0)),
	}, nil), freeLock())
	wantSpawn(t, a, RoleWorker, pi64(2))

	// same rank+priority → older created_at.
	a = decide(t, recs([]Task{
		tk(1, StatusSeeded, created(at(-1))), tk(2, StatusSeeded, created(at(-90))),
	}, nil), freeLock())
	wantSpawn(t, a, RoleWorker, pi64(2))

	// full tie → lower id.
	a = decide(t, recs([]Task{
		tk(9, StatusSeeded, created(at(-7))), tk(6, StatusSeeded, created(at(-7))),
	}, nil), freeLock())
	wantSpawn(t, a, RoleWorker, pi64(6))
}

func TestStep3_ArchivedRowInvisible(t *testing.T) {
	// Would-otherwise-win: the archived row out-ranks (verified vs seeded).
	rec := recs([]Task{
		tk(1, StatusVerified, archived(), decided(DecisionCancelled, at(-5))),
		tk(2, StatusSeeded),
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleWorker, pi64(2))
}

func TestStep3_BlockedRowInvisible(t *testing.T) {
	// Would-otherwise-win: blocked verified row out-ranks the seeded row.
	rec := recs([]Task{
		tk(1, StatusVerified, blocked()),
		tk(2, StatusSeeded),
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleWorker, pi64(2))
}

func TestStep3_PackagedRankZeroInvisible(t *testing.T) {
	// A packaged row is in the review queue, out of pipeline dispatch: with
	// only packaged rows (queue below cap), (3) finds nothing and the tick
	// falls through to the propose fallback.
	rec := recs([]Task{tk(1, StatusPackaged), tk(2, StatusPackaged)},
		[]Packet{pk(1), pk(2)})
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleStrategistPropose, nil)
}

func TestStep3_ParkedInitiativeInvisible_ChildrenAreItsPresence(t *testing.T) {
	// Would-otherwise-win: initiative (older, same rank) vs its child.
	rec := recs([]Task{
		tk(1, StatusSeeded, initiative(), created(at(-100))),
		tk(2, StatusSeeded, childOf(1), created(at(-1))),
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleWorker, pi64(2))
}

func TestStep3_DrainedInitiative_ArchivedChildrenDontPark(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusSeeded, initiative()),
		tk(2, StatusSeeded, childOf(1), archived(), decided(DecisionCancelled, at(-5))),
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleStrategistInitiative, pi64(1))
}

func TestStep3_BlockedInitiative_UnblockedChildKeepsFlowing(t *testing.T) {
	// One blocked child blocks the initiative (propagated flag); unblocked
	// siblings keep dispatching on their own flags.
	rec := recs([]Task{
		tk(1, StatusSeeded, initiative(), blocked()), // propagated block
		tk(2, StatusSeeded, childOf(1), blocked()),   // the blocked child
		tk(3, StatusSeeded, childOf(1)),              // unblocked sibling
	}, nil)
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleWorker, pi64(3))
}

// ---------------------------------------------------------------------------
// A/4 — Step (4): nothing dispatchable → Strategist(propose) with dedupe memory.
// ---------------------------------------------------------------------------

func TestStep4_EmptySystem_SpawnsPropose(t *testing.T) {
	a := decide(t, recs(nil, nil), freeLock())
	wantSpawn(t, a, RoleStrategistPropose, nil)
}

func TestStep4_OnlyBlockedPackagedParkedRows_SpawnsPropose(t *testing.T) {
	rec := recs([]Task{
		tk(1, StatusSeeded, blocked()),
		tk(2, StatusPackaged),
		tk(3, StatusSeeded, initiative()), // parked: open child 4
		tk(4, StatusSeeded, childOf(3), blocked()),
	}, []Packet{pk(2)})
	a := decide(t, rec, freeLock())
	wantSpawn(t, a, RoleStrategistPropose, nil)
}

func TestStep4_DedupeMemory_TwentyMostRecentRejectedTitles(t *testing.T) {
	var tasks []Task
	for i := int64(1); i <= 25; i++ {
		tasks = append(tasks, tk(i, StatusProposed, archived(),
			decided(DecisionRejected, at(int(-i))))) // task 1 newest, 25 oldest
	}
	// Cancelled/approved rows never feed the dedupe memory.
	tasks = append(tasks, tk(30, StatusSeeded, archived(), decided(DecisionCancelled, at(0))))
	a := decide(t, recs(tasks, nil), freeLock())
	wantSpawn(t, a, RoleStrategistPropose, nil)
	if len(a.Spawn.DedupeTitles) != 20 {
		t.Fatalf("want 20 titles, got %d", len(a.Spawn.DedupeTitles))
	}
	if a.Spawn.DedupeTitles[0] != "task-1" || a.Spawn.DedupeTitles[19] != "task-20" {
		t.Fatalf("want newest-first task-1..task-20, got first=%s last=%s",
			a.Spawn.DedupeTitles[0], a.Spawn.DedupeTitles[19])
	}
}

// ---------------------------------------------------------------------------
// E — the many-obligation precedence cascade: a state owing everything at
// once yields exactly the top-most obligation; stripping obligations one at
// a time walks the whole precedence chain
// reap → console → land → 2a → 2b → idle → (room) 3 → 4.
// ---------------------------------------------------------------------------

func TestPrecedenceCascade_ManyObligationState(t *testing.T) {
	cfg := DefaultConfig()
	clk := noonClock()

	// The everything-owed state:
	tasks := []Task{
		tk(1, StatusSeeded), // the crashed run's subject
		tk(10, StatusPackaged, decided(DecisionApproved, at(-40)), branch("b10", "s10")), // landing pending
		tk(20, StatusWorked),   // in-flight refinement (packet 20 below)
		tk(30, StatusPackaged), // refinement candidate (packet 30 below)
		tk(40, StatusVerified), // pipeline work waiting
		tk(50, StatusProposed), // proposal waiting
	}
	pkts := []Packet{pk(10), pk(20), pk(30)}                         // occupancy 3 = at cap
	rec := Records{Tasks: tasks, Packets: pkts, LastBriefingAt: nil} // console due
	reapableLock := Lock{
		Held: true, RunID: "run-dead", SubjectID: pi64(1),
		AcquiredAt:     base.Add(-5 * time.Minute), // never heartbeated → watchdog
		HardDeadlineAt: base.Add(time.Hour),
	}

	step := func(name string, rec Records, lock Lock, check func(Action)) {
		a := Decide(rec, lock, cfg, clk)
		assertWellFormed(t, a)
		check(a)
		// Determinism under repetition at every cascade state.
		if b := Decide(rec, lock, cfg, clk); !reflect.DeepEqual(a, b) {
			t.Fatalf("%s: nondeterministic decision:\n%+v\n%+v", name, a, b)
		}
	}

	// 1. Everything owed at once → the reap, and only the reap.
	step("reap", rec, reapableLock, func(a Action) {
		if a.Kind != KindReap || a.Reap.RunID != "run-dead" {
			t.Fatalf("many-obligation state must reap first, got %+v", a)
		}
	})

	// 2. Lease freed → console.
	step("console", rec, freeLock(), func(a Action) {
		if a.Kind != KindSpawn || a.Spawn.Role != RoleStrategistConsole {
			t.Fatalf("want console next, got %+v", a)
		}
	})

	// 3. Briefing delivered today → the land effect.
	rec.LastBriefingAt = pt(briefedToday)
	step("land", rec, freeLock(), func(a Action) {
		if a.Kind != KindLand || a.Land.TaskID != 10 {
			t.Fatalf("want landing of task 10, got %+v", a)
		}
	})

	// 4. Landing succeeded (task+packet archived; occupancy back to cap via a
	// saturated packet on 60) → (2a) advances the in-flight refinement.
	rec.Tasks[1].Archived = true
	rec.Packets[0].Archived = true
	rec.Tasks = append(rec.Tasks, tk(60, StatusPackaged))
	rec.Packets = append(rec.Packets, pk(60, saturated()))
	step("2a", rec, freeLock(), func(a Action) {
		if a.Kind != KindSpawn || a.Spawn.Role != RoleVerifier || *a.Spawn.SubjectID != 20 {
			t.Fatalf("want Verifier(20) advancing in-flight refinement, got %+v", a)
		}
	})

	// 5. Round-trip finished (20 back at packaged) → (2b) starts one: Refiner
	// on the best non-saturated packet — packet 20 (oldest, same priority).
	rec.Tasks[2].Status = StatusPackaged
	step("2b", rec, freeLock(), func(a Action) {
		if a.Kind != KindSpawn || a.Spawn.Role != RoleRefiner || *a.Spawn.SubjectID != 20 {
			t.Fatalf("want Refiner(20), got %+v", a)
		}
	})

	// 6. Every queued packet saturated → idle, with pipeline work still
	// waiting (at cap the only spendable capacity is refinement, then idle).
	rec.Packets[1].Saturated = true // packet 20
	rec.Packets[2].Saturated = true // packet 30
	step("idle", rec, freeLock(), func(a Action) {
		if a.Kind != KindIdle || a.Idle != IdleQueueSaturated {
			t.Fatalf("want idle(queue-saturated), got %+v", a)
		}
	})

	// 7. A packet archives (operator decided task 60) → room → step (3):
	// furthest-first picks the verified row over the seeded and proposed ones.
	rec.Packets[3].Archived = true // packet 60
	rec.Tasks[6].Archived = true   // task 60
	step("3", rec, freeLock(), func(a Action) {
		if a.Kind != KindSpawn || a.Spawn.Role != RolePackager || *a.Spawn.SubjectID != 40 {
			t.Fatalf("want Packager(40), got %+v", a)
		}
	})

	// 8. Pipeline drained (40, 50, 20, 30, 1 all gone) → propose fallback.
	for i := range rec.Tasks {
		rec.Tasks[i].Archived = true
	}
	for i := range rec.Packets {
		rec.Packets[i].Archived = true
	}
	step("4", rec, freeLock(), func(a Action) {
		if a.Kind != KindSpawn || a.Spawn.Role != RoleStrategistPropose {
			t.Fatalf("want Strategist(propose) fallback, got %+v", a)
		}
	})
}
