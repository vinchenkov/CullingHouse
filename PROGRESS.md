# PROGRESS — Mission Control implementation ledger

<!-- Header block: kept current by every session. -->
LAST GREEN SHA: (this commit)
PHASES PASSING: Phase 0 COMPLETE (S1–S8 all green, no fallback ADRs; only operator-leg deferrals remain); Phase 1 COMPLETE (1a substrate 155; 1b walking skeleton reviewed-and-fixed — fake-harness 43, agent-runner 13, runner/image 2, resident 31, dispatch + cmd/mc suites; Docker e2e PASS ×4 total)
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
- [~] Phase 2 — Dispatch + domain correctness
  - [x] Wave 1 unparked acceptance: dispatch table + SQL differential;
        domain aggregates; completion/fencing/two budgets; process flock +
        independent CAS; strict role/runner identity; immutable routing,
        directives, and claimed-state briefs; adversarial review closed
  - [!] Strategist wave CLI: isolated under Parked (durable holistic Editor
        plan-review representation is operator/spec input)
  - [ ] Wave 2 full §18 verb/error/scope surface
  - [ ] Split-brain kill-point convergence suite
  - [ ] Nightly randomized/metamorphic/lifecycle properties + planted mutants
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

- 2026-07-12 — **Stored Daily Console schedule is CLI-configurable.** `mc
  init` now accepts the all-or-none `--console-hour/--console-minute/
  --console-tz` triple, validates ranges and embedded IANA zones before
  creating the spine, persists the three lock-row tunables, and distinguishes
  explicit midnight from the hour-24 unset sentinel. Go fast lane green.

NEXT: Add the contract-promised `mc/verbs/dispatchverb_test.go` differential
suite: every dispatch Action kind through real SQL load/apply, nullable lock/
task fields, Daily Console activity loading, and re-enter mutation. Then the
process flock and routing.md resolution remain before wave-1 review closure.

- 2026-07-12 — **Phase 2 SQL↔dispatch adapter differential green.** New
  `mc/verbs/dispatchverb_test.go` mirrors hand-built projections into real
  spine rows and proves all five actions (idle/spawn/reap/land/reenter) agree
  with frozen `dispatch.Decide`; verifies claim/reap/reenter writes, effect-only
  landing, NULL last-heartbeat/task/subject/worksource handling, Editor pool
  snapshot, same-day Daily Console suppression, and invalid-zone rollback.

NEXT: Close the two omitted Phase 2 wave-1 obligations identified at takeover:
(1) process-level `mc.dispatch` flock on the spine volume with concurrent
loser/no-effect coverage (§10), then (2) parse authoritative routing.md,
validate role bindings/decorrelation, stamp the resolved binding, and refuse
unresolved routing before lease claim. Initiative wave remains Parked.

- 2026-07-12 — **Process-level dispatch flock green.** `verbs.Dispatch`
  resolves the file-backed main spine, takes `mc.dispatch.lock` beside it
  before reading any records, and holds it through the transaction/effect
  decision; BEGIN IMMEDIATE + lease CAS remain the durable correctness fence.
  Black-box subprocess test proves a dispatch cannot evaluate/open a Run while
  another process holds the flock, then resumes with one spawn on release;
  the existing four-claimant test remains exactly one spawn/three lease-held
  idles. Go fast lane green.

NEXT: Finish Phase 2 wave-1 routing: replace `binding='fake'` in applySpawn
with authoritative routing.md parsing/validation; unresolved roles or invalid
producer/judge decorrelation must fail before lease claim/effect, while the
test-only fake binding remains expressible through test config. Initiative
wave remains Parked. Then adversarially review wave 1 against its contract.

- 2026-07-12 — **Phase 2 wave-1 routing green.** ADR-007 pins one strict
  `$MC_HOME/routing.md` table and an injected canonical binding registry.
  Spawn validates every role/reference plus Strategist↔Editor and
  Worker↔Verifier decorrelation before claim; missing/invalid config opens no
  Run, while pending land/reap-class reconciliation remains usable. Resolved
  harness/model binding propagates through effect → run.json and the Run keeps
  `harness/binding`. Production excludes fake; only build-tagged CLI/fake-E2E
  binaries accept explicitly named fake routes. Canonical, failure, MC_HOME,
  build-tag fake, and resident propagation tests green; Docker-tag suite
  compiles without running Docker.

NEXT: Adversarially review Phase 2 wave 1 (contract + domain/verbs/dispatch
adapter/routing) before declaring the wave complete. Prioritize transaction
atomicity, initiative drain/plan-review boundaries, carried correction/refine
briefs and exception labels, all terminal caller/role/subject fences, and
route snapshot truth. Fix confirmed findings red-first. Initiative wave CLI
remains Parked pending the operator's durable plan-review representation.

- 2026-07-12 — **Phase 2 adversarial review: overlapping waves closed.** A
  new domain regression first proved that `BirthWave` could append another
  wave while the initiative still had open children. The aggregate now names
  the §6.1 strict-drain refusal before inserting anything; the full Go fast
  lane is green. (The substrate cannot distinguish successive inserts in one
  legitimate multi-child wave because a wave deliberately has no record;
  the sole-writer `mc` aggregate is the atomic boundary.)

NEXT: Continue confirmed Phase 2 wave-1 findings red-first: make reap derive
its subject from the fenced lease, then require a live packet and complete
landing fence for packet decisions/re-entry. Initiative wave CLI remains
Parked pending the operator's durable plan-review representation.

- 2026-07-12 — **Phase 2 adversarial review: reap authority closed.** A red
  regression proved a subject-carrying lease could be reaped without charging
  its subject because the write aggregate trusted an optional action payload.
  `ApplyReap` now fences and reads the authoritative subject from the live
  lock before touching the Run, making omitted/injected subject accounting
  impossible; subjectless runs still charge nothing. Full Go fast lane green.

NEXT: Require the task's live Review Packet at approve/revise/re-entry and
require complete SHA/target landing inputs before branch approval; prevent
packet birth/rerender after a decision. Then continue the remaining
adversarial findings. Initiative wave CLI remains Parked.

- 2026-07-12 — **Phase 2 adversarial review: review-surface fences closed.**
  Red domain regressions proved packaged rows could approve/re-enter without
  ever becoming reviewable, branch work could be irreversibly approved with
  missing SHA/target inputs, and decided packet renders could be rewritten.
  Domain plus substrate now require the live lifetime packet for approval and
  re-entry, require the complete landing fence before branch approval, reject
  packet birth/rerender after a decision, and keep decided renders immutable.
  The SQL↔dispatch landing fixture now constructs the legal packet-first
  order. Full Go fast lane green.

NEXT: Close the remaining initiative lattice findings: cancellation must
overwrite every open child's decision to cancelled, and a propagated parent
block cannot be manually cleared while a live child remains blocked. Then
pin verdict outcome/deepening/correction carriers red-first. Initiative wave
CLI remains Parked.

- 2026-07-12 — **Phase 2 adversarial review: initiative cancellation fixed.**
  A red landing-pending-child regression proved the parent archive cascade
  preserved an open child's earlier `approved` decision. The substrate now
  makes parent cancellation authoritative for every still-open child:
  decision/timestamp become `cancelled`, the child archives, then its packet
  archives. Domain and raw-SQL backstops agree; full Go fast lane green.

NEXT: Prevent manual unblocking of a parent initiative while any live child
remains blocked, with matching aggregate and substrate regressions. Then pin
verdict outcome/deepening/correction carriers red-first. Initiative wave CLI
remains Parked.

- 2026-07-12 — **Phase 2 adversarial review: maximally strict block restored.**
  A red regression proved `task unblock` could clear the propagated parent
  flag while its child stayed blocked. Domain and substrate now refuse a
  parent unblock while any live blocked child exists; resolving the last
  child still drives the existing auto-clear path. Full Go fast lane green.

NEXT: Pin Verifier outcome carriers: PASS refinement is genuine,
BUDGET-SPENT refinement is churn, correction belongs only to CORRECT, and
required evidence/SHA must be enforced in the domain transaction. Then close
strict batch parsing and Strategist mode fences. Initiative wave CLI remains
Parked.

- 2026-07-12 — **Phase 2 adversarial review: Verifier carrier matrix fixed.**
  Red domain/CLI regressions proved a PASS refinement could increment churn,
  PASS/BUDGET-SPENT could persist correction files, CORRECT could carry a
  meaningless landing SHA, and direct aggregate callers could omit evidence,
  SHA, or the matching Verifier Run. Outcome-specific validation now exists
  in the real state-law layer and the CLI rejects impossible flag shapes:
  PASS→genuine, BUDGET-SPENT→churn, correction only on CORRECT, SHA only on
  the two verified outcomes. The verdict carrier must update exactly the
  matching Verifier Run/subject. Complete fast lane green: Go plus fake 43,
  agent-runner 12, runner/image 2, resident 30.

NEXT: Close strict single-document batch parsing and exact Strategist mode /
subject-shape fencing red-first, then fix invalid-console-TZ reap precedence
and blocked-proposal Editor visibility. Initiative wave CLI remains Parked.

- 2026-07-12 — **Phase 2 adversarial review: batch parsing is truly full.**
  Red Editor and Strategist process tests proved a valid first JSON document
  could commit while trailing JSON or unknown fields were ignored. All three
  batch terminals now share one strict single-document decoder: unknown keys
  and any second value/garbage fail before the transaction. Regressions assert
  pool/tasks and lease remain untouched. Full Go fast lane green.

NEXT: Add exact Strategist mode plus subject-shape fences for propose and the
implemented-but-parked initiative terminal. Then fix invalid-console-TZ reap
precedence and blocked-proposal Editor visibility. Initiative wave CLI remains
Parked pending durable plan-review representation.

- 2026-07-12 — **Phase 2 adversarial review: Strategist capabilities split.**
  Red process tests proved initiative/console envelopes could terminate a
  propose run and that propose accepted a subject-carrying lease. Exact
  run.json role modes now fence propose vs initiative; propose additionally
  requires the fenced lease to be subjectless, wave requires an initiative
  mode, and initiative done-declaration accepts only that same exact mode.
  Complete fast lane green (Go + all four Bun suites).

NEXT: Restore step-(0) precedence so a corrupt Console timezone cannot wedge
a stale lease, then exclude/refuse blocked proposals at the Editor snapshot
and decision boundaries. Initiative wave CLI remains Parked pending durable
plan-review representation.

- 2026-07-12 — **Phase 2 adversarial review: stale leases outrank Console.**
  A red SQL-adapter regression proved an invalid stored IANA zone aborted
  before step (0), indefinitely wedging a stale global lease. A held lease
  now executes only keep/reap logic with a non-semantic clock placeholder;
  Console timezone validation remains fail-closed once the lease is free.
  The stale subject is charged and the lease releases despite corrupt Console
  config. Complete fast lane green.

NEXT: Exclude blocked proposals from Editor pool snapshots and refuse stale
Editor verdicts against a concurrently blocked proposal at the aggregate
boundary. Then address saturated operator-directed recovery and runtime route
truth. Initiative wave CLI remains Parked.

- 2026-07-12 — **Phase 2 adversarial review: blocked proposals made invisible.**
  Red pure-dispatch and aggregate/backstop tests proved a blocked proposal
  entered the Editor's exact-coverage snapshot and could still be promoted or
  rejected. Pool construction now excludes it, while domain and substrate
  reject stale Editor writes until operator unblock. Complete fast lane green.

NEXT: Restore the valid saturated → operator-revise → genuine-deepening
recovery while keeping saturated packets out of automatic refinement. Then
make the fake-only resident fail closed for canonical runtime routes and begin
the missing immutable spawn-brief carriers. Initiative wave CLI remains Parked.

- 2026-07-12 — **Phase 2 adversarial review: operator saturation recovery fixed.**
  A red end-to-end aggregate regression proved saturation was permanent even
  after operator revise. The packet lattice now allows a streak decrease only
  while its live owning task is at `worked` in the operator-requested recovery
  round; the substrate recomputes `saturated=0` below threshold. Packaged
  saturated packets still reject direct resets and remain excluded from
  automatic step-(2b) selection. Complete fast lane green.

NEXT: Make the current fake-only resident/runner fail closed on canonical
runtime routes so recorded binding can never lie about executed adapter. Then
assemble immutable spawn briefs carrying Editor records, Strategist dedupe,
refine/correction notes, and Packager exception evidence. Initiative wave CLI
remains Parked.

- 2026-07-12 — **Phase 2 adversarial review: runtime route truth restored.**
  Red resident and real-runner process tests proved a canonical
  `codex/chatgpt` envelope still invoked the fake harness. Until the later
  real-adapter phase lands, both layers now refuse non-`fake/fake` routes
  before creating launch bytes or invoking the fake CLI. No canonical Run can
  claim one adapter and execute another. Complete fast lane green; counts are
  now fake 43, agent-runner 13, runner/image 2, resident 31.

NEXT: Assemble immutable spawn briefs in `mc` from the claimed state snapshot
and copy them unchanged into run.json: Editor proposal records, Strategist
dedupe titles, Worker refine/correction input, and Packager budget-spent
exception/evidence. Initiative wave CLI remains Parked.

- 2026-07-12 — **Phase 2 wave-1 immutable brief carriers green.** ADR-008
  pins `mc.spawn-brief.v1`: `mc dispatch` now renders role input inside the
  same transaction that claims the lease, and the resident copies it
  byte-for-byte into run.json. Regressions cover full Editor proposal records,
  Strategist rejected-title dedupe, Worker refinement notes/latest correction,
  Packager BUDGET-SPENT exception/evidence, and Console queue/blocked state.
  The generic skeleton prompt is gone. Complete fast lane green.

NEXT: Continue wave-1 adversarial closure with strict `mc complete` arm/field
validation and real CLI zombie/new-holder coverage, then authenticate and make
immutable the runner lifecycle verbs. Keep initiative wave CLI Parked pending
durable plan-review representation; frozen prose role directives remain a
required authored artifact after the carrier schema.

- 2026-07-12 — **Phase 2 adversarial review: `mc complete` payload truth fixed.**
  Process regressions now reject every cross-arm field instead of silently
  dropping it (`branch`, `reason`, `outputs`), require Packager render paths,
  and require Strategist(initiative)'s completion report. `--outputs` is no
  longer inert: the terminal transaction stores `runs.output_path`, and the
  next role's ADR-008 brief carries the latest report/artifact reference.
  Complete fast lane green.

NEXT: Bind heartbeat and session registration to the runner's own run.json,
make locator registration idempotent-but-immutable, and add old CLI complete /
heartbeat versus new-holder snapshots. Then close atomic rollback/CAS coverage
gaps. Initiative wave CLI remains Parked; frozen prose directives remain due.

- 2026-07-12 — **Phase 2 adversarial review: runner lifecycle authority fixed.**
  Heartbeat and register-session now require the caller's own pipeline
  run.json before touching state; heartbeat remains live-lease fenced while
  locator registration remains legal after own-run lease release. Same-value
  registration retries are idempotent, conflicting replacements fail in both
  verb and substrate, and locator nullability travels as a pair. A real CLI
  reap/reclaim test proves old complete, old heartbeat, and an old container
  supplying the new token leave the new lease bit-for-bit unchanged. Complete
  fast lane green.

NEXT: Add true aggregate-level concurrent CAS coverage (separate from the
process flock), plus Editor/Strategist/Packager transactional rollback cases.
Then reconcile remaining wave-1 items and author the frozen prose directives.
Initiative wave CLI remains Parked pending durable plan-review representation.

- 2026-07-12 — **Phase 2 adversarial acceptance gaps closed.** Four concurrent
  BEGIN-IMMEDIATE claimants now prove the domain CAS independently of the
  process flock (one Run winner, three coded losers). Real CLI regressions
  prove Strategist valid-first/DB-invalid-second, Editor reject-first/
  blocked-second, and Packager stage-before-WIP-cap all roll back task,
  activity, Run, packet, and lease writes. Packet cancel now uses a distinct
  aggregate requiring the live Review Packet, while generic initiative/child
  cancellation remains available to the state machine. Complete fast lane
  green.

NEXT: Author and embed the frozen role directives so ADR-008 briefs carry
both immutable state and product-owned instructions (including orchestration-
by-default). Then perform a final wave-1 contract diff and advance only the
unparked Phase 2 lines. Initiative wave CLI remains Parked.

- 2026-07-12 — **Required authored artifact: frozen directives/briefs green.**
  Eight tracked role directives are embedded in the `mc` binary and carried
  inside every `mc.spawn-brief.v1` document. Each pins role jurisdiction,
  orchestration-by-default with read-only depth-1 subagents, the inline-work
  exception, and the single terminal action; role-specific rubrics cover
  contrastive Editor judgment, Verifier gates, correction/refinement,
  exception-labeled packaging, initiative boundaries, and Console ranking.
  Tests prove every dispatch role has a distinct directive and unknown roles
  fail closed. This satisfies the spec §16.1/Inv.20 authored role-directive
  and brief-template deliverable. Complete fast lane green.

NEXT: Perform the final Phase 2 wave-1 contract/spec diff, record the parked
and wave-2 deferrals precisely, and advance the ledger only for acceptance
sets that are actually green. Initiative wave CLI remains Parked pending the
durable holistic plan-review representation.

- 2026-07-12 — **Phase 2 wave-1 final contract/spec diff complete.** Every
  unparked wave-1 acceptance set is green: dispatch's taken/not-taken and
  invisibility table, SQL adapter differential, all six domain aggregate
  suites, completion/fencing/two-budget ownership, strict batch/role/lifecycle
  boundaries, atomic rollbacks, CAS+flock, route truth, and immutable
  directives/briefs. Docker-tag and `test_fake_routing` builds also compile.
  The spec-over-contract fixes that crossed wave-1 directory/schema fences
  are recorded in IMPLEMENTATION-NOTES/ADRs. The only omitted wave-1 terminal
  is `mc strategist wave`, explicitly Parked because implementing the ADR as
  written would violate the spec's mandatory holistic Editor review.

  Phase 2 is not complete: handoff Part 3 still requires (1) the full §18
  happy/error/scope surface (`console`, worksource/initiative/operator,
  doctor/backup/reset/onboard, Homie/outbox, structured error JSON), (2) the
  split-brain kill-point convergence suite, and (3) nightly dispatch/
  metamorphic/lifecycle properties with generator-honesty/planted-mutant
  gates. These are Wave 2, not silently counted green.

NEXT: Author the Phase 2 wave-2 acceptance contract from handoff Part 3 and
spec §18, preserving the parked initiative-wave line. Then TDD the full CLI
verb/error/scope matrix before split-brain and nightly suites.

- 2026-07-12 — **Phase 2 wave-2 contract authored.** The contract orders
  provenance before new surface area, pins structured error JSON, enumerates
  core/operator, Console/Homie/outbox, and operational verb acceptance,
  defines the deterministic split-brain boundary table, and names the nightly
  generator-honesty/planted-mutant gates. It explicitly excludes rather than
  bypasses the parked initiative-wave terminal.

NEXT: TDD wave 2 slice 1: enforce ADR-001 D6 provenance on every existing
verb before it opens/mutates the spine, with pipeline attempts at init,
dispatch, task add, packet decide, task unblock, and land report proven
bit-for-bit inert. Then add structured error JSON.

- 2026-07-12 — **Phase 2 wave 2 provenance slice 1 green.** Existing
  host/operator writes now load identity and reject pipeline callers before
  opening or mutating state: init, dispatch, task add, packet decide, task
  unblock, and land report. Homie admission is explicit and requires the
  verb in the immutable run.json allowlist; host remains the no-run.json
  scope. The process matrix proves no forged task/decision/landing/init bytes
  and an unchanged lease. Complete fast lane green.

NEXT: Add the wave-2 structured error envelope while preserving exit codes,
stderr diagnostics, and self-delegation byte fidelity. Then extend the scope
matrix as new operator/Homie/transport verbs land. Runner-private vs model
capability enforcement remains a Phase 3 structural-boundary mechanism.

- 2026-07-12 — **Phase 2 wave 2 structured errors green.** Every local
  rejection now emits one stdout JSON object with stable `error.code` and
  `error.message` while retaining the stderr diagnostic and exit 1/2 split.
  Coded domain errors preserve their slug, usage/environment errors use
  `usage`, and uncoded CLI-domain refusals use stable `domain-rejection`.
  Success JSON and delegated byte/exit passthrough remain unchanged. Complete
  fast lane green.

NEXT: TDD `mc initiative add`, `mc worksource add|list|pause|archive`, and
`mc task interrupt` with the new error/scope matrix. Preserve the parked
initiative-wave terminal; these operator record verbs do not depend on it.

- 2026-07-12 — **Phase 2 wave 2 `initiative add` green.** Host or allowlisted
  Homie intent now files an `origin:user`, `scope=initiative` proposal into
  the ordinary contrastive pool; title, Worksource, and a checkable `--charter`
  are mandatory and expedited priority -1 is preserved. Pipeline provenance
  is denied before insertion. This adds no wave state and does not touch the
  parked holistic plan-review question. Complete fast lane green.

NEXT: TDD `mc worksource add|list|pause|archive`, including active-only
dispatch visibility and immutable historical rows. Then implement operator
interrupt as cancel+lease-clear+exact stop effect.
