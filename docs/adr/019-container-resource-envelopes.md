# ADR-019 — Finite container resource envelopes

- Status: Accepted
- Date: 2026-07-13
- Where the spec delegates: the Phase-3 handoff requires resource bounds;
  the spec fixes Docker Desktop ≥4 CPU/≥8 GiB, S1's setuid preconditions, and
  S8's browser image but supplies no per-container values or override grammar.

## Decision 1 — deployment-scoped typed classes

Resource configuration is three positive integers—never free-form Docker
flags or unit strings:

```toml
[container.resources.pipeline]
cpu_millis = 2000
memory_mib = 4096
pids = 512
```

The same fields may appear for exactly six deployment-level classes:

| Class | Shipped CPU / memory / PIDs | Allowed operator range |
|---|---:|---:|
| `pipeline` | 2000m / 4096 MiB / 512 | 1000–4000m / 2048–6144 MiB / 256–1024 |
| `homie` | 1000m / 2048 MiB / 256 | 500–2000m / 1024–4096 MiB / 128–512 |
| `helper` | 500m / 512 MiB / 128 | 250–1000m / 256–1024 MiB / 64–256 |
| `network-guard` | 500m / 512 MiB / 128 | 250–1000m / 256–1024 MiB / 64–256 |
| `setup` | 1000m / 1024 MiB / 128 | 500–2000m / 512–2048 MiB / 64–256 |
| `landing` | 1000m / 1024 MiB / 128 | 500–2000m / 512–2048 MiB / 64–256 |

Omission resolves field-by-field to these finite shipped defaults (§16.2).
Unknown classes/keys, duplicate values, zero/negative values, `-1`, strings
such as `unlimited|max|4g`, and out-of-range integers reject. There is no
Worksource/repository override in v1: Sandbox Profiles cannot turn machine
budget into jurisdiction-controlled escalation or create another config
layer. A future profile override may only tighten and requires another ADR.

The resident-hosted credential gateway is native (§12), so it has no Docker
class here. `network-guard` is only ADR-018's credential-free runtime-side
namespace/firewall/relay.

Each immutable plan carries the fully resolved tuple. The resident neither
defaults nor recomputes it. A config change affects new containers; helpers
whose frozen tuple no longer matches are replaced rather than updated.

The helper tuple is deployment bootstrap, not a dispatch effect. Darwin `mc`
parses shipped defaults plus host `MC_HOME/config.toml` before creating the
only macOS→spine crossing. An invalid or unappliable helper tuple yields a
local `doctor`/onboarding diagnostic and no weaker helper; a durable health
event can be reconciled only after a valid helper crossing is restored.

## Decision 2 — hard Docker limits

For every class the plan sets:

- `NanoCpus = cpu_millis × 1,000,000`;
- `Memory = memory_mib × 1,048,576`;
- `MemorySwap = Memory` (no extra/unlimited swap tier);
- `PidsLimit = pids`; and
- `OomKillDisable = false`.

CPU shares and memory reservations are not substitutes for ceilings. Limits
are never changed with `docker update`; a retry creates a fresh container
from its new frozen plan. Docker inspect and the runtime's kernel view must
agree. On cgroup v2 this is finite `cpu.max`, `memory.max`,
`memory.swap.max=0`, and `pids.max`; on cgroup v1 it is the equivalent finite
CPU quota/period, `memory.limit_in_bytes`, memory+swap relationship, and
`pids.max`. If neither readable view is exposed, onboarding must prove the
same controls with a bounded enforcement workload before declaring the
runtime capable.

An OOM or other runtime-observable limit termination is infrastructure
failure, never a quality verdict. PID exhaustion may surface only as
`fork/clone` `EAGAIN` and need not terminate the container; if it prevents
progress, ordinary heartbeat/deadline reaping classifies the run as
infrastructure. CPU excess is throttled and does not itself change canonical
state.

## Decision 3 — setuid-compatible security by class

| Class | Startup/live uid | Security/capability posture | Spine |
|---|---|---|---|
| pipeline, Homie, helper | `10002:10002` | `CapDrop=ALL`; `no-new-privileges` absent so setuid `mc` may reach euid 10001 | fixed named volume |
| setup, landing | `10002:10002` | `CapDrop=ALL`; `no-new-privileges=true` | absent |
| network guard | root only during trusted bootstrap, then dedicated `10003:10003` | `CapDrop=ALL`; bootstrap `CapAdd` is exactly `NET_ADMIN,NET_BIND_SERVICE,SETPCAP,SETUID,SETGID`; NNP on; readiness requires uid 10003 and all live/bounding caps zero | absent |

All classes require `Privileged=false`, default seccomp (never unconfined),
private PID/IPC namespaces, no host devices, no host or writable cgroup bind,
no runtime socket or Engine endpoint, and no host-network fallback. A
runtime-provided namespaced read-only cgroup view is permitted for inspection.
Network mode is exact: pipeline/Homie join `container:<their-guard>`;
helper/setup/landing use `none`; only the guard joins its isolated routed
bridge. No class silently receives Docker's default bridge.

No container gains a capability outside its row. The guard binary is the
static PID 1; there is no shell wrapper, Docker `HEALTHCHECK`, sidecar, or
production `docker exec` that could start a fresh root/capability-bearing
process from the container's bootstrap config. `NET_BIND_SERVICE` exists only
long enough to bind the guard's loopback DNS service on port 53 and is dropped
from the effective, permitted, and bounding sets immediately after that bind;
it is never a live-service capability. The guard self-checks that transition
before continuing. After the remaining bootstrap work it drops the other four
capabilities and privilege, enumerates every process in its PID namespace via
`/proc/*/status`, and announces readiness only when all run as uid 10003 with
zero effective, permitted, inheritable, ambient, and bounding capabilities.
Docker Engine authority can always exec by definition, so that authority
remains resident-only and is never mounted or exposed to an agent.

For pipeline, Homie, and helper, both the image filesystem holding `mc` and
the named volume must honor suid; acceptance is the stronger S1 behavior,
not a mount-option guess: agent-uid direct read/write fails while the same
operation through `mc` succeeds with the expected real/effective uids.

Setup/landing perform only the exact in-container Git plan and do not need
the spine/setuid gate. Enabling NNP there narrows a separate effect path
without breaking S1.

## Decision 4 — failure classification

The common parser runs in config load, `doctor`, onboarding, and pre-claim
planning. Resource configuration is deployment-owned and has no profile
override, so an explicit invalid/unbounded tuple is configuration health: no
claim/container and no task charge/block. The helper bootstrap has the local
diagnostic exception above until the crossing is restored.

If a valid tuple cannot be applied because the runtime lacks or loses cgroup
capability, that is deployment health, not a task defect: no new claim and no
task charge/block. An apply/inspect mismatch after a valid claim is
infrastructure failure; the container is never started without its limits.
No path silently omits, raises, or substitutes a soft bound.

## Consequences and acceptance

For every class, Docker inspect proves exact `NanoCpus`, `Memory`,
`MemorySwap`, `PidsLimit`, user, privilege, `CapDrop`, `CapAdd`, security
options, network mode, namespaces, and absence of sockets/devices. The guard's
inspected `CapAdd` is exactly the five-name bootstrap set above; every other
class has an empty `CapAdd`. The staged guard probe first proves that
`NET_BIND_SERVICE` is present in the bootstrap effective, permitted, and
bounding sets and that no capability outside the five-name set is present; it
then proves that `NET_BIND_SERVICE` is absent from every set immediately after
the port-53 bind. Final in-container `/proc/*/status` inspection proves that
all effective, permitted, inheritable, ambient, and bounding capability sets
are zero before readiness. In-container cgroup files or the accepted
enforcement probe prove the kernel resource view. The inspected tuple must
equal the immutable effect.

ADR-018's preclaim objects are not an exception: the guard uses
`network-guard`, both started capless clients and the never-started mount
canary use the selected candidate's `pipeline` or `homie` tuple, and the
Git-setup canary uses `setup`. Tests inspect every hard/resource/security field
on all five containers and prove an omitted or unbounded probe limit prevents the
probe from starting and prevents claim.

Mechanism canaries run at the **minimum** allowed envelope:

- S8 Chromium/Playwright in pipeline, non-root with the spike-accepted
  `--no-sandbox` browser flag (never a root browser or unconfined container);
- a representative pipeline launch from the final pinned agent-image digest:
  `tini` starts the production runner from its RO source mount, the runner
  starts the deterministic local fixture adapter/harness, registers the
  native-session locators, completes one top-level turn, and exits cleanly;
- a Homie lifecycle launch from that same final digest: a fresh fixture turn
  registers its native handle and trace filename, the canonical session is
  ended, and a second minimum-envelope container resumes in continue mode
  against the same mounted session folder and native handle. The resumed
  fixture emits a delayed multi-chunk second turn, whose first chunk is
  observed before completion, and appends turn two to the same native file
  rather than replacing or copying it;
- S2 five-way concurrency and 50 MiB round-trip in helper;
- gateway HTTPS/WebSocket streaming in network guard;
- representative worktree creation and merge in setup/landing; and
- S1 under final pipeline, Homie, and helper security flags.

The pipeline and Homie fixture adapters are test-only registered routes mounted
RO beside the production runner; they make no network calls, carry no provider
credential, and are absent from production routing. The image under test is
still the byte-for-byte final production digest. These canaries prove the
runner, adapter, native-session, continuation, and streaming paths without
turning a live provider call into a Phase-3 or image-minimum prerequisite.

Before any mutable Worksource is accepted, a macOS VirtioFS canary at the
final `10002:10002` uid performs create, write, fsync, atomic rename, delete,
and representative Git worktree/index/ref operations on the sacrificial bind.
S1's named-volume gate and S8's uid 1001 browser do not prove this. Failure is
a red Phase-3 mechanism line: there is no root agent, host `chown`, or
permission-widening fallback.

If a minimum cannot pass its mechanism canary, the ADR minimum is raised;
the test is never bypassed and the limit is never removed.

## Alternatives rejected

- Docker defaults, `-1`, or unit-bearing passthrough can be unbounded.
- CPU shares/memory reservations are contention hints, not hard ceilings.
- One class either starves Playwright or overallocates control helpers.
- Dynamic percentages of current VM capacity make plans non-replayable.
- Profile/repository widening creates a machine-budget escalation path.
- NNP on spine-mounting containers disables the accepted setuid gate.
- Root agent/helper, privileged mode, or a Docker socket is unnecessary.
