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
