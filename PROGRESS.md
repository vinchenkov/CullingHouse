# PROGRESS — Mission Control implementation state

<!-- Live cross-session state only. Narrative history is in docs/ledger/. -->

REPO PATH: `~/dev/ai/homie`. Never relocate this repo into `~/Documents`,
`~/Desktop`, or `~/Downloads`: macOS TCC can revoke an agent session's own
filesystem access there during fan-out. Full Disk Access does not fix it.

LAST GREEN SHA: `ba3811c` — ADR-025 S1.3b: the `mc __setup-initiative`
subcommand + closed-union envelope arm wrapping S1.3a's
`MaterializeInitiativeStore` — the entire container side of the InitiativeSetup
cut (cross-base store/worktree, spike-validated GIT_DIR/GIT_WORK_TREE checkout;
fresh/retry with idempotent-residue). Host-side tested. Atop S1.1 (the
`initiative_setup_receipts` v14 table + read half) and S2/S3a (the inert
host-side mount arms). Still inert end-to-end: nothing invokes the cut yet, no
receipt producer, so a real child health-refuses on an absent store. S2 was
adversarially reviewed (3 lenses, no findings). Full fast suite green (after a
transient mc-land hang cleared on re-run — ledger 2026-07-23); `verbs`/
`cmd/mc`/`substrate` cold `-count=1` green; launchd not loaded. Prior codex
green was `28d6102` (production reset lifecycle).
Full Docker lanes (26 `docker_boundary`; 10 `docker_e2e`) were green at
`c8f37e9`-era HEAD. Extended Playwright smoke and install.sh dev walk were last
green at `d0ef4bb`; real onboarding crossing at `bf5981d`. Production image
`mc-prod` rebuilt for `28d6102`:
`sha256:7b3dbd79f204038bc02dfd477ab2c3899dc535c4c0b1ba1f9a275982af0861ab`,
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

Schema is v14. `mc onboard home` migrates v1 through v14 in place. v11 widened
the approve landing fence to assignment-armed tasks; v12 retires
`egress_policy`/`network_allow` and narrows `runtime_auth_delivery` to
`projection|materialized` (ADR-022) via the chain's first rebuild-and-copy
(NULL-stash of the worksource references, not deferred FKs — see ledger); v14
adds `initiative_setup_receipts` (ADR-025 D3: one immutable, initiative-keyed
row per shared-store cut — both setup roots + the recorded cut SHA). The
resident `SPINE_SCHEMA_VERSION` moved to 14 in lockstep.

## Known intermittent failures

1. `TestOnboardConcurrentFreshHomeNeverDeletesTheWinner` (`mc/verbs`), roughly
   1 in 21 full-suite runs. Repro:
   `cd mc && for i in $(seq 1 25); do mise exec -- go test ./verbs/ -count=1 || break; done`
   Concurrent provision creates a non-empty SQLite file before schema commit;
   `onboard.go` temporarily calls it corruption. Fail-closed and pre-existing;
   diagnosis is in `IMPLEMENTATION-NOTES.md` (2026-07-15).

2. `resident one-use dispatch control > rejects every identity mismatch before
   accepting child output` (`resident/src/resident-control.test.ts`),
   load-sensitive. Repro while another suite runs:
   `for i in $(seq 1 8); do ./resident/check.sh || break; done`
   Test-only child exit can race the fd-3 poller and surface `EBADF`; production
   waits for ack. Fail-closed and pre-existing.

## Phase state

- [x] Phase 0 — architecture-kill spikes S1–S8 green; no fallback ADRs.
- [x] Phase 1 — substrate and walking skeleton.
- [x] Phase 2 — dispatch, domain correctness, §18 verbs, split-brain
      convergence, initiative-wave review, and randomized properties.
- [x] Phase 3 — boundary conformance. COMPLETE 2026-07-22: all seven
      phase3-contract §8 "advances only when" bullets verified green (ledger
      2026-07-22 "§8 sweep" + this readiness check). Operator signed off on
      advancing 2026-07-22.
  - [x] ADR-016 through ADR-021 boundary design, mount authorization,
        jurisdiction, prepare/attest/commit, first-task and sealed-tree setup,
        production Worker/Verifier crossings, and sealed landing are complete.
  - [x] ADR-022 free-internet credential projection and synthetic-mint Docker
        acceptance are complete (`9c45d2b`..`c8f37e9`).
  - [x] Phase contract §8 mechanical checklist, Docker lanes, fast suite, tag
        vets, and packaged→approve→merge→archived walk were recorded green.
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
  - [x] Production Routing, Worksource, Tunables, and Surfaces are split by
        authority through one closed onboarding-state frame; real first-run
        and inputless replay crossed the native helper (`bf5981d`).
  - [x] Production binding catalog owns per-binding credential delivery and
        activates the provider-key/foreign-static pre-claim fence (`675cbe0`).
  - [x] Resident runtime-grant parsing/projection is a closed OAuth/static
        union and every non-fake route fails closed without it (`8886c09`).
  - [x] Runtime-auth import is isolated, owner-only, transactionally published,
        and blocked until every real adapter no-op passes (`556dc1e`).
  - [x] Real Codex/Claude-SDK/MiniMax adapters, native-session persistence,
        closed selection, and locked arm64 production runtime (`3007478`).
  - [x] Runtime-auth live no-op crosses the installed production adapter,
        adopts staged provider rotation durably, and requires exact native
        evidence before closed-set revalidation/publication (`4fa0dee`).
  - [x] Home onboarding atomically publishes the fixed production runner
        manifest under `MC_HOME/release/runner`; replay and upgrades preserve
        a closed owner-only runtime mount (`6202498`).
  - [x] Provider-owned Codex/Claude subscription logins run in disposable,
        minimal-environment homes and clean their sources around verified
        atomic import; metered/ambient credentials refuse first (`bd4385b`).
  - [x] Native resident/dashboard source and UI are atomically installed as a
        closed owner-only host payload, separate from the agent-visible runner
        tree (`1513fe3`).
  - [x] Production native host and Linux helper builds share one immutable
        release commit identity; malformed build identities fail closed before
        compilation (`73b710b`).
  - [x] Supervision atomically prepares exact resident/dashboard configs and
        per-user LaunchAgent plists only while both labels are unloaded;
        Homie receives the complete Worksource catalog read-only (`89c6f75`).
  - [x] Operator-present supervision activation installs/loads both exact jobs
        transactionally, requires a fresh release-bound tick receipt, rolls
        back every partial first activation, and drives doctor (`f10ddfc`).
  - [x] The production whole wizard composes all deterministic sections,
        preserves every dual-input flag, spends no token on healthy replay,
        and never implicitly activates launchd (`291aca8`).
  - [x] Production backup/restore crosses path-free framed snapshots with
        digest/schema/deployment fences, atomic owner-only host publication,
        retention, lost-slot-only restore, and resident startup/due chores
        (`072061f`).
  - [x] Production reset is confirmation-gated, requires supervision unloaded,
        commits a host backup before exact helper/volume teardown, and has an
        identity-bound already-reset replay (`28d6102`).
  - [~] ADR-025 accepted (production initiative mounts/cut/arc landing).
        Landed inert host-side, all fail-closed until a receipt producer exists
        (details in `docs/ledger/phase-5.md`): D10 reserve + two-family worktree
        grammar + two-base child skeleton/resolver (`fc72175`); S2 the
        receipt-vouched Worker mount arm (`6fd88cb`, 3-lens review clean); S3a
        the Verifier/Packager forced-RO reader arm (`875dcd8`). A real child
        still resolves an absent store and refuses; every other role/shape
        refuses. S1.1 landed the `initiative_setup_receipts` spine table (v14) +
        `LoadSubjectInitiativeSetup` + loader wiring (keyed on the parent
        initiative) + `CutSHA` carrier — the READ half of the D3 receipt; the
        register/write is owed to S1.5. S1.3 landed the container side of the
        cut: `MaterializeInitiativeStore` (cross-base store/worktree,
        spike-validated) and the `mc __setup-initiative` subcommand/envelope
        arm, host-side tested. Owed: S1.4 (dispatch step), S1.5 (resident
        precreate + register), S3b (D6 fence), S4–S6.
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
  were verified in those installed binaries without reading token values.
  Isolated provider-owned acquisition/import is complete at `bd4385b`; the
  operator must perform both browser consents and securely supply the MiniMax
  subscription key in the operator-present acceptance window.
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

- Sealed landing stays its separate `preparedLanding` lane: no lease/Run/write
  at dispatch; `mc land report` writes. Keep the three-part self-abort action
  identity and assignment-derived branch. Pinned tests:
  `TestNoLandingCellIsPlanAddressable`, `TestPlanMountsRefusesEveryLandingCell`,
  `mc/dispatch/sealed_landing_test.go`, `mc/substrate/landing_fence_test.go`.
- ADR-022 opens network only for agents; setup/landing/verifier stay offline.
  `MC_HOME/refresh-grants` stays protected, and resident schema version stays
  in lockstep with substrate. Details: `IMPLEMENTATION-NOTES.md`.

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

NEXT: ADR-025 S1.4 — dispatch emits the `InitiativeSetup` step. Per D3: for any
live, seeded `scope='initiative'` row whose `tasks.branch` is set but whose
initiative setup receipt is ABSENT, dispatch emits an `InitiativeSetup` step at
the first tick where promotion is observable and BEFORE any other
initiative-family spawn for that row. Study the existing task precreate/setup
step emission in the dispatch effect (how `TaskPrecreate`/`captureTaskSetup`
ride the prepared plan and how the resident consumes them; grep
`PrivateDispatchTaskPrecreate`, `snapshot.TaskPrecreate`, the dispatch effect
step types, and how `scope='initiative'` rows with `branch` set are recognized
in `mc/dispatch`). The step carries the initiative id + the target ref (fresh
cut) and, on retry, the recorded cut SHA — but the RECEIPT is not a
task_assignments row (D3). Then S1.5 (resident precreate + run the container +
`RegisterInitiativeSetup` write, deferred from S1.1), S3b (D6 fence), S4–S6. See
ADR-025 §Slices.
