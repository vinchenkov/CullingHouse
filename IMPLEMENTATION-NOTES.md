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

## 2026-07-13 — Generic worktree assignment is not implemented by the skeleton
- Where: Phase-2 split-brain `workspace bytes / git commit / complete` rows;
  spec §6.2, §10, §11.5, Inv. 25; Phase-1 Docker e2e fake Worker
- Gap: neither the resident nor agent runner creates or assigns a task
  worktree; the skeleton mounts the whole Worksource, while its scripted
  Worker unconditionally runs `git worktree add -b` and invokes `mc complete`
  in a later fake-harness step. On retry, the add can fail because the
  branch/worktree already exists, yet the fake harness deliberately records
  a shell failure without aborting later steps, allowing that e2e behavior
  to claim `worked` without proving Git convergence. The immutable brief
  carries no assigned worktree path before completion. Initiative shared
  worktree provisioning remains additionally Parked with the wave model.
- Choice: scope the Phase-2 fast convergence proof honestly to a standalone
  scripted Worker: the fresh attempt inspects the canonical existing Git
  state and performs reconciliation plus fenced `mc complete` in one
  `set -eu` step. Keep Git semantics out of the mechanical runner. The
  Phase-3 mount/worktree implementation must make "assigned worktree"
  concrete before canonical harness acceptance, and the Docker e2e behavior
  must become retry-safe when crash recovery is promoted there; any frozen
  directive/brief change follows ADR-008 versioning rather than silent text
  drift.
- Spec impact: none (this is named incomplete implementation, not a changed contract)
- Needs your decision: no

## 2026-07-13 — Landing cleanup debt is visible but not yet a durable health record
- Where: Phase-2 split-brain `merge success / cleanup or report gap`;
  runner/image/mc-land; resident/src/effects.ts; spec §7/§13
- Gap: an exact successful merge is irreversible even when later worktree or
  branch cleanup fails. The current Phase-2 resident preserves success truth
  and exposes the residue through `mc-land` stderr and its health log, but it
  does not write a durable cleanup-debt row. If the residue is dirty, locked,
  or its ref has moved, safe replay must preserve it; after the success report
  archives the task, ordinary landing dispatch no longer retries that debt.
- Choice: make the report-loss path idempotent from an immutable exact Git
  receipt: a target first-parent two-parent merge whose second parent is the
  verified SHA and whose message and author/committer identity bind the full
  branch/SHA/target tuple. The fresh merge pins the strategy, disables
  autostash and hooks, and isolates content-sensitive Git commands from
  Worksource-local, per-worktree, global, and info/attributes configuration
  while retaining the real primary index, working tree, objects, and refs.
  Fresh landing refuses pre-existing executable transforms or core.worktree
  redirects in either checkout; the isolated view also closes their check/use
  race. Recovery never recomputes mutable merge-driver output. Reconcile only
  a still-exact clean task worktree/ref, and preserve any moved, dirty, locked,
  redirected, executable-configured, or index-hidden residue with an explicit
  warning. The Phase-2 acceptance test captures both stderr and resident-log
  visibility without claiming durability. The later System Health/cleanup
  implementation must surface persistent Git residue durably; do not turn an
  already-successful landing into failure or delete ambiguous operator state
  meanwhile.
- Spec impact: none (success truth and exact cleanup remain §7 obligations)
- Needs your decision: no

## 2026-07-13 — Forbidden-env wildcard is a scan shape, not the shipped deny floor
- Where: Phase-3 handoff forbidden-env `*_API_KEY` mechanism; spec §5,
  §16.3, Inv. 13; `docs/phase3-contract.md` §2.2
- Gap: the handoff abbreviates the mechanism as a `*_API_KEY` scan, while
  the winning spec defines the non-removable default guard as exactly
  `CODEX_API_KEY` and `ANTHROPIC_API_KEY` and separately permits
  operator-managed Worksource tool secrets. Treating the handoff glob as an
  unconditional deny rule would reject legitimate names the spec permits.
- Choice: enumerate and classify every wildcard-shaped key in both env
  planes, but reject only the effective extend-only guard whose shipped floor
  is the spec's two names. A sentinel extension proves arbitrary names can be
  added and rejected. This preserves Inv. 13 without silently widening it.
- Spec impact: handoff Part 3 could say “scan `*_API_KEY`; reject the §16.3
  effective guard” instead of using the glob as shorthand.
- Needs your decision: no

## 2026-07-13 — Homie historical trace access preserves folder exclusivity
- Where: Phase-3 mount plan/per-container scope; spec Inv. 22/26, §11.3,
  §15.3; `docs/phase3-contract.md` §4
- Gap: Inv. 26 says every run's trace-only session folder is mounted only
  into its owning container, while §15.3 requires an operator-scope Homie to
  receive read-only mounts across session files. Mounting the whole sessions
  tree would violate the invariant and give a live file a second directory
  alias.
- Choice: keep each folder owner-exclusive and give a Homie only enumerated,
  completed native trace **files** as individual read-only references under
  a deterministic operator-reference tree. Its own session folder remains
  the only RW trace destination. This satisfies the later file-access clause
  while preserving the named invariant and is easy to replace if the spec is
  clarified.
- Spec impact: §15.3 should distinguish individual historical file mounts
  from session-directory mounts.
- Needs your decision: no

## 2026-07-13 — `open+audit` retains a control-address floor
- Where: Phase-3 gateway topology; spec §11.4; ADR-018 Decisions 3–4
- Gap: §11.4 describes `open+audit` literally as “everything passes through
  the proxy and every hostname is logged.” Taken without a destination floor,
  that also admits loopback, link-local metadata, Docker/runtime control, and
  Mission Control's own gateway/admin endpoints. The spec separately requires
  confinement to be fail-closed but does not reconcile the two statements.
- Choice: keep arbitrary public HTTP/IP-literal access with mandatory audit,
  but make loopback/unspecified/link-local/metadata/multicast and discovered
  runtime/control endpoints non-removable denies. RFC1918/ULA HTTP is admitted
  only through an explicit `egress_allow` domain; explicit raw private access
  remains `network_allow`. This preserves the fail-closed invariants, changes
  the least security-sensitive behavior, and is easy to widen later.
- Spec impact: §11.4 should replace “everything” with “everything outside the
  non-removable control-address floor; private HTTP requires an explicit
  domain.”
- Needs your decision: no

## 2026-07-13 — Homie trace projection supersedes individual file mounts
- Where: Phase-3 mount-plan adversarial review; spec Inv. 22/26, §11.3,
  §15.3; ADR-017 Decisions 6–7
- Gap: the earlier individual-file choice preserves folder exclusivity, but a
  permanent trace store eventually exceeds ADR-016's finite mount count and
  would make every later Homie plan invalid. The spec requires both forever
  retention and operator-scope access but does not define a bounded mount
  namespace.
- Choice: supersede only the earlier note's individual-mount mechanism with
  one owner-folder-preserving RO projection root. Each projection entry is a
  hard link on the same `MC_HOME` filesystem to a finalized immutable trace
  and must prove `os.SameFile`; active writers, copies, symlinks, and fallback
  byte materialization reject. The source session directory remains mounted
  only into its owner, there is still one inode/one set of bytes, and the
  derived directory entries are rebuildable by polling. This preserves the
  invariants, deviates least from both clauses, and is reversible if the spec
  later defines a different bounded view.
- Spec impact: §15.3 should name a bounded same-inode historical-trace view
  rather than implying an unbounded number of Docker binds.
- Needs your decision: no

## 2026-07-13 — Helper uses a component label, not an agent tier
- Where: Phase-3 orphan/liveness review; spec §11.1, §11.5–§11.6;
  ADR-016 Decision 7
- Gap: §11.5 says the stateless helper “carries the mc-tier labeling like
  every other container,” while §11.1 defines the closed tier values as only
  `pipeline|homie` and §11.6 uses that label specifically to select the two
  different agent-liveness domains. The helper belongs to neither.
- Choice: label it `mc-managed=true,mc-component=helper` with no `mc-tier`.
  Agent and network-guard containers retain the owning pipeline/Homie tier;
  sweeps query the closed tier/component taxonomy. This prevents a helper
  from masquerading as lease- or registry-owned execution, preserves both
  liveness invariants, and is trivial to revise if a third tier is specified.
- Spec impact: §11.5 should replace “mc-tier” for the helper with the
  component label and reserve `mc-tier` for pipeline/Homie execution
  envelopes.
- Needs your decision: no

## 2026-07-13 — A null-locator Homie preflight refusal is non-terminal
- Where: Phase-3 Homie wake adversarial review; spec §15.4–§15.5; ADR-012
  Decision 2; ADR-016 Decisions 3–4
- Gap: ending a Homie whose first launch was refused or failed before native
  locator registration leaves an ended conversation that ADR-012 correctly
  refuses to resume. The spec's conversation-row fallback is the eventual
  format-churn answer, but its explicit priming grammar is not yet designed;
  silently treating a fresh launch as a native resume would be unsafe.
- Choice: before the first locator only, a stable candidate-policy refusal
  leaves the canonical status active and stores a code plus fingerprint of
  the rejected candidate inputs; the same fingerprint is skipped so it
  cannot starve pipeline work, and any relevant repair makes it eligible.
  A confirmed pre-start runtime failure retains the durable launch generation
  for exact retry. Once locators exist, the ordinary launch-fenced end/resume
  path applies. This preserves the three canonical statuses, avoids inventing
  an implicit lossy replay, and is reversible when the explicit
  conversation-row fallback is authored.
- Spec impact: §15.4/§15.5 should distinguish failure before first native
  registration from exit/failure of an established resumable session.
- Needs your decision: no

## 2026-07-13 — Explicit row resume supersedes null-locator refusal suppression
- Where: second adversarial review of ADR-016; spec §10, §15.4–§15.5;
  ADR-012 Decision 2
- Gap: the preceding fingerprinted-refusal choice is not implementable without
  letting the lock-domain selector observe current host path/config state
  before candidate selection. Unconditionally skipping the marker would never
  notice repair; rechecking it as the oldest candidate would starve lower work.
- Choice: supersede that suppression mechanism with the spec's designed
  conversation-row fallback. Candidate-policy refusal ends every Homie once.
  A host-only explicit `homie resume --from-rows` is legal for a null-locator
  session after repair; it starts a fresh harness primed from a fixed bounded,
  loss-marked completed-row tail, while native resume remains the default and
  never silently downgrades. A committed unstarted launch generation remains
  effect debt for transient pre-start failures. This uses durable records the
  spec already names, preserves the canonical statuses and no-starvation
  posture, and removes an unobservable state predicate.
- Spec impact: §15.4 should give the conversation-row fallback an explicit
  mode/priming contract; ADR-012's deferred arm is now filled by ADR-016.
- Needs your decision: no

## 2026-07-13 — Shared trace projection contains pipeline traces only
- Where: second adversarial review of ADR-017 Decision 7; spec Inv. 22/26,
  §15.3–§15.4
- Gap: removing a projected hardlink before a Homie resumes does not revoke an
  already-open descriptor in another warm Homie. If the source inode is then
  appended as the resumed native session, that descriptor can observe a live
  trace despite the directory entry being gone.
- Choice: restrict the operator projection to finalized, writer-closed
  **pipeline** traces. §15.3 grants Homie read scope across Worksources'
  session files; Homie sessions have no Worksource and expose their durable
  visible history through conversation rows. A Homie still mounts its own
  native folder for resume. This preserves live-trace owner isolation at the
  kernel boundary and is safer than treating polling/reopen discipline as a
  security fence.
- Spec impact: §15.3 should say “pipeline Worksource session files” if that is
  the intended boundary; if cross-Homie native trace access is required, it
  needs a revocable projection mechanism rather than hardlinks/binds.
- Needs your decision: no

## 2026-07-13 — Standalone tasks use a sanitized task-local Git repository
- Where: Phase-3 committed-state mount review; spec §5, §6.2, §7, §11.1;
  ADR-016 Decisions 5–6; ADR-017 Decisions 5–6
- Gap: §5 requires agents to see committed state only, while the worktree and
  landing prose implies that `mc/task-<id>` lives in the real Worksource
  repository before approval. Even with refs filtered, sharing that real
  object database exposes operator-staged, stashed, aborted, manually hashed,
  and other unreachable objects through ordinary Git plumbing.
- Choice: before landing, keep the standalone branch/worktree in an isolated
  task-local repository containing only the object closure reachable from the
  pinned base. Never use local-clone hardlinks or real-repository alternates.
  Worker completion publishes a privileged immutable closure seal; Verifier
  builds in a disposable same-SHA source while its canonical controls remain
  RO. Approved landing alone imports the reviewed closure, CAS-creates the
  real task ref, and performs the required primary-checkout `merge --no-ff`.
  This changes the location, not the reviewed SHA or landing topology, and is
  the smallest reversible design that makes §5 literal. Initiative
  shared-worktree mechanics remain Parked rather than inferred from it.
- Spec impact: §§6.2, 7, and 11.1 should distinguish the pre-landing isolated
  task repository from the real ref that landing materializes.
- Needs your decision: no

## 2026-07-13 — One stale-writer cleanup may precede Console or landing
- Where: Phase-3 dispatch ordering review; spec §10 and §11.6; ADR-016
  Decision 3
- Gap: §11.6 requires orphan cleanup at every tick start, while §10 gives due
  Console and landing nominal one-tick priority. With one effect per tick, a
  setup/landing or mismatched agent survivor can still be an active stale
  writer; selecting new control work first would preserve that unsafe process.
- Choice: after lease/recovered-health reconciliation, return at most one
  deterministic exact-id orphan/ephemeral cleanup before Console, landing, or
  reenter. The next confirmed-absence tick resumes the control table. This can
  delay those actions by one cleanup tick but preserves the fail-closed and
  single-writer invariants, never lets ordinary Homie housekeeping displace
  them, and is easy to reverse if the spec defines a safe concurrent cleanup.
- Spec impact: §10 should name this bounded stale-writer safety exception to
  the one-tick Console/landing latency.
- Needs your decision: no

## 2026-07-14 — Mount targets reject Unicode format/line separators, not just ASCII controls
- Where: Phase-3 cross-harness takeover review of 4380e0d (`mc/boundary`);
  ADR-017 Decision 1 target grammar
- Gap: ADR-017 says a target component contains "no empty, `.`, `..`, colon,
  backslash, NUL, control, or leading-slash component" — "control" is
  unqualified, but the shipped byte-wise check only rejected ASCII controls
  (`< 0x20`, `0x7f`). An adversarial probe confirmed C1 NEL (U+0085), LINE
  SEPARATOR (U+2028), PARAGRAPH SEPARATOR (U+2029), ZWSP (U+200B) and the
  RTL override (U+202E) all passed `ValidateTarget`.
- Choice: reject `unicode.IsControl` (which covers C0 and C1, so U+0085 is
  caught by the ADR's own word) plus the `Cf`, `Zl`, and `Zp` categories.
  `Zl`/`Zp` are line-break-equivalent to serializers that render targets
  line-wise; `Cf` carries invisible reordering/spoofing (U+202E). This is
  strictly narrower than the ADR text, so it is the conservative direction:
  fail-closed, no new acceptance, and a one-line revert if an operator ever
  needs a format rune in a container path. NBSP (`Zs`) stays legal because
  ASCII space is already legal — excluding it would be new scope, not a fix.
  Fullwidth solidus (U+FF0F) also stays legal: it is not a POSIX separator,
  and Docker receives structured bind objects rather than concatenated `-v`
  strings (ADR-017 Decision 3), so it cannot smuggle a path break.
- Spec impact: ADR-017 Decision 1 should say "Unicode control, format, or
  line/paragraph separator" where it now says "control".
- Needs your decision: no

## 2026-07-14 — Takeover-review residue on the pure mount policy (informational)
- Where: Phase-3 cross-harness takeover review of 4380e0d; two independent
  read-only lenses (claims-vs-tests audit, adversarial bypass hunt)
- Gap: neither lens found a major defect, a blocked-floor bypass, an RW-over-RO
  escalation, or a target-collision bypass; the shipped 18-component + 22-glob
  floor is pinned exactly and the star-glob matcher survived ~3M fuzzed cases
  against `path.Match`. Four smaller mismatches between the ledger's prose and
  the code remain worth recording:
  (1) the per-entry `path` UTF-8 check is unreachable — the document-level gate
      always fires first; the test named for it was proving the wrong check.
  (2) "rejects over-limit input before allocation" is literally true only of
      the 256 KiB pre-decode size cap; the 256-entry bound is checked after the
      decoder has already materialized the entries.
  (3) two `[[allow]]` entries may share one identical source `path` (only
      targets are de-overlapped here).
  (4) the pure layer de-overlaps container targets only; allow-root identity
      and overlap are wholly deferred to the identity layer.
- Choice: kept the unreachable check as a redundant guard (removing a
  fail-closed assertion to chase dead code is the wrong direction in this
  system) and commented it as redundant; renamed the mislabeled test to name
  the check it actually exercises; added the missing regressions (exact
  1025-byte target boundary; ASCII-case-insensitive matching of operator
  additions in both directions). (2) is prose, not behavior — the pre-decode
  cap bounds what the decoder can allocate, so no fix. (3) and (4) are the
  next slice's obligation: ADR-017 Decision 3 makes canonical `os.SameFile`
  identity — not string arithmetic — the authority that rejects identical and
  ancestor/descendant-overlapping allow roots. Recorded here so that seam
  cannot be quietly left open.
- Spec impact: none
- Needs your decision: no

## 2026-07-14 — Standalone-task `/workspace` is an RO 0555 task root, not RW scratch
- Where: Phase-3 decorrelated cross-harness review of ADR-016..019; spec §11.3;
  ADR-017 Decisions 5–6. Found independently by two lenses, which is why it is
  recorded once here rather than twice.
- Gap: §11.3's mount table fixes `/workspace` as host-source "container-local
  scratch", access "RW", purpose "staged outputs, evidence captures". ADR-017
  Decision 6's destination table binds the same container path for
  standalone-task roles as an "exact operator-owned mode-0555 task-local
  skeleton root, always RO", and Decision 5 makes that non-recoverable ("uid
  10002 cannot chmod the parent or create a sibling"). Both columns diverge:
  container-local scratch becomes a host bind, and RW becomes RO. The flip is
  undeclared. ADR-017's own deviation note scopes itself to §§6.2, 7, and 11.1;
  the "Standalone tasks use a sanitized task-local Git repository" entry above
  scopes to §§5, 6.2, 7, 11.1 and changes only where the branch lives. Neither
  names §11.3. A grep of this file for `/workspace` or `0555` returned nothing
  before this entry — an absence of text, not a failure to find it. The ADR
  header's delegation claim does not cover the flip either: it is scoped to
  allowlist grammar and collision-free destinations, and §11.3 does not leave
  the access mode unfixed.
- Choice: keep the design and fix the record. The 0555 RO root is the mechanism
  that makes §5's committed-state isolation literal; a root that uid 10002 could
  chmod would not isolate anything, so it cannot also be an agent-writable
  scratch root. The change tightens confinement, strengthens Inv. 22's scoped
  jurisdiction rather than weakening it, and breaks no invariant (§11.3's table
  is not one). The capability §11.3 wanted is relocated, not destroyed: roles
  keep writable ground at `/workspace/artifacts/<target>/<suffix>` (bilateral,
  normally RW), `/home/agent`, and `/mc/records/output`, and the Verifier keeps
  its sealed-tree materialization overlaid RW. Non-standalone roles still get
  the spec's RW container-local scratch directory from the image rootfs, so the
  deviation is scoped to exactly the standalone-task roles. Per §6 this logs and
  goes; only the ledger was incomplete.
- Spec impact: §11.3's `/workspace` row should distinguish the standalone-task
  RO task-root bind from the ordinary RW scratch directory, and say where
  standalone roles' container-local scratch lands.
- Needs your decision: no

## 2026-07-14 — ADR-017 mandates an initiative/child refusal ADR-016 cannot classify
- Where: Phase-3 decorrelated cross-harness review of ADR-016..019; ADR-017
  Decision 4 and its acceptance section; ADR-016 Decision 4
- Gap: ADR-017 requires a plan-tier outcome it gives no way to express. It says
  an "initiative/child candidate needing that path is not eligible for the
  accepted Phase-3 spawn path", and its acceptance section makes the refusal a
  tested obligation: such a candidate "is refused as unsupported rather than
  receiving a standalone worktree, committed projection, or live primary
  fallback". But ADR-016 states "The v1 consequence classes are closed" and its
  three rows (stale/protocol, deployment health, candidate policy) enumerate
  every stable code; none names an unsupported or parked candidate shape.
  ADR-016 never mentions "initiative" or "wave" at all — grep returns zero — so
  nothing elsewhere in it carves the case out, and Decision 3 forbids the escape
  hatch ("It never falls through to another candidate"). ADR-017's own fifteen
  `mount.*` codes have no unsupported-shape member either, and its Decision 4
  grant list is closed and simply lacks a shared initiative worktree, so the
  planner would not naturally emit a rejected mount to carry the refusal. The
  refusal would have to come from somewhere neither document defines.
- Choice: record rather than invent a code. Reachability is real but narrow, and
  the two halves differ: nothing gates Editor promotion of an `initiative`-scope
  proposal, so Strategist(initiative) can be selected today; the wave-child arm
  stays unreachable while `strategist wave` is not CLI-wired. The existing "A
  promoted operator initiative dead-ends while the wave verb is parked" entry
  covers Editor promotion and the zero-wave arc packet, not this boundary-tier
  code, so it does not already declare the gap. No shipped Go code implements an
  initiative-unsupported refusal — no code implements this plane at all — so
  this is a sibling-ADR coherence defect in the design documents, with no red
  test and nothing to revert.
- Spec impact: none — the resolution belongs in the ADRs. The follow-on ADR that
  extends ADR-017's closed table (or the next slice that wires the boundary
  tier, whichever lands first) must either give the refusal a stable code, name
  the existing `mount.*` code it reuses, or state that §10 selection filters the
  shape out before prepare. Recorded so the choice cannot be made by accident in
  code.
- Needs your decision: no

## 2026-07-14 — The ledger's "two explicit spec deviations" undercounts 20a1a50 (informational)
- Where: Phase-3 decorrelated cross-harness review; PROGRESS.md at 20a1a50
  ("phase3: accept boundary architecture ADRs")
- Gap: the ledger entry written in that commit says "The two explicit spec
  deviations—pre-landing task-local Git and one stale-writer cleanup before
  Console/landing—are appended to `IMPLEMENTATION-NOTES.md`". The same commit
  appended eight `##` entries (+155 lines). Two named, eight written. Every one
  of the eight carries a non-"none" Spec impact line proposing concrete spec
  text changes (§11.4, §15.3, §11.5, §15.4/§15.5, §15.4, §15.3, §§6.2/7/11.1,
  §10). The most charitable reading — "deviations from explicit spec text", as
  against gap-filling where the spec is silent — still undercounts: the
  `open+audit` control-address floor narrows §11.4's literal "everything passes"
  row, and "Helper uses a component label, not an agent tier" directly overrides
  §11.5's "It carries the mc-tier labeling like every other container".
- Choice: record the miscount here rather than rewrite the ledger entry, which
  is append-only and correct as a historical record of what that session
  believed. This entry is the correction. No ADR content is at fault and no
  invariant is touched; it is an AGENTS.md §5 ledger-accuracy defect, and it
  matters only because the ledger is the sole cross-session, cross-harness
  memory — six load-bearing Phase-3 deviations were invisible from it. Worth
  noting that the marker one might reach for, a "Spec impact:" line, does not
  distinguish a deviation from an informational entry: it is a mandatory
  template field with "none" as a permitted value.
- Spec impact: none
- Needs your decision: no

## 2026-07-14 — ADR-018 describes separate user namespaces the target does not have
- Where: Phase-3 decorrelated cross-harness review; ADR-018 Decision 1 and
  Decision 8; handoff §4.3 canaries; spike S1 (`spikes/01-setuid-gate`)
- Gap: ADR-018 twice lists "user" among the namespaces that "remain separate"
  between guard and agent — once for the production pair, once for the preclaim
  probe clients. On the pinned target that is false. The handoff makes "no
  user-namespace remap" a permanent canary, and S1's trusted evidence records an
  identity `/proc/self/uid_map` of `0 0 4294967295` — the init user namespace,
  shared by every container. The ADR's own mechanism depends on that sharing:
  `meta skuid` matches the socket's kernel credential through the user namespace
  owning the network namespace, so an agent in a genuinely separate userns with
  a different map would munge uid 10002 to overflowuid and no owner predicate
  could ever match. Read literally, the two sentences are jointly unrealizable.
- Choice: log it as the prose defect it is. The design is right and only its
  description is wrong: the sentence's job is to enumerate what
  `NetworkMode=container:` does *not* share, and the same list includes
  "capability namespaces", which is not a Linux namespace type — it is loose
  isolation-posture prose, not a topology specification. Nothing in ADR-018
  *requires* creating separate user namespaces, and Docker offers no
  per-container userns absent daemon-wide remap, which the handoff forbids
  outright; no implementer would enable remap on the strength of this sentence.
  Nor is it a security defect: a userns-remapped daemon wedges the guard, which
  lands in Decision 8's already-enumerated "actual guard cannot reach ready"
  class with the correct fail-closed consequence (deployment health, no claim,
  no task charge). The cost is an unhelpful error message, not a fail-open. No
  Go code under `mc/` mentions user namespaces, so nothing shipped is
  implicated.
- Spec impact: none — the correction is ADR-side. A future slice must strike
  "user" from both lists and state affirmatively that the shared init user
  namespace is what makes `meta skuid` meaningful, so the enforcement mechanism
  is not left resting on a stated precondition that contradicts it. Adding
  uid-remap/ECI to Decision 8's deployment-health causes would give diagnostic
  parity with §11.7, which already assigns that message to `mc doctor` and
  onboarding; that part is optional, and §11.7's assignment means Decision 8's
  silence is a diagnostics gap, not a spec contradiction.
- Needs your decision: no

## 2026-07-14 — ADR-018's preclaim probe budget permits a tick to outlast the reap bound
- Where: Phase-3 decorrelated cross-harness review; ADR-018 Decisions 6 and 8;
  spec §16.2 `tick_interval_s`, §10, §7, Inv. 3
- Gap: Decision 8 mandates five containers plus a bridge per spawn/wake
  candidate, with create/inspect/start/stop/remove-and-confirm-absent
  sequencing, and makes the fixture unskippable ("A production candidate cannot
  skip this fixture merely because a read-only policy calculation succeeded").
  Decision 6 puts that Docker probe inside a one-use control channel created
  "For each resident tick", whose non-inventory wall allowance is 120 seconds —
  explicitly twice `tick_interval_s`. The spec states the reap-latency bound as
  "threshold + one tick" at 60s, and §10 says a firing that lands while a tick
  is in flight "is skipped, never queued or run concurrently". Those hold
  together only if a tick fits inside 60s. ADR-018's own budget permits a
  compliant, successful spawn tick to consume 120s, at which point a firing is
  skipped and the stated bound no longer holds — likewise §7's "landing latency
  is at most one tick" and Inv. 3's accepted "up-to-one-tick latency". Neither
  ADR-016 nor ADR-018 restates or relaxes the bound anywhere.
- Choice: log and go. The defect is narrower than "the probe blows the bound":
  120s is a timeout ceiling, not an asserted expected duration — the ADR never
  claims the probe takes that long — and the spec never independently guarantees
  any tick completes within `tick_interval_s`, since base-spec ticks already
  start containers and wire proxies. What is confirmed is that ADR-018's text
  sanctions a worst case exceeding the spec's stated bound on a mandatory path
  without saying so. Inv. 3's normative core (one dispatcher, one action per
  tick) is preserved; only its descriptive latency clause is strained, and
  nothing fails open. This is design-document text — no Go code implements the
  preclaim probe.
- Spec impact: §16.2's "reap-latency bounds are threshold + one tick" should say
  "threshold + one tick, where a tick that performs a preclaim proof may itself
  exceed `tick_interval_s`", or ADR-018 should bound the probe under the tick
  interval. The slice that implements the preclaim proof must resolve which:
  measure the real probe cost and either fit it under `tick_interval_s` or amend
  the bound with evidence. Recorded so the latency claim is not silently
  inherited as true.
- Needs your decision: no

## 2026-07-14 — ADR-019 sources its machine budget to the spec, not the handoff (informational)
- Where: Phase-3 decorrelated cross-harness review; ADR-019 header; spec §16.3;
  handoff §4.3 operator-input row 4
- Gap: ADR-019's header reads "the Phase-3 handoff requires resource bounds; the
  spec fixes Docker Desktop ≥4 CPU/≥8 GiB". The spec fixes no such thing: it
  contains zero occurrences of "GiB", "GB", or "CPU", and §16.3's `[container]`
  knob table lists five knobs (runtime, base image tag, package-cache root,
  mount allowlist path, additional blocked patterns) with no resource values.
  The sentence names the handoff separately in the same clause, so "the spec"
  cannot be read as loose shorthand for the `specs/` suite. The real source is
  the handoff's operator-input row, which reads "≥4 CPU / ≥8 GB" — so the header
  also carries a unit slip. The premise is load-bearing, not decorative: the
  range table's ceilings (pipeline max 4000m / 6144 MiB) are sized against that
  budget, so the table's justifying authority is the misattributed one. The
  sibling ADRs all cite the spec by section number; ADR-019 cites "the spec" for
  a figure with no section to cite.
- Choice: record it. The numbers are correct against the handoff, no invariant
  is breached, and no code depends on the header prose — this is a citation and
  traceability defect in one header line, not a design error. Two of the
  header's three attributions are defensible on the merits (the spec does fix
  the setuid preconditions and the browser image, even if labeling them "S1's"
  and "S8's" borrows spike names from the handoff); only the ≥4 CPU/≥8 GiB
  clause is sourced to a document that does not contain it. It matters because
  an auditor tracing the ceilings to their authority would look in the spec and
  find nothing, and because "≥8 GB" is a floor the operator must meet, not a
  capacity the ceilings must sum under.
- Spec impact: none — ADR-019's header should cite the handoff's operator-input
  row and say GB, matching the source.
- Needs your decision: no
