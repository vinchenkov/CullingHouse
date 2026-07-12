# Phase 1b contract — the walking skeleton

Single interface document for the three parallel builders (mc/, resident/,
runner/). Behavioral contract: `specs/mission-control-spec.md` (spec §…);
operating protocol: `specs/implementation-handoff.md` (handoff). **The spec
wins on conflict with this document too** — this file only pins what the
skeleton builds *now*, and every pin cites its source.

**What the skeleton proves** (handoff Part 3, Phase 1(b)): one `origin:user`
task traverses `tick → dispatch → lease → fake-harness Worker → mc complete →
… → packet → approve → land` through the real Go binary, the real resident,
real container topology, and the fake harness — asserting **timer-driven
advancement** (no manual `mc dispatch` calls anywhere in the test) and the
**approve/land split** (§7).

**Marking convention.** `[SKELETON]` = built now, happy path plus the exact
writes the e2e asserts. `[P2]` = deferred to Phase 2 (domain correctness) or
Phase 3 (boundary conformance); parse-and-reject or absent now. The skeleton
needs no error path beyond the fencing rejections listed explicitly.

---

## 1. Topology: what runs where, and the Docker gate

Per §11.5 the spine never leaves the lock domain; per Inv. 24 it lives on a
runtime-local **named volume**. The skeleton honors that literally:

| Piece | Where | Why |
|---|---|---|
| spine (SQLite file) | Docker named volume, created per-test | Inv. 24; §11.5 "the spine never leaves the lock domain". Host processes must not open it (also technically unsound: WAL across the VirtioFS/VM kernel boundary) |
| `mc` (linux/arm64) | baked into the e2e image | §11.2 (baked, never bind-mounted). Setuid bit / privileged uid **[P3]** — skeleton runs one uid; `mc` is sole writer by construction (nothing else links sqlite in-container) |
| warm helper container | `docker run -d --label mc-managed --label mc-tier=helper <image> sleep infinity`, spine volume mounted | §11.5: host-side spine access crosses into the lock domain via the helper |
| host `mc` (darwin) | same Go binary; **self-delegates** | §11.5 "self-delegation, decided": when `MC_HELPER=<container>` is set and the spine is not directly reachable, `mc` re-invokes itself `docker exec -i <helper> mc <argv…>`, passing stdin/stdout/stderr and the exit code through untouched. Auto-detection at startup **[P3]**; skeleton uses the explicit env pair `MC_SPINE` (direct path) / `MC_HELPER` (delegate) |
| resident (Bun) | host process, spawned by the e2e | §15.1; it only invokes `mc` and the docker CLI |
| agent container | `docker run --rm` per run, `--network none`, labels `mc-managed`, `mc-tier=pipeline`, name `mc-run-<run_id>` | §11.1/§11.6 (labels, `--rm`, exact-name stop); `--network none` is the fake family's `egress_policy` (§5 'none') — see §4 below |
| `mc-land` container | short-lived `docker run --rm` per land effect | §7 Landing: "a script baked into the base image and versioned with it" |

**Clock discipline** holds by construction: every timestamp is written by `mc`
with `datetime('now')` inside the transaction (§10), and with the helper
topology all lease math runs in-VM (handoff S7 rule).

**The image.** New minimal image `mc-fake-e2e`, Dockerfile in `runner/image/`:
`FROM ubuntu:24.04` + git ≥ 2.48 via git-core PPA (§6.2 relative worktree
links) + Bun 1.3.9 (the mise pin, §16.1) + tini as PID 1 (§11.2) + `COPY`
of the linux/arm64 `mc` and `mc-land` — pins as exact-version `ARG`s (§11.2).
Built natively arm64, no `--platform` (env facts; S8 pattern). We do **not**
reuse `mcspike-08-base` (2.6 GB): its contents — the two harness CLIs, Node,
Playwright — exist to serve *real* harnesses (§11.2), and the fake family is
"registered as a third harness family **in test configs only**" (handoff
Part 3 tooling). Spec-faithful without a gateway or credentials because the
fake harness is deterministic, token-free, and makes no network calls
(runner/fake-harness/cli.ts header): there is no runtime auth to inject
(§11.4 exists to deliver credentials — the fake has none, so Inv. 16 is
satisfied vacuously), and `--network none` is the strictest legal
`egress_policy` (§5). Runner source is bind-mounted RO at `/app/src`, never
baked (§11.2).

**The opt-in gate: a Go build tag, decided.** The e2e lives in `mc/e2e/` with
`//go:build docker_e2e`; the fast suite (`mise exec -- go test ./...` in
`mc/`) never compiles it, and the phase-completion run is
`go test -tags docker_e2e ./e2e/...`. Build tag over env var because an
env-var skip still compiles Docker-touching code into the fast lane and a
typo'd variable silently skips forever, while a tag keeps the fast lane
Docker-free *by construction* (AGENTS.md §3) and is greppable. Bun suites
(resident, fake harness) are Docker-free unconditionally.

---

## 2. The `mc` verb surface (skeleton-minimal)

Derived from §18 and ADR-001 — no invented names except `mc init` (flagged,
see Ambiguity A1). Common contract: single JSON object on **stdout** per
invocation (§10: effect data is "the ordinary command result"); diagnostics
on stderr; exit **0** success, **1** domain rejection (fencing mismatch,
validation, illegal transition — the substrate abort surfaces here), **2**
usage/environment error. Every mutation runs under `BEGIN IMMEDIATE` on the
S5 connection discipline (`mc/substrate.Open`). Identity: tier/role read
**only** from `/mc/run.json` when present (§11.5, ADR D2); no `run.json` ⇒
host scope. Full per-container refusal matrix **[P3]**; skeleton implements
the checks below.

### Host scope (reach the spine via self-delegation → helper)

| Verb | argv / stdin | Spine writes | Notes |
|---|---|---|---|
| `mc init` **[SKELETON-ONLY]** | `--spine <path>` seeds via flags: `--worksource <id> --workspace-root <path>` + tunables `--tick... --timeout-minutes --grace-minutes --heartbeat-interval-s --spawn-grace-s --hard-deadline-minutes` | applies `substrate.Schema` (`substrate.Init`), inserts `meta`, one `sandbox_profiles` + `worksources` row, updates `lock` tunable columns | Provisioning is really `mc onboard`'s (§17, §16.4); see Ambiguity A1 + deviation note |
| `mc task add <title>` | `--worksource <id> [--description …] [--priority N]` → `{"task_id":N}` | INSERT tasks: `status='proposed'`, **`origin='user'`** (§18: task add files origin:user) | operator files the skeleton's one task |
| `mc dispatch` | no args → effect JSON (below) | claim (CAS on free lock + INSERT runs row, Inv. 4) / reap writes / reenter update — all in one transaction | §10; the only verb that calls `Decide()`. Losing racer aborts before effect data (§10 fencing). Process flock **[P2]** |
| `mc packet decide <task>` | `--approve` (`--revise/--cancel [--reason]` parse but exit 1 "deferred" **[P2]**; reason asymmetry validated now — required for revise/cancel, forbidden for approve, §7) | approve: `decision='approved'`, `decided_at` — a **pure state write**, no dispatch, no filesystem effect (§7, Inv. 2). Landing-pending is *derived*, not a column (schema NOTE(P1.9)): approved + branch + packaged + unarchived + unblocked. Branchless task: archive synchronously (§7) — skeleton's task has a branch, so this arm is **[P2]** |
| `mc land report <task>` | `--status success\|failure [--reason …]` | success: `archived=1` (trigger cascades to packet), lease untouched (landing holds no lease, §7). failure: `blocked=1, blocked_reason` **[P2 tests; write implemented]** | ADR D6 row "`mc land report` (resident reports landing result, §7)" |
| `mc task get <id>` / `mc packet list` / `mc lock get` / `mc run list` | → row(s) as JSON | none (reads) | §18 `mc <record> get/list`; the e2e's only assertion channel into the spine (it cannot open the volume — forced faithfulness). `lock get` / `run list` exist because the §7 ladder asserts lease state (steps 3–5: owner, heartbeat advancement, runs-row birth/end) — integration fix: the original text pinned the ladder but listed only the task/packet reads |

**`mc dispatch` effect JSON** (mirrors `dispatch.Action`, §10 walk):
`{"action":"idle","reason":…}` · `{"action":"spawn","run_id":…,"role":…,
"subject_id":N|null,"worksource":…,"pool_ids":[…],"session_path":…,
"heartbeat_interval_s":N}` · `{"action":"land","task_id":N,"branch":…,
"verified_sha":…,"target_ref":…}` · `{"action":"reap","run_id":…,
"stop_container":true,…}` · `{"action":"reenter","task_id":N}` **[P2]**.

### Pipeline-role scope (in-container; identity from `run.json`, ADR D2)

All fenced: `--run <run_id>` must match the live lease's `run_id` or exit 1,
never double-applied (§10, §18 deny rule 2). Role-matched against
`run.json.role` (ADR D2). Each is the run's **terminal action**: writes the
output, advances status, releases the lease (NULLs the claim columns), stamps
`runs.ended_at`/`outcome` — one transaction (ADR D3; Inv. 10). Never
dispatches (Inv. 3).

| Verb | argv / stdin | Spine writes (beyond the D3 boilerplate) |
|---|---|---|
| `mc complete <task> --run <id> --status worked [--branch <name>] [--outputs <path>]` | Worker terminal | `seeded → worked`; records `tasks.branch` (see deviation note D-2). `--reason/--needs-operator/--infra/--correction-count` (§18) parse-reject **[P2]** |
| `mc complete <task> --run <id> --status packaged [--outputs <path>]` | Packager terminal | `verified → packaged` **and packet birth in the same transaction** (ADR D4 "not new verbs"; Inv. 11; the WIP-cap trigger fires here) |
| `mc editor decide --run <id> --batch -` | stdin `{"verdicts":[{"task":N,"decision":"promote","reason":…}]}` | per ADR D4: promote → `proposed → seeded`; must cover exactly the run's snapshotted pool (stored on the runs row at claim). reject arm + zero-promotion guard **[P2]** |
| `mc strategist propose --run <id> --batch -` | stdin `{"proposals":[…]}`; **empty array legal** | ADR D4 inserts; subjectless lease release (constraint b). In the skeleton the fake Strategist always sends `[]` — see §6 liveness note |
| `mc verifier verdict <task> --run <id> --outcome pass --evidence <path> --sha <sha>` | Verifier terminal | `worked → verified`; records `tasks.verified_sha` (deviation D-2). `correct`/`budget-spent`/`--deepening` **[P2]** (ADR D4) |

### Runner lifecycle scope (§11.5 private scope; never in the model's tools)

| Verb | Spine writes |
|---|---|
| `mc heartbeat <run_id>` | `lock.last_heartbeat_at = datetime('now')` iff `run_id` matches (fenced, §10); can never extend `hard_deadline_at` (Inv. 1) |
| `mc run register-session <run_id> --native-ref <ref> --file <name>` | `runs.native_session_ref`, `runs.trace_filename` (ADR D5; §15.4 locators). **Own-row identity, not lease-fenced** (ADR D6 "(own run)"): fired at session-start, it may lose the race against the behavior's terminal verb releasing the lease, and must still land — a lease fence here would silently lose the locators forever |

Within-container runner-vs-model scope separation is best-effort by decision
(§11.5) and moot for a scripted fake; per-container kernel enforcement **[P3]**.

---

## 3. `Decide()` — where it lives, who calls it

- **Promotion**: `spikes/06-dispatch-table`'s package is copied to
  `mc/dispatch` (import path **`mc/dispatch`**, module `mc`), *whole*:
  `dispatch.go`, `dispatch_test.go`, `property_test.go` (S6 RESULT §"Phase 2
  promotion notes": "promote by moving the package … the suite lifts
  unchanged"). Only the package clause/module wiring changes; `NOTE(S6.n)`
  markers stay. `spikes/06-dispatch-table/` is left byte-identical as the
  frozen spike record; `mc/dispatch` is authoritative from this commit on.
  Its tests join the fast suite (pure Go, no Docker).
- **The one caller is the `mc dispatch` verb** (§10: "`mc dispatch` … executes
  inside the lock domain"; Inv. 2/3). It loads `Records`/`Lock` from the spine
  and `Config` from the lock row's tunable columns inside one
  `BEGIN IMMEDIATE`, reads `Clock.Now` from `datetime('now')` in the same
  transaction (§10 clock discipline), calls `Decide`, applies the action's
  writes, commits, and prints the effect JSON. The **resident never computes
  a decision** — it is "the mechanical hand of a dispatch decision already
  committed in the spine" (§15.1, §10).
- The record-loading queries mirror §10's SQL; `Decide` remains pure and
  I/O-free (Inv. 21).

---

## 4. Runtime Adapter contract — the `fake` family

The adapter contract (§9) as implemented by `runner/fake-harness/cli.ts`
(existing, authoritative; its README.md + `bun test` suite are Phase 1b
deliverables of the runner builder):

- **Invocation**: `bun /app/src/fake-harness/cli.ts --behavior <file>
  --session-dir <dir> [--turn-file <file>] [--session-id <id>]`; the one
  top-level turn arrives on stdin (or `--turn-file`) — the runner supplies
  it from `run.json.brief` unchanged (§11.5 pipeline runner: one turn).
- **Session dir lifecycle**: created by the resident *before* launch (§10
  effect order: folder → run.json → container; §11.3 mount plan), bind-mounted
  RW at **`/mc/session`**; the harness appends `native.jsonl` there **as it
  runs** (append per event — §9: "no post-exit copy, nothing lost if the
  container dies mid-run"; Inv. 26). Host side: `MC_HOME/sessions/<run_id>/`
  (§16.1), `MC_HOME` = a scratch temp dir in tests (env facts: never
  `~/.mission-control`).
- **Completion detection**: the runner reads the stdout event stream *only*
  to detect completion and exit status (§9): `session-start` (yields the
  native session handle → `mc run register-session`), `turn-complete` with
  `status:"success"` then exit 0 = clean turn; process exit with **no**
  `turn-complete` = crash (scripted `crash` step, exit 1–255) — the runner
  exits with the harness and lease recovery does the rest (§10, §11.5: "never
  converts an ordinary harness exit into success"). `hang` = alive, silent
  forever → exercises hard-deadline reaping. Exit 2 = usage/behavior-file
  error; exit 70 = internal invariant breach (both fail the spawn).
- **Env interpolation is the fake family's "brief comprehension".** A real
  role reads its brief; a scripted behavior cannot, so the runner exports
  `MC_RUN_ID`, `MC_ROLE`, `MC_SUBJECT_ID` (empty when null), `MC_POOL_IDS`
  (editor), `MC_SPINE` into the harness env, and behavior `exec` steps invoke
  the real scoped `mc` with them (behavior.ts: "how the fake 'agent' calls
  mc"). This is fake-family adapter mechanics, not a runner power grab: the
  *agent side* of the container still invokes `mc` itself (§10
  completion-is-a-pure-write; §11.5 "the runner never proxies, replays, or
  infers an `mc` invocation").
- **cli.ts vs spec — flags for the builder** (all minor):
  1. cli.ts hardcodes the native filename `native.jsonl`; fine, but the
     runner must register it via `--file` rather than assuming (§15.4 keeps
     the filename as a locator because real harnesses differ).
  2. cli.ts validates `--session-dir` exists but not that it is writable;
     a read-only mount would die on first append — acceptable (fail-closed),
     README should say so.
  3. `hang` never emits heartbeats itself — correct (§10: the heartbeat is
     the **runner's**, never the harness's); README must state it so no one
     "fixes" it.
  4. No deviations found in event ordering or durable-first emit
     (native.jsonl append precedes stdout mirror — correct priority).

**The skeleton agent runner** (`runner/agent-runner/`, Bun, bind-mounted RO,
entrypoint via tini): reads `/mc/run.json` (fixed path, RO — §11.5), starts
the fake harness through the adapter, supplies the one turn, emits
`mc heartbeat <run_id>` every `heartbeat_interval_s` from run.json **starting
after** `session-start` (§10: begins only after the adapter established the
session), registers the session handle, waits for harness exit, exits with
it. It does **not**: poll for work, retry, interpret output, call `mc
complete`, or touch the spine otherwise (§11.5 "It does not" list).

---

## 5. The resident (Bun) — skeleton tick loop

Per §10 "Where dispatch runs": a plain in-process interval timer, default
`tick_interval_s` 60, configurable (`MC_TICK_INTERVAL_MS` for tests); **one
tick at a time** — a firing that lands mid-tick is skipped, never queued. A
tick = invoke `mc dispatch` (host binary, self-delegating) and effect the
returned JSON in order (§10: folder → `run.json` → container; no proxy for
the fake family):

- `spawn` → mkdir `MC_HOME/sessions/<run_id>` (the trace-only folder,
  Inv. 26); write `run.json` at `MC_HOME/runs/<run_id>.json` — a **sibling**
  of the sessions tree, never inside a session folder (spec §4: mounted
  "separately from the session folder"; keeps the RO `/mc/run.json` mount
  free of any RW alias through `/mc/session`, §11.3); `docker run --rm -d`
  the agent container with the §1 mounts/labels.
- `land` → `docker run --rm` the `mc-land` container (workspace mounted at
  the canonical path, §6.2) with `(branch, verified_sha, target_ref)` argv;
  then `mc land report <task> --status success|failure` from its exit code
  (§7 Landing steps 1–3: SHA fence; **HEAD fence** — the script never checks
  out the target, it fails closed unless the primary checkout is already on
  it, §6.2 "the operator's own git work is untouched"; `merge --no-ff` in
  the primary checkout, with `git merge --abort` on a failed merge so
  nothing half-lands and the §7 retry never wedges; remove worktree + branch
  by exact path).
- `reap` → `docker stop mc-run-<run_id>` (exact name, strict charset —
  §11.6 decide-then-effect; only the host holds the control socket), then
  remove `MC_HOME/runs/<run_id>.json` ("removed with the container", §11.3).
- `idle` / `reenter` → nothing.

**Injectable time**: the loop is `startTickLoop(deps)` where `deps` carries
`{intervalMs, setTimer, clearTimer, runMc, docker, log}`; `bun test` drives a
fake timer deterministically (tick-skip-while-in-flight, effect ordering,
spawn/land/reap effect handling with stubbed `runMc`). The Docker e2e uses
the real timer at a short interval — advancement stays *timer-driven*; the
e2e only observes (see §7).

**Must NOT do (ever, §15.1/§10/Inv. 2)**: compute dispatch decisions, parse
natural language, open the spine, convert harness exit into state. **Deferred
[P2/P3]**: helper ensure-running at boot/tick, wake-from-sleep immediate
tick, orphan sweep at tick start (§11.6), outbox delivery, Discord, console
scheduling, `mc backup` chore.

## 6. `run.json` (skeleton schema, §11.5 launch envelope)

Mounted RO at `/mc/run.json`, materialized at spawn on the host at
`MC_HOME/runs/<run_id>.json` — **outside** the session folder, so the RW
`/mc/session` mount never aliases the identity file `mc` trusts (spec §4
separateness; §11.3 nested-RO grain; Inv. 26 trace-only folder) — and
removed with the container (§11.3 materialize-at-spawn; the skeleton removes
it in the reap effector — normal-exit removal awaits a container-exit hook,
see the deviation log). Fixed schema, mechanical read:

```json
{ "run_id": "…", "tier": "pipeline", "role": "worker",
  "subject_id": 42, "worksource": "ws-e2e",
  "harness": "fake", "model_binding": "fake",
  "mode": "fresh", "brief": "<prepared opening brief text>",
  "pool_ids": [42], "heartbeat_interval_s": 1,
  "harness_config": { "behavior": "/mc/behaviors/worker.json" },
  "mounts": { "session": "/mc/session", "workspace": "/workspace/source" } }
```

`harness_config.behavior` selects the scripted behavior (behaviors dir
bind-mounted RO at `/mc/behaviors` from the e2e's fixtures). Role→behavior
mapping lives in the **resident's e2e config** for the skeleton; routing.md
parsing and `mc dispatch`-side binding resolution are **[P2]** (§9.1) — see
deviation D-4.

---

## 7. The e2e (mc/e2e, `//go:build docker_e2e`) — assertion ladder

Fixtures: temp `MC_HOME`; host git repo (the Worksource; `main` with one
commit; `worktree.useRelativePaths=true` per §6.2; `.mc-worktrees/`
gitignored); spine volume + helper + image; tunables shrunk via `mc init`
(tick 500 ms, heartbeat 1 s, spawn-grace 5 s, hard-deadline generous).
The e2e invokes **only** operator/host verbs: `mc init`, `mc task add`,
`mc packet decide`, and the reads `mc task get`/`mc packet list`/`mc lock
get`/`mc run list` — never `mc dispatch` (timer-driven mandate). All state
asserts poll the read verbs with a deadline; git asserts use read-only host
git (§6.2 sanctions this).

| # | Wait for / act | Assert |
|---|---|---|
| 1 | `mc init`, `mc task add` | task `proposed`, `origin='user'`, lock free |
| 2 | start resident | — |
| 3 | tick → Editor spawn | lock held `owner='editor'`, runs row exists (Inv. 4), `MC_HOME/sessions/<run>/native.jsonl` appears |
| 4 | fake Editor promotes (behavior: `mc editor decide` batch over `$MC_POOL_IDS`) | task `seeded`, lock free, `runs.ended_at` set |
| 5 | tick → Worker spawn; behavior sleeps 2.5 s then worktree `+ mc/task-<id>` branch + commit + `mc complete --status worked --branch …` | during run: `lock.last_heartbeat_at` set and **advances** (runner heartbeat, §11.6); after: task `worked`, `tasks.branch` recorded, commit visible on branch from host, `runs.native_session_ref='fake-session'` (register-session) |
| 6 | tick → Verifier; `mc verifier verdict … --outcome pass --sha $(git rev-parse …)` | task `verified`, `verified_sha` recorded |
| 7 | tick → Packager; `mc complete --status packaged` | task `packaged`, packet row unarchived — born in the same transaction (Inv. 10/11) |
| 8 | interim ticks (board drained) | any Strategist(propose) spawns terminate via empty batch; **no new tasks appear** (§10 step 4 fires legitimately; liveness note below) |
| 9 | **approve**: `mc packet decide <task> --approve` | *the split, first half (§7)*: exit 0; **host `main` HEAD unchanged**, asserted first and host-locally — approve is a pure state write, no filesystem effect (Inv. 2). The follow-on spine reads (task `decision='approved'`, `status='packaged'`, `archived=0`, packet unarchived) race the live tick loop and tolerate an already-landed row; step 10 re-verifies every landed invariant |
| 10 | tick → land effect → `mc-land` → `mc land report` | *the split, second half (§7)*: `main` advanced by a `--no-ff` merge whose second parent is `verified_sha`; worktree dir and `mc/task-<id>` branch gone; task `archived=1`, packet archived (cascade trigger); lock free |
| 11 | teardown | resident stopped; `mc-managed`-labeled containers removed; volume removed |

Every stage 3→10 requires a *subsequent* tick after the prior terminal write
(§10: "`mc complete` … never dispatches; the next timer tick selects the
follow-on stage") — the ladder passing under a running timer with the e2e
never dispatching **is** the timer-driven assertion.

**Liveness note (step 8).** Once the board is drained (`packaged` ranks 0),
§10 step (4) legitimately spawns Strategist(propose) each tick until approve.
The skeleton must survive it, so `mc strategist propose` + a
`behaviors/strategist.json` that submits `{"proposals": []}` are in scope —
which also exercises ADR constraint (b), the subjectless lease, for free.

## 8. Directory layout & ownership (disjoint)

- **`mc/`** (Go builder): `cmd/mc/` (main + verb mux + self-delegation),
  `mc/dispatch/` (promoted S6 package + suite), `mc/verbs/` (or per-verb
  packages; builder's choice inside `mc/`), `mc/e2e/` (build-tagged e2e —
  consumes resident + runner strictly through this contract), existing
  `mc/substrate/` suite stays green (155 tests + check.sh). The schema
  gained exactly **two additive, default-preserving columns** during 1b
  integration, both demanded by §2's verb surface:
  `lock.hard_deadline_minutes` (the `mc init` lease tunable dispatch stamps
  into `hard_deadline_at` — schema `NOTE(P1b.1)`) and `runs.pool_snapshot`
  (the Editor pool snapshotted at claim that `mc editor decide` must cover
  exactly — schema `NOTE(P1b.2)`). Any further schema change is out of
  scope for 1b.
- **`resident/`** (Bun builder): `src/` tick loop + effectors, `*.test.ts`
  (fake-timer unit tests), `package.json` + lockfile (§16.1).
- **`runner/`** (Bun builder): `fake-harness/` (existing cli.ts +
  behavior.ts; **add** README.md — the fake family's adapter contract doc —
  and `bun test` suite), `agent-runner/` (skeleton pipeline runner),
  `image/` (Dockerfile + `mc-land` script + build script).

Fast suite = `mc` `go test ./...` (substrate + dispatch + verb unit tests
against temp spine files, no Docker) + `bun test` in `resident/` and
`runner/`. gofmt/vet clean. Docker e2e runs only via `-tags docker_e2e`.

---

## 9. Ambiguities (conservative resolutions pinned)

- **A1 — no provisioning verb in §18.** Onboarding owns provisioning
  (§16.4/§17) but is a Phase 5 deliverable. Pinned: skeleton-only `mc init`
  (host scope), expected to be absorbed by `mc onboard` sections
  `home|worksource|tunables`; single verb, easy to fold/rename. Alternatives
  (driving sqlite directly from the test; docker cp a db) bypass `mc` as sole
  writer — worse. → deviation D-1.
- **A2 — who writes `tasks.branch` / `tasks.verified_sha`.** §7 landing
  consumes them; no verb is specified to record them. Pinned: the Worker's
  `mc complete --branch` records the branch at the moment it becomes real;
  the Verifier's `verdict --sha` records the SHA it verified (§7: "only the
  exact reviewed commit can land" — the SHA is verification-time knowledge).
  → deviation D-2.
- **A3 — worktree location.** §6.2 requires MC worktrees "in the same
  bind-mounted tree as the parent repo" but names no path. Pinned:
  `<workspace_root>/.mc-worktrees/task-<id>`, branch `mc/task-<id>` (§6.2
  names the branch pattern). Revisit in the Phase 2 git-topology work.
- **A4 — `target_ref`.** Never assigned anywhere in the spec. Pinned:
  defaults to `main` at `mc task add` time (column already exists).
- **A5 — routing resolution for a family the routing table doesn't know.**
  §9.1 makes routing.md authoritative and the fake family is test-config-only
  (handoff). Pinned for the skeleton: the resident's test config maps
  role→behavior and `mc dispatch` stamps `binding='fake'`; routing.md
  parsing + reject-unresolved lands in Phase 2. → deviation D-4.
- **A6 — cross-process "injectable clock" for the e2e.** Lease math must use
  the lock domain's clock (§10), so the e2e cannot inject time into `mc`.
  Pinned: injectable timer is a *resident unit-test* affordance; the e2e uses
  the real timer at a short configured interval — still timer-driven, which
  is what Phase 1 asserts. Deterministic-time lease tests live where the
  clock is already injectable: `mc/dispatch` (`Clock`), Phase 2.
- **A7 — empty Editor pool vs step (4).** Between `packaged` and approve the
  board is drained and step (4) fires every tick (container churn ~1/tick).
  Harmless and spec-mandated; bounded by the e2e window. No suppression knob
  is added (that would deviate from §10's walk).
