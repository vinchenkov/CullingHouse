# PROGRESS — Mission Control implementation ledger

<!-- Header block: kept current by every session. -->
REPO PATH: `~/dev/ai/homie`. **Never relocate this repo into `~/Documents`,
`~/Desktop`, or `~/Downloads`.** Those three are macOS's TCC-protected triad;
agent fan-out breaks TCC attribution there and silently revokes the session's
own filesystem access mid-run (claude-code#59065, open). Moved out of
`~/Documents` on 2026-07-15 after exactly that killed a session. Full Disk
Access does NOT fix it — the failure precedes any policy lookup. Symptom:
`stat` works, reads return `Operation not permitted`, git says
`Unable to read current working directory`.

LAST GREEN SHA: 747f077 (local; the operator pushes manually — decided 2026-07-14. Agents: do not push.)
PHASES PASSING: Phase 0 COMPLETE (S1–S8 all green, no fallback ADRs; only operator-leg deferrals remain); Phase 1 COMPLETE (1a substrate 172; 1b walking skeleton reviewed-and-fixed — fake-harness 43, agent-runner 13, runner/image 40, resident 42, dispatch + cmd/mc suites; Docker e2e PASS ×4 total); Phase 2 COMPLETE for every unparked acceptance line (domain/§18 surface, deterministic split-brain convergence, bounded honesty + five mutants, tagged dispatch/metamorphic/twin-spine lifecycle properties; the initiative-wave CLI is no longer isolated — ADR-020 landed 2026-07-14 and closed the last Phase 2 acceptance line)
KNOWN-FAILING: `TestOnboardConcurrentFreshHomeNeverDeletesTheWinner` (mc/verbs),
INTERMITTENT — ~1 in 21 full-suite runs; 0/21 at HEAD, 15/15 and 60/60 green in a
clean worktree, so the rate is chance and the race is pre-existing, NOT caused by
the D4 slice (Go runs a package's tests in file order and onboard_test.go sorts
first, so the new tests cannot influence it). Real bug, fail-closed, breaks no
invariant. Repro: `cd mc && for i in $(seq 1 25); do mise exec -- go test ./verbs/
-count=1 || break; done`. Cause: `onboard.go:446` refuses a spine that
`exists && bytes > 0` with no meta identity as corruption — but that is also the
transient state of a *concurrent provisioner* (SQLite writes its first 4096-byte
page before the schema transaction commits), so a loser hard-fails with
`restore from backup (§16.4)`. Fix direction: that ambiguous state should
await/retry like the existing `awaitConcurrentProvision`/`recoverConcurrentProvision`
paths (which already handle the *later* stages of this same race) and refuse only
if it stays table-less. Owner: whoever next touches onboarding — not a Phase 3
blocker. Full diagnosis in IMPLEMENTATION-NOTES.md (2026-07-15).

Note the spine is now schema v3 (substrate.CurrentSchemaVersion): `mc onboard home` migrates older spines in place (v1→v2→v3); scratch MC_HOME spines need no action. Known storage gap: ADR-016 D2's activity/outbox hex fences admit BLOB forgeries (no typeof pin — they shipped in frozen v1→v2 and a column CHECK cannot be altered); a fence trigger in a later migration closes it. The D3 launch fences on homie_sessions already carry the typeof pin. Flagged in schema.sql at the D2 columns; log entry 2026-07-16.
FAST SUITE: mc/check.sh (gofmt + vet on the untagged build AND on the nightly/docker_e2e/test_fake_routing tagged builds — they must compile every commit, added 2026-07-14 after a tagged suite rotted invisibly — + go test ./...; includes substrate + promoted dispatch) + runner/fake-harness/check.sh + runner/agent-runner/check.sh + runner/image/check.sh + resident/check.sh. Docker e2e (phase-completion lane): cd mc && mise exec -- go test -tags docker_e2e -timeout 15m ./e2e/...

## Phases

Phases 0–2 are COMPLETE; their detail lived here long after it stopped being
state. It is in `docs/ledger/` (narrative), `spikes/*/RESULT.md` (spike
evidence), and the phase contracts (acceptance). Only what is still live is
kept below. Operator legs that remain open are under `## Parked`, not here.

- [x] Phase 0 — Architecture-kill spikes S1–S8, all GREEN, no fallback ADR
      signed (so ADRs 002–006 stay empty stubs — see docs/adr/INDEX.md).
      Still live from the spike findings:
      - S3: the canonical codex refresh token may be consumed on a race;
        recovery copy at `~/.mc-dev-home/spike03/race-codex/auth.json`
      - S2/S3/S4 deferred legs (30-min hold, DD-restart-mid-refresh, DD-restart)
        belong to the Phase 3/4 suites; S7's sleep drill + Resource Saver are
        operator legs (Parked)
      - S6's 8 interpretation notes are cited in-code as NOTE(S6.n)
- [x] Phase 1 — Substrate + walking skeleton. 1a schema/trigger lattice +
      155-case backstop (771480e); 1b contract, fake harness, agent runner, mc
      binary, resident tick loop, mc-fake-e2e image, Docker e2e behind the
      `docker_e2e` tag; adversarial review closed (12 findings → 9 fixed incl. 4
      majors, 4 refuted with reasons).
- [x] Phase 2 — Dispatch + domain correctness, every unparked acceptance line:
      dispatch table + SQL differential, domain aggregates, completion/fencing/
      two budgets, process flock + independent CAS, strict role/runner identity,
      immutable routing/directives/briefs, the full §18 verb/error/scope
      surface, the nine-kill-point split-brain convergence suite, and the
      nightly randomized/metamorphic/lifecycle properties with planted mutants.
      ADR-020 landed 2026-07-14 and closed the last line (the Editor's holistic
      wave review has a durable state, a dispatch arm, and a terminal).
- [ ] Phase 3 — Boundary conformance (Docker)
  - [x] Contract + adversarial mechanism/ownership review
        (`docs/phase3-contract.md`)
  - [x] Delegated boundary ADRs accepted after adversarial review: ADR-016
        spawn/wake crossing, ADR-017 mount/file plane, ADR-018 gateway/network
        topology, ADR-019 finite resource envelopes
  - [x] Pure mount policy: strict allowlist TOML/limits, POSIX targets and
        collision rejection, immutable blocked floor + additive patterns,
        bilateral RO/RW access (`mc/boundary`)
  - [x] Cross-harness takeover review of the Codex range (72a39db..4380e0d):
        no majors; mount-target control grammar deviation fixed red-first
        (67c4b61). ADR-vs-spec lens re-run separately (credit exhaustion)
  - [x] Filesystem identity + containment: trust seams, canonical resolution,
        raw+resolved blocked matching, `os.SameFile` allow-root uniqueness and
        ancestry, derived/validated suffix, symlink stays-vs-escapes (e01a2af)
  - [x] ADR-016..019 findings VERIFIED (operator decision 2 of 2026-07-14) and
        closed: 10 confirmed / 7 refuted, only 1 of 6 alleged majors survived;
        ADR-017's unrealizable privileged-tree ownership fixed (c6ca202), six
        deviations logged (69c19be), evidence in docs/reviews/ (6636e1e)
  - [x] Protected set + cross-Worksource jurisdiction (Dec. 3 step 5, Dec. 5):
        ADR-021 steps 1–8 complete after takeover repair, D8 absent-root/case
        semantics, D9/D11 reconstruction drift, and the planted-mutant sweep
        (3ad3411..ebb7613)
  - [x] macOS ACL leg of the trust seam: native no-follow volume/object
        snapshot, any non-owner allow grant rejected, membership UUID aliases
        resolved fail-closed, portable/static builds retained (942985e)
  - [x] ADR-016 D4 refusal taxonomy + closed detail, the pure half of the
        invalid-plan/no-claim transaction (`mc/refusal`, 315e932): whole
        consequence table by code, authority as a mount-only discriminator,
        allowlist carve-out always health, unknown/incoherent input refused;
        detail is enumerated-only so hostile text is leak-proof by
        construction. Anti-drift guard in boundary/codes_test.go. 4 mutants
        dead
  - [x] ADR-016 D4 consequence router at the dispatch seam (`verbs.applyRefusal`,
        8aa679e): the impure half. Stale → no mutation; Health → one
        `dispatch.health` activity; Candidate → subject task blocked with
        `confinement:<code>` / subjectless → health / Homie → ended in the same
        transaction. D4's four-part invariant (zero Runs, free lock, no spawn, no
        fall-through) asserted on every arm via a seeded fall-through bait task.
        20 tests / 109 subtests. `homieEndTx` factored so the seam can end inside
        its own transaction. 9 of 10 mutants dead (M6 equivalent by construction).
        Three deviations logged: the Homie end is unfenced-but-vacuous (D3's
        launch columns absent), `dispatch_key` is an input (no prepare step to
        derive it), the health action is one activity row (no §15.6 outbox
        fan-out — no block path has one yet). NOT YET REACHABLE from `mc
        dispatch`: nothing produces a Refusal, so the router has no caller but
        its tests
  - [x] ADR-016 D3 storage + fences (5fb4221..747f077): the eleven
        launch-fencing/resume-debt columns as the v2→v3 migration, pairing
        lattice as CHECKs with typeof pins and the closed (0,0) empty-prefix
        encoding; the D4 Homie end's `current_launch_id` generation fence
        (miss = no consequence, stale posture); the `homie.preflight_health`
        marker write half with its golden-vectored candidate key. Adversarial
        review (6 confirmed findings, all fixed; 2 refuted) closed the slice.
        The launch columns have NO production writer yet (`homie start` uses
        their defaults; resume does not clear/set them) — that is the
        selector/effector slices' work
- [ ] Phase 4 — E2E control loops (six scenario families)
- [ ] Phase 5 — Real-subscription acceptance (operator-scheduled)
- [ ] Release prep (after Phase 5): swap the repo's construction face for
      its distribution face — rewrite CLAUDE.md/AGENTS.md
      operator/contributor-facing (front door = install.sh + /onboard per
      spec §16.1/§17), ship the /onboard skill, operator decides fate of
      PROGRESS.md / IMPLEMENTATION-NOTES.md / spikes/ (keep-as-history vs
      docs/history/); specs/ and docs/adr/ stay. This repo IS the final
      deliverable (handoff §4.2 row 1) — no separate folder.

## Parked

Operator-only decisions. **No tombstones** (AGENTS.md §5): a resolved item is
deleted, not struck through. History is in `docs/ledger/`.

- **S7 sleep drill**: the 30-min Mac sleep mid-lease test needs the operator (an
  agent cannot sleep the machine it runs on). Instructions in
  `spikes/07-launchd-clock/RESULT.md`. All other S7 sub-tests passed.

NEXT: Begin ADR-016 D1's dispatch seam — the slice that makes a
`refusal.Refusal` producible and everything parked behind it reachable: the D4
router, its launch fence, the preflight marker, and D2's stored receipt fences
all have no production caller until this exists. Read `docs/adr/INDEX.md` →
016 D1 (line 25: one external command, private same-binary prepare → attest →
commit composition) and only then D5 (line 593) for the host-evidence half.

Slice it smallest-first: D1's command frame and the private prepare step that
(a) derives `dispatch_key` (today an INPUT to `applyRefusal` — the logged
honesty gap), (b) observes the launch generation the D4 fence compares, and
(c) runs the plan validator over `mc/boundary` so an invalid plan yields a
typed `refusal.Refusal` instead of nothing. The full D5 path-evidence predicate
is its own later slice; do not pull it into the frame slice.

Measured 2026-07-16, do not re-derive: `verbs.Dispatch` is still Phase 2's
single-transaction `Decide()` → `applyAction` (five kinds). Nothing produces a
`refusal.Refusal`. The eleven launch/debt columns exist (schema v3) but have NO
production writer — `homie start` relies on their defaults; resume does not yet
clear/set them; `grep -rn homie mc/dispatch` → zero hits. Keep aggregate mount
no-drop acceptance open until the planner exists. Queued behind this slice:
the D2 BLOB fence trigger (see header note). Do not load launchd.
