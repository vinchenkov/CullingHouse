# ADR-008: immutable spawn-brief carrier

- Status: accepted
- Date: 2026-07-12
- Scope: spec §9.2, §10, §11.5; Inv. 10, 12, 20

## Decision

`mc dispatch` renders the complete opening brief inside the same
`BEGIN IMMEDIATE` transaction that selects the action and claims the lease.
The spawn effect carries that rendered string, and the resident copies it
byte-for-byte into `run.json`. The resident and runner never reconstruct or
augment semantic brief state.

The frozen carrier is `mc.spawn-brief.v1`: a short product-owned heading plus
deterministic indented JSON. It contains the resolved role and the role's
immutable inputs from the claim snapshot:

- every subject role: the task/initiative record, including criteria,
  correction budget, landing references, current refinement notes, and the
  latest `mc complete --outputs` report/artifact reference — **except
  `editor(plan-review)`, which is the one role denied that last field**
  (ADR-020 D4): on an initiative subject it resolves to
  Strategist(initiative)'s own mandatory completion report, i.e. the
  producer's authored prose handed to its own judge, which Inv. 9 and §3's
  "blind to Strategist reasoning" forbid. Records-only does not buy that
  blindness — the record is a pointer to an artifact — so the suppression is
  explicit;
- Editor: the full records for the exact proposed-pool snapshot;
- Editor(plan-review): the full records of every open wave child — the exact
  set the holistic verdict is rendered over (ADR-020 D4). Rides
  `runs.pool_snapshot`, whose meaning generalizes from "the proposed pool this
  Editor run must cover exactly" to "the id set this Editor run was shown and
  must act on exactly"; a reader must know the mode to know the vocabulary;
- Strategist(propose): recently rejected titles as dedupe memory;
- Strategist(initiative): the latest **unanswered** wave plan-review objection
  (`plan_review_sendback`; a later `wave.passed` retires it — ADR-020 D4),
  which is distinct from and never written into `refine_notes`;
- Worker: the latest CORRECT verdict and correction-file path;
- Packager: the latest verdict, evidence path, deepening mark, and a computed
  `exception_labeled` bit for BUDGET-SPENT;
- Strategist(console): the live Review Queue and blocked-task records.

The fixed role directives and prose rubrics remain a separate tracked
artifact under `mc/verbs/directives/`, embedded in the `mc` binary. Every v1
document carries its role's directive alongside the typed state; directives
do not change who owns state assembly.

## Consequences

- The decision-to-effect gap cannot change an agent's opening facts.
- A resident restart needs no direct spine read and cannot drop a carrier.
- Brief changes are schema changes: add a new version and tests rather than
  silently changing the meaning of `mc.spawn-brief.v1`.
- Paths remain references to the file plane; large file contents do not enter
  the spine or effect JSON.
