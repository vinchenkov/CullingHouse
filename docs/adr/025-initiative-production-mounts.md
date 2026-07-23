# ADR-025 — Production initiative mounts, cut, and arc landing crossing

## Status

Accepted (2026-07-23). This is the follow-on ADR that ADR-017 D6 requires for
its Parked initiative arm ("a follow-on ADR extends this closed table"),
constrained by ADR-023 D1/D3/D5: shared branch `mc/initiative-<id>`, branchless
children, shared worktree `<workspace_root>/.mc-worktrees/initiative-<id>`. It
also discharges ADR-023's deferred "promotion-time resident worktree-cut
effect" (D3 below) and amends ADR-023 D5's production sentence accordingly.
The design was adversarially reviewed through git-mechanics,
security/jurisdiction, and state-machine lenses before acceptance; every
decision below incorporates those findings.

Until the final slice in §Slices lands, real-harness initiatives remain
fail-closed exactly as today (health refusals, no charge, no block); no slice
weakens any existing refusal.

## Context

Under real routing, every initiative-family spawn is refused today: children
by the ADR-017 D6 refusal (`mountattest.go:238-249`), and
Strategist(initiative)/Editor(plan-review)/arc Verifier/arc Packager by the
"no realizable Git mount arm" health refusal (`mountattest.go:279-291`, the
arc row carries no accepted completion seal because it has no
`task_assignments` row — ADR-023 D4). The fake lane works because children
fall through to the legacy whole-Worksource RW bind and create the shared
worktree in the real checkout themselves; production must never mount the
real repository's objects/refs/config/hooks (ADR-017), and the host must
never execute operator-installed Git (ADR-016 D5, `gitregistry.go:8-10`).

The legacy arc landing (ADR-023 D4, `runner/image/mc-land`) discovers a
branch's worktree only via `git worktree list` of the REAL repo and fences on
`refs/heads/<branch>` equalling `verified_sha` exactly. A store-linked shared
worktree is invisible to it, and the real repo only ever receives the
initiative's objects through an explicit import.

## Decision

**D1 — Host layout; pointer bytes are container-relative verbatim.** The
initiative-local sanitized store lives at
`<workspace_root>/.mission-control/initiatives/initiative-<id>/` with exactly
two children: `git/` (bare sanitized repo: exact closure pack of the cut
commit, `HEAD -> refs/heads/mc/initiative-<id>`, that ref at the cut SHA,
sanitized config, empty hooks/info/objects-info/packed-refs/shallow,
`worktrees/mc-initiative-<id>/`) and `source/` (an empty structural directory
that exists only as the in-container mountpoint for `/workspace/source` —
Docker cannot invent a mountpoint under an RO bind). The store root carries
the task-root discipline (operator-owned, mode 0555). The shared worktree is
the checkout at `<workspace_root>/.mc-worktrees/initiative-<id>` (ADR-023 D5's
path), a canary-proved 0700 operator-owned child of `.mc-worktrees`, which is
itself operator-owned 0700 and identity-checked like `.mission-control/tasks`.
Every pointer file (`<worktree>/.git`, store
`worktrees/mc-initiative-<id>/{commondir,gitdir}`) carries the SAME
container-relative bytes the task grammar uses (`gitdir:
../git/worktrees/mc-initiative-<id>`, `../..`, `../../../source/.git`),
written verbatim on the host. These pointers do not resolve on the host; that
is deliberate and safe because no host process may ever run Git over them
(ADR-016 D5). This amends ADR-017:425-429's "identical on the host and in the
container" for initiative rows: the invariant is identical BYTES and
container-side resolution; host-side resolution is unused by construction.

**D2 — Container rows: the same closed destination table, two host bases.**
An initiative child receives exactly the 15 destinations of ADR-017 D6's
table (`taskPlanRows`, `mc/verbs/taskskeleton.go:59-80`) with the worktree
name `mc-initiative-<id>` and initiative-local sources: 14 rows resolve
against the store root; `/workspace/source` alone binds the external worktree
`<workspace_root>/.mc-worktrees/initiative-<id>`. Because the source rows'
host base is not a child of the `/workspace` host base, derivation and
resolution are a sibling two-base path (`initiativePlanRows` + resolver
analog), never a reuse of the single-root task code; bind mechanics are
unaffected (mounts attach by container destination). Access mirrors the task
table: RW for the child Worker on source/git, `readOnlyView` forces every row
RO for child Verifier/Packager. The ADR-017 D6 destination table is amended
IN PLACE (single table — the `adr017Rows` guard parses destination cells
verbatim and a parallel table would silently blind it): the three
`<mc-task-name>` cells generalize to the closed worktree-name grammar
`mc-task-<id> | mc-initiative-<id>` (collision-free by distinct literal
prefixes), and every duplicated grammar pin updates in lockstep
(`boundary/typedkind_internal_test.go` map keys,
`validTaskWorktreeName`/`validTaskPlanDestination` in `mountplan.go` and the
helper-boundary recheck, `setupenvelope.go:125`, the resident's
`worktree_name` derivation). Existing TypedKinds are reused; per-attest
TypedRoots hold exactly one subject's receipt-vouched identities, so ADR-021
D10a's one-kind-one-row discipline holds. The row derivation keys on
`SubjectInitiativeID`, never on which id happens to parse from a path.

**D3 — The disk cut is the promotion-observable `InitiativeSetup` effect.**
Dispatch emits an `InitiativeSetup` step for any live, seeded
`scope='initiative'` row whose `tasks.branch` is set but whose initiative
setup receipt is absent — at the first tick where promotion is observable and
before any other initiative-family spawn for that row. The resident
precreates the skeleton (store root 0555 with exactly `{git, source}`,
worktree dir 0700 under `.mc-worktrees`), runs `mc __setup-initiative` in a
network=none, uid-10002, cap-drop-ALL container (real repo RO, store RW,
worktree RW) that materializes the sanitized store cut from the CURRENT main
tip and the checkout (generalizing `MaterializeFirstTaskStore`), then
registers an initiative-keyed durable receipt carrying BOTH root identities
(store root and worktree root — two vouched roots; the worktree is not a
descendant of the store root) and the recorded cut SHA. A retry after a
failed or interrupted setup reuses the recorded cut SHA exactly and never
re-resolves main (mirror of the fresh/retry task-assignment discipline);
siblings of an already-committed child are never silently rebased. The
receipt is its own initiative-keyed spine record — NEVER a `task_assignments`
row, which would both refuse the plain child terminal and destroy ADR-023
D3's synchronous-archive-at-approve arm. Reading recorded against Inv. 25
and ADR-023 D5: "cut at promotion" is satisfied at the first dispatch tick
at which promotion is observable — the earliest any actor outside the
promoting transaction can act; ADR-023 D5's production sentence is amended to
this reading.

**D4 — Children complete seal-free; seal machinery is suppressed for
initiative subjects.** Production children use the plain unsealed Worker
terminal exactly as ADR-023 D3 defines (branch validated against the parent's
shared branch, never stored). The sealed-completion spine cannot and must not
apply: `attestCandidateMounts` suppresses `CompletionSeal` and
`AcceptedSealRebuild` step emission whenever `SubjectInitiativeID` is set (an
accepted-seal rebuild of a SHARED store would destroy sibling commits). The
integrity posture — weaker than the standalone sealed terminal — is
explicitly accepted, bounded by: the global execution lease (at most one
sanctioned pipeline container system-wide, Inv. 1), receipt-vouched
initiative roots, container-confined writes into the initiative-local store
only, D6's producer-absence/cleanliness fence, per-child packet review, and
full arc-stage verification before anything reaches main.

**D5 — Child Verifier/Packager get seal-free RO views; Refiner keeps
refusing.** A Verifier or Packager whose subject is an initiative child
receives the D2 table with every row forced RO, gated on the vouched
initiative receipt and D6's fence — no completion-seal gate (D4). Refiner
over a child refuses, preserving parity with the standalone table (no refiner
arm exists there either). All other combinations retain today's refusals:
initiative children on non-repo or profile-less Worksources; any non-Worker
role while the store/worktree is absent (setup authority stays Worker-tier:
the `InitiativeSetup` step is emitted by dispatch, not granted to a role);
missing/unvouched receipts; editor/strategist/console roles over a child
subject. The snapshot-capture branch
(`captureDispatchMountHostSnapshot:526`) gates on `SubjectInitiativeID`
BEFORE the task-skeleton arm so a child can never trigger standalone
task-precreate (which `validatePrivateTaskPrecreateCandidate` would wedge as
a protocol error). The fake lane is unchanged.

**D6 — Producer-absence and cleanliness fence for the shared store.** Before
any next initiative-family container for initiative I is prepared, the
resident must confirm the ABSENCE of every prior child container of I (the
ADR-017:533 producer-absence discipline extended from per-task to
per-initiative — reap's best-effort `docker stop` is not confirmation), and
attestation must assert store-worktree cleanliness (working tree and index
clean at the branch tip). A reaped-but-unstopped child holding the shared RW
bind is cross-run interference, not self-harm, and silent contamination of
the next child's commit is the failure mode this fence exists to prevent.

**D7 — Strategist(initiative) and Editor(plan-review) arms.** These roles
receive the same non-mutating durable-record/console arm the standalone
Strategist/Editor grant already provides — the role set of the existing
grant extends; no RW Git arm and no new destination row. If implementation
finds those roles currently reach the `mountattest.go:279-291` refusal on
repo Worksources, the fix is scoped to admitting the existing non-Git arm
for the initiative-family roles, not to inventing workspace access.

**D8 — Arc verification proves cleanliness and merge-cleanliness from the
store; no objects reach the real repo before approval.** The arc Verifier
receives the initiative store rows RO (the branch objects at tip) plus the
existing execution-scoped committed-tree projection of the real repo's
current main (Verifier projection machinery, ADR-017), verifies the shared
branch merges cleanly onto current main, verifies the store worktree/index
are clean at the tip (this REPLACES the legacy lane's dirty-worktree fence,
which mc-land can no longer provide — its worktree discovery resolves empty
for store-linked worktrees), and pins `verified_sha` to the tip. Strict
drain (no open children, `AdvanceStage` worked-arm) remains the precondition
for the done-declaration that precedes arc verification, so no child commit
can move the tip after `verified_sha` is pinned.

**D9 — Landing import, unchanged mc-land, resident-owned teardown.** After
the operator approves the arc packet, at land-prep the resident runs a
containerized import (network=none; real repo RW, store git RO): pack the
store branch's closure (`pack-objects --revs`), `index-pack` with full
verification into the real repo, then an atomic compare-and-swap ff-only
creation/advance of `refs/heads/mc/initiative-<id>` to exactly
`verified_sha` — mirroring the sealed lane's `importSealedClosure` /
`createLandingRefCAS` pattern (`mc/verbs/landsealed.go`). It then invokes
the UNCHANGED legacy `mc-land <branch> <verified_sha> <target_ref>`: the SHA
fence, `--no-ff` merge, and branch compare-and-delete all work; worktree
discovery benignly resolves empty and skips removal. On the recorded landing
success the resident removes the worktree directory and the initiative store
host-side (pure `rm`-level cleanup, no Git, exact-identity fenced like
task-root removal, residue preserved as debt) — and removes the store before
any import retry could resurrect the branch mc-land just deleted. mc-land
itself gains no delete capability over `.mission-control` or `.mc-worktrees`
paths.

**D10 — `.mc-worktrees` is reserved and excluded.** The setup/seal/verify
reserved-tree-component rejection extends to `.mc-worktrees` (today only
`.mission-control` and `.git`), so no child can commit paths that would
collide with the live worktree area at merge time. Onboarding adds
`.mc-worktrees/` to the real repo's `info/exclude` beside
`.mission-control/`. Every setup-class container that binds the workspace RO
gains an empty RO cover over `<workspace bind>/.mc-worktrees` (beside the
existing `.mission-control` cover) so no setup container can read another
initiative's uncommitted child work.

## Slices

Each slice keeps the fast suite green and is inert (fail-closed) until the
last lands; real-harness initiatives stay health-refused until S6.

- **S1** — `InitiativeSetup` cut: skeleton precreate, `mc __setup-initiative`
  materializer, initiative-keyed receipts (two roots + cut SHA), retry
  reuse, `.mc-worktrees` discipline, D10 reservations/covers.
- **S2** — Child Worker mount rows: `initiativePlanRows` two-base
  derivation + resolver, snapshot gating, seal-emission suppression, ADR-017
  D6 in-place amendment + every grammar pin, refusal-retention tests.
- **S3** — Child Verifier/Packager RO arms; D6 fence.
- **S4** — Strategist(initiative)/Editor(plan-review) role-set extension.
- **S5** — Arc Verifier arm (store RO + main projection + cleanliness fence)
  and arc Packager.
- **S6** — Land-prep containerized import + resident post-land teardown;
  Docker-lane acceptance of the full production initiative lifecycle.

## Consequences

- Invariants preserved: **25** (one branch, one shared worktree, cut at the
  first promotion-observable tick, main moves only on the arc approval —
  children stay branchless, D3/D4), **11** (child and arc packets hold their
  slots independently; receipts are never assignments), **1** (landing takes
  no lease; the import and land containers are lease-free effects), **10**
  (landing-pending stays derived; receipts and import outcomes live outside
  the tasks/review_packets spine), **22** (store and worktree are strict
  descendants of the initiative's one Worksource).
- The real repository's objects, refs, config, and hooks are never mounted
  into any agent container; the real repo receives initiative objects
  exactly once, at approved landing, through a verified import — the same
  posture ADR-017 chose for standalone tasks.
- The host never executes operator-installed Git anywhere in this design.
- Deviations recorded in IMPLEMENTATION-NOTES (2026-07-23): the
  promotion-time reading of Inv. 25/ADR-023 D5, the seal-free child
  completion posture, and the host-unresolvable pointer bytes amending
  ADR-017:425-429.
- Reversibility: every slice is additive behind the existing refusals;
  reverting restores the current fail-closed initiative surface without
  touching the standalone task path.
