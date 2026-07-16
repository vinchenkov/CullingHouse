# AGENTS.md — the standing instruction (Mission Control implementation)

This file is the whole autonomy mechanism. Both harnesses (Claude Code and
Codex) reload it every session. The behavioral contract is
`specs/mission-control-spec.md`; the operating protocol is
`specs/implementation-handoff.md`. **The spec wins on conflict.**

Neither the spec nor the handoff is a session-start read. They are
conflict-resolution references: open them at the section a task names, when
a rule here is ambiguous, or when a test disagrees with an ADR. Loading
either one to "get oriented" is the failure this section exists to prevent. The files can be read in their entirety but only in scenarios where a holistic view is necessary.

## 0. Who this file is talking to

**§§1–3, §5, and §§7–8 govern a session-level agent resuming the
implementation.** They do not govern a spawned agent.

**If you were spawned with a specific task — a review, a probe, a search, a
single artifact — that task is your whole job.** Do not run the resume
ritual, do not read `PROGRESS.md`, do not run the suite, do not consult
`NEXT:`. Your task prompt wins over those sections. Return your artifact and
stop. (§7's "never ask" and §1's "never ask what to do next — the ledger
says" are aimed at the session-level agent; for a spawned agent they are
wrong, and following them means abandoning the task you were given.)

**§4 and §6 bind every agent, spawned or not, and a task prompt does not
override them.** Path-scoped adds only; `git add .` is banned; no force-push;
never destroy uncommitted work — including the untracked probes other agents
leave in `.claude/worktrees/`. A judgment call the spec doesn't cover gets
logged per §6 no matter who made it.

*Terminology:* in this file, **task prompt** means the instruction a harness
gives a spawned agent. It is not a **brief** — that word is reserved for
`mc.spawn-brief.v1`, the immutable carrier Mission Control freezes for a
pipeline role (ADR-008, ADR-020 D4, `mc/verbs/brief.go`). Do not mix them.

The guard lives here because this file is injected into spawned agents
automatically — verified for Claude Code 2026-07-15, **unverified for
Codex**. Assume inheritance; write task prompts that stand alone regardless.

## 1. Session-start protocol (the resume ritual)

**Read exactly this. It is a closed set.**

1. `PROGRESS.md` — the whole file. It is small by construction (~200 lines);
   if it is not, compacting it is the current task. Narrative history lives
   in `docs/ledger/` and is **not** a startup read.
2. `git log --oneline -10` and `git status`. **Uncommitted work is data,
   never discard it** — including untracked files in `.claude/worktrees/`.
3. Run the test suite (the fast lane: Phases 1–2, no Docker). If no test
   suite exists yet, building it is the current task.
4. **The one artifact `NEXT:` names**, if it names one — an ADR (via
   `docs/adr/INDEX.md`, then the named Decision by line, not the whole file),
   a phase contract, a spike RESULT.

**Do not read at startup:** the spec, the handoff, `IMPLEMENTATION-NOTES.md`,
`docs/ledger/`, `docs/priors/`, or any ADR `NEXT:` did not name. Go to them
when a task, a failing test, or a genuine ambiguity sends you — not before.
A startup that costs 50k tokens has spent its judgment before the first edit.

5. Continue from the ledger's `NEXT:` line. Never ask what to do next —
   the ledger says.

## 2. Takeover review

When the previous session ran on the other harness, the first substantive
act is an **adversarial review of the outgoing work** before building on it
— the system's own decorrelated producer/judge principle applied to its own
construction.

**Scope it, and spawn it.** The review reads `git diff <last-reviewed-sha>..HEAD`
against the relevant `docs/phaseN-contract.md` — not the spec, not the phase's
whole surface. Run it as a **read-only spawned agent that returns a verdict**,
so its working context is discarded and never enters the implementing
session's window (§9's criterion, applied to the most expensive instruction
in this file). Give it a task prompt that stands alone; per §0 it will not
run the resume ritual.

Findings go to `IMPLEMENTATION-NOTES.md` (and `## Parked` in `PROGRESS.md` if
operator input is needed).

## 3. Work order and definition of done

- **Phase 0 spikes first**, stop-on-red per handoff Part 2: a red spike
  stops the affected line (not the whole run) until its fallback ADR is
  signed by the operator.
- Then **TDD through the phases in handoff Part 3 order**. Each phase's
  full test set is its acceptance criteria; its `NEXT:` marker may not
  advance until those tests pass.
- The **fast suite** (Phases 1–2, no Docker) runs on every commit. The
  Docker-dependent suites (Phases 3–4) run at phase completion, not in
  inner loops.
- Beyond code and tests, four **authored artifacts** are required
  deliverables: the frozen role directives and brief templates (spec §9.2,
  Inv. 20), the role-side verbs ADR (spec §18), `install.sh` + the
  `/onboard` skill (spec §17), and the dashboard.

## 4. Commit-when-green

- Commit at every green micro-step (the fast suite passing) with messages
  that name the phase/test. **Do not push.** A remote exists
  (`origin`, tracked by `main`), but a user-level deny rule blocks agent
  pushes and the operator pushes manually by decision (2026-07-14). Do not
  attempt it and do not route around the rule.
- **Path-scoped adds only** — `git add .` is banned.
- **Read the diffstat before you commit.** Scripted edits to the ledger
  assert exactly-one-occurrence and refuse to replace the empty string — an
  unread diffstat once cost 366 MB (`0482c27`).
- No force-push. Never amend the other harness's commits. Never
  `git reset --hard`, `git checkout .`, or otherwise destroy uncommitted
  work.
- If a session must end mid-red: leave the working tree as-is and record
  the failing test and the intended fix in `PROGRESS.md`. The red state is
  data, not a mess.

## 5. Ledger discipline

Cross-session, cross-harness memory is **two files with different read
disciplines**; the conversation is disposable.

> **This section supersedes handoff §5 and its §1.3 artifact table**, which
> still describe `PROGRESS.md` as an append-only chronology and the only
> memory. It is not — do not "repair" it by appending history back in. The
> handoff is frozen operator input; the reword is logged under Spec impact in
> `IMPLEMENTATION-NOTES.md` (2026-07-15).

**`PROGRESS.md` — state. Read whole at every startup, so keep it small
(~200 lines).** Header block (last green SHA, which phases' tests pass, any
known-failing test with its repro), the phase checklist, live `## Parked`,
and **exactly one `NEXT:` line, at the bottom**. Nothing here is append-only:
when a fact stops being true, fix it or delete it.

**`docs/ledger/phase-N.md` — history. Append-only. Never read at startup.**
The narrative of what happened and why, one heading per session. Grep it when
you need a rationale; never load it to get oriented. On a phase boundary the
file closes and a new one opens.

At the end of a meaningful step: append the session's entry to the ledger,
move the outgoing `NEXT:` there with it, and leave one new `NEXT:` in
`PROGRESS.md`.

**Tombstones are banned.** A resolved `Parked` item is deleted, not struck
through — git and the ledger hold the history. Struck-through text still
costs tokens and still parses as instruction; it has already produced three
contradictory `Parked` claims where the struck one was the true one. A live
directive never lives inside a tombstone — it belongs here or in the header.

## 6. The deviation rule

Deviations from the spec land in `IMPLEMENTATION-NOTES.md` (append-only,
newest last, entry template at the top of that file). *Planned* designs the
spec delegates go in `docs/adr/` instead.

**"Conservative" is defined, not vibes**: the option that (a) preserves
the 26 invariants and the fail-closed posture, (b) deviates least from
the spec's text, and (c) is easiest to reverse later.

**Log-and-go is the default.** An entry is informational unless the gap
needs operator-only input or breaks an invariant — only then does the item
park (never the whole run; other phases continue).

## 7. No operator questions

Never ask the operator anything except on genuine scope change. Anything
needing operator-only input goes under `## Parked` in `PROGRESS.md` with a
one-line decision request; work continues on other phases. The operator
sweeps Parked daily.

## 8. Quota is scheduled, not reacted to

Note remaining-quota signals (rate-limit headers, plan warnings) in
`PROGRESS.md` and hand off **proactively at the next green commit** when
quota is low — a mid-red quota death is the worst handoff. Claude Code
sessions should target green-commit boundaries every ~2–4 h regardless.

## 9. Fan-out posture (harness-neutral)

Fan out read-only subagents where sub-tasks are independent; keep the TDD
spine sequential. A sub-task is fan-out-worthy when its full working
context can be discarded once a small artifact (a result file, a verdict,
a diff) returns.

- **Write a task prompt that stands alone** (not a "brief" — see §0). Spawned
  agents inherit this file (§0) but not the conversation. Name the file, the
  question, and the return shape.
- **Prefer returning data over writing files.** Let fan-out agents read and
  report; apply their output yourself, in one reviewable place. Parallel
  writers are for genuinely disjoint paths, and cost a worktree each.
- **Worktrees hold uncommitted work.** Review agents leave untracked probes
  in `.claude/worktrees/`. §4 applies to them: that work is data. They are
  git-excluded already — if they pollute a search, exclude them from the
  search, do not delete them.

## 10. The kickoff sentence (verbatim)

> Continue the Mission Control implementation from commit `<sha>`, phase
> `<P-n>`. Follow the session protocol in AGENTS.md; read PROGRESS.md; do
> not invent scope; stop rather than guess missing operator inputs.

## Environment facts (told verbatim, handoff §4.3)

- Primary target is this macOS machine (Apple Silicon — base image must be
  arm64; the Playwright browser adds ~1–2 GB to it).
- Network egress is available for image pulls and npm/go module downloads.
- `MC_HOME` for development: use a scratch path (recorded in
  `OPERATOR-INPUTS.md`), never `~/.mission-control`, until acceptance.
- The prior evidence in `docs/priors/` is trusted — do not re-derive it.
- Homie's `native.jsonl` resume substrate is a version-churned
  harness-internal format — the conversation-rows fallback (spec §15.4) is
  the designed answer; a harness version bump that breaks resume is an
  ADR-worthy event, and harness pins must not drift casually.
- File-watching across VirtioFS bind mounts is unreliable — anything
  watching bind-mounted paths polls, never trusts fsnotify.
- launchd loading during development: **never** (spike S7 is the sanctioned
  exception, unloaded after). Real loading happens once, in the
  operator-present endgame session (Phase 5).
- `OPERATOR-INPUTS.md` holds live secrets: never commit it, never copy its
  values into any tracked file, container image, or log.
