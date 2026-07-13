# ADR-009: Homie registry verb contract

- Status: accepted
- Date: 2026-07-12
- Scope: spec §11.5, §15.3–§15.5, §18; ADR-001 D6

## Context

The spec names `mc homie start|bind|list` but does not pin their flag grammar,
JSON result shape, duplicate-binding behavior, or whether a Homie registry
record is mirrored into `runs`. These choices cross provenance, resume, and
surface-routing boundaries and therefore cannot be left to callers.

## Decision

### Command grammar and scopes

- `mc homie start --from <surface:channel_ref>
  [--allow <verb,verb|none>]` is host-only. Omitted `--allow` freezes the
  maximum Homie-agent surface from ADR-001 D6; `none` creates a read-only
  session; an explicit list must be a subset of that maximum.
- `mc homie bind <session> --from <surface:channel_ref>` is host-only.
- `mc homie list` is host-all. An active Homie whose frozen allowlist contains
  `homie.list` sees only its own canonical session. Pipeline identities are
  refused.

`surface` is exactly `discord|dashboard|cli`; `channel_ref` is non-empty and
may itself contain `:`. Start and bind never accept caller-supplied ids,
routes, paths, status, timestamps, locators, launch mode, mounts, or effects.

### Registry birth

Start resolves the authoritative `routing.md` Homie row before mutation,
allocates an `h-<random>` id (disjoint from pipeline run ids), and derives
`sessions/<id>` plus `mc-homie-<id>`. One `BEGIN IMMEDIATE` transaction writes
the active `homie_sessions` row and initial binding. The exact historical
`harness/binding`, path, container name, and canonical JSON allowlist are
non-null and frozen. Native handle and trace filename remain null until the
runner registers the pair.

There is no mirrored `runs` row: `homie_sessions` is the spec's Homie liveness
and resume authority. Start returns durable registry data only. It never
touches the global lease, creates files, writes outbox, or returns a spawn
effect; the later inbound commit makes the session eligible and the resident
tick owns launch (Inv. 2–3).

### Binding ownership and retries

At most one active session owns a concrete `(surface, channel_ref)` globally.
A same-session bind retry is an idempotent success with `bound:false`; another
session's claim is refused without transfer. Starting from an already-owned
place is refused rather than silently selecting or replacing that session.
Ended/reaped sessions cannot bind; resume changes status and appends a fresh
binding event. Binding history is immutable and never deleted.

### Canonical Homie-agent fence

Every Homie-authorized operator mutation performs its authorization inside
the same write transaction as the mutation: the session id must exist, its
status must be active, the envelope allowlist must equal the persisted frozen
set, and that set must contain the verb. This prevents an ended zombie or a
forged/stale envelope from continuing to write. Structural host-only verbs
remain denied even if an envelope claims them.

List returns deterministic session rows ordered by `(created_at,id)`, with a
structured verb array and nested active bindings. Host list retains
active/ended/reaped rows; history remains in the spine even when inactive.

## Consequences

- Surface routing is unambiguous and a bind retry cannot create duplicate
  history.
- Routing changes affect new sessions only; resume always uses the captured
  historical runtime.
- The resident/Homie-runner launch and transport capability remain separate
  later slices. Pipeline `spawn` is not reusable because it claims the lease
  and materializes a pipeline-tier envelope.
- The Phase-3 kernel-backed scope gate is still required; these Phase-2
  checks are the domain/provenance layer, not a containment substitute.
