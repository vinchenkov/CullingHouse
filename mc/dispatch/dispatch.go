// Package dispatch implements mc dispatch's selection logic as a pure
// function — spike S6 (specs/implementation-handoff.md Part 2).
//
// (records, lock state, clock inputs) → exactly one action | idle.
//
// No I/O, no globals, no reading of the wall clock: time enters only as an
// injected Clock value, and it is consulted only where the spec permits time
// to matter (spec §2 Inv. 21) — lease/watchdog reaping thresholds (§10 step 0)
// and the Daily Console stored schedule (§10 step 0b, §14). Eligibility and
// dispatch ordering are time-invariant: they read only stored record fields
// (created_at, decided_at, priority, stage_rank, ids).
//
// The tick walk implemented here is spec §10 "Control flow — one tick, walked
// top-down", steps (0), (0b), (0c), (1), (2a), (2b), (3), (4), plus the
// invisibility rules of §5/§6/§8 (blocked, archived, parked initiatives,
// packaged rank 0, saturated/decided packets).
//
// Interpretations the spec's text decides but does not spell out in one place
// are marked "NOTE(Sn.k)" and documented in RESULT.md.
package dispatch

import (
	"fmt"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// Record types (the dispatch-relevant projection of the two-table substrate,
// spec §4/§5: tasks + review_packets, plus the two stored facts the walk
// needs from elsewhere in the spine: the lock singleton and the most recent
// daily.briefing activity event).
// ---------------------------------------------------------------------------

// Status is the last stage a task completed (spec §6, Inv. 10).
type Status string

const (
	StatusProposed Status = "proposed"
	StatusSeeded   Status = "seeded"
	StatusWorked   Status = "worked"
	StatusVerified Status = "verified"
	StatusPackaged Status = "packaged"
)

// Rank mirrors the generated stage_rank column (spec §5 Task): how far into
// the pipeline the row is, for furthest-first dispatch. packaged ranks 0 —
// in the review queue, out of pipeline dispatch. Unknown statuses rank -1 and
// are invisible to every query (defensive; the substrate forbids them).
func (s Status) Rank() int {
	switch s {
	case StatusProposed:
		return 1
	case StatusSeeded:
		return 2
	case StatusWorked:
		return 3
	case StatusVerified:
		return 4
	case StatusPackaged:
		return 0
	}
	return -1
}

// Scope distinguishes ordinary tasks from initiatives (spec §5).
type Scope string

const (
	ScopeTask       Scope = "task"
	ScopeInitiative Scope = "initiative"
)

// TaskDecision is the terminal bookkeeping mark, orthogonal to status (§6).
type TaskDecision string

const (
	DecisionNone      TaskDecision = ""
	DecisionApproved  TaskDecision = "approved"
	DecisionRejected  TaskDecision = "rejected"
	DecisionCancelled TaskDecision = "cancelled"
)

// Task is one row of the `tasks` table (spec §5), restricted to the fields
// dispatch reads. Proposals, ordinary tasks, initiatives and wave children
// are all Task rows; Scope/Status/InitiativeID say which.
type Task struct {
	ID           int64
	Title        string
	Scope        Scope
	InitiativeID *int64 // set on wave children only (substrate forbids nesting)
	Priority     int    // -1 = expedited, 0–3 = P0–P3; lower wins
	CreatedAt    time.Time
	Status       Status
	Blocked      bool // dispatchability flag, deliberately not a status (§5, §6)
	// PlanReviewed: the Editor passed this wave child's plan (ADR-020 D1).
	// Meaningful only on a child; on every other row the substrate pins it
	// false, where it reads "not applicable", never "unreviewed".
	PlanReviewed     bool
	DispatchRetries  int // infra-death budget; decremented only by the reaper (§10)
	Decision         TaskDecision
	DecidedAt        *time.Time
	Archived         bool
	Worksource       string
	WorksourceStatus string // empty in hand-built legacy fixtures means active

	// Landing inputs (§7 Landing): a task whose record carries a branch gets
	// a durable landing-pending mark at approve; the land effect carries
	// (task, branch, verified SHA, target ref). Branch == "" means an
	// artifact-plane deliverable — approve archives it synchronously and no
	// land effect ever exists for it.
	Branch      string
	VerifiedSHA string
	TargetRef   string

	// Sealed is the immutable first-task closure assignment (ADR-016 D5) when
	// this task has one, and nil otherwise. It is the SECOND branch home: a
	// sealed standalone task is branchless in `tasks` by construction, so
	// `Branch` above is empty for it and this carries the branch instead.
	// Non-nil is what puts a row in the sealed landing lane.
	Sealed *SealedAssignment
}

// SealedAssignment is the `task_assignments` row as dispatch reads it: the
// frozen closure pin a sealed task was assigned at its first setup, which a
// retry reuses and never rebases (ADR-016 D5). Every column is immutable from
// insert, so this is evidence, not state.
//
// TargetRef here is the ref frozen AT ASSIGNMENT, which may have drifted from
// the task's current TargetRef. Landing must refuse on divergence rather than
// silently prefer one — see SealedLandingPending.
type SealedAssignment struct {
	Branch        string
	TargetRef     string
	TaskRootKey   string
	ObjectFormat  string
	BaseSHA       string
	LocalRepoUUID string
	ClosureDigest string
}

// SealedLandingPending reports whether this row is owed a SEALED §10 step (0c)
// landing: the same conjuncts as LandingPending, but keyed on the assignment
// instead of `tasks.branch`, plus the two landing facts the §7 approve fence
// guarantees.
//
// The two lanes partition by construction — `tasks.branch`'s only writer is
// the `--status worked --branch` terminal that ADR-016 D6 closes to assigned
// tasks — so no row satisfies both. A row that somehow carries both branch
// homes is incoherent and belongs to neither lane; it is refused here rather
// than landed down the wrong one.
//
// Deliberately NOT a conjunct: agreement between the assignment's frozen
// TargetRef and the task's current one. A diverged row stays in the lane so
// the landing step can refuse it LOUDLY; excluding it here would make it
// silently unlandable forever, which is the failure mode this slice exists to
// remove.
func (t Task) SealedLandingPending() bool {
	return t.Sealed != nil && t.Branch == "" &&
		t.Decision == DecisionApproved && !t.Archived && !t.Blocked &&
		t.Status == StatusPackaged &&
		t.VerifiedSHA != "" && t.TargetRef != ""
}

// LandingPending reports whether this row is owed the §10 step (0c) land
// effect: approved, branch-carrying, not yet landed (unarchived).
//
// NOTE(S6.3): blocked gates the land effect. A failed landing "blocks the
// task with the landing reason ... the operator commits/stashes, or a
// correction task reconciles, then a later tick retries the landing" (§7).
// §6 is categorical that blocked = 1 makes a row invisible to dispatch, and
// Inv. 3 liveness demands it here: an unconditional retry would burn every
// tick's one action on a landing that keeps failing (and head-of-line-block
// the fixed landing order) until the operator fixes the tree. So the retry
// trigger is the ordinary operator unblock (§5 "Operator attention").
// Documented in RESULT.md as a spec-clarification note.
func (t Task) LandingPending() bool {
	return t.Decision == DecisionApproved && !t.Archived && !t.Blocked &&
		t.Branch != "" && t.Status == StatusPackaged
}

// Packet is one row of `review_packets` (spec §5), restricted to the fields
// dispatch reads. Exactly one per task; task_id is both PK and FK.
type Packet struct {
	TaskID    int64
	CreatedAt time.Time
	Saturated bool // computed by the substrate at refine_streak >= 3, never hand-set
	Archived  bool
}

// Records is the canonical durable state the tick recomputes from (§10:
// "the rows are the queue"). LastBriefingAt is the timestamp of the most
// recent `daily.briefing` activity event (nil if none ever) — the stored
// fact step (0b) checks for same-day delivery.
type Records struct {
	Tasks          []Task
	Packets        []Packet
	LastBriefingAt *time.Time
}

// Lock is the execution-lease singleton (spec §10). Held=false is the free
// lock. LastHeartbeatAt is nil for a stamped-but-never-heartbeated slot
// (the first-heartbeat watchdog's case). SubjectID is nil for propose-style
// runs (Strategist(propose)/Strategist(console)).
type Lock struct {
	Held            bool
	RunID           string
	Owner           string
	SubjectID       *int64
	AcquiredAt      time.Time
	LastHeartbeatAt *time.Time
	HardDeadlineAt  time.Time
}

// Config carries the §16.3 knobs dispatch reads.
type Config struct {
	ReviewWIPCap   int // default 3 (Inv. 18)
	TimeoutMinutes int // lease timeout, default 60
	GraceMinutes   int // lease grace, default 15
	SpawnGraceS    int // first-heartbeat watchdog, default 60
	// Daily Console stored schedule: delivery time-of-day in ConsoleLoc
	// (§14, §16.3 "delivery time + IANA timezone").
	ConsoleHour   int
	ConsoleMinute int
	ConsoleLoc    *time.Location // nil = UTC
}

// DefaultConfig returns the spec §16.3 defaults.
func DefaultConfig() Config {
	return Config{
		ReviewWIPCap:   3,
		TimeoutMinutes: 60,
		GraceMinutes:   15,
		SpawnGraceS:    60,
		ConsoleHour:    8,
		ConsoleMinute:  0,
		ConsoleLoc:     time.UTC,
	}
}

// Clock is the injected clock input — the only channel through which wall
// time reaches the decision (Inv. 21).
type Clock struct {
	Now time.Time
}

// ---------------------------------------------------------------------------
// Action — the function's output. Exactly one action or idle per evaluation
// (Inv. 3: each tick commits exactly one action; idle commits zero).
// ---------------------------------------------------------------------------

// Kind discriminates the action union.
type Kind string

const (
	// KindIdle: the tick commits nothing (lease held and fresh, or queue at
	// cap with every packet saturated).
	KindIdle Kind = "idle"
	// KindReap: step (0) — one mutation: mark the run reaped, free the lease,
	// charge/block the subject per budget, and return the container-stop
	// effect to the resident.
	KindReap Kind = "reap"
	// KindSpawn: one claim-and-spawn — claim the lease, open the run, return
	// the spawn effect data.
	KindSpawn Kind = "spawn"
	// KindLand: step (0c) — one land effect for the resident's mc-land
	// container.
	KindLand Kind = "land"
	// KindReenter: step (2b), initiative arm — one pure mutation
	// (packaged → seeded); no spawn this tick, (2a) picks it up later.
	KindReenter Kind = "reenter"
)

// Role names the leased pipeline role a spawn opens (§3; Strategist mode
// lives in the run brief, so the three modes are distinct spawn shapes here).
type Role string

const (
	RoleEditor Role = "editor"
	// RoleEditorPlanReview is the Editor's second mode: the holistic plan
	// review of one wave (§3, §6.1, Inv. 12; ADR-020 D3). A mode, exactly like
	// Strategist's three — baseRole() strips it, so lock.owner and runs.role
	// still store the flat "editor" the schema CHECKs permit, and routing
	// resolves by base role (Inv. 9 holds by construction: the producer is
	// Strategist(initiative), the judge a fresh Editor on a decorrelated
	// runtime).
	RoleEditorPlanReview     Role = "editor(plan-review)"
	RoleWorker               Role = "worker"
	RoleVerifier             Role = "verifier"
	RolePackager             Role = "packager"
	RoleRefiner              Role = "refiner"
	RoleStrategistPropose    Role = "strategist(propose)"
	RoleStrategistInitiative Role = "strategist(initiative)"
	RoleStrategistConsole    Role = "strategist(console)"
)

// IdleReason says which zero-action outcome this is.
type IdleReason string

const (
	IdleLeaseHeld      IdleReason = "lease-held"      // step (0): fresh lease → return
	IdleQueueSaturated IdleReason = "queue-saturated" // step (2b) empty: at cap, nothing in flight, no refinement candidate
)

// ReapReason names which crash-detection threshold fired (§10 step 0).
type ReapReason string

const (
	ReapSpawnWatchdog ReapReason = "spawn-watchdog" // stamped but never heartbeated past spawn_grace_s
	ReapLeaseTimeout  ReapReason = "lease-timeout"  // heartbeated then silent past timeout+grace
	ReapHardDeadline  ReapReason = "hard-deadline"  // still heartbeating past hard_deadline_at
)

// Reap is the step-(0) action payload.
type Reap struct {
	RunID  string
	Reason ReapReason
	// SubjectID / ChargeRetries: reaping a subjectless run (propose/console)
	// charges nothing (§10 step 0).
	SubjectID     *int64
	ChargeRetries bool
	// BlockSubject: the decrement lands the budget at 0 → set blocked with
	// the reason instead of looping silently (§10 step 0).
	BlockSubject bool
	// StopContainer: effect data for the resident — only the host side holds
	// the control socket (§11.6).
	StopContainer bool
}

// Spawn is the claim-and-spawn payload.
type Spawn struct {
	Role Role
	// SubjectID is nil for the subjectless runs (propose, console).
	SubjectID *int64
	// ProposedPool: Editor only — the brief snapshots the *entire* proposed
	// pool for contrastive rank-then-cut, never the subject row alone (§10
	// step 3 table). Ascending id order (deterministic snapshot).
	ProposedPool []int64
	// Wave: editor(plan-review) only — the id set of every open child the
	// holistic verdict is rendered over (ADR-020 D4). Ascending id order.
	// Distinct from ProposedPool, which is named for what it is; both ride
	// runs.pool_snapshot, whose meaning generalizes to "the id set this Editor
	// run was shown and must act on exactly".
	Wave []int64
	// DedupeTitles: Strategist(propose) only — the 20 most recently rejected
	// titles, newest first (§10 step 4).
	DedupeTitles []string
}

// Land is the step-(0c) effect payload (§7 Landing, §10 step 0c).
type Land struct {
	TaskID      int64
	Branch      string
	VerifiedSHA string
	TargetRef   string
}

// Reenter is the step-(2b) initiative-arm mutation payload (§8, §10 step 2b):
// packaged → seeded; Strategist(initiative) carries the deepening as waves.
type Reenter struct {
	TaskID int64
}

// Action is the single evaluation result. Exactly one payload is non-nil for
// the non-idle kinds; KindIdle carries only IdleReason.
type Action struct {
	Kind    Kind
	Idle    IdleReason
	Reap    *Reap
	Spawn   *Spawn
	Land    *Land
	Reenter *Reenter
}

func idle(r IdleReason) Action { return Action{Kind: KindIdle, Idle: r} }

// ---------------------------------------------------------------------------
// Decide — the tick, walked top-down (§10).
// ---------------------------------------------------------------------------

// Decide evaluates one tick against the given records, lock state, and clock
// inputs, returning exactly one action or idle. It is pure: no I/O, no
// globals, inputs are never mutated, and identical inputs always produce the
// identical Action.
func Decide(rec Records, lock Lock, cfg Config, clk Clock) Action {
	// (0) Reconcile the lease. Fresh → return; reapable → reap and return.
	// Dispatch runs only when the lease is free (Inv. 3).
	if lock.Held {
		if reason, ok := reapable(lock, cfg, clk); ok {
			return reapAction(rec, lock, reason)
		}
		return idle(IdleLeaseHeld)
	}

	// (0b) Console due? The one time-triggered dispatch, riding Inv. 21's
	// stored-schedule carve-out (§14).
	if consoleDue(rec, cfg, clk) {
		return Action{Kind: KindSpawn, Spawn: &Spawn{Role: RoleStrategistConsole}}
	}

	// (0c) Landing pending? One land effect ahead of queue selection; with
	// several pending, one per tick in a fixed deterministic order.
	if t, ok := nextLanding(rec); ok {
		return Action{Kind: KindLand, Land: &Land{
			TaskID:      t.ID,
			Branch:      t.Branch,
			VerifiedSHA: t.VerifiedSHA,
			TargetRef:   t.TargetRef,
		}}
	}

	// (1) Queue occupancy: unarchived packets across all Worksources
	// (Inv. 18 — the single global cap). Landing-pending packets still count:
	// the slot frees on landing success, never at approve time (§10 step 0c).
	occ := 0
	for _, p := range rec.Packets {
		if !p.Archived {
			occ++
		}
	}

	if occ >= cfg.ReviewWIPCap {
		// Queue at cap: exactly two moves, then idle (§8, Inv. 19).
		// (2a) Advance an in-flight refinement first.
		if t, ok := inFlightRefinement(rec); ok {
			return spawnFor(rec, t)
		}
		// (2b) Start one on the best non-saturated packet.
		if t, ok := refinementStart(rec); ok {
			if t.Scope == ScopeInitiative {
				// Pure mutation: re-enter packaged → seeded; Strategist(initiative)
				// carries it as ordinary waves, which (2a) then picks up.
				return Action{Kind: KindReenter, Reenter: &Reenter{TaskID: t.ID}}
			}
			id := t.ID
			return Action{Kind: KindSpawn, Spawn: &Spawn{Role: RoleRefiner, SubjectID: &id}}
		}
		// Every queued packet saturated → idle; churning saturated work is
		// negative value (§8).
		return idle(IdleQueueSaturated)
	}

	// Queue has room:
	// (3) the single next dispatch.
	if t, ok := nextDispatch(rec); ok {
		return spawnFor(rec, t)
	}

	// (4) Nothing dispatchable → spawn Strategist(propose) to seed, with the
	// dedupe memory (20 most recently rejected titles).
	return Action{Kind: KindSpawn, Spawn: &Spawn{
		Role:         RoleStrategistPropose,
		DedupeTitles: rejectedTitles(rec, 20),
	}}
}

// ---------------------------------------------------------------------------
// Step (0): lease reconciliation.
// ---------------------------------------------------------------------------

// reapable evaluates the three crash-detection thresholds (§10). A threshold
// fires at exactly its boundary instant (now >= threshold). The check order
// (watchdog, timeout, hard deadline) fixes the reported reason when several
// hold at once; the reap outcome is identical either way.
func reapable(lock Lock, cfg Config, clk Clock) (ReapReason, bool) {
	now := clk.Now
	if lock.LastHeartbeatAt == nil {
		// First-heartbeat watchdog: stamped-but-never-heartbeated is a failed
		// spawn, reaped in seconds.
		if !now.Before(lock.AcquiredAt.Add(time.Duration(cfg.SpawnGraceS) * time.Second)) {
			return ReapSpawnWatchdog, true
		}
	} else {
		// Lease timeout: heartbeated then went silent → reaped on the full
		// lease (timeout + grace) measured from the last heartbeat.
		// NOTE(S6.5): "reaped on the full lease" read as
		// last_heartbeat_at + timeout + grace (documented in RESULT.md).
		full := time.Duration(cfg.TimeoutMinutes+cfg.GraceMinutes) * time.Minute
		if !now.Before(lock.LastHeartbeatAt.Add(full)) {
			return ReapLeaseTimeout, true
		}
	}
	// Hard deadline: still heartbeating past hard_deadline_at is reaped
	// regardless — liveness is not progress.
	if !now.Before(lock.HardDeadlineAt) {
		return ReapHardDeadline, true
	}
	return "", false
}

// reapAction builds the one reap mutation. NOTE(S6.8): "decrement the
// subject's dispatch_retries ... at dispatch_retries = 0, set blocked with
// the reason instead" is read as: the reap always charges a subject-carrying
// run, and when the decrement lands the budget at 0 the same transaction sets
// blocked (never a silent loop).
func reapAction(rec Records, lock Lock, reason ReapReason) Action {
	r := &Reap{RunID: lock.RunID, Reason: reason, StopContainer: true}
	if lock.SubjectID != nil {
		id := *lock.SubjectID
		r.SubjectID = &id
		r.ChargeRetries = true
		if t := findTask(rec, id); t != nil && t.DispatchRetries-1 <= 0 {
			r.BlockSubject = true
		}
	}
	return Action{Kind: KindReap, Reap: r}
}

// ---------------------------------------------------------------------------
// Step (0b): the Daily Console stored schedule.
// ---------------------------------------------------------------------------

// consoleDue: the stored delivery time has passed today (in the configured
// zone) and no same-day daily.briefing event exists (§14, §10 step 0b).
func consoleDue(rec Records, cfg Config, clk Clock) bool {
	loc := cfg.ConsoleLoc
	if loc == nil {
		loc = time.UTC
	}
	now := clk.Now.In(loc)
	delivery := time.Date(now.Year(), now.Month(), now.Day(),
		cfg.ConsoleHour, cfg.ConsoleMinute, 0, 0, loc)
	if now.Before(delivery) {
		return false
	}
	if rec.LastBriefingAt != nil {
		lb := rec.LastBriefingAt.In(loc)
		if lb.Year() == now.Year() && lb.YearDay() == now.YearDay() {
			return false // already delivered today
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Step (0c): landing.
// ---------------------------------------------------------------------------

// nextLanding picks the one land effect for this tick. With several rows
// pending, one lands per tick in a fixed deterministic order until drained.
// NOTE(S6.4): the spec fixes that the order is deterministic but delegates
// its key; approval order (decided_at, then id) is chosen and documented.
func nextLanding(rec Records) (Task, bool) {
	var cand []Task
	for _, t := range rec.Tasks {
		// Deliberately no worksourceActive gate: §10 (0c) has no status
		// qualifier, archive is terminal, and no unpause verb exists — a
		// gated landing would strand approved work and its Inv. 18 slot.
		if t.LandingPending() {
			cand = append(cand, t)
		}
	}
	if len(cand) == 0 {
		return Task{}, false
	}
	sort.Slice(cand, func(i, j int) bool {
		di, dj := timeOrZero(cand[i].DecidedAt), timeOrZero(cand[j].DecidedAt)
		if !di.Equal(dj) {
			return di.Before(dj)
		}
		return cand[i].ID < cand[j].ID
	})
	return cand[0], true
}

// ---------------------------------------------------------------------------
// Steps (2a)/(2b): the at-cap moves.
// ---------------------------------------------------------------------------

// inFlightRefinement is the §10 (2a) query: a non-packaged, unblocked,
// unarchived task joined to an unarchived packet through either arm —
// p.task_id = t.id (a packet's own task that round-tripped back into the
// pipeline) or p.task_id = t.initiative_id (the wave children of a
// packet-holding initiative). Parked initiatives (open children) are hidden;
// their children are their dispatch presence.
//
// Order: stage_rank DESC, priority, created_at — as written in the spec; the
// expedite partition prefix appears only in the with-room query (3), taken
// literally (NOTE(S6.6)). Final id tiebreak added for total determinism
// (NOTE(S6.1)).
func inFlightRefinement(rec Records) (Task, bool) {
	live := make(map[int64]bool, len(rec.Packets)) // task ids with an unarchived packet
	for _, p := range rec.Packets {
		if !p.Archived {
			live[p.TaskID] = true
		}
	}
	var cand []Task
	for _, t := range rec.Tasks {
		if !worksourceActive(t) || t.Archived || t.Blocked || t.Status == StatusPackaged || t.Status.Rank() < 0 {
			continue
		}
		linked := live[t.ID] || (t.InitiativeID != nil && live[*t.InitiativeID])
		if !linked {
			continue
		}
		if !childGate(t) {
			continue // unreviewed wave child (ADR-020 D2(a))
		}
		if t.Scope == ScopeInitiative && hasOpenChildren(rec, t.ID) && !planReviewPending(rec, t) {
			continue
		}
		cand = append(cand, t)
	}
	if len(cand) == 0 {
		return Task{}, false
	}
	sort.Slice(cand, func(i, j int) bool { return lessInFlight(cand[i], cand[j]) })
	return cand[0], true
}

func lessInFlight(a, b Task) bool {
	if ra, rb := a.Status.Rank(), b.Status.Rank(); ra != rb {
		return ra > rb // furthest-first
	}
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
}

// refinementStart is the §10 (2b) query: the best unarchived, non-saturated
// packet whose task is undecided. Order: task priority, packet created_at
// (task id tiebreak added — NOTE(S6.1)).
//
// NOTE(S6.2): §6's categorical rule — blocked = 1 makes a row invisible to
// dispatch — is applied here even though the spec's (2b) SQL omits the
// filter: without it, a blocked packaged task would get a Refiner spawned on
// it (and a blocked, re-entered task skipped by (2a) would reach (2b) and
// receive an illegal packaged→seeded re-entry from a non-packaged status).
// Documented in RESULT.md.
func refinementStart(rec Records) (Task, bool) {
	type pair struct {
		p Packet
		t Task
	}
	var cand []pair
	for _, p := range rec.Packets {
		if p.Archived || p.Saturated {
			continue
		}
		t := findTask(rec, p.TaskID)
		if t == nil {
			continue // inconsistent state; defensive
		}
		if t.Decision != DecisionNone { // decided packets are out of the refinement pool (§10 step 0c)
			continue
		}
		if !worksourceActive(*t) || t.Blocked || t.Archived { // NOTE(S6.2)
			continue
		}
		if t.Status != StatusPackaged {
			// Defensive: a non-packaged, unblocked task with a live packet is
			// (2a)'s to advance; only packaged rows can legally re-enter.
			continue
		}
		cand = append(cand, pair{p: p, t: *t})
	}
	if len(cand) == 0 {
		return Task{}, false
	}
	sort.Slice(cand, func(i, j int) bool {
		a, b := cand[i], cand[j]
		if a.t.Priority != b.t.Priority {
			return a.t.Priority < b.t.Priority
		}
		if !a.p.CreatedAt.Equal(b.p.CreatedAt) {
			return a.p.CreatedAt.Before(b.p.CreatedAt)
		}
		return a.t.ID < b.t.ID
	})
	return cand[0].t, true
}

// ---------------------------------------------------------------------------
// Step (3): the single next dispatch (queue has room).
// ---------------------------------------------------------------------------

// nextDispatch is the §10 (3) query: unarchived, unblocked, stage_rank > 0,
// initiatives hidden while they have open children. Order: expedite partition
// first, then furthest-first, then priority, then age, then id.
func nextDispatch(rec Records) (Task, bool) {
	var cand []Task
	for _, t := range rec.Tasks {
		if !worksourceActive(t) || t.Archived || t.Blocked || t.Status.Rank() <= 0 {
			continue
		}
		if !childGate(t) {
			continue // unreviewed wave child (ADR-020 D2(a))
		}
		if t.Scope == ScopeInitiative && hasOpenChildren(rec, t.ID) && !planReviewPending(rec, t) {
			continue // parked — its children are its dispatch presence
		}
		cand = append(cand, t)
	}
	if len(cand) == 0 {
		return Task{}, false
	}
	sort.Slice(cand, func(i, j int) bool { return lessNextDispatch(cand[i], cand[j]) })
	return cand[0], true
}

func lessNextDispatch(a, b Task) bool {
	ea, eb := a.Priority <= -1, b.Priority <= -1
	if ea != eb {
		return ea // expedite lane partitions ahead of everything
	}
	if ra, rb := a.Status.Rank(), b.Status.Rank(); ra != rb {
		return ra > rb // furthest-first
	}
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
}

// spawnFor maps the selected row to a role by the (status, scope) table
// (§10 step 3; step 2a maps by the same table) and builds the spawn.
func spawnFor(rec Records, t Task) Action {
	id := t.ID
	sp := &Spawn{SubjectID: &id}
	switch t.Status {
	case StatusProposed:
		sp.Role = RoleEditor
		sp.ProposedPool = proposedPool(rec) // batch-wise: the entire pool, never this row alone
	case StatusSeeded:
		switch {
		case t.Scope == ScopeInitiative && planReviewPending(rec, t):
			// The one exception to §10's parked rule: the wave, held to
			// Inv. 12's bar before any child can be worked (ADR-020 D2(c)).
			sp.Role = RoleEditorPlanReview
			sp.Wave = wave(rec, t.ID)
		case t.Scope == ScopeInitiative:
			sp.Role = RoleStrategistInitiative // drained ⇒ owed the next wave or the done-declaration
		default:
			sp.Role = RoleWorker
		}
	case StatusWorked:
		sp.Role = RoleVerifier // task, or initiative arc (completion report + clean merge)
	case StatusVerified:
		sp.Role = RolePackager // task packet / arc packet
	default:
		// Unreachable: both selecting queries exclude packaged/unknown rows.
		panic(fmt.Sprintf("dispatch: no role for status %q", t.Status))
	}
	return Action{Kind: KindSpawn, Spawn: sp}
}

// ---------------------------------------------------------------------------
// Helpers (pure; operate only on the passed records).
// ---------------------------------------------------------------------------

func findTask(rec Records, id int64) *Task {
	for i := range rec.Tasks {
		if rec.Tasks[i].ID == id {
			t := rec.Tasks[i] // copy; never expose the caller's backing array
			return &t
		}
	}
	return nil
}

func worksourceActive(t Task) bool {
	// Empty preserves hand-built Phase-0/1 fixtures; the SQL loader always
	// supplies the authoritative status.
	return t.WorksourceStatus == "" || t.WorksourceStatus == "active"
}

func hasOpenChildren(rec Records, initiativeID int64) bool {
	for _, c := range rec.Tasks {
		if c.InitiativeID != nil && *c.InitiativeID == initiativeID && !c.Archived {
			return true
		}
	}
	return false
}

// planReviewPending: this initiative's wave is owed the Editor's holistic
// plan review (ADR-020 D2). The status conjunct is load-bearing and travels
// with the SQL rendering: without it an initiative past seeded with an open
// child would map to Verifier/Packager rather than the review. It is
// currently unreachable (the birth rules and the strict-drain trigger forbid
// it), but the two forms of one predicate must not disagree.
func planReviewPending(rec Records, t Task) bool {
	if t.Scope != ScopeInitiative || t.Status != StatusSeeded {
		return false
	}
	for _, c := range rec.Tasks {
		if c.InitiativeID != nil && *c.InitiativeID == t.ID && !c.Archived && !c.PlanReviewed {
			return true
		}
	}
	return false
}

// childGate: an unreviewed wave child is invisible to dispatch, full stop
// (ADR-020 D2(a)). On a non-child the flag is not applicable, so it passes.
func childGate(t Task) bool {
	return t.InitiativeID == nil || t.PlanReviewed
}

// wave snapshots every open child of the initiative (ascending id) — the set
// the holistic verdict is rendered over (ADR-020 D4). Archived children are
// not part of the wave: a partially cancelled wave shows only survivors.
func wave(rec Records, initiativeID int64) []int64 {
	var ids []int64
	for _, c := range rec.Tasks {
		if c.InitiativeID != nil && *c.InitiativeID == initiativeID && !c.Archived {
			ids = append(ids, c.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// proposedPool snapshots every open proposal (ascending id) for the Editor's
// contrastive rank-then-cut brief.
func proposedPool(rec Records) []int64 {
	var ids []int64
	for _, t := range rec.Tasks {
		if worksourceActive(t) && t.Status == StatusProposed && !t.Archived && !t.Blocked {
			ids = append(ids, t.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// rejectedTitles: the n most recently rejected titles, newest first
// (§10 step 4's dedupe-memory query; id DESC tiebreak added — NOTE(S6.1)).
func rejectedTitles(rec Records, n int) []string {
	var rej []Task
	for _, t := range rec.Tasks {
		if t.Decision == DecisionRejected {
			rej = append(rej, t)
		}
	}
	sort.Slice(rej, func(i, j int) bool {
		di, dj := timeOrZero(rej[i].DecidedAt), timeOrZero(rej[j].DecidedAt)
		if !di.Equal(dj) {
			return di.After(dj)
		}
		return rej[i].ID > rej[j].ID
	})
	if len(rej) > n {
		rej = rej[:n]
	}
	titles := make([]string, 0, len(rej))
	for _, t := range rej {
		titles = append(titles, t.Title)
	}
	return titles
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
