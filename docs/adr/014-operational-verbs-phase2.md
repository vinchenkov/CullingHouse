# ADR-014 — Operational verbs at the Phase-2 tier: doctor, backup, reset

- Status: Accepted
- Date: 2026-07-13
- Where the spec delegates: §18 names the three verbs and §16.4 their
  posture (snapshot-first destructive reset, VACUUM INTO backups, loss
  detection that never re-initializes), and §17 requires every doctor
  failure to name its repairing onboarding section — but none of them pin
  result shapes, exit semantics, snapshot naming, or what "validate" means
  before the Docker-dependent phases exist. Wave-2 contract §1.5 scopes
  Phase 2 to CLI validation and temp-file/state semantics.

## Decision 1 — all three are host scope, refusing before the spine opens

run.json callers (pipeline or Homie, any allowlist) are refused before any
file is opened or created, per the wave-2 provenance rule. None of the
three may materialize spine bytes at a missing MC_SPINE path: `backup` and
`reset` refuse on a missing file; `doctor` reports it as a failing finding
with the §16.4 "restore from backup" language.

## Decision 2 — `mc backup`: VACUUM INTO temp, rename, no retention

`MC_HOME/backups/spine-<UTC>Z.db` (second-resolution stamp; same-second
collisions take a `-N` suffix), written as `<final>.tmp-<pid>` and renamed
on completion, exactly the §16.4 shape. The snapshot is taken with `VACUUM
INTO` on the ordinary open handle — consistent, non-blocking, and a fresh
copy rather than the live locked file (Inv. 24's spirit at the dev file
tier). Retention/pruning belongs to the resident's tick chore (§16.4) and
is deliberately not in the verb. Result: `{snapshot, bytes}`.

## Decision 3 — `mc reset`: confirmation flag, snapshot-first, file-tier

Without `--confirm` the verb refuses (domain error naming exactly what it
would do) and takes no snapshot — an unconfirmed reset must be a pure
no-op. With it, the snapshot must complete before anything is deleted; a
failed snapshot aborts with the spine untouched. Destruction at this tier
deletes the spine file plus `-wal`/`-shm` siblings — the dev-tier stand-in
for §16.4 volume teardown, which lands with the Phase 3 container story.
Output carries paths and the `reset` flag only; config and secret values
never appear in output or logs (handoff §4.1).

## Decision 4 — `mc doctor`: total finding surface, always exits 0

Findings are `{check, status: ok|fail|deferred, detail, onboard_section}`.
Phase 2 validates what exists without Docker: MC_HOME shape (`home`),
spine identity — meta row, deployment UUID, schema version, with missing
meta reported as spine loss, never repaired (`home`) — Worksource/sandbox
profile references (`worksource`), and routing.md against the active
registry (`routing`). The Phase 3/5 probes (container runtime capability,
gateway health, per-binding runtime auth, supervision) appear as
`deferred` findings with their repairing sections from day one, so the
finding list's shape is total and a consumer never wonders whether a check
was skipped silently. Doctor always exits 0 with `ok` carrying the
verdict: a failing deployment still yields the complete findings list
rather than one structured-error envelope, which is the §13/§17 use — the
error envelope would swallow every finding after the first.

## Consequences

- The resident tick chore and the future `mc onboard verify` section call
  the same testable functions; no host-side shell logic accrues.
- The §16.4 loss-detection fail-closed rule for *other* verbs (refusing on
  a fresh volume where the MC_HOME UUID expects a spine) needs the volume
  story and lands with Phase 3; doctor's spine finding carries the language
  already.
- `mc onboard`'s section dispatcher remains the §17 wizard slice; doctor's
  section names are already aligned to it.
