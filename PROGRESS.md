# PROGRESS — Mission Control implementation ledger

<!-- Header block: kept current by every session. -->
LAST GREEN SHA: (none yet — scaffold in progress)
PHASES PASSING: (none)
KNOWN-FAILING: (none)

## Phases

- [ ] Phase 0 — Architecture-kill spikes (S1–S8, handoff Part 2)
  - [ ] S1 setuid gate on named volumes
  - [ ] S2 warm-helper docker exec fidelity
  - [ ] S3 OAuth lifecycle in bind-mounted control dirs — **parked** (needs operator inputs, see Parked)
  - [ ] S4 egress gateway + CA trust — **parked** (needs proxy CA decision + binding auth, see Parked)
  - [ ] S5 SQLite WAL + crash discipline on named volume
  - [ ] S6 dispatch decision table as pure function
  - [ ] S7 launchd + sleep + clock drill (sleep sub-test needs operator presence)
  - [ ] S8 arm64 image build + Playwright smoke
- [ ] Phase 1 — Substrate + walking skeleton (fake harness built here)
- [ ] Phase 2 — Dispatch + domain correctness
- [ ] Phase 3 — Boundary conformance (Docker)
- [ ] Phase 4 — E2E control loops (six scenario families)
- [ ] Phase 5 — Real-subscription acceptance (operator-scheduled)

## Parked

- ~~Secrets in git history~~ **RESOLVED 2026-07-10**: operator explicitly
  accepts the values in local history; no scrub, no rotation. Noted in
  OPERATOR-INPUTS.md.
- **Private remote** (handoff §1.1): still recommended (disk-failure
  protection for long autonomous stretches). Operator: create an empty
  private GitHub repo, then `git remote add origin <url> && git push -u
  origin main`. Agents push after green commits once it exists.
- ~~Proxy CA pair~~ **RESOLVED 2026-07-10**: agent generated it —
  `~/.mc-dev-home/ca/ca.key` (0600, host-side only) + `ca.crt` (valid to
  2036). S4 unparked.
- **Credential materialization for S3/Phase 5** — two operator commands
  (the agent is not permitted to copy live login tokens itself):

      security find-generic-password -s "Claude Code-credentials" -w > ~/.mc-dev-home/cred/claude/.credentials.json && chmod 600 ~/.mc-dev-home/cred/claude/.credentials.json
      cp -p ~/.codex/auth.json ~/.mc-dev-home/cred/codex/auth.json && chmod 600 ~/.mc-dev-home/cred/codex/auth.json

  S3 runs against these copies, never against `~/.claude` / `~/.codex`.
  Caveat the operator accepts by running S3: an in-container token refresh
  may rotate a refresh token and could invalidate the host-side login
  (re-login is quick if so).
- ~~Token-spend authorization~~ **RESOLVED 2026-07-10**: all bindings
  subscription-based; unconstrained, Phase 5 + smoke pre-authorized.
- **Sacrificial Worksource standing directive** (handoff §4.1 row 7):
  `llm-council` path recorded, but no standing directive written. Needed
  before Strategist(propose) e2e tests and the smoke.
- **Docker Desktop settings snapshot** (handoff §4.1 row 4): not recorded
  (ECI state, Resource Saver, VM sizing, version pin). Agent will probe what
  it can in S1/S7 and record findings; operator confirms and freezes.
- **S7 sleep drill**: the 30-min Mac sleep mid-lease test needs the operator
  (an agent cannot sleep the machine it runs on). All other S7 sub-tests
  proceed.
- **Codex autonomy profile** (handoff §1.5): the agent is not permitted to
  write `approval_policy="never"` / `sandbox_mode="danger-full-access"`
  into `~/.codex/config.toml` (auto-mode classifier denial — correctly:
  self-configured unsafe agents need the operator's hand). Operator: append
  this block to `~/.codex/config.toml`, then the takeover smoke
  (`codex exec -p mc` + one `/goal` set/clear in the repo) can run:

      [features]
      goals = true

      [profiles.mc]
      approval_policy = "never"
      sandbox_mode = "danger-full-access"

      [projects."/Users/vinchenkov/Documents/dev/ai/homie"]
      trust_level = "trusted"

- **Claude Code permission posture** (handoff §1.4): the agent may not
  widen its own allowlist (`.claude/settings.json` write denied by the
  classifier). Operator: either run sessions in this folder with
  `claude --dangerously-skip-permissions` (handoff-recommended), or create
  `.claude/settings.json` yourself allowing `go/bun/docker/git/mise/sqlite3`.
- **db_schemas.sql missing** (handoff §1.1): not present in the seeded
  folder. Proceeding by deriving the schema from spec §4/§5 (spec wins
  anyway). Informational unless the operator has the file — if so, drop it
  in the repo root.
- **docs/priors/ POC evidence missing**: the `poc/` copies and original
  memory notes were not seeded (memory dir empty). The three §4.3 priors are
  reconstructed as one-line notes in `docs/priors/` marked RECONSTRUCTED.
  If original POC material exists, drop it into `docs/priors/`.

## Chronology

- 2026-07-10 — Session 1 (Claude Code). Found seed folder bare (specs at
  root, no scaffold) and OPERATOR-INPUTS.md tracked in git with live
  secrets. Untracked + gitignored it; parked the history question. Seeded
  the §1.1 scaffold: specs/, AGENTS.md, CLAUDE.md, PROGRESS.md,
  IMPLEMENTATION-NOTES.md, docs/adr/ slots, docs/priors/ (reconstructed),
  spikes/ stubs.

- 2026-07-10 — Toolchain pinned via tracked .mise.toml (Go 1.24.5, Bun
  1.3.9); mise installed. Phase 0 spike workflow launched: S1/S2/S5/S6/S8
  in parallel, then a serialized Docker-restart drill + S7 (launchd) —
  restart-dependent assertions serialized because they share the daemon.
- 2026-07-10 — ADR-001 (role-side verbs + verb-by-scope table, spec §18)
  authored and Accepted. One genuine spec ambiguity found and tracked:
  the Editor's wave plan-review has no defined dispatch stage (children
  are born seeded → Workers would dispatch immediately); routed to S6's
  ambiguity list, see ADR-001 Open Questions.

NEXT: Phase 0 — collect spike results (workflow in flight), fold RESULT.md
files, commit per spike, write fallback ADRs for any red. Then Phase 1.
