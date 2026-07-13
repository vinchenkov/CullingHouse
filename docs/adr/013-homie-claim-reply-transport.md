# ADR-013 — Homie runner claim/reply transport

- Status: Accepted
- Date: 2026-07-13
- Where the spec delegates: §11.5 names the runner-scoped capability — "poll
  for the next pending inbound turn belonging to its conversation, atomically
  claim it … append the completed reply through `mc` against the claimed
  turn, complete the claim" with claims and reply submission "fenced and
  idempotent" — and §15.5 requires the reply row plus its outbox fan-out in
  one write, but neither pins the verb grammar, the claim-state law, or how
  a reply is durably tied to the turn it answers. ADR-010 deferred exactly
  this slice.

## Decision 1 — `mc homie claim <session>`: durable, set-once, resumable

Runner scope: run.json tier `homie` whose `run_id` names this exact session
— the same private-lifecycle posture as ADR-012's register-session arm. The
frozen `verb_allowlist` is never consulted: it governs the model's operator
verbs, and chat transport must never be reachable by widening it. Host and
pipeline identities are refused outright; the within-container runner-vs-
model grain stays the accepted §11.5 best-effort residue until Phase 3.

One transaction: the session must be `active` (a zombie runner's transport
on an ended/reaped session is inert; resume is an explicit host transition,
never a traffic side effect), then the lowest-id pending inbound row
(`direction='inbound' AND completed_at IS NULL` — the existing partial
index) is returned. If unclaimed, `claimed_by`/`claimed_at` stamp now
(claimed_by = the session id, the only durable runner identity that exists —
the container is per-session); if already claimed, the same turn returns
with `reclaimed: true` and its original stamp — that is how a fresh runner
resumes a dead one's turn, so claim state is **set-once**, enforced by
substrate trigger. Nothing pending is an ordinary empty result, not an
error. Claiming is bookkeeping: it never advances `last_activity_at`.

## Decision 2 — `mc homie reply <session> --to <id>`: one logical reply

Same runner fence. Body or normalized relative attachments required. One
transaction, all-or-nothing:

1. The `--to` row must be this conversation's inbound turn and be claimed
   ("claim before reply" is the protocol, and the claim is durable, so every
   legal retry path already has it).
2. The reply row appends with `direction='reply'`, `surface='homie'`,
   NULL channel_ref, the next seq — and **`reply_to = <id>`**, a new
   additive column. This is the durable linkage the idempotency law needs:
   without it, "runner death cannot create two logical replies to one
   inbound turn" cannot be checked after a crash. Storage enforces the law
   directly: a partial unique index (one reply per turn), a CHECK that
   replies always carry `reply_to` and inbound rows never do, and a trigger
   requiring the referenced row to be an inbound turn of the same session.
3. The inbound turn completes (`completed_at`, set-once by trigger, and a
   CHECK requires completion to imply a claim), `last_activity_at`
   advances, and one `homie_reply` outbox row fans to **every** active
   binding — origin included, unlike the inbound echo's every-other rule —
   because §15.5 says a reply reads identically on every bound surface.
   The payload mirrors the echo shape: message id, seq, body, structured
   attachment references, plus `reply_to`.

Replay grammar (register-session's): re-submitting the identical logical
reply (same body, same canonical attachment JSON) against a completed turn
returns the existing row with `replied: false` and writes nothing — the
crash-after-commit retry; its outbox rows committed with the original
transaction. Any different payload is refused: the turn has its one logical
reply. Claim state on reply rows themselves is impossible (CHECK).

## Consequences

- Runner death at any point is safe by construction: before claim → the turn
  is still pending; after claim → the fresh runner reclaims the same turn;
  after commit → the retry is a no-op. No in-memory queue exists.
- The one-reply-per-turn law lives in the substrate, not just verb code, so
  a confused or injected model invoking its own conversation's transport is
  confined exactly as §11.5 promises.
- `homie history` continues to render replies from the same rows; `reply_to`
  is available to any surface that later wants threading, at zero cost now.
- The resident's adaptive outbox polling (§15.5) needs no new machinery:
  replies land as ordinary `homie_reply` outbox rows behind the existing
  `mc outbox poll|ack` cursor.
