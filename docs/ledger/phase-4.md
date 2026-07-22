# Phase 4 ledger — E2E control-loop scenario families

Append-only history, newest last. Never a startup read; grep it for rationale.
Scope: handoff Part 3 "Phase 4" — six fake-harness scenario families, real
containers/spine/resident, all progress timer-driven.

## 2026-07-22 — Phase 4 opened

Phase 3 closed COMPLETE (all seven phase3-contract §8 bullets green; operator
signed off on advancing). Phase 4 begins with scenario family (1) full
pipeline + landing. The happy-path pipeline already exists as
`TestWalkingSkeleton` (docker_e2e); Phase 4 adds the approve/land SPLIT
assertion and the landing-failure and multi-approve-drain variants, and will
force closed the three non-blocking landing loose ends carried from Phase 3
(unenforced 15-min landing deadline, `SealedLandingResult` has no spine
consumer, ADR-016 D7 label non-conformance in setup/legacy-land — all in
`IMPLEMENTATION-NOTES.md` 2026-07-20/21). Recon of the scenario-1 gap vs.
existing coverage is in flight.

NEXT: from the recon, build scenario (1) — start with the approve/land split
assertion on the existing pipeline, then the landing-failure variant (which
needs the landing failure taxonomy to be observable), closing the landing
loose ends as the variants demand them.
