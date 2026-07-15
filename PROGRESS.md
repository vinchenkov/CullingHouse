# PROGRESS — Mission Control implementation ledger

<!-- Header block: kept current by every session. -->
LAST GREEN SHA: e01a2af (local; push blocked by an operator deny rule — see Parked)
PHASES PASSING: Phase 0 COMPLETE (S1–S8 all green, no fallback ADRs; only operator-leg deferrals remain); Phase 1 COMPLETE (1a substrate 172; 1b walking skeleton reviewed-and-fixed — fake-harness 43, agent-runner 13, runner/image 40, resident 42, dispatch + cmd/mc suites; Docker e2e PASS ×4 total); Phase 2 COMPLETE for every unparked acceptance line (domain/§18 surface, deterministic split-brain convergence, bounded honesty + five mutants, tagged dispatch/metamorphic/twin-spine lifecycle properties; initiative-wave CLI remains explicitly isolated under Parked)
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
- [x] Phase 2 — Dispatch + domain correctness (all unparked acceptance)
  - [x] Wave 1 unparked acceptance: dispatch table + SQL differential;
        domain aggregates; completion/fencing/two budgets; process flock +
        independent CAS; strict role/runner identity; immutable routing,
        directives, and claimed-state briefs; adversarial review closed
  - [!] Strategist wave CLI: isolated under Parked (durable holistic Editor
        plan-review representation is operator/spec input)
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
  - [ ] Protected set + cross-Worksource jurisdiction (Dec. 3 step 5, Dec. 5)
  - [ ] macOS ACL leg of the trust seam (needs the native ACL API)
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

- ~~Push to origin is blocked by an operator deny rule~~ **RESOLVED
  2026-07-14**: the operator pushes manually; the `~/.claude/settings.json`
  deny stays. It cannot be overridden per-repo (user-level deny beats
  project-level allow; hooks and `bypassPermissions` do not clear it).
  **Agents: keep committing at every green micro-step per AGENTS.md §4, do
  not attempt the push, and do not route around the rule.** AGENTS.md §4's
  "push if a remote exists" is superseded for this repo and should be reworded
  at Release prep.

- **Initiative wave holistic Editor review** — **DECIDED 2026-07-14**: the
  operator accepts reading (i), a review gate between wave birth and first
  child dispatch. See the 2026-07-14 session-end NEXT item 3 for the pinned
  shape; ADR-020 is owed. The old decision request below is closed.

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
  other Phase 2 work continues. **2026-07-13 addendum (takeover review):**
  until this resolves, a promoted `initiative add` proposal dead-ends —
  Strategist(initiative) can only declare done with zero children (strict
  drain passes trivially) or block out. The hole itself stays sealed (`wave`
  is not CLI-wired), but avoid filing initiatives until decided; see
  IMPLEMENTATION-NOTES 2026-07-13.

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

- 2026-07-12 — **Phase 2 wave 2 Worksource lifecycle green.** Add validates
  kind/seeding mode and any sandbox-profile reference; list remains open to
  pipeline reads; pause/archive are operator or allowlisted-Homie writes and
  pipeline attempts are inert. SQL dispatch now carries authoritative
  Worksource status and excludes paused/archived tasks from landing,
  refinement, with-room selection, and Editor pool snapshots. Archive is
  terminal and Worksource rows cannot be deleted. Complete fast lane green.

NEXT: Implement `mc task interrupt` as one operator transaction: cancel the
live task with reason `operator_interrupt`, end the matching Run, clear only
its lease, and return the exact container-stop effect. Add stale/non-live and
pipeline-provenance negatives, then resident effect coverage.

- 2026-07-12 — **Phase 2 wave 2 operator interrupt green.** `task interrupt`
  now requires host or allowlisted-Homie provenance and an exact live lease
  subject. One transaction cancels/archives the task with
  `operator_interrupt`, ends the matching Run as `interrupted`, and clears
  only that lease; wrong-subject, replay, and pipeline attempts are inert.
  The returned stop effect names the exact container, and the resident stops
  it and removes the ephemeral launch envelope. Complete fast lane green;
  resident suite now 32.

NEXT: TDD Console publication over existing activity/outbox tables: exact
Strategist(console), subjectless own-run fence, content path required,
same-day event + destination rows + Run end/lease release atomically. Keep
the broader Homie/outbox transport surface as the following slice.

- 2026-07-12 — **Phase 2 wave 2 Console publication green.** The exact
  Strategist(console) terminal now requires its own subjectless run and a
  normalized `outputs/` content path. One transaction writes the
  `daily.briefing` suppression event, fans a small path payload to the trusted
  Console destinations, ends the Run, and releases the lease. The always-on
  dashboard is the Phase-2 destination; Phase 5's deferred `config.toml`
  layer expands that private resolver to every configured surface. Wrong
  mode, host scope, caller-run mismatch, subject-carrying lease, traversal,
  and missing content are inert; an injected outbox abort proves the event,
  terminal, and lease roll back together. Complete fast lane green.

NEXT: TDD the first Homie registry slice: `mc homie start|bind|list` with
host/allowlisted-Homie provenance, immutable session identity/locators,
active-binding uniqueness, and no pipeline lease. Return only durable
state/effect data; conversation send/history and runner transport follow.

- 2026-07-12 — **Phase 2 Homie registry substrate backstops green.** The
  takeover's three independent read-only audits found that Phase 1 allowed
  two active sessions to own one surface place. Active ownership is now
  globally unique by `(surface, channel_ref)`; bind-event identity is frozen,
  inactive history cannot reactivate, and end/reap deactivates bindings.
  Homie start provenance is non-null and immutable, while native session
  handle + trace filename register as a paired set-once locator. Raw-SQL
  negatives cover every guard; complete fast lane green. Substrate suite now
  171.

NEXT: TDD `mc homie start|bind|list`. Per ADR-001 D6, start/bind are strictly
host scope; an allowlisted Homie may list only its own active session, while
host list includes active/ended/reaped rows. Start atomically writes registry
and its initial binding after trusted Homie route resolution and never touches the
pipeline lease, Runs, outbox, file plane, or resident effects.

- 2026-07-12 — **Phase 2 Homie start/bind/list green.** ADR-009 pins the
  previously unspecified CLI and record contract. Host-only start resolves
  the trusted Homie route, mints a disjoint `h-` identity, freezes the
  canonical agent-verb allowlist/path/container/runtime binding, and writes
  the registry row plus initial surface binding atomically. It returns no
  launch effect and leaves a simultaneously live pipeline lease, Runs,
  outbox, and file plane untouched. Bind retries are idempotent for the same
  session/place and never transfer an occupied place. Host list includes all
  resumable statuses; an allowlisted Homie sees only its own active row.
  Every Homie-authorized operator mutation now rechecks the canonical active
  registry row and exact frozen allowlist inside its write transaction, so an
  ended zombie or forged envelope is inert; this also enables the missing
  Homie `task block` arm from ADR-001 D6. Invalid route/input/scope, binding
  collision, duplicate-origin start, and injected initial-binding failure all
  leave no partial registry state. Complete fast lane green.

NEXT: TDD `mc homie send|history|end`: host-origin inbound append with stable
per-session sequence, origin binding and cross-surface echo outbox in one
transaction; deterministic durable history; host or allowlisted-own end that
deactivates bindings without deleting rows or touching the pipeline lease.
Keep implicit resume and runner claim/reply behind their following slices.

- 2026-07-12 — **Phase 2 Homie send/history/end green.** ADR-010 pins the
  inbound and lifecycle grammar. Host-only send accepts text and normalized
  attachment references, creates the first-traffic origin binding, allocates
  the next durable per-session sequence, advances activity time, and writes
  one `homie_echo` row for every other active binding in a single transaction.
  Injected outbox failure rolls back the message, implicit binding, and time.
  History renders the permanent transcript and structured attachments in
  stable order; host retains ended-session access while an allowlisted Homie
  is active-own only. End atomically flips status, appends the reason event,
  and lets the substrate deactivate bindings; injected event failure rolls
  all of that back, and host replay is idempotent. Pipeline/Homie transport
  forgeries, cross-session reads/ends, invalid attachment carriers, occupied
  origins, and ended sends are inert. Attachment and outbox JSON shapes now
  also have raw-schema CHECKs. Complete fast lane green; substrate suite 172.

NEXT: TDD `mc outbox poll|ack` as a host/native-surface-only delivery cursor:
poll one surface's undelivered rows in stable id order without mutation; ack
only a row owned by that same surface, idempotently. Then add explicit Homie
resume plus a real Homie-runner capability seam before claim/reply/register;
do not expose outbound transport through the model's Homie-agent identity.

- 2026-07-12 — **Phase 2 outbox delivery cursor green.** ADR-011 pins
  host/native-surface-only `poll --surface [--limit]` and
  `ack <id> --surface`. Poll returns only that surface's undelivered rows in
  stable id order with structured payloads and is byte-for-byte read-only.
  Ack verifies row ownership, stamps the spine clock once, leaves other
  surfaces untouched, and preserves the original timestamp on replay.
  Pipeline and Homie identities, wrong-surface/missing-row acks, invalid
  surfaces, and invalid limits are inert. The substrate now closes outbox
  surfaces to `discord|dashboard|cli` in addition to its JSON-object check.
  Complete fast lane green.

NEXT: Design and TDD the Homie-runner capability seam, then extend
`mc run register-session` to the exact own Homie session and add host-only
`mc homie resume <session> --from <surface:channel_ref>` as a record-only
status/binding transition. Require the immutable locator pair for native
continue mode; keep any conversation-row fallback explicit and audited.

- 2026-07-13 — Claude Code takeover from quota-interrupted Codex session.
  Resume ritual found the outbox delivery cursor slice (ADR-011,
  mc/verbs/outbox.go, schema surface CHECK, CLI tests, ledger entry)
  complete but UNCOMMITTED — Codex died on quota after going green but
  before committing. Nothing discarded: full fast lane re-verified green
  (Go all packages; fake 43, agent-runner 13, runner/image 2, resident 32)
  and the slice committed as 2f85fbe, pushed. Cross-harness adversarial
  takeover review launched per AGENTS.md §2 before any code edit: six
  read-only lens agents over the Codex range (wave-2 provenance/scope +
  structured errors; operator record verbs; Console publication; Homie
  registry/messaging; outbox cursor — extra scrutiny, never reviewed even
  once; plus a decorrelated spot-check of four wave-1 self-review closure
  claims: CAS, runner lifecycle authority, brief immutability, route truth).

NEXT: Collect and adversarially verify the takeover-review findings,
record confirmed/refuted in IMPLEMENTATION-NOTES.md, fix confirmed defects
red-first, re-green the full fast lane, then resume wave 2 at the
Homie-runner capability seam / register-session / homie resume slice above.

- 2026-07-13 — **Takeover review closed: 2 majors + 5 fix-class minors, all
  FIXED red-first** (a329c1a..63f7c8e, each committed at green). Majors:
  (1) Homie `worksource pause|archive` checked the status value against the
  frozen verb-name allowlist — the spec-§15.3 cell was dead and untested;
  (2) `nextLanding` gated on Worksource `active` with no spec license —
  approved work in a paused/archived Worksource stranded forever, its packet
  burning an Inv. 18 slot (three rows wedged dispatch at queue-saturated).
  Also fixed: task/initiative add now refuse inactive Worksources (new slug
  `worksource-inactive`); land report + outbox poll|ack scope-check before
  spine open; zero-args and delegation-failure now emit the JSON envelope;
  outbox gained no-delete/immutable/delivered-once substrate triggers and
  conversation rows an active-session INSERT fence; untagged ActiveRegistry
  regression test added (whole CLI suite builds tagged — a fake-family
  regression failed zero tests); weak review-claim assertions strengthened
  (full-column outbox snapshot, lease-release rollback injection, CAS
  lock↔run pairing). Wave-1 spot-check: all four Codex self-review claims
  HOLD on behavior. Nine informational entries appended to
  IMPLEMENTATION-NOTES (TZ blast radius, cross-midnight Console edge,
  content-path serving-seam obligation, id-disjointness backstop, initiative
  dead-end → Parked addendum, Homie-interrupt orphan window, worksource-add
  advisory deferral, image build-tag obligation); ADR-011's agent-exclusion
  wording corrected to best-effort-until-P3. Complete fast lane green
  (Go all packages; fake 43, agent-runner 13, runner/image 2, resident 32).

NEXT: Resume Phase 2 wave 2 at the Homie-runner capability seam: design the
seam (ADR), extend `mc run register-session` to the exact own Homie session,
and add host-only `mc homie resume <session> --from <surface:channel_ref>`
as a record-only status/binding transition requiring the immutable locator
pair for native continue mode; conversation-row fallback stays explicit and
audited. Then runner claim/reply transport. Initiative wave CLI remains
Parked pending the durable plan-review representation.

- 2026-07-13 — **Phase 2 Homie locator seam + record-only resume green**
  (746bbad). ADR-012 pins both: `run register-session` gains a homie-tier
  arm writing the set-once native locator pair onto the caller's own
  canonical registry row (identity not liveness — post-end registration
  stays legal per Inv. 26; allowlist untouched, runner-private scope;
  cross-session/pipeline/host attempts inert, conflicting replays refused in
  verb and trigger). `mc homie resume <session> --from <surface:channel_ref>`
  is host-only and record-only: ended|reaped → active + `homie.resumed`
  activity + fresh binding in one transaction; occupied place aborts the
  whole transition (session stays ended, no activity residue); missing
  locator pair refuses, naming the §15.4 conversation-rows fallback as a
  separate explicit arm; already-active resume is idempotent only for the
  exact crash-after-commit retry, otherwise "use bind"; no launch effect
  (relaunch is the resident's, Phase 3+, matching start's posture).
  Complete fast lane green.

NEXT: TDD the Homie runner claim/reply transport (the ADR-010 deferral):
fenced idempotent claim of pending inbound conversation rows over the
existing claimed_by/claimed_at/completed_at columns, and a runner-scope
`homie reply` that appends the reply row + per-binding echo/reply outbox
rows atomically — never through the model's Homie-agent identity. Then
sweep the remaining §18 operational verbs (doctor/backup/reset) toward the
wave-2 contract before the split-brain suite. Initiative wave CLI remains
Parked.

- 2026-07-13 — **Phase 2 Homie runner claim/reply transport green.** ADR-013
  pins the ADR-010 deferral. `mc homie claim <session>` is the runner's
  own-session scope (run.json tier homie, allowlist never consulted, host/
  pipeline/cross-session refused): lowest-id pending inbound turn, claim
  stamp set-once, a fresh runner reclaims a dead one's turn with the
  original stamp, nothing-pending is an empty result, no activity advance.
  `mc homie reply <session> --to <id>` requires the claimed own-conversation
  inbound turn and appends the reply row (new additive `reply_to` column —
  the durable linkage the double-reply law needs), completes the turn,
  advances activity, and fans one `homie_reply` outbox row to EVERY active
  binding (origin included, per §15.5) in one transaction; injected outbox
  failure rolls back everything. Identical-payload replay of a committed
  turn is idempotent (`replied: false`); any different payload is refused —
  one logical reply per inbound turn, enforced in storage by a partial
  unique index on reply_to plus CHECKs (reply⇔reply_to, claim-pair,
  completion-requires-claim, replies never claimable) and triggers
  (own-session-inbound reference, claim and completion set-once).
  Ended/reaped sessions refuse transport (resume stays an explicit host
  transition). Complete fast lane green (Go all packages incl. docker-tag
  vet + fake-routing build; fake 43, agent-runner 13, runner/image 2,
  resident 32); substrate suite +1 (TestConversationClaimReplyLattice).

NEXT: Sweep the remaining §18 operational verbs toward the wave-2 contract:
`mc doctor` (validate routing, schema, MC_HOME shape — container/gateway
probes stay Phase 3), `mc backup` (spine snapshot verb), and `mc reset`
(confirmation-required destructive re-init, snapshot first). Then the
deterministic split-brain kill-point convergence suite. Initiative wave CLI
remains Parked pending the durable plan-review representation.

- 2026-07-13 — **Phase 2 operational verbs green.** ADR-014 pins the
  Phase-2 tier of `doctor|backup|reset`, all strictly host scope refusing
  before any spine open, none able to create spine bytes at a missing
  MC_SPINE. `mc backup`: `VACUUM INTO` a temp name under
  `MC_HOME/backups/`, renamed on completion, same-second collisions
  suffixed; snapshot proven complete (tasks + meta identity) and the
  source stays writable; retention stays the resident tick chore's.
  `mc reset`: unconfirmed is a pure no-op refusal (no snapshot); confirmed
  snapshots first and aborts untouched if the snapshot fails, then deletes
  the spine + WAL/SHM siblings (dev-tier stand-in for §16.4 volume
  teardown); output is paths-only, never secrets. `mc doctor`: total
  finding surface `{check, status ok|fail|deferred, detail,
  onboard_section}` — MC_HOME shape, spine meta/UUID/schema (missing spine
  reported as loss with restore-from-backup language, never repaired),
  Worksource/sandbox-profile references, routing.md against the active
  registry; container/gateway/runtime-auth/supervision appear as deferred
  findings with their §17 repairing sections from day one; doctor always
  exits 0 with `ok` carrying the verdict and mutates nothing. Complete
  fast lane green (Go + docker-tag vet + fake-routing build; fake 43,
  agent-runner 13, runner/image 2, resident 32).

NEXT: TDD the `mc onboard` section dispatcher (§17, wave-2 contract §1.5):
named sections `preflight|home|runtime-auth|routing|container|worksource|
tunables|surfaces|supervision|verify`, resumable/idempotent per §16.4
(meta-first, never re-init a non-empty spine), Phase-2 doubles for host
effects, no launchd load ever (S7 rule). Then the deterministic split-brain
kill-point convergence suite (contract §4). Initiative wave CLI remains
Parked pending the durable plan-review representation.

- 2026-07-13 — **Codex takeover found Claude's quota-boundary onboarding
  commit red and incomplete.** HEAD 76b3694 is pushed but fails Go compile
  on an unused `database/sql` import; the new CLI tests and verb were
  committed without an `onboard` dispatcher/parser or the cited ADR-015.
  The other four fast lanes are green (fake 43, agent-runner 13,
  runner/image 2, resident 32). Cross-harness adversarial review also
  confirmed acceptance defects beyond the interrupted wiring: initial
  schema + meta were not atomic; the §16.4 UUID mirror/loss fence and
  meta-first read-only inspection were absent; the git-tree fence rejected
  allowed ignored roots yet missed symlink targets; the default surface
  path silently left Daily Console disabled; explicit tunable/surface
  replays were not skip-idempotent; and a conflicting `default` sandbox
  profile could misbind the first Worksource. Findings recorded in
  IMPLEMENTATION-NOTES; no implementation edit preceded the review.

NEXT: Close the confirmed onboarding findings red-first: author ADR-015,
wire the CLI, make first provisioning atomic and UUID-mirrored/meta-first,
fix the git fence, require and idempotently persist the Console schedule,
and harden tunable/Worksource replays. Re-green the complete fast lane and
commit without amending 76b3694. Then begin the deterministic split-brain
kill-point convergence suite; initiative wave CLI remains Parked.

- 2026-07-13 — **Phase 2 onboarding dispatcher green; takeover review
  closed.** ADR-015 pins the Phase-2 `mc onboard` tier and explicitly names
  every later-phase section obligation. The CLI is host-only, supports the
  ten ordered/named sections and dual-input flags, distinguishes
  `done|ok|deferred`, and never loads launchd. Home reuses the read-only
  preflight even by name; the git fence resolves symlinks and permits only
  paths proven ignored. Existing spines are inspected read-only/meta-first;
  schema + meta commit atomically; the UUID mirror is atomically published
  and compared; non-empty unidentified/lost/mismatched spines refuse; an
  injected failed fresh provision is retryable. Concurrent fresh callers
  adopt the one committed winner instead of deleting it (30× stress + race
  pass). Routing publishes a synced validated file atomically without
  following symlinks. First-Worksource, tunable, and surface replay decisions
  serialize under BEGIN IMMEDIATE; conflicting/permissive profiles refuse;
  the Console schedule is required and doctor/verify check both it and the
  UUID mirror. The cross-harness residual audit passes all six closure cells.
  Complete fast lane green (Go all packages; fake 43, agent-runner 13,
  runner/image 2, resident 32), plus Docker-tag vet and fake-routing-tag
  compile green.

NEXT: TDD the deterministic split-brain convergence suite (wave-2 contract
§4), starting with `action selected / before effect` and `session folder /
before run.json` through the injectable resident seams. Restart the ordinary
loop and assert the record/lock/effect triple converges with a new run and no
duplicate durable work; add no fault hook to `mc`. Then walk the remaining
kill boundaries. Initiative wave CLI remains Parked pending the durable
plan-review representation.

- 2026-07-13 — **First two deterministic split-brain boundaries green.** A
  new Docker-free cross-boundary resident suite builds the real test-tagged
  `mc`, commits each spawn decision against a temp spine, and injects death
  only through resident dependencies. Both `action selected / before effect`
  and `session folder / before run.json` restart through the ordinary tick
  loop: the spawn watchdog reaps the old Run, tolerates the absent container,
  releases the lease, charges exactly one dispatch retry, and reselects the
  unchanged subject under a distinct Run. The folder-boundary fixture proves
  the old trace-only folder remains present and empty while the absent
  envelope is safely removed; an extra tick proves the new live lease idles
  without duplicate Run, retry charge, or host effect. No production fault
  hook or recovery branch was added. Complete fast lane green (Go all
  packages; fake 43, agent-runner 13, runner/image 2, resident 34).

NEXT: Extend the same parameterized split-brain harness through `run.json /
before container` and `container start / before first heartbeat` (wave-2
contract §4). Assert reap removes the materialized envelope in both cases,
the trace-only folder survives, and the spawn watchdog charges exactly one
retry before the unchanged subject receives a distinct Run; model the
post-start death at the injectable Docker seam, never with a fault hook in
`mc`. Then continue to the workspace/commit boundary rows. Initiative wave
CLI remains Parked pending the durable plan-review representation.

- 2026-07-13 — **Remaining pre-heartbeat split-brain boundaries green.**
  The parameter table now covers `run.json / before container` and
  `container start / before first heartbeat` with one stateful test-local
  Docker daemon shared across the dead and restarted resident instances.
  The former proves the old container never became live; the latter proves
  it did become live even though the Docker result was lost. In both cases
  the ordinary restarted loop commits one watchdog reap, exact-name stop
  leaves the old container absent, the materialized envelope is removed,
  the permanent empty trace folder survives, and retry opens a distinct Run
  with the only live container. The record/lock/effect triple agrees on
  run, role, subject, Worksource, binding, and mounts; an extra tick proves
  no duplicate Run/start/charge. The audit also isolated arbitrary
  outcome-ambiguous stop failure as a composed-failure gap owned by the
  already-specified Phase-3 orphan sweep + pre-spawn assertion; logged in
  IMPLEMENTATION-NOTES rather than hidden by the single-fault fixtures.
  Complete fast lane green (Go all packages; fake 43, agent-runner 13,
  runner/image 2, resident 36).

NEXT: TDD the `workspace bytes / before git commit` and `git commit / before
complete` split-brain rows (wave-2 contract §4) through the injectable fake
harness/agent-runner process seams and the real `mc` CLI against a temp git
Worksource. Kill and restart the ordinary lifecycle; assert canonical task
status stays unchanged, partial bytes are inert, an existing task-branch
commit is observed rather than duplicated, and watchdog recovery charges
only its declared retry. Add no fault hook to `mc`. Then cover operator
approve/landing and outbox delivery boundaries. Initiative wave CLI remains
Parked pending the durable plan-review representation.

- 2026-07-13 — **Standalone Worker Git split-brain boundaries green.** The
  deterministic harness now drives the real resident, agent runner, fake
  harness, `mc` CLI, SQLite spine, and a temporary Git Worksource through
  both `workspace bytes / before git commit` and `git commit / before
  complete`. The first attempt reaches a real post-heartbeat lease and dies
  with either staged bytes or one task-branch commit; fixture-only clock
  surgery then lets the ordinary restarted loop reap it by `lease-timeout`,
  charge exactly one Worker retry, and select a distinct Run. The retry
  reuses the one canonical worktree: staged bytes become exactly one commit,
  while an existing commit is observed at the same SHA and completed without
  another commit. Both rows end worked with retry 2, no correction, one task
  branch/worktree/commit, a clean primary checkout, and unchanged `main`.
  Detached container-return semantics remain separate from runner completion,
  and a fivefold focused stress run is green. The missing generic worktree
  assignment and retry-unsafe Phase-1 e2e behavior are explicitly scoped and
  recorded in IMPLEMENTATION-NOTES rather than hidden in the fixture.
  Complete fast lane green (Go all packages; fake 43, agent-runner 13,
  runner/image 2, resident 38).

NEXT: TDD `operator approve / before land` and `merge success / cleanup or
report gap` through the real CLI, ordinary resident loop, `mc-land`, and a
temporary Git Worksource. Prove approval without effect deterministically
re-emits landing-pending; after a real successful merge, prove restart never
creates a second merge, preserves success truth, and exposes cleanup/report
debt. Inject death only at host-side effect seams, never in `mc`. Then cover
the outbox delivery boundary. Initiative wave CLI remains Parked pending the
durable plan-review representation.

- 2026-07-13 — **Approval/landing split-brain boundaries green.** The real
  CLI now binds Worker-reported standalone branches to `mc/task-<id>` (and an
  initiative child to its assigned parent branch), refuses branchless land
  reports, and makes a committed landing success terminal and replay-safe.
  The ordinary resident loop proves approval-without-effect re-emits the same
  land tuple, while cleanup-loss and report-loss restart from one exact merge
  receipt without a second merge, Run, lease, retry charge, or status lie.
  `mc-land` now fences the numeric task namespace and exact SHA, verifies real
  primary/task bytes with zero-stat indexes, preserves unrelated operator
  edits, pins merge behavior and receipt identity, compare-deletes the task
  ref, and surfaces ambiguous residue as success-with-cleanup-debt. An
  adversarial pass found primary/task config redirects, executable content
  transforms, replacement refs, stat-cache/index-visibility hiding, and late
  cleanup config races; content-sensitive merge/preflight/removal now use a
  minimal isolated Git common/config/attributes view sharing only the intended
  real objects, refs, linked-worktree metadata, index, and working tree. The
  40-case direct landing matrix includes all of those regressions plus hooks,
  rename inference, conflict abort, moved/symbolic refs, path spaces, and
  report replay. Complete fast lane green (Go gofmt/vet/all packages; fake
  43, agent-runner 13, runner/image 40, resident 41).

NEXT: TDD the final deterministic split-brain row, `message/outbox insert /
delivery` (wave-2 contract §4), through the real `mc` CLI and the host/native
surface delivery seam. Prove message+outbox insertion is one transaction;
death after physical delivery but before ack re-polls the same durable outbox
id for at-least-once delivery; external de-duplication keys that id; ack and
ack-response-loss replay are idempotent; and the record/lock/effect triple
converges without deleting the source message. Then implement the nightly
property package and bounded fast honesty/mutant gates (§5). Initiative wave
CLI remains Parked pending the durable plan-review representation.

- 2026-07-13 — **Final message/outbox split-brain boundary green.** One real
  `homie send` transaction durably appends the inbound conversation row and
  its destination outbox row. A fixture-local native-surface client then
  drives the real `outbox poll → external delivery → outbox ack` protocol:
  death after the external post but before ack re-polls the byte-identical
  durable id, and an explicitly external id-keyed fake collapses the retry to
  one logical post while preserving Mission Control's at-least-once contract.
  A second death after the ack commit proves response-loss replay is inert,
  retains the first delivery timestamp and source history, and leaves the
  record/lock/effect triple unchanged. No production Discord/dashboard loop
  or exactly-once claim was introduced; independent adversarial review
  confirmed that boundary. Complete fast lane green (Go gofmt/vet/all
  packages; fake 43, agent-runner 13, runner/image 40, resident 42).

NEXT: Implement the Phase 2 nightly property package from wave-2 contract §5
under `mc/property` with the `nightly` build tag: dispatch purity/cardinality,
ineligible-row metamorphism, and lifecycle random walks checked against the
substrate after every step. Keep runtime randomized suites non-gating, but
TDD bounded fast generator-honesty floors and the complete named planted-
mutant gate (blocked filter, packet archive, budgets, lease token, WIP cap).
Initiative wave CLI remains Parked pending the durable plan-review
representation.

- 2026-07-13 — **Property generator, dispatch, and fast mutation gates
  green.** The new test-only `mc/property` package shares one reproducible
  corpus between the untagged fast gates and `nightly`-tagged runtime tests.
  The bounded observer derives, rather than trusts, floors for every declared
  legal status/scope/decision, packet/scope, decision/packet/scope, blocked,
  and six-shape lease bucket. Its exact registry kills all five contract
  mutants with exercised witnesses: dropped blocked filtering, ignored packet
  archive, blurred budgets, weakened lease fencing, and a missing WIP cap in
  both enforcement layers. The tagged dispatch run proves action-union
  cardinality, determinism, deep purity (including pointer fields and nil
  slice identity), and 4,096 query-scoped ineligible-row insertions across
  blocked, archived, paused-Worksource, and archived-packet history. Tagged
  dispatch totals: 16,384 generated states + 4,096 metamorphic cases green.
  Complete fast lane green (Go gofmt/vet/all packages including the bounded
  property gates; fake 43, agent-runner 13, runner/image 40, resident 42).

NEXT: TDD the remaining `nightly` twin-spine lifecycle random walk. Drive
every exported stateful `mc/domain` operation against one domain spine and
the corresponding canonical raw SQL against a second spine under matched
`BEGIN IMMEDIATE` transactions; normalize timestamps, compare acceptance and
state, and audit the trigger invariants after every step. Include valid and
classified invalid intents, rejection rollback, all task/verdict/packet/wave/
budget/lease arms, and fixed seed+step replay. Initiative wave CLI remains
Parked pending the durable plan-review representation.

- 2026-07-13 — **Quota-interrupted twin-spine lifecycle property recovered,
  adversarially hardened, and green.** The prior Codex session exhausted its
  quota with one preserved test artifact; the operator checkpointed those
  bytes as 0a66894 while this session was grounding. Four fixed-seed walks now
  drive every exported stateful `mc/domain` operation against independent
  domain/raw spines under matched `BEGIN IMMEDIATE`, with a complete curated
  arm walk, randomly interleaved task/initiative strata, and 160 reproducible
  perturbation steps per seed. Every accepted step compares result and full
  relevant durable state; every classified rejection proves the exact domain
  code and rollback; both spines audit trigger invariants, foreign keys, lease
  arithmetic, timestamp validity/bounds, and periodic integrity checks.
  Adversarial review caught and closed four oracle weaknesses before
  acceptance: refinement BUDGET-SPENT had no post-write rejection witness;
  domain-only raw paths were permissively labeled `may accept` despite this
  schema deterministically accepting them; snapshots collapsed or omitted
  timestamp/carrier/tunable state; and forced lifecycle strata were credited
  together with the narrower random tail. The closure adds the composite
  rollback witness, requires raw acceptance, derives subjectless Fence truth
  from the raw lock, validates the actual heartbeat stamp, snapshots every
  relevant column, poisons every status edge to prove stage restamping, and
  carries a non-null packet thesis through rerender. Complete nightly suite +
  nightly vet green; focused lifecycle ×10 green; Docker-tag compile/vet and
  fake-routing-tag compile/vet green; complete fast lane green (Go all
  packages; fake-harness 43, agent-runner 13, runner/image 40, resident 42).
  Phase 2 is complete for every unparked acceptance line; the initiative-wave
  CLI remains isolated under Parked and is not implied accepted.

NEXT: Begin Phase 3 boundary conformance by authoring
`docs/phase3-contract.md` from handoff Part 3 before production changes. Pin
one Docker-backed test per enforcement mechanism, the mechanism ownership and
failure semantics, and the exact non-Docker compile/fast lanes. Then TDD the
first independent mount-validation/fail-closed slice (accept + reject for
symlink, blocked pattern, `..`/`:`, cross-Worksource, and RW-only-when-both-
agree; an unappliable rule blocks without spawning). Promote S1/S2 as frozen
canaries, do not load launchd, and do not route around the parked initiative-
wave representation.

- 2026-07-13 — **Phase 3 grounded and boundary contract adversarially
  closed.** Before production changes, the full Phase-3 handoff paragraph was
  reconciled against spec §§5/9/11/16/17, every Phase-0 boundary result,
  ADR-014/015, the current image/resident/runner/onboarding seams, and all
  open implementation notes. `docs/phase3-contract.md` now assigns a named
  real-mechanism test to every handoff group; freezes configuration trust
  roots, immutable mount/network effects, production-vs-fake identity,
  S1/S2 promotion, Homie/pipeline scope, orphan/resource semantics, exact
  no-Docker/Docker lanes, and later-phase exclusions. Three independent
  read-only audits found that nearly every boundary remains spike- or
  skeleton-only and caught an unimplementable first draft: it skipped reap
  reconciliation when readiness was red, promised a spine health write while
  the macOS runtime crossing was unavailable, assigned native-path
  resolution to a helper without the host file plane, blurred the same-uid
  runner grain into kernel authorization, omitted the lease-free Homie wake
  path, and left gateway/raw-TCP ownership unnamed. The closure requires a
  single external `mc dispatch` with an ADR-pinned host↔lock-domain handshake
  before claim, keeps all state consequences inside `mc`, carries separate
  registry-driven Homie planning, and adds exact secret non-forwarding,
  identity-matched orphan, plural-mount, and defaulted-resource evidence.
  The handoff's wildcard-env shorthand and the Homie historical-file/Inv. 26
  tension are resolved conservatively and logged in
  `IMPLEMENTATION-NOTES.md`. Complete fast lane green (Go all packages;
  fake-harness 43, agent-runner 13, runner/image 40, resident 42); diff check
  green. No Docker, production behavior, secret, or launchd state changed.

NEXT: Author the four Phase-3 pre-code decisions in the next available ADRs:
ADR-016 pins the one-command host↔lock-domain pipeline/Homie plan handshake,
candidate/TOCTOU fence, immutable effect/reason schema, and exact
pipeline/Homie/helper liveness labels; ADR-017 pins the extend-only mount
allowlist grammar, blocked-pattern floor, and collision-free plural mount
destinations; ADR-018 pins the Docker Desktop gateway/raw-TCP topology,
DNS/rebinding posture, injection scope, and audit ownership; ADR-019 pins
finite CPU/memory/PID defaults and override bounds. Adversarially review and
commit those plans before production code. Then TDD the first pure
`mc/boundary` accept/reject table and its invalid-plan/no-claim transaction
slice. Do not load launchd or route around the parked initiative-wave model.

- 2026-07-13 — **Quota handoff recovered; Phase-3 boundary ADRs accepted.**
  Grounding resumed from green `72a39db`: ledger, recent commits, dirty tree,
  spec/handoff, current seams, and the complete fast lane were re-read/run
  before new implementation. The preserved work was documentation only and
  was treated as data. A direct Docker Desktop canary also proved a cached
  `alpine:3.22` container reaches a server bound only to host `127.0.0.1` via
  `host.docker.internal`, supporting the loopback-only native gateway; no
  production container, secret, or launchd state changed.

  ADR-016 now pins the resident-only prepare/attest/commit composition,
  pre-prepare deployment identity, digest-only raw host files, exact request
  and action receipts, bounded-memory paged inventories with count-derived
  progress, one-action lifecycle order, attested landing, launch/container/
  runner fencing, Homie effect debt, and explicit bounded O(tail) row resume.
  ADR-017 pins the strict extend-only allowlist and identity walk, complete
  destination tables, closure-only isolated standalone Git store, privileged
  completion seal, disposable Verifier source, topology-fenced landing,
  same-inode pipeline-trace projection, and a crash-safe attachment queue,
  publication, deduplication, and GC protocol. ADR-018 pins the loopback native
  gateway, per-launch uid-filtered guard namespace, fixed DNS/raw rules,
  one-use resident control, registration-generation fencing, an exact
  five-container preclaim proof, and failure classes. ADR-019 pins six finite
  deployment-only CPU/memory/PID classes and exact capability/security
  envelopes.

  Repeated independent audits found and closed response-loss double actions,
  large-inventory/cardinality wedges, stale Docker states, resident-restart
  adoption, pipeline/Homie starvation, empty and aggregate-overflow row
  priming, unsafe real Git object sharing, producer/seal races, bare/external
  Git-control leakage, setup-parent write authority, attachment intake and
  dedup-GC crash races, unbounded probe clients, and stale contract prose.
  The two explicit spec deviations—pre-landing task-local Git and one
  stale-writer cleanup before Console/landing—are appended to
  `IMPLEMENTATION-NOTES.md`; initiative shared-worktree/holistic review remains
  Parked. Complete fast lane green (Go all packages; fake-harness 43,
  agent-runner 13, runner/image 40, resident 42); diff check green.

NEXT: TDD the first pure `mc/boundary` slice. Start with red table tests for
strict `mount-allowlist` parsing (deny-all and ordinary accept; unknown/
duplicate/malformed input), target grammar/collisions, the shipped blocked
floor plus additive patterns, and bilateral RO/RW results. Implement only the
pure parser/policy needed to turn those tests green, run the complete fast
lane, and commit. Then add canonical filesystem identity/symlink and
cross-Worksource/protected-root checks before integrating invalid-plan/no-claim
dispatch. Do not load launchd or route around the parked initiative-wave
model.

- 2026-07-13 — **Phase-3 pure mount policy GREEN.** TDD began with a
  build-red `mc/boundary` table suite, then added a filesystem-free policy
  package shared by later profile admission and dispatch planning. The
  allowlist parser now accepts only TOML v1 `version = 1` plus literal
  `[[allow]]` tables, including valid deny-all, and rejects unknown,
  duplicate, missing, mistyped, malformed, case-variant, inline/dotted, and
  over-limit input before allocation or filesystem work. BurntSushi TOML
  v1.6.0 is pinned as a direct dependency so comments/escapes and duplicate
  semantics come from a real TOML parser while metadata checks keep the
  schema syntactically closed.

  Relative targets enforce exact UTF-8/POSIX byte and component limits with
  no cleaning or renaming; pairwise collision checks reject equality and
  ancestor overlap without case-folding. A local adversarial pass caught and
  regression-tested the non-adjacent lexical trap `docs`, `docs-api`,
  `docs/api` before commit. The private exact 18-component + 22-glob shipped
  floor is always evaluated—even by a zero-value policy—while validated
  operator patterns are additive only, bounded, ASCII-case-insensitive, and
  matched by a closed literal-plus-`*` implementation. The bilateral access
  table returns the requested mode or rejects RW-over-RO; it never silently
  downgrades or drops a mount. Two independent read-only reviews accepted the
  implementation after exact-floor, near-miss, wrong-type, zero-value, and
  maximum-bound tests were added. Complete serial fast lane green (Go all
  packages; fake-harness 43, agent-runner 13, runner/image 40, resident 42);
  diff check green. No Docker, secret, production runtime, or launchd state
  changed.

NEXT: TDD the filesystem identity half of `mc/boundary`: strict
`MC_HOME`/`mount-allowlist` owner-mode/non-symlink regular-file trust seams,
canonical `Abs → Clean → EvalSymlinks` source resolution, raw+resolved blocked
address checks, filesystem-identity allow-root ancestry with exact-one-root
authorization and suffix validation, and symlink-stays/escapes fixtures. Keep
protected-root and cross-Worksource bilateral exclusions as the immediately
following pure-policy slice, then integrate invalid-plan/no-claim dispatch.
Do not load launchd or route around the parked initiative-wave model.

- 2026-07-14 — **Claude Code takeover from quota-interrupted Codex session;
  mount-target control grammar closed.** Resume ritual found `4380e0d` clean,
  pushed, and green — the complete fast lane reproduced exactly the ledger's
  header counts (Go all packages; fake-harness 43, agent-runner 13,
  runner/image 40, resident 42). Nothing was uncommitted; nothing discarded.
  The cross-harness adversarial takeover review of `72a39db..4380e0d` (the
  Codex range: four accepted Phase-3 ADRs + the pure mount policy) launched
  before any code edit per AGENTS.md §2, as five read-only lenses with
  independent skeptical verification of every finding.

  **Credit exhaustion killed three of the five lenses mid-run** (allowlist
  grammar, blocked floor, ADR-vs-spec conformance); the two that completed
  were recovered from the workflow journal rather than lost. Neither found a
  major defect: the shipped 18-component + 22-glob floor is pinned exactly by
  `reflect.DeepEqual`, the closed star-glob matcher survived ~3M fuzzed cases
  against `path.Match`, and there is no blocked-floor bypass, RW-over-RO
  escalation, or target-collision bypass. The surviving lenses substantially
  cover the grammar/floor lenses' territory; the ADR-vs-spec lens is the real
  gap and was relaunched separately.

  One confirmed deviation, FIXED red-first: ADR-017 Decision 1 forbids a
  `control` target component without qualification, but `ValidateTarget` only
  rejected ASCII controls, so C1 NEL (U+0085), U+2028/U+2029, ZWSP, and the
  RTL override all passed. Targets now reject `unicode.IsControl` plus
  `Cf`/`Zl`/`Zp`; NBSP and fullwidth solidus stay legal with recorded reasons.
  Also filled the test gaps the claims-vs-tests audit named: the exact
  1025-byte target boundary, ASCII-case-insensitive matching of operator
  additions in both directions, and the mislabeled test that claimed to prove
  the (unreachable) per-entry UTF-8 check. Two entries appended to
  IMPLEMENTATION-NOTES (the control-grammar deviation; the informational
  residue, incl. the allow-root overlap/identity seam left to the next slice).

- 2026-07-14 — **Phase-3 filesystem identity and containment GREEN**
  (`e01a2af`). TDD from a build-red suite; 55 cases. Trust seams: allowlist =
  non-symlink regular file, MC_HOME = non-symlink directory, both
  operator-owned with no group/other bits, `Lstat` so a symlink to a trusted
  object is not itself trusted, stricter owner mode not a grant. Resolution
  retains raw-clean AND canonical (`Abs → Clean → EvalSymlinks` + stat)
  because the floor matches both — a symlink named `innocent` pointing at
  `.ssh` rejects. Containment is filesystem identity, never string
  arithmetic: `ResolveAllowlist` enforces Decision 1's uniqueness law with
  `os.SameFile` (byte-identical, symlink-aliased, and ancestor-overlapping
  roots all reject `mount.source_alias`), closing both seams the takeover
  audit named as deferred. `Authorize` walks the resolved source's ancestors
  by identity, accepts at exactly one root, derives the suffix from that walk
  and validates it: in-root symlink accepted, escaping one `symlink_escape`
  (distinguished from `not_allowlisted` by where the raw address lived),
  `safe-root-evil` never satisfies `safe-root`, colon legal in the matched
  root's own spelling but not in a descendant suffix. Coded rejections use
  ADR-017's stable `mount.*` slugs via the domain's `errors.As` convention.
  Four planted mutants (identity→prefix, blocked-on-raw-only, unvalidated
  suffix, overlap check removed) each die with exercised witnesses. Complete
  fast lane green; gofmt/vet and both tagged builds clean.

  **Two obligations deliberately NOT in this slice** (named so they cannot be
  quietly lost): (1) the macOS **ACL leg** of the trust seam — ADR-017
  Decision 1 requires "any allow ACE granting a non-owner access rejects",
  which needs the native ACL API (no stdlib path; cgo would need a
  darwin-only build tag since `mc` also builds for the Linux container). Owner/
  mode/non-symlink are enforced; a granting ACL is currently NOT detected.
  (2) **Protected-root and cross-Worksource jurisdiction** (Decision 3 step 5,
  Decision 5) — `Authorize` applies no `denied_root`/`cross_worksource` check
  yet, so it must not be wired into production planning before that slice
  lands.

NEXT: TDD the protected-set/jurisdiction slice of `mc/boundary` (ADR-017
Decision 5 + Decision 3 step 5): the non-subtractable protected union
(profile `denied_paths`, other Worksources' roots, real Git control dirs,
`MC_HOME/sessions` and the attachment/output/config/control/backup/runtime-auth
roots, gateway/CA key roots, and the `~/.ssh`-class home roots), bidirectional
intersection so a source that is an ancestor of a protected root also rejects,
and the directional `broad_root` rule (a source may not equal or be an ancestor
of `$HOME`, `/Users`, `/`, while `~/src/project` stays eligible). Codes
`mount.denied_root`/`mount.cross_worksource` already exist unused. Then the
macOS ACL leg (a darwin-tagged native read, else an explicit ADR-recorded
fallback), then the stable-code mapping for the parser's uncoded rejections,
then integrate the invalid-plan/no-claim dispatch transaction. Do not load
launchd or route around the parked initiative-wave model.

- 2026-07-14 — **Session end: environment-blocked, three operator decisions
  recorded.** Landed this session: `67c4b61` (takeover review of the Codex
  range; mount-target control grammar deviation fixed red-first), `e01a2af`
  (Phase-3 filesystem identity + containment, 55 cases, four planted mutants
  killed), `df17dfe` (ledger). All green at commit; all local-only.

  **BLOCKER — macOS TCC on `~/Documents` (read this before diagnosing
  anything).** Symptom: `getcwd()` inside the repo returns EPERM while
  path-based reads still work *intermittently*. Because `git` and the Go
  toolchain call `getcwd()` before anything else, **both die outright** —
  including `git -C <abspath>` run from a safe cwd, which fails before `-C`
  applies. `/bin/pwd` appears to work only because it falls back to `$PWD`;
  `/bin/pwd -P` shows the truth. This is NOT a harness sandbox issue — it
  persists with `dangerouslyDisableSandbox`. The operator's fix is a fresh
  terminal (has worked before) or Full Disk Access for the terminal app.
  If reads flap, STOP: do not run read-dependent review agents (see below).

  **Operator decisions (2026-07-14):**
  1. **Push**: the operator pushes manually. The `git push origin main` deny
     lives in `~/.claude/settings.json` and CANNOT be overridden per-repo —
     verified against the docs: a user-level deny beats a project-level allow,
     hooks cannot clear it, and `bypassPermissions` does not either. **Agents
     must not attempt to push and must not route around the rule** (a bare
     `git push` is evasion, not a fix). Keep committing at every green
     micro-step exactly as AGENTS.md §4 says — only the push leg is the
     operator's. This supersedes the "Push to origin is blocked" Parked item.
  2. **ADR-016..019 findings**: verify before further Phase-3 code (below).
  3. **Initiative wave**: reading (i) is ACCEPTED — see the Parked entry.

NEXT (in order, once `getcwd` works — prove it with `cd <repo> && /bin/pwd -P`
and `git status` BEFORE trusting anything else):

1. `git add docs/reviews/2026-07-14-adr-016-019-review.json` and commit it —
   it is currently **untracked**, written during a readable window. It holds
   all 17 findings with full evidence from the decorrelated cross-harness
   review of ADR-016..019 (which Codex had only self-reviewed). Losing it
   loses the only record.
2. **Verify the 17 findings, 6 of them major, before any further Phase-3
   code** (operator decision 2). They are UNVERIFIED — two verification passes
   were destroyed by the TCC failure, not by refutation. **Do not read the
   saved verdicts as refutations**: every agent explicitly wrote
   "confirmed:false means UNVERIFIABLE, NOT refuted". A verifier prompt that
   says "default to REFUTED" is DANGEROUS under flapping TCC — it silently
   converts real findings into false all-clears. Gate any such run on a real
   read probe first. The six majors: ADR-016 gating the lease-free Homie tier
   behind the pipeline lease (would break Inv. 1/§11.6/§15.5); ADR-017 turning
   spec §11.3's RW `/workspace` into an RO 0555 bind; ADR-016 packing Git
   closure extraction into the 60s `spawn_grace_s` budget (would burn the
   3-retry infra budget and block tasks); ADR-017's uid-10001 0700 roots having
   no creation mechanism for an unprivileged LaunchAgent; ADR-017 Decision 8
   requiring the host surface to read a tree it makes unreadable to the host
   uid; ADR-018 asserting separate user namespaces, which would break its own
   uid-filtering mechanism. Fix or log what survives, red-first.
3. **Author the initiative-wave ADR** (ADR-020) — the operator ACCEPTED
   reading (i) on 2026-07-14, unparking a decision open since 07-12. Shape:
   a `plan_reviewed` flag on wave children born 0; dispatch query (3) will not
   dispatch a child at 0; a NEW arm makes an initiative with any unreviewed
   open child visible to the **Editor** — the one exception to §10's "an
   initiative with open children is parked" rule, which otherwise wedges the
   initiative; the Editor terminal either passes the wave (children → 1) or
   sends it back in prose (children cancelled via the existing cascade,
   initiative returns to drained and Strategist(initiative) replans). Treat
   "holistic" as wave-level pass/fail, not per-child rejection, unless the
   operator says otherwise. Readings (ii) and (iii) are dead: (iii) breaks
   producer≠judge; (ii) collapses into (i) because seeded children still
   dispatch before the batch pass runs. Adversarially review the ADR, then TDD
   it, then wire `strategist wave`.
4. Then resume the Phase-3 protected-set/jurisdiction slice per the earlier
   NEXT — but only after (2), since two of the majors target ADR-017
   Decision 5, which that slice implements.

Two Phase-3 obligations remain deliberately open and must not be lost: the
macOS **ACL leg** of the trust seam (ADR-017 Dec. 1 requires rejecting any ACE
granting a non-owner; owner/mode/non-symlink are enforced but a granting ACL
is currently NOT detected — empirically confirmed 2026-07-14: `chmod +a "staff
allow read"` is invisible to both mode bits and `xattr`, so `TrustPolicyFile`
accepts it; needs the native ACL API behind a darwin build tag), and
**protected-root/cross-Worksource jurisdiction** (`mount.denied_root` /
`mount.cross_worksource` exist as codes but are unused — `Authorize` must not
be wired into production planning until that slice lands).
