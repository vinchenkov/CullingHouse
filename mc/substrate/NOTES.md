# Substrate notes — Phase 1a (spine schema + trigger lattice + backstop tests)

Scope: `mc/substrate/` — the redundant backstop layer of spec §4. Real
enforcement arrives in the Phase 2 domain layer; every rule here holds
standalone against raw SQL. Spec wins on conflict; S6's `NOTE(S6.n)`
interpretations are honored where they touch the substrate (S6.2/S6.3 blocked
filters and derived landing-pending, S6.8 reap-budget blocking — the schema
carries the fields those semantics need and forbids nothing they require).

## Decisions

Each entry is the conservative option per the handoff definition: preserves
the invariants + fail-closed posture, deviates least from the spec's text,
and is easiest to reverse.

- **NOTE(P1.1) — self-assignment is a no-op, not a transition.** The §6
  matrix governs *transitions*; `UPDATE … SET status = status` changes no
  state, so the trigger fires only `WHEN OLD.status <> NEW.status`. Aborting
  no-ops would add machinery for a statement `mc` never issues.
- **NOTE(P1.2) — birth statuses are enforced, and rows are born undecided and
  unarchived.** §6 "proposed — born here" and §6.1 "children are born seeded"
  are read as birth rules and trigger-enforced (`tasks_birth_rules`).
  Fail-closed extension: `INSERT` with a `decision` or `archived = 1` aborts —
  every terminal mark must be earned through the ledgered flow (reject /
  cancel / approve on an existing row), so a fabricated already-decided row
  cannot enter the leverage ledger. Reversal is deleting one CASE arm.
- **NOTE(P1.3) — the WIP cap value 3 is baked into the trigger.**
  `REVIEW_WIP_CAP` is tunable policy (§16.3) but a trigger body cannot read
  config; the shipped default is compiled into `packets_wip_cap`. Retuning the
  knob is a schema migration (`DROP TRIGGER` + recreate at onboarding), which
  Phase 2's config layer owns. Fail-closed extension: un-archiving a packet
  back over the cap aborts too (`packets_wip_cap_on_unarchive`) — the cap
  cannot be cheated from either direction.
- **NOTE(P1.4) — archived rows are terminal in every direction** (revised by
  the takeover review; the original entry allowed un-archiving on a claim
  that turned out false against the live schema). Four guards now carry it:
  no status transition on an archived row (`tasks_archived_are_terminal`);
  no status transition on any decided row (`tasks_decided_never_move` — §6:
  rejected/cancelled rows leave the pool only through archive, approved rows
  hold at `packaged` through landing, and the §10 with-room query has no
  decision filter, so a decided row that moved would re-enter dispatch); no
  un-archiving at all (`tasks_no_unarchive` — fail-closed: no spec flow
  un-archives a task, and resurrection of a landed row would re-derive
  landing-pending (NOTE(P1.9)/NOTE(S6.3)) and emit a land effect for an
  already-merged, already-deleted branch); and no rewriting an archived
  row's `decision`/`decided_at` (`tasks_archived_decision_frozen` — the
  rejected record *is* the leverage ledger, §5, same provenance class as
  NOTE(P1.5)). If a Homie manual-correction surface ever genuinely needs
  resurrection, dropping `tasks_no_unarchive` is one reversible trigger —
  behind an explicit ADR, not a default.
- **NOTE(P1.5) — identity columns are immutable.** `scope`, `initiative_id`,
  `origin`, `worksource`, `created_at`, `id` never change after birth
  (`tasks_identity_immutable`). Without this, re-parenting or re-scoping would
  dodge the birth rules, strict drain, and the cascade; `origin` mutation
  would counterfeit operator provenance (§18 threat model). No spec flow
  mutates any of them.
- **NOTE(P1.6) — archiving any task archives its packet.** §4's cascade rule
  names only the initiative case ("their packets with them"), but every path
  that archives a packeted task (approve+land, cancel) must free the slot in
  the same transaction (§7, Inv. 11, Inv. 18). `tasks_archive_cascades_packet`
  makes that a substrate guarantee instead of an `mc` discipline. A packet can
  still be archived while its task lives on — that is the spec's own
  refinement shape, unconstrained.
- **NOTE(P1.7) — the propagated block reason can point at an already-resolved
  child.** With children #1 and #2 both blocked, the initiative's reason is
  `blocked child #1`; unblocking #1 alone keeps the initiative blocked (per
  §6.1: it clears only when the *last* blocked child resolves) but the reason
  string still names #1. The spec fixes the clear condition, not the reason's
  freshness; auto-clear keys on the `blocked child #%` prefix, so staleness
  never mis-clears. Repointing the reason is a cheap later addition if wanted.
- **NOTE(P1.8) — `saturated` is trigger-computed at `refine_streak >= 3` and
  divergent hand-sets abort in both directions.** The spec names a trigger
  (`packets_saturate`), not a generated column, so a trigger it is; guard
  triggers make hand-setting (or hand-clearing a genuinely saturated packet)
  abort, which is what "computed, never hand-set" (§8) means at the storage
  layer. There is no auto-desaturation: the spec never desaturates — a
  saturated packet waits only on the operator. The takeover review found a
  two-step side door (lower the streak first, then clear the flag), so a
  saturated packet's `refine_streak` is also frozen against decreases
  (`packets_saturated_streak_frozen`): refinement never dispatches on
  `saturated = 1` (§10 step 2b), so no genuine-deepening reset can
  legitimately occur there. If a Phase 2 flow ever needs an operator-driven
  desaturation (e.g. after a revise round-trip), that is a deliberate,
  ADR-worthy trigger change.
- **NOTE(P1.9) — landing-pending is derived state, not a column.** Following
  S6 (`dispatch.go` `LandingPending()`): approved + branch-carrying +
  unarchived (+ not blocked, NOTE(S6.3)) *is* the durable landing-pending
  mark of §7. The schema carries `branch`, `verified_sha`, `target_ref` on
  `tasks`; no boolean to drift out of sync with the facts that define it.
- **NOTE(P1.10) — no-delete backstops on `tasks`, `review_packets`, `runs`,
  `homie_sessions`, `conversation_messages`, `lock`, `activity`.** The spec's
  language is keep-forever for all of these (leverage ledger §5, packet slot
  Inv. 11, traces Inv. 26, sessions/conversations §15.4, activity Inv. 7, the
  lock singleton §10). `outbox` rows stay deletable — they are delivery
  bookkeeping only (§15.5) and pruning delivered rows is plausibly legitimate.
- **NOTE(P1.11) — `stage_entered_at` is stamped by trigger on every status
  transition**, with the lock domain's own clock (`datetime('now')`, §10
  clock discipline), so the field can never lie about when a stage began.
- **NOTE(P1.12) — the lattice never relies on nested trigger firing.** Every
  cascade writes all of its downstream effects directly in its own body
  (e.g. `initiatives_archive_cascade` archives the children's packets itself
  rather than counting on `tasks_archive_cascades_packet` to fire from inside
  a trigger). The schema is therefore correct under any `recursive_triggers`
  setting — verified against both the modernc driver and the sqlite3 CLI
  default (off).
- **NOTE(P1.13) — conversation rows: content immutable, claim state mutable.**
  §15.4 calls the rows append-only while §11.5/§15.5 require durable claim and
  delivery bookkeeping on the same records. Split by column: identity/content
  columns abort on UPDATE; `claimed_by`/`claimed_at`/`completed_at` may
  advance. DELETE always aborts. Attachment carriers are JSON arrays, and
  outbox payloads are JSON objects, CHECK-pinned so later transport writers
  cannot place malformed durable work on either bus.
- **NOTE(P1.14) — defaults where the spec names none:** `priority` defaults
  to 2 (mid-band of P0–P3; expedite is always explicit), `dispatch_retries`
  to 3 (§16.3's shipped budget), lease timing columns to the §16.3 table.
  All are writer-overridable; none affects any invariant.
- **NOTE(P1.15) — `meta` is created empty.** §16.4: first-ever *provisioning*
  writes the deployment UUID / schema version row; the schema only guarantees
  the singleton shape (`id = 1`).
- **NOTE(P1.16) — `rejected` is not status-constrained.** §6 marks only
  *approve* as substrate-enforced ("`approved` only from `packaged`
  (substrate-enforced)"); rejected-at-proposed stays a domain rule, per the
  spec's own assignment.
- **NOTE(P1.17) — go.mod says `go 1.25.0` while mise pins Go 1.24.5.**
  modernc.org/sqlite v1.53.0 (the S1/S5-validated pin, which wins) itself
  requires `go >= 1.25.0`; the pinned 1.24.5 toolchain resolves this via Go's
  own `GOTOOLCHAIN=auto` switch (it fetched go1.25.12), exactly as spike S5's
  module already did. Revised by the takeover review: the auto-selected
  toolchain is now an explicit tracked pin — `toolchain go1.25.12` in
  `mc/go.mod` — so the compiler that actually builds `mc` is declared, not
  floating (§16.1). Bumping the repo-root mise pin to 1.25.x instead is the
  cleaner endgame but touches a file outside `mc/`; parked for the
  ledger-holding session.
- **NOTE(P1.18) — the lock singleton carries claim-consistency CHECKs.**
  `run_id`/`owner`/`acquired_at`/`hard_deadline_at` travel together and
  `subject`/`last_heartbeat_at`/`worksource` exist only under a claim, so a
  "half-free" lock is unstorable (the takeover review found `worksource`
  missing from the set — a free lock could retain worksource residue; it now
  also carries a FK to `worksources`, matching `runs.worksource`). `subject`
  stays nullable under a claim (§10: propose-style runs hold the lease with
  no subject).
- **NOTE(P1.19) — `homie_bindings` is bind-event history, one row per bind.**
  §15.4: active bindings clear when a session ends, and a resume "creates a
  fresh binding" for the requesting surface — under the original composite
  PK `(session_id, surface, channel_ref)` that primary resume flow was
  unstorable (re-binding the same Discord channel aborted UNIQUE). Takeover
  fix: surrogate `id` PK; a partial unique index keeps at most one *active*
  session per (surface, place) globally — inbound routing names "the session"
  bound there, so session-scoped uniqueness is ambiguous. A no-delete trigger
  keeps the history ("the row, its bindings history … persist indefinitely",
  §15.4), identity fields cannot be rewritten, inactive events cannot be
  resurrected, and end/reap deactivates current bindings automatically.
- **NOTE(P1.20) — packets are born live.** A born-archived packet would dodge
  `packets_wip_cap` (which fires only `WHEN NEW.archived = 0`) while
  permanently consuming the task's one-packet-for-life slot (Inv. 11); no
  spec flow produces one. `packets_born_live` aborts it — the packet-side
  mirror of NOTE(P1.2)'s nothing-is-born-archived rule.
- **NOTE(P2.4) — Homie start provenance is immutable registry state.** The
  start-known fields (`id`, `created_at`, container name, frozen verb
  allowlist, session path, and exact runtime binding) are non-null and
  immutable; the allowlist is structurally a JSON array. The runner-known
  native handle and trace filename are a paired
  set-once locator, mirroring `runs`. Status and `last_activity_at` remain the
  only ordinary mutable session fields; ended/reaped rows remain resumable.

## Test inventory

`substrate_test.go` + `helpers_test.go`: 155 cases (22 top-level functions,
table-driven subtests), pure SQL against a temp spine **file** (WAL requires
one), no `mc` binary. Coverage: the full 5×5 transition matrix × three
populations (task, initiative, wave child — the child's `proposed` cells pin
unreachability instead), birth matrix × task/initiative/wave-child +
born-decided/born-archived aborts for both plain rows and children,
archived-terminal cases (+ decision-frozen, no-unarchive incl. landed rows,
decided-unarchived-never-moves), correction_count bounds,
blocked-needs-reason + unblock-clears, archive-needs-decision +
decision↔decided_at pairing, approve-only-from-packaged × both scopes +
landing-pending-never-pulled-back, no-initiative-nesting, wave-child birth
rules, strict drain (incl. the decided-but-unarchived child still counting
as open), blocked-child propagation + auto-clear (unblock, cancel,
operator-set never clears, born-blocked), cascade archive (children, their
packets, own arc packet, plain-task packet), WIP cap (birth, unarchive,
one-per-task-for-life), packet-requires-packaged (every status + archived +
missing task + born-archived), saturation (threshold, both hand-set
directions, the two-step streak-lowering side door, birth guard, streak
reset), activity append-only (UPDATE + DELETE), stage_rank generation (all
five ranks + unwritable), identity immutability, no-delete backstops,
homie_bindings history (re-bind after end, one-active-per-place, no delete),
conversation append-only/claim split, WAL + foreign-keys pragmas, lock/meta
singleton shape (incl. worksource residue + FK).

Run: `mc/substrate/check.sh` (= `cd mc && mise exec -- go test ./substrate/`).
