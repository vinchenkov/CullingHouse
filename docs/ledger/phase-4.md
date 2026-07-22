# Phase 4 ledger — E2E control-loop scenario families

Append-only history, newest last. Never a startup read; grep it for rationale.
Scope: handoff Part 3 "Phase 4" — six fake-harness scenario families, real
containers/spine/resident, all progress timer-driven.

## 2026-07-22 — Phase 4 opened

Phase 3 closed COMPLETE (all seven phase3-contract §8 bullets green; operator
signed off on advancing). Phase 4 begins with scenario family (1) full
pipeline + landing. The happy-path pipeline already exists as
`TestWalkingSkeleton` (docker_e2e); Phase 4 adds the approve/land SPLIT
assertion and the landing-failure and multi-approve-drain variants, and will
force closed the three non-blocking landing loose ends carried from Phase 3
(unenforced 15-min landing deadline, `SealedLandingResult` has no spine
consumer, ADR-016 D7 label non-conformance in setup/legacy-land — all in
`IMPLEMENTATION-NOTES.md` 2026-07-20/21). Recon of the scenario-1 gap vs.
existing coverage is in flight.

NEXT: from the recon, build scenario (1) — start with the approve/land split
assertion on the existing pipeline, then the landing-failure variant (which
needs the landing failure taxonomy to be observable), closing the landing
loose ends as the variants demand them.

## 2026-07-22 — scenario (1) landing-failure + recovery variant DONE

Commit adds `mc/e2e/landing_failure_test.go`
(`TestLandingFailureAndRecoveryDockerBoundary`, green 12.6s real containers).
Deterministic refusal mechanism: an untracked `skeleton.txt` in the operator
checkout at the path the reviewed branch adds → mc-land's operator-byte
protection refuses in preflight (exit 77) before any merge. The legacy land()
reports ANY nonzero mc-land exit as `--status failure` → `blocked=1`,
status/decision retained, main unmerged, worktree/branch intact. Recovery =
remove obstruction + `mc task unblock` → next tick re-lands (LandingPending
gates on !blocked, dispatch.go:186-197). Did NOT need any of the three landing
loose ends. Scenario (1) status: approve/land split ✓ (TestWalkingSkeleton),
landing-failure ✓ (this), multi-approve-drain still TODO.

NEXT: scenario (1) multi-approve-drain — two/three tasks through the pipeline
to packaged, approve all, assert nextLanding drains them one-per-tick in
(decided_at, id) order and all archive. Then scenario (1) is complete; move to
family (2) correction rally.

## 2026-07-22 — scenario (1) COMPLETE (multi-approve-drain green)

`mc/e2e/multi_approve_drain_test.go` (`TestMultiApproveDrainDockerBoundary`,
green 12.2s). Family (1) now fully covered: approve/land split
(TestWalkingSkeleton), landing-failure+recovery
(TestLandingFailureAndRecovery), multi-approve-drain (this). Custom behaviors
overwrite f.base/behaviors/{editor,worker}.json AFTER setup(), BEFORE
startResident — the roleBehaviors config already points at the bind-mounted
dir, so no resident.json patch is needed.

Two findings from the reap-loop debug (live container/spine inspection while
the test ran):
- MC_POOL_IDS is COMMA-separated (agent-runner main.ts:60). The default
  single-task editor gets away with `printf '...%s...' "$MC_POOL_IDS"` only
  because one id has no comma; any multi-id editor batch must `tr ',' ' '`
  first or EditorDecide refuses it (pool-coverage, ADR-001 D4) and the editor
  is reaped and re-spawned in a loop.
- POSSIBLE ORPHAN-SWEEP GAP (probe in scenario 5): a fast-failing editor spawn
  (container exits nonzero, --rm removes it) left its run row OPEN
  (outcome=null, lease held) for ~2 min without being reaped, while a sibling
  failed run was reaped promptly. The stuck lease starved dispatch (idle
  ticks) until the test timed out. Worth a fault-matrix scenario: a spawn
  whose container dies immediately must be reaped promptly, not left holding
  the lease until timeout_minutes.

NEXT: full docker_e2e regression to confirm all families-so-far green
together, then scenario family (2) correction rally — 3 Verifier CORRECTs → 4th
ships BUDGET-SPENT (correction_count=3).

## 2026-07-22 — scenario (2) correction rally DONE

`mc/e2e/correction_rally_test.go` (`TestCorrectionRallyDockerBoundary`, green
8.7s, first try). Pattern reused from family (1): overwrite worker.json +
verifier.json after setup(). Key mechanics (recon-confirmed): CORRECT needs
--correction, forbids --sha, does worked→seeded + cc++; BUDGET-SPENT needs
--sha, forbids --correction, requires cc==3, does worked→verified
(exception-labeled). Role behavior is static, so verifier.json reads
correction_count via `mc task get` (parsed with tr+sed) and branches. Worker
is re-work-safe (create worktree once, append a commit each pass). Self-proving
end-state: packaged at cc==3 ⇒ budget-spent shipped it (4th CORRECT rejected).
Full docker_e2e was green at 12 tests before this; now 13.

NEXT: scenario family (3) backpressure — queue filled to 3 → no new pipeline
dispatch; Refiner works the best non-saturated packet; a re-entered task
advances at cap; three failed refinements → saturated → idle. Recon in flight.

## 2026-07-22 — scenario (3) backpressure DONE (scoped)

`mc/e2e/backpressure_test.go` (`TestBackpressureDockerBoundary`, green 11.6s).
Proves at cap: WIP-cap blocks a 4th task's Editor; auto-Refiner spawns +
re-enters at cap; re-entered task advances through Worker at cap;
cancel-drains-queue frees the 4th to packaged. Four custom behaviors
(multi-verdict editor, re-work-safe worker, refine_notes-aware always-pass
verifier with --deepening genuine on refinement rounds, re-enter refiner);
new addResidentRoleBehavior/patchResidentConfig helpers add the "refiner"
roleBehaviors key (setup writes none).

Two hard-won findings:
- The saturation arithmetic is EXPENSIVE to drive E2E: churning a packet needs
  budget-spent (--deepening churn), which requires correction_count==3, so the
  first churn needs a full 3-correct ramp. Deliberately NOT driven through
  containers — the dispatch decision table (dispatch_test.go:930-990), the
  packets_saturate substrate triggers, and lifecycle_nightly own it.
- CHURN IS A ROBUSTNESS HAZARD: an infinite genuine-refinement loop across 3
  tasks issued docker exec/run calls fast enough that the helper went
  unresponsive (~54s: "Failed to connect", "hello_ack no progress") and the
  post-drain editor never ran → 2-min timeout. Fix: observe promptly, drain
  early, keep the churn window short (dropped a 12s sustained-check loop).
  Relevant to scenario 5 (the fast-fail/reap and helper-liveness paths).

NEXT: full docker_e2e regression (14 tests) to confirm no churn flakiness under
the full suite, then scenario family (4) initiative lifecycle — charter → wave
children → strict drain → arc packet → land, with block-propagation and
cancel-cascade variants.

## 2026-07-22 — scenario (4) initiative landing is BLOCKED by parked mechanics (operator scope)

Recon + code confirmation (mc/verbs/complete.go, mc/dispatch/dispatch.go,
mc/domain/initiative.go, grep across land verbs): the initiative lifecycle's
LANDING half is not production-built.
- No code sets an initiative's `tasks.branch`; the done-declaration explicitly
  FORBIDS `--branch` (complete.go:117-118). Children share "the parent's one
  branch" (complete.go:142-155) but nothing assigns that shared branch.
- The shared-worktree / child mount representation is parked
  (mountattest.go:238-249: "initiative children have no authorized mount
  representation yet ... parked"). Children run only under the fake harness's
  legacy whole-workspace bind.
- No initiative landing support or test exists (empty grep for initiative in
  land verbs / boundarydocker / e2e). The property walk
  (lifecycle_nightly Track C) drains children by CANCEL and CANCELS the arc
  packet ("retire arc") — it never lands, precisely because landing is unwired.

So family (4) as specified in the handoff ("charter → ... → arc packet →
approve/LAND, block-propagation, cancel-cascade") cannot reach LAND with
current code. The STATE MACHINE (wave birth, plan-review gate, strict drain,
done-declaration, arc packet, block-propagation, cancel-cascade) IS fully
built and exhaustively unit/property-tested (dispatch_test.go initiative
cases, initiative_test.go, task_test.go TestCancel, lifecycle_nightly Track C).

What is BUILDABLE as an E2E without un-parking: charter → wave → plan-review
pass → children dispatch/run (fake) → strict-drain (via cancel) →
done-declaration → verified → packaged → arc PACKET, plus block-propagation and
cancel-cascade — but the arc is CANCELLED, not landed (mirroring the property
walk). Landing needs the parked shared-worktree/branch representation.

DECISION FOR THE OPERATOR (parked in PROGRESS): (a) un-park the initiative
shared-worktree/branch representation so family (4) can land through real
containers (significant, and it is a Phase-3-deferred design item); (b) accept
a scoped family-4 E2E that proves the initiative control loop through the arc
PACKET (no land); or (c) defer family (4) and proceed to families (5) fault
matrix and (6) homie loop, which do not depend on the parked mechanics.

NEXT: awaiting operator scope decision on family (4). Families (5) and (6) are
unblocked if the operator prefers to proceed there first.

## 2026-07-22 — scenario (4) initiative lifecycle DONE via ADR-023 (real merge)

Un-parked and landed. The operator chose option 1 (build the initiative
landing). ADR-023 authored (docs/adr/023) extending ADR-017 D6's parked
initiative/child path; three decisions: D1 shared branch grammar
mc/initiative-<id> cut at promotion; D3 (load-bearing) children stay BRANCHLESS
so approving a child archives it synchronously with no merge — only the arc row
carries the branch and lands, preserving Inv. 25; D4 arc lands via the existing
legacy branch lane (no new lane/envelope). Code deltas (all green):
- domain.Promote sets tasks.branch = mc/initiative-<id> on initiative rows
  (task.go); the §7 landing fence fires on approve, so it is inert until arc
  approval.
- complete.go Worker terminal validates a child's --branch against the shared
  branch but no longer stores it (D3); cli_test updated to worked/null.
- runner/image/mc-land: branch-namespace allowlist extended from mc/task-<id>
  to also accept mc/initiative-<id> (the comment there had flagged this as the
  parked line); new mc-land.test.ts acceptance case. Rebuild picks it up.
- mc/e2e/initiative_lifecycle_test.go (TestInitiativeLifecycleDockerBoundary,
  green 10.3s): full loop — initiative add → Editor promote (branch cut) →
  Strategist(initiative) marker-based wave/done → Editor plan-review pass →
  two children commit to the ONE shared worktree and archive on approval
  (branchless, main never moves) → strict drain → done-declaration → arc
  verify → arc packet → approve → REAL --no-ff merge of the shared branch onto
  main, both children's files landed, shared branch/worktree deleted. Asserts
  main stays put through every child approval and moves only on the arc.

New behaviors: strategist(initiative) [marker in .mc-worktrees decides
wave-vs-done], editor(plan-review) [pass], child worker [reads initiative_id
via mc task get, shares one worktree, completes --branch mc/initiative-<init>],
verifier [scope-aware: shared-branch tip for child and arc].

Deliberately deferred (Phase 5, still parked per ADR-023 D6): the PRODUCTION
real-harness per-child shared-worktree Git-control mount rows (ADR-017 D6
extension); the block-propagation and cancel-cascade E2E variants (state
machine fully unit/property-tested; the happy-path merge was the un-parking
blocker). The wave-boundary "merge main into the branch" drift step (§6.1).

NEXT: full docker_e2e regression (15 tests) to confirm no regression, then
Phase 4 families (5) fault matrix and (6) homie loop. Optionally the (4)
block-propagation + cancel-cascade E2E variants now that landing works.

## 2026-07-22 — scenario (5) fault matrix DONE (scoped; three gaps flagged)

`mc/e2e/fault_matrix_test.go` — four green tests:
- TestFaultReapRetry (10.7s): a Worker that dies before session-start (MALFORMED
  behavior → harness exits before emitting session-start → never heartbeats) is
  reaped at spawn_grace (5s, spawn-watchdog class), charges dispatch_retries,
  frees the lease; task re-selects (Inv. 10) and completes when a valid behavior
  is swapped in; reaped run's session folder survives (Inv. 26).
- TestFaultBudgetExhaustion (18.2s): a permanently broken Worker drains the
  retry budget to 0 → task BLOCKED with a stable reason (§10, no silent loop).
- TestRebootDrill (7.4s): resident killed mid-pipeline + restarted → resumes
  from spine alone to packaged, one packet, session folders survive.
- TestInterrupt (4s): hang Worker holds the lease → `mc task interrupt` cancels+
  archives + frees lease (spine effect).

KILL-CLASS LEVER (recon-confirmed): the fake harness ALWAYS emits session-start
(→ heartbeat) before any step, so a `crash`-first behavior lands on the slow
15-min lease-timeout path. A MALFORMED behavior exits before session-start →
never-heartbeated → fast 5s spawn-watchdog. That is the clean fast-fail lever.

THREE GENUINE GAPS FLAGGED (not asserted; would each need new code, out of the
fake-harness E2E's scope):
1. Fast-fail LIVENESS reap: reaping is time-threshold-only; there is no
   "container confirmed absent → reap now" path. A never-heartbeated fast-fail
   reaps at spawn_grace (5s, fine); a HEARTBEATED fast-fail waits the full
   lease-timeout (15 min E2E / 75 min prod). The family-3 finding. A prompt
   liveness reap needs a resident spawn-liveness probe + a spine reap trigger.
2. Interrupt CONTAINER-STOP: the resident tick loop never applies an interrupt
   effect (dispatch emits none); the container-stop is owed to the orphan sweep
   (IMPLEMENTATION-NOTES 2026-07-20). The spine cancel+lease-free is real.
3. Wake-from-sleep IMMEDIATE tick: unimplemented, deferred [P2/P3]
   (phase1b-contract.md:249). The loop is a plain setInterval.
Also: tick-loop "one dispatch at a time" is unit-covered (tick-loop.test.ts) +
enforced by the single CAS lease (Inv. 1/3); not re-asserted at E2E.

NEXT: full docker_e2e regression (19 tests), then scenario family (6) homie
loop — send → tick wake → reply → outbox/ack; resume; console schedule; plus
one Playwright dashboard smoke.

## 2026-07-22 — scenario (6) homie loop: console DONE; conversational runtime + dashboard UNIMPLEMENTED (operator scope)

Recon (agent, verified against code) found family (6) splits sharply:

BUILT + now tested:
- Console schedule: `mc/e2e/console_schedule_test.go`
  (TestConsoleScheduleDockerBoundary, green 8s). dispatch consoleDue (step 0b)
  + ConsolePublish + outbox poll/ack all exist; only a strategist(console)
  fixture behavior was missing (no product code).
- The homie RECORD layer (homie_sessions/conversation_messages/outbox schema +
  verbs start/bind/send/claim/reply/resume/history/outbox) is fully built and
  CLI-tested (cli_test.go TestHomieWorksourcePauseArchive etc.). Coverable as a
  verb-level record-loop test (start→send→echo→claim→reply→reply-outbox→ack→
  resume→history) — NOT a container E2E (no runtime needed). Not yet written.

UNIMPLEMENTED — the conversational RUNTIME (would need substantial new product
code, a whole workstream, NOT un-parking existing design):
- Homie WAKE selector in dispatch: the tick never converts a pending inbound
  turn into a spawn. Explicit code TODOs: dispatchseam.go:874 "Homie candidates
  arrive with the future wake selector"; homiepreflight.go:26 "Nothing selects
  Homie candidates yet (the D1/D5 planner slice)".
- Homie SPAWN in the resident: types.ts Effect union has no homie action;
  effects.ts spawn() hardcodes tier:"pipeline"; no homie roleBehavior.
- Homie RUNNER: does not exist in runner/ (only agent-runner + fake-harness).
- Resume RELAUNCH: record-only; the resident relaunch authority is the same
  missing spawn path.
- Outbox DELIVERY loop: verbs exist, no per-surface actor runs poll/ack.
- DASHBOARD: does not exist at all (no web app anywhere); AGENTS.md §3 lists it
  as a remaining authored deliverable. The "one Playwright dashboard smoke" is
  therefore impossible until the dashboard is built.

So the family-(6) core "send → tick wakes container → reply → outbox → deliver/
ack" and the dashboard Playwright smoke are BLOCKED on building the homie
conversational runtime + the dashboard — a large workstream. This is an
operator scope decision (bigger than the family-4 landing un-park). Parked in
PROGRESS.

NEXT: operator decision on the homie runtime + dashboard. Buildable interim
without that decision: the homie record-loop verb test. Phase 4 otherwise
COMPLETE — families (1)-(5) green + (6) console; full docker_e2e 20 tests.

## Homie runtime S1 — lease-free wake selector wired into the dispatch tick

Operator chose BUILD for the family-(6) homie runtime + dashboard. S1 landed
green (commit 82381d9):

- schema v13 adds lock.homie_idle_timeout_s (default 1800); the resident
  SPINE_SCHEMA_VERSION mirror bumped in lockstep.
- three ADR-016 D3 fence receipts (mc/verbs/homie.go): HomieLaunchBind (CAS the
  64-hex docker id onto the current launch, idempotent, fenced, non-idle
  guard), HomieRunnerStarted (stamp launch_started_at), HomieExit (Case A end
  once a locator exists / Case B pre-runner null-locator clear retaining launch
  debt). CLI: mc homie launch-bind|runner-started|exit.
- selectHomieWake (homieselect.go) — the spine-only core: branch 6 idle-end
  wins over branch 7 (oldest (last_activity_at,id) non-idle no-launch session
  with pending inbound or resume debt; fresh unless resume-only).
- loadHomieSchedRows + homieWakeRound wired into dispatchseam.go's prepare,
  gated on sel.action.Kind == KindIdle (the pipeline committed nothing this
  tick — Homie is the lease-free tier, Inv. 1/22). Spawn wake mints/persists a
  16-hex launch generation + mode, clears resume debt, carries a rows-mode
  prime cutoff into the current prime pair, emits homie-wake{session,launch,
  mode,binding,container_name}. Idle end calls homieEndTx (status->ended; the
  homie_sessions_deactivate_bindings trigger clears bindings), emits
  homie-stop. Both write a dispatch receipt for idempotent replay.
- homie_idle_timeout_s plumbed through loadLock/tunables.

DEFERRED (need the resident's live container inventory, so they are resident-
outage recovery, not the happy path): branch-5 transitional/terminal container
reconciliation and branch-7 unstarted-launch-debt / adopt-a-created-container.

NEXT: S2 — the resident spawn effect consuming homie-wake/homie-stop (tier
"homie" run.json, no lease/heartbeat, operator-scope RO mounts, launch-bind ->
start -> runner-started). Then S3 runner, S4 E2E, S5 delivery+resume, S6
dashboard (ADR) + Playwright.

## Homie runtime S2 + S3 — resident effectors and the conversation runner

S2 (commits 19aadf6, 23b078b): the resident learned two effect arms the
dispatch tick emits. homie-wake materializes a trace-only folder + a heartbeat-
free tier:"homie" run.json (run_id == session), creates a --rm mc-tier=homie
container over a FIXED operator-read-scope mount set (workspace bound RO, Inv.
22), CASes the exact 64-hex docker id onto the launch via `mc homie launch-bind`
BEFORE start (a fenced bind / failed create / non-canonical id abandons the
container), republishes run.json carrying container_id so the in-container
runner can report its own id, then starts. homie-stop stops the bound container
by id; a pre-bind idle end stops nothing. No lease, no heartbeat, concurrent.
Credential projection deferred (fake route is token-free). ResidentConfig gained
homieCmd (falls back to agentCmd).

S3 (commit 6bf3bd5): runner/homie-runner/main.ts — the lease-free loop. Boot:
`mc homie runner-started` once with launch + container id; a fenced report
exits 0 (superseded launch). Then claim (oldest pending inbound, durable) ->
run one fake-harness turn feeding the operator's body on stdin, capture the
turn-complete output -> `mc homie reply --to`. A harness with no turn-complete
leaves the turn claimed and exits 1; a quiet conversation idles out after
maxIdlePolls empty claims. Seams (mcJSON/runHarnessTurn/sleep) injected so the
fast suite drives the loop without subprocesses. 9 unit tests green.

NEXT: S4 Docker E2E send->wake->reply (add homie-runner to the e2e image, wire
resident homieCmd + a homie behavior fixture, tick-driven reply assertion).
S5 delivery + native/rows resume (needs a homie register-session verb). S6
dashboard (ADR) + Playwright.

## Homie runtime S4 — Docker E2E green + the branch-7 preemption fix

The send->wake->reply loop is proven end to end
(TestHomieConversationDockerBoundary, ~6s): `mc homie start` + `mc homie send`,
a tick wakes and binds a tier:"homie" container, the in-container homie-runner
reports runner-started, claims the pending turn, runs the fake homie behavior,
and posts "homie ack" — asserted via `mc homie history` with the pipeline lease
free throughout.

The fix that made it work: the S1 wiring gated the wake selector on KindIdle
only, but a fresh/liveness spine retains a strategist-propose spawn candidate
(KindSpawn) almost every tick, so the Homie never got a turn (the first e2e run
timed out with only idle/connect ticks). ADR-016 D3 branch 7 is explicit — an
eligible Homie wake wins over a merely-RETAINED pipeline spawn candidate
(re-decided next tick, "at most one unnecessary pipeline turn") and over the
idle result, but never a lock-domain reap/reenter/landing (branches 1-3). The
gate now runs homieWakeRound when the pipeline action is KindIdle OR KindSpawn
and returns the Homie final when eligible; the discarded candidate was only
prepared (no lease, no runid persisted), so preemption is clean. A dispatch-
level integration test (TestDispatchWakesHomieOverPipelineCandidate) locks it
in. Walking-skeleton pipeline e2e regression-checked green.

E2E wiring: e2e resident config gained homieCmd + a homie.json behavior; the
homie-runner source mounts RO like the agent-runner (no image rebuild).

NEXT: S5 outbox delivery loop + native/rows resume (needs a new homie
register-session verb for native_session_ref/trace_filename). S6 dashboard
(ADR) + Playwright.

## Homie runtime S5 — native locator registration + functional resume

S5a (76d4529): the homie-runner registers its native session locator on the
first observed session-start. runHarnessTurn now returns a TurnResult carrying
the harness's session-start id alongside the reply; the loop registers it once
via `mc run register-session <session> --native-ref <id> --file native.jsonl`,
which already delegates to registerHomieSessionLocators for the homie tier (the
"new verb" the handoff imagined already existed). Idempotent, non-fatal.

S5b (9a8304e): resume was non-functional for two coupled reasons, both fixed:
- homieEndTx ends a session without clearing current_launch_id, so a resumed
  session looked already-launched to the wake selector (ADR-016 D3 branch 7
  requires no launch) and never re-woke. HomieResume now clears the entire dead
  launch generation in its reactivation UPDATE (the container is gone). A new
  inbound turn wakes it fresh; true native cross-turn continuity (feeding the
  recorded native ref to a real harness) is deferred as a real-harness concern.
- The container name is session-fixed (mc-homie-<session>), so the resume wake
  raced the first generation's --rm container ("Conflict. The container name is
  already in use"). Since the launch fence guarantees at most one live
  generation, the wake effector force-removes any stale same-named container
  before create.

Proven by TestHomieResumeDockerBoundary (answer -> register native locator ->
end -> resume -> new turn answered on a fresh wake, ~5s); TestHomieConversation
still green. Delivery: OutboxPoll/OutboxAck + HomieReply's homie_reply fan
already exist, so the delivery loop is present at the verb level; a real push
surface (discord) is separate, and the dashboard is pull-based (history/outbox).

NEXT: S6 dashboard — ADR (TS on Bun, loopback HTTP, reads spine ONLY via mc
verbs, honoring Inv. 24) + web app (conversation view + send box) + one
Playwright smoke. Then return to remaining authored deliverables.

## 2026-07-22 — S6 dashboard Console (ADR-024) + a pre-existing fast-suite red

Fast-suite red found on session start: TestHomieClaimReply/reply_appends_…
expected a pipeline `spawn` while an unserved homie turn was pending; red
since 38d0c36 (S4's deliberate branch-7 preemption), masked afterward by Go
test caching — PROGRESS's "five-leg fast green at 191719d" was stale. Fixed
(75b2db5) by absorbing the homie-wake before the spawn expectation, matching
production ordering (the runner claims from inside the woken launch).

S6a (30af818): ADR-024 pins the delegated dashboard choices — zero-framework
Bun.serve package `dashboard/`, spine reached ONLY by spawning the real `mc`
through one injected seam (Inv. 15/24; no SQLite import may appear), thin
one-to-one verb-mirror API (list/history/start/send/resume + outbox drain),
fail-closed bind/auth (loopback 127.0.0.1:7333 default; non-loopback refused
without a token; a configured token gates every request pre-spawn, §15.7),
per-session DERIVED dashboard channel ref (a (surface,channel_ref) place
binds one session, so a shared "dashboard:console" ref cannot work; reuse the
session's dashboard binding from `homie list` or mint dash-<8hex>, stateless
per Inv. 23), pull-based Console polling (tight 1s for 30s after send, lazy
10s) with trivial outbox ack. Console tab only; the other four §13 tabs are
inert placeholders. 25 bun tests; check.sh joined as the fast suite's sixth
leg.

S6b (75bd35f): the one Playwright smoke (ADR-024 D7), Docker-free —
smoke.sh builds the test_fake_routing mc, provisions a scratch spine, serves
the real dashboard, drives real Chromium: new session → send ping → a
fabricated tier:"homie" run.json claims and replies (the sanctioned fast-lane
runner stand-in) → the reply renders inside the tight polling window → the
dashboard outbox drains to zero. Playwright 1.54.0 pinned by the committed
bun.lock (§16.1); browsers via `bunx playwright install chromium`. Gotcha
pinned in mc.ts: Bun.spawn does not see process.env mutations made after
module load unless env is passed explicitly per spawn.

Deferred from S6 (not blockers): LaunchAgent unit generation (install/onboard,
§16.1); the four remaining tabs with their subsystems; SSE/WebSocket push
behind the same API if polling ever chafes.

NEXT: adversarial review of the S6 diff, then the remaining authored
deliverables (frozen role directives + brief templates, install.sh + /onboard).

## 2026-07-22 — S6c: adversarial review of the S6 diff, six confirmed findings fixed

A 13-agent review workflow (4 dimensions × find, then one skeptic per
finding prompted to refute) over 9d2cdb4..75bd35f confirmed 6 of 9 raw
findings; all fixed in one micro-step (see the S6c commit for the list):
three majors — no Origin/Host browser boundary on the open loopback API
(CSRF + DNS rebinding could start sessions/inject messages), the §15.4
resume affordance unreachable from the UI (route built + tested but never
called), and a smoke drain assertion racing the browser's own drain — plus
three minors (token mode unusable from a browser since auth gated the
static shell; mc usage envelopes surfacing as 500; stale history rendering
under a switched session). Rejected as unreal: drain-only-when-selected,
localhost-name trust, bearer timing-compare. The smoke now also proves
end → resume → send-again through the browser. ADR-024 D3/D4 amended to
record the shipped behavior.

NEXT: remaining authored deliverables — frozen role directives + brief
templates (spec §9.2, Inv. 20), then install.sh + /onboard (spec §17).

## 2026-07-22 — authored deliverable: frozen role directives finalized (§9.2, Inv. 20)

Recon showed the plumbing (brief.go carrier, directives.go go:embed, delivery
resident→run.json→runner stdin) fully built; the gap was CONTENT. An inline
audit of the nine directive files against spec §9.2 found none named the
four-pattern menu, none carried the escape valve, no claude-sdk directive
used the exact trigger term "dynamic workflow", no codex directive
instructed named-pattern/bounded-rounds, and the Verifier lacked the
criterion-driven N/A gate rule (spec §7). All fixed, additively — the four
test-guarded phrases ("Orchestrate by default.", "read-only", "depth-1",
"exactly one") are untouched. New TestDirectivesEncodeTheSelfOrchestrationContract
pins the contract with whitespace-collapsed matching (directives are wrapped
prose); the coverage test gained the missing editor(plan-review) row.
Homie deliberately has no frozen directive (outside the leased pipeline;
its per-session freeze is the verb allowlist, spec:664). The brief-template
half of the deliverable was already complete (mc.spawn-brief.v1 in brief.go,
per-role payloads asserted in dispatchverb_test.go).

NEXT: verify ADR-001 satisfies the §18 role-side verbs ADR deliverable
(likely yes — record the conclusion), then install.sh + /onboard (§17).
