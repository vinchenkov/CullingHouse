# PROGRESS — Mission Control implementation state

<!-- Live cross-session state only. Narrative history is in docs/ledger/. -->

REPO PATH: `~/dev/ai/homie`. Never relocate this repo into `~/Documents`,
`~/Desktop`, or `~/Downloads`: macOS TCC can revoke an agent session's own
filesystem access there during fan-out. Full Disk Access does not fix it.

LAST GREEN SHA: `c8f37e9`+doctor-tighten — ALL lanes green: five-leg fast,
all three tag vets, full `docker_boundary` (26 subtests, incl. the two-arm
doctor capability probe), full `docker_e2e` (10 tests: `ok mc/e2e 58s`, incl.
both credential-projection legs and the packaged→approve→merge→archived
walk). Production image `mc-prod` at `5d7f539`:
`sha256:47b27eda69019d1e97c9618466ed391470447ebae6025270abb1931914c487a6`,
arm64/linux, Docker Desktop 29.4.0 aarch64, native (no --platform, no
emulation) — unchanged by TS-only commits since. LESSON pinned by `ada715d`:
the resident's `SPINE_SCHEMA_VERSION` (resident-control.ts:12) mirrors
`substrate.CurrentSchemaVersion` in lockstep — every schema bump must touch
BOTH, and only the Docker handshake lane catches the miss.

The operator pushes manually; agents do not push.

PHASES PASSING: Phase 0 COMPLETE; Phase 1 COMPLETE; Phase 2 COMPLETE. Phase 3
is in progress. Completed implementation history is in
`docs/ledger/chronology-phase-0-2.md` and `docs/ledger/phase-3.md`; do not read
either at startup.

FAST SUITE:
`./mc/check.sh && ./runner/fake-harness/check.sh && ./runner/agent-runner/check.sh && ./runner/image/check.sh && ./resident/check.sh`

Phase-completion Docker regression:
`cd mc && mise exec -- go test -tags docker_e2e -timeout 15m ./e2e/...`

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
  - [ ] OPERATOR SIGN-OFF to advance Phase 3 → Phase 4 (parked below): the
        only remaining Phase 3 items are operator-owned, not mechanical.
- [ ] Phase 4 — six E2E control-loop scenario families.
- [ ] Phase 5 — operator-scheduled real-subscription acceptance.
- [ ] Release prep — install/onboard front door and construction-document
      disposition.

## Parked

- **Phase 3 → Phase 4 advancement (operator sign-off)**: the §8 mechanical
  checklist is green (ledger 2026-07-22 "§8 sweep"), so what remains is
  operator-owned, not code: (1) the LIVE-PROVIDER credential legs — a real
  Claude/Codex subscription call through a projected token; `OPERATOR-INPUTS.md`
  has only gateway-era `CLAUDE_CRED_DIR`/`CODEX_CRED_DIR` pointers, no ADR-022
  refresh-grant material, so the resident's `MC_HOME/refresh-grants` store must
  be populated from a real subscription before this can run; (2) the Phase-5
  runtime-auth health turn (same dependency); (3) the S7 sleep drill below.
  The synthetic-mint Docker acceptance proves the projection mechanism end to
  end without live credentials. Operator: confirm advancing to Phase 4, or
  provide refresh-grant material to close the live legs first.
- **S7 sleep drill**: the 30-minute Mac sleep mid-lease test requires the
  operator. Instructions: `spikes/07-launchd-clock/RESULT.md`. All other S7
  sub-tests passed.

_(The parked §3 gateway/forbidden-env scope decision is RESOLVED — see
"Credential design" below.)_

## Sealed landing (COMPLETE host-side; Docker evidence pending)

LIVE since `d91e388`: `nextLanding` selects an approved assignment-backed row,
routes it through prepare/attest/commit on BOTH paths, and the sealed E2E
merges `packaged -> approve -> merge -> archived` with a real `--no-ff` merge.
Design/activation narrative: phase-3 ledger (2026-07-20/21 entries); six
delegated decisions in `IMPLEMENTATION-NOTES.md` (2026-07-21). The §2 Docker
acceptance lane below is what turns the host-side proof into evidence.

Constraints for anyone touching the seam:

- The self-abort gate is ACTION identity with three parts — `MERGE_HEAD` is
  the reviewed SHA, `MERGE_MSG` is a message this landing WROTE (our subject +
  trailer at column 0), and the target still at the frozen preimage. Do not
  loosen any of the three; the operator can reproduce `MERGE_HEAD` and
  `git merge --log` can splice an agent subject into MERGE_MSG. Details:
  `IMPLEMENTATION-NOTES.md` (2026-07-20).
- A landing is a SEPARATE lane — the third `preparedDispatch` variant
  (`landing *preparedLanding`), never a `preparedCandidate`. The spawn seam
  dereferences `cand.spawn` unguarded in dozens of places; the separation is
  what keeps those unreachable with a nil Spawn. A landing takes no lease,
  opens no Run, writes nothing at dispatch time; `mc land report` writes.

MUST NOT RELAX: `TestNoLandingCellIsPlanAddressable` and
`TestPlanMountsRefusesEveryLandingCell` (`landingplan_test.go:77,103`) — an
earlier draft widened them and review rejected it. Also
`mc/dispatch/sealed_landing_test.go` and `mc/substrate/landing_fence_test.go`.

The branch comes from the immutable assignment and is never projected into
`tasks.branch`; `complete.go:163` is that column's only writer and is closed to
assigned tasks, which is what makes the lanes partition. `/repo` is not
plan-addressable. The nested `.mission-control` cover must prevent the RW source
alias from exposing the sealed task root as writable.

### 2. Phase 3 completion lane — DONE

The realized landing mount table, RO alias/cover, network/uid/capability
envelope, and the `packaged → approve → merge → archived` walk are proved in
`docker_e2e`/`docker_boundary`; the §8 mechanical checklist is green (ledger
2026-07-22 "§8 sweep"). What remains before Phase 4 is operator-owned, parked
above.

## Credential design (ADR-022) — BUILT and PROVEN (synthetic); live legs parked

ADR-022 (operator-approved 2026-07-21) supersedes ADR-018 whole: free internet
for agent containers, one property kept — **access token in, refresh token
out** via a resident-hosted token service. The whole gateway apparatus (egress
modes, `--network none` for agents, `network_allow`, egress audit, `doctor
gateway`) is STRUCK. Static keys (MiniMax) can't be split → materialized with
the D5 scoped-key advisory.

Implementation COMPLETE, all green (`9c45d2b`..`c8f37e9`): schema v12;
`mc/boundary/envpolicy.go` forbidden-env builder + pre-claim dispatch refusal;
`resident/src/token-service.ts` + `refresh-broker.ts` + `credential-projector.ts`
wired through `main.ts` and the spawn seam; `resolveGatewaySecretRoots`
repurposed to deny-mount `MC_HOME/refresh-grants`; doctor gateway retired +
container-runtime capability probe added; `TestCredentialProjection*` Docker
acceptance (synthetic mints). Load-bearing invariants that must not regress:
`--network none` dropped for the AGENT class only (setup/landing/verifier keep
it, logged deviation); `resolveGatewaySecretRoots` deny-mounts the grant store
(never delete); `gateway_control_version` retained (golden bytes). Full design
narrative and the delegated choices: ledger 2026-07-21/22 + `IMPLEMENTATION-NOTES.md`.

Still open (non-blocking, tracked below/parked): binding-catalog
`ProviderCredentialKeys`/`DeclaredStaticKey` sourcing and the operator
env-guard config surface for the forbidden-env builder; the live-provider
credential legs (parked, need real refresh-grant material).

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

NEXT: Phase 3's §8 mechanical checklist is GREEN and the credential surface is
built and proven with synthetic mints (ledger 2026-07-22 "§8 sweep"). Advancing
Phase 3 → Phase 4 is now an OPERATOR SIGN-OFF (see ## Parked) — the remaining
Phase 3 items (live-provider credential legs, Phase-5 runtime-auth turn, S7
sleep drill) are operator-owned, not code. While awaiting that sign-off, the
buildable non-blocking follow-ups, smallest first: (a) source the binding
catalog's `ProviderCredentialKeys`/`DeclaredStaticKey` into the forbidden-env
builder (today `attestCandidateEnvPolicy` passes none, so only the floor +
refresh-token fence are active — the per-binding provider-key rejection is
inert until the catalog is threaded); (b) the operator env-guard config surface
for `NewEnvGuard` additions (no loader exists — `BlockPolicy` has the same gap);
(c) the non-blocking obligations in `IMPLEMENTATION-NOTES.md` (2026-07-21):
unenforced 15-minute landing deadline, `SealedLandingResult` has no spine
consumer, ADR-016 D7 label non-conformance in setup/legacy-land. Do NOT start
Phase 4 without the operator sign-off above.
