# S6 — Dispatch decision table as a pure function: RESULT

**Status: GREEN.** The pure function and the exhaustive decision-table test
compile and all cases pass with spec-derived semantics. No point was found
where the state machine or "who is owed what" is genuinely ambiguous — i.e.
where two readings both survive the spec's own text and the table cannot
decide. Eight interpretation points were surfaced and each is decided *by the
spec* (or explicitly delegated by it); they are documented below as
spec-clarification notes so Phase 2 inherits them consciously, not silently.

## Rerun

```sh
/Users/vinchenkov/Documents/dev/ai/homie/spikes/06-dispatch-table/check.sh
```

(gofmt + `go vet` + `go test -count=1`; exits nonzero on any failure. Go via
mise from the repo root config; no Docker.)

## What was built

- `dispatch.go` — `Decide(records, lock, config, clock) → Action` implementing
  spec §10's tick walk as a total, pure, deterministic function: steps (0)
  reconcile-the-lease (three reap thresholds, budget charge/exhaustion,
  subjectless reap), (0b) console-if-due, (0c) landing (one land effect per
  tick, fixed order), (1) queue occupancy, (2a) advance-in-flight-refinement,
  (2b) start-refinement (Refiner spawn for task packets, pure
  `packaged → seeded` re-entry for initiative packets), (3) the single next
  dispatch with the (status, scope) → role table, (4) the Strategist(propose)
  fallback with the 20-title dedupe memory. No I/O, no globals; wall time
  enters only through the injected `Clock` and is consulted only for lease
  thresholds and the console schedule (Inv. 21).
- `dispatch_test.go` — the exhaustive decision table (promoted whole into
  Phase 2): every walk step taken and not-taken, every invisibility rule with
  a would-otherwise-win row, deterministic tie-breaking, the many-obligation
  precedence cascade.
- `property_test.go` — 11k randomized evaluations: exactly zero-or-one
  well-formed action per evaluation always; determinism; input immutability;
  time-invariance of eligibility/ordering (same-day and 40-day clock shifts).

Written as a real Go module (`mcspike/dispatch`, package `dispatch`) so the
suite lifts into Phase 2 unchanged.

## Assertion table

| # | Assertion (handoff Part 2, S6) | Result | Evidence |
|---|---|---|---|
| 1 | Step (0) reconcile taken: spawn-watchdog reap (never heartbeated ≥ `spawn_grace_s`) | PASS | `TestStep0_SpawnWatchdog_NeverHeartbeated` (+ boundary not-taken test) |
| 2 | Step (0) taken: lease-timeout reap (silent ≥ timeout+grace after heartbeat) | PASS | `TestStep0_LeaseTimeout_*` (taken + boundary not-taken) |
| 3 | Step (0) taken: hard-deadline reap while still heartbeating | PASS | `TestStep0_HardDeadline_ReapedWhileStillHeartbeating` |
| 4 | Step (0) reap budget: charge `dispatch_retries`; at 0 block instead of silent loop; subjectless run charges nothing; stop effect always returned | PASS | `TestStep0_ReapExhaustsBudget_BlocksInsteadOfSilentLoop`, `TestStep0_ReapSubjectlessRun_ChargesNothing` |
| 5 | Step (0) not-taken both ways: fresh lease → idle (even with every obligation waiting); free lock → walk continues | PASS | `TestStep0_FreshLeaseIdles_EvenWithEveryObligationWaiting`, `TestStep0_NotTaken_FreeLockWalksOn` |
| 6 | Step (0b) console taken (never briefed / briefed yesterday), incl. precedence over landing+queue and firing at cap | PASS | `TestStep0b_ConsoleDue_*`, `TestStep0b_ConsoleDueAtCap_StillFires` |
| 7 | Step (0b) not-taken: before delivery time; already delivered same-day | PASS | `TestStep0b_NotTaken_*` |
| 8 | Step (0c) landing taken: one land effect (task, branch, verified SHA, target ref) ahead of queue selection; multi-pending drains one per tick in fixed order | PASS | `TestStep0c_LandingPending_ReturnsLandEffectAheadOfQueueSelection`, `TestStep0c_MultiplePending_OnePerTickInFixedOrder` |
| 9 | Step (0c) not-taken: no pending; blocked landing-pending waits for unblock; approved branchless row never lands | PASS | `TestStep0c_NotTaken_NoPendingLanding`, `TestStep0c_BlockedLandingPending_DoesNotRetryUntilUnblocked`, `TestStep0c_ApprovedBranchlessRow_NeverLands` |
| 10 | At cap: no new pipeline dispatch ever (not even expedited P-1 pipeline rows) | PASS | `TestAtCap_NoNewPipelineWorkEver`, `TestStep2b_NotTaken_AllSaturated_Idle` |
| 11 | Step (2a) taken: re-entered task advances at cap; role by the same (status, scope) table as (3); wave children of a packet-holding initiative advance via the join's second arm | PASS | `TestStep2a_AdvancesReenteredTask`, `TestStep2a_RoleMapping_SameTableAsStep3`, `TestStep2a_WaveChildOfPacketHoldingInitiative_AdvancesAtCap` |
| 12 | Step (2a) not-taken: nothing in flight → (2b) | PASS | `TestStep2a_NotTaken_NothingInFlight` |
| 13 | Step (2b) taken: task packet → Refiner spawn; initiative packet → pure re-entry mutation (no spawn) | PASS | `TestStep2b_TaskPacket_SpawnsRefiner`, `TestStep2b_InitiativePacket_ReentersDirectly_NoSpawn` |
| 14 | Step (2b) not-taken: all packets saturated → idle (never falls to (3)/(4) at cap) | PASS | `TestStep2b_NotTaken_AllSaturated_Idle` |
| 15 | Step (3) taken: full (status, scope) → role table, both scopes, incl. Editor batch brief snapshotting the entire proposed pool | PASS | `TestStep3_RoleMapping`, `TestStep3_EditorBriefSnapshotsEntirePool` |
| 16 | Step (3) not-taken → step (4): empty system and only-blocked/packaged/parked systems spawn Strategist(propose); dedupe memory = 20 most recent rejected titles, newest first, non-rejected excluded | PASS | `TestStep4_*` |
| 17 | Invisibility w/ would-otherwise-win row: archived task | PASS | `TestStep3_ArchivedRowInvisible` |
| 18 | Invisibility: blocked task — in (3), in (2a), and in (2b) | PASS | `TestStep3_BlockedRowInvisible`, `TestStep2a_BlockedInFlightRowInvisible`, `TestStep2b_BlockedPackagedTaskInvisible` |
| 19 | Invisibility: packaged rank-0 rows out of pipeline dispatch | PASS | `TestStep3_PackagedRankZeroInvisible` |
| 20 | Invisibility: parked initiative (open children) in (3) and (2a); archived children don't park; blocked initiative's unblocked children keep flowing | PASS | `TestStep3_ParkedInitiativeInvisible_ChildrenAreItsPresence`, `TestStep2a_ParkedInitiativeInvisible_ChildWins`, `TestStep3_DrainedInitiative_ArchivedChildrenDontPark`, `TestStep3_BlockedInitiative_UnblockedChildKeepsFlowing` |
| 21 | Invisibility: saturated packet out of the (2b) pool; decided (landing-pending) packet out of the (2b) pool; archived packet out of occupancy | PASS | `TestStep2b_SaturatedPacketInvisible`, `TestStep2b_DecidedPacketInvisible`, `TestOccupancy_ArchivedPacketFreesTheQueue` |
| 22 | Deterministic tie-breaking: (3) expedite partition → stage_rank DESC → priority → created_at → id; (2a) rank → priority → created_at → id; (2b) priority → packet age → id; landing decided_at → id | PASS | `TestStep3_ExpediteLanePartitionsAheadOfStageOrder`, `TestStep3_WithinExpeditePartition_FurthestFirstStillApplies`, `TestStep3_PriorityThenAgeThenID`, `TestStep2a_Ordering_FurthestFirstThenPriorityThenAgeThenID`, `TestStep2b_Ordering_PriorityThenPacketAgeThenID`, `TestStep0c_MultiplePending_OnePerTickInFixedOrder` |
| 23 | Exactly zero-or-one action per evaluation, always; well-formed payloads | PASS | `assertWellFormed` on every table case + `TestProperty_ExactlyZeroOrOneAction_DeterministicAndPure` (5000 random states) |
| 24 | Purity: deterministic under repetition; inputs never mutated | PASS | same property test (double-evaluate + deep-compare inputs) |
| 25 | Inv. 21 time-invariance: with lease free and console quiet, wall clock never changes the decision (6 h and 40-day shifts) | PASS | `TestProperty_TimeInvariance_SameDayAfterBriefing`, `TestProperty_TimeInvariance_AcrossDaysBeforeDelivery` |
| 26 | Many-obligation precedence state: reap → console → land → (2a) → (2b) → idle → (3) → (4), asserted by stripping one obligation at a time from an everything-owed state | PASS | `TestPrecedenceCascade_ManyObligationState` |

Nothing DEFERRED: S6 needs no Docker and no restart drill.

## Documented interpretations (spec-clarification notes, not ambiguities)

Each point below has one reading the spec's own text selects; none blocks the
table. They are listed so the spec owner can fold one-line clarifications into
§10 and so Phase 2 doesn't rediscover them. Codes match `NOTE(S6.n)` comments
in `dispatch.go`.

- **S6.1 — final `id` tie-breaker.** The spec's (3) query ends in `..., id`;
  the (2a)/(2b)/dedupe queries omit a final key, so two rows equal on every
  listed key would order nondeterministically. Extended (2a)/(2b)/dedupe with
  the same terminal `id` key (3) already uses. Least-deviation, and required
  by the spike's own determinism assertion.
- **S6.2 — `blocked` in (2b).** §6 is categorical ("`blocked = 1` makes a row
  invisible to dispatch") but the (2b) SQL omits `t.blocked = 0`. Without it,
  a blocked packaged task would get a Refiner spawned on it, and a blocked
  re-entered task skipped by (2a) would reach (2b) and receive an illegal
  `packaged → seeded` re-entry from a non-packaged status. §6 governs; the
  filter is applied. Suggest adding `AND t.blocked = 0` (and, defensively,
  `AND t.status = 'packaged'`) to the §10 (2b) query text.
- **S6.3 — blocked landing-pending rows do not retry until unblocked.** §7
  says a failed landing "blocks the task ... a later tick retries the
  landing"; §6 says blocked rows are invisible to dispatch. Read together:
  the block removes the row from step (0c) (otherwise a permanently failing
  landing would burn every tick's one action and head-of-line-block the
  landing drain), and the retry trigger is the ordinary operator unblock
  (§5's steer + unblock answer path). This is the one note worth an explicit
  sentence in §7/§10: "the retry fires on the first tick after the task is
  unblocked."
- **S6.4 — landing drain order.** §10 (0c) fixes that multiple pending
  landings drain "in a fixed deterministic order" but delegates the key.
  Chosen: approval order — `decided_at`, then `id`.
- **S6.5 — lease-timeout base.** "A run that heartbeated then went silent is
  reaped on the full lease" is read as `last_heartbeat_at + timeout + grace`
  (the hard deadline separately bounds total runtime). Thresholds fire at the
  exact boundary instant (`now >= threshold`).
- **S6.6 — expedite lane at cap.** The `(priority <= -1)` partition prefix
  appears only in the with-room query (3); the (2a) at-cap ordering is
  `stage_rank DESC, priority, created_at` as written. Taken literally: at cap
  refinement advancement is furthest-first regardless of expedite. (Priority
  still orders within a rank.) Flagging in case the intent was for P-1 to
  partition there too.
- **S6.7 — `worksource.status` / `seeding_mode` are not dispatch inputs.**
  No §10 query consults Worksource status (`paused`) or `seeding_mode`; the
  decision table as specified is complete without them. If `paused` is meant
  to gate dispatch of that Worksource's rows (or `propose-only` to gate the
  step-(4) seeding target), that is presently unspecified — out of scope for
  this table, worth a spec sentence eventually.
- **S6.8 — reap budget semantics.** "Decrement ... at `dispatch_retries = 0`,
  set blocked instead" is read as: the reap always charges a subject-carrying
  run, and when the decrement lands the budget at 0 the same transaction sets
  `blocked` (never a silent loop).

## Artifacts

No Docker artifacts (S6 is the no-Docker spike). Files, all under
`/Users/vinchenkov/Documents/dev/ai/homie/spikes/06-dispatch-table/`:
`go.mod`, `dispatch.go`, `dispatch_test.go`, `property_test.go`, `check.sh`,
`RESULT.md`.

## Phase 2 promotion notes

- The suite is a self-contained Go module; promote by moving the package under
  `mc`'s module and pointing the domain layer's record projections at these
  types (or mapping them).
- The `NOTE(S6.n)` markers in `dispatch.go` are the exact spots to revisit if
  the spec owner clarifies differently.
- The property suite's generator leans substrate-legal (WIP cap ≤ 3,
  archive-requires-decision, approve-only-from-packaged); when the Phase 1
  trigger lattice lands, the lifecycle random walk can replace it as the
  differential oracle (handoff Part 3, Phase 2).
