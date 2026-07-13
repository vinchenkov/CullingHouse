# ADR-012 — Homie-runner lifecycle seam and record-only resume

- Status: Accepted
- Date: 2026-07-13
- Where the spec delegates: §15.4 names `mc homie resume <session> --from
  <surface:channel_ref>` and the native-locator resume substrate but pins
  neither the runner's registration seam nor resume's record grammar;
  ADR-001 D5/D6 scope register-session "(own run)" for the pipeline tier
  only; ADR-009/010 deferred runner transport.

## Decision 1 — register-session extends to the exact own Homie session

`mc run register-session <id> --native-ref <ref> --file <name>` gains a
Homie arm: a caller whose run.json is tier `homie` with `run_id == <id>`
writes the native session handle + trace filename onto its own canonical
`homie_sessions` row. Everything else is unchanged; the pipeline arm keeps
writing `runs`.

- Same grammar as the pipeline arm: the pair is set-once — same-value
  retries are idempotent, conflicting replacements are refused in the verb
  and by the existing `homie_sessions_native_locators_immutable` trigger.
- No liveness requirement: registration needs identity, not fencing
  (ADR-001 D5 rationale). A session that ended before its runner registered
  must still record its locators or resumability is lost forever (Inv. 26).
- No allowlist involvement: this is the runner's private lifecycle scope,
  not a model-issued operator verb — the frozen `verb_allowlist` governs
  the model's `mc` capability only. Until Phase 3 separates runner-private
  and model capabilities structurally (the same boundary the pipeline
  runner already rides, deviation D-mc-3), a model self-registering its own
  truthful locator pair is harmless: own row, set-once, immutable.
- Cross-session writes are refused on the own-run identity check exactly
  like the pipeline arm; a pipeline identity naming a Homie session id
  falls through to the runs row and is refused as unknown.

## Decision 2 — `mc homie resume` is a record-only status/binding transition

`mc homie resume <session> --from <surface:channel_ref>`, host scope only
(like start/bind). One transaction:

1. The session must exist and be `ended` or `reaped` (both are resumable —
   the registry comment and §15.4 say ended/reaped are not terminal).
2. **Native continue mode requires the immutable locator pair.** A session
   whose locators were never registered is refused with a message naming
   the §15.4 conversation-rows fallback; that fallback is a deliberate,
   separate, audited arm (explicit flag, future slice) — never an implicit
   downgrade.
3. Status flips back to `active`, `last_activity_at` advances, an
   `activity` row `homie.resumed` records the requesting surface (mirror of
   `homie.ended`), and a fresh binding for the requesting place is created
   through the ordinary binding path — the partial unique index means a
   place owned by another active session aborts the whole transaction and
   the session stays ended. History is never rewritten (the pre-end
   bindings stay inactive; resume binds only the requesting surface).
4. Record-only: no launch effect is returned. Re-mounting the session
   folder and relaunching the recorded harness is the resident's host
   authority (Phase 3+), exactly like start's no-launch posture.

Retry grammar: resuming an already-active session is idempotent iff the
requested place is already actively bound to that same session (the
crash-after-commit retry) and returns `resumed: false`; any other resume of
an active session is refused ("use bind"). Unknown sessions, pipeline or
Homie-agent identities, invalid surface refs, and occupied places are
refused with no partial state.

## Consequences

- The Homie runner's start-of-session ritual is now identical in shape to
  the pipeline runner's: register locators early, idempotently, without
  racing lifecycle state.
- `homie list`'s ended-but-resumable rows plus registered locators are
  sufficient for the resident to effect a real resume later; no second
  bookkeeping surface is invented.
- Runner claim/reply transport remains the next slice (ADR-010 deferral);
  outbound transport stays outside the model's Homie-agent identity.
