# ADR-015 — Onboarding section dispatcher at the Phase-2 tier

- Status: Accepted
- Date: 2026-07-13
- Where the spec delegates: §17 fixes the ordered section names and
  fail-closed/idempotent posture; §16.4 fixes spine identity and loss
  handling; §18 fixes the host-only command surface. Wave-2 contract §1.5
  requires a testable section dispatcher with Phase-2 doubles for host
  effects, leaving real auth/container/supervision probes to Phase 3/5.

## Decision 1 — grammar, scope, and result shape

`mc onboard [<section>] [flags]` is strictly host scope and refuses a
run.json caller before any deployment effect. With no section it walks
`preflight|home|runtime-auth|routing|container|worksource|tunables|surfaces|
supervision|verify` in that order; a named section runs alone. Success is
`{ok:true, sections:[{section,status,detail}]}`. `done` means this invocation
changed durable state, `ok` means the already-healthy state was skipped, and
`deferred` names an explicitly staged probe. The first rejection stops the
walk while earlier completed sections remain durable, so rerunning resumes.

The CLI accepts dual-input flags for the first Worksource, timing tunables,
and the complete Daily Console `(hour, minute, IANA timezone)` triple.
Partial triples and negative tunables are usage errors. `--smoke` is a
named Phase-4/5 deferral, not a simulated success.

## Decision 2 — preflight and the repository fence

Phase 2 proves OS/architecture and git availability. Container-runtime and
init-system capability effects remain explicit later-phase probes. MC_HOME
is resolved through existing symlinks before checking repository ancestry.
An in-tree root is legal only when `git check-ignore` proves that concrete
path ignored by every containing worktree, matching §16.1; an invalid git
marker or non-ignored path refuses. Preflight writes nothing.
The `home` section re-runs this read-only preflight even when selected by
name, so section re-entry cannot bypass the repository fence.

## Decision 3 — meta-first, atomic file-tier provisioning

The Phase-2 stand-in receives an explicit `MC_SPINE` file; the derived
runtime-local named volume and warm-helper crossing land with the Phase-3
container boundary. That staging does not relax §16.4 identity semantics:

- an existing spine is inspected read-only before MC_HOME is scaffolded or
  SQLite connection pragmas can mutate it;
- a non-empty spine without a current meta identity is refused untouched;
- fresh schema plus meta identity commit in one `BEGIN IMMEDIATE`
  transaction; a failed first provision removes only the fresh empty
  spine/WAL/SHM files it created, so retry does not weaken the rule that any
  pre-existing non-empty spine without meta is loss and must be refused;
- `MC_HOME/deployment.uuid` is atomically written after the DB commit,
  adopted from a valid pre-mirror deployment when absent, and compared on
  every later home run;
- a mirror with a missing/empty spine, or a mirror/meta mismatch, is spine
  loss and refuses with restore-from-backup guidance.

Concurrent fresh-home callers serialize at SQLite. A loser re-inspects a
schema conflict and adopts the winner's committed identity; it never deletes
the winner. A transient WAL/open lock waits only for that identity to become
readable. Cleanup occurs only when the failed transaction leaves no committed
tables, which keeps injected/crashed first provisioning retryable without
weakening the non-empty-spine refusal.

Before any non-preflight/non-home section can write, the same identity pair
must already validate. The Phase-2 home scaffold creates the file-plane
directories needed by implemented code. `config.toml`, the mount allowlist,
the named volume, and helper lifecycle land with their config/container
implementations; routing is already its own validated section.

## Decision 4 — deterministic local sections

Routing validates before its first write and never rewrites an existing
file. Its first publication is a synced temp file installed by an atomic
no-replace link; existing symlinks and non-regular files refuse. The first
Worksource requires an existing directory, records its symlink-resolved
absolute path, and refuses rather than silently reusing a conflicting or
permissive `default` profile. First-Worksource selection and all timing and
surface compare/write decisions run under `BEGIN IMMEDIATE`: concurrent
identical replay yields one `done` and one `ok`, not two stale decisions.
Detail ordering is stable and a no-flag tunables re-entry reports the stored
values rather than claiming defaults. An unconfigured Daily Console requires
the dual-input schedule instead of treating the disabled sentinel as
healthy. Doctor and verification check both the UUID mirror pair and the
configured Console schedule.

Runtime auth, container/gateway/helper probes, and supervision are explicit
`deferred` results in Phase 2. Supervision never generates or loads launchd
state during development; S7 remains the only sanctioned development load.
Verification runs the common doctor surface and fails if a non-deferred
finding remains.

## Decision 5 — explicitly staged §17 machinery

This dispatcher is not the distribution front door yet. The following
remain scheduled work, rather than implied by a `done` Phase-2 section:

- Phase 3 boundary/config work: the derived named spine volume and helper
  crossing; `config.toml`; default/extend-only mount allowlist; configured
  binding catalog and routing-remap path; image build, gateway and helper
  probes; Worksource blocked-pattern/mount validation, git contract, and
  deny-profile review; dashboard/Discord process configuration.
- Phase 4: the dashboard Playwright smoke and the fake full-loop onboarding
  integration used by the six E2E families.
- Phase 5 front door/endgame: real provider auth/no-op probes, interactive
  prompts and answer-file equivalence, directive/seeding/advisory shepherding,
  `install.sh`, the repo `/onboard` skill, `--smoke`, supervision-unit load,
  and the restore drill. Launchd generation may be tested earlier as bytes,
  but loading remains operator-present Phase 5 only.

## Consequences

- The quota-interrupted tests become an executable Phase-2 contract rather
  than silently claiming full §17 installation.
- A crash can leave a committed spine without its mirror, but the next home
  run safely reconstructs the mirror from the valid meta row; the reverse
  order never occurs.
- Later phases replace the named doubles and fill the staged section
  machinery above. Section grammar, identity rules, and idempotency remain
  stable.
