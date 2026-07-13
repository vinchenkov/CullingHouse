# IMPLEMENTATION-NOTES — the deviation log

Append-only, newest last. Addressed to the operator: every judgment call
the agents made that the spec didn't cover. *Planned* designs the spec
delegates live in `docs/adr/`, not here.

Entry template:

```
## <date> — <one-line title>
- Where: <phase/test/spec § that surfaced it>
- Gap: <what the spec didn't cover or got wrong>
- Choice: <the conservative option taken, and why it is the conservative one>
- Spec impact: <sections whose text should change, or "none">
- Needs your decision: no | yes → also parked in PROGRESS.md
```

---

## 2026-07-10 — OPERATOR-INPUTS.md was committed with live secrets
- Where: handoff §1.1 / §4 (pre-session seeding); found at session start
- Gap: the handoff requires `.gitignore` covering `OPERATOR-INPUTS.md`
  before the first commit; the seed commits tracked it instead (MiniMax
  key, Discord bot token now in local git history)
- Choice: untracked the file (`git rm --cached`), added `.gitignore`,
  kept history intact after a history-rewrite attempt was declined; no
  remote will be created until the operator scrubs history or rotates the
  keys. Conservative: reversible, fail-closed (nothing leaves the machine).
- Spec impact: none
- Needs your decision: yes → parked in PROGRESS.md

## 2026-07-10 — db_schemas.sql not seeded
- Where: handoff §1.1 scaffold table
- Gap: the "substrate starting point" file is absent from the seed folder
- Choice: derive the substrate schema directly from spec §4/§5/§6 — the
  handoff already marks the file "starting point, spec wins", so building
  from the spec deviates least
- Spec impact: none
- Needs your decision: no (informational; supply the file if it exists)

## 2026-07-10 — docs/priors/ evidence reconstructed, not copied
- Where: handoff §1.1 (docs/priors/), §4.3 (trusted priors)
- Gap: no `poc/` folder exists anywhere findable and the Claude Code
  memory directory is empty; the trusted priors could not be copied
- Choice: wrote the three §4.3 priors as short notes marked RECONSTRUCTED
  from the handoff/spec text, without inventing detail beyond what those
  documents state. Conservative: preserves the "do not re-derive" intent
  while flagging reduced evidentiary weight.
- Spec impact: none
- Needs your decision: no (informational; drop original POCs in if found)

## 2026-07-10 — dev MC_HOME scratch path chosen by agent
- Where: handoff §4.3 (environment facts); OPERATOR-INPUTS.md "Paths"
- Gap: OPERATOR-INPUTS.md references "the scratch path above" but records
  none
- Choice: `~/.mc-dev-home` (outside every git tree, never
  `~/.mission-control`); recorded in OPERATOR-INPUTS.md. Conservative:
  out-of-tree avoids the §16.1 git-working-tree fence entirely.
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — Phase 1b pins a skeleton-only `mc init` provisioning verb
- Where: Phase 1b contract §2/§9 A1; spec §18 (verb surface), §16.4/§17 (provisioning)
- Gap: §18 defines no init verb; spine provisioning belongs to `mc onboard`, a Phase 5 deliverable, but the Docker e2e must provision a spine inside a named volume where only `mc` can write
- Choice: one host-scope `mc init` (schema + meta + one worksource/profile + tunables), documented as the precursor to onboard sections home|worksource|tunables. Conservative: keeps mc the sole writer (alternatives bypass it), single verb, trivially folded/renamed later
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — Worker records `tasks.branch`; Verifier records `verified_sha`
- Where: Phase 1b contract §2/§9 A2; spec §7
- Gap: Landing consumes (branch, verified SHA, target ref) but no verb is specified to write them
- Choice: `mc complete --branch` (worker; the branch becomes real at worker completion) and `mc verifier verdict --sha` (the SHA is verification-time knowledge — "only the exact reviewed commit can land"). Extends §18's complete flags and ADR-001 D4's verdict flags by one flag each; reversible, Phase 2's verb ADR pass can rehome them
- Spec impact: §18 `mc complete` flag list; ADR-001 D4 verdict signature (additive)
- Needs your decision: no

## 2026-07-11 — Skeleton grade of §11.5 enforcement
- Where: Phase 1b contract §1/§2; spec §11.5 (setuid gate, kernel-denied spine, per-container scope matrix)
- Gap: the kernel-backed gate is Phase 3's deliverable (handoff Part 3), but the skeleton runs the real container topology now
- Choice: spine stays in the lock domain (named volume, helper, self-delegation — the §11.5 architecture), while setuid/uid-deny and the full per-container refusal matrix defer to Phase 3; skeleton mc is sole writer by construction (nothing else in-container links sqlite) and implements run.json identity + role-match + run_id fencing. Least deviation that keeps fail-closed topology without pulling Phase 3 forward
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — Skeleton routing: resident test config stands in for routing.md
- Where: Phase 1b contract §6/§9 A5; spec §9.1 (routing.md authoritative, reject-unresolved)
- Gap: the fake family exists in test configs only (handoff Part 3), and routing.md parsing is Phase 2 scope
- Choice: e2e resident config maps role→behavior file; `mc dispatch` stamps binding='fake'. Reversible: Phase 2 moves resolution into mc and the resident config shrinks to nothing
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — fake-harness tool-use truncation cap redefined as UTF-8 bytes with boundary back-off
- Where: Phase 1b, runner builder; contract §4 ("truncation at 8KiB") / cli.ts CAPTURE_LIMIT
- Gap: neither the spec nor the 1b contract defines the truncation unit; the draft cli.ts
  documented bytes but measured UTF-16 code units, letting multibyte output exceed 8 KiB ~3x
- Choice: cap = 8192 UTF-8 bytes, cut backed off to a character boundary (kept prefix is
  always valid UTF-8, possibly a few bytes under 8192). Conservative: matches the code's own
  stated unit, fail-closed (never keeps more than documented), pinned by tests, trivially
  reversible; a code-unit cap would over-retain and a mid-character byte cut would emit U+FFFD
  into the durable native.jsonl record
- Spec impact: none (README.md documents it as the fake family's contract)
- Needs your decision: no

## 2026-07-11 — Two additive spine columns the contract's own pins require
- Where: Phase 1b, mc builder; contract §2 (`mc init … --hard-deadline-minutes`
  "updates lock tunable columns"; `mc editor decide` coverage "stored on the
  runs row at claim") vs contract §8 ("schema changes are out of scope")
- Gap: the Phase 1a schema has no lock column for the hard-deadline tunable
  and no runs column for the Editor pool snapshot; the contract pins both
  behaviors while declaring schema changes out of scope
- Choice: added `lock.hard_deadline_minutes INTEGER NOT NULL DEFAULT 240` and
  `runs.pool_snapshot TEXT` (NOTE(P1b.1)/NOTE(P1b.2) in schema.sql). Additive,
  defaulted, no trigger interaction; all 155 substrate tests pass unmodified.
  Alternatives (activity-row smuggling, runtime ALTER, fixed constants) either
  diverge the spine from Schema or drop a pinned tunable. Easiest to reverse.
- Spec impact: none (spec §10 lists lock fields non-exhaustively re tunables)
- Needs your decision: no

## 2026-07-11 — `mc init` takes no tick-interval flag
- Where: contract §2 tunable list ("--tick…") vs contract §5 (resident tick is
  MC_TICK_INTERVAL_MS in tests, in-process default 60s)
- Gap: nothing in the skeleton reads a stored tick interval — the resident must
  not open the spine, and no verb returns it
- Choice: omitted the flag rather than store a value nothing reads (would need
  a third schema column). The e2e shrinks the tick via the resident's env.
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — MC_RUN_JSON env override for the run.json path
- Where: ADR-001 D2 / §11.5 fixed mount /mc/run.json; CLI test tier needs to
  materialize identities without Docker
- Gap: tests cannot write /mc/run.json on the host
- Choice: default stays /mc/run.json; MC_RUN_JSON overrides. Within-container
  scope separation is best-effort by decision (§11.5) and the --run fence still
  binds any forged role to its own live lease; per-container kernel-backed
  refusals are [P3] where this override must be dropped for baked binaries or
  gated out.
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — Console schedule pinned "not configured" (hour-24 sentinel)
- Where: dispatch verb Config loading; §10 step (0b), §14; contract §5 defers
  console scheduling to [P2]
- Gap: DefaultConfig's 08:00 UTC would make every post-08:00-UTC tick spawn
  Strategist(console), which has no fake behavior and no `mc console publish`
  in the skeleton
- Choice: pass ConsoleHour=24 (normalizes to next-day 00:00 → consoleDue never
  true) — disables step (0b) without modifying the promoted dispatch package
  (contract §3: only package/module wiring may change). Phase 2 replaces the
  sentinel with the stored §16.3 schedule.
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — Strategist proposals insert origin='autonomous', not 'agent'
- Where: ADR-001 D4 ("origin='agent'") vs substrate CHECK (user|autonomous)
- Gap: vocabulary mismatch between the ADR and the Phase 1a schema
- Choice: 'autonomous' — the schema's agent-provenance value; the ADR's
  'agent' reads as the same class. Schema wins (it is the shipped backstop).
- Spec impact: ADR-001 D4 wording could say 'autonomous'
- Needs your decision: no

## 2026-07-11 — session_path is MC_HOME-relative ("sessions/<run_id>")
- Where: contract §2 spawn effect JSON / §4 host-side MC_HOME/sessions/<run_id>
- Gap: mc executes in the lock domain (helper container in the real topology)
  and cannot know the host's MC_HOME to emit an absolute path
- Choice: emit and store the MC_HOME-relative path; the resident (which owns
  MC_HOME) joins it before mkdir/mount. Deterministic in every topology.
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — mc/e2e (docker_e2e) not built in this work order
- Where: contract §7/§8 place the build-tagged e2e in the mc builder's dir
- Gap: the e2e consumes the resident and runner deliverables of the two
  parallel builders; my work order (verbs + dispatch promotion + CLI tests +
  CAS test) does not include it
- Choice: left mc/e2e absent rather than stub it green; the fast suite is
  Docker-free by construction either way. The integration step that assembles
  all three builders' outputs should author it against contract §7's ladder.
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — TickDeps extends the contract's pinned bundle with `fs` and `config`
- Where: Phase 1b resident, contract §5 ("deps carries {intervalMs, setTimer, clearTimer, runMc, docker, log}")
- Gap: the spawn effector must mkdir the session folder and write run.json (§10 effect order), but the pinned bundle names no filesystem dependency and no static wiring (image, mounts, role→behavior map)
- Choice: added two members — injectable `fs` `{mkdir, writeFile}` and a `config: ResidentConfig` — rather than reaching for globals or ambient env; all six pinned names kept verbatim. Conservative: pure superset of the contract's text, keeps tests hermetic (fail-closed testability), trivially reversible
- Spec impact: none (contract §5 could list the two extra deps)
- Needs your decision: no

## 2026-07-11 — MC_SPINE reaches the runner via container env at docker run
- Where: Phase 1b resident spawn effector, contract §4 (runner exports MC_SPINE into the harness env) vs §6 (run.json fixed schema has no spine field)
- Gap: the contract makes the runner export MC_SPINE but gives it no source: run.json's schema is fixed and spine-less
- Choice: the resident passes `-e MC_SPINE=<config.spineDbPath>` on `docker run`; the runner inherits it from its own env. Conservative: leaves the §6 schema byte-identical, uses the ordinary container-env channel, one-line to change if the runner builder chose another convention (reconcile at e2e integration)
- Spec impact: none; contract §4/§6 could name the channel
- Needs your decision: no

## 2026-07-11 — run.json materialized inside the session dir
- Where: Phase 1b resident spawn effector, contract §6 / spec §11.3 (materialize-at-spawn; no host path named)
- Gap: no pin for where run.json lives on the host before its RO bind-mount
- Choice: `<mcHome>/sessions/<run_id>/run.json` — one mkdir, removed with the session dir. Side effect: also visible in-container at /mc/session/run.json (RW mount) alongside the canonical RO /mc/run.json; harmless for the skeleton, trivial to relocate
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — skeleton brief is a deterministic placeholder string
- Where: Phase 1b resident spawn effector, contract §6 ("brief": "<prepared opening brief text>") / §4 (env interpolation is the fake family's brief comprehension)
- Gap: no verb or config source supplies brief text in the skeleton; the fake harness never reads it (scripted behaviors use env), but the field is in the fixed schema
- Choice: `"Skeleton run <run_id>: role=<role>, subject=<id|none>"` — deterministic, non-empty, clearly synthetic. Real brief templates are a required Phase authored artifact (spec §9.2, Inv. 20) and will replace this
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — no lockfile: bun refuses to write one for a zero-dependency package
- Where: Phase 1b resident deliverables, contract §8 ("package.json + lockfile, §16.1")
- Gap: `bun install` (and `--save-text-lockfile`) emits no bun.lock when there are no dependencies — there is nothing to lock
- Choice: ship package.json only, zero dependencies (also satisfies "no external deps if avoidable"); a lockfile appears automatically with the first real dependency
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — `mc lock get` + `mc run list` read verbs; contract §2 read-channel list corrected
- Where: Phase 1b integrator; contract §7 ladder (asserts lock owner, heartbeat advancement,
  runs-row birth/end, session locators) vs contract §2 ("mc task get / mc packet list …
  the e2e's only assertion channel")
- Gap: the contract pinned ladder assertions its own verb list could not observe; the e2e
  cannot open the spine volume (forced faithfulness, Inv. 24)
- Choice: two pure reads under §18's `mc <record> get/list` umbrella (verbs/reads.go), and
  amended the contract §2 table + §7 fixture text to name them. Conservative: reads only,
  no new write surface, keeps mc the sole spine window; alternatives (sqlite in the helper,
  volume inspection) bypass mc or the lock domain
- Spec impact: none (§18 already names the generic record get/list surface)
- Needs your decision: no

## 2026-07-11 — untagged doc.go in mc/e2e so the gate reads cleanly from the untagged toolchain
- Where: contract §1 build-tag gate; `go test ./e2e/...` without the tag
- Gap: a package whose every file is build-tagged out makes explicit untagged invocation exit 1
  "matched no packages" (the `./...` wildcard was always fine)
- Choice: one untagged doc.go (no code) so the fast lane reports `mc/e2e [no test files]`;
  gate verified both ways
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — image pins: Bun exact, tini exact-prefix via apt, git pinned as an enforced floor
- Where: contract §1 "pins as exact-version ARGs (§11.2)"; runner/image/Dockerfile
- Gap: the git-core PPA publishes only its current build — an exact apt version string breaks
  on every upstream release, which is churn without determinism
- Choice: ARG BUN_VERSION=1.3.9 (exact), ARG TINI_VERSION=0.19.0 (apt glob), ARG GIT_MIN=2.48
  enforced with dpkg --compare-versions at build (build fails if the PPA ever regresses below
  §6.2's floor). Conservative: fail-closed at build time, trivially tightened later
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — mc-land resolves the worktree path from git's own registration, not an argv path
- Where: spec §7 "remove worktree + branch by exact path"; land effect JSON (contract §2)
  carries branch/sha/target only
- Gap: no component knows the worktree path at land time except git itself
- Choice: `git worktree list --porcelain` filtered by the exact branch ref — the registered
  (exact) path, never a guessed convention path; absent worktree is tolerated, branch delete
  uses -d (merged-only). Reversible when a later phase adds the path to the effect JSON
- Spec impact: none
- Needs your decision: no

## 2026-07-11 — runner emits the first heartbeat at session-start, then every interval
- Where: contract §4 ("emits mc heartbeat every heartbeat_interval_s … starting after
  session-start"); runner/agent-runner/main.ts
- Gap: "starting after" does not say whether the first beat is at t=0 or t=interval
- Choice: immediate first beat + interval thereafter — halves the spawn-watchdog exposure
  (spawn_grace_s 5 s vs docker-run + double bun boot latency) with no invariant contact
  (heartbeats can never extend hard_deadline_at, Inv. 1)
- Spec impact: none
- Needs your decision: no

## 2026-07-12 — run.json relocated outside the session folder; normal-exit removal deferred
- Where: Phase 1b fixes (spec §4 file plane, §11.3, Inv. 26); adversarial
  review finding on resident/src/effects.ts
- Gap: the skeleton materialized run.json INSIDE sessions/<run_id>/,
  creating a writable alias of the RO /mc/run.json mount (defeating the
  §11.3 per-container grain) and permanently polluting the trace-only
  folder. The earlier "run.json materialized inside the session dir" entry's
  "Spec impact: none" was wrong.
- Choice: materialize at MC_HOME/runs/<run_id>.json (sibling dir), mount
  only that file RO; remove it in the reap effector. "Removed with the
  container" on NORMAL exit is deferred: the skeleton resident is
  fire-and-forget (docker run -d --rm) with no container-exit hook — a
  leftover runs/<id>.json sits outside every session folder, so Inv. 26 and
  the §11.3 grain hold now; full removal lands with the Phase 2/3 lifecycle
  work (orphan sweep / exit observation). Conservative: least new
  machinery, fail-closed, easy to finish later.
- Spec impact: none (spec text stands; contract §5/§6 updated to match)
- Needs your decision: no

## 2026-07-12 — agent container named mc-run-<run_id>, not §11.1's mc-<worksource>-<run_id>
- Where: Phase 1b topology (spec §11.1 naming, §11.6 exact-name stop);
  contract §1 pinned the name without logging the deviation
- Gap: §11.1 names instances mc-<worksource>-<run_id>, but subjectless runs
  (Strategist propose; future Homie tier) carry no worksource, and the reap
  effect carries only run_id — the spec pattern has no defined value for
  the very runs the skeleton spawns.
- Choice: keep mc-run-<run_id> (resident/src/effects.ts spawn+stop,
  mc/verbs/verbs.go newRunID comment, e2e filter). Functionally safe: the
  §11.6 orphan sweep and pre-spawn assertion are label-keyed
  (mc-managed/mc-tier), and spawn/stop are self-consistent. Renaming later
  touches two components + the e2e; operator tooling written against
  §11.1's literal pattern must use the labels (which the spec also
  mandates) or this name.
- Spec impact: §11.1 "named mc-<worksource>-<run_id>" → "named
  mc-run-<run_id>" (or define the subjectless form)
- Needs your decision: no (log-and-go; flag if you want the spec's literal
  pattern for worksource-bearing runs)

## 2026-07-12 — Codex takeover audit found four Phase 1 invariant defects
- Where: cross-harness takeover review required by AGENTS.md §2, before
  building on Phase 1; spec §7/§9/§18 and Inv. 4/10/11/18/25/26
- Gap: the outgoing implementation was green by its existing tests but (1)
  role terminals compared the caller-supplied `--run` only to the live lease,
  never to `run.json`'s caller identity; (2) `mc-land` could merge main and
  then fail during cleanup, causing the resident to report a false landing
  failure; (3) the runner fire-and-forgot permanent session-locator
  registration before `process.exit`; and (4) raw packet archive/unarchive
  could free and resurrect queue slots independently of task decisions.
- Choice: stop the Phase 2 spine and repair all four red-first before relying
  on Phase 1: bind every terminal token to the caller identity before lease
  fencing; make pre-merge checks fail closed and treat post-merge cleanup as
  cleanup debt rather than a failed merge; await locator registration before
  runner exit; and enforce packet archival as a one-way consequence of owning
  task archival. These are the smallest changes that restore the written
  invariants and are straightforward to reverse locally.
- Spec impact: none (the implementation and its tests were wrong; the spec is
  explicit)
- Needs your decision: no

## 2026-07-12 — Phase 2 wave-1 adds three temporary carrier fields
- Where: Phase 2 domain layer; spec §7/§8/§10/§16.3; NOTE(P2.1–P2.3)
- Gap: the spec requires a stored Daily Console schedule, durable Verifier
  evidence/correction/deepening records, and revise/refine notes in the next
  brief, but does not assign physical columns; the final config/onboarding
  layer does not exist yet.
- Choice: add `lock.console_hour/console_minute/console_tz` with hour 24 as a
  fail-closed not-configured sentinel; add verdict/evidence/correction/
  deepening fields to the Verifier's permanent `runs` row; add overwrite-only
  `tasks.refine_notes`, cleared on packaging. This keeps each terminal one
  transaction and avoids a new table. Onboarding later absorbs the console
  values into the §16.3 config source.
- Spec impact: none for verdict/notes storage; §16.3's file-backed schedule is
  temporarily implemented in the lock row until onboarding lands
- Needs your decision: no

## 2026-07-12 — Refinement judgment applies at the rally-ending verdict
- Where: Phase 2 task/packet aggregates; spec §8; ADR-001 open question 2
- Gap: §8 defines genuine deepening versus churn but does not pin the exact
  transaction that updates `refine_streak`.
- Choice: derive a refinement round from the task's live packet and apply the
  streak at PASS/BUDGET-SPENT, where the Verifier supplies `--deepening` and
  the rally ends. CORRECT does not update the streak; BUDGET-SPENT is always
  churn. This needs no carrier column and keeps judgment with the Verifier.
- Spec impact: none
- Needs your decision: no

## 2026-07-12 — Refiner re-entry uses `mc complete --status seeded`
- Where: Phase 2 CLI/domain integration; spec §8/§18; ADR-001 D4 pattern
- Gap: §8 gives Refiner one terminal action (scope a deepening and re-enter
  packaged→seeded), but §18 and ADR-001 name no dedicated role-side verb.
- Choice: use `mc complete <task> --status seeded --outputs <scope>` with a
  Refiner role fence. It is the same subject-status terminal pattern as an
  initiative done-declaration and keeps re-entry in one transaction.
- Spec impact: §18 should explicitly include the seeded Refiner arm
- Needs your decision: no

## 2026-07-12 — `mc complete --correction-count` is accepted grammar with no writer
- Where: Phase 2 two-budget ownership; spec §7/§10/§18; ADR-001 verifier
  verdict design
- Gap: §18 lists `--correction-count` under `mc complete`, while the delegated
  role verb makes `mc verifier verdict` the sole quality-budget writer.
  Implementing both would blur the two-budget ownership and allow competing
  correction arithmetic.
- Choice: parse but reject `--correction-count`; all correction changes go
  through the fenced Verifier outcome. Conservative by Inv. 10 and easiest to
  reverse if §18 later assigns the flag a non-writing meaning.
- Spec impact: §18 should remove or define `--correction-count`
- Needs your decision: no

## 2026-07-12 — Routing closure crosses the Phase 2 wave-1 directory fence
- Where: Phase 1b Ambiguity A5; Phase 2 takeover review; spec §9.1/§16.2;
  phase2-contract §7 said resident/runner were untouched in wave 1
- Gap: the wave-1 contract omitted the Phase 1-explicit routing deferral, and
  its directory fence would leave `mc dispatch` hardcoding fake plus the
  resident hardcoding fake into run.json — unresolved routing and a false
  permanent runtime locator.
- Choice: the spec wins: resolve the strict ADR-007 routing table in `mc`
  before lease claim, carry harness/model_binding in the spawn effect, and
  make the resident copy those resolved fields. The fake family exists only
  in explicitly build-tagged CLI/E2E binaries; production cannot resolve or
  fall back to it. Runs store the immutable `harness/binding` locator.
- Spec impact: none; phase2-contract §7's ownership fence was incomplete
- Needs your decision: no

## 2026-07-12 — Canonical routes refuse the fake-only execution skeleton
- Where: Phase 2 takeover review; spec §9 Runtime Adapter; Phase 1 fake harness
- Gap: routing now truthfully records canonical harness/binding choices, while
  the resident image and agent runner do not gain real Codex/Claude adapters
  until the later container/runtime phase. Without an interim fence, a Run
  recorded as canonical would actually execute the fake harness.
- Choice: both resident and runner refuse every non-`fake/fake` spawn before
  any fake adapter effect. The explicitly test-tagged fake E2E remains green;
  canonical routes can execute only after their real adapter registry lands.
  This preserves trace truth and is trivially removed adapter-by-adapter.
- Spec impact: none; this is the fail-closed state between ordered phases
- Needs your decision: no

## 2026-07-12 — Phase 2 Console targets the core dashboard pending surface config
- Where: Phase 2 `mc console publish`; spec §14/§15.5/§16.3;
  phase2-contract A-P2-5
- Gap: Console publication must resolve alert-class routing inside trusted
  code, but the authoritative `config.toml` and onboarding surface layer do
  not land until Phase 5. Letting the Strategist pass destinations would move
  policy into an untrusted role.
- Choice: resolve the always-enabled dashboard as the sole Phase-2 Console
  destination. The resolver is private to `mc`; Phase 5 replaces it with the
  configured enabled-surface route without changing the terminal or outbox
  schema. Conservative: it delivers through the required core surface,
  preserves the policy boundary, and cannot accidentally send to an
  unconfigured Discord destination.
- Spec impact: temporary staging only; full §16.3 alert-class expansion lands
  with `config.toml`/onboarding
- Needs your decision: no

## 2026-07-12 — Active Homie bindings are unique per concrete surface place
- Where: Phase 2 Homie registry takeover audit; spec §15.4; prior
  NOTE(P1.19)
- Gap: the Phase-1 partial index included `session_id`, so two active sessions
  could both own the same `(surface, channel_ref)`. The spec routes inbound
  traffic to "the session bound" to a place, making that state ambiguous.
- Choice: make active ownership globally unique on `(surface, channel_ref)`;
  preserve historical bind events, prohibit row rewriting/reactivation, and
  automatically deactivate a session's bindings when it ends or is reaped.
  No implicit transfer exists: a conflicting session is refused until the
  current session ends. This is the fail-closed, least-invented reading.
- Spec impact: §15.4 could state the global uniqueness explicitly
- Needs your decision: no

## 2026-07-13 — Cross-harness takeover review of the Codex wave-2 surface
- Where: AGENTS.md §2 takeover review of 99b0a9a..2f85fbe (wave-2 surface +
  spot-check of four wave-1 self-review claims); six decorrelated lenses
- Gap: two majors and a set of backstop/ordering gaps survived the outgoing
  session's own review: (1) the Homie `worksource pause|archive` allowlist
  cell checked the status value (`worksource.paused/.archived`) instead of
  the frozen verb name — the spec-§15.3 capability was unreachable and
  untested; (2) `nextLanding` gated on Worksource `active`, but §10 (0c) has
  no status qualifier — with archive terminal and no unpause verb, approved
  work stranded forever and its packet burned an Inv. 18 slot (three rows
  wedged dispatch at queue-saturated); (3) task/initiative add accepted
  paused/archived Worksources, silently filing permanently-invisible rows;
  (4) `land report` and `outbox poll|ack` opened/created the spine before
  their scope check (contract §1 ordering); (5) zero-args and wrapper-only
  delegation failures skipped the stdout JSON envelope; (6) the outbox had
  no substrate fences and conversation rows had no active-session INSERT
  backstop; (7) no test exercised the untagged `ActiveRegistry()`.
- Choice: all fixed red-first in a329c1a..63f7c8e (landing ungated — the
  spec's text; intake refused with new stable slug `worksource-inactive`;
  scope preflights hoisted to match dispatch/init; envelopes completed;
  no-delete/immutability/delivered-at-set-once outbox triggers plus a
  conversation active-session trigger; untagged-registry regression test;
  weak review-claim assertions strengthened). Wave-1 spot-check: all four
  claims (CAS, runner lifecycle authority, brief immutability, route truth)
  HOLD on behavior.
- Spec impact: §5 could state pause/archive intake+selection semantics and
  that landing is exempt; §15.4 could pin conversation-append liveness
- Needs your decision: no

## 2026-07-13 — Corrupt stored Console timezone halts all free-lock dispatch
- Where: takeover review of `verbs.Dispatch` step (0b); spec §10/§14
- Gap: Codex made `time.LoadLocation` failure on the stored `console_tz`
  abort the whole free-lock dispatch (no spawn/land/re-enter), not just the
  Console carve-out; the blast radius was never logged. Reachability is
  low (init validates the zone; tzdata is compiled in), and step-(0) reap
  precedence is preserved, so a stale lease can never wedge.
- Choice: keep the whole-dispatch fail-closed abort (a corrupt spine row is
  evidence of deeper corruption; halting is the conservative read) and log
  it here rather than silently narrowing to skip-Console-only.
- Spec impact: none
- Needs your decision: no

## 2026-07-13 — Cross-midnight Console publish consumes the next day
- Where: takeover review of `consoleDue`/`ConsolePublish`; spec §14
- Gap: suppression keys on the event's local calendar day. A dispatch just
  before local midnight whose run commits just after it stamps
  `daily.briefing` on day D+1, suppressing D+1's delivery (operator gets
  day-D content stamped D+1, then nothing until D+2). This follows §14's
  literal same-day rule; the edge is spec-inherent, untested, and at most
  one day of latency, self-healing.
- Choice: log-and-go; no code change. Pinning commit-time vs dispatch-time
  day semantics is a spec question not worth inventing an answer to.
- Spec impact: §14 could pin which instant's calendar day suppresses
- Needs your decision: no

## 2026-07-13 — Console content path is lexically validated; serving seam owes containment
- Where: takeover review of `ConsolePublish` path validation; spec §15.5
- Gap: `outputs/`-relative validation is string-only (traversal/absolute
  forms are all refused) but nothing resolves against the real file plane —
  a symlink planted inside `outputs/` by the publishing container would pass
  and be followed later by whatever serves `content_path`. No delivery-side
  component exists yet (dashboard is Phase 3+), so there is no exploit today.
- Choice: record the obligation now: every consumer of an outbox
  `content_path` must resolve-and-contain under `MC_HOME` before serving.
  The payload is implicitly MC_HOME-relative; that convention is now named.
- Spec impact: none (implementation obligation)
- Needs your decision: no

## 2026-07-13 — Homie/pipeline id disjointness is mint-time only
- Where: takeover review of `mc homie start` id mint; ADR-009
- Gap: `h-` prefix disjointness from 16-hex pipeline run ids holds through
  `mc`'s generators, but neither `homie_sessions.id` nor `runs.id` carries a
  schema CHECK pinning its shape — ADR-009's "disjoint" claim has no
  storage backstop. No mc code path can collide them today.
- Choice: log-and-go (informational). Prefix CHECKs are cheap but invent
  schema the spec doesn't ask for; revisit if a second writer ever appears.
- Spec impact: none
- Needs your decision: no

## 2026-07-13 — A promoted operator initiative dead-ends while the wave verb is parked
- Where: takeover review of `initiative add` + dispatch; parked
  initiative-wave line (ADR-001 open question 1)
- Gap: nothing gates Editor promotion of an `initiative`-scope proposal, so
  dispatch will spawn Strategist(initiative) whose only wired terminals are
  a zero-children done-declaration (strict drain passes trivially) or
  blocking out. A live harness could ship a bogus zero-wave arc packet
  through the ordinary review chain. The parked hole itself stays sealed —
  `strategist wave` is not CLI-wired, so no child can bypass Editor review.
- Choice: log and extend the Parked entry rather than inventing a promotion
  gate: the operator's plan-review decision will define the wave lattice,
  and a zero-wave arc still crosses Verifier and operator approval before
  anything lands. Operators should simply not file initiatives until the
  parked decision resolves.
- Spec impact: none
- Needs your decision: no (folded into the existing Parked decision request)

## 2026-07-13 — Homie-issued interrupt leaves the container to the future orphan sweep
- Where: takeover review of `mc task interrupt`; spec §15.3/§11.6
- Gap: the returned stop-container effect is actionable only when the
  resident invokes the verb; §15.3's sanctioned Homie path strands the
  effect inside the Homie container, so the interrupted pipeline container
  keeps running (records-level Inv. 1 holds; container-level §11.1 does not)
  until the §11.6 orphan sweep lands. The skeleton resident has no sweep yet.
- Choice: log the interim window as a named deferral riding the existing
  Phase 3 resident-hardening line; no interim mechanism invented.
- Spec impact: none (§11.6 already owns the answer)
- Needs your decision: no

## 2026-07-13 — Worksource add ships without the §5 connect-time advisory
- Where: takeover review of `mc worksource add`; spec §5/§18
- Gap: §5's connect-time secrets advisory and the non-repo git-init/read-only
  flow are onboarding-section-6 machinery; the standalone verb currently
  records the row only. §18 calls standalone add "reusable" beyond
  onboarding, so the obligation attaches to the verb eventually.
- Choice: defer to the §17 onboarding/`install.sh` deliverable where the
  interactive flow lives; the bare record verb stays record-only until then.
- Spec impact: none
- Needs your decision: no

## 2026-07-13 — The only image build path bakes the fake-routing tag
- Where: takeover review follow-on to the untagged-registry gap;
  runner/image/build.sh
- Gap: `build.sh` compiles the in-container `mc` with
  `-tags test_fake_routing` — correct for the `mc-fake-e2e` image it
  produces, but it is currently the only image recipe, and a Phase 3+
  production image derived from it would ship the fake family inside the
  container's mc.
- Choice: record the obligation: the Phase 3 production image gets its own
  untagged build path, and the fake tag stays confined to images named for
  it. The new untagged `ActiveRegistry` regression test pins the binary side.
- Spec impact: none
- Needs your decision: no

## 2026-07-13 — Quota-boundary onboarding was red and weakened §16.4/§17
- Where: cross-harness takeover review of 76b3694; Phase 2 wave-2
  operational-verb slice; spec §16.1/§16.4/§17; AGENTS.md §2
- Gap: the pushed commit did not compile and omitted its CLI dispatcher and
  cited ADR. Its initial schema and meta writes were not one transaction;
  it created MC_HOME and opened a foreign spine with WAL pragmas before
  validating meta; it neither wrote nor compared the deployment UUID
  mirror; its lexical git fence rejected the spec-permitted ignored-root
  case but could be bypassed through a symlink; an inputless surfaces
  section reported success while preserving the hour-24 disabled-Console
  sentinel; identical explicit tunable/surface replays rewrote state; and
  first-Worksource setup could silently reuse a `default` sandbox profile
  whose workspace root disagreed with the requested one.
- Choice: treat 76b3694 as a red quota checkpoint, not an accepted slice.
  Keep the Phase-2 direct-file spine and explicit runtime-auth/container/
  supervision doubles delegated by the wave-2 contract, but close every
  deterministic local-state defect red-first: read existing spines without
  mutation, atomically create schema+meta, persist/compare the UUID mirror,
  resolve symlinks and honor `git check-ignore`, require the dual-input
  Console schedule until configured, skip identical writes, and refuse a
  conflicting profile. This preserves the fail-closed posture, tracks the
  spec most closely, and leaves the named-volume effects reversible for
  their scheduled phase.
- Spec impact: none; ADR-015 names the intentionally staged Phase-2 effects
- Needs your decision: no

## 2026-07-13 — Ambiguous container-stop failure awaits the orphan sweep
- Where: Phase-2 split-brain convergence, `container start / before first
  heartbeat`; spec §11.6–§11.7; resident/src/effects.ts reap effector
- Gap: the skeleton resident currently logs every resolved nonzero
  `docker stop` result as if the exact container were already absent, then
  removes its launch envelope. A transient or outcome-ambiguous stop can
  therefore leave the old container alive after `mc dispatch` has already
  committed the reap and freed the lease; a later spawn could coexist with
  that zombie. A rejected stop skips envelope removal, but still cannot
  restore the already-committed lease. The deterministic single-fault row
  proves the specified successful-stop and confirmed-absence paths only.
- Choice: keep the Phase-2 fixture honest with a stateful Docker model and
  do not equate arbitrary failure with absence in its assertions. Close the
  composed-failure window in the scheduled Phase-3 §11.6 implementation:
  label-scoped orphan cleanup at every tick start plus the pipeline
  pre-spawn at-most-one assertion. Those mechanisms re-establish container
  truth before any replacement; changing only the reap error branch cannot.
- Spec impact: none (§11.6 already specifies both required mechanisms)
- Needs your decision: no
