# AGENTS.md — the standing instruction (Mission Control implementation)

This file is the whole autonomy mechanism. Both harnesses (Claude Code and
Codex) reload it every session. The behavioral contract is
`specs/mission-control-spec.md`; the operating protocol is
`specs/implementation-handoff.md`. **The spec wins on conflict.**

## 1. Session-start protocol (the resume ritual)

1. Read `PROGRESS.md` (header block + tail + the `NEXT:` line).
2. `git log --oneline -20` and `git status`. **Uncommitted work is data,
   never discard it.**
3. Run the test suite (the fast lane: Phases 1–2, no Docker). If no test
   suite exists yet, building it is the current task.
4. Continue from the ledger's `NEXT:` line. Never ask what to do next —
   the ledger says.

## 2. Takeover review

When the previous session ran on the other harness, the first substantive
act is an **adversarial review of the outgoing phase against the spec**
before building on it — the system's own decorrelated producer/judge
principle applied to its own construction. Findings go to
`IMPLEMENTATION-NOTES.md` (and `## Parked` in `PROGRESS.md` if operator
input is needed).

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
  that name the phase/test. Push if a remote exists.
- **Path-scoped adds only** — `git add .` is banned.
- No force-push. Never amend the other harness's commits. Never
  `git reset --hard`, `git checkout .`, or otherwise destroy uncommitted
  work.
- If a session must end mid-red: leave the working tree as-is and record
  the failing test and the intended fix in `PROGRESS.md`. The red state is
  data, not a mess.

## 5. Ledger discipline

Update `PROGRESS.md` after every meaningful step: append-only human
chronology ending in an explicit `NEXT:` line. The header block at the top
carries the last green SHA, which phases' tests currently pass, and any
known-failing test with its repro. It is the only cross-session,
cross-harness memory; the conversation is disposable.

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
