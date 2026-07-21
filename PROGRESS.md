# PROGRESS — Mission Control implementation state

<!-- Live cross-session state only. Narrative history is in docs/ledger/. -->

REPO PATH: `~/dev/ai/homie`. Never relocate this repo into `~/Documents`,
`~/Desktop`, or `~/Downloads`: macOS TCC can revoke an agent session's own
filesystem access there during fan-out. Full Disk Access does not fix it.

LAST GREEN SHA: `96c5364` — five-leg fast lane green. Docker suite last 8/8 at
`4a69d15`. The operator pushes manually; agents do not push.

PHASES PASSING: Phase 0 COMPLETE; Phase 1 COMPLETE; Phase 2 COMPLETE. Phase 3
is in progress. Completed implementation history is in
`docs/ledger/chronology-phase-0-2.md` and `docs/ledger/phase-3.md`; do not read
either at startup.

FAST SUITE:
`./mc/check.sh && ./runner/fake-harness/check.sh && ./runner/agent-runner/check.sh && ./runner/image/check.sh && ./resident/check.sh`

Phase-completion Docker regression:
`cd mc && mise exec -- go test -tags docker_e2e -timeout 15m ./e2e/...`

Schema is v11. `mc onboard home` migrates v1 through v11 in place. v11 widens
the approve landing fence so an immutable task assignment, not only
`tasks.branch`, arms it.

## Known intermittent failures

1. `TestOnboardConcurrentFreshHomeNeverDeletesTheWinner` (`mc/verbs`), roughly
   1 in 21 full-suite runs. Repro:
   `cd mc && for i in $(seq 1 25); do mise exec -- go test ./verbs/ -count=1 || break; done`
   A concurrent provisioner creates a non-empty SQLite file before its schema
   transaction commits; `onboard.go` temporarily mistakes that state for
   corruption. It should await/retry and refuse only if the file stays
   table-less. Fail-closed, pre-existing, not a Phase 3 blocker. Diagnosis:
   `IMPLEMENTATION-NOTES.md` (2026-07-15). Seen again at `4757df2`, followed by
   8/8 isolated greens and a green full-suite rerun.

2. `resident one-use dispatch control > rejects every identity mismatch before
   accepting child output` (`resident/src/resident-control.test.ts`),
   load-sensitive. Repro while another suite runs:
   `for i in $(seq 1 8); do ./resident/check.sh || break; done`
   The test-only child exits immediately after its hello, allowing subprocess
   reaping to race the fd-3 socket poller and surface `EBADF`. Production waits
   for the ack. Fail-closed and not a Phase 3 blocker. Capture the exact failing
   test name on the next sighting.

## Phase state

- [x] Phase 0 — architecture-kill spikes S1–S8 green; no fallback ADRs.
- [x] Phase 1 — substrate and walking skeleton.
- [x] Phase 2 — dispatch, domain correctness, §18 verbs, split-brain
      convergence, initiative-wave review, and randomized properties.
- [ ] Phase 3 — boundary conformance.
  - [x] ADR-016 through ADR-021 boundary design and adversarial review.
  - [x] Mount policy, jurisdiction, identity/ACL containment, refusal
        taxonomy, prepare/attest/commit crossing, authorization carrier, and
        lock-domain guard.
  - [x] First-task setup, recovery, completion seal, accepted-seal rebuild,
        disposable Verifier projection, and production Worker/Verifier Docker
        crossings.
  - [x] Production sealed pipeline reaches `verified` and `packaged`.
  - [x] Sealed landing steps 1–4: assignment lane, mount grammar and host
        anchors, closed envelope arm, fenced lander, closure import, CAS ref,
        and merge. The lane remains inert end to end.
  - [x] Adversarial Git corpus gap analysis complete; rename inference pinned.
  - [x] Operator-approved scoped self-abort, implemented and reviewed.
  - [x] Landing id and attest-side carrier producer; both inert.
  - [x] Resident sealed-landing arm and container envelope; inert.
  - [ ] Route landing through the dispatch seam, then turn it on atomically.
  - [ ] Run and record the complete Phase 3 real-mechanism/Docker acceptance
        lane before advancing.
- [ ] Phase 4 — six E2E control-loop scenario families.
- [ ] Phase 5 — operator-scheduled real-subscription acceptance.
- [ ] Release prep — install/onboard front door and construction-document
      disposition.

## Parked

- **S7 sleep drill**: the 30-minute Mac sleep mid-lease test requires the
  operator. Instructions: `spikes/07-launchd-clock/RESULT.md`. All other S7
  sub-tests passed.

## Current work: sealed landing

The corpus gap and the scoped self-abort are closed (`96b86a2` through
`2d2cffb`, the last of which hardens the abort gate after adversarial review).
The lane remains inert end to end; keep it that way until the coordinated
activation step, because a partially active lane can convert today's loud
`Approve` refusal into a durable blocked row.

The self-abort gate is ACTION identity and it has three parts: `MERGE_HEAD` is
the reviewed SHA, `MERGE_MSG` is a message this landing WROTE (our subject, and
the trailer as a line at column 0), and the target is still at the frozen
preimage. Do not loosen any of the three. Stage (7) publishes the reviewed
commit as `refs/heads/mc/task-<id>`, so an operator can produce that same
`MERGE_HEAD`; and `git merge --log` can splice an agent-authored subject into
their MERGE_MSG, so a substring match on the trailer is forgeable. Details:
`IMPLEMENTATION-NOTES.md` (2026-07-20 finding, and the review disposition).

### 1. Activate the sealed lane

Build the producers FIRST — they stay inert on their own, because nothing
reaches them until the selector flips. Only the last step must be atomic.

1. [x] The landing id (`landingid.go`) and the attest-side carrier producer
   `captureLandingPlan` (`landingcapture.go`). Both green and inert.
2. [x] Resident `runSealedLanding` and the `MountPlan.landing` mirror, with the
   ADR-019 landing-class envelope pinned by test. Green and inert; it has no
   effect arm, so step 3 changes routing alone. The carrier grew
   `ApprovedRunID` for ADR-016:846's label.
3. [ ] Route the landing THROUGH prepare/attest/commit, then flip. PLANNED as
   16 micro-steps; the plan is `docs/ledger/phase-3.md` (2026-07-20, "step 3
   planned"), which is the working document for this slice — read it before
   touching the seam. The shape: a landing is a SEPARATE LANE, a third variant
   of `preparedDispatch` (`landing *preparedLanding`, sibling of `final` and
   `candidate`), never a variant of `preparedCandidate`. That keeps the ~35
   unguarded `cand.spawn` derefs unreachable BY TYPE, and leaves
   `preparedCandidate`, `dispatchAttest`, `dispatchCommit`, `applySpawn` and
   `loadDispatchMountState` byte-identical.

   Steps 1-15 each leave the lane inert and the fast suite green. Step 16 is
   the atomic switch and is exactly two edits: `nextLanding`'s filter gains
   `|| t.SealedLandingPending()`, and `Approve` stops refusing an assigned
   sealed row. Neither works alone.

   HARD ORDERING: do not run step 16 until step 15 is green. Step 15 closes
   ADR-016:373-375's confirmed-absence gate resident-side; without it a crashed
   attempt's `mc-landing-<id>` container makes the next attempt collide on the
   name and report `land report failure`, converting infrastructure trouble
   into a durable blocked row (ADR-016:576 forbids exactly this).

Invert, do not delete: `TestApprove/assigned sealed task refuses rather than
archiving` (`task_test.go:823`) and `TestStep0c_ApprovedBranchlessRow_NeverLands`
(`dispatch_test.go:412`) — the latter stays green on its fixture, so it is its
INTENT that goes stale. `LandReport` against an assignment-backed row is
untested in either direction. MUST NOT RELAX:
`TestNoLandingCellIsPlanAddressable` and `TestPlanMountsRefusesEveryLandingCell`
(`landingplan_test.go:77,103`) — an earlier draft widened them and review
rejected it.

The branch comes from the immutable assignment and is never projected into
`tasks.branch`; `complete.go:163` is that column's only writer and is closed to
assigned tasks, which is what makes the lanes partition. `/repo` is not
plan-addressable. The nested `.mission-control` cover must prevent the RW source
alias from exposing the sealed task root as writable.

### 2. Phase 3 completion lane

Prove the realized landing mount table, RO alias/cover behavior,
network-none/uid/capability envelope, VirtioFS import durability, and the five
ADR-017:758–760 crash cuts. Carry the production E2E through
`packaged → approve → merge → archived`.

Then satisfy `docs/phase3-contract.md` §8: every §3 row has a named green
real-mechanism test; production image has no fake route; no production override
redirects spine/identity; `doctor` has no Phase-3-owned deferral; fast, tagged,
Phase 3 Docker, and Docker E2E lanes are green. Record image digest,
architecture, capability probes, commands, and green evidence here before
advancing to Phase 4.

## Known later obligations

- Production spine-volume ownership is unspecified; the E2E fixture currently
  uses permissive volume-root setup. This belongs to install/onboarding.
- The setup clearing mechanism is delegated by the ADRs but lacks its own ADR.
- Landing currently cannot validate `pinned_closure_digest` because the
  assignment digest describes the initial closure, not the accepted rebuilt
  seal. Retry after a successful merge deliberately refuses rather than
  adopting. Details: `IMPLEMENTATION-NOTES.md` (2026-07-20).
- Landing failure taxonomy/backoff, serialized expected Git topology, and
  initiative-child sealed landing remain explicitly unresolved. Keep the
  canonical landing row derived; use the assignment's frozen `target_ref` and
  refuse divergence. Details are in the Phase 3 ledger.

NEXT: Sealed-lane activation step 3 is PLANNED — execute micro-steps 1-15 in order, TDD, committing each green; the lane stays inert throughout. The plan is `docs/ledger/phase-3.md` (2026-07-20, "step 3 planned"); read it first, it names every test, insertion point, and corpus basis. Do NOT run step 16 (the atomic switch) until step 15 is green.
