# ADR-011: surface-scoped outbox delivery cursor

- Status: accepted
- Date: 2026-07-12
- Scope: spec §15.5–§15.7, §18

## Decision

The native delivery surface uses two host-only verbs:

- `mc outbox poll --surface <discord|dashboard|cli> [--limit <1..1000>]`
- `mc outbox ack <id> --surface <discord|dashboard|cli>`

Poll is a pure read of undelivered rows for exactly one surface, ordered by
ascending outbox id. It returns the small payload as a structured JSON object
and defaults to 100 rows. Poll does not claim, hide, timestamp, or otherwise
mutate a row: an unacknowledged delivery remains visible after any native
process restart, preserving at-least-once semantics.

Ack runs under `BEGIN IMMEDIATE`, verifies the row exists and belongs to the
named surface, and stamps `delivered_at` with the spine clock only when it is
null. A same-surface replay is an idempotent success with `acked:false` and
the original timestamp. A different surface can neither inspect through
poll nor acknowledge the row.

The substrate restricts destination surfaces to the three durable binding
vocabulary values and CHECKs every payload as a JSON object. Dashboard and
Discord have native delivery loops; `cli` is retained as the registry's
declared surface and can back the later `history --follow` client without a
second delivery table.

## Consequences

- Insert/delivery is the Phase-2 split-brain convergence shape: a crash after
  insert or after physical delivery but before ack simply re-polls the row.
- De-duplication at the external API boundary may use the durable outbox id;
  acknowledgement never removes the conversation/event record.
- Homie agents and pipeline containers cannot poll or ack, so outbound rows
  are not an agent-controlled egress channel (§18 deny rule 3).
