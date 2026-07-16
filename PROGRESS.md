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

LAST GREEN SHA: 556968c (local; the operator pushes manually — decided 2026-07-14, see Parked. Agents: do not push.)
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

Note the spine is now schema v2 (substrate.CurrentSchemaVersion): a spine created by an mc build older than 48eaf63 is migrated in place by `mc onboard home`; scratch MC_HOME spines need no action.
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

Reconciled against evidence 2026-07-15; eleven resolved entries removed. Two
were actively lying: a live entry asked the operator to create a private remote
(`origin` exists and `main` tracks it), and a live entry said the Sacrificial
Worksource directive was unwritten and blocked e2e (it is in OPERATOR-INPUTS.md).
In both, the struck-through entry was the true one.

- **Docker Desktop settings snapshot** (handoff §4.1 row 4): measured 2026-07-15 —
  nothing left to look up, only the freeze decision is the operator's. Server
  **29.4.0**; `EnhancedContainerIsolation: false` (S1's setuid gate depends on this
  staying off — if a Docker auto-update ever flips it, the Phase 3/4 Docker suites
  break in a way that reads like a code bug), `UseResourceSaver: false`,
  `AutoStart: true`; VM **14 CPU / 8092 MiB RAM / 1024 MiB swap / 122880 MiB disk**.
  Operator: confirm these are the frozen values and pin the version. Worth a glance
  while there: 8 GiB against 14 CPUs is thin for Phase 4's six scenario families
  (the arm64 + Playwright image is ~1–2 GB before anything runs).
- **S7 sleep drill**: the 30-min Mac sleep mid-lease test needs the operator (an
  agent cannot sleep the machine it runs on). Instructions in
  `spikes/07-launchd-clock/RESULT.md`. All other S7 sub-tests passed.
- **Codex autonomy profile** (handoff §1.5): the agent may not write
  `approval_policy="never"` / `sandbox_mode="danger-full-access"` into
  `~/.codex/config.toml` (auto-mode classifier denial — correctly: self-configured
  unsafe agents need the operator's hand). Operator: append, then the takeover
  smoke (`codex exec -p mc` + one `/goal` set/clear) can run:

      [features]
      goals = true

      [profiles.mc]
      approval_policy = "never"
      sandbox_mode = "danger-full-access"

      [projects."/Users/vinchenkov/dev/ai/homie"]
      trust_level = "trusted"

- **Claude Code permission posture** (handoff §1.4): the agent may not widen its
  own allowlist (`.claude/settings.json` write denied by the classifier).
  Operator: either run sessions here with `claude --dangerously-skip-permissions`
  (handoff-recommended), or create `.claude/settings.json` allowing
  `go/bun/docker/git/mise/sqlite3`.
- **db_schemas.sql missing** (handoff §1.1): not in the seeded folder. Schema is
  derived from spec §4/§5 (spec wins anyway). Informational — drop the file in the
  repo root if it exists.
- **docs/priors/ POC evidence missing**: `poc/` copies and original memory notes
  were not seeded. The three §4.3 priors are reconstructed one-line notes marked
  RECONSTRUCTED. Drop original POC material into `docs/priors/` if it exists.

NEXT: Land ADR-016 D3's launch-fencing columns as the v2→v3 migration, red-first
(`docs/adr/INDEX.md` → 016 D3, line 275; the column list is lines 280–302). This
is the smallest slice that unblocks the most: it is named as the blocker by two
separate deviations logged 2026-07-15, and by the Homie arm of the D4 router
that just landed.

Eleven columns on `homie_sessions`, with the pairing rules as CHECKs, not as Go
politeness: `current_launch_id` (exactly 16 lowercase hex when present) and
`current_launch_mode` (`fresh|native|rows`) are both-null-or-both-present;
`current_container_id` (exactly 64 lowercase hex) and `launch_bound_at` are
paired and require a current launch; `launch_started_at` requires the bound
pair; `resume_owed` (`0|1`) plus `resume_mode` (`native|rows`) carry the debt and
are **mutually exclusive with a current launch**; only `rows` mode carries the
paired `*_prime_through_seq` / `*_prime_row_count` cutoff/count (non-negative),
on both the launch and resume sides. `homie start` initializes every
launch/debt field empty/zero.

Copy the hex CHECK shape from the D2 fences already in `schema.sql:742-757` —
`length() = N AND length(CAST(x AS BLOB)) = N AND x NOT GLOB '*[^0-9a-f]*'`. The
dual-length test is not decoration: it is what stops a NUL-truncated forgery
storing as a distinct UNIQUE value its own lookup cannot find. The v1→v2
migration at `substrate.go:111-120` (`migrationV1ToV2`) is the pattern to follow,
and `substrate/migration_test.go` is where its test lives.

Then close the two things this unblocks, in order:
  1. The D4 Homie arm's launch fence. `verbs/refusalroute.go`'s RefusalHomie arm
     names the seam in a comment; it gains a `current_launch_id` predicate and
     nothing else changes.
  2. `homie.preflight_health` (ADR-016 line 433) — D3's starvation marker, NOT a
     D4 consequence. Its `candidate_key = SHA256` covers the pre-prepare
     canonical session id, current launch/resume debt, frozen binding, and
     conversation sequence, plus `defer_pipeline=true`. It is only meaningful
     once something selects Homie candidates, which nothing does yet
     (`grep -rn homie mc/dispatch` → zero hits).

Measured, still true, do not re-derive: there is NO prepare/attest/commit seam —
`verbs.Dispatch` is still Phase 2's single-transaction `Decide()` → `applyAction`
(five kinds). Nothing produces a `refusal.Refusal`, so `applyRefusal` has no
caller but its tests. Making it reachable is D1/D5's slice (the host plan
validator over `mc/boundary`), not this one. Keep aggregate mount no-drop
acceptance open until the planner exists. Do not load launchd.
