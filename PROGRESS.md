# PROGRESS — Mission Control implementation state

<!-- Live cross-session state only. Narrative history is in docs/ledger/. -->

REPO PATH: `~/dev/ai/homie`. Never relocate this repo into `~/Documents`,
`~/Desktop`, or `~/Downloads`: macOS TCC can revoke an agent session's own
filesystem access there during fan-out. Full Disk Access does not fix it.

LAST GREEN SHA: `ada715d` — five-leg fast lane green; `docker_e2e` green
(`ok mc/e2e 32s`, 8 tests) at `ada715d` on schema v12 with the ADR-022 spawn
seam live; `docker_boundary` all-PASS (24 subtests) at `bd39089` (the one
commit between them is resident-TS-only and cannot affect that Go lane). All
three tag vet lanes clean at `bd39089`. LESSON pinned by `ada715d`: the
resident's `SPINE_SCHEMA_VERSION` (resident-control.ts:12) mirrors
`substrate.CurrentSchemaVersion` in lockstep — every future schema bump must
touch BOTH, and only the Docker lane catches the miss. Production image
`mc-prod`
`sha256:8f12cc425a6d8f37e364b1627bb0e349a7fdbccf59035a25f58a57224a044a02`,
arm64/linux, Docker Desktop 29.4.0 aarch64, native (no --platform, no
emulation).

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

_(The parked §3 gateway/forbidden-env scope decision is RESOLVED — see
"Credential design" below. Nothing operator-only is parked here now.)_

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

## Credential design (RESOLVED — ADR-022, operator-approved 2026-07-21)

The parked §3 gateway question is answered: the operator requires **free
internet** for agent containers, which deletes the egress gateway/proxy as a
network control. ADR-022 supersedes ADR-018 whole and replaces it with a
resident-hosted **token service**: **access token in, refresh token out.** A
live POC (`scratchpad/oauth-poc/POC-RESULT-v2.md`) proved both runtimes adopt a
host-managed access token with only a dummy refresh in the container — Claude
PUSH (keep `.credentials.json` always-valid so it never self-refreshes), Codex
PULL (broker its refresh via `CODEX_REFRESH_TOKEN_URL_OVERRIDE`). Static keys
(MiniMax) can't be split → v1 materializes with the scoped-key advisory (D5).

Authoritative texts, already written: ADR-022; spec §11.4 amendment (2026-07-21)
+ Inv. 16/23 markers; `docs/phase3-contract.md` head amendment + the §3
*Credential projection* and *Forbidden env* rows. The whole gateway apparatus
(egress modes, `--network none`, `network_allow` enforcement, egress audit, the
`doctor gateway` finding) is STRUCK, not deferred.

**Not yet built** — this is the next implementation surface, spike-first:
1. [x] Spike `CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST` and real-token Codex startup
   refresh — DONE 2026-07-21, `spikes/09-credential-projection/RESULT.md`. Both
   resolved from source. **D3 adopts the flag** (Claude bypasses its credential
   store; the `invalid_grant` wipe is unreachable; the `.credentials.json`
   dummy-refresh rewrite is now the documented fallback). **D4 keeps the broker**
   as the mandatory reactive-401/long-session fallback (a real fresh JWT skips
   only the startup refresh). ADR-022 D3/D4 + Residuals amended;
   `IMPLEMENTATION-NOTES.md` (2026-07-21 S9) records the delegated choices.
2. [x] Step 2 COMPLETE (commits `9c45d2b`, `dd380e0`, `d4e14f0`, `029acc4`):
   schema v12; `mc/boundary/envpolicy.go` forbidden-env builder (§16.3 floor,
   extend-only guard, D7 fence, typed env.invalid/env.forbidden);
   `resident/src/token-service.ts` (refresh-ahead behind an injected mint
   seam, Claude flag writer, Codex auth.json writer);
   `resident/src/refresh-broker.ts` (loopback /oauth/token). All green, all
   inert. Ledger: 2026-07-21 "ADR-022 step 2 built end to end".
3. Wire into the resident spawn seam, removing the gateway wiring; relax the
   `docker_boundary` `--network none`/egress assertions to the §3 credential
   rows; retire the `doctor gateway` finding; source binding-catalog
   `ProviderCredentialKeys`/`DeclaredStaticKey` for the env builder.

Removal/wiring inventory for steps 2–3 is mapped in the phase-3 ledger
(2026-07-21 S9 entry) — spawn seam, schema migration, forbidden-env floor,
doctor finding, assertion swaps. Three load-bearing constraints carried up
here: (a) drop `--network none` for the **agent class only** — setup/landing/
verifier classes keep isolation, logged as a deviation; (b) CRITICAL —
`resolveGatewaySecretRoots` (`mc/verbs/mountattest.go:408`) must be
**repurposed, not deleted**, to deny-mount the on-disk refresh-grant store, or a
candidate mount could bind the refresh grants in; (c) `gateway_control_version`
is the RETAINED one-use control channel, not egress — do not rename without
lockstep golden bytes.

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

NEXT: Drive the `docs/phase3-contract.md` §8 completion checklist. Both Docker lanes are green at v12 with the spawn seam live (no agent-class egress assertion drift surfaced — the e2e's own fixtures pin their own containers, and resident-spawned fake-family argv is unchanged). What §8 still needs, in buildable order: (1) the doctor `container-runtime` capability probe (its "runs in Phase 3" deferral is Phase-3-owned and §8 forbids it) — real-mechanism probes for the setuid gate and helper exec; (2) the §3 *Credential projection* acceptance row's Docker test — needs the `main.ts` projector wiring, which needs the `MC_HOME/refresh-grants` store capture (install/onboarding obligation) and REAL provider credentials for the live-call legs: park the live legs for the operator if credentials are not in `OPERATOR-INPUTS.md`; the fail-closed/no-refresh-material legs are buildable with synthetic mints; (3) the §3 *Forbidden env* row's mechanism test through the production owner (dispatch-side env-plane wiring: `LoadDispatchWorksourceProjection` grows the two env columns, `BuildEnvPlan` refuses `env.forbidden` before claim, effect carries validated entries; SENTINEL walk + docker-inspect absence proof); (4) §8 sweep: no fake route in the production image, no production override redirecting spine/identity, then record digest/arch/capability-probe evidence here. Steps 2 and 3(a)-(d)+(f) of ADR-022 are DONE (`9c45d2b`..`ada715d`); the SPINE_SCHEMA_VERSION lockstep lesson is pinned above. Non-blocking obligations remain in `IMPLEMENTATION-NOTES.md` (2026-07-21): unenforced 15-minute landing deadline, `SealedLandingResult` has no spine consumer, ADR-016 D7 label non-conformance in setup/legacy-land.
