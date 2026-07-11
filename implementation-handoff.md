# Mission Control — Implementation Handoff

How to hand `mission-control-spec.md` to implementing agents and get a
finished, verified system back with minimal operator follow-up. The spec
(`specs/mission-control-spec.md`) is the behavioral contract and wins on
conflict; this document is the operating protocol for the handoff itself and
the definition of done.

The workflow is deliberately simple: **Claude Code is the primary
implementing harness**; when its quota maxes out the operator terminates it
and boots **Codex** in the same repo to continue. Only one harness writes at
a time, so the handoff is manual and sequential — no lock, no gate scripts.
**Done is the phased test plan** (Part 3): each phase's tests are its
acceptance criteria, and the implementation is finished when they all pass
plus the `install.sh` smoke goes green. The agent records which phases' tests
pass in `PROGRESS.md`.

Contents: how the handoff runs (Part 1), the architecture-kill spikes that
precede all product code (Part 2), the definition of done — the phased test
plan (Part 3), and what the operator supplies before the first session
(Part 4).

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
| `OPERATOR-INPUTS.md` | the completed Part 4 checklist with real values, not placeholders — every blank in it is a mid-flight question |
| `.gitignore` | seeded **before the first commit**, covering at minimum `OPERATOR-INPUTS.md` (it holds live secrets) and the dev `MC_HOME` scratch path if it lives in-tree |
| `PROGRESS.md` | the ledger (§1.2, §1.3) — empty except the phase list and a `NEXT:` line pointing at Phase 0 |
| `IMPLEMENTATION-NOTES.md` | the deviation log, empty except the entry template (§1.6) |
| `docs/adr/` | ADR template plus pre-named empty slots: the role-side verbs + verb-by-scope table (the one design the spec delegates, spec §18), and one slot per spike fallback (Part 2) — S1→socket-daemon writer, S2→Engine-API exec, S3→setup-token/serialized refresh, S4→base_url routing for minimax, S5→DELETE journal mode. Standard shape: Status / Context / Decision / Consequences |
| `spikes/` | eight stub directories `spikes/NN-name/`, one per Part 2 spike, each expecting a `RESULT.md` with a machine-rerunnable check |

Create a **private remote** for the folder and have `AGENTS.md` push after
green commits — long stretches of local-only autonomous work are one disk
failure from gone.

### 1.2 `AGENTS.md` — the standing instruction

This file is the whole autonomy mechanism; both harnesses reload it every
session. It must contain:

1. **Session-start protocol** (the resume ritual): read `PROGRESS.md`,
   `git log --oneline -20`, and `git status` (uncommitted work is data,
   never discard it), run the test suite (once one exists — if it does not
   yet, building it is the current task), then continue from the ledger's
   `NEXT:` line. Never ask what to do next — the ledger says.
2. **Takeover review**: when the previous session ran on the other harness,
   the first substantive act is an **adversarial review of the outgoing
   phase against the spec** before building on it — the system's own
   decorrelated producer/judge principle applied to its own construction.
   Findings go to `IMPLEMENTATION-NOTES.md` (and `## Parked` in `PROGRESS.md`
   if operator input is needed).
3. **The work order and definition of done**: Phase 0 spikes first,
   stop-on-red per Part 2; then TDD through the phases in Part 3 order. The
   fast suite (Phases 1–2, no Docker) runs on every commit; each phase's
   full test set is its acceptance criteria and its `NEXT:` marker may not
   advance until those tests pass. Beyond code and tests, the spec also
   requires four **authored artifacts** the agent must produce: the frozen
   role directives and brief templates (§9.2, Inv. 20), the role-side verbs
   ADR (§18), `install.sh` + the `/onboard` skill (§17), and the dashboard.
4. **Commit-when-green**: commit at every green micro-step (the fast suite
   passing) with messages that name the phase/test; push if a remote exists.
   Path-scoped adds only (`git add .` is banned); no force-push; never amend
   the other harness's commits; never `git reset --hard`, `git checkout .`,
   or otherwise destroy uncommitted work. If a session must end mid-red,
   leave the working tree as-is and record the failing test and the intended
   fix in `PROGRESS.md` — the red state is data, not a mess.
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
| `PROGRESS.md` | plain data file | Append-only human chronology ending in a `NEXT:` line. A small header block at the top carries the last green SHA, which phases' tests currently pass, and any known-failing test with its repro. The next session — same harness or the other — reads it to resume. |
| `IMPLEMENTATION-NOTES.md` | plain data file | Append-only deviation log addressed to the operator (entry template in §1.6). The operator's window into every judgment call the agents made. |

### 1.4 Running Claude Code (the primary harness)

- **Permissions are the #1 autonomy killer.** Later phases hammer Docker,
  `go test`, `bun test`, and git in shapes an allowlist won't fully
  anticipate. Since the work lives in a dedicated folder with a scratch
  `MC_HOME` and a sacrificial Worksource, run sessions with permissions
  bypassed (`claude --dangerously-skip-permissions`) in that folder. If you
  prefer a softer posture, pre-seed `.claude/settings.json` with an
  allowlist (`go test`, `bun test`, `docker *`, `git *`, `mise *`) and
  accept occasional prompts — but know each prompt is a stall until you
  return.
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
  `/goal <kickoff sentence> Done = the current phase's tests pass per
  PROGRESS.md.` — Codex persists the goal per-thread and works toward it
  without input; `/goal resume` continues after an interruption. Headless
  fallback: repeated `codex exec --profile mc "<kickoff sentence>"`
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
  (go/bun/docker/git/mise), with the effective profile recorded in
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
*planned* designs the spec delegates (the role-side verbs table §18, the
pre-declared spike fallbacks in §1.1, and any post-start harness version
bump). ADRs are deliverables; the deviation log is where surprises land.

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
land in `OPERATOR-INPUTS.md` or an ADR. **Red spikes are the one exception**:
an architecture-kill failure stops the affected line (not the whole run —
other spikes and phases continue) until the fallback ADR is signed.

---

## Part 2 — Phase 0: Architecture-kill spikes (run before any product code)

Each spike is a throwaway script committed to `spikes/NN-name/` with a
`RESULT.md` containing a machine-rerunnable check. A red spike **stops the
line**: write an ADR choosing the pre-declared fallback (§1.1), get operator
sign-off, then proceed. Each spike's assertions are later folded into the
permanent test suite. Spikes are ranked by redesign blast radius.

**S1 — The setuid gate on Docker Desktop named volumes** *(highest blast
radius).* The entire Inv. 2 architecture — kernel-enforced sole-writer,
agent-uid denial, privileged `mc` — rests on setuid + uid ownership behaving
natively inside the Docker Desktop VM. **Probe:** a static Go binary
(modernc.org/sqlite), owned by a privileged uid with the setuid bit, baked
into an image; the spine file on a **named volume** owned by that uid. From
an agent-uid process: direct `open()` on the DB must fail EACCES; the same
read through the setuid binary must succeed. Verify survival across container
restart, image rebuild, Docker Desktop restart, volume detach/reattach. Also
assert as permanent canaries: runtime is `runc` (Enhanced Container Isolation
off, no user-namespace remap), `no-new-privileges` **not** set on agent
containers, the volume mount not `nosuid`. **Fallback ADR:** a privileged
writer daemon behind a unix socket with peer-credential checks.

**S2 — Warm-helper `docker exec` fidelity.** Every host-side `mc` invocation
on macOS funnels through this. **Probe matrix (non-TTY):** exit codes
{0, 1, 126, 127, 137, signal-death} propagate exactly; 8-bit-clean round-trip
of ~50 MB random bytes stdin→stdout (sha256 compare); interleaved
stdout/stderr demux; a long-lived stream (`history --follow` shape) held
30 min; SIGINT/SIGTERM cancellation (docker exec does not proxy signals);
N=5 concurrent execs; helper killed → next host `mc` lazily recreates;
Docker Desktop restart → detect-and-rewarm. **Fallback ADR:** Docker Engine
API exec (framed streams), or a long-lived framed RPC over the exec channel.

**S3 — OAuth lifecycle in bind-mounted control dirs.** The `materialized`
delivery posture (spec §11.4) assumes in-container refresh writes rotated
tokens back to canonical host-side dirs through a VirtioFS bind mount.
**Probe:** mount the directory, never the file (refresh replaces the inode, so
a file-level mount goes stale); force a token refresh in-container and confirm
rotated tokens land in the canonical dir and a subsequent run starts from
them; restart Docker mid-refresh → no corruption; the concurrent-refresh race
(two containers sharing one auth dir); the Claude macOS quirk (a host-side
`claude login` stores to Keychain and can delete `.credentials.json` — the
container credential home must be its own canonical dir, never `~/.claude`);
evaluate `claude setup-token` as a mitigation. **Fallback ADR:** serialize all
refreshes, or adopt `setup-token` for the claude binding; for codex, follow
OpenAI's documented CI pattern.

**S4 — Egress gateway + CA trust per harness.** The proxy design assumes each
harness honors `HTTPS_PROXY` and trusts an injected CA. **Probe:** one live
no-op turn per binding through the proxy. Codex needs `CODEX_CA_CERTIFICATE`
(with `SSL_CERT_FILE` fallback) at a pinned version ≥ unified custom-CA
support, and its sandbox filters env vars so CA vars must be explicitly passed.
Claude needs the CA in the **real process env** (`NODE_EXTRA_CA_CERTS`) —
settings-file CA vars are reported-but-not-honored. Test **streaming paths,
not just one POST**. Verify fail-closed: CA removed → clean TLS failure;
direct non-proxy egress refused. **Recommended simplification (day-2 ADR):**
for the `minimax` static-token binding, skip TLS interception — route via
`ANTHROPIC_BASE_URL` to the gateway and inject the header there.

**S5 — SQLite WAL + crash discipline on the named volume.** **Probe:**
WAL + `busy_timeout` + `BEGIN IMMEDIATE`; `kill -9` the writer mid-transaction
→ reopen → `integrity_check` clean, committed rows survive, uncommitted
vanish; concurrent reader during write; Docker Desktop restart mid-write.
Confirm modernc.org/sqlite sets per-connection PRAGMAs on **every** connection
path. Fail-closed rule to encode: reject any DB relocation to a bind mount.
**Fallback ADR:** `journal_mode=DELETE` + exclusive locking, or a single
long-lived writer process. (Bundle with S1 — a half-day confirmation.)

**S6 — Dispatch decision table as a pure function** *(highest semantic risk;
no Docker).* Implement `mc dispatch`'s selection logic as a pure function
`(records, lock, clock-inputs) → action | idle` before any pipeline stage
exists, and write the exhaustive decision-table test against it. **Pass:**
every walk step taken and not-taken; every invisibility rule tested with a
would-otherwise-win row; deterministic tie-breaking; exactly zero-or-one
action per evaluation; the many-obligation precedence state asserted.
**Fail:** the state machine or "who is owed what" semantics are ambiguous →
resolve in the spec's terms before building stages. This suite is promoted
whole into Phase 2.

**S7 — launchd + sleep + clock drill.** **Probe:** resident under a real
LaunchAgent (a spike exception to "no launchd during dev," unloaded after):
Docker not yet up at load → backoff-and-retry works; minimal PATH; correct
Docker socket/context resolution for non-interactive launchd. Then sleep the
Mac 30+ min mid-lease, wake → immediate tick fires; **VM clock vs host clock
agree** (Docker VM clock skew after macOS sleep is a recurring failure mode).
This validates the spec's R2 rule concretely: all lease math is done in-VM by
`mc`. **Also check** Docker Desktop's **Resource Saver** (VM pause-on-idle) —
disable it or prove ticks survive pause/resume. Record in `OPERATOR-INPUTS.md`.

**S8 — arm64 image build + Playwright smoke.** Build the base image natively
(verify `Architecture: arm64`, no QEMU); one Chromium launch + screenshot
in-container. Measure build time and pin by digest so agents never rebuild it
in a loop. Blast radius is Dockerfile-only — last for a reason.

**Sequencing:** Day 1: S1 + S5. Day 2: S2 + S6. Day 3: S3 + S4 (record the
MITM-vs-base_url ADR). Day 4: S7 + S8, freeze ADRs, fold spike assertions into
the permanent suite. Then product code.

---

## Part 3 — Definition of done: the phased test plan

Acceptance criteria written as tests, so the implementing agent can prove it
is finished without the operator pointing out what's missing. Phases are
ordered by derisking; each is written red-first and unblocks the next.
**A phase is done when its tests pass** — the agent records that in
`PROGRESS.md`; there is no separate gate to satisfy. Practical lane split: the
fast tests (Phases 1–2, no Docker) run on every commit; the Docker-dependent
suites (Phases 3–4) run at phase completion, not in inner loops, so container
churn doesn't burn wall-clock or quota.

**Tooling assumptions** (agent may substitute with justification): `mc` in Go
→ `go test`, table-driven, SQLite via `modernc.org/sqlite` (pure Go — the
setuid gate demands a static binary). Resident, dashboard, runner in
TypeScript on Bun → `bun test`. *(Both decided, spec §16.1.)* A **fake
harness** — a tiny CLI/SDK-shaped stub implementing the Runtime Adapter
contract (start session, accept one turn, emit a completion event, write a
`native.jsonl`, exit with a scripted status), registered as a third harness
family in test configs only — lets every control-loop behavior (heartbeats,
crash recovery, correction rallies, backpressure) be tested deterministically,
fast, and token-free.

**Phase 1 — Substrate + walking skeleton.** Two things prove it. (a)
*Substrate backstop tests* (pure SQL against a temp spine, no `mc` binary):
the full state-transition matrix both scopes — every legal edge commits, every
illegal edge aborts — plus one representative test per trigger class:
`correction_count` bounds (0–3), blocked-needs-reason, archive-needs-decision
(and `decision`↔`decided_at`), approve-only-from-`packaged`,
no-initiative-nesting, wave-child birth rules, strict-drain on
done-declaration, blocked-child propagation + auto-clear, cascade archive,
the review WIP cap (max 3 unarchived packets), the saturation trigger
(`refine_streak ≥ 3`), `activity` append-only, `stage_rank` generation. These
are a redundant backstop; real enforcement lives in the Phase 2 domain layer.
(b) *The walking skeleton*: one `origin:user` task traverses
`tick → dispatch → lease → fake-harness Worker → mc complete → … → packet →
approve → land` through the real Go binary, real resident, real container
topology, and fake harness — asserting timer-driven advancement and the
approve/land split. **The fake harness is built here.**

**Phase 2 — Dispatch + domain correctness** *(largest investment).* Proves:
the **dispatch decision table** covered as one table — the tick walk
(reconcile, console-if-due, landing, queue-at-cap, queue-with-room), every
step's taken *and* not-taken branch, every "invisible to this query" rule
tested where the excluded row would otherwise win (S6 built this); the
**domain aggregates**, one suite each — task state machine (verdict outcomes
PASS / CORRECT / BUDGET-SPENT), dispatch-retry budget + exhaustion → blocked,
initiative (wave birth, strict drain, block propagation), review packet
(`refine_streak`, saturation), review queue (WIP cap + at-cap selection
order), lease (three reap conditions + reap outcome), all lease math on an
injectable clock inside the lock domain; **completion & fencing** —
`mc complete` is a pure write that never dispatches, stale-run fencing on
complete and heartbeat, CAS claim (exactly one winner), the two separate
budgets (`correction_count` vs `dispatch_retries`); **the CLI verbs** — every
§18 verb's happy path + declared error paths, driven through the real `mc`
CLI against a temp spine; the **split-brain suite** — parameterized
kill-points across the non-atomic boundaries (action selected / container
created / files modified / git commit / status written / outbox insert /
delivery) → restart → assert convergence with no duplicate commit, wave,
merge, or lying status (outbox is at-least-once with dedup); and the
**randomized property suites** (nightly, non-gating except the
generator-honesty and planted-mutant checks) — dispatch state fuzzer,
metamorphic invisibility pass, lifecycle random walk with the trigger lattice
as differential oracle, and the generator-honesty gates (coverage-distribution
assertion + planted-mutant kill list).

**Phase 3 — Boundary conformance** *(requires Docker).* One test per
enforcement *mechanism*: mount validation (one accept + one reject per rule
class — symlinks, blocked patterns, `..`/`:`, RW-only-when-both-agree,
cross-Worksource); the fail-closed inversion (an unappliable mount/network
rule aborts dispatch and blocks the task — assert the row); forbidden-env scan
(`*_API_KEY`); the `MC_HOME` git-working-tree fence; blocked-pattern list
extend-only; routing decorrelation validation; **the setuid gate (S1
promoted)** with the runc / no-new-privileges / nosuid canaries;
per-container scope (homie vs pipeline verb refusals, `run_id` fencing,
read-only `run.json`); the mount plan; the gateway probe (authenticated call
succeeds, direct egress refused, unwired gateway → no spawn); `egress_policy`
three modes; **the helper (S2 promoted)**; the orphan sweep; resource bounds.

**Phase 4 — E2E control loops** *(fake harness; exactly six scenario
families).* Real containers, real spine, real resident, all progress
timer-driven. **(1) Full pipeline + landing** — one task through
Editor→Worker→Verifier PASS→Packager→packet→approve→land, asserting the
approve/land split, plus landing-failure and multi-approve-drain variants.
**(2) Correction rally** — three Verifier CORRECTs → fourth ships BUDGET-SPENT
(`correction_count = 3`). **(3) Backpressure** — queue filled to 3 → no new
pipeline dispatch, Refiner on the best non-saturated packet, re-entered task
advances at cap, three failed refinements → saturated → idle. **(4)
Initiative lifecycle** — charter → shared worktree → wave children → strict
drain → arc packet → approve/land, with block-propagation and cancel-cascade
variants. **(5) Fault matrix** — one parameterized kill per kill-class → reap
on the right threshold → same task re-selected → completes on retry; plus the
interrupt path, tick-loop discipline (at most one dispatch at a time,
wake-from-sleep fires an immediate tick), the reboot drill, and
session-folder permanence. **(6) Homie loop** — inbound `mc homie send` →
timer tick wakes the container → reply → outbox per binding → delivery + ack;
resume re-mounts the same folder; the console schedule sub-test. The
dashboard gets **one Playwright smoke path**.

**Phase 5 — Real-subscription acceptance** *(thin, manual,
operator-scheduled).* Per enabled binding: one live no-op turn **and** one
small streaming tool-using turn through its declared delivery posture (gateway
for `minimax`, bind-mounted control dir for `codex`/`claude`) — streaming is
where compat endpoints and proxies diverge. **OAuth refresh canary (S3
promoted):** force one refresh per OAuth binding through the deployed posture
and confirm rotated tokens land in the canonical dirs. Real-harness
sharp-edge regressions (`claude-sdk` `options.env` replacement and
`settingSources: []` subagent spawn; `codex exec` under the proxy without
metered fallback — see `docs/priors/`). **Front door + smoke:** from a fresh
clone, `install.sh` performs the Level-0 bootstrap (prerequisite checks,
build/install `mc`, hand off to `mc onboard`), then `mc onboard --smoke` runs
the transient-Worksource full-pipeline pass with real models — **this is the
final acceptance test; the implementation is done when it passes on this
machine.** Finally, the **endgame session** (operator present, once): load the
launchd units for real, observe start / kill-survive / login-return, and run
the restore-from-backup drill. *OAuth-binding live tests never sit in any
automated suite — alert, don't block.*

**Cuts and never-cuts.** Cut with least risk: a separate CLI test tier
(merged into Phase 2), scope-permutation matrices, per-role crash duplicates,
standalone interrupt tests, dashboard visual coverage beyond one smoke,
exhaustive CLI help/formatting asserts, randomized suites as commit gates.
**Never cut:** security-boundary negatives, credential refresh, proxy
fail-closed, the dispatch decision table, split-brain reconciliation,
migrations/backup-restore, approval-provenance fencing.

---

## Part 4 — Ahead-of-time operator input

Everything here must exist **before** the agent starts, or it will block
mid-implementation on things only the operator can do. The live values live
in `OPERATOR-INPUTS.md` (never committed — it is in `.gitignore` from the
first commit); this section is the checklist of what that file must answer.

### 4.1 Must provide (agent cannot obtain these)

| # | Item | Why / detail |
|---|---|---|
| 1 | **ChatGPT-plan OAuth** for Codex | Interactive browser login. Run `codex login` on this machine; the agent needs the resulting `auth.json` reachable for the materialization path and acceptance tests. Force one refresh before session one. |
| 2 | **Anthropic subscription auth** for the `claude` binding | Interactive. Run `claude login` (or equivalent SDK auth). On macOS Claude Code stores OAuth in the keychain; in-container Linux uses a file — the container-side credential home must be its own canonical dir, never the operator's `~/.claude` (S3). Force one refresh; also mint a `claude setup-token` as the S3 mitigation candidate. Confirm which Claude install method (native vs npm) is in use and pin it — the two diverge on keychain behavior. |
| 3 | **MiniMax subscription key** | Provide the key value directly; onboarding (§17.3) wires it into the egress gateway's injection table — there is no secrets store. |
| 4 | **Container runtime, running and frozen** | **Decided: Docker Desktop.** Must be running; "start at sign-in" on; Enhanced Container Isolation **off**, no user-namespace remap; VirtioFS backend noted; **Resource Saver disabled** (or S7 proves tick survival across pause/resume); ≥4 CPU / ≥8 GB to the VM; version pinned against auto-update surprises. Export the settings snapshot into `OPERATOR-INPUTS.md`. |
| 5 | **Discord decision** | In or out of v1? If in: create the bot in a **sacrificial server**; record token + guild/channel ids + your Discord user id for the operator allowlist. Prefer slash commands / buttons / DMs over free-text-in-guild-channels — the Message Content privileged intent is only needed for the free-text-in-channel path; enable it in the dev portal only if that path is kept. If out: say so — the spec makes it optional; the surfaces still need the outbox + dashboard path. |
| 6 | **A sacrificial test Worksource** | A throwaway git repo (local path is fine) the system may commit to, merge into, and run autonomous agents against during e2e tests and the smoke. It must be genuinely disposable — never a real project. |
| 7 | **The sacrificial Worksource's standing directive** | Unattended runs use `seeding_mode: auto`; Strategist(propose) proposes from this directive, so it must exist and point at checkable work. |
| 8 | **Token-spend authorization** | Real-subscription acceptance (Phase 5) + smoke burns subscription usage. State the budget (e.g. "acceptance tier + up to 3 smoke attempts"). |
| 9 | **Proxy CA pair** | Generate it before session one; the CA **private key** lives host-side only, never in any container mount. |
| 10 | **Codex version floor** | Pin a codex CLI version with unified custom-CA support (`CODEX_CA_CERTIFICATE`); record it — load-bearing for S4, not just a reference floor. |
| 11 | **Pre-built arm64 images** | Pre-build/pre-pull everything: base image (with and without the Playwright layer), golang, oven/bun. Never let an agent loop burn quota waiting on a cold image build. |

Keychain pre-authorization is **dropped**: Mission Control does not touch
the macOS keychain (spec §5 "Secrets"). Claude Code's own host-side
keychain storage of its OAuth (row 2's note) is that tool's behavior, not
Mission Control's.

### 4.2 Decided (recorded so no one re-litigates them mid-flight)

| # | Decision |
|---|---|
| 1 | **Where the implementation lives:** a fresh folder, working name "Homie" (final software name TBD), created by the operator before handoff and seeded per §1.1. Nothing lives inside the existing `homie` research repo, satisfying the spec §16.1 clean-shared-tree assumption. |
| 2 | **Languages/toolchain** — folded into the spec (§16.1): Go + `modernc.org/sqlite` for `mc`; TypeScript on Bun for the resident, dashboard, and runner; toolchain versions via a tracked `mise` config. |
| 3 | **Pinned harness versions** — mechanism folded into the spec (§11.2, §16.1): exact-version Dockerfile `ARG`s for the harness CLIs, `package.json` + committed lockfile for the SDK, `mise` for Go/Bun. The agent pins latest-stable at implementation start. Any post-start version bump is an ADR-worthy event; pins must not drift casually. |
| 4 | **launchd loading during development: never** (S7 is the sanctioned spike exception, unloaded after). Onboarding's plist-generation code is tested as code (output parses, `plutil -lint` passes, paths exist, restart policies correct) without handing a unit to launchd. Real loading happens exactly once, in the operator-present endgame session (Phase 5). |
| 5 | **Onboarding front door** — folded into the spec (§16.1, §17): the repo ships `install.sh` and the `/onboard` skill; `mc` is agent-facing only; no operator-facing material may name an `mc` command. |

### 4.3 Environment facts the agent should be told verbatim

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

### 4.4 Day-0 action checklist (condensed to actions)

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
9. Create the Homie folder and seed the §1.1 scaffold, `.gitignore` first —
   including the `spikes/` stubs with pre-declared fallback ADR slots.
   Create the private remote.
10. Configure both harnesses' autonomy postures (§1.4, §1.5) and smoke-test
    each with a trivial command in the folder — including one `codex exec`
    and one `/goal` set/clear.
11. **Schedule yourself for the endgame**: the one operator-present session
    (Phase 5) — launchd unit loading, the real-subscription acceptance pass,
    and the `install.sh` → smoke pass — happens once, after everything else
    is green. Between Day 0 and then, your recurring duties are the daily
    Parked sweep (§1.7) and spike fallback sign-offs (Part 2).
