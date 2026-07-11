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

- **Secrets in git history.** `OPERATOR-INPUTS.md` (MiniMax key, Discord bot
  token) was committed in the two seed commits (`13187fa`, `d24e2c9`). It is
  now untracked + gitignored, but the values remain in local history. A
  history rewrite was declined mid-session. Decision needed: (a) approve a
  history scrub (backup bundle exists in session scratchpad), or (b) rotate
  the MiniMax key and Discord bot token. **No private remote will be created
  or pushed until one of these happens.**
- **Private remote** (handoff §1.1): no `gh` CLI / credentials available to
  create it. Operator: create a private repo and `git remote add origin …`
  — after the secrets-in-history item is resolved.
- **Proxy CA pair** (handoff §4.1 row 9): not provided. Agent can generate
  one via openssl (private key host-side only) unless the operator prefers
  to supply it — confirm, then S4 unparks.
- **Claude auth posture for the `claude` binding** (handoff §4.1 row 2):
  host `claude` is keychain-authenticated; no container-side credential dir
  or `setup-token` was minted. Needed for S3/Phase 5.
- **Token-spend authorization** (handoff §4.1 row 8): no budget sentence in
  `OPERATOR-INPUTS.md`. Phase 5 live turns will not run until stated.
- **Sacrificial Worksource standing directive** (handoff §4.1 row 7):
  `llm-council` path recorded, but no standing directive written. Needed
  before Strategist(propose) e2e tests and the smoke.
- **Docker Desktop settings snapshot** (handoff §4.1 row 4): not recorded
  (ECI state, Resource Saver, VM sizing, version pin). Agent will probe what
  it can in S1/S7 and record findings; operator confirms and freezes.
- **S7 sleep drill**: the 30-min Mac sleep mid-lease test needs the operator
  (an agent cannot sleep the machine it runs on). All other S7 sub-tests
  proceed.
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
