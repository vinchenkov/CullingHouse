# Phase 2 contract — dispatch + domain correctness (wave 1)

Interface document for the two parallel wave-1 builders (A: dispatch
decision-table coverage; B: domain aggregates + verb rewiring + completion &
fencing). Behavioral contract: `specs/mission-control-spec.md` (spec §…);
operating protocol: `specs/implementation-handoff.md` Part 3 "Phase 2"
(handoff). **The spec wins on conflict with this document.** Prior pins that
still bind: `docs/phase1b-contract.md` (the skeleton), `docs/adr/001-role-side-verbs.md`
(ADR-001), the `NOTE(S6.n)` interpretations in `mc/dispatch/dispatch.go`,
and the `NOTE(P1.n)`/`NOTE(P1b.n)` substrate notes.

**Wave split.** Wave 1 (this contract's build scope): the dispatch decision
table covered to the handoff's bar, and the domain-aggregate layer with
completion & fencing (handoff Phase 2, first three clauses). Wave 2 (seams
only, §6 below): the full §18 verb/error surface, the split-brain kill-point
suite, the nightly randomized property suites.

---

## 1. The domain-aggregate layer

### 1.1 Seat and shape

New package **`mc/domain`** (module `mc`), between `mc/verbs` (CLI plane) and
`mc/substrate` (storage backstop). The layering is the handoff's own (Phase 1
text: the substrate rules "are a redundant backstop; real enforcement lives in
the Phase 2 domain layer"):

- **`mc/domain` = real enforcement.** Every state-law precondition is checked
  in Go and rejected with a *named* `DomainError` before SQL runs; budget and
  streak arithmetic live here; every function is transaction-scoped.
- **`mc/substrate` = redundant backstop.** A domain bug cannot store an
  illegal state (spec §4); domain suites include one *backstop-agreement*
  case per rule class: (a) the domain rejects with its named error, and
  (b) the same write issued as raw SQL still aborts on the trigger.
- **Cascades stay substrate-implemented.** The archive→packet cascade,
  child-cancel cascade, block propagation/auto-clear, and saturation
  computation (schema triggers, NOTE(P1.6)/(P1.8)/(P1.12)) are *writes*, not
  validations — one implementation, in the lattice, exactly as Phase 1b
  already relies on (`verbs.PacketDecide` archives; the trigger cascades).
  Domain suites assert cascade *outcomes*; they do not re-implement them.
  Deviates least; a Go re-implementation would be the drifting duplicate.

**API style, pinned.** Each aggregate exposes transaction-scoped functions
`func X(ctx context.Context, q Q, …) (Result, error)` operating inside the
caller's `BEGIN IMMEDIATE` (the `verbs.Q` interface moves to `mc/domain` and
is re-exported by `mc/verbs` so existing signatures survive). Pure helpers
(threshold math, budget arithmetic, ordering) are separate `func`s where the
math is separable — mirroring `mc/dispatch`'s pure core. Domain functions
never open connections, never BEGIN/COMMIT, never read the wall clock: a
timestamp needed for arithmetic arrives as an explicit `now time.Time`
parameter whose only production source is `SELECT datetime('now')` inside the
same transaction (spec §10 clock discipline — all lease math on one clock,
the lock domain's own, injectable for tests *by parameter*, exactly like
`dispatch.Clock`). Stamps (not arithmetic) keep using `datetime('now')`
directly in SQL.

**Errors.** `DomainError` gains a stable `Code` slug (`stale-run`,
`role-mismatch`, `illegal-transition`, `wip-cap`, `budget-exhausted`,
`correction-required`, `not-packaged`, `already-decided`, `strict-drain`,
`zero-promotion`, `pool-mismatch`, …). Wave 1 sets codes at every domain
rejection; the CLI keeps its Phase 1b contract (exit 1, message on stderr) —
rendering codes in the stdout JSON is wave 2's §18 error-surface decision.
This is the seam that lets wave 2 declare error paths without re-touching
every site.

### 1.2 The aggregates (one file + one suite each, handoff Phase 2 list)

| Aggregate | File | API (tx-scoped unless marked pure) | Spec |
|---|---|---|---|
| Task state machine | `mc/domain/task.go` | `Promote`, `RejectProposal` (Editor arms); `AdvanceStage` (seeded→worked incl. initiative done-declaration, verified→packaged); `ApplyVerdict(VerdictArgs)` — outcomes PASS / CORRECT / BUDGET-SPENT per the §7 table; `Reenter(task, notes)` (packaged→seeded — operator revise, Refiner re-entry, and dispatch step 2b's initiative arm are the *same* transition, Inv. 11); `Block`/`Unblock`; `Approve`/`Cancel` decision writes | §6, §7 |
| Dispatch-retry budget | `mc/domain/retry.go` | `ChargeInfra(subject, reason)` — decrement `dispatch_retries`; at 0 set `blocked` with the reason in the same transaction, never a silent loop. Two callers only: the reap applier and `mc complete --infra` | §10 "Two budgets", §16.3, §18 `--infra` |
| Initiative | `mc/domain/initiative.go` | `BirthWave(initiative, children)` — whole wave or nothing (ADR-001 constraint a; §10 crash table); done-declaration rides `AdvanceStage` with the strict-drain check named in domain (`strict-drain`) ahead of the trigger; block propagation/auto-clear asserted against the lattice | §6.1 |
| Review packet | `mc/domain/packet.go` | `Birth(task, renderPath)` — requires live `packaged` task, WIP-cap named error ahead of the trigger; `ApplyDeepening(task, genuine bool)` — reset streak on genuine, increment on churn; saturation is *read*, never written (trigger-computed, NOTE(P1.8)) | §7, §8, Inv. 11/18 |
| Review queue | `mc/domain/queue.go` | `Occupancy(ctx, q)` (the §10 step-1 count, global across Worksources, Inv. 18) used by `packet.Birth`; the queue *suite* builds at-cap states through domain ops and asserts (a) cap enforcement both layers, (b) at-cap selection order by driving the frozen `mc/dispatch.Decide` as a black-box oracle over the stored state | §8, §10 (1)/(2a)/(2b) |
| Lease | `mc/domain/lease.go` | `Claim(ClaimArgs)` — CAS on the free row + runs-row insert (Inv. 4), stamps `hard_deadline_at`; `Heartbeat(runID)` — fenced, may never touch `hard_deadline_at` (Inv. 1); `Fence(runID)` (today's `verbs.fenceRun`); `Release(runID)` (fenced); `ApplyReap(reap)` — mark reaped → `retry.ChargeInfra` when subject-carrying → free the lease, one transaction. The three reap *conditions* are not re-implemented: they live in `mc/dispatch.reapable` (frozen, S6-proven); domain owns the write side | §10 lock/lease, Inv. 1/4 |

`mc/verbs` keeps: CLI-plane identity (`LoadIdentity`, `requireRole`,
`RunJSONPath`), flag/payload validation, JSON rendering, and the
`inTx` driver. Everything that states or changes *state law* moves to domain.

### 1.3 Verb → aggregate rewiring (CLI contract preserved)

The Phase 1b CLI contract (verb names, argv, stdout JSON, exit codes —
phase1b-contract §2) is **stable**; `cmd/mc` tests keep their meaning. The
rewiring, verb by verb:

| Verb | Wave-1 change | Aggregates |
|---|---|---|
| `mc dispatch` | `applySpawn`/`applyReap`/reenter bodies move behind `lease.Claim`/`lease.ApplyReap`/`task.Reenter`; `Decide()` untouched; ConsoleHour sentinel replaced by the stored schedule (§4.3) | lease, retry, task |
| `mc complete --status worked` | via `task.AdvanceStage`; **role map fix**: subject scope `task` → `worker`, scope `initiative` → `strategist` (the done-declaration, ADR-001 D4 "not new verbs") | task, lease |
| `mc complete --status packaged` | via `task.AdvanceStage` + `packet.Birth` (same tx, Inv. 10/11); on a re-packaging (task already holds an unarchived packet) it re-renders in place — update `render_path`, no second birth (Inv. 11 "exactly one per task, for life") | task, packet, lease |
| `mc complete --status seeded` | **new arm**: the Refiner terminal — packaged→seeded re-entry with `--outputs <deepening scope>` carried as notes (§8; role `refiner`); see Ambiguity A-P2-2 | task, lease |
| `mc complete --needs-operator --reason` | **new arm**: terminal that blocks the subject (flag, not status — §6) and releases the lease; run outcome `blocked` | task, lease |
| `mc complete --infra --reason` | **new arm**: terminal charging `dispatch_retries`, status untouched; run outcome `infra-failed` | retry, lease |
| `mc complete --correction-count` | stays parse-rejected permanently — superseded by `mc verifier verdict` (ADR-001); deviation note | — |
| `mc verifier verdict` | `correct` and `budget-spent` arms land via `task.ApplyVerdict`; `--correction <path>` required for correct, `correction_count++` (CHECK 0–3 backstop); budget-spent requires count = 3, advances with the exception label; `--deepening genuine\|churn` required iff the subject holds an unarchived packet (the refinement-round-trip fact, derived, no new column) → `packet.ApplyDeepening` in the same tx; verdict record = additive `runs` columns (§4.2) | task, packet, lease |
| `mc editor decide` | `reject` arm (`decision='rejected'`+archive, reason mandatory) and the zero-promotion guard (reject an all-reject batch when no unarchived, unblocked, dispatchable row exists outside the proposed pool — ADR-001 D4) via `task.Promote`/`task.RejectProposal` | task, lease |
| `mc strategist propose` | unchanged surface; insert moves behind a domain birth helper | task, lease |
| `mc strategist wave` | **new verb** (ADR-001 D4): whole-wave atomic birth into a live still-seeded initiative | initiative, lease |
| `mc packet decide` | `--revise` → `task.Reenter` with `--reason` stored as the carried notes (§7: "notes carried into the next run brief"; packet keeps its slot, Inv. 11); `--cancel` → `task.Cancel` (decision+archive; initiative cascade via lattice); approve unchanged | task |
| `mc task block\|unblock` | **new verbs** (§18; host + pipeline-own-subject per ADR-001 D6) | task |
| `mc land report` | failure arm gains its deferred tests; writes unchanged | task |
| `mc heartbeat`, `mc run register-session` | move behind `lease.Heartbeat` / stay as-is (own-row semantics, fixed in the 1b review — runner.go) | lease |

**Legitimate test rewrites** (the only ones): cmd/mc tests that assert the
skeleton's *parse-and-reject "[P2] deferred"* behavior flip to assert the real
behavior — `TestCompleteDeferredFlagsRejected`, the deferred arms inside
`TestPacketDecideValidation` (revise/cancel), `TestVerifierVerdictValidation`
(correct/budget-spent/--deepening), the Editor reject-deferral case — plus
`TestPipelineWalk`'s role expectation wherever the done-declaration role fix
bites. Every other cmd/mc assertion is contract and stays.

---

## 2. The two budgets — never blurred (spec §10 "Two budgets, distinct")

| | `correction_count` | `dispatch_retries` |
|---|---|---|
| Meaning | rally quality budget, ≤ 3 (§7) | infra/dispatch-death budget (§10, §16.3) |
| Owner | task aggregate (`ApplyVerdict`) | retry aggregate (`ChargeInfra`) |
| Only writers | `mc verifier verdict --outcome correct` (increment) | the reaper (`lease.ApplyReap`) and `mc complete --infra` (decrement) |
| Exhaustion | fourth verdict must be `budget-spent`: ships exception-labeled (§7) | `blocked` with reason, same tx (§10 step 0) |
| Backstop | `CHECK (correction_count BETWEEN 0 AND 3)` | `CHECK (dispatch_retries >= 0)` |

Structural separation: no domain function names both columns; `ApplyVerdict`
has no access to `dispatch_retries`, `ChargeInfra` none to
`correction_count`. Pinned by tests both ways: reaping a lease whose owner is
`verifier` mid-rally leaves `correction_count` untouched (§10 crash table);
a CORRECT verdict leaves `dispatch_retries` untouched; `--infra` on a run
whose brief was a correction round leaves `correction_count` untouched.

---

## 3. Completion & fencing — coverage plan

Already proven in `mc/cmd/mc/cli_test.go` (keep, do not rewrite):
`mc complete` returns no effect data and frees the lease
("never dispatches", Inv. 3 — cli_test.go ~394–421); terminal replay rejected
(stale-run fencing); stale heartbeat rejected (~378); register-session lands
after lease release (own-row identity, not lease fencing — the 1b review fix,
ADR-001 D6 "(own run)"); approve is a pure state write with no filesystem
effect (§7; ~500); CAS claim with 4 concurrent claimants → exactly one winner
(`TestDispatchCASSingleWinner`); the role/tier/fencing refusal matrix
(`TestRoleScopeEnforcement`).

Wave 1 adds (builder B):

- **Zombie-vs-new-holder** (the §10/§18 sentence not yet tested): after reap
  + re-claim, the *old* run's `mc complete` and `mc heartbeat` are rejected
  **and the new lease is bit-for-bit untouched** (heartbeat, deadline,
  subject). Location: `mc/domain/lease_test.go` (deterministic, tx-level) +
  one CLI walk in `cli_test.go`.
- **Heartbeat never extends `hard_deadline_at`** (Inv. 1): assert the column
  before/after a fenced heartbeat. `mc/domain/lease_test.go`.
- **Pure-write completeness across every terminal**: each new arm
  (verdict correct/budget-spent, complete seeded/needs-operator/infra, wave,
  reject batch) returns no effect data, frees the lease, and advances state
  only per its own table row — the next `mc dispatch` (and only it) selects
  the follow-on stage (§10 "Completion is a pure write"). `cli_test.go`.
- **CAS single-winner at the domain grain**: `lease.Claim` against a held
  lock aborts before any write lands (the in-tx backstop branch in today's
  `applySpawn`). `mc/domain/lease_test.go`.
- **Fencing on the new verbs**: every wave-1 terminal (`strategist wave`,
  verdict arms, complete arms) rejects a non-matching `--run` and is
  role-matched per ADR-001 D2 — extend the `TestRoleScopeEnforcement` matrix
  rather than writing per-verb copies.

---

## 4. Dispatch decision table — builder A's gap list

### 4.1 What S6 already proves (frozen; do not duplicate)

`mc/dispatch/dispatch_test.go` (~988 lines): every step's primary taken and
not-taken branch — (0) fresh-idles / three thresholds each with a boundary
not-taken for watchdog and timeout / budget-exhaust block / subjectless
charge-nothing; (0b) due, never-briefed, before-time, already-today, beats
landing+queue, fires at cap; (0c) ahead of queue, multi-pending fixed order,
blocked-pending held (NOTE(S6.3)), branchless never lands; occupancy +
archived-packet frees; (2a) re-entered advance, role table, wave-child at
cap, parked/blocked invisibility, 4-key ordering; (2b) Refiner vs reenter,
ordering, saturated/decided/blocked invisibility, all-saturated idle; (3)
role table, furthest-first, expedite partition (+ within-partition), full
tiebreak chain, archived/blocked/rank-0/parked invisibility, drained
initiative, blocked-initiative-unblocked-child; (4) empty spawns propose,
hidden-rows spawn propose, dedupe-20; the many-obligation precedence cascade.
`property_test.go`: zero-or-one action, determinism, purity, time-invariance
(Inv. 21) over 5000 randomized states.

### 4.2 The concrete gaps wave 1 closes (tests only, in `mc/dispatch/`)

1. **Reap-reason precedence under simultaneous thresholds** (dispatch.go
   fixes the check order watchdog → timeout → hard-deadline): (a)
   never-heartbeated + past hard deadline → `spawn-watchdog`; (b)
   heartbeated-stale + past hard deadline → `lease-timeout`; outcome
   identical either way (assert the Reap payload equal modulo Reason).
2. **Hard-deadline boundary not-taken** (one second before
   `hard_deadline_at`, heartbeat fresh → idle lease-held).
3. **Reap with `DispatchRetries` already 0** → `BlockSubject` still set
   (the `-1 <= 0` clamp edge; §10 "never a silent loop").
4. **Console in a non-UTC `ConsoleLoc`**: delivery computed in the stored
   zone while `Clock.Now` is UTC — due after local delivery time, not due
   before, same-day suppression keyed on the local calendar day (§16.3
   "delivery time + IANA timezone"); plus a `ConsoleMinute ≠ 0` case.
5. **Console briefed-yesterday-after-delivery** → due today (staleness is
   calendar-day, not 24 h).
6. **Landing-pending occupancy composite** (§10 step 0c "still counts toward
   queue occupancy"): approved+branch+blocked task (landing held) whose
   packet is 1 of 3 unarchived → no land effect, occupancy = cap, tick goes
   to (2a)/(2b) — the excluded row would otherwise win step 3 as… nothing;
   the *at-cap mode itself* is the assertion.
7. **(0c) tie order**: two pending with equal `decided_at` → lower id lands
   first (NOTE(S6.4) key pinned).
8. **(2a) archived-packet link invisible**: a task whose only packet link is
   an *archived* packet would win (2a) by rank — must be invisible; a lower
   row with a live link is selected instead (invisibility-with-winner rule).
9. **(2a) expedite non-partition pin** (NOTE(S6.6)): at cap, a P-1 low-rank
   in-flight row vs a P2 higher-rank one → higher rank wins (the expedite
   prefix exists only in query (3), taken literally).
10. **(2a) re-entered *initiative* (drained) at cap** → `strategist(initiative)`
    spawned via the (2a) arm `p.task_id = t.id` (the arc-packet round-trip,
    §8 move 1 second sentence).
11. **(3) Editor pool excludes archived proposals**: a rejected+archived
    proposal must not appear in `ProposedPool` while a live proposal
    dispatches the Editor (snapshot honesty; ADR-001 D4 coverage check
    depends on it).
12. **(4) dedupe tie order**: equal `decided_at` → id DESC (NOTE(S6.1) pin).
13. **Occupancy is global across Worksources** (Inv. 18): 3 unarchived
    packets on three different Worksources cap a fourth Worksource's
    dispatch into refinement mode.

Builder A also owns keeping `property_test.go`'s generator honest against
any new record fields (none expected in wave 1 — the dispatch projection is
unchanged).

### 4.3 The ConsoleHour=24 sentinel (deviation D-mc-4) — decided

Wave 1 **replaces the hardcoded sentinel with the stored §16.3 schedule**,
kept fail-closed: three additive lock-row tunables
`console_hour INTEGER NOT NULL DEFAULT 24` / `console_minute … DEFAULT 0` /
`console_tz TEXT NOT NULL DEFAULT 'UTC'` (`NOTE(P2.1)`), settable via
`mc init` flags; `verbs.Dispatch` loads them instead of the constant. The
default (hour 24 = never due) preserves every existing behavior byte-for-byte
until an operator/test configures a real time — so the sentinel survives as
the *stored not-configured value*, no longer a hardcode. `mc console publish`
(the Strategist(console) terminal, ADR-001 D4) stays **wave 2** with the rest
of the §18 surface; a console configured before wave 2 would spawn a run that
reaps subjectless and harmlessly (charges nothing, §10 step 0) — a test-only
path, noted as such. Deviation note below.

### 4.4 Loader differential tests (builder B, `mc/verbs/`)

`dispatch_test.go`'s purity never exercises `loadRecords`/`loadLock`/
`applyAction`. Builder B adds a differential suite in
`mc/verbs/dispatchverb_test.go`: states written through real SQL (NULL
columns, derived landing-pending per NOTE(P1.9)/(S6.3), wave children,
briefing activity rows) → `verbs.Dispatch` output equals the effect implied
by `dispatch.Decide` over the hand-built projection. At least one case per
Action kind, plus NULL-handling (`subject`, `decided_at`, `last_heartbeat_at`,
`branch`) and the reenter write (packaged→seeded actually lands).

---

## 5. Additive schema changes (builder B; each carries a `NOTE(P2.n)`
comment in schema.sql and a deviation-log entry; substrate suite must stay
green unmodified except new-column coverage)

- `NOTE(P2.1)` — `lock.console_hour` / `lock.console_minute` /
  `lock.console_tz` (§4.3 above; §16.3 timing knobs, lock-row tunable
  pattern of NOTE(P1b.1)).
- `NOTE(P2.2)` — verdict record on the verifier's own runs row:
  `runs.verdict_outcome TEXT CHECK (… IN ('pass','correct','budget-spent'))`,
  `runs.evidence_path TEXT`, `runs.correction_path TEXT`,
  `runs.deepening TEXT CHECK (… IN ('genuine','churn'))` — ADR-001 D4 "one
  transaction writes the verdict record (gate-ladder evidence path
  included)"; the next Worker brief's correction file is *queried* from the
  subject's latest correct-verdict run, no task column needed.
- `NOTE(P2.3)` — `tasks.refine_notes TEXT`: the carried notes of §7
  ("Revise → … notes carried into the next run brief") and §8's Refiner
  deepening scope; written by `task.Reenter`, read at spawn-brief assembly,
  cleared on the next packaging.

No new triggers in wave 1. Anything beyond these three rides a fresh
deviation note first.

---

## 6. Wave-2 seams (named now, not designed)

- **Split-brain kill points.** The non-atomic boundaries (handoff Phase 2:
  action selected / container created / files modified / git commit / status
  written / outbox insert / delivery) are all *between* `mc` transactions —
  inside the resident's effector sequence and the runner. Injection will ride
  the already-injectable seams: `resident` `TickDeps`
  (`{…, runMc, docker, fs}` — fault-wrapping stubs), fake-harness behavior
  `crash`/`hang` steps, and killing the runner between `mc` calls. Wave 1
  must therefore **not foreclose**: (i) every verb stays exactly one
  `BEGIN IMMEDIATE` transaction (domain functions take `Q`, never
  open/commit — §1.1) so the kill-point set stays the between-verbs set;
  (ii) `TickDeps` injectability survives any resident change; (iii) no fault
  hooks in the `mc` binary itself.
- **Nightly property suites** (handoff: dispatch state fuzzer, metamorphic
  invisibility pass, lifecycle random walk with the trigger lattice as
  differential oracle, generator-honesty + planted-mutant gates) live in a
  new **`mc/property/`** package behind a `//go:build nightly` tag
  (non-gating, the 1b build-tag precedent). Wave 1 must keep
  `dispatch.Records`/`Lock`/`Config`/`Action` exported and stable, and keep
  every domain op drivable against a raw temp spine (the oracle needs both
  layers reachable).
- **Full §18 verb/error surface** (wave 2): `mc console publish`, homie
  family, `mc outbox poll|ack`, `mc worksource …`, `mc initiative add`,
  `mc task interrupt`, `mc doctor|backup|reset|onboard`, stdout error-JSON
  rendering of the §1.1 error codes, and the per-scope refusal matrix as a
  test matrix (ADR-001 Consequences). Wave 1's contribution is only the
  `Code` field and the domain seams they will call into.

---

## 7. Directory ownership (parallel, collision-free)

- **Builder A** owns `mc/dispatch/**` — **tests only**. `dispatch.go` is
  frozen (S6-promoted, phase1b-contract §3): if a §4.2 test proves a genuine
  behavior gap, builder A commits the red test skipped
  (`t.Skip("NOTE(P2-gap-n): …")`) with a written finding and routes the code
  change through the integrator; it never edits `dispatch.go` itself.
- **Builder B** owns `mc/domain/**` (new), `mc/verbs/**`, `mc/cmd/mc/**`,
  and the §5 additive schema changes in `mc/substrate/schema.sql` (+ their
  new-column substrate tests appended to `substrate_test.go`; every existing
  substrate case stays untouched and green).
- Shared read-only: builder B imports `mc/dispatch` (black box) for the §4.4
  differential and queue-order suites; builder A imports nothing of B's.
- Neither touches `resident/`, `runner/`, or `mc/e2e/` in wave 1 (the e2e's
  behaviors gain the new verbs at integration, with the Phase 4 work).
- Fast lane stays the gate: `mc/check.sh` + the three Bun `check.sh`s,
  gofmt/vet clean (incl. `-tags docker_e2e`), no cgo, no Docker in Phases
  1–2 tests (AGENTS.md §3).

---

## 8. Ambiguities — conservative resolutions pinned

- **A-P2-1 — `refine_streak` apply point** (ADR-001 open question 2 said
  "at re-packaging"). Pinned: applied **at the rally-ending verdict**
  (pass or budget-spent) *when the subject holds an unarchived packet* — that
  live-packet fact is the refinement-round-trip marker, derived, needing no
  carrier column between verify and package; the write target (the packet)
  exists for life (Inv. 11) and the §8 semantics ("a Verifier pass is a
  genuine deepening, a fail is churn") are judged exactly where the ADR put
  the `--deepening` input: the Verifier. Budget-spent on a refinement round
  is churn (increment) — `--deepening` still required, `genuine` rejected
  with it. Conservative: zero new schema, one transaction, reversible by
  moving one call. → deviation note.
- **A-P2-2 — the Refiner's terminal verb.** §8 gives the Refiner one output
  (scope a single deepening, re-enter packaged→seeded); §18/ADR-001 name no
  verb for it. Pinned: it rides `mc complete <task> --run <id> --status
  seeded --outputs <scope-file>` — a plain subject-status advance, exactly
  the ADR-001 "not new verbs" pattern for the done-declaration; role-matched
  `refiner`, notes land in `tasks.refine_notes`. → deviation note.
- **A-P2-3 — done-declaration role match.** `mc complete --status worked` on
  an initiative subject requires run.json role `strategist` (ADR-001 D4 makes
  the done-declaration Strategist(initiative)'s terminal; the skeleton's
  worker-only mapping was scope-blind). Task subjects stay `worker`.
- **A-P2-4 — `--correction-count` on `mc complete`** (§18 grammar) is
  permanently parse-rejected: the Verifier's budget arithmetic moved into
  `mc verifier verdict` by ADR-001 and a second writer would blur budget
  ownership (§2). → deviation note.
- **A-P2-5 — console schedule storage.** §16.3 puts it in `config.toml`
  `[timing]`, but no config layer exists until onboarding (Phase 5). Pinned:
  lock-row tunables with the not-configured default (§4.3), the NOTE(P1b.1)
  pattern; `mc onboard` absorbs them later exactly as it absorbs `mc init`
  (phase1b-contract Ambiguity A1). → deviation note.
- **A-P2-6 — where revise/refine notes live.** §7 names the behavior, no
  storage. Pinned: `tasks.refine_notes` (`NOTE(P2.3)`), overwritten per
  re-entry (the packet render carries history; the row carries only the
  *next* brief's payload). → deviation note.
- **A-P2-7 — wave plan review** (ADR-001 open question 1) stays open: wave 1
  builds `mc strategist wave` exactly as ADR-001 D4 specifies (children born
  `seeded`, immediately dispatchable). Candidate readings (i)–(iii) all
  remain reachable; resolving them is operator/spec territory, not a wave-1
  blocker. Parked, not decided.
- **A-P2-8 — re-packaging vs packet birth.** Inv. 11 (one packet per task,
  for life) means the Packager's completion on a task with a live packet
  must *update* (re-render in place, §8) rather than insert; the substrate's
  PK already forces this. Pinned in `task.AdvanceStage`/`packet` composition
  (§1.3 row 3).
