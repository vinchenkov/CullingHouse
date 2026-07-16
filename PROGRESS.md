# PROGRESS — Mission Control implementation ledger

<!-- Header block: kept current by every session. -->
LAST GREEN SHA: 48eaf63 (local; the operator pushes manually — decided 2026-07-14, see Parked. Agents: do not push.)
PHASES PASSING: Phase 0 COMPLETE (S1–S8 all green, no fallback ADRs; only operator-leg deferrals remain); Phase 1 COMPLETE (1a substrate 172; 1b walking skeleton reviewed-and-fixed — fake-harness 43, agent-runner 13, runner/image 40, resident 42, dispatch + cmd/mc suites; Docker e2e PASS ×4 total); Phase 2 COMPLETE for every unparked acceptance line (domain/§18 surface, deterministic split-brain convergence, bounded honesty + five mutants, tagged dispatch/metamorphic/twin-spine lifecycle properties; the initiative-wave CLI is no longer isolated — ADR-020 landed 2026-07-14 and closed the last Phase 2 acceptance line)
KNOWN-FAILING: (none). Note the spine is now schema v2 (substrate.CurrentSchemaVersion): a spine created by an mc build older than 48eaf63 is migrated in place by `mc onboard home`; scratch MC_HOME spines need no action.
FAST SUITE: mc/check.sh (gofmt + vet on the untagged build AND on the nightly/docker_e2e/test_fake_routing tagged builds — they must compile every commit, added 2026-07-14 after a tagged suite rotted invisibly — + go test ./...; includes substrate + promoted dispatch) + runner/fake-harness/check.sh + runner/agent-runner/check.sh + runner/image/check.sh + resident/check.sh. Docker e2e (phase-completion lane): cd mc && mise exec -- go test -tags docker_e2e -timeout 15m ./e2e/...

## Phases

- [~] Phase 0 — Architecture-kill spikes (S1–S8, handoff Part 2)
  - [x] S1 setuid gate — GREEN incl. DD-restart + volume reattach; no fallback
  - [x] S2 exec fidelity — GREEN (30-min hold deferred to Phase 3 suite);
        signal-cancellation protocol finding in RESULT.md
  - [x] S3 OAuth lifecycle — GREEN (materialized posture works both bindings;
        DD-restart-mid-refresh deferred to serialized leg; see RESULT.md
        finding 5: canonical codex refresh token may be consumed — recovery
        copy at ~/.mc-dev-home/spike03/race-codex/auth.json)
  - [x] S4 egress gateway + CA — GREEN (fail-closed net shape proven live;
        codex streams over WebSocket; ADR-005 base_url routing confirmed;
        DD-restart leg deferred to serialized leg)
  - [x] S5 SQLite WAL crash discipline — GREEN incl. DD restart mid-write
  - [x] S6 dispatch decision table — GREEN; 8 interpretation notes (NOTE(S6.n))
  - [x] S7 launchd + clock — GREEN unattended; sleep drill + Resource Saver
        are operator legs (see Parked)
  - [x] S8 arm64 image + Playwright — GREEN, digest-pinned
- [x] Phase 1 — Substrate + walking skeleton (fake harness built here)
  - [x] 1a substrate: schema + trigger lattice + 155-case backstop (771480e)
  - [x] 1b walking skeleton: contract (docs/phase1b-contract.md), fake
        harness (runner/fake-harness), agent runner (runner/agent-runner),
        mc binary (init/task add/dispatch/complete/editor decide/strategist
        propose/verifier verdict/packet decide/land report/heartbeat/
        register-session/lock get/run list; S6 Decide() promoted to
        mc/dispatch byte-identical), resident tick loop (resident/),
        mc-fake-e2e image (runner/image), Docker e2e green ×2 behind
        `docker_e2e` build tag
  - [x] 1b adversarial review closure: correctness lens re-run, 12 findings
        adversarially verified → 9 confirmed and FIXED (4 major: run.json
        RW-alias/Inv. 26, mc-land silent checkout, mc-land missing
        merge --abort, register-session lease-fence race), 4 refuted with
        documented reasons; fast lane + Docker e2e re-green
- [x] Phase 2 — Dispatch + domain correctness (all unparked acceptance)
  - [x] Wave 1 unparked acceptance: dispatch table + SQL differential;
        domain aggregates; completion/fencing/two budgets; process flock +
        independent CAS; strict role/runner identity; immutable routing,
        directives, and claimed-state briefs; adversarial review closed
  - [x] Strategist wave CLI: UNPARKED and landed 2026-07-14 via ADR-020 (the
        Editor's holistic plan review now has a durable state, a dispatch arm,
        and a terminal)
  - [x] Wave 2 full unparked §18 verb/error/scope surface
  - [x] Split-brain kill-point convergence suite
    - [x] action selected / before effect; session folder / before run.json
    - [x] run.json / before container; container start / before heartbeat
    - [x] workspace bytes / before commit; git commit / before complete
    - [x] operator approve / before land; merge success / cleanup or report gap
    - [x] message/outbox insert / delivery
  - [x] Nightly randomized/metamorphic/lifecycle properties + planted mutants
    - [x] bounded generator honesty + exact five-mutant fast gate
    - [x] tagged dispatch state fuzzer + ineligible-row metamorphism
    - [x] tagged twin-spine lifecycle random walk
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

- **Docker Desktop settings snapshot** (handoff §4.1 row 4): partially recorded.
  `UseResourceSaver: False` and `AutoStart: True` are verified. Still unrecorded:
  ECI state, VM sizing, version pin. Operator confirms and freezes.
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

      [projects."/Users/vinchenkov/Documents/dev/ai/homie"]
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

NEXT: Implement ADR-016's invalid-plan/no-claim dispatch transaction red-first,
without wiring production planning — unchanged from the previous NEXT, which the
D2 storage slice was the prerequisite for. Re-read Decisions 1–4 and derive the
exact typed classified-refusal input at the commit seam; prove allowlist
trust/invalid refusals record deployment health, candidate-owned mount refusals
apply their subject/task, subjectless, or Homie consequence, and every arm
leaves zero new Run rows, a free lock, no spawn effect, and no fall-through to
another candidate. D2's fences are now storable: the commit-side `dispatch_key`
and the prepare-side `dispatch_request_id`/`dispatch_result` receipt exist and
are enforced, so the transaction can use them rather than re-deriving idempotency.
Keep aggregate mount no-drop acceptance open until the planner exists. Do not
load launchd.
