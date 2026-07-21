# AGENTS.md — Mission Control standing instruction

Both harnesses reload this file every session. The behavioral contract is
`specs/mission-control-spec.md`; the operating protocol is
`specs/implementation-handoff.md`; the spec wins on conflict. Neither is a
startup read. Open only the task-named section, unless a holistic conflict
review is genuinely necessary.

## 0. Scope

Sections 1–3, 5, and 7–8 govern session-level agents resuming implementation.
A spawned agent with a specific review, probe, search, or artifact has only
that task: do not run the resume ritual, read `PROGRESS.md`, run the suite, or
follow `NEXT:`. Return the requested artifact and stop.

Sections 4 and 6 bind every agent and cannot be overridden by a task prompt:
use path-scoped adds, never force-push or destroy uncommitted work, and log
unresolved judgment calls. Work in `.claude/worktrees/` is also uncommitted
data; never delete it.

A spawned agent receives a **task prompt**. A Mission Control **brief** is the
immutable `mc.spawn-brief.v1` pipeline carrier (ADR-008, ADR-020 D4); do not
confuse them. Assume spawned agents inherit this file and give them standalone
prompts containing the file, question, and return shape.

## 1. Session start

Read exactly this closed set:

1. All of `PROGRESS.md`. It must stay near 200 lines; if oversized, compact it
   before implementation. `docs/ledger/` is history, never startup context.
2. `git log --oneline -10` and `git status`. Uncommitted work is data.
3. The fixed Phase 1–2 fast suite named in `PROGRESS.md` (no Docker). If none
   exists, building it is the current task.
4. The single artifact `NEXT:` names, if any: for an ADR, use
   `docs/adr/INDEX.md` and read only the named Decision; otherwise read only
   the named phase contract or spike `RESULT.md`.
5. Continue from the final `NEXT:` line; do not ask what to do next.

Do not read the spec, handoff, `IMPLEMENTATION-NOTES.md`, `docs/ledger/`,
`docs/priors/`, or unnamed ADRs at startup. Consult them only when the task, a
failure, or a genuine ambiguity requires them.

## 2. Cross-harness takeover

Before building on work produced by the other harness, spawn a read-only
adversarial review of `git diff <last-reviewed-sha>..HEAD` against the relevant
`docs/phaseN-contract.md`, not the whole spec or phase surface. The standalone
review returns a verdict. Record findings in `IMPLEMENTATION-NOTES.md`; park
only operator-required decisions in `PROGRESS.md`.

## 3. Work order and definition of done

- Phase 0 spikes precede affected implementation; a red spike stops only its
  line until the operator signs the fallback ADR.
- TDD through handoff Part 3 in order. A phase advances only when its complete
  acceptance set passes.
- Run the fast suite on each commit; run Docker-dependent suites at phase
  completion, not in inner loops.
- Required authored deliverables remain: frozen role directives and brief
  templates (spec §9.2, Inv. 20), role-side verbs ADR (spec §18),
  `install.sh` plus `/onboard` (spec §17), and the dashboard.

## 4. Commit-when-green and git safety

- Commit each green micro-step with a phase/test message. Do not push; the
  operator pushes manually. Do not route around the deny rule.
- Stage explicit paths only; `git add .` is banned. Read the diffstat first.
- Scripted ledger edits must assert exactly one match and refuse an empty
  replacement string.
- Never force-push, amend another harness's commit, run `git reset --hard` or
  `git checkout .`, or otherwise destroy uncommitted work.
- If ending mid-red, leave the tree intact and record the failing test, repro,
  and intended fix in `PROGRESS.md`.

## 5. Cross-session memory

This section supersedes handoff §5 and its §1.3 artifact table.

`PROGRESS.md` is current state, read whole at startup and kept near 200 lines:
last green SHA, passing phases, known failures with repros, open phase rows,
live `## Parked`, and exactly one `NEXT:` line at the bottom. It is not
append-only; delete or correct stale facts.

`docs/ledger/phase-N.md` is append-only history, never a startup read. Add one
heading per session recording what happened and why. Grep it for rationale
instead of loading it for orientation. At a phase boundary, close it and open
the next.

At a meaningful step, append the outgoing state and `NEXT:` to the phase
ledger, then leave one new `NEXT:` in `PROGRESS.md`.

Tombstones are banned. Delete resolved Parked items rather than striking them
through; git and the ledger preserve history. Live directives belong in current
state, never inside historical text.

## 6. Deviations and delegated design

Append spec deviations and uncovered judgment calls to
`IMPLEMENTATION-NOTES.md`, newest last, using its entry template.

ADRs are agent-authored design records, not independent authority; they never
supersede the spec. Use one only for a design the spec explicitly delegates.

Before relying on an ADR, classify its claim:
explicitly delegated → binding within the spec's constraints; conservative
internal mechanism → permissible but derivative and reversible; new behavior
operator policy → no authority without operator approval.

The conservative choice preserves all 26 invariants and fail-closed posture,
deviates least from the spec, and is easiest to reverse. If an ADR needs to be made but invalidates one of the 26 invariants, then let the author know a decision must be made. This is last case scenario.


Log-and-go unless operator-only input is required or an invariant breaks. Park
only that decision; continue independent work.

## 7. Operator input and quota

Do not ask the operator except for a genuine scope change. Put operator-only
decisions as one-line requests under `PROGRESS.md` `## Parked`; continue other
work. Record quota signals in `PROGRESS.md`; when quota is low, hand off
proactively at the next green commit. Target green boundaries every 2–4 hours
rather than risking a mid-red stop.

## 8. Fan-out

Fan out independent read-only work whose context can be discarded after a
small result; keep the TDD spine sequential. Prefer returned data over file
writes. Use parallel writers only for disjoint paths, each in its own worktree,
and review them centrally. Spawned prompts must stand alone. Exclude
`.claude/worktrees/` from noisy searches; never delete their uncommitted probes.

## 9. Environment and fixed constraints

- Primary target: this Apple Silicon Mac; images must be arm64. Network egress
  is available for image pulls and module downloads.
- Development `MC_HOME` is the scratch path in `OPERATOR-INPUTS.md`, never
  `~/.mission-control`, until acceptance.
- Evidence in `docs/priors/` is trusted; do not re-derive it.
- `native.jsonl` is version-churned harness internals. The conversation-row
  fallback is deliberate (spec §15.4); pin drift or broken resume needs an ADR.
- VirtioFS bind watchers poll; never trust fsnotify.
- Never load launchd during development except the sanctioned, unloaded S7
  spike. Real loading occurs once with the operator in Phase 5.
- `OPERATOR-INPUTS.md` contains live secrets: never commit it or copy values
  into tracked files, images, or logs.

Kickoff (verbatim): `Continue the Mission Control implementation from commit
<sha>, phase <P-n>. Follow the session protocol in AGENTS.md; read PROGRESS.md;
do not invent scope; stop rather than guess missing operator inputs.`
