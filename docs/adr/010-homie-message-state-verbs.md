# ADR-010: Homie inbound, history, and end verbs

- Status: accepted
- Date: 2026-07-12
- Scope: spec §15.4–§15.5, §18; ADR-001 D5–D6

## Context

The spec names `mc homie send|history|end` and their ownership but leaves the
body/attachment grammar, echo payload, first-traffic binding transaction,
and end-retry record unspecified. These details determine whether transcript,
surface rendering, and registry liveness can diverge after a crash.

## Decision

### Inbound send

`mc homie send <session> --from <surface:channel_ref> [--body <text>]
[--attachments <json-array>]` is host/native-surface only. At least one of
body or attachments is required. Attachment values are normalized relative
file-plane references; bytes never enter the spine.

One `BEGIN IMMEDIATE` transaction:

1. requires an active session (ended/reaped traffic uses explicit resume in
   the following slice);
2. creates the origin binding on first traffic, or accepts the existing
   same-session binding; another session's active ownership is refused;
3. allocates `seq = max(session.seq)+1` and appends the inbound conversation
   row;
4. advances `last_activity_at` with the spine clock; and
5. writes one `homie_echo` outbox row for every other active binding.

The echo payload is an inline JSON object carrying message id, sequence,
body, structured attachment references, and the origin surface/place. The
origin gets no echo. Any outbox failure rolls back the message, new binding,
and activity timestamp together.

### History

`mc homie history <session>` is a deterministic snapshot ordered by
`(seq,id)`, with attachment JSON rendered as an array. Host may read any
retained session. An allowlisted active Homie may read only its own session,
fenced against the canonical registry and frozen allowlist. Ended/reaped
history remains host-readable forever.

The spec's `history --follow` streaming client is not represented as a false
single-object response in this slice; it remains an explicit Phase-2 client
loop to add alongside the transport/outbox polling surface.

### End

`mc homie end <session> --reason <text>` is host or an allowlisted active
Homie acting on itself. The transaction changes `active → ended` and appends
a `homie.ended` activity record containing the reason; the substrate trigger
deactivates all current bindings in that same transaction. A host retry on an
already ended/reaped row is a no-op and appends no duplicate event. The verb
returns no stop effect and never touches the pipeline lease: registry truth
commits first, then the resident's ordinary Homie orphan sweep owns the
container stop (§11.6).

## Consequences

- Conversation order is allocated under the same serialized writer boundary
  as the append, with no in-memory cursor.
- A surface restart can reconstruct transcript and pending echoes entirely
  from the spine.
- Native inbound transport cannot be invoked from a Homie model, even with an
  overbroad claimed allowlist.
- Implicit send-to-ended resume, runner claims/replies, resume launch, and
  `history --follow` remain separate, named Phase-2 slices rather than being
  approximated here.
