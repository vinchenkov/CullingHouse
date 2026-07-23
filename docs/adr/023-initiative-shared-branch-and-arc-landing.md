# ADR-023 — Initiative shared branch and arc landing

## Status

Accepted (2026-07-22). Amended 2026-07-23 by ADR-025, which is the follow-on
ADR this Status paragraph and ADR-017 D6 required: it supplies the production
real-harness mount representation and discharges the deferred promotion-time
worktree-cut effect. ADR-025 D3 amends D5's production sentence — the disk
cut runs as the promotion-observable `InitiativeSetup` dispatch effect
(create-if-absent at the first tick where promotion is visible), and under
that topology the legacy land no longer discovers or deletes the shared
worktree itself; teardown moves to the resident (ADR-025 D9). Extends ADR-017 D6's closed mount table for the
initiative/child path it explicitly Parked ("not eligible for the accepted
Phase-3 spawn path until the operator resolves the parked design and a
follow-on ADR extends this closed table"). Governs the initiative branch
grammar, the child completion terminal, and the arc landing lane. It does NOT
un-park the *production real-harness* shared-worktree Git-control mount
representation — that remains ADR-017 D6's parked item, now constrained by the
decisions here, and is owed when real-harness initiatives are enabled
(Phase 5). This ADR is what makes Phase 4 family (4) — the fake-harness
initiative control loop — reach a real merge to main.

## Context

The behavioral contract is fully specified (Inv. 25, §5, §6.1, §7): an
initiative gets one branch and one shared worktree cut at promotion; every
wave child commits its changes into that shared branch in the same breath as
its Worker-stage completion; the child's packet diffs against the initiative
branch; approving the *arc* packet writes the landing-pending mark and a
following tick merges the branch onto main, archives, and deletes the
worktree. Only the arc approval moves main.

The state machine, dispatch selection (`spawnFor`, `nextLanding`), the landing
fence, and `verified_sha`/`target_ref` population on the arc row are all
already built and scope-agnostic. Three mechanism questions were Parked:
(a) the branch NAME (the spec deliberately does not name it —
`complete.go:144` "Do not invent its naming convention here"); (b) how
children mount/commit to the shared branch; (c) the arc landing lane.

The load-bearing hazard: `Approve` (`mc/domain/task.go`) lands anything
branch-carrying-or-assigned and synchronously archives only branchless +
unassigned rows. If a child stored the shared branch in its own `tasks.branch`,
approving that child's packet would select it in `nextLanding` and merge to
main — a premature, per-child landing that violates Inv. 25 ("main moves only
on the arc-packet approval").

## Decision

**D1 — Branch grammar `mc/initiative-<id>`.** The shared branch is named
`mc/initiative-<id>`, mirroring the standalone `mc/task-<id>` grammar. This is
the one operator-policy choice; the conservative default reuses the existing
`mc/` namespace so the branch is visibly Mission-Control-owned and never
collides with a task branch. Cut off main at promotion (Inv. 25).

**D2 — Promotion sets the initiative's `tasks.branch`.** `domain.Promote`, on a
`scope='initiative'` row, sets `tasks.branch = 'mc/initiative-<id>'` in the
same transaction as `status='seeded'`. This is the spine half of "cut at
promotion". The git branch/worktree on disk is materialized separately (D6).
Setting the branch flips `LandingPending()` true only once the arc row is
additionally `approved`, `packaged`, and carries `verified_sha` + `target_ref`
— none of which hold until arc verification — so it is inert through the wave
lifecycle and cannot land prematurely.

**D3 — Wave children are BRANCHLESS (the correctness decision).** A child
commits its changes into the shared branch inside the shared worktree, but its
own `tasks.branch` column stays empty. The child Worker terminal
(`mc/verbs/complete.go`) validates that any supplied `--branch` equals the
initiative's shared branch (§6.1, Inv. 25) but does NOT store it on the child
row. Consequently a child is always branchless + unassigned, so `Approve`
takes the synchronous-archive arm: approving a child's packet accepts its
contribution and archives it **without any merge to main** — the child's work
is already integrated into the shared branch. Only the initiative (arc) row
carries `tasks.branch` and lands. This preserves Inv. 25 (main moves only on
the arc approval), Inv. 11 (each packet — child or arc — holds its slot until
its own operator decision), and eliminates the premature-per-child-merge
hazard by construction rather than by a scope filter in the landing lane.

**D4 — Arc landing uses the legacy branch lane; no new lane.** An initiative
has no `task_assignments` row, so `SealedLandingPending()` never selects it; it
carries `tasks.branch`, so `LandingPending()` does, and the existing legacy
`land()` effect (`resident/src/effects.ts`, baked `mc-land <branch>
<verified_sha> <target_ref>`) merges `mc/initiative-<id>` → main and deletes
the branch's worktree (§7 steps). The shared branch is just another branch to
the legacy lander; `nextLanding` already selects it scope-agnostically. No
landing envelope, `landing.json`, or sealed lane change is required.

**D5 — Shared worktree lifecycle.** The shared worktree is
`<workspace_root>/.mc-worktrees/initiative-<id>` on branch `mc/initiative-<id>`.
Every wave child commits there; the arc verifier verifies the shared branch
merges cleanly onto current main and pins `verified_sha` to its tip; the
legacy land deletes it on merge success (§7 step 3). In PRODUCTION the resident
cuts the worktree as a promotion-time effect (part of D6 below). For the
fake-harness control-loop E2E, the child Worker creates it idempotently on
first use (create-if-absent), which is behaviorally equivalent for the state
machine and needs no new resident effect.

**D6 — The production real-harness mount representation stays Parked, now
constrained.** ADR-017 D6's refusal (`mountattest.go:238-249`) fires only under
*real* routing and is skipped under the fake harness, where children fall
through to the legacy whole-Worksource RW bind and therefore already share one
`/workspace/source`. That is adequate for the fake-harness control-loop proof
(Phase 4) and this ADR relies on it. The *production* per-child shared-worktree
Git-control mount rows — a real branch-isolated worktree bound RW with the
generated Git-control nest, extending ADR-017's closed D6 table — remain owed
when real-harness initiatives are enabled (Phase 5), and are now constrained by
D1/D3/D5: shared branch `mc/initiative-<id>`, branchless children, shared
worktree `.mc-worktrees/initiative-<id>`.

## Consequences

- Phase 4 family (4) reaches a real `--no-ff` merge to main: charter → wave →
  plan review → children commit to the shared branch and archive on approval
  (branchless, synchronous) → strict drain → done-declaration → arc verify →
  arc packet → approve → legacy land merges the shared branch → archived,
  worktree deleted.
- Invariants preserved: **25** (one branch/worktree, main moves only on the arc
  approval — enforced by D3 branchless children), **11** (each packet holds its
  slot; child and arc packets decided independently), **1** (landing takes no
  lease; the legacy land is lease-free), **10** (landing-pending stays derived,
  never a column), **22** (the shared worktree lives inside the initiative's one
  Worksource; no cross-Worksource hole).
- Reversibility: the code deltas are small and local — `Promote` sets one
  column; the child Worker terminal drops one `UPDATE tasks SET branch`.
  Reverting restores the pre-ADR branchless-no-land initiative and the
  synchronous-archive-at-approve behavior.
- Deliberately deferred (Phase 5): the production real-harness shared-worktree
  Git-control mount rows and the promotion-time resident worktree-cut effect
  (D6); the "merge main into the branch at every wave boundary" drift-absorption
  step (§6.1) — a Strategist(initiative) action that is orthogonal to landing
  and not needed for the control-loop proof.
