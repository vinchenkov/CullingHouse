# PROGRESS — Mission Control implementation state

<!-- Live cross-session state only. Narrative history is in docs/ledger/. -->

REPO PATH: `~/dev/ai/homie`. Never relocate this repo into `~/Documents`,
`~/Desktop`, or `~/Downloads`: macOS TCC can revoke an agent session's own
filesystem access there during fan-out. Full Disk Access does not fix it.

LAST GREEN SHA: `cc7dac9` — ADR-025 S3b.3b-5: the owed end-to-end coverage on the
D6 commit re-authoring fence — a real-routed initiative-child Worker driven
through dfPrepare/dispatchAttest/dfCommit (happy path carries the marker in the
committed effect; a stripped `attested.mountPlan.InitiativeChild` returns a stale
refusal with no new run / free lease). Verified `DispatchCommitPrivate` routes
through `dispatchCommit`, so the fence guards the private path too. The full
dispatch side of the D6 marker (S3b.3b-1..5) is now COMPLETE + covered: the
`initiative_child` marker (id + frozen prior-child run set) is authored at attest
on a real-routed child (`expectedInitiativeChildMarker`), re-derived + DeepEqual-
fenced at `dispatchCommit` against the broker frame, and reaches the effect's
mount_plan. Commit chain: S3b.1 `ad88dc9` (resident absence loop) → S3b.2
`cf71fc7` (`mc __verify-initiative-clean` executor) → S3b.3a `8b9c209` (loader) →
S3b.3b-1 `4f43ba6` (freeze into mount state) → S3b.3b-2 `bfcf9b9` (marker type +
validation) → S3b.3b-3/4 `cc7a3a8` (author + commit fence) → S3b.3b-5 `cc7dac9`
(e2e coverage). S1 (the cut) is COMPLETE (S1.5b-2 `8aba956`). Only S3b.4 (the
resident create-path gate) remains to make D6 LIVE — the resident does not yet
consume the marker. Full fast suite green (the load-sensitive resident EBADF flake —
intermittent #2, ~1 in 3 under heavy looping — clears on re-run);
`verbs`/`dispatch`/`substrate` cold `-count=1` green; launchd not loaded. Prior
codex green was `28d6102`.
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
   load-sensitive; observed ~1 in 3 full-suite runs during heavy parallel
   looping (2026-07-24). Repro while another suite runs:
   `for i in $(seq 1 8); do ./resident/check.sh || break; done`
   Test-only child exit can race the fd-3 poller and surface `EBADF` on
   read/close; production waits for ack. Always clears on an isolated re-run.
   Fail-closed and pre-existing.

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
  - [x] Production install/bootstrap: fail-closed preflight (`bc0dee4`);
        credential-store refusal + domain-separated deployment identities
        (`9cec34f`/`e7c4ca2`); private `__onboard-spine` state matrix (`e0a0397`);
        native uid-10002 helper + Home crossing (`ca4eae4`); composed
        host/helper doctor (`d6c6384`).
  - [x] Production onboarding wizard: Routing/Worksource/Tunables/Surfaces split
        (`bf5981d`); binding catalog + pre-claim fence (`675cbe0`); resident
        runtime-grant OAuth/static union (`8886c09`); isolated runtime-auth import
        (`556dc1e`); real Codex/Claude-SDK/MiniMax adapters + arm64 runtime
        (`3007478`); live no-op crossing + staged rotation (`4fa0dee`); runner
        manifest publish (`6202498`); provider-owned subscription logins
        (`bd4385b`); native source/UI install (`1513fe3`); shared release
        identity (`73b710b`); the composed whole wizard (`291aca8`).
  - [x] Production supervision + lifecycle: LaunchAgent prep while unloaded
        (`89c6f75`); operator-present activation with rollback (`f10ddfc`);
        path-free backup/restore (`072061f`); confirmation-gated reset (`28d6102`).
  - [~] ADR-025 accepted (production initiative mounts/cut/arc landing); full
        slice map in `docs/ledger/phase-5.md`. Groundwork + S2/S3a landed the
        inert host-side mount arms (Worker RW + Verifier/Packager RO,
        receipt-vouched; `fc72175`/`6fd88cb`/`875dcd8`; S2 3-lens review clean).
        S1 (the cut) is COMPLETE: S1.1 the `initiative_setup_receipts` v14 table +
        read; S1.3 the container side (`MaterializeInitiativeStore` + `mc
        __setup-initiative`); S1.4 the whole route-free dispatch lane, LIVE
        in-process (emission gated on `RealRouting`; design + build-tag nuance in
        IMPLEMENTATION-NOTES 2026-07-23); S1.5a/a.2 the Go register+continue verbs
        + CLIs; S1.5b-1 `precreateInitiativeSkeleton`; S1.5b-2 the resident
        `initiative-setup` effect handler that runs the cut. Owed: S1.4c-2c
        (Darwin private-frame carrier — non-blocking, guarded fail-closed), S3b
        (D6 fence), S4–S6.
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

NEXT: ADR-025 S3b.4 — the resident create-path gate that makes the D6 fence LIVE
(the whole dispatch side, S3b.1/S3b.2 executors + S3b.3 marker, is landed +
covered). In resident/src: (1) types.ts — add `initiative_child?: { initiative_id:
number; prior_child_runs: string[] }` to `MountPlan`. (2) effects.ts `spawn()` —
when `effect.mount_plan.initiative_child` is present, BEFORE the `docker create`
(~effects.ts:705), run the two D6 fences fail-closed (log + return on any failure,
no spawn): first `requireInitiativeChildrenAbsent(marker.prior_child_runs, deps)`
(already landed, effects.ts:394), then launch `mc __verify-initiative-clean`
(S3b.2, already landed) in a container mirroring the `initiativeSetup` handler's
run (network none, uid 10002, cap-drop ALL) binding ONLY store + worktree +
fence.json — `${store}:/repo/store:ro`, `${store}/git:/repo/store/git`,
`${worktree}:/repo/worktree`, `${fenceJson}:/mc/fence.json:ro` — with envelope
{schema_version:1, operation:"initiative-clean-fence", initiative_id,
store_root:"/repo/store", worktree_root:"/repo/worktree"}; rm the fence.json after.
OPEN CHOICE: the store/worktree HOST paths — derive from the plan entries (store =
source of the `/workspace` entry, worktree = source of `/workspace/source`; the
ADR-025 D2 table fixes those destinations) OR carry them in the marker (cleaner
data flow but re-touches the Go marker/validation/commit-fence + the 3 S3b.3b
tests). Lean derive-from-entries (less churn; destinations are fixed by D2). (3)
Wire nothing new in TickDeps — reuse `deps.docker`/`deps.fs`/`deps.runMc`. (4)
effects.test.ts: fence-clean (both docker inspects absent + `__verify-initiative-clean`
exit 0) → create proceeds; a present prior child → spawn refused (no create); a
non-zero clean fence → spawn refused; assert the exact fence-container argv +
fs events (write/rm fence.json). Then S4–S6; the Darwin private-frame carrier
(S1.4c-2c) is owed but non-blocking. See ADR-025 §Slices + the D6-fence scout
(phase-5 ledger 2026-07-23/24) + the S3b.3b scout map.
