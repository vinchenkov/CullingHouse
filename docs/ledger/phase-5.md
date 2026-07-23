# Phase 5 ledger — operator-scheduled real-subscription acceptance

Append-only history; never a startup read. One heading per session.

## (opened 2026-07-22)

Opened at Phase 4 close (d0ef4bb, all lanes green). Phase 5 work is
operator-present by design: live provider credentials (refresh grants +
token_url/client_id extraction), the production install bootstrap (warm
helper provisioning order — see IMPLEMENTATION-NOTES 2026-07-22), the
one-time launchd load, initiative production mount rows (ADR-023 D6), and
the S7 sleep drill. Nothing here starts without the operator's go.

## 2026-07-22 — kickoff authorized; mechanical Phase 5 work begins

The active operator goal authorized continuing through Phase 5 and requested
subagent orchestration. The session-start six-leg fast suite is green at
`35240bd`; the resident and dashboard loopback-listener tests required an
unsandboxed rerun after the restricted runner denied `Bun.serve({port: 0})`.
No product assertion failed on the permitted reruns.

Private-input presence was checked without printing values.
`OPERATOR-INPUTS.md` exists and is ignored, but still lacks the Phase 5
subscription-spend budget, the Codex custom-CA version floor, and the OAuth
`token_url`/`client_id` constants already called out by the handoff. Those
gate live token spend, not the agent-doable fail-closed implementation work.

NEXT: finish the required cross-harness review and Phase 5 gap map, then land
the first green test-first implementation slice. Do not run live-token turns
or load launchd until the parked private inputs and operator-present window
exist.

## 2026-07-22 — production preflight fails closed (`bc0dee4`)

The mandatory cross-harness review returned three high-severity onboarding
findings and they were recorded in `IMPLEMENTATION-NOTES.md`. The first red/
green slice added a Docker-free front-door contract to the fixed fast suite.
`install.sh` now refuses a missing Docker CLI before deployment writes, starts
Docker Desktop and polls up to 60 seconds on macOS, refuses an unavailable
daemon, and returns nonzero rather than reporting success when `mc-helper` is
absent. All six fast legs are green.

The runtime-auth probe verified the public OAuth endpoint/client constants and
required runtime switches in installed Codex 0.145.0 and Claude 2.1.218 without
printing credentials. The personal Codex login must not be imported because a
refresh grant needs one rotation owner; Claude is currently unauthenticated.

NEXT: build the bootstrap-safe idempotent helper provision/capability probe so
fresh production install reaches the wizard without a host-side spine open.

## 2026-07-22 — credential-store refusal + deployment identities (`9cec34f`, `e7c4ca2`)

Two additional green slices landed while the bootstrap crossing was reviewed.
The resident now treats only `ENOENT` as an absent refresh-grant store;
permission/I/O ambiguity aborts startup, and duplicate binding owners refuse
before the token service starts. The runtime-auth probe's remaining findings
stay open (notably configured non-fake routes without a projector, provider-
exact parsing, durable rotation, and MiniMax materialization).

The shared deployment package now canonicalizes MC_HOME through an existing
ancestor and derives domain-separated 12-hex identities. Symlink aliases
converge; different homes get distinct `mc-spine-*` volumes and `mc-helper-*`
containers. Onboarding preflight consumes that canonicalizer, so its git fence
and future runtime names use one physical identity.

The read-only design follow-up settled bootstrap as a path-free private
same-binary crossing: Darwin owns home checks/scaffold/mirror and exact Docker
envelope management; Linux owns `/mc/spine` meta initialize/migrate/compare.
The final helper is the provisional crossing and never mounts MC_HOME. This is
recorded in `IMPLEMENTATION-NOTES.md`; no new ADR is needed.

NEXT: implement and test the private `__onboard-spine` state matrix, then the
Darwin helper provision/mirror/capability composition.
