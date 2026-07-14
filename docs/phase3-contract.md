# Phase 3 contract — boundary conformance

Behavioral authority: `specs/mission-control-spec.md`, especially §§5, 9,
11, 16, and 17. Acceptance authority: `specs/implementation-handoff.md`
Part 3 "Phase 3". **The spec wins on conflict with this document.** Phase 2
is accepted for every unparked line at `bf55820`; the initiative-wave
representation remains parked and this phase may not route around it.

Phase 3 turns the Phase 0 boundary spikes into the production boundary. It
is complete only when every enforcement mechanism in handoff Part 3 has one
named test through its real enforcement point. Pure planners and validators
stay in the ordinary no-Docker fast lane; kernel/runtime claims are proved by
the tagged Docker boundary suite at phase completion.

## 1. Locked ownership and failure semantics

The boundary has four ordered responsibilities across three owners. Their
separation is acceptance contract, not an implementation suggestion.

1. **The resident owns runtime observations and effects, never state.** At
   boot and every tick start it ensures the helper where possible, inventories
   labeled containers, and probes runtime, gateway, image, and network
   capabilities. Red readiness forbids a *new spawn claim* but does not skip
   lock reconciliation: when the lock domain remains reachable, the tick
   still invokes the one real `mc dispatch` so step 0 may reap and other
   non-spawn actions may converge (§10, §11.7). When Docker and therefore the
   macOS helper/spine crossing are wholly unreachable, no durable write is
   physically possible; the resident emits a local diagnostic, retries, and
   records the health event through `mc` on the first recovered crossing.
2. **The cross-boundary `mc dispatch` command owns selection and the trusted
   plan handshake.** The external call remains one real command. Its host
   side is the only place that can resolve the native file plane and observe
   Docker applicability; its lock-domain side is the only place that can load
   canonical Worksource/Profile state and write the consequence. Before a
   claim, the two sides must agree on one candidate-bound plan and capability
   snapshot produced by the same pure policy package. The prerequisite ADR
   must choose the concrete self-delegation handshake, replay/candidate hash,
   and TOCTOU recheck; this contract does not pretend the warm helper can see
   unmounted host Worksource paths, and it does not give the resident a spine
   write path.
3. **The lock-domain half owns the state consequence.** After the pure
   decision selects a spawn and before `domain.Claim`, it accepts only the
   matching, fully validated host plan. Invalid, stale, incomplete, or
   unappliable mount/env/auth/network evidence blocks a subject task with a
   stable, precise confinement reason in that transaction and returns no
   spawn effect. It creates no Run and leaves the lock free. A subjectless
   spawn cannot invent a task to block: it records a health/configuration
   failure, returns no effect, and leaves the lock free.
4. **The resident mechanically effects the frozen plan.** The spawn effect
   carries the fully materialized plan. The resident neither adds, drops,
   downgrades, re-resolves, nor reinterprets policy. It uses argument arrays,
   not a shell or concatenated `-v` strings. If the runtime changes after the
   pre-claim readiness/applicability checks, no partial container may run;
   an already-claimed run follows the existing infrastructure-failure/reap
   budget. A runtime race is not relabeled as a valid plan or silently run
   with weaker confinement.

This ordering is the fail-closed inversion required by §§11.3–11.4. It also
keeps `mc` as the only state writer: the resident never opens the spine or
manufactures a task transition.

Before production code for this flow, planned ADRs must pin:

- the exact one-command host↔lock-domain planning handshake, readiness input,
  candidate/TOCTOU fence, immutable spawn-effect schema, and stable
  invalid-plan reason codes; it must cover both leased pipeline spawn and
  the lease-free, registry-driven Homie wake/resume path frozen by ADR-009
  and ADR-012, plus exact pipeline/Homie/helper labels and liveness matching;
- the mount-allowlist grammar, complete shipped blocked-pattern floor, and
  comparison rules, including deterministic destinations for plural
  Worksource, artifact, session-file, and seeding mounts;
- the Docker Desktop gateway/network topology: proxy-bypass denial, DNS
  resolution/rebinding posture, raw-TCP host:port enforcement lifecycle,
  static-secret injection scope, and egress-audit ownership; and
- finite agent-container CPU, memory, and PID defaults plus configuration
  bounds. The spec delegates those values; they are not operator questions.

## 2. Configuration and trust roots

### 2.1 Configuration layers

The effective configuration is resolved once with the spec's precedence:
shipped defaults, then `MC_HOME/config.toml` plus `routing.md`, then the
selected Worksource/Sandbox Profile. A later layer may tune a value only
where the schema permits it; it may not weaken a non-removable security
floor. Worksource repository files are never configuration inputs.

`MC_HOME` is the only production env-level configuration knob (§16.3).
Production binaries do not honor `MC_SPINE`, `MC_RUN_JSON`, fake-harness
selectors, workspace overrides, or helper-name overrides from an agent's
environment. The spine path in the lock domain and `/mc/run.json` in an
agent container are fixed. Tests inject paths and fakes through constructors
or test-tagged seams that are absent from the untagged binary. In particular,
`MC_RUN_JSON=/missing` must never turn a container into host scope, and a
crafted file path must never forge tier, role, allowlist, or run identity.

The mount allowlist is `MC_HOME/mount-allowlist`, outside every mounted root.
Its shipped blocked-pattern list is a non-removable floor; operator entries
are additive only. Profile save and every spawn both parse and validate the
same effective rules. Symlinks are resolved at the time of each validation,
so a post-save swap fails at spawn.

### 2.2 Forbidden environment policy

Both `harness_env_policy` and `tool_env_policy` are constructed from an
explicit safe base; neither spreads the resident's ambient environment. The
scanner enumerates every `*_API_KEY`-shaped entry as required by the handoff,
then rejects names in the effective forbidden guard. The non-removable
shipped guard is exactly the spec §16.3 floor: `CODEX_API_KEY` and
`ANTHROPIC_API_KEY`. Operators may extend it, never shrink it. Arbitrary
Worksource tool-secret names are not banned merely for ending in
`_API_KEY`—§5 deliberately permits operator-managed tool secrets. A rejected
key is proven absent from the final container environment and from logs.
The mechanism test adds `SENTINEL_API_KEY` to the operator guard, proves it
is found and rejected, then proves the same wildcard-shaped name is still
enumerated but not silently treated as part of the shipped floor when that
extension is absent.

Routing uses the configured binding catalog and the strict `routing.md`
parser. Every role must resolve, fake bindings are test-tag-only, and
Strategist↔Editor plus Worker↔Verifier must use different harness families.
Validation occurs before lease claim and has no fallback.

## 3. Enforcement-mechanism acceptance matrix

Each row has one production owner and one minimum test through that owner.
Unit tests of helpers supplement but do not replace the mechanism test.

| Mechanism | Production owner | Required acceptance evidence |
|---|---|---|
| Mount source + target validation | shared `mc/boundary` policy used by profile save and both sides of `mc dispatch` | One ordinary accept and one reject for each class: symlink stays/escapes; every shipped blocked-pattern class; clean relative target vs absolute, `..`, or `:`; requested RO; RW only when request and allowlist both permit it; own Worksource vs another Worksource; ordinary root vs credential/control root. Rejection never silently downgrades or drops a mount. |
| Fail-closed plan inversion | `mc dispatch` transaction | An unappliable mount and an unappliable `network_allow` rule each leave zero Run rows, a free lock, no spawn effect/container, and the pipeline subject blocked with the exact reason. A subjectless pipeline or lease-free Homie invalid plan records health, returns no effect, and does not fabricate a task, lease, Run, or registry transition. |
| Forbidden env | boundary env builder + `doctor` | Both harness and tool policy reject each shipped forbidden key before claim; `SENTINEL_API_KEY` proves wildcard enumeration/classification without expanding the spec's guard floor; a declared permitted tool secret survives only in its declared plane; the final Docker inspect/process env contains no forbidden key. |
| `MC_HOME` git fence | onboarding `preflight`/`home` | Existing outside-tree and ignored-in-tree accepts remain green; unignored worktree and symlink-alias bypass reject before writes. This Phase 2 implementation is promoted, not rewritten. |
| Blocked list extend-only | config/allowlist loader | Shipped floor parses; additive local entries work; omission/redefinition cannot remove a floor entry; malformed or shrinking input fails closed. |
| Routing decorrelation | routing loader + dispatch planner | Complete configured catalog resolves; missing/unknown/fake production binding rejects pre-claim; both producer/judge pairs are different families. Existing strict routing tests remain the oracle. |
| Setuid gate (S1 promoted) | production image + named spine volume | Under the real untagged image and real production `mc`, agent uid direct read and write both fail with `EACCES`; the same operations through `mc` succeed; ruid/euid and 0700 directory/0600 database ownership are asserted. Permanent canaries prove runc, no uid remap/ECI, `no-new-privileges` absent, suid honored/no `nosuid`, and native arm64/no emulation. Container restart, image rebuild, and volume reattach preserve the gate. |
| Per-container scope | fixed RO `/mc/run.json` + `mc` authorization | Docker proves the envelope cannot be changed or replaced and has no RW alias. The real binary enforces kernel-backed pipeline/Homie tier, role, allowlist, own/other/stale run/session identity, and zero-state-change denials. Separately, env/capability tests prove runner-only lifecycle/transport verbs are not exposed to the model or subagents; this within-container same-uid grain is the spec's explicit best-effort boundary, not a claimed kernel refusal. Ordinary spine reads are Worksource-scoped; Homie alone has the explicit board-wide `mc get/list` exception. |
| Complete mount plan | boundary planner + resident effector | Docker inspect and in-container probes prove every §11.3 mount's presence, mode, and accessibility, and prove other Worksource/session/operator credential roots absent. The inspected argv/plan equals the immutable dispatch effect. |
| Gateway + three egress modes | resident-hosted gateway + ADR-pinned Docker network layer | A deterministic local authenticated upstream succeeds through injected auth and trusted CA; direct DNS and direct IP fail. A second allowed non-provider upstream and a different binding/Worksource prove the selected credential is injected only at its provider and no other binding/control dir is present. An unwired/dead gateway yields no spawn. `none` and `allowlist` each have host allow/deny probes; `open+audit` permits an arbitrary gateway host and records it while its negative probe proves direct bypass still fails. Raw TCP remains independently `network_allow`-only in all three modes. |
| Warm helper (S2 promoted) | darwin self-delegation + helper lifecycle | Same-version real `mc` round-trips rc 0/1/126/127/137/143, 50 MB 8-bit stdin/stdout, separate stdout/stderr, five concurrent calls, and a 30-minute stream. INT/TERM explicitly terminates the remote process group and returns 128+signal. Kill/remove and daemon restart lazily rewarm one labeled helper. It has no run.json and therefore host scope. |
| Orphan sweep | resident tick-start reconciliation | Label-scoped inventory stops a pipeline container unless its run **and Worksource** labels match the live lease, and stops a Homie container unless its session identity matches an active registry row within idle policy; a mismatched live row is not liveness. Unrelated containers and the explicitly labeled helper survive. Before spawn, confirmed absence of another pipeline container is mandatory; ambiguous stop/absence refuses replacement. Session folders are never swept; only the ephemeral envelope is removed on normal exit. The lifecycle ADR pins the helper label/sweep exclusion because §11.5 does not assign the helper a `pipeline|homie` tier value. |
| Resource bounds | resident effector from resolved config | Docker inspect proves finite CPU, memory, and PID bounds, non-root agent uid, no privileged mode or Docker socket, and the required setuid-compatible security options. Omission resolves to finite ADR-pinned shipped defaults (§16.2); only an explicit invalid or unbounded override rejects before claim. The ADR decides whether overrides are deployment- or profile-scoped. |

`mc doctor` exercises the same production parsers and probes; it does not
carry a parallel approximation. Each failed Phase 3 check names its repairing
onboarding section. Runtime identity/version strings are not acceptance:
capabilities are.

## 4. Canonical spawn and mount plan

The immutable plan is per Worksource and per run (§11.1). It includes the
container name `mc-<worksource>-<run_id>`, `mc-managed`, `mc-tier`,
Worksource, and run labels; image digest; non-root uid; resource bounds;
network/gateway rules; explicit env; entrypoint/argv; and only these mounts:

| Container destination | Source | Access |
|---|---|---|
| `/workspace` | container-local scratch | RW, ephemeral |
| `/workspace/source` | the assigned Worksource workspace/worktree | RW for ordinary single-Worksource runs; RO for cross-Worksource seeding |
| deterministic artifact destinations | validated profile `artifact_roots` | RW for ordinary runs; RO for Homie operator scope |
| fixed `/mc/run.json` | resident-materialized launch envelope | RO, separate nested mount, ephemeral |
| harness session-write path | only `sessions/<run_id>/` | RW, permanent trace-only folder |
| fixed sandbox-config path | resident-materialized validated config projection | RO, separate nested mount, ephemeral |
| declared reference destinations | validated `readonly_mounts` | RO |
| `/home/agent` | per-Worksource synthetic home | RW |
| declared package-cache paths | per-Worksource state | RW |
| `/app/src` | shared agent-runner source | RO |
| fixed spine location | runtime-local named volume | RW to privileged `mc` uid; inaccessible to agent uid |
| declared harness control path | canonical `runtime_control_dir`, only for a `materialized` binding | RW, explicit audited downgrade |
| declared proxy/CA paths | run-scoped gateway material | minimum access; CA certificate only, never gateway secret |

The mount ADR assigns collision-free, deterministic destinations for plural
artifact/reference roots and for every multi-Worksource exception; no two
host sources collapse onto one container path. No source has a second alias
with a stronger mode. `run.json`, runner source, allowlist/config, and sandbox
inputs have no RW alias. No agent container gets the runtime socket or the
operator's real HOME. An ordinary run gets no other Worksource root and no
other run's session bytes. Session folders persist forever; the envelope and
sandbox projection do not.

For a standalone task, the plan names the canonical retry-safe
`mc/task-<id>` branch and worktree path and the primary checkout remains
untouched. The resident effects that exact assignment through a short-lived
setup container at the canonical mount path; Mission Control never mutates a
Worksource with host Git (§6.2). Setup failure after an otherwise valid claim
is infrastructure failure, not permission to mount the primary checkout.
The parked initiative wave/shared-worktree representation is not inferred.
Strategist(propose)'s cross-Worksource seeding plan is the spec's enumerated
RO filesystem exception: it receives deterministic, distinct RO workspace
mounts, but that does not silently grant board-wide `mc get/list` access.

A Homie uses a separately planned, lease-free `mc-tier=homie` effect selected
from active registry/pending-turn truth, never the pipeline lease. Its
operator profile receives distinct RO mounts for every Worksource workspace
and artifact root plus board-wide scoped `mc get/list`; its own session folder
is the only RW trace destination. To reconcile §15.3's access to historical
"session files" with Inv. 26's rule that another run's **folder** is mounted
only into its owner, the plan may enumerate completed native trace files as
individual RO references under a deterministic operator-reference tree; it
never mounts another session directory and never gives a stronger alias.
Start/resume still returns record truth only (ADR-009/012); the later tick
selects and validates the Homie wake effect.

The production image is a separate untagged build from `mc-fake-e2e`. It
contains the Linux/arm64 static `mc` built from the same source/version/build
identity as the platform-specific host install, owned and mode 4755 by the
privileged uid; a distinct agent uid; exact pinned Codex and Claude harnesses;
Node, Bun, git ≥2.48, Chromium/Playwright, and tini. The runner remains a
read-only source mount. A production build cannot import the
`test_fake_routing` registry or fake adapter.
The helper and agent image use the same Linux build of `mc`; the Darwin host
artifact is release/schema compatible, not byte-identical.

Non-live fixture tests complete the canonical Codex and Claude adapter
boundary—safe env construction, start/resume, native session handle/file,
completion/exit, and signal forwarding—so the production image/registry does
not merely refuse every canonical route. Live provider turns and refreshes
remain Phase 5.

## 5. Gateway and network contract

The resident owns the gateway and runtime networking; the container receives
only proxy coordinates and the trusted CA for `gateway` delivery. The static
credential stays host-side and is injected only for the selected provider.
For OAuth `materialized` delivery, the one canonical control directory is
mounted, never copied; the plan records this declared downgrade, and the
proxy still enforces egress.

HTTP and raw TCP are separate planes. Provider endpoints are always the
minimum HTTP set. `none` adds nothing, `allowlist` adds only configured
domains, and `open+audit` admits arbitrary HTTP while logging every host.
None of them permits direct proxy bypass. Raw TCP admits only successfully
resolved and applicable `network_allow` host:port rules, including under
`open+audit`; its audit record is the standing rule, never a false claim of
per-connection observation.

Automated Phase 3 probes use local fake upstreams and dummy credentials.
There is no live token spend in this phase.

## 6. Helper, lifecycle, and operations

Darwin `mc` determines that the fixed spine is unreachable and self-delegates
the entire real argv/stdin/stdout/stderr/exit stream into the derived,
deployment-labeled helper. It does not select the helper or spine from
ambient env. The helper mounts the derived named volume, runs the identical
`mc` version, has no run.json, and has no agent harness, gateway, or runtime
socket. Any invoker may lazily recreate it; the resident also ensures it at
boot and every tick. There is no host-file spine fallback.

The resident begins each tick with helper/runtime readiness and the orphan
sweep, then calls `mc dispatch`, then effects at most one action. Pipeline
liveness is the live global lease; Homie liveness is its active registry row
and idle policy. An uncertain container stop is not absence. Normal exit
removes only the ephemeral run envelope; retained session folders and native
traces are never garbage-collected.

Phase 3 closes ADR-014/ADR-015's boundary deferrals: `home` creates the
derived named volume and deployment identity; config/routing/allowlist
examples and the effective config land under `MC_HOME`; `container` builds
and pins the image, helper, and gateway idempotently; Worksource onboarding
validates the git/mount/profile contract; backup crosses the helper into a
temporary `MC_HOME/backups` file before rename; reset snapshots before named
volume teardown; and `doctor` turns Phase-3-owned deferred runtime, image,
setuid, helper, gateway, forbidden-env, mount, and network findings into real
checks. No section loads launchd.

## 7. Work order and directory ownership

Keep the TDD spine sequential in this order:

1. Author the delegated ADRs named in §1.
2. Add pure config, allowlist, mount, env, and network validation with table
   tests. Recommended seat: new `mc/boundary`; it has no Docker dependency.
3. Integrate the ADR-pinned host↔lock-domain planning handshake into the one
   external `mc dispatch`; prove invalid subject and subjectless paths before
   changing the resident. Extend the spawn-effect schema only after its ADR
   is accepted.
4. Add the untagged production image, fixed spine/run identity seams, named
   volume ownership, and S1 canaries.
5. Make the resident effect the frozen plan, including in-container worktree
   assignment, mount modes, explicit env, and real per-container scope.
6. Promote helper self-delegation and S2, including remote cancellation.
7. Productize gateway/network enforcement and all three egress modes.
8. Add tick-start readiness/orphan reconciliation, pre-spawn exclusion,
   normal-exit envelope cleanup, and ADR-pinned resource bounds.
9. Close onboarding/doctor/backup/reset integration, then run the complete
   boundary acceptance matrix.

Primary ownership is non-overlapping until integration: `mc/boundary` owns
pure policy and plans; `mc/verbs` owns transaction integration; `runner/image`
owns the production image; `mc/cmd` owns self-delegation; `resident` owns
runtime effects/reconciliation; the gateway owns HTTP enforcement; onboarding
and doctor consume all of them last. The Phase 3 Docker package owns assembly
tests and may share fixtures with Phase 1 only after both tag contracts remain
independent.

## 8. Test lanes and definition of done

Every green micro-step runs the existing complete fast lane:

```text
cd mc && ./check.sh
cd runner/fake-harness && ./check.sh
cd runner/agent-runner && ./check.sh
cd runner/image && ./check.sh
cd resident && ./check.sh
```

All pure boundary/config/plan/env/argv/adapter fixture tests are untagged and
therefore join that lane. On relevant commits, compile and vet the tagged
packages without starting Docker:

```text
cd mc && mise exec -- go test -run '^$' -tags docker_boundary ./...
cd mc && mise exec -- go vet -tags docker_boundary ./...
cd mc && mise exec -- go test -run '^$' -tags docker_e2e ./...
cd mc && mise exec -- go vet -tags docker_e2e ./...
cd mc && mise exec -- go test -run '^$' -tags test_fake_routing ./...
cd mc && mise exec -- go vet -tags test_fake_routing ./...
```

At Phase 3 completion, run the real `docker_boundary` suite with a timeout
long enough for S2's 30-minute stream, then re-run the existing
`docker_e2e` regression suite. Runtime-destructive canaries such as a Docker
Desktop restart remain serialized and explicit; ordinary inner loops do not
churn Docker. Record image digest, architecture, capability probes, and all
commands in `PROGRESS.md`.

Phase 3 advances only when:

- every row in §3 has a named real-mechanism test and is green;
- the production image contains no fake route and has its own untagged build;
- no production env override can redirect spine or identity;
- `doctor` has no Phase-3-owned deferred finding;
- the complete fast lane, tag compile/vet lanes, Phase 3 Docker suite, and
  existing Docker E2E regression are green;
- no host fallback, dropped rule, partial spawn, live credential fixture,
  launchd load, or session-folder cleanup occurred; and
- the ledger records the green evidence before `NEXT:` advances to Phase 4.

## 9. Explicit exclusions

The following do not become accepted by implication:

- the parked initiative-wave CLI, holistic child-plan review
  representation, and initiative shared-worktree mechanics;
- Phase 4's six timer-driven fake-harness scenario families and dashboard
  Playwright smoke;
- Phase 5 live provider no-op/tool turns, OAuth refresh canaries,
  `install.sh`, the `/onboard` skill/front door, real smoke Worksource, and
  the one operator-present launchd endgame;
- S7's operator sleep drill, real Discord, and derived per-Worksource images;
- launchd loading during development, under any name.

These exclusions may have seams or fixtures built where Phase 3 requires
them, but their later acceptance evidence remains owned by the named phase.
