# ADR-020 — Initiative wave plan review (the Editor's holistic gate)

## Status

**Accepted after adversarial review, and implemented** (2026-07-14). D1–D5 are
green on the fast lane and `mc strategist wave` is CLI-wired — the Parked entry
this ADR exists to close is retired.

Two things the build changed from the text above, both recorded in
IMPLEMENTATION-NOTES.md rather than silently: D1's §4 row is logged as an
additive §4-class rule instead of edited into the spec, and D5's send-back is
logged as the first non-operator-rooted `decision = 'cancelled'`. One thing the
build found that no lens did: `loadRecords` never read `plan_reviewed`, so
without D2(e) every real spine child projected as unreviewed and the whole wave
lane inverted — children never dispatching, the Editor re-reviewing forever. A
red differential caught it; the pure layer is only as true as its projection.

Resolves ADR-001 Open Question 1, `docs/phase2-contract.md` A-P2-7, and the
`## Parked` entry "Initiative wave holistic Editor review" — parked 2026-07-12,
**decided by the operator 2026-07-14** in favour of reading (i).

The review — four decorrelated lenses (code-cite fidelity, dispatch/deadlock,
substrate/state-law, spec/invariants), each finding then judged by an independent
skeptic with no default verdict — raised 12 findings and confirmed 11. All 11 are
folded into the text above; the record, with full evidence and per-finding
reasoning, is `docs/reviews/2026-07-14-adr-020-review.json`, and the draft it
judged is commit `d19c7e9`. The material corrections:

- **the one major** — D4 asserted the Editor's blindness to the producer
  (Inv. 9) came "for free" from the brief being records-only. It does not:
  `brief.go:74-84` loads `LatestOutputPath` outside every role gate, and on an
  initiative subject that pointer resolves to Strategist(initiative)'s own
  mandatory completion report. D4 now *specifies* the suppression, and the
  Consequences' Inv. 9 argument no longer claims it is free.
- **fail-open SQL** — D2(b)'s rendering dropped `planReviewPending`'s own
  `status = 'seeded'` conjunct (latent, but the two forms disagreed).
- **two overstatements retracted** — the storage trigger cannot prevent a git
  *commit*, only the record of one (D1 now names the residual failure mode); and
  `initiatives_archive_cascade` is not a precedent for a non-operator
  `decision = 'cancelled'`, so D5 now logs this ADR as the first such widening.
- **an unimplementable instruction** — D1's §4 row named a "rule list in the
  schema header" that exists nowhere; the row is now recorded in
  IMPLEMENTATION-NOTES rather than written into the spec.
- **an unbounded brief field** — `plan_review_sendback` now has a recency rule.
- plus two wrong code cites (sharp edge 1's `dispatch.go:362`; D5's
  `domain.Cancel` call sites) and sharp edge 1's escapability claim.

One finding was **refuted** and is recorded so it is not re-litigated: that
keeping `mc.spawn-brief.v1` violates ADR-008, whose stated remedy for a brief
change is "add a new version". The additions here are additive and role-gated, so
no existing role's v1 brief changes meaning; the version stays.

## Context

Spec §3 makes the Editor's **holistic plan review** of every wave a mandatory
stage ("Also performs the holistic plan review of every wave a
Strategist(initiative) composes (§6.1)"), and Inv. 12 binds it to a purpose:
"its holistic plan review holds wave children to the same bar" — done-ness
written *before* work starts. §6.1 repeats it: children "are born `seeded` —
their review is the Editor's holistic plan review of the wave, never a per-child
contest in the proposal pool".

Nothing in the spec gives that review a state or a slot, and three mechanisms
actively close the door on it:

1. **Children are born `seeded`** — `tasks_birth_rules`
   (`mc/substrate/schema.sql:155`) *requires* it, and `domain.BirthWave`
   (`mc/domain/initiative.go:70`) inserts exactly that.
2. **§10 query (3) dispatches a `seeded` task to a Worker** — the
   `(status, scope)` table, implemented in `dispatch.spawnFor`
   (`mc/dispatch/dispatch.go:653`). A freshly born child is dispatchable on the
   very next tick, before any judge has seen it.
3. **§10 parks an initiative with open children** — the `NOT EXISTS` arm of
   query (3), implemented as `hasOpenChildren` (`mc/dispatch/dispatch.go:622`).
   The initiative is invisible for as long as the wave is open, so there is no
   moment at which the wave, as a wave, is owed anything.

The consequence is live today: `mc strategist wave` is deliberately not
CLI-wired, and the Strategist(initiative) directive
(`mc/verbs/directives/strategist-initiative.md:18`) carries the line "(The wave
terminal remains disabled until the durable holistic Editor-review state is
defined.)" A promoted initiative proposal dead-ends — it can only declare done
with zero children (strict drain passes trivially) or block out.

**The operator's decision (2026-07-14) pins reading (i)**: a review gate between
wave birth and first child dispatch. This ADR specifies it. The pinned shape,
taken as given:

- a `plan_reviewed` flag on wave children, born 0;
- query (3) will not dispatch a child at `plan_reviewed = 0`;
- a new dispatch arm makes an initiative with any unreviewed open child visible
  to the **Editor** — the one deliberate exception to §10's parked rule, which
  would otherwise wedge the initiative forever;
- the Editor terminal either passes the wave (all children → 1) or sends it back
  in prose (children cancelled through the existing archive cascade, the
  initiative returns to drained, Strategist(initiative) replans);
- "holistic" means **wave-level pass/fail**, never per-child rejection.

**Readings (ii) and (iii) are rejected** and recorded here so they are not
re-litigated:

- **(iii) review inside the wave transaction, before birth** — dead: it puts the
  judgment in the producer's own session and runtime, breaking Inv. 9
  (producer ≠ judge on decorrelated runtimes) and Inv. 2's
  agents-never-dispatch-successors posture. It is the one reading the spec
  forbids outright.
- **(ii) plan review folded into the next Editor batch pass** — dead: it
  collapses into (i) without solving anything. `mc editor decide`'s pass is
  selected by query (3) from a `proposed` row, but the children are already
  `seeded` and rank *above* the proposed pool under furthest-first, so a Worker
  claims a child on an earlier tick than the batch pass it was supposed to wait
  for. Any repair of that ordering *is* a gate on child dispatch — i.e. reading
  (i). It also forces the batch verb to mix two unrelated contests (contrastive
  rank-then-cut over proposals vs. a holistic yes/no over one wave) under D4's
  exact-pool coverage rule and §3's zero-promotion guard, neither of which has
  any meaning for a wave.

Constraints this design is written against: ADR-001 D1–D4 and D6 (verb grammar,
identity/fencing, terminal semantics, the verb-by-scope table), ADR-008
(`mc.spawn-brief.v1` is rendered inside the claim transaction and is the run's
immutable opening input), spec §4's make-impossible-states-impossible table, and
the 26 invariants.

## Decision

### D1. The carrier: `tasks.plan_reviewed`, born 0, one-way, meaningful only on a wave child

One additive column on `tasks` (the NOTE(P1b.1) additive-column pattern; the
default preserves every existing behaviour):

```sql
plan_reviewed INTEGER NOT NULL DEFAULT 0 CHECK (plan_reviewed IN (0, 1)),
-- only a wave child can ever carry the reviewed mark; on every other row the
-- column is pinned at 0 and reads as "not applicable", never as "unreviewed".
CHECK (initiative_id IS NOT NULL OR plan_reviewed = 0)
```

`plan_reviewed` is **not a status** and does not touch `stage_rank`. It is a
dispatchability flag in the same family as `blocked` — §5's reasoning applies
verbatim: it must not destroy pipeline position, and a status would.
Inv. 10 is untouched: no stage completes here.

The column defaults to 0 rather than to 1-for-non-children because fail-closed
means an unreviewed row is the default state of the world and a review is the
only thing that changes it. The paired `CHECK` is what keeps that safe: a row
that is not a wave child can never carry 1, so the value `1` has exactly one
meaning anywhere in the spine — *a wave child whose plan the Editor passed*.

Three substrate rules, in `mc/substrate/schema.sql`, and a fourth row for §4's
enforcement table:

- **Born unreviewed.** Extend `tasks_birth_rules` (schema.sql:155) with
  `WHEN NEW.plan_reviewed <> 0 THEN RAISE(ABORT, 'tasks are born unreviewed
  (ADR-020 D1)')` — the NOTE(P1.2) "born undecided and unarchived" symmetry.
- **One-way, live rows only.** A new `tasks_plan_review_one_way`:
  `BEFORE UPDATE OF plan_reviewed WHEN NEW.plan_reviewed <> OLD.plan_reviewed
  AND (OLD.plan_reviewed = 1 OR OLD.archived = 1 OR OLD.decision IS NOT NULL
  OR OLD.initiative_id IS NULL)` → ABORT. The mark never clears (a send-back
  destroys the children instead of un-reviewing them — D5), never lands on a
  decided or archived row, and never lands on a non-child.
- **The gate, in storage.** A new `children_work_requires_plan_review`:
  `BEFORE UPDATE OF status WHEN OLD.initiative_id IS NOT NULL AND
  OLD.plan_reviewed = 0 AND OLD.status = 'seeded' AND NEW.status = 'worked'` →
  ABORT. This is the redundant backstop doing exactly the job §4 assigns it —
  **and no more than that job**. Stated precisely: a bug in `mc dispatch` cannot
  make the spine *record* a `seeded → worked` advance on a child the Editor never
  passed. It cannot prevent the *commit*, and this ADR does not claim it does.
  §4's own scope is storage ("a bug in `mc` cannot write an illegal state,
  because the storage rejects it" — states, not filesystem effects), and §6.2 is
  explicit that the git plane is "contract enforced by `mc`'s runtime and the
  role briefs, not by SQLite": every mutating git operation runs inside the
  container, and `mc/verbs/complete.go` performs no git operation at all. The
  ordering is commit-then-record (`worker.md` directs "finish with one committed
  branch state", *then* the terminal `mc complete`), so under the hypothesized
  dispatch bug the trigger fires strictly after the commit exists.

  **The residual failure mode, named rather than papered over**: a wrongly
  dispatched Worker commits unreviewed changes to the initiative's shared branch,
  its `mc complete` aborts, and the run exits without an accepted terminal — so
  the lease rules recover it and charge `dispatch_retries` on the child (§10).
  The blast radius is bounded by Inv. 25's topology: the changes sit on the
  initiative's own branch and worktree, and merging into main always requires an
  explicit operator approval, which a never-packaged child cannot reach. The
  spine stays legal; the branch carries dead bytes. Accepted — the dispatch-side
  filter (D2) is the policy, this is the fence, and a fence that keeps illegal
  state out of storage is worth having even though it cannot reach into git.
- **§4 enforcement row — recorded, not written into the spec.** The rule "wave
  children work only after the Editor's plan review", enforced by
  `children_work_requires_plan_review`, is a new row of §4's *kind*. The §4 table
  lives in `specs/mission-control-spec.md:79-89` — the behavioral contract, which
  AGENTS.md says wins on conflict — not in `mc/substrate/schema.sql`, whose header
  (lines 1-18) is continuous prose and contains no rule list. (An earlier draft of
  this bullet directed the row into a "spec-mirroring rule list in the schema
  header" that does not exist anywhere in the repo.) This ADR therefore does not
  edit the spec: the row is recorded in `IMPLEMENTATION-NOTES.md` as an additive
  §4-class rule per AGENTS.md §6, and the trigger carries a comment naming the
  rule it enforces, matching how every other trigger in the file cites its spec
  clause.

`plan_reviewed` is deliberately **not** added to `tasks_identity_immutable`
(schema.sql:260) — it must move, exactly once, 0 → 1.

### D2. Dispatch: the gate and the new arm, in both selecting queries

The pinned shape names query (3). It must also land in query (2a), and this is
load-bearing rather than tidy: §8's at-cap rule 1 says "For an initiative's arc
packet the in-flight work includes its wave children, so they keep dispatching
at cap too", and (2a) is where that happens (`inFlightRefinement`,
`mc/dispatch/dispatch.go:515`, whose join arm `p.task_id = t.initiative_id`
exists for precisely this). Gate the children in (3) only, and an initiative
bounced back from operator review deadlocks the refinement lane — see the sharp
edge in Consequences.

Two record predicates, defined once and used by both queries:

```
planReviewPending(I) :=  I.scope = 'initiative'
                     AND I.status = 'seeded'
                     AND EXISTS (child C : C.initiative_id = I.id
                                        AND C.archived = 0
                                        AND C.plan_reviewed = 0)

childGate(T)         :=  T.initiative_id IS NULL OR T.plan_reviewed = 1
```

**(a) The child gate.** Both `nextDispatch` (dispatch.go:616) and
`inFlightRefinement` (dispatch.go:515) add `childGate(t)` to their candidate
filters, beside the existing `archived`/`blocked`/rank tests. An unreviewed
child is invisible to dispatch, full stop.

**(b) The parked-rule exception.** Both queries currently drop an initiative via
`hasOpenChildren` (dispatch.go:622, dispatch.go:531). That test becomes
`hasOpenChildren(rec, t.ID) && !planReviewPending(rec, t)`: an initiative with
open children stays parked **unless** its wave is unreviewed, in which case it
is visible. In SQL terms the §10 (3) `NOT EXISTS` arm becomes:

```sql
AND (scope = 'task'
     OR NOT EXISTS (SELECT 1 FROM tasks c
                    WHERE c.initiative_id = tasks.id AND c.archived = 0)
     OR (tasks.status = 'seeded'
         AND EXISTS (SELECT 1 FROM tasks c
                     WHERE c.initiative_id = tasks.id AND c.archived = 0
                       AND c.plan_reviewed = 0)))
```

The `tasks.status = 'seeded'` conjunct is not decoration — it is
`planReviewPending`'s own conjunct, and the SQL is a rendering of that predicate,
so dropping it makes the two forms genuinely disagree on the fail-open side.
(`I.scope = 'initiative'` *is* legitimately absorbed by the leading
`scope = 'task'` disjunct; status is absorbed by nothing. §10 query (3)'s
`stage_rank > 0` excludes only `packaged`, not `worked`/`verified`.) Without it,
an initiative at `worked`/`verified` with open unreviewed children would be
visible to (3), where the unchanged (status, scope) table maps it to
Verifier/Packager rather than to the plan review, while the Go form would park it.
The divergence is currently latent — `initiatives_declare_requires_drained`
(schema.sql:250-256) and `tasks_birth_rules` (schema.sql:155) make an initiative
past `seeded` with an open child unreachable — but a latent divergence between the
two forms of one predicate is exactly what a differential suite exists to catch,
so both forms carry the conjunct.

**(c) The role map.** The `(status, scope)` table's `seeded, initiative` row
splits on the same predicate, in `dispatch.spawnFor` (dispatch.go:653), which
already receives `rec`:

| (status, scope) | condition | Dispatch |
|---|---|---|
| `seeded`, initiative | `planReviewPending` | **Editor (plan review)** — the wave, held to Inv. 12's bar |
| `seeded`, initiative | otherwise (drained) | **Strategist(initiative)** — unchanged |

**(d) Step number and precedence: none.** The arm is deliberately *not* a new
numbered step. It changes step (3)'s (and (2a)'s) visibility predicate and role
map and nothing else, so the existing `ORDER BY` decides its precedence with no
new rule: expedite lane first, then furthest-first, then priority, then age,
then id (`lessNextDispatch`, dispatch.go:634). This is the conservative choice
under AGENTS.md §6 — it deviates least from §10's text (which numbers steps by
*kind* of move: reap, console, land, occupancy, the at-cap pair, the single
next dispatch), it preserves Inv. 3 (still exactly one action per tick), it
inherits the expedite lane for free (a P-1 operator proposal still pulls the
Editor ahead of a plan review), and it is reversed by deleting a predicate
rather than by renumbering a spec walk.

The one consequence to state plainly: the initiative row sits at `stage_rank`
2 (`seeded`) and the proposed pool at rank 1, so **a pending plan review
outranks the ordinary contrastive pool pass** under furthest-first. That is the
intended reading of "finish what is closest to done before starting anything
new" — the wave is live work, the pool is not-yet-work.

**(e) The dispatch projection.** `dispatch.Task` gains `PlanReviewed bool`;
`loadRecords` (`mc/verbs/dispatchverb.go:192`) adds `plan_reviewed` to its
`SELECT`. The `tasks_dispatch` index (schema.sql:149) needs no change — the
loader reads whole tables and `Decide` is pure.

### D3. The role: `editor(plan-review)`, a mode, exactly like Strategist's three

A new `dispatch.Role` constant `RoleEditorPlanReview Role = "editor(plan-review)"`.

This reuses the Strategist-mode mechanism unchanged and adds no machinery:

- `baseRole()` (`mc/verbs/verbs.go:113`) strips the parenthesized mode, so
  `lock.owner` and `runs.role` receive the flat `editor` the schema CHECKs
  already permit (schema.sql:582, schema.sql:633). No enum change.
- `resolveSpawnRoute` (`mc/verbs/dispatchverb.go:411`) resolves by base role, so
  the plan review routes to the Editor's existing `codex` / `chatgpt` binding
  with **no `routing.md` change**. Inv. 9 then holds by construction: the wave's
  producer is Strategist(initiative) on `claude-sdk`/`claude`; its judge is a
  fresh Editor session on the decorrelated `codex` family.
- `applySpawn` (dispatchverb.go:392) already puts the *exact* role string in the
  spawn effect, which the resident copies into `run.json`, which is where
  `RunIdentity.Role` comes from. The mode therefore reaches the terminal's fence
  with no new plumbing — the same path that makes `requireExactRole(id,
  "strategist(propose)")` work today (verbs.go:218).

A new frozen directive `mc/verbs/directives/editor-plan-review.md`, embedded and
registered in `directiveForRole` (`mc/verbs/directives.go:39`) — Inv. 20, and an
`mc/verbs/directives_test.go` obligation. It is a separate document rather than a
reuse of `editor.md` because the job is genuinely different: `editor.md` says
"Judge the entire `proposed_pool` contrastively … Rank leverage per cost", which
is a *contest*; the plan review is a holistic yes/no over one wave against one
charter. It must say, at minimum: judge the wave as a whole against the
initiative's charter and Inv. 12's bar (every child's description states
checkable success criteria; the wave, taken together, is the coherent
*currently-actionable* step the charter's next increment needs, §6.1); pass or
send back, never per-child; a send-back's reason is the objection
Strategist(initiative) must answer, so it is written as prose an author can act
on; orchestrate by default (Inv. 14, read-only depth-1 subagents for
criterion-checkability audits and charter-coverage checks; the top-level run
keeps the verdict); exactly one terminal action. The line disabling the wave
terminal is dropped from `strategist-initiative.md` when this lands.

### D4. The brief: `mc.spawn-brief.v1` gains a wave, and the snapshot rides `pool_snapshot`

The Editor's plan-review brief needs three things, all from the claim
transaction's snapshot (ADR-008):

- **the charter** — `doc.Subject` already carries it: the lease subject is the
  initiative, and `briefTask.Description` is the charter (`mc/verbs/brief.go:68`,
  brief.go:144). No change.
- **the wave** — a new field on `spawnBriefDocument`, `wave []briefTask`, holding
  the full record of every open child (title, `description` = the child's
  checkable criteria, priority), loaded by the existing `loadBriefTask`. Rendered
  only for `RoleEditorPlanReview`, exactly as `proposed_pool` is rendered only
  for `RoleEditor` (brief.go:87).
- **nothing else — and this costs one gate, it is not free.** The Strategist's
  reasoning must not reach the judge: §3's "blind to Strategist(propose)'s
  reasoning" is the same blindness the Leverage Gate depends on (Inv. 9). Being
  built from records only does **not** buy that blindness, because one record is
  a pointer to producer-authored prose. `buildSpawnBrief` loads
  `LatestOutputPath` for *every* spawn with a non-nil `SubjectID`
  (`mc/verbs/brief.go:74-84`) — the block sits outside the `RoleEditor` gate at
  brief.go:87 and is the sole un-role-gated enrichment. Its query is
  `SELECT output_path FROM runs WHERE subject = ? AND output_path IS NOT NULL
  ORDER BY created_at DESC, id DESC LIMIT 1`, and for an initiative subject the
  row it finds is **Strategist(initiative)'s own completion report**: `mc
  complete` *requires* `--outputs` on the initiative done-declaration
  (`mc/verbs/complete.go:110-115`) and writes it to `runs.output_path`
  (complete.go:203-206) on a run whose `subject` is the initiative
  (`mc/domain/lease.go:89-93`).

  So: **`LatestOutputPath` must be suppressed for `RoleEditorPlanReview`** — the
  conservative shape is to keep the load where it is and clear/skip it for that
  one role, since every other subject role's brief is unchanged by construction.
  Without this line the judge reads the producer's own report and the
  decorrelation this gate exists to provide is defeated.

  The leak is invisible on a virgin initiative's *first* plan review (no prior
  initiative-subject run carries `output_path`, so the field is empty) and opens
  on every plan review after an arc round-trip (§6.1's `packaged → seeded`, or
  the correction rally). A property that holds only until the first round-trip
  is not a property, so the suppression is specified here rather than left to
  the test to discover.

**The child-id snapshot reuses `runs.pool_snapshot`** (schema.sql:644) with no
schema change. The column's meaning generalizes from "the proposed pool this
Editor run must cover exactly" to **"the id set this Editor run was shown and
must act on exactly"** — which is what it always was; the pool was merely its
only instance. `dispatch.Spawn` gains a distinct `Wave []int64` field (rather
than overloading `ProposedPool`, which is named for what it is), and
`applySpawn` (dispatchverb.go:359) passes whichever of the two the Editor mode
populated into `ClaimArgs.PoolSnapshot`. Ascending id order, deterministic, same
as `proposedPool` (dispatch.go:708).

The Strategist(initiative) brief gains one field, `plan_review_sendback`: the
latest **unanswered** `wave.sent_back` activity row for this initiative (prose +
timestamp), rendered only for `RoleStrategistInitiative`. Unanswered is the
load-bearing word and needs a rule, because `activity` is append-only
(schema.sql:693, schema.sql:699) and an initiative has *many* wave boundaries
(§6.1's lazy decomposition "at each boundary" under strict drain). "The latest
`wave.sent_back` for this initiative", unqualified, would re-serve a long-answered
objection at every future boundary forever.

**The recency rule, with no new machinery**: render the latest `wave.sent_back`
only if it is **newer than the latest `wave.passed`** for the same initiative
(both rows are D5's own writes, `subject = <initiative id>`; absent a
`wave.passed`, any `wave.sent_back` is unanswered). A send-back is answered
exactly when a replanned wave passes, so this is the precise semantics D3's
directive language already assumes ("the objection Strategist(initiative) must
answer"). It is deliberately not scoped by wave birth: `BirthWave`
(`mc/domain/initiative.go:64-83`) writes no activity row, and adding one purely
to date a brief field would deviate further than reading the two rows this ADR
already creates.

Without the field the send-back is a
silent loop — the Strategist replans blind and re-pitches the wave the Editor
just refused. This deliberately does **not** reuse `tasks.refine_notes`: that
column is single-slot and already owned by the §7 operator-revise / §8 Refiner
path (NOTE(P2.3)), and the two carriers must coexist — an initiative under
refinement whose replanned wave gets sent back needs *both* the operator's
revision notes and the Editor's objections, and writing the send-back into
`refine_notes` would silently destroy operator intent. Reading it from `activity`
instead of adding a column follows NOTE(P2.2)'s established precedent ("the next
Worker brief's correction file is queried from the subject's latest
correct-verdict run — no task column").

### D5. The terminal: `mc editor plan-review`, a new verb

```
mc editor plan-review --run <run_id> --initiative <id> --verdict pass|send-back [--reason <prose>]
```

**A new verb, not an arm of `mc editor decide`.** Overloading `decide` would
force four unrelated changes onto it — a second meaning for its exact-pool
coverage rule, a `promote|reject` vocabulary that fits nothing here, §3's
zero-promotion guard (`mc/verbs/roles.go:103`) made conditional, and a second
transition table — for no gain. ADR-001's own Consequences licenses this
directly: "adding a role someday = new verbs + table row, no scope-mechanism
change". One new row in the D6 verb-by-scope table:

| Verb | host | pipeline-role | pipeline-runner | homie-agent | homie-runner |
|---|---|---|---|---|---|
| `mc editor plan-review` | — | ✓ (editor/plan-review) | — | — | — |

Flags, not a stdin batch: the verdict is one wave-level scalar, not a batch of
per-element decisions, so ADR-001 D1's `--batch -` grammar has nothing to carry.
`mc verifier verdict` is the existing flags-only precedent.

**Provenance, scope, fencing (ADR-001 D2, D6; §18 deny rule 2)** — four checks,
in order, none of them from arguments:

1. `requireExactRole(id, "editor(plan-review)")` — role *and mode* from
   `run.json`. An `editor` pool run cannot invoke it.
2. `requireOwnRun(id, run)` — the `--run` token must equal `run.json`'s own
   `run_id`, so an old same-role container cannot act as a newer holder.
3. `fenceRun(ctx, q, run)` inside the transaction — the token must match the
   live lease, and the returned subject must equal `--initiative` (the
   `StrategistWave` shape, roles.go:274).
4. `requireLive` on the initiative — an initiative decided or archived mid-run
   (an operator cancel) rejects the terminal with `already-decided`/`archived`.

**Reciprocally, `mc editor decide` tightens** from `requireRole(id, "editor")`
(roles.go:47) to `requireExactRole(id, "editor")`. Without it, an
`editor(plan-review)` identity passes base-role matching and reaches `decide`.
The pool run's role string stays the plain `"editor"`, so this is a one-line
fail-closed tightening that costs the existing arm nothing — and it is exactly
what `requireExactRole` was written for (verbs.go:215: "Lock.owner is flat, so
run.json is the only place that can prevent … terminals from crossing").
(Defence in depth, not sole reliance: even if `decide` were reached from a
plan-review run, the children are `seeded`, and `domain.Promote` /
`domain.RejectProposal` refuse anything that is not `proposed`.)

**`--reason` is asymmetric**: mandatory for `send-back`, forbidden for `pass`.
This mirrors §7's operator decision flow verbatim ("reason required for
revise/cancel, forbidden for approve; asymmetric by design") and ADR-001 D4's
Editor reject arm. A pass needs no prose because the work itself is the next
thing that happens; a send-back is worthless without the objection.

**D3-standard terminal semantics** (ADR-001 D3): one transaction writes the
verdict, releases the lease (`releaseLease`), and stamps `endRun`; it never
dispatches (Inv. 2, Inv. 3); a second call is rejected by the released lease.

**The `pass` arm** — one transaction:

1. Require the snapshot (`runs.pool_snapshot`) to **exactly equal** the live
   open-child set of the initiative. This is D4's exact-pool coverage rule
   applied for the same reason it exists there: a holistic verdict asserts a
   property *of a set*, so a verdict rendered over a set that no longer exists
   is stale and is refused (`pool-mismatch`), never partially applied.
2. `UPDATE tasks SET plan_reviewed = 1` for every child in the snapshot.
3. `INSERT INTO activity (actor, kind, subject, detail) VALUES ('editor',
   'wave.passed', <initiative id>, <child ids>)` — Inv. 7; actor is the logical
   originator.
4. `endRun` + `releaseLease`.

**The initiative's own status and decision do not move on either path** — it
stays `seeded`, `decision IS NULL`, unblocked. Inv. 10 is preserved exactly: the
plan review completes no stage of the initiative, and there is nothing to
recompute dispatch from but the flags. After a pass, `planReviewPending` is false
and `hasOpenChildren` is true, so the initiative returns to parked and its
children — now `plan_reviewed = 1` — dispatch to Workers through the unchanged
(status, scope) table.

**The `send-back` arm** — one transaction, reusing the existing cancellation path
rather than inventing a cascade:

1. `requireLive` on the initiative; `--reason` present.
2. For every child in the snapshot **still open** (`archived = 0`), the ordinary
   decision write: `decision = 'cancelled'`, `decided_at = datetime('now')`,
   `archived = 1`. That fires the substrate cascades already in place —
   `tasks_archive_cascades_packet` (schema.sql:381; a no-op, an unreviewed child
   can hold no packet) and `children_block_clears_on_archive` (schema.sql:342).
   Snapshot members already archived are skipped, not errors: this arm asserts
   nothing about the set, it destroys it, and an already-cancelled child is
   already in the target state. (Asymmetric with `pass` by design — `pass`
   asserts a holistic property over a set and so demands the exact set.)
3. `INSERT INTO activity (actor, kind, subject, detail) VALUES ('editor',
   'wave.sent_back', <initiative id>, <reason prose>)` — the send-back's one
   durable store, and D4's brief carrier.
4. `endRun` + `releaseLease`.

The initiative now has zero open children ⇒ **drained** ⇒ the next tick's query
(3) selects it (`seeded`, initiative, `planReviewPending` false) and dispatches
Strategist(initiative), whose brief carries the objection. `BirthWave`'s
overlapping-wave refusal (`initiative.go:53`) passes cleanly because the drain is
real.

**This ADR does introduce the first non-operator-rooted `decision = 'cancelled'`,
and says so plainly.** An earlier draft claimed the precedent already existed —
that `initiatives_archive_cascade` (schema.sql:361) "already writes exactly that
mark, mechanically, with no operator in the loop". The second half is false, and
the review that caught it enumerated every writer of `archived = 1` on `tasks` to
prove it: `Approve` (task.go:409) and `land.go:52` require `packaged`, reachable
only through a strict-drain-guarded `worked` (task.go:361-367), so at archive time
there are zero open children and the cascade body updates zero rows;
`RejectProposal` (task.go:59) is `proposed`-only, and `tasks_birth_rules`
(schema.sql:161-166) admits children only into a `seeded` initiative, so it too
fires the cascade as a no-op; and `Cancel` is operator-gated at both call sites
(`RequireOperatorVerb`, verbs/task.go:127; `requireOperatorVerbTx`,
verbs/packet.go:42). Every firing that actually writes the mark onto a child is
rooted in an operator decision. The cascade is mechanical *propagation* of an
operator's cancel, not an independent writer.

What survives is the weaker, true argument, and it is sufficient: §6's gloss
("`cancelled` by the operator at any stage") describes the operator's verb, not
an exclusive writer; §6.1's own word for what happens to open children is
"cancelled"; and the alternative `decision = 'rejected'` is unavailable
(`RejectProposal` is `proposed`-only, and these children are `seeded`). So the
Editor's send-back is a genuine widening of who may root that mark, chosen because
the vocabulary has no better cell — logged here as the deviation it is rather than
dressed as precedent.

**`domain.Cancel` gains an `actor string` parameter** (`mc/domain/task.go:421`,
which today hard-codes `actor='operator'` in its activity insert). Inv. 7 requires
it — actor is the logical originator — and a parameter beats a parallel
implementation that would drift from `Cancel`'s body. The production call sites are
two, but not the two an earlier draft named: `mc/verbs/task.go:146`, and
`mc/domain/task.go:451` — inside `CancelPacket`'s own body. (`mc/verbs/packet.go:60`
calls `CancelPacket`, not `Cancel`, so the parameter does not reach it.) That
exposes a design point the draft hid: **`CancelPacket` hard-codes `"operator"`**
when it calls `Cancel`, rather than growing the parameter itself — it is the
operator packet-cancel arm by construction (its own doc comment and its
`requireOperatorVerbTx` gate), so it has no other actor to pass. The send-back
calls `Cancel` directly with `"editor"`.

Roughly eleven test call sites also pass through `Cancel` (`initiative_test.go`,
`packet_test.go`, `queue_test.go`, `task_test.go`, `lifecycle_nightly_test.go`);
they are compile-time discoveries at zero risk, but they are mechanical work this
ADR owes an implementer, so they are named here and in "What gets harder".

### D6. The wave is uniform in `plan_reviewed`, by construction

Every open child of an initiative always belongs to exactly one wave and always
shares one `plan_reviewed` value. This is not asserted; it follows:

- `BirthWave` refuses to insert while any child is open (`initiative.go:53`,
  the 2026-07-12 overlapping-waves fix), so two waves can never coexist;
- children are born 0 (D1), all in one transaction (ADR-001 constraint a);
- the pass arm sets all of them to 1 in one transaction (D5);
- the flag never returns to 0 (D1) — a send-back destroys the wave instead.

So "any unreviewed open child" ≡ "the whole wave is unreviewed", and there is no
mixed state in which some children dispatch while their wave is under review.
Partial operator cancellation shrinks a wave but never splits it. This is what
makes D2's single predicate sufficient and D5's wave-level verdict honest.

### D7. Crash recovery: one row on §10's table

| Reaped owner/mode | What the next tick sees |
|---|---|
| `editor(plan-review)` | wave intact, all children still `plan_reviewed = 0`; `planReviewPending` still true; the arm re-fires over the (re-snapshotted) wave with a fresh Editor |

The mirror of the existing `editor` row ("pool intact; the batch re-fires over
the whole pool"), and it needs no rollback for the same reason nothing else does:
the run never mutated its subject (Inv. 10). Note the reaper charges
`dispatch_retries` on the initiative (the run carries it as subject), and at 0
blocks it with the reason rather than looping — §10's existing rule, unchanged.

## Consequences

### What this buys

- **Inv. 12 becomes true for waves.** Today a wave child can be Worked and
  committed before any judge reads its criteria; after this, it is never
  *dispatched* before one does (the D2 filter), and the spine can never *record*
  the advance even if that filter breaks (`children_work_requires_plan_review`).
  The two layers are policy and fence, not two guarantees of the same strength:
  as D1 states, the trigger cannot reach a git commit, so the storage layer
  bounds the damage of a dispatch bug rather than eliminating it.
- **Inv. 9 extends to wave planning** with no new *routing* mechanism: the
  producer is Strategist(initiative) on `claude-sdk`/`claude`, the judge a fresh
  Editor session on `codex`/`chatgpt`, decorrelated by the routing default
  (`resolveSpawnRoute` resolves by base role, D3). Blindness to the producer's
  reasoning, however, is **not** inherited from the brief being records-only —
  it costs the one explicit suppression D4 specifies (`LatestOutputPath`, which
  otherwise carries Strategist(initiative)'s own completion report to its own
  judge). With that gate the invariant holds; without it, it does not.
- **Inv. 8 (feed-forward) is preserved, not bent.** §3's topology is
  `Strategist(propose) → Editor → Worker/Strategist(initiative) → …`, and this
  gate puts the Editor exactly where the topology already has it: judging
  Strategist output before it reaches Workers. The send-back is not upstream
  messaging — it is a rejection travelling as durable state (cancelled rows plus
  an activity row read into the next brief), structurally identical to the
  leverage ledger's rejected titles feeding Strategist(propose)'s dedupe memory
  (§5, §10 step 4), which the spec already licenses.
- **`mc strategist wave` unblocks**, and with it the initiative lane end to end:
  the directive's disabling line and the Parked entry both retire.
- **Inv. 25 holds trivially** on the send-back path: an unreviewed child was
  never dispatchable, so it has no worktree changes and no commit to revert.
  "A killed or rejected child never had its changes committed at all" (§6.2)
  becomes a theorem rather than a hope.
- Inv. 1, 2, 3, 4, 5, 11, 13, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 26 are
  untouched by construction: no new lease semantics (an ordinary leased Editor
  run), no verb that dispatches, still one action per tick, no packet born or
  destroyed, no runtime knowledge above the adapter (routing resolves by base
  role), no new non-reviewable lane, no time input (`plan_reviewed` is a stored
  field, and eligibility stays time-invariant), no scope widening — the
  plan-review run's Worksource is derived from its subject (`domain.Claim`,
  `mc/domain/lease.go:47`), so it is *narrower* than the pool pass, which spans
  Worksources.

### Sharp edges

1. **The at-cap deadlock the pinned shape does not mention — and why (2a) is
   not optional.** Take an initiative bounced back from operator review (§8
   rule 2's initiative arm: `packaged → seeded`, packet still holding its slot),
   queue at cap. Strategist(initiative) births a refinement wave; children are
   unreviewed. If the gate lives only in query (3): (2a) filters the children out
   by `childGate`, and (2b) skips the initiative because `refinementStart`
   requires `status = 'packaged'` (dispatch.go:586) while it now sits at
   `seeded`. The initiative then makes **no autonomous progress ever again**: it
   is invisible to (3) with an unreviewed wave nothing can review, its packet
   holds its slot, and no tick can re-package it. That is exactly the deadlock §8
   rule 1 exists to prevent. D2 therefore lands both the gate and the exception in
   (2a) as well as (3); the (2a) arm maps by the same (status, scope) table it
   already uses (`spawnFor`, called at dispatch.go:359-360 inside the at-cap
   block — *not* dispatch.go:362, which opens the (2b) arm, the one arm that
   returns `KindReenter` without consulting that table), so the Editor plan-review
   arm is reachable at cap. The plan review births no packet, so Inv. 18 is not at
   risk from letting it run at cap.

   Two precision notes, since this edge's argument is the reason (2a) is not
   optional and it should not rest on overstatement. (i) The stall is not
   literally unbreakable, and it is not literally a whole-tick `queue-saturated`
   idle: `refinementStart` scans *all* packets (dispatch.go:363), so any other
   non-saturated packaged packet still yields a Refiner spawn — it is this
   initiative that is stuck, not the queue. (ii) The operator *does* hold a key,
   as they do in sharp edge 5 — but the honest distinction is that this one is
   **destructive** (`mc packet cancel` reaches `domain.Cancel`, archives the
   initiative, and the `initiatives_archive_cascade` / `tasks_archive_cascades_packet`
   triggers annihilate the wave and free the slot), whereas edge 5's is
   **restorative** (unblock resumes the work untouched). A stall whose only exit
   is destroying the initiative is still the deadlock §8 rule 1 forbids, and that
   narrower claim justifies (2a) just as fully.

2. **An unbounded send-back ↔ replan loop is possible, and this ADR does not
   bound it.** Each round is two leased runs and no budget: a send-back is not a
   correction (it never touches the initiative's `correction_count`, and the
   initiative never leaves `seeded`), so nothing counts the rounds. A pathological
   Editor/Strategist pair could ping-pong indefinitely, and because a pending
   plan review outranks the proposal pool under furthest-first (D2d), it would
   starve the ordinary contrastive pool for as long as it ran. Judgment: real,
   but not invariant-breaking (no state corrupts, no slot leaks, the operator's
   attention surface is untouched) and **not this ADR's to fix** — the spec pins
   no budget here, and inventing one would deviate further than logging it
   (AGENTS.md §6). Two mitigations are already in the design: D4's
   `plan_review_sendback` brief field makes a replan answer the objection instead
   of repeating the wave, and every round leaves an `activity` row
   (`kind = 'wave.sent_back'`, `subject = <initiative id>`), so the loop is
   *observable* — a count per initiative is the dashboard signal and the evidence
   a future ADR would need to justify a bound.

3. **An operator cancelling one child mid-review charges the initiative's infra
   budget.** D5's exact-set rule refuses the stale `pass`; the Editor run then
   exits without an accepted terminal and is recovered by the lease rules (§10:
   "a run that exits without an accepted terminal action is recovered by the
   lease rules"), which decrement `dispatch_retries` on the subject — the
   initiative — and at 0 block it. So a rare operator race spends an infra retry
   on work that never had an infra failure, blurring §10's "two budgets, distinct"
   in the one direction the spec did not anticipate. Accepted for now as the
   fail-closed cost of not applying a holistic verdict to a set it was not
   rendered over; it is cheaply reversible (a later revision can give the terminal
   a stale-snapshot arm that frees the lease without charging, which is a strictly
   additive change).

4. **A partially cancelled wave cannot strand an initiative** — the case
   analysis is total, and worth pinning as tests. All children cancelled ⇒ zero
   open ⇒ drained ⇒ Strategist(initiative) replans. Some cancelled, wave
   unreviewed ⇒ survivors still unreviewed ⇒ `planReviewPending` ⇒ the Editor
   sees a smaller wave (D6: never a split one) ⇒ pass or send back. Some
   cancelled, wave passed ⇒ survivors reviewed ⇒ parked ⇒ they dispatch, drain
   normally. There is no reachable combination that is both invisible to dispatch
   and owed something.

5. **A blocked initiative with an unreviewed wave waits for the operator, by
   design.** Both queries exclude `blocked` rows, so an initiative blocked
   directly, or by `children_block_propagates` (schema.sql:305) from a child the
   operator blocked pre-dispatch, takes its unreviewed wave out of sight until
   the operator unblocks. That is §6's categorical rule ("blocked = 1 makes a row
   invisible to dispatch … waits for the operator") applied unchanged, and the
   operator holds the key, so it is a wait and not a wedge. Flagged because it
   *looks* like the parked-initiative wedge this ADR exists to fix, and is not.

6. **A pass under an initiative blocked mid-run lets the children dispatch.**
   Block propagation runs child → initiative, never initiative → children, and
   §6.1 is explicit that "unblocked siblings keep dispatching on their own
   flags". So an operator who blocks the initiative while the Editor is reviewing
   gets a wave that starts working anyway. This is the spec's existing semantics
   for every wave, not a hole this gate opens, and the terminal deliberately does
   **not** add a refuse-if-blocked rule — the pass is a verdict about plan
   quality, not a dispatch decision, and inventing a new blocking interaction
   here would deviate more than inheriting the existing one. Logged, not fixed.

7. **The two Editor modes are indistinguishable in the spine.** `runs.role` and
   `lock.owner` both store the flat `editor` (`baseRole`), so only `run.json`
   knows which mode a run is — and `runs.pool_snapshot` holds proposal ids for
   one mode and child ids for the other. That is the Strategist precedent
   working as designed (mode lives in the brief and the launch envelope, §10),
   and D5's `requireExactRole` on both verbs is what keeps the two from crossing.
   The cost is real and should be stated: a spine-only reader (the dashboard, a
   post-mortem) cannot tell an Editor pool run from an Editor plan review without
   reading its brief or joining `pool_snapshot` back to `tasks.status`.

### Tests that pin it

Phase-2 fast lane (no Docker), red-first, per AGENTS.md §3:

- **Substrate** (`mc/substrate/substrate_test.go`): children born
  `plan_reviewed = 0`; the birth abort on `plan_reviewed = 1`; the
  non-child `CHECK`; one-way (1 → 0 aborts; 0 → 1 on an archived, decided, or
  non-child row aborts); `children_work_requires_plan_review` aborting a
  `seeded → worked` on an unreviewed child *and* permitting it at 1.
- **Dispatch** (`mc/dispatch/dispatch_test.go`, `property_test.go`): an
  unreviewed child is never selected by (3) or (2a); an initiative with an
  unreviewed wave maps to `editor(plan-review)` in both queries; the same
  initiative drained maps to Strategist(initiative); the same initiative with a
  passed wave is parked and its children dispatch; the at-cap refinement-wave
  case of sharp edge 1 does not idle; a P-1 proposal still outranks a pending
  plan review; `Decide` stays pure and total (`spawnFor` never panics on the
  split branch). The SQL differential suite gets the new predicate on both
  queries.
- **Verbs** (`mc/verbs/dispatchverb_test.go`, a `roles_test` arm): the `pass`
  terminal sets every snapshotted child and leaves the initiative's
  status/decision untouched; the exact-set refusal on a mid-run cancellation
  (`pool-mismatch`); the `send-back` terminal cancels open children with
  `actor='editor'`, tolerates already-archived members, drains the initiative,
  and writes `wave.sent_back`; `--reason` required for send-back and forbidden
  for pass; all four fences (wrong mode, wrong run, stale lease, decided
  initiative); `mc editor decide` refused from an `editor(plan-review)` identity
  and still accepted from `editor`; the plan-review brief carries the charter and
  the full wave and nothing else — specifically, **`latest_output_path` is empty
  on a plan review whose initiative already carries a completion report from a
  prior arc round-trip** (D4's suppression; the test must construct the
  round-trip, because a virgin initiative passes it vacuously); the
  Strategist(initiative) brief carries an unanswered send-back, carries **no**
  send-back once a later `wave.passed` answers it, and does not clobber
  `refine_notes`.
- **Scope table** (ADR-001 D6 matrix): `mc editor plan-review` denied at host,
  homie-agent, and runner scopes.
- **Directives** (`mc/verbs/directives_test.go`): every `dispatch.Role`,
  including the new one, resolves to a frozen embedded directive.
- **End of the line**: `mc strategist wave` is CLI-wired only once the above are
  green, and `strategist-initiative.md`'s disabling line is removed in the same
  change.

### What gets harder

- **`mc.spawn-brief.v1` gains fields.** ADR-008 says brief changes are schema
  changes; `wave` and `plan_review_sendback` are additive and role-gated, so v1's
  meaning for every existing role is unchanged — but the ADR-008 field list must
  be updated in the same change, and the golden-brief tests re-pinned. The
  `LatestOutputPath` suppression (D4) is the one *subtractive* brief change, and
  it is role-scoped to `RoleEditorPlanReview`, which no brief carries today;
  ADR-008's "every subject role" field list must gain that exception.
- **`runs.pool_snapshot` now carries two id vocabularies.** Documented in D4 and
  in sharp edge 7; a reader must know the mode.
- **`domain.Cancel`'s signature changes**, touching two production call sites
  (`verbs/task.go:146` and `domain/task.go:451` inside `CancelPacket`, which
  hard-codes `"operator"`) and roughly eleven test call sites across five files.
  All are compile-time discoveries.
- **Reversal cost is low, by construction**: dropping the arm means deleting one
  predicate from two queries, one role constant, one verb, one directive, and
  three triggers; the column can stay at its default and mean nothing. Nothing
  here is a one-way door.
