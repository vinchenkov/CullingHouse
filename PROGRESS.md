# PROGRESS — Mission Control implementation ledger

<!-- Header block: kept current by every session. -->
LAST GREEN SHA: (this commit)
PHASES PASSING: Phase 0 COMPLETE (S1–S8 all green, no fallback ADRs; only operator-leg deferrals remain); Phase 1 COMPLETE (1a substrate 155; 1b walking skeleton reviewed-and-fixed — fake-harness 43, agent-runner 11, resident 29, dispatch + cmd/mc suites; Docker e2e PASS ×4 total)
KNOWN-FAILING: (none)
FAST SUITE: mc/check.sh (gofmt+vet+go test ./... — includes substrate + promoted dispatch) + runner/fake-harness/check.sh + runner/agent-runner/check.sh + runner/image/check.sh + resident/check.sh. Docker e2e (phase-completion lane): cd mc && mise exec -- go test -tags docker_e2e -timeout 15m ./e2e/...

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
- [ ] Phase 2 — Dispatch + domain correctness
- [ ] Phase 3 — Boundary conformance (Docker)
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
- ~~Credential materialization~~ **RESOLVED 2026-07-10 21:15**: operator ran
  both commands; `~/.mc-dev-home/cred/{claude/.credentials.json,codex/auth.json}`
  exist, 0600. Rotation-may-invalidate-host-login caveat stands, accepted.
- ~~Token-spend authorization~~ **RESOLVED 2026-07-10**: all bindings
  subscription-based; unconstrained, Phase 5 + smoke pre-authorized.
- **Sacrificial Worksource standing directive** (handoff §4.1 row 7):
  `llm-council` path recorded, but no standing directive written. Needed
  before Strategist(propose) e2e tests and the smoke.
- **Docker Desktop settings snapshot** (handoff §4.1 row 4): not recorded
  (ECI state, Resource Saver, VM sizing, version pin). Agent will probe what
  it can in S1/S7 and record findings; operator confirms and freezes.
- **S7 sleep drill**: the 30-min Mac sleep mid-lease test needs the operator
  (an agent cannot sleep the machine it runs on). Instructions in
  spikes/07-launchd-clock/RESULT.md. All other S7 sub-tests passed.
- ~~Docker Desktop settings~~ **RESOLVED 2026-07-10 21:15**: operator
  flipped both — verified `UseResourceSaver: False`, `AutoStart: True`.
- ~~Sacrificial Worksource standing directive~~ **RESOLVED 2026-07-10**:
  agent-authored at operator request; recorded in OPERATOR-INPUTS.md
  (hardening/tests directive with command-checkable criteria).
- ~~Private remote~~ **RESOLVED**: origin pushed and in sync.
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
- **Initiative wave holistic Editor review** (spec §3/§6.1, ADR-001 open
  question 1, phase2-contract A-P2-7): children are required to be born
  `seeded`, but the current dispatch table would send them straight to Worker
  with no state/slot for the Editor's mandatory holistic plan review. Decision
  request: choose a durable representation/dispatch step for reviewed-vs-
  unreviewed wave children. This blocks only the `strategist wave` CLI line;
  other Phase 2 work continues.

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

- 2026-07-10 — Operator created private remote (github.com:vinchenkov/
  CullingHouse); GitHub push protection caught the Discord token in the two
  seed commits. History rewritten with `git filter-repo --invert-paths
  --path OPERATOR-INPUTS.md` — **all SHAs changed** (tip now `887e30e`);
  backup bundle in session scratchpad. Any kickoff sentence must use the
  new SHAs. Push pending (operator runs it).

- 2026-07-10 — Phase 0 workflow completed: 6 agents, 0 errors. Every
  runnable assertion across S1/S2/S5/S6/S7/S8 PASSED through two hard
  Docker Desktop restarts; **no fallback ADRs needed**. Drill findings for
  the resident's watchdog: (1) macOS TCC blocks launchd payloads under
  ~/Documents — runtime bits must live in ~/Library; (2) `docker info
  --format` exits 0 while printing 500s during daemon transitions —
  liveness must validate output shape, not exit code; (3) Docker Desktop
  4.70.0 quit can wedge 10+ min — watchdog must confirm com.docker.backend
  exit before relaunch. All spikes committed. S3/S4 remain: credential
  files not yet materialized by operator; CA pair ready.

- 2026-07-10 ~22:10 — **Phase 1a substrate GREEN and committed (771480e)**:
  schema.sql + trigger lattice + 155-case backstop suite. Adversarial
  review (3 lenses) found 13 real defects, all fixed — see the workflow
  summary; NOTES.md carries 20 NOTE(P1.n) decisions. Independent verify:
  gofmt/vet clean, suite green in 0.9s.
- 2026-07-10 ~21:35 — **Harness login expiry mid-run** killed both S3/S4
  spike agents ("Login expired"); timing coincides with S3 forcing a claude
  token refresh against the credential copy — plausibly the accepted
  rotation-invalidates-host-login caveat firing (not confirmed; could be
  ordinary expiry). Operator re-ran /login; S3/S4 workflow relaunched.
  Note for the future: expect host /login re-auth after S3-style refresh
  probes; Phase 5's refresh canary should schedule around it.
- 2026-07-10 ~19:20 — **QUOTA: Claude Code session limit hit** (resets
  20:50 America/Los_Angeles). The Phase 1a substrate-author subagent died
  mid-spec-reading; no files written, tree clean — this IS the green
  boundary. Handing off per handoff §1.4/§1.5. Phase 1a workflow can be
  re-launched fresh (nothing to resume; the failed run wrote nothing).

- 2026-07-11 — Session start (Claude Code): found S3/S4 results landed
  untracked from the relaunched workflow — **both GREEN, Phase 0 complete,
  zero fallback ADRs across all eight spikes**. Also found the first two
  fake-harness files (runner/fake-harness/{cli,behavior}.ts) written but
  uncommitted (no README/tests yet — Phase 1b work-in-progress, kept).
  Committed S3/S4 (out/ evidence dirs stay untracked per spike convention,
  now gitignored). Operator notes ledgered: S3 finding 5 (canonical codex
  refresh token may be consumed; recovery copy at
  ~/.mc-dev-home/spike03/race-codex/auth.json); DD-restart-mid-refresh legs
  deferred to the serialized drill in Phase 3.

- 2026-07-11/12 — **Phase 1b walking skeleton GREEN end-to-end** (workflow:
  contract + 3 parallel builders + integrator, 19 agents). The 11-step
  ladder passes through the real mc binary (self-delegating into a warm
  helper container per §11.5), real resident (Bun, 500ms timer), real
  container topology (spine on a named volume, mc-fake-e2e image,
  --network none), and the fake harness: proposed → Editor → seeded →
  Worker (heartbeat advances, branch+commit) → worked → Verifier →
  verified → Packager → packaged+packet → approve (pure write, main
  unmoved) → tick → mc-land → landed (--no-ff merge of verified_sha,
  worktree/branch gone, cascade archive, lock free). PASS ×2, no leaks.
  22 deviation entries appended to IMPLEMENTATION-NOTES.md (notable: two
  additive schema columns lock.hard_deadline_minutes + runs.pool_snapshot;
  skeleton-only `mc init`; MC_RUN_JSON test override — flagged by review).
- 2026-07-12 — **QUOTA: session limit hit mid-review** (reset 20:40 PT
  07-11): the correctness lens, 8 of 10 verifiers, and the fixer died.
  Two lenses survived with 10 findings (2 major: run.json materialized
  inside the RW-mounted session dir → forgeable identity + Inv. 26
  violation; mc-land `git checkout` switches the operator's checked-out
  branch, §6.2 violation). 1 finding confirmed (contract §8 still claims
  "no schema changes" — false). Findings saved to session scratchpad;
  committed the green boundary per AGENTS.md §8, review closure relaunched.

- 2026-07-12 — **Phase 1 COMPLETE.** Review-closure workflow (14 agents,
  0 errors): fresh correctness lens found 4 new findings (2 major);
  12 total adversarially verified → 9 confirmed, all FIXED, 4 refuted
  (MC_RUN_JSON override = already-logged P3 deferral; same-tx overclaim =
  Phase 2 split-brain territory, code satisfies invariants now; resident
  exec timeouts and land-report idempotency = deferred with reasons in the
  workflow record). Fixes: run.json now at MC_HOME/runs/<run_id>.json
  mounted RO (no RW alias, reap removes it; session folders assert
  trace-only), mc-land gained a HEAD fence (no checkout) + merge --abort
  on conflict, register-session fences on own-row identity instead of the
  live lease (race with the terminal verb eliminated, regression test
  added), contract §8 schema pin corrected, role outputs moved out of
  session folders, ladder-9 approve assertions de-flaked, runner process
  behaviors now tested as a real subprocess (exit-code passthrough,
  register-once, heartbeat-nonfatal). Suites after fixes: fast lane green
  (43/11/29 bun + full go), Docker e2e PASS ×2 with rebuilt image. Two
  deviation entries appended (run.json relocation; container-naming
  log-and-go).

NEXT: Phase 2 — dispatch + domain correctness (handoff Part 3, largest
investment). Order: (1) Phase 2 contract (like docs/phase1b-contract.md —
pin the domain-aggregate layer's shape, where it sits between verbs and
substrate, the §18 full verb/error surface, split-brain kill-point
harness design, property-suite scope); (2) build in waves: domain
aggregates + completion/fencing first, then decision-table + CLI error
paths, then split-brain suite, then nightly property suites; adversarial
review per wave. Reuse the Phase 1b workflow shape (contract → parallel
builders → integrate → review → verify → fix).

Kickoff (next session, either harness): "Continue the Mission Control
implementation from commit `<current main tip>`, phase `P-2`. Follow the
session protocol in AGENTS.md; read PROGRESS.md; do not invent scope; stop
rather than guess missing operator inputs."

- 2026-07-12 — Codex takeover from quota-interrupted Claude Code session.
  Resume ritual found commit 2be0e47 clean and pushed but **mid-red and
  unledgered**: it contains the Phase 2 wave-1 contract, domain aggregates,
  dispatch gap tests, schema additions, and verb rewiring (+4358/-304), while
  `cmd/mc` and its CLI tests remain on the Phase 1 signatures/deferred arms.
  Fast-lane truth at takeover: domain/dispatch/substrate focused Go suites
  green; fake-harness 43, agent-runner 11, resident 29 green; full Go lane
  compile-fails at the stale VerifierVerdict call above. Cross-harness
  adversarial takeover review launched before any code edit, per AGENTS.md
  §2; uncommitted work was absent and nothing was discarded.

NEXT: Complete the cross-harness audit of 2be0e47, record confirmed findings
in IMPLEMENTATION-NOTES.md, then restore the CLI boundary with the smallest
TDD integration slice (VerifierVerdict args first) and re-run the full fast
lane before advancing any Phase 2 wave marker.

- 2026-07-12 — **Cross-harness takeover review closure, four Phase 1 defects
  fixed red-first.** (1) Every role terminal now binds `--run` to the caller's
  immutable run.json identity before checking the live lease; the old
  same-role/new-holder attack leaves task and lease untouched. (2) the agent
  runner awaits native-session locator registration even when the harness
  exits immediately (12 runner tests green). (3) packet archive is now a
  one-way consequence of owning-task archive; direct slot freeing/resurrection
  aborts at the substrate, including initiative cascades. (4) mc-land
  preflights dirty/locked worktrees before main moves and treats unexpected
  post-merge cleanup residue as logged debt, never a false landing failure (2
  new Docker-free mc-land tests; resident 30 green). Focused Go suites green.
  The full Go lane now reaches only the expected stale Phase 2 CLI assertions
  listed in KNOWN-FAILING.

NEXT: Resume Phase 2 wave-1 integration red-first: replace the stale packet,
Editor, and Verifier deferred-arm CLI tests with real happy/error-path tests,
wire the already-built verb APIs through cmd/mc, and restore the full fast
suite before taking the next Phase 2 seam.

- 2026-07-12 — **Green recovery boundary after Codex takeover.** Phase 2 CLI
  now drives packet revise/cancel, mixed Editor promote/reject, and Verifier
  PASS/CORRECT/BUDGET-SPENT plus refinement-deepening validation through the
  real binary; old deferred tests replaced with isolated happy/error paths.
  The Verifier API compile break is closed. Complete expanded fast lane green:
  Go all packages; fake-harness 43; agent-runner 12; mc-land 2; resident 30.
  Phase 2 storage/verb interpretations missing from 2be0e47 are now logged;
  the initiative-wave plan-review hole is parked and only that line is held.

NEXT: Continue Phase 2 wave 1 at the next independent red-green slice:
`mc complete` seeded/needs-operator/infra + task block/unblock (keep
`--correction-count` rejected), then console init tunables and the promised
verbs/dispatch loader differential suite. Before wave-1 closure, add the
spec-required process flock and replace hardcoded fake binding with validated
routing.md resolution. Do not wire `strategist wave` until its Parked decision.

- 2026-07-12 — **Phase 2 CLI terminals green.** Red-first real-binary tests
  now cover host block/unblock, pipeline own-subject-only block and denied
  unblock; `mc complete --needs-operator` (status preserved, blocked reason,
  run outcome, lease release, no dispatch); `--infra` (dispatch budget only);
  and Refiner `--status seeded` at queue cap (same task re-enters, packet slot
  stays live). `--correction-count` remains an explicit rejection owned by
  Verifier verdict. Complete expanded fast lane green (Go + 43/12/2/30 Bun).

NEXT: Phase 2 wave-1 adapter correctness: add `mc init` console schedule
tunables and the contract-promised `mc/verbs/dispatchverb_test.go`
differential suite (real SQL projection/action application versus pure
dispatch). Then implement the §10 process-level dispatch flock and validated
routing.md binding resolution. Initiative wave remains isolated under Parked.
