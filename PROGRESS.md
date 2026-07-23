# PROGRESS — Mission Control implementation state

<!-- Live cross-session state only. Narrative history is in docs/ledger/. -->

REPO PATH: `~/dev/ai/homie`. Never relocate this repo into `~/Documents`,
`~/Desktop`, or `~/Downloads`: macOS TCC can revoke an agent session's own
filesystem access there during fan-out. Full Disk Access does not fix it.

LAST GREEN SHA: `d6c6384` — Phase 5 production doctor composes host-only facts
with a version-fenced, path-free helper runtime report; Container and Verify
reuse it, helper absence remains a complete exit-0 diagnosis, and the unnamed
wizard refuses until every mixed-authority section is split. The six-leg fast
suite is green.
Full `docker_boundary` + full `docker_e2e` (-count=1, `ok mc/e2e 169s`), the
extended Playwright dashboard smoke, and the install.sh dev walk were last
green at `d0ef4bb`. Docker lanes last ran green
at `c8f37e9`-era HEAD (full `docker_boundary` 26 subtests, full `docker_e2e`
10 tests incl. both credential legs) and are untouched since: commits after
are test-only or the new `dashboard/` package. Production image `mc-prod`
rebuilt from `d6c6384`:
`sha256:ea411c714c35c2df2962aafb4646199d1f0bff6769653af9c50816ab5abb8ad9`,
arm64/linux, native. LESSON pinned by `ada715d`: the resident's
`SPINE_SCHEMA_VERSION` (resident-control.ts:12) mirrors
`substrate.CurrentSchemaVersion` in lockstep — every schema bump must touch
BOTH, and only the Docker handshake lane catches the miss. LESSON (75b2db5):
Go test caching can mask a red left by a semantics change — a "suite green"
claim needs `-count=1` or a cold cache on the affected package.

The operator pushes manually; agents do not push.

PHASES PASSING: Phase 0–4 COMPLETE. Phase 5 is in progress. Completed
implementation history is in `docs/ledger/chronology-phase-0-2.md`,
`docs/ledger/phase-3.md`, and `docs/ledger/phase-4.md`; the live phase ledger is
`docs/ledger/phase-5.md`. Do not read any of them at startup.

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
- [x] Phase 4 — COMPLETE 2026-07-22: all six scenario families and four
      authored deliverables green at `d0ef4bb`; full details are closed in
      `docs/ledger/phase-4.md`. ADR-023 leaves only its production
      real-harness initiative-child mount rows for Phase 5.
- [ ] Phase 5 — IN PROGRESS 2026-07-22: operator kickoff authorized in the
      active goal. Build and mechanically verify the real-subscription,
      onboarding, supervision, and restore paths before the operator-present
      live acceptance.
  - [x] Production install fails closed before writes when Docker is absent or
        stopped, and exits nonzero when the warm helper is missing (`bc0dee4`).
  - [x] Credential-store read ambiguity and duplicate binding owners refuse
        resident startup before any token-free route can launch (`9cec34f`).
  - [x] Canonical MC_HOME aliases derive one domain-separated runtime identity;
        different homes cannot share helper or spine-volume names (`e7c4ca2`).
  - [x] Private `__onboard-spine` has a strict path-free frame and complete
        init/repair/match/mismatch/loss/migrate/newer state matrix (`e0a0397`).
  - [x] Production Home builds the native image, reconciles only its derived
        managed helper, runs it as uid 10002 with one named spine volume and
        finite bounds, crosses through general setuid `mc`, publishes only the
        host UUID mirror, and passes the live capability probe (`ca4eae4`).
  - [x] Production doctor merges a closed four-finding helper report with host
        Home/routing/service facts, preserves the total nine-row/exit-0
        diagnostic contract, and drives real Container/Verify sections
        (`d6c6384`).
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
- **Phase 5 live-provider credentials**: synthetic projection is green, but
  the live legs need dedicated Mission Control-owned OAuth logins. The host
  Codex 0.145.0 personal ChatGPT login exists but MUST NOT be copied (single-
  owner rotation); Claude 2.1.218 currently reports unauthenticated. Public
  provider `token_url`/`client_id` constants and both required runtime switches
  were verified in those installed binaries without reading token values. Build
  isolated-login import into `MC_HOME/refresh-grants`; the operator must perform
  the Claude/browser consent and securely supply the MiniMax subscription key.
- **Phase 5 operator-input completion**: `OPERATOR-INPUTS.md` exists and is
  ignored, but does not yet record the required subscription-spend budget,
  Codex custom-CA version floor. Build the fail-closed import path independently;
  do not spend live tokens or load launchd until the budget and operator-present
  window exist.
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

- The setup clearing mechanism is delegated by the ADRs but lacks its own ADR.
- Landing currently cannot validate `pinned_closure_digest` because the
  assignment digest describes the initial closure, not the accepted rebuilt
  seal. Retry after a successful merge deliberately refuses rather than
  adopting. Details: `IMPLEMENTATION-NOTES.md` (2026-07-20).
- Landing failure taxonomy/backoff, serialized expected Git topology, and
  initiative-child sealed landing remain explicitly unresolved. Keep the
  canonical landing row derived; use the assignment's frozen `target_ref` and
  refuse divergence. Details are in the Phase 3 ledger.

## Homie runtime

ADR-016 D3 S1–S6 is complete and green: real Docker send→wake→reply→resume,
Console delivery/ack, and Playwright smoke. Deferred Phase 5 work is true
native resume, container reconciliation, Homie credential projection,
dashboard LaunchAgent generation, and the four non-Console tabs. Details and
commit map are in the closed Phase 4 ledger.

NEXT: split Routing, Worksource, Tunables, Surfaces, Runtime-auth, and
Supervision into host/path-free-helper halves, then make the unnamed production
wizard run the complete ordered composition. Keep live-token and launchd-load
acceptance legs parked.
