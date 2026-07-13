# Phase 2 wave-2 contract — command surface, convergence, properties

Behavioral authority: `specs/mission-control-spec.md`; acceptance authority:
`specs/implementation-handoff.md` Phase 2. The spec wins. Wave 1 is accepted
except the initiative-wave terminal parked in `PROGRESS.md`; this contract
does not route around that decision.

## 1. Work order

1. **Provenance first.** Make the existing command surface obey ADR-001 D6
   before adding more verbs. A pipeline identity cannot invoke `init`,
   `dispatch`, `task add`, `packet decide`, `task unblock`, or `land report`;
   host cannot invoke role/runner terminals. Every refusal precedes spine
   mutation. Reads remain open, subject to Worksource filtering in Phase 3.
2. **Structured errors.** Domain rejection writes one stdout JSON object
   `{"error":{"code":"<stable-code>","message":"<text>"}}`, still writes
   the diagnostic to stderr, and exits 1. Usage/environment errors use code
   `usage` and exit 2. Self-delegation preserves bytes and exit status.
3. **Operator/core records.** Add happy and declared-error paths for
   `initiative add`, `task interrupt`, and
   `worksource add|list|pause|archive`. These are ordinary one-transaction
   record writes; interrupt clears the matching live lease and returns a
   stop-container effect, but never dispatches.
4. **Console + communication records.** Add `console publish`, Homie
   session/binding/message verbs, and outbox poll/ack over the Phase-1 tables.
   Claims/replies are own-session fenced and idempotent. Reply + destination
   outbox rows are one transaction. The parked initiative-wave line remains
   untouched.
5. **Operational verbs.** Add `doctor`, `backup`, `reset`, and the `onboard`
   section dispatcher as testable host-scope functions. Phase 2 proves CLI
   validation and temp-file/state semantics; real auth/container/supervision
   probes remain Phase 3/5. Reset is confirmation-gated and snapshots first.
6. **Split-brain convergence**, then **nightly properties**. No randomized
   suite begins until the deterministic surface and kill points are green.

Commit at every green numbered slice; the complete fast lane gates each.

## 2. Scope matrix acceptance

The executable matrix is ADR-001 D6. At minimum, test every cell that differs
from its row neighbor; do not duplicate identical read cells.

| Scope | Identity source | May write |
|---|---|---|
| host | no run.json | host/operator/resident/surface verbs only |
| pipeline-role | tier pipeline + role | own fenced role terminal; own-subject block |
| pipeline-runner | tier pipeline + runner capability | own heartbeat/register only |
| homie-agent | tier homie + frozen `verb_allowlist` | listed spine-only operator subset |
| homie-runner | tier homie + transport capability | own claim/reply/register only |

No command trusts a caller-supplied scope. Test each denial with a state
snapshot proving zero task/lease/outbox/session change.

## 3. Command acceptance table

| Family | Required happy/error facts |
|---|---|
| existing get/list | JSON shape stable; missing record coded; pipeline read filtering seam retained |
| `initiative add` | `scope=initiative`, `origin=user`, charter required, priority including -1; pipeline denied |
| `worksource` | add validates id/title/kind/profile reference; pause/archive make work invisible without deleting history; list read-only |
| `task interrupt` | reason `operator_interrupt`; cancels subject, clears only its matching lease, returns exact stop effect; stale/non-live idempotent refusal |
| `console publish` | exact strategist(console), subjectless own run; content path required; same-day `daily.briefing` + configured destination outbox rows + release atomically |
| Homie state | start/bind/send/list/history/resume/end persist; ended sessions remain listable/resumable; bindings unique; message sequence stable |
| Homie transport | claim one pending inbound idempotently; reply own claimed turn once; append reply + fan-out outbox atomically |
| outbox | poll returns undelivered rows for one surface in stable order; ack only that surface's row; replay ack idempotent |
| doctor | every finding has status and repairing onboard section; no mutation |
| backup | SQLite-consistent snapshot under requested temp `MC_HOME`; source remains usable |
| reset | confirmation required; backup completes before destructive re-init; live secrets never enter output/log |
| onboard | named sections only, resumable/idempotent; Phase-2 doubles for host effects; no launchd load |

## 4. Split-brain suite

Use the injectable resident/runner seams, never fault hooks in `mc`. For each
row: commit the left-side decision, fail at the boundary, restart the ordinary
loop, and assert convergence without duplicate durable work.

| Kill boundary | Required convergence |
|---|---|
| action selected / before effect | lease watchdog reaps; same subject reselected |
| session folder / before run.json | reap; folder remains trace-safe; retry gets new run |
| run.json / before container | reap; envelope removed with reap |
| container start / before first heartbeat | spawn watchdog; retry budget charged once |
| workspace bytes / before git commit | status unchanged; partial work inert in worktree |
| git commit / before complete | status unchanged; retry sees committed branch state, no duplicate merge |
| operator approve / before land | landing-pending deterministically re-emits |
| merge success / cleanup/report gap | no second merge; cleanup debt visible; success truth preserved |
| message/outbox insert / delivery | append + outbox atomic; delivery at least once, ack deduplicates |

The suite is parameterized by boundary and asserts the record/lock/effect
triple after restart.

## 5. Nightly properties

New package `mc/property` behind `//go:build nightly`:

- dispatch state fuzzer: zero-or-one action, deterministic/pure;
- metamorphic invisibility: adding an ineligible row cannot change a winner;
- lifecycle random walk: every domain operation compared with substrate
  acceptance and invariants after every step;
- generator honesty: coverage-distribution floors for status/scope/decision/
  packet/lease combinations;
- planted mutants: named changes (drop blocked filter, ignore packet archive,
  blur budgets, weaken lease token, exceed WIP cap) must each be killed.

Nightly runtime is non-gating; generator-honesty and the planted-mutant list
run in the fast lane against a bounded seed set.

## 6. Definition of done

Phase 2 advances only when:

- every unparked ADR-001 D6 cell and §18 verb has happy + declared error paths
  through the real CLI;
- all deterministic split-brain rows converge;
- the nightly suites compile/run and honesty/mutant gates pass;
- Go, all four Bun suites, Docker-tag compile, and fake-routing-tag compile
  are green;
- the parked initiative-wave decision is still isolated and explicitly
  excluded, never silently treated as accepted.
