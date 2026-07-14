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
   side is the only place that can resolve the native file plane, load
   host-side config/routing/allowlist bytes, and observe Docker applicability;
   raw operator files never cross into the helper, only digests and a closed
   non-secret planning projection do. Dispatch requires the resident's
   one-shot inherited gateway-control descriptor; direct shell dispatch is
   refused, while every other host `mc` verb retains ordinary helper
   self-delegation. Before helper selection or mutation on **every** branch,
   the host reads the fixed safe `MC_HOME/deployment.uuid` mirror into the
   closed prepare request and the helper requires it to equal the spine
   identity; attest reopens and rechecks it. Its lock-domain side reads only canonical spine
   Worksource/Profile/Homie state and writes the consequence. Before a
   claim, the two sides must agree on one candidate-bound plan and capability
   snapshot produced by the same pure policy package. ADR-016 fixes the
   concrete self-delegation handshake, replay/candidate hash, and TOCTOU
   recheck; this contract does not pretend the warm helper can see
   unmounted host Worksource paths, and it does not give the resident a spine
   write path. A command-scoped request id and exact prepare-side result
   receipt prevent a lost response from re-entering selection after a reap,
   Homie end, recovered-health write, or other direct mutation. Attested
   commits use their separate candidate/action digest fence.
3. **The lock-domain half owns the state consequence.** After the pure
   decision selects a spawn and before `domain.Claim`, it accepts only the
   matching, fully validated host plan. Ordinary record/config/inventory
   drift is `preflight.stale`; malformed/substituted protocol evidence is an
   inert protocol error. Neither blocks, writes health, claims, or falls
   through. A successfully matched **current candidate-policy refusal**
   blocks a subject task with its stable sanitized confinement reason and
   returns no spawn effect. It creates no Run and leaves the lock free. A
   subjectless pipeline candidate cannot invent a task, so it records health.
   An invalid Homie atomically ends the launch-fenced session. If its first
   native locator never existed, ADR-016's explicit host-only `--from-rows`
   resume primes a fresh harness from a bounded deterministic conversation-row
   tail after repair; it never pretends to be native continuation. The ended
   row cannot starve later work. Deployment config or shared capability
   failure is health/no charge, not candidate policy. In particular, a
   candidate-authored profile/Worksource mount failure is policy, while a
   release runner, generated system source, clean projection, same-filesystem
   hardlink, or other shared typed-file-plane failure is deployment health and
   never charges, blocks, or ends that candidate. Landing is also attested:
   runtime, image, mount, and shared Git applicability failures are health and
   retain the pending landing; only the fixed landing program's semantic Git
   refusal blocks the task.
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

ADRs 016–019 pin the following prerequisites before production code for this
flow:

- the exact one-command host↔lock-domain planning handshake, readiness input,
  candidate/TOCTOU fence, immutable spawn-effect schema, and stable
  invalid-plan reason codes; it must cover both leased pipeline spawn and
  the lease-free, registry-driven Homie wake/resume path frozen by ADR-009
  and ADR-012, while preserving §10's fresh-held-lease early return, plus
  durable Homie launch/container fencing, resident-observed exit, exact
  pipeline/Homie/helper/setup/landing labels and liveness matching;
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

Worksource/Profile/artifact/reference writers and `homie start` run the same
worst-case plan budgeter before accepting state. The resulting catalog must
fit every pipeline role and the all-Worksource Homie projection within the
closed frame/collection bounds; onboarding/migration applies the same check
to existing state. Dispatch therefore does not discover a permanent
cardinality wedge only after otherwise valid state has been admitted.

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
Validation occurs before a pipeline lease claim and has no fallback. An
existing Homie is different: its registry-frozen historical harness/binding
resolves directly against current binding capability and never reparses or
depends on current `routing.md`; routing changes affect new sessions only.

## 3. Enforcement-mechanism acceptance matrix

Each row has one production owner and one minimum test through that owner.
Unit tests of helpers supplement but do not replace the mechanism test.

| Mechanism | Production owner | Required acceptance evidence |
|---|---|---|
| Mount source + target validation | shared `mc/boundary` policy used by profile save and both sides of `mc dispatch` | One ordinary accept and one reject for each class: symlink stays/escapes; every shipped blocked-pattern class; clean relative target vs absolute, `..`, or `:`; requested RO; RW only when request and allowlist both permit it; own Worksource vs another Worksource; ordinary root vs credential/control root. Rejection never silently downgrades or drops a mount. |
| Fail-closed plan inversion | `mc dispatch` transaction | An unappliable candidate-authored mount and an unappliable `network_allow` rule each leave zero Run rows, a free lock, no agent spawn, and the pipeline subject blocked with the exact sanitized reason. Ordinary state/config/inventory/trust staleness leaves every row unchanged and never falsely blocks. A subjectless pipeline invalid plan records health; a Homie is launch-fenced ended, including a null-locator first launch, which remains explicitly recoverable through bounded `--from-rows` resume after repair. Neither fabricates a task, lease, or Run. Same request/action replay emits no second mutation, activity, or outbox event, including a lost prepare-side response after a reap or Homie end. Temporary labeled preclaim fixtures are removed before commit or swept before later selection. Landing runtime/applicability failure records health and retains the pending tuple; only a fixed-program semantic Git refusal blocks. |
| Forbidden env | boundary env builder + `doctor` | Both harness and tool policy reject each shipped forbidden key before claim; `SENTINEL_API_KEY` proves wildcard enumeration/classification without expanding the spec's guard floor; a declared permitted tool secret survives only in its declared plane; the final Docker inspect/process env contains no forbidden key. |
| `MC_HOME` git fence | onboarding `preflight`/`home` | Existing outside-tree and ignored-in-tree accepts remain green; unignored worktree and symlink-alias bypass reject before writes. This Phase 2 implementation is promoted, not rewritten. |
| Blocked list extend-only | config/allowlist loader | Shipped floor parses; additive local entries work; omission/redefinition cannot remove a floor entry; malformed or shrinking input fails closed. |
| Routing decorrelation | routing loader + dispatch planner | Complete configured catalog resolves; missing/unknown/fake production binding rejects a pipeline pre-claim; both producer/judge pairs are different families. Existing strict routing tests remain the oracle. Separately, an existing Homie wakes/resumes under its frozen binding while current `routing.md` is malformed. |
| Setuid gate (S1 promoted) | production image + named spine volume | Under the real untagged image and real production `mc`, agent uid direct read and write both fail with `EACCES`; the same operations through `mc` succeed; ruid/euid and 0700 directory/0600 database ownership are asserted. Permanent canaries prove runc, no uid remap/ECI, `no-new-privileges` absent, suid honored/no `nosuid`, and native arm64/no emulation. Container restart, image rebuild, and volume reattach preserve the gate. |
| Per-container scope | fixed RO `/mc/run.json` + `mc` authorization | Docker proves the envelope cannot be changed or replaced and has no RW alias. The real binary enforces kernel-backed pipeline/Homie tier, role, allowlist, own/other/stale run/session/launch identity, and zero-state-change denials. Separately, env/capability tests prove runner-only lifecycle/transport verbs are not exposed to the model or subagents; this within-container same-uid grain is the spec's explicit best-effort boundary, not a claimed kernel refusal. Ordinary spine reads are Worksource-scoped; Homie alone has the explicit board-wide `mc get/list` exception. |
| Launch fencing | registry + Run receipts + runner/private lifecycle verbs + resident inventory | Pipeline create records the exact plan/container through a private time-fenced `launch-bind` immediately before start; the trusted runner repeats the lease/plan/container/grace/deadline fence before any adapter or model. Homie wake commits durable launch id/mode/cutoff-count before effect; create binds the exact Docker id/time; runner-start, locator, transport, failure, exit, liveness, and stop CAS that generation. The unstarted generation itself remains effect debt when resume debt is cleared, including lost-response/no-inbound retry. Exact-created adoption additionally requires the complete original resource/network envelope and the same live resident gateway-registration generation; resident death before start instead forces exact agent-first/guard-next teardown, confirmed absence, and whole-envelope recreation under the same launch id/mode. End→resume with an old survivor stops only the old id/label before waking the new generation; stale old exit/model/runner calls cannot end or stop it. A pre-start null-locator exit retains debt, while a normal observed started/locator-established exit ends exactly once. Row fallback captures a completed-prefix cutoff/count and fetches only its bounded newest tail. |
| Complete mount plan | boundary planner + resident effector | Docker inspect and in-container probes prove every ADR-017 typed mount's presence, fixed destination, mode, nested cover, and accessibility: isolated sanitized task-local repository/worktree, privileged completion seal, disposable Verifier source, own trace, attachments, exact role/run outputs/corrections/revisions/context/workflow records, synthetic home/cache/control overlays, resolver/CA/policy, and the same-inode one-root finalized-pipeline-trace projection. Other Worksource/session/operator credential roots, the real Git object/config/ref store, dirty primary bytes, and broad `MC_HOME` are absent. Seeding/no-worktree roles see clean pinned committed projections. Setup and landing receive their separate exact minimal tables; landing alone receives the real repository RW plus the reviewed task store RO. An unstarted token-labeled preclaim mount canary creates/inspects/removes the candidate bind set, and the shared Git-setup probe mutates only a token-derived sacrificial repository. The inspected argv/plan equals the immutable dispatch effect. |
| Gateway + three egress modes | resident-hosted gateway + ADR-pinned Docker network layer | A deterministic local authenticated upstream succeeds through injected auth and trusted CA; direct DNS and direct IP fail. A second allowed non-provider upstream and a different binding/Worksource prove the selected credential is injected only at its provider and no other binding/control dir is present. An unwired/dead gateway yields no spawn. `none` and `allowlist` each have host allow/deny probes; `open+audit` permits an arbitrary public gateway authority, admits private HTTP only through an explicit domain, and records it while its negative probe proves direct bypass/control-floor denial. Raw TCP remains independently `network_allow`-only in all three modes. |
| Warm helper (S2 promoted) | darwin self-delegation + helper lifecycle | Same-version real `mc` round-trips rc 0/1/126/127/137/143, 50 MB 8-bit stdin/stdout, separate stdout/stderr, five concurrent calls, and a 30-minute stream. INT/TERM explicitly terminates the remote process group and returns 128+signal. Kill/remove and daemon restart lazily rewarm one labeled helper. It has no run.json and therefore host scope. Dispatch additionally proves the one-use resident control channel and paged, digest-closed runtime/registration inventories beyond 256 objects without truncation or a cardinality wedge. |
| Orphan sweep | lock-domain `mc dispatch` decision + resident stop effector | Label-scoped inventory stops a pipeline container unless its run **and Worksource** labels match the live lease, and stops a Homie container unless its exact session/launch/container id and complete envelope match an active registry row. Idle age is not orphanhood: threshold equality atomically ends then exact-id stops in the dedicated branch. Unrelated containers and the no-tier helper survive; setup/landing/probe residue and closed derived file artifacts have exact component/action liveness and cleanup. At most one deterministic safety cleanup returns before Console/land/reenter and all Homie housekeeping; this logged bounded deviation prevents a stale writer from surviving. Replacement waits for a later confirmed-absence snapshot. Ambiguous stop/absence refuses replacement. Session folders and source traces are never swept. |
| Resource bounds | resident effector from resolved config | Docker inspect proves finite CPU, memory, and PID bounds, non-root agent uid, exact network mode, no privileged mode or Docker socket, and the required setuid-compatible security options. All five preclaim probe containers are bounded too: guard class, two candidate-class clients, candidate-class mount canary, and setup-class Git canary. The guard's bootstrap `CapAdd` is exactly `NET_ADMIN,NET_BIND_SERVICE,SETPCAP,SETUID,SETGID`; `NET_BIND_SERVICE` disappears immediately after port 53 bind and every live capability set is zero before readiness. Omission resolves to finite ADR-pinned shipped defaults (§16.2). ADR-019 makes overrides deployment-only, so an explicit invalid/unbounded tuple is health/no claim rather than a task block; invalid helper bootstrap is local until the crossing is restored. Final-uid VirtioFS and cgroup v1/v2-or-capability canaries are mandatory. |

`mc doctor` exercises the same production parsers and probes; it does not
carry a parallel approximation. Each failed Phase 3 check names its repairing
onboarding section. Runtime identity/version strings are not acceptance:
capabilities are.

## 4. Canonical spawn and mount plan

The immutable plan is per Worksource and per run (§11.1). It includes the
container name `mc-<worksource>-<run_id>`, `mc-managed`, `mc-tier`,
Worksource, and run labels; image digest; non-root uid; resource bounds;
network/gateway rules; explicit env; entrypoint/argv; and a digest-covered
ordered typed mount plan. ADR-017's operation-specific destination tables are
the exact authority; this condensed table fixes the plan's categories:

| Container destinations | Typed source and access |
|---|---|
| `/workspace`, `/workspace/source` | exact isolated task skeleton RO with the Worker's canonical source child overlaid RW; Packager/Refiner inherit canonical RO; Verifier overlays a disposable same-SHA source RW over a canonical RO task store; otherwise a clean committed projection RO or registered non-repository source RO; structural rootfs only when no workspace bind is authorized |
| `/workspace/source/.git`, `/workspace/source/.mission-control` | fixed relative task-local Git pointer and reserved-root cover, both RO |
| `/workspace/git`, `/workspace/git/**` | sanitized task-local Git root overlaid RW for Worker and inherited RO by later roles; generated config/hook/pack/worktree controls remain nested RO in every role; the real repository's objects, refs, config, index, and hooks are never present |
| `/workspace/artifacts/**`, `/workspace/references/**` | one allowlisted owning-Worksource source at its bilateral mode, or a declared reference RO |
| `/workspace/seeding/**` | one clean pinned committed-tree projection or registered non-repository root RO, Strategist(propose) only |
| `/workspace/operator/worksources/**`, `/workspace/operator/traces` | Homie's registered Worksource/artifact RO views with inert Git/task-root covers, plus exactly one RO same-inode finalized-pipeline-trace projection |
| `/mc/run.json`, `/mc/sandbox.json`, `/mc/session` | exact launch envelope RO, non-secret sandbox projection RO, and only the owning permanent trace folder RW |
| `/mc/completion-seal`, `/mc/attachments/{in,out}` | Worker's Docker-RW but privileged mode-0700 seal staging/publish root, usable only by setuid `mc`; the published seal becomes immutable and accepted only with completion; Homie published input RO and private setuid-published output plane; direct model access is no stronger than ADR-017 permits |
| `/mc/records/**`, `/mc/workflow/plan.js` | only exact prior inputs/current output, correction, revision, context, or adapter workflow path at the role/run-specific mode |
| `/mc/network/policy.json`, `/mc/gateway/ca.crt`, `/etc/resolv.conf` | generated policy/resolver and public CA only, all RO; no gateway secret |
| `/mc/spine`, `/app/src` | fixed named spine volume behind setuid `mc`, and exact release runner source RO |
| `/home/agent`, fixed package caches, one declared harness control path | synthetic scoped home/caches RW; canonical materialized control directory only for its selected binding as an explicit audited downgrade |

Setup and landing are separate no-agent effect classes and never inherit that
table. Setup receives only the exact operation's real source RO (when closure
extraction is required), task skeleton RO with only the operation's `source`/
`git` children RW, accepted seal, disposable/projection root, and
`/mc/setup.json`. Landing alone receives the real repository/primary
checkout RW, the reviewed sealed task store RO, and `/mc/landing.json`; its
nested `.mission-control` cover prevents a second stronger alias. Both use the
fixed image program, `network=none`, cleared/safe Git inputs, no spine, session,
HOME, control, runner, credential, or runtime socket, and ADR-019's finite
class envelope.

The mount ADR assigns collision-free, deterministic destinations for plural
artifact/reference roots and for every multi-Worksource exception; no two
host sources collapse onto one container path. No source has a second alias
with a stronger mode. `run.json`, runner source, allowlist/config, and sandbox
inputs have no RW alias. No agent container gets the runtime socket or the
operator's real HOME. An ordinary run gets no other Worksource root and no
other run's session bytes. Session folders persist forever; the envelope and
sandbox projection do not.

For a standalone task, the canonical retry-safe store is
`<workspace_root>/.mission-control/tasks/task-<id>/{source,git}`. Trusted setup
copies only the complete object closure reachable from the pinned base/target
SHA into the isolated task-local repository—never a local-clone hardlink,
alternate, or the real object database—and creates relative worktree links so
the operator can inspect the host path with read-only Git commands. The real
primary checkout and its staged, stashed, dangling, dirty, and untracked bytes
remain absent from every pipeline agent.

Worker alone mutates the canonical local branch/worktree. Immediately before
completion's state transition, trusted `mc` validates it and writes a
privileged immutable closure pack/manifest seal whose identity and digest are
accepted with the completion. Correction Worker is rebuilt/reconciled from
that exact accepted seal. Verifier receives the sealed canonical task store RO
and an execution-scoped same-SHA source RW; its trusted one-phase verdict gate
checks the disposable tracked tree against the seal, and later writes cannot
alter canonical bytes. Packager/Refiner receive only the sealed view RO. No
downstream role starts until the accepted seal exists and the producer
envelope is confirmed absent.

The resident effects these closed operations through short-lived setup
containers; Mission Control never invokes operator-installed host Git. Setup
failure after a valid claim is infrastructure failure, not permission to bind
the primary checkout. Landing alone validates and imports the exact reviewed
closure into the real repository, CAS-creates the real `mc/task-<id>` ref, and
performs the required `merge --no-ff` in the primary checkout. The canonical
pending/action row plus exact import/ref/merge topology makes every crash cut
idempotent; only after recorded success may exact cleanup remove the local ref,
task root, and seal. The pre-landing task-local ref is the explicit logged
§6.2 deviation chosen to preserve §5's stronger committed-state invisibility.

Git seeding views are short-lived clean committed-tree projections created at
pinned SHAs and mounted RO, not the live primary checkout; uncommitted or
untracked operator bytes are therefore invisible.
The parked initiative wave/shared-worktree representation is not inferred.
Strategist(propose)'s cross-Worksource seeding plan is the spec's enumerated
RO filesystem exception: it receives deterministic, distinct RO workspace
mounts, but that does not silently grant board-wide `mc get/list` access.

A Homie uses a separately planned, lease-free `mc-tier=homie` effect selected
from active registry/pending-turn or explicit-resume debt only after the
pipeline lease is free; it never holds that lease. Its
operator profile receives distinct RO mounts for every Worksource workspace
and artifact root plus board-wide scoped `mc get/list`; every descendant Git
administrative identity is hidden by a nested inert cover, while a bare or
otherwise uncoverable Git shape uses a clean projection at the same logical
destination. Its own session folder is the only RW trace destination.
Finalized writer-closed **pipeline** session
files appear through one RO operator projection root. Every derived entry is
a same-filesystem hard link that must prove `os.SameFile` with the immutable
spine-located source; there is no copied trace, symlink, active writer, or
source-folder bind, and the root is reconciled by polling. Other Homie native
traces are excluded because they may become writable on resume and unlink
cannot revoke an open fd; their durable visible history is the conversation
rows. Its stable per-session attachment input root is RO and output root RW,
so new references appear without remounting. Inbound file publication first
durably enqueues the stable surface message/body and complete bounded
attachment manifest; only the oldest queued row gets an expiring publication
attempt, so concurrent websocket events are not left in native memory.
Outbound seals the same complete byte-credit manifest under the runner's live
turn claim before staging. Content-addressed objects retain
spine liveness edges for every bound/materialized/consumed publication, and an
exact `delete_pending` CAS prevents dedup reuse from racing cleanup. Each
publisher has a short lock-domain owner lease, three failed ingress attempts
produce a surface-visible terminal receipt so the queue advances, and terminal
files are removed only after a durable `cleanup_pending` transition.
Start/resume still returns record
truth only (ADR-009/012); the later tick selects, persists a launch generation,
and validates the Homie wake effect. Null-locator recovery is the explicit
`--from-rows` resume mode, never an implicit native fallback. Its snapshot is
a completed-prefix cutoff/count and a bounded O(tail) fetch; the empty prefix
is exactly `(through_seq=0,row_count=0)` and emits no omission marker before
the original pending inbound is claimed.

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

The parsed native gateway listens only on the host loopback address; the
isolated guard reaches that listener through Docker Desktop's host gateway,
authenticates with a one-launch stdin-delivered credential, and is the only
container uid permitted to do so. Every pipeline/Homie gets one isolated
guard network namespace. Agent and setuid-`mc` traffic are separated by
`meta skuid` nftables rules, Docker DNS is denied, the guard serves the fixed
resolver, and readiness requires all five bootstrap capabilities dropped from
every live set after port-53 bind and firewall setup. Resident restart loses
the process-local registration generation, so old guards are dismantled and
the complete envelope is recreated rather than adopted.

HTTP and raw TCP are separate planes. Provider endpoints are always the
minimum HTTP set. `none` adds nothing, `allowlist` adds only configured
domains, and `open+audit` admits arbitrary public HTTP plus explicitly named
private domains while logging every authority; the non-removable
loopback/metadata/control-address floor is logged as a conservative spec
clarification. None permits direct proxy bypass. Raw TCP admits only successfully
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
socket. It has `mc-managed=true,mc-component=helper` and no `mc-tier`, and
mounts no host config/routing/allowlist/credential root. Any invoker may
lazily recreate it; the resident also ensures it at boot and every tick.
There is no host-file spine fallback. The one exception to ordinary invoker
shape is `mc dispatch`: only the resident may call it in production, supplying
ADR-018's inherited one-use gateway-control fd for secret-free
registration inventory, binding/CA/matcher attestation, and probe registration. That fd never reaches
the helper or an agent; a direct Darwin shell dispatch is refused.

The resident begins each tick by ensuring the helper where possible and
collecting the bounded prepared readiness/inventory observation: exact managed
container identities/config digests/labels and closed Docker state
(`created|running|paused|restarting|removing|exited|dead`), exact live guard
registration generations from the resident's in-memory table,
capability epochs, and closed derived ephemeral artifacts, never volatile
uptime/counters or session-tree scans. Runtime, derived-file, and gateway
registration inventories are externally sorted/merged in bounded memory and
streamed in 128-item digest-closed pages with per-page no-progress and
count-derived finite deadlines; they have no fixed cardinality cutoff. After
native attest/probes and before commit, the broker freshly re-enumerates and
requires the digest/count/candidate evidence to match the prepared observation;
the tick-start view alone is never a TOCTOU authorization. The one
`mc dispatch` call decides at most one action in ADR-016's exact order:
pipeline lease fresh/reap; recovered-crossing health; one deterministic
orphan/ephemeral cleanup; due Console, attested landing, or reenter while
retaining an ordinary pipeline candidate/all-saturated result; one Homie
launch/exit reconciliation; one idle Homie end; then the lease-free Homie
wake-versus-retained-pipeline choice. A matching prior Homie-only preflight
health marker gives the retained pipeline candidate one unconditional turn,
so broken Homie setup cannot starve the pipeline tier. The
resident then effects it mechanically. Pipeline liveness is the live global
lease. Homie liveness is the active row's exact current launch and Docker id;
idle age is handled only by the atomic idle-end branch. An uncertain container
stop is not absence: paused/restarting/removing objects remain non-replaceable
through exact cleanup, and exited/dead objects are removed before confirmed
absence. Resident-observed normal exit invokes the private
launch-fenced end; a stale old-launch exit cannot end a resumed generation.
Every prepare-side mutation records its exact command request/result receipt,
and every attested action uses its canonical action key, so response loss
cannot fall through to a second branch. Setup and landing survivors observed
on a later single-flight tick are always residue and are stopped before retry;
their foreground deadlines are bounded by the landing cap or the remaining
pipeline/Homie start window.
Normal cleanup removes only exact ephemeral launch/setup/landing/network
artifacts; retained attachments, authored records, session folders, and native
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
8. Add tick-start readiness inventory plus lock-domain orphan/idle
   reconciliation, pre-spawn exclusion,
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
