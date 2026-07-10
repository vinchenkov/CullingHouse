# Mission Control — Implementation Handoff

How to hand `mission-control-spec.md` to implementing agents and get a
finished, verified system back with minimal operator follow-up. The spec
(`specs/mission-control-spec.md`) is the behavioral contract and wins on
conflict; this document is only the operating protocol for the handoff
itself.

The workflow is deliberately simple: **Claude Code is the primary
implementing harness**; when its quota maxes out the operator terminates it
and boots **Codex** in the same repo to continue. Only one harness writes at
a time, so the handoff is manual and sequential — no lock, no gate scripts.
Agents decide when they are done by **finishing the spec's acceptance
criteria** (Inv. 12 / §7), not by any exit code.

Contents: how the handoff runs (Part 1), what the operator supplies before
the first session (Part 2).

---

## Part 1 — Running the handoff

### 1.1 The handoff repo scaffold

Create the fresh implementation folder (working name **"Homie"**, final
software name TBD; path recorded in `OPERATOR-INPUTS.md`) and seed it with
exactly these files before the first agent session. The agent runs `git init`
and builds the repo around them.

| File | Job |
|---|---|
| `specs/mission-control-spec.md`, `specs/implementation-handoff.md` | the contract (spec wins on conflict) |
| `db_schemas.sql` | substrate starting point, marked "starting point, spec wins; predates Arbiter→Editor, Planner→Strategist(initiative) renames, and its Homie-inserts-proposals intake predates Strategist(propose)" |
| `docs/priors/` | copies of `poc/` evidence **and the memory notes as plain files** — Claude Code's memory directory is invisible to Codex, so trusted priors must live in the repo |
| `AGENTS.md` | the canonical standing instruction (§1.2) — Codex reads it natively |
| `CLAUDE.md` | one line: `@AGENTS.md` — Claude Code imports it; both harnesses read the same protocol with zero drift |
| `OPERATOR-INPUTS.md` | the completed Part 2 checklist with real values, not placeholders — every blank in it is a mid-flight question |
| `.gitignore` | seeded **before the first commit**, covering at minimum `OPERATOR-INPUTS.md` (it holds live secrets) and the dev `MC_HOME` scratch path if it lives in-tree |
| `PROGRESS.md` | the ledger (§1.2, §1.4) — empty except the phase list and a `NEXT:` line pointing at the first task |
| `IMPLEMENTATION-NOTES.md` | the deviation log, empty except the entry template (§1.6) |
| `docs/adr/` | ADR template plus the one design the spec delegates: the role-side verbs + verb-by-scope table (spec §18). Standard shape: Status / Context / Decision / Consequences |

Create a **private remote** for the folder and have `AGENTS.md` push after
green commits — long stretches of local-only autonomous work are one disk
failure from gone.

### 1.2 `AGENTS.md` — the standing instruction

This file is the whole autonomy mechanism; both harnesses reload it every
session. It must contain:

1. **Session-start protocol** (the resume ritual): read `PROGRESS.md`,
   `git log --oneline -20`, and `git status` (uncommitted work is data,
   never discard it), run `make test` — if `make test` does not exist yet,
   building it is the current task — then continue from the ledger's `NEXT:`
   line. Never ask what to do next — the ledger says.
2. **Takeover review**: when the previous session ran on the other harness,
   the first substantive act is an **adversarial review of the outgoing
   phase against the spec** before building on it — the system's own
   decorrelated producer/judge principle applied to its own construction.
   Findings go to `IMPLEMENTATION-NOTES.md` (and `## Parked` in `PROGRESS.md`
   if operator input is needed).
3. **The work order and definition of done**: TDD through the spec's phases.
   `make test` gates every commit. **Done means the spec's acceptance
   criteria are met** — a task's checkable success criteria (Inv. 12) mapped
   to concrete evidence by the Verifier's rubric (§7); no ledger item is
   marked complete until its criteria are satisfied. Beyond code and tests,
   the spec also requires four **authored artifacts** the agent must
   produce: the frozen role directives and brief templates (§9.2, Inv. 20),
   the role-side verbs ADR (§18), `install.sh` + the `/onboard` skill (§17),
   and the dashboard.
4. **Commit-when-green**: commit at every green micro-step with messages
   that name the phase/test; push if a remote exists. Path-scoped adds only
   (`git add .` is banned); no force-push; never amend the other harness's
   commits; never `git reset --hard`, `git checkout .`, or otherwise destroy
   uncommitted work. If a session must end mid-red, leave the working tree
   as-is and record the failing test and the intended fix in `PROGRESS.md` —
   the red state is data, not a mess.
5. **Ledger discipline**: update `PROGRESS.md` (append-only human chronology
   ending in an explicit `NEXT:` line) after every meaningful step. It is
   the only cross-session, cross-harness memory; the conversation is
   disposable.
6. **The deviation rule** (§1.6), including the definition of
   "conservative" verbatim, so the agent never has to guess it.
7. **No operator questions** except genuine scope change. Anything needing
   operator-only input is *parked* per §1.5; work continues on other phases.
8. **Quota is scheduled, not reacted to**: note remaining-quota signals
   (rate-limit headers, plan warnings) in `PROGRESS.md` and hand off
   *proactively at the next green commit* when quota is low — a mid-red
   quota death is the worst handoff. Claude Code sessions should target
   green-commit boundaries every ~2–4 h regardless, given rolling session
   windows.
9. **Harness-neutral phrasing.** Say "fan out read-only subagents where
   sub-tasks are independent," not "use the Workflow tool" — the same text
   must drive both harnesses.
10. **The kickoff sentence, verbatim** (so both harnesses are driven by the
    identical string): *"Continue the Mission Control implementation from
    commit `<sha>`, phase `<P-n>`. Follow the session protocol in AGENTS.md;
    read PROGRESS.md; do not invent scope; stop rather than guess missing
    operator inputs."*

### 1.3 The handoff artifacts

Two plain data files carry all cross-session, cross-harness state. They are
data, not scripts — nothing to run, no judgment required to operate them.

| Artifact | What it is | What it does |
|---|---|---|
| `PROGRESS.md` | plain data file | Append-only human chronology ending in a `NEXT:` line. A small header block at the top carries the last green SHA and any known-failing test with its repro. The next session — same harness or the other — reads it to resume. |
| `IMPLEMENTATION-NOTES.md` | plain data file | Append-only deviation log addressed to the operator (entry template in §1.6). The operator's window into every judgment call the agents made. |

### 1.4 Running Claude Code (the primary harness)

- **Permissions are the #1 autonomy killer.** Later phases hammer Docker,
  `go test`, `bun test`, and git in shapes an allowlist won't fully
  anticipate. Since the work lives in a dedicated folder with a scratch
  `MC_HOME` and a sacrificial Worksource, run sessions with permissions
  bypassed (`claude --dangerously-skip-permissions`) in that folder. If you
  prefer a softer posture, pre-seed `.claude/settings.json` with an
  allowlist (`go test`, `bun test`, `docker *`, `git *`, `mise *`,
  `make *`) and accept occasional prompts — but know each prompt is a stall
  until you return.
- **Keep it always working** with `/loop` in dynamic mode, using the §1.2
  kickoff sentence as the prompt. The ledger makes every iteration
  idempotent, so context compaction and session restarts cost nothing.
- **Bound every loop**: one bounded phase sub-goal per loop, max iterations,
  stop on repeated identical failure / missing operator input / invariant
  deviation — never "finish Mission Control."
- **When quota is low**, finish to green, commit, update `PROGRESS.md`'s
  `NEXT:` line, and stop. Then terminate Claude Code and start Codex (§1.5).

### 1.5 Codex takeover (and back)

- **Switch at phase boundaries or quota exhaustion, never mid-task or
  mid-file.** The outgoing agent finishes to green, commits, and updates
  `PROGRESS.md`. The switch itself is one sentence: start the other harness
  in the same folder with the §1.2 kickoff sentence (with the current SHA
  and phase filled in). Only one harness runs in the tree at a time.
- **Codex's loop equivalent is Goal Mode** (`/goal`; requires
  `features.goals = true` in `~/.codex/config.toml`). On takeover, start
  the Codex TUI in the repo and set
  `/goal <kickoff sentence> Done = the current phase's acceptance criteria
  met per PROGRESS.md.` — Codex persists the goal per-thread and works
  toward it without input; `/goal resume` continues after an interruption.
  Headless fallback: repeated `codex exec --profile mc "<kickoff sentence>"`
  invocations (the ledger makes repetition idempotent). The same goal bounds
  as §1.4 apply.
- **Configure Codex's autonomy up front.** Its default sandbox blocks the
  Docker socket, which the container phases require. Add a profile in
  `~/.codex/config.toml` with `approval_policy = "never"`,
  `sandbox_mode = "danger-full-access"`, and `features.goals = true` (the
  same trust granted to Claude Code's bypass, justified the same way:
  dedicated folder, scratch state). **Mark the repo trusted** — untrusted
  repos skip project-scoped config/hooks. Verify one trivial `codex exec`
  **and** one `/goal` set/clear in the repo **before** the first failover —
  a quota outage is the wrong moment to debug config.
- **Configuration parity, checked in:** `.claude/settings.json` and the
  Codex profile grant **equivalent** capabilities
  (go/bun/docker/git/make/mise), with the effective profile recorded in
  `OPERATOR-INPUTS.md`. A test passing under Claude Code's bypass but
  failing under Codex's sandbox is an undocumented environment difference,
  not a code bug.
- **Pin everything**: Go, Bun, Docker images by digest, codex CLI,
  claude-code CLI, agent-sdk, formatters. A pre-commit hook runs the
  formatters so the two harnesses don't ping-pong style diffs.
- **Specialize by strength when choice exists:** Codex on mechanical
  density (SQL fixtures, table-driven Go tests, decision-table enumeration,
  property generators); Claude Code on high-judgment work (adapter seams,
  ADR drafts, crash-recovery reasoning, role directives). Either finishes a
  whole vertical slice.
- **Expect capability asymmetry, not correctness asymmetry.** Claude Code
  fans out with dynamic workflows, Codex with collaboration tool-calling;
  AGENTS.md's harness-neutral phrasing lets each use its own mechanism. The
  TDD spine is sequential either way.

### 1.6 The deviation log

**`IMPLEMENTATION-NOTES.md` — the deviation log, addressed to the operator.**
Append-only, newest last. Alongside it, `docs/adr/NNN-<slug>.md` records
*planned* designs the spec delegates (the role-side verbs table, §18, and any
post-start harness version bump). ADRs are deliverables; the deviation log is
where surprises land.

Required deviation entry shape:

```
## <date> — <one-line title>
- Where: <phase/test/spec § that surfaced it>
- Gap: <what the spec didn't cover or got wrong>
- Choice: <the conservative option taken, and why it is the conservative one>
- Spec impact: <sections whose text should change, or "none">
- Needs your decision: no | yes → also parked in PROGRESS.md
```

- **"Conservative" is defined, not vibes**: the option that (a) preserves
  the 26 invariants and the fail-closed posture, (b) deviates least from
  the spec's text, and (c) is easiest to reverse later. Put this definition
  in AGENTS.md verbatim.
- **Log-and-go is the default.** An entry is informational unless the gap
  needs operator-only input or breaks an invariant — only then does the
  item park (never the whole run; other phases continue).

### 1.7 Parked-question protocol (operator latency)

Agents never wait on the operator. Anything needing operator-only input goes
under `## Parked` in `PROGRESS.md` with a one-line decision request; work
continues on other phases. The operator sweeps Parked once daily; answers
land in `OPERATOR-INPUTS.md` or an ADR.

---

## Part 2 — Ahead-of-time operator input

Everything here must exist **before** the agent starts, or it will block
mid-implementation on things only the operator can do. The live values live
in `OPERATOR-INPUTS.md` (never committed — it is in `.gitignore` from the
first commit); this section is the checklist of what that file must answer.

### 2.1 Must provide (agent cannot obtain these)

| # | Item | Why / detail |
|---|---|---|
| 1 | **ChatGPT-plan OAuth** for Codex | Interactive browser login. Run `codex login` on this machine; the agent needs the resulting `auth.json` reachable for the materialization path and acceptance tests. Force one refresh before session one. |
| 2 | **Anthropic subscription auth** for the `claude` binding | Interactive. Run `claude login` (or equivalent SDK auth). On macOS Claude Code stores OAuth in the keychain; in-container Linux uses a file — the container-side credential home must be its own canonical dir, never the operator's `~/.claude`. Force one refresh; also mint a `claude setup-token` as a mitigation candidate for file-refresh fragility. Confirm which Claude install method (native vs npm) is in use and pin it — the two diverge on keychain behavior. |
| 3 | **MiniMax subscription key** | Provide the key value directly; onboarding (§17.3) wires it into the egress gateway's injection table — there is no secrets store. |
| 4 | **Container runtime, running and frozen** | **Decided: Docker Desktop.** Must be running; "start at sign-in" on; Enhanced Container Isolation **off**, no user-namespace remap; VirtioFS backend noted; **Resource Saver disabled** (its VM pause-on-idle stalls the helper and distorts tick timing); ≥4 CPU / ≥8 GB to the VM; version pinned against auto-update surprises. Export the settings snapshot into `OPERATOR-INPUTS.md`. |
| 5 | **Discord decision** | In or out of v1? If in: create the bot in a **sacrificial server**; record token + guild/channel ids + your Discord user id for the operator allowlist. Prefer slash commands / buttons / DMs over free-text-in-guild-channels — the Message Content privileged intent is only needed for the free-text-in-channel path; enable it in the dev portal only if that path is kept. If out: say so — the spec makes it optional; the surfaces still need the outbox + dashboard path. |
| 6 | **A sacrificial test Worksource** | A throwaway git repo (local path is fine) the system may commit to, merge into, and run autonomous agents against during e2e tests and the smoke. It must be genuinely disposable — never a real project. |
| 7 | **The sacrificial Worksource's standing directive** | Unattended runs use `seeding_mode: auto`; Strategist(propose) proposes from this directive, so it must exist and point at checkable work. |
| 8 | **Token-spend authorization** | Real-subscription acceptance + smoke burns subscription usage. State the budget (e.g. "acceptance tier + up to 3 smoke attempts"). |
| 9 | **Proxy CA pair** | Generate it before session one; the CA **private key** lives host-side only, never in any container mount. |
| 10 | **Codex version floor** | Pin a codex CLI version with unified custom-CA support (`CODEX_CA_CERTIFICATE`); record it — load-bearing for the egress/CA wiring, not just a reference floor. |
| 11 | **Pre-built arm64 images** | Pre-build/pre-pull everything: base image (with and without the Playwright layer), golang, oven/bun. Never let an agent loop burn quota waiting on a cold image build. |

Keychain pre-authorization is **dropped**: Mission Control does not touch
the macOS keychain (spec §5 "Secrets"). Claude Code's own host-side
keychain storage of its OAuth (row 2's note) is that tool's behavior, not
Mission Control's.

### 2.2 Decided (recorded so no one re-litigates them mid-flight)

| # | Decision |
|---|---|
| 1 | **Where the implementation lives:** a fresh folder, working name "Homie" (final software name TBD), created by the operator before handoff and seeded per §1.1. Nothing lives inside the existing `homie` research repo, satisfying the spec §16.1 clean-shared-tree assumption. |
| 2 | **Languages/toolchain** — folded into the spec (§16.1): Go + `modernc.org/sqlite` for `mc`; TypeScript on Bun for the resident, dashboard, and runner; toolchain versions via a tracked `mise` config. |
| 3 | **Pinned harness versions** — mechanism folded into the spec (§11.2, §16.1): exact-version Dockerfile `ARG`s for the harness CLIs, `package.json` + committed lockfile for the SDK, `mise` for Go/Bun. The agent pins latest-stable at implementation start. Any post-start version bump is an ADR-worthy event; pins must not drift casually. |
| 4 | **launchd loading during development: never.** Onboarding's plist-generation code is tested as code (output parses, `plutil -lint` passes, paths exist, restart policies correct) without handing a unit to launchd. Real loading happens exactly once, in the operator-present endgame session. |
| 5 | **Onboarding front door** — folded into the spec (§16.1, §17): the repo ships `install.sh` and the `/onboard` skill; `mc` is agent-facing only; no operator-facing material may name an `mc` command. |

### 2.3 Environment facts the agent should be told verbatim

- Primary target is this macOS machine (Apple Silicon — base image must be
  arm64; note the Playwright browser adds ~1–2 GB to it).
- Network egress is available for image pulls and npm/go module downloads.
- `MC_HOME` for development: use a scratch path, never
  `~/.mission-control`, until acceptance.
- The prior evidence in `docs/priors/` (POCs plus the memory notes: Claude
  subagent `settingSources` failure mode, runtime depth-1 subagent capping,
  LiteLLM-proxy-forces-metered-fallback observation) are trusted priors —
  do not re-derive them.
- Homie's `native.jsonl` resume substrate is a version-churned
  harness-internal format — the conversation-rows fallback (spec §15.4) is
  the designed answer, but a harness version bump that breaks resume is an
  ADR-worthy event, and harness pins must not drift casually.
- File-watching across VirtioFS bind mounts is unreliable — anything
  watching bind-mounted paths polls, never trusts fsnotify.

### 2.4 Day-0 action checklist (condensed to actions)

1. Run `codex login` and `claude login`; force one refresh of each OAuth
   binding; mint the `setup-token`; record where each credential landed in
   `OPERATOR-INPUTS.md`.
2. Record the MiniMax key in `OPERATOR-INPUTS.md`.
3. Freeze and snapshot the Docker Desktop configuration per must-provide
   row 4.
4. Generate the proxy CA pair (private key host-side only).
5. Pre-build/pre-pull all arm64 images; pin the codex CLI at the custom-CA
   version floor.
6. Create (or explicitly confirm) the sacrificial test Worksource and write
   its standing directive; record paths.
7. Make the Discord call; if in, set up the bot in a sacrificial server per
   must-provide row 5.
8. Write the token-budget sentence.
9. Create the Homie folder and seed the §1.1 scaffold, `.gitignore` first.
   Create the private remote.
10. Configure both harnesses' autonomy postures (§1.4, §1.5) and smoke-test
    each with a trivial command in the folder — including one `codex exec`
    and one `/goal` set/clear.
11. **Schedule yourself for the endgame**: the one operator-present session —
    launchd unit loading, the real-subscription acceptance pass, and the
    `install.sh` → smoke pass — happens once, after everything else is done.
    Between Day 0 and then, your recurring duty is the daily Parked sweep
    (§1.7).
