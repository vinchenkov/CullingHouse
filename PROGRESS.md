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

LAST GREEN SHA: 315e932 (local; the operator pushes manually — decided 2026-07-14, see Parked. Agents: do not push.)
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
  - [x] ADR-016 D4 refusal taxonomy + closed detail, the pure half of the
        invalid-plan/no-claim transaction (`mc/refusal`, 315e932): whole
        consequence table by code, authority as a mount-only discriminator,
        allowlist carve-out always health, unknown/incoherent input refused;
        detail is enumerated-only so hostile text is leak-proof by
        construction. Anti-drift guard in boundary/codes_test.go. 4 mutants
        dead
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

NEXT: Apply the ADR-016 D4 consequence router at the dispatch seam, red-first —
the impure half of the invalid-plan/no-claim transaction, whose input
(`refusal.Refusal`/`Classify`, 315e932) now exists and is proven. Route
ClassHealth to one health action, ClassCandidate by subject (block the subject
task with the code / subjectless pipeline → health / Homie → end with
`confinement:<code>`), and ClassStale to no mutation at all. Prove every arm
leaves zero new Run rows, a free lock, no spawn effect, and no fall-through to
another candidate — the four-part invariant has no fixture yet.

Three corrections to the prior NEXT, measured not assumed (see the ledger entry
for 2026-07-15):
  1. There is NO commit seam to attach to. `verbs.Dispatch` is still the Phase-2
     single-transaction `Decide()` → `applyAction` (five kinds); prepare/attest/
     commit does not exist. This slice creates the seam.
  2. D2's fences are storage-only. `grep -rn "MC-DISPATCH"` and `sha256` over
     mc/ both return **zero hits**; no Go code writes dispatch_key,
     dispatch_request_id, dispatch_result, source_activity_id, or
     event_destination_key. The real derivation needs a preparation token from a
     prepare step that does not exist. Take `dispatch_key` as an INPUT to the
     transaction alongside the refusal; leave derivation to the prepare slice.
  3. The Homie arm cannot be launch-fenced yet: none of D3's eleven
     `homie_sessions` launch/resume columns exist (48eaf63 landed D2 only). Land
     the end arm unfenced with the fence seam explicit, or do D3's v2→v3
     migration first — an operator-free call, but log it either way.

Also unbuilt and needed: a health-event writer, `homie.preflight_health`, and
`verbs.HomieEnd`'s body factored out of its own `inTx` so the seam can call it.
Keep aggregate mount no-drop acceptance open until the planner exists. Do not
load launchd.
