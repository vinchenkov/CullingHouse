# PROGRESS — Mission Control implementation state

<!-- Live cross-session state only. Narrative history is in docs/ledger/. -->

REPO PATH: `~/dev/ai/homie`. Never relocate this repo into `~/Documents`,
`~/Desktop`, or `~/Downloads`: macOS TCC can revoke an agent session's own
filesystem access there during fan-out. Full Disk Access does not fix it.

LAST GREEN SHA: `75bd35f` — six-leg fast suite green (incl. the new
dashboard leg) + the Playwright dashboard smoke. Docker lanes last ran green
at `c8f37e9`-era HEAD (full `docker_boundary` 26 subtests, full `docker_e2e`
10 tests incl. both credential legs) and are untouched since: commits after
are test-only or the new `dashboard/` package. Production image `mc-prod` at
`5d7f539`:
`sha256:47b27eda69019d1e97c9618466ed391470447ebae6025270abb1931914c487a6`,
arm64/linux, native. LESSON pinned by `ada715d`: the resident's
`SPINE_SCHEMA_VERSION` (resident-control.ts:12) mirrors
`substrate.CurrentSchemaVersion` in lockstep — every schema bump must touch
BOTH, and only the Docker handshake lane catches the miss. LESSON (75b2db5):
Go test caching can mask a red left by a semantics change — a "suite green"
claim needs `-count=1` or a cold cache on the affected package.

The operator pushes manually; agents do not push.

PHASES PASSING: Phase 0–3 COMPLETE. Phase 4 (six E2E control-loop scenario
families) is in progress. Completed implementation history is in
`docs/ledger/chronology-phase-0-2.md` and `docs/ledger/phase-3.md`; the live
phase ledger is `docs/ledger/phase-4.md`. Do not read any of them at startup.

FAST SUITE:
`./mc/check.sh && ./runner/fake-harness/check.sh && ./runner/agent-runner/check.sh && ./runner/image/check.sh && ./resident/check.sh && ./dashboard/check.sh`

Phase-completion Docker regression:
`cd mc && mise exec -- go test -tags docker_e2e -timeout 15m ./e2e/...`
Phase-completion dashboard browser smoke: `./dashboard/smoke.sh` (no Docker).

Schema is v12. `mc onboard home` migrates v1 through v12 in place. v11 widened
the approve landing fence to assignment-armed tasks; v12 retires
`egress_policy`/`network_allow` and narrows `runtime_auth_delivery` to
`projection|materialized` (ADR-022) via the chain's first rebuild-and-copy
(NULL-stash of the worksource references, not deferred FKs — see ledger).

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
- [x] Phase 3 — boundary conformance. COMPLETE 2026-07-22: all seven
      phase3-contract §8 "advances only when" bullets verified green (ledger
      2026-07-22 "§8 sweep" + this readiness check). Operator signed off on
      advancing 2026-07-22.
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
  - [x] Landing routed through the dispatch seam and turned on atomically.
  - [x] ADR-022 free-internet credential projection: schema v12, forbidden-env
        builder + pre-claim refusal, resident token service/writers/broker,
        spawn-seam projector, gateway deny-root repurpose, doctor gateway
        retirement, doctor container-runtime capability probe, and the
        credential-projection Docker acceptance (synthetic mints). Commits
        `9c45d2b`..`c8f37e9`.
  - [x] Ran and recorded the complete Phase 3 real-mechanism/Docker acceptance
        lane: full docker_boundary (26 subtests) + full docker_e2e (10 tests,
        incl. the two credential legs and the packaged→approve→merge→archived
        walk) + five-leg fast + all tag vets, all green at HEAD. §8 mechanical
        checklist satisfied (see ledger 2026-07-22 "§8 sweep").
- [ ] Phase 4 — six fake-harness E2E control-loop scenario families (real
      containers/spine/resident, timer-driven). Scope: handoff Part 3
      "Phase 4". Ledger: `docs/ledger/phase-4.md`.
  - [x] (1) Full pipeline + landing — approve/land split (TestWalkingSkeleton),
        landing-failure+recovery, multi-approve-drain. Green.
  - [x] (2) Correction rally — 3 CORRECTs → 4th ships BUDGET-SPENT. Green.
  - [x] (3) Backpressure — WIP cap blocks dispatch, refiner re-entry at cap,
        drain-frees. Green (saturation arithmetic cited to unit tests).
  - [x] (4) Initiative lifecycle — UN-PARKED and landed (operator chose to
        build it). ADR-023: shared branch mc/initiative-<id> cut at promotion,
        children branchless (only the arc lands), mc-land namespace extended.
        `TestInitiativeLifecycleDockerBoundary` green — full charter→wave→
        plan-review→children→drain→done→arc→REAL merge to main. Green. The
        block-propagation + cancel-cascade E2E variants remain optional (state
        machine unit/property-tested); the PRODUCTION real-harness child mount
        rows stay parked for Phase 5 (ADR-023 D6). Ledger 2026-07-22 "(4) ...
        DONE via ADR-023".
  - [x] (5) Fault matrix — reap→retry→complete, budget-exhaustion→blocked,
        reboot drill (resident restart resumes), interrupt (spine effect),
        session-folder permanence. Green (4 tests). Three gaps flagged not
        asserted (need new code, out of fake-harness scope): fast-fail LIVENESS
        reap (time-based only), interrupt CONTAINER-STOP (owed to orphan sweep),
        wake-from-sleep immediate tick (unimplemented [P2/P3]). Ledger
        2026-07-22 "(5) ... DONE (scoped; three gaps flagged)".
  - [x] (6) Homie loop — DONE 2026-07-22 (runtime S1–S5 + dashboard S6).
        Full send→wake→reply→resume loop proven by real Docker E2E; the S6
        Console (ADR-024) proven by the Playwright smoke. Details below and
        in the ledger.
- [ ] Phase 5 — operator-scheduled real-subscription acceptance.
- [ ] Release prep — install/onboard front door and construction-document
      disposition.

## Parked

- **Initiative PRODUCTION real-harness mount rows (Phase 5, ADR-023 D6)**: the
  fake-harness initiative lifecycle lands via ADR-023 (shared branch, branchless
  children, legacy land lane). The PRODUCTION per-child shared-worktree
  Git-control mount rows (the real branch-isolated worktree bound RW, extending
  ADR-017 D6's closed table) remain owed for when real-harness initiatives run
  (Phase 5), now constrained by ADR-023 D1/D3/D5. `mountattest.go:238-249` still
  refuses initiative children under REAL routing (skipped under fake, which is
  all Phase 4 needs).
- **Phase 5 live-provider credentials (operator-dependent, NOT a Phase 4
  blocker)**: the ADR-022 credential mechanism is proven with synthetic mints;
  the live legs need a real Claude/Codex subscription refresh grant in
  `MC_HOME/refresh-grants`. `OPERATOR-INPUTS.md` has only gateway-era
  `CLAUDE_CRED_DIR`/`CODEX_CRED_DIR` pointers. Two fields (`token_url`,
  `client_id`) are not captured in the repo (the POC mocked the provider
  endpoint) — closing the live legs needs those two provider constants
  extracted + an `mc onboard runtime-auth` extractor built. This is Phase 5
  work; Phase 4 is fake-harness and needs none of it.
- **S7 sleep drill**: the 30-minute Mac sleep mid-lease test requires the
  operator. Instructions: `spikes/07-launchd-clock/RESULT.md`. All other S7
  sub-tests passed.

_(The parked §3 gateway/forbidden-env scope decision is RESOLVED — see
"Credential design" below.)_

## Load-bearing invariants from completed Phase 3 (do not regress)

Full narratives are in `docs/ledger/phase-3.md` and `IMPLEMENTATION-NOTES.md`;
these are the constraints a Phase 4+ change must not break.

- **Sealed landing** is a SEPARATE lane — the third `preparedDispatch` variant
  (`landing *preparedLanding`), never a `preparedCandidate`; the spawn seam
  dereferences `cand.spawn` unguarded, so the separation is the only thing
  keeping those nil-safe. A landing takes no lease, opens no Run, writes
  nothing at dispatch; `mc land report` writes. The self-abort gate is ACTION
  identity, three parts (`MERGE_HEAD`=reviewed SHA, `MERGE_MSG`=a message this
  landing WROTE, target at frozen preimage) — do not loosen any. The branch
  comes from the immutable assignment, never `tasks.branch` (`complete.go:163`
  is that column's only writer, closed to assigned tasks — this is what
  partitions the lanes). MUST NOT RELAX: `TestNoLandingCellIsPlanAddressable`,
  `TestPlanMountsRefusesEveryLandingCell` (`landingplan_test.go:77,103`),
  `mc/dispatch/sealed_landing_test.go`, `mc/substrate/landing_fence_test.go`.
- **ADR-022 credentials** (all green, `9c45d2b`..`c8f37e9`): `--network none`
  dropped for the AGENT class ONLY (setup/landing/verifier keep it, logged
  deviation); `resolveGatewaySecretRoots` deny-mounts `MC_HOME/refresh-grants`
  (repurposed, never delete); `gateway_control_version` retained (golden
  bytes); the resident's `SPINE_SCHEMA_VERSION` mirror tracks
  `substrate.CurrentSchemaVersion` in lockstep. Still open (non-blocking):
  binding-catalog `ProviderCredentialKeys`/`DeclaredStaticKey` sourcing +
  operator env-guard config surface (the per-binding provider-key fence is
  inert until threaded; floor + refresh-token fence are live).

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

## Homie runtime (ADR-016 D3) — S1-S6 DONE + green (detail in docs/ledger/phase-4.md)

The full send->wake->reply->resume loop is proven by real Docker E2E
(TestHomieConversationDockerBoundary, TestHomieResumeDockerBoundary); the
walking-skeleton pipeline e2e is still green.
- S1 (82381d9): schema v13 homie_idle_timeout_s; launch-bind/runner-started/exit
  fence verbs; selectHomieWake + loadHomieSchedRows + homieWakeRound.
- S2 (19aadf6, 23b078b): resident homie-wake/homie-stop effectors.
- S3 (6bf3bd5, 978c2bb): runner/homie-runner claim->turn->reply loop, idle-out.
- S4 (38d0c36, e1e3fc8): the Homie wake PREEMPTS a retained pipeline spawn
  candidate (ADR-016 D3 branch 7), not KindIdle-only; Docker E2E green.
- S5 (76d4529, 9a8304e): runner registers its native locator; functional resume
  (HomieResume clears the dead launch; wake effector rm -f's the stale
  same-named container before create). Delivery verbs (OutboxPoll/OutboxAck +
  HomieReply's homie_reply fan) already exist.
- S6 (30af818, 75bd35f): ADR-024 dashboard Console — zero-framework Bun.serve
  `dashboard/` package, spine ONLY via spawned `mc` (Inv. 15/24), verb-mirror
  API, fail-closed loopback/auth bind, derived per-session channel ref,
  pull-based polling with trivial outbox ack. Unit lane = sixth fast-suite
  leg; browser proof = `./dashboard/smoke.sh` (Playwright, Docker-free).

DEFERRED: true native cross-turn continuity (real-harness --resume; fake adapter
starts anew); dispatch branch-5 container reconciliation + branch-7 unstarted-
launch recovery (need resident container inventory); homie credential projection
(fake route is token-free); dashboard LaunchAgent generation (install/onboard);
the four non-Console tabs (each with its subsystem).

NEXT: adversarial review of the S6 diff (9d2cdb4..HEAD) against ADR-024 +
spec §13/§15.4-15.7; then the remaining authored deliverables — frozen role
directives + brief templates (spec §9.2, Inv. 20), install.sh + /onboard
(spec §17).
