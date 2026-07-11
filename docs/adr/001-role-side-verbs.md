# ADR-001 — Role-side verbs + verb-by-scope table (spec §18 delegated design)

## Status
Accepted (agent-delegated design per spec §18; revisions only via a
superseding ADR). Open questions at the bottom are tracked, not blocking.

## Context

Spec §18 delegates the design of the role-side verbs: the Editor's output is
a *batch* of per-proposal verdicts, Strategist(propose) inserts N proposals
under a subjectless run, Strategist(initiative) inserts a wave in one
transaction, and the Verifier writes a verdict plus a correction file — none
of which fits `mc complete <task> --status`. Three locked constraints:

- (a) batch outputs commit **atomically** — never half-visible to a tick;
- (b) the lease's `subject` is **nullable** — propose-style runs hold the
  lease with no subject task and no placeholder rows;
- (c) the new verbs live in the **pipeline-role scope** — fenced to the
  calling run's identity (read-only `run.json`, §11.5), absent from Homie's
  and the host CLI's surfaces.

## Decision

### D1. Verb grammar: `mc <role> <verb>`, JSON batch on stdin

Role-side verbs are namespaced by role (`mc editor decide`,
`mc strategist propose`, …). Batch payloads arrive as JSON on stdin
(`--batch -`): parse fully, validate fully, then commit in one
`BEGIN IMMEDIATE` transaction — any invalid element aborts the whole batch
(constraint a). Artifacts (evidence, correction files, reports) stay on the
file plane; the payload carries paths, never content.

### D2. Identity and fencing

Every role-side verb derives *tier and role* from `run.json` at its fixed
mount path (never from arguments) and additionally requires the explicit
`--run <run_id>` fencing token, exactly like `mc complete`: a call whose
`run_id` no longer matches the live lease is rejected, never applied. A
role-side verb also **role-matches**: `mc editor decide` from a run whose
`run.json` role is not `editor` is refused, no matter what invokes it.

### D3. Terminal semantics

Each role-side verb below is the run's **terminal action**: it writes the
role's output, advances state where the state machine says so, and releases
the lease in the same transaction (mirroring `mc complete`). It never
dispatches (Inv. 3). A run may issue at most one terminal action; a second
is rejected by the lease fence (the lease is already released).

### D4. The verbs

**`mc editor decide --run <id> --batch -`** (role: editor)
Payload: `{"verdicts": [{"task": <id>, "decision": "promote"|"reject",
"reason": "<prose>"}]}`.
- Must cover **exactly** the proposal ids snapshotted into the run's brief
  at dispatch (recorded on the run row) — no more, no fewer; proposals that
  arrived after the snapshot wait for the next batch.
- `promote` → `proposed → seeded`. `reject` → `decision='rejected'`,
  archived, reason mandatory. No defer, no merge, no rewrite (spec §3).
- Zero-promotion batches are rejected when the ready queue is empty
  (mechanical check: no unarchived, unblocked, dispatchable row exists
  outside the proposed pool) — spec §3's guard, enforced here.

**`mc strategist propose --run <id> --batch -`** (role: strategist, mode
`propose`; subjectless lease per constraint b)
Payload: `{"proposals": [{"worksource": …, "scope": "task"|"initiative",
"title": …, "description": "<checkable success criteria / charter>",
"priority": <int, optional>}]}`.
- Inserts all proposals `status='proposed'`, `origin='agent'`, one
  transaction. No placeholder subject rows anywhere.

**`mc strategist wave --run <id> --initiative <id> --batch -`** (role:
strategist, mode `initiative`)
Payload: `{"children": [{"title": …, "description": …, "priority": …}]}`.
- All children born `seeded`, `scope='task'` (no initiative nesting —
  substrate-enforced), `initiative_id` set, only into a live, still-seeded
  initiative (substrate-enforced). Whole wave or nothing (constraint a;
  spec §10 crash table relies on this).
- The initiative's own status does not move (it stays `seeded`, now parked
  behind open children).

**`mc verifier verdict <task> --run <id> --outcome pass|correct|budget-spent
--evidence <path> [--correction <path>] [--deepening genuine|churn]`**
(role: verifier)
One transaction writes the verdict record (gate-ladder evidence path
included) and applies the §7 outcome table:
- `pass` → `worked → verified`.
- `correct` → requires `--correction` and `correction_count < 3`;
  `worked → seeded`, `correction_count++`, correction-file path recorded.
- `budget-spent` → requires `correction_count = 3`; `worked → verified`
  with the exception label (packet renders it in `risk`/`evidence`).
- `--deepening` is required when the run brief marks the round a
  refinement round-trip; the substrate uses it to increment or reset
  `refine_streak` at re-packaging (spec §8).
Validation failures (wrong count, missing correction) reject the call;
the Verifier's brief carries the budget so this is never a surprise.

**`mc console publish --run <id> --content <path>`** (role: strategist,
mode `console`; subjectless lease)
One transaction records the same-day `daily.briefing` event and writes the
console's outbox rows (`kind='console'`, destinations from alert-class
routing, §16.3). Final payload shape is pinned when §14 is implemented in
Phase 2; the verb name, scope, and atomicity are decided here.

**Not new verbs, by decision:**
- The **initiative done-declaration** (`seeded → worked` with the
  completion report) is `mc complete <initiative> --run <id> --status
  worked --outputs <report>` — it is a plain subject-status advance and
  fits `mc complete`'s contract; the strict-drain trigger (spec §6.1)
  guards it in the substrate, not in a bespoke verb. Deviates least.
- **Worker/Packager terminals** remain `mc complete` (their outputs are a
  single subject advance plus artifacts). The Packager's completion births
  the review packet in the same transaction (Inv. 11 — packet born only
  from `packaged`; the WIP-cap trigger fires here).
- **Blocking on a decision point** remains `mc complete … --needs-operator
  --reason` (spec §18); `mc task block` also stays available to a pipeline
  run for its own subject (it is absent from §18's deny list), fenced to
  the run's subject.

### D5. Runner lifecycle verbs (named here for the table's completeness)

The pipeline runner's private lifecycle scope (§11.5) gets exactly two
verbs: `mc heartbeat <run_id>` (spec §18) and **`mc run register-session
<run_id> --native-ref <ref> --file <name>`** (the "register the harness's
native session handle" traffic, §11.5/§15.4). The Homie runner's transport
scope gets **`mc homie claim <session>`** (atomically claim the next
pending inbound turn; idempotent, fenced) and **`mc homie reply <session>
--turn <id> …`** (append reply + outbox rows in one transaction, §15.5).

### D6. The verb-by-scope table

Scopes (derived from `run.json` presence/tier, §11.5/§18): **host** (no
`run.json`: operator's harness, resident, dashboard, install script),
**pipeline-role** (tier=pipeline; role-matched per D2),
**pipeline-runner** (private lifecycle scope), **homie-agent** (tier=homie,
§15.3 allowlist, frozen per session at start), **homie-runner** (private
transport scope).

| Verb | host | pipeline-role | pipeline-runner | homie-agent | homie-runner |
|---|---|---|---|---|---|
| `mc onboard` / `mc reset` | ✓ | — | — | — (host-effect) | — |
| `mc doctor` / `mc backup` | ✓ | — | — | — | — |
| `mc dispatch` | ✓ (resident tick only) | — | — | — | — |
| `mc <record> get/list` | ✓ | ✓ (scope-filtered reads, Inv. 22) | — | ✓ (board-wide) | — |
| `mc task add` / `mc initiative add` | ✓ | — (deny rule 1) | — | ✓ (`origin:user`) | — |
| `mc task block` | ✓ | ✓ (own subject only) | — | ✓ | — |
| `mc task unblock` | ✓ | — (deny rule 1) | — | ✓ | — |
| `mc task interrupt` | ✓ | — | — | ✓ | — |
| `mc packet decide` | ✓ | — (deny rule 1) | — | ✓ | — |
| `mc worksource add/list/pause/archive` | ✓ | list only | — | ✓ | — |
| `mc complete` | — | ✓ (own run, fenced) | — | — | — |
| `mc editor decide` | — | ✓ (editor) | — | — | — |
| `mc strategist propose` | — | ✓ (strategist/propose) | — | — | — |
| `mc strategist wave` | — | ✓ (strategist/initiative) | — | — | — |
| `mc verifier verdict` | — | ✓ (verifier) | — | — | — |
| `mc console publish` | — | ✓ (strategist/console) | — | — | — |
| `mc heartbeat` | — | — | ✓ (own run) | — | — |
| `mc run register-session` | — | — | ✓ (own run) | — | ✓ (own session) |
| `mc homie start/bind/send/list/history/resume/end` | ✓ | — | — | session-state subset (list/history/end own session) | — |
| `mc homie claim` / `mc homie reply` | — | — | — | — | ✓ (own session, fenced) |
| `mc outbox poll/ack` | ✓ (surface delivery loops) | — | — | — | — |
| `mc land report` (resident reports landing result, §7) | ✓ (resident) | — | — | — | — |

Everything not denied defaults open per §18 (reads are context, not risk).
The three §18 deny rules are visible in the table: operator verbs denied to
pipeline (rule 1), identity fencing on every write (rule 2, the `--run` /
session tokens), outbound-surface writes confined to transport/host scopes
(rule 3).

## Consequences

- Phase 2's CLI test tier drives every verb above (happy path + declared
  error paths) through the real `mc` against a temp spine; the scope table
  becomes a test matrix (per-container refusals are Phase 3's
  scope-conformance tests).
- Batch atomicity gives the §10 crash table its guarantees for free: a
  reaped Editor/Strategist run left either the whole batch or nothing.
- `run.json`-derived role matching means adding a role someday = new verbs
  + table row, no scope-mechanism change.

## Open questions (tracked, non-blocking)

1. **Wave plan-review scheduling.** The spec requires the Editor's holistic
   plan review of every wave (§3, §6.1, Inv. 12) but defines no dispatch
   stage or state for it: children are born `seeded`, which query (3)
   dispatches to Workers immediately. Candidate readings: (i) a review gate
   between wave birth and first child dispatch (needs a visibility rule),
   (ii) plan review folded into the next Editor batch pass, (iii) review
   before birth inside the wave transaction (violates producer≠judge
   session separation). Routed to spike S6's ambiguity list; the wave verb
   above is compatible with all three.
2. **`refine_streak` increment point** — the exact trigger placement
   (verdict write vs re-packaging) is Phase 2 substrate design; the
   `--deepening` flag here is the input it needs either way.
3. **`mc console publish` payload** — pinned when §14 lands in Phase 2.
