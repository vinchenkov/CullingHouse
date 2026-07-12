package dispatch

// Property suite for spike S6 requirements D (exactly zero-or-one action per
// evaluation, always) and the global purity/Inv. 21 obligations:
//   - Decide is total and well-formed over randomized states;
//   - Decide is deterministic (same inputs → identical Action);
//   - Decide never mutates its inputs;
//   - Decide is time-invariant outside the lease/schedule carve-outs
//     (Inv. 21: with the lease free and the console not due at either
//     instant, wall-clock never changes the decision).

import (
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// genState builds a randomized, mostly-substrate-legal state. It leans legal
// (the substrate forbids illegal rows) but keeps enough variety to exercise
// every branch: mixed scopes, wave children, blocked/archived flags, terminal
// decisions, packets in every flag combination, and every lock shape.
func genState(r *rand.Rand) (Records, Lock) {
	n := r.Intn(12)
	var tasks []Task
	var initiativeIDs []int64
	statuses := []Status{StatusProposed, StatusSeeded, StatusWorked, StatusVerified, StatusPackaged}

	for i := 0; i < n; i++ {
		id := int64(i + 1)
		t := Task{
			ID:              id,
			Title:           "t",
			Scope:           ScopeTask,
			Priority:        r.Intn(5) - 1, // -1..3
			CreatedAt:       base.Add(time.Duration(r.Intn(600)-300) * time.Minute),
			Status:          statuses[r.Intn(len(statuses))],
			DispatchRetries: r.Intn(4),
			Worksource:      "ws",
			TargetRef:       "main",
		}
		if r.Intn(4) == 0 {
			t.Scope = ScopeInitiative
			initiativeIDs = append(initiativeIDs, id)
		} else if len(initiativeIDs) > 0 && r.Intn(3) == 0 {
			t.InitiativeID = &initiativeIDs[r.Intn(len(initiativeIDs))]
		}
		if r.Intn(6) == 0 {
			t.Blocked = true
		}
		switch r.Intn(8) {
		case 0: // rejected (Editor) — archived with decision
			if t.Status == StatusProposed {
				t.Decision = DecisionRejected
				t.DecidedAt = pt(base.Add(time.Duration(-r.Intn(600)) * time.Minute))
				t.Archived = true
			}
		case 1: // cancelled at any stage
			t.Decision = DecisionCancelled
			t.DecidedAt = pt(base.Add(time.Duration(-r.Intn(600)) * time.Minute))
			t.Archived = true
		case 2: // approved — only from packaged; sometimes branch-carrying (landing pending)
			t.Status = StatusPackaged
			t.Decision = DecisionApproved
			t.DecidedAt = pt(base.Add(time.Duration(-r.Intn(600)) * time.Minute))
			if r.Intn(2) == 0 {
				t.Branch = "mc/task-x"
				t.VerifiedSHA = "sha"
			} else {
				t.Archived = true // branchless approve archives synchronously
			}
		}
		tasks = append(tasks, t)
	}

	// Packets: at most one per task; only for tasks that are/were packaged.
	var pkts []Packet
	live := 0
	for _, t := range tasks {
		if t.Status != StatusPackaged && r.Intn(3) != 0 {
			continue // re-entered tasks may hold a packet; others usually not
		}
		if r.Intn(2) == 0 {
			continue
		}
		p := Packet{
			TaskID:    t.ID,
			CreatedAt: t.CreatedAt.Add(time.Duration(r.Intn(300)) * time.Minute),
			Saturated: r.Intn(3) == 0,
			Archived:  t.Archived, // a packet archives with its task's decision
		}
		if !p.Archived && live >= 3 {
			continue // substrate WIP cap: never more than 3 unarchived packets
		}
		if !p.Archived {
			live++
		}
		pkts = append(pkts, p)
	}

	rec := Records{Tasks: tasks, Packets: pkts}
	switch r.Intn(3) {
	case 0:
		rec.LastBriefingAt = nil
	case 1:
		rec.LastBriefingAt = pt(briefedToday)
	case 2:
		rec.LastBriefingAt = pt(base.Add(-26 * time.Hour))
	}

	lock := Lock{}
	if r.Intn(3) == 0 {
		lock = Lock{
			Held:           true,
			RunID:          "run-f",
			AcquiredAt:     base.Add(time.Duration(-r.Intn(240)) * time.Minute),
			HardDeadlineAt: base.Add(time.Duration(r.Intn(240)-120) * time.Minute),
		}
		if len(tasks) > 0 && r.Intn(4) != 0 {
			lock.SubjectID = pi64(tasks[r.Intn(len(tasks))].ID)
		}
		if r.Intn(3) != 0 {
			lock.LastHeartbeatAt = pt(base.Add(time.Duration(-r.Intn(200)) * time.Minute))
		}
	}
	return rec, lock
}

func deepCopyRecords(rec Records) Records {
	cp := Records{
		Tasks:   append([]Task(nil), rec.Tasks...),
		Packets: append([]Packet(nil), rec.Packets...),
	}
	for i := range cp.Tasks {
		if cp.Tasks[i].InitiativeID != nil {
			v := *cp.Tasks[i].InitiativeID
			cp.Tasks[i].InitiativeID = &v
		}
		if cp.Tasks[i].DecidedAt != nil {
			v := *cp.Tasks[i].DecidedAt
			cp.Tasks[i].DecidedAt = &v
		}
	}
	if rec.LastBriefingAt != nil {
		v := *rec.LastBriefingAt
		cp.LastBriefingAt = &v
	}
	return cp
}

func TestProperty_ExactlyZeroOrOneAction_DeterministicAndPure(t *testing.T) {
	cfg := DefaultConfig()
	clk := noonClock()
	r := rand.New(rand.NewSource(6)) // fixed seed: reproducible

	for i := 0; i < 5000; i++ {
		rec, lock := genState(r)
		recBefore := deepCopyRecords(rec)
		lockBefore := lock

		a := Decide(rec, lock, cfg, clk)
		assertWellFormed(t, a) // exactly zero-or-one action, well-shaped payload

		// Deterministic: identical inputs → identical action.
		if b := Decide(rec, lock, cfg, clk); !reflect.DeepEqual(a, b) {
			t.Fatalf("iteration %d: nondeterministic:\n%+v\n%+v\nstate: %+v", i, a, b, rec)
		}

		// Pure: inputs never mutated.
		if !reflect.DeepEqual(rec, recBefore) {
			t.Fatalf("iteration %d: Decide mutated Records", i)
		}
		if !reflect.DeepEqual(lock, lockBefore) {
			t.Fatalf("iteration %d: Decide mutated Lock", i)
		}
	}
}

func TestProperty_TimeInvariance_SameDayAfterBriefing(t *testing.T) {
	// Inv. 21: with the lease free and the console already delivered today,
	// two clocks hours apart must yield the identical decision — eligibility
	// and ordering read stored fields only.
	cfg := DefaultConfig()
	r := rand.New(rand.NewSource(21))
	noon := clock(base)                       // 12:00
	evening := clock(base.Add(6 * time.Hour)) // 18:00 same day

	for i := 0; i < 3000; i++ {
		rec, _ := genState(r)
		rec.LastBriefingAt = pt(briefedToday) // console quiet at both instants
		a := Decide(rec, freeLock(), cfg, noon)
		b := Decide(rec, freeLock(), cfg, evening)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("iteration %d: decision changed with wall clock:\nnoon:    %+v\nevening: %+v", i, a, b)
		}
	}
}

func TestProperty_TimeInvariance_AcrossDaysBeforeDelivery(t *testing.T) {
	// Same invariance across a 40-day clock jump, both instants before the
	// day's delivery time and never briefed.
	cfg := DefaultConfig()
	r := rand.New(rand.NewSource(40))
	d1 := clock(time.Date(2026, 7, 1, 7, 0, 0, 0, time.UTC))
	d2 := clock(time.Date(2026, 8, 10, 7, 30, 0, 0, time.UTC))

	for i := 0; i < 3000; i++ {
		rec, _ := genState(r)
		rec.LastBriefingAt = nil
		a := Decide(rec, freeLock(), cfg, d1)
		b := Decide(rec, freeLock(), cfg, d2)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("iteration %d: decision changed across days:\nd1: %+v\nd2: %+v", i, a, b)
		}
	}
}
