# ADR-018 — Per-launch network guard and resident-owned egress gateway

- Status: **Superseded by ADR-022 (2026-07-21).** The free-internet requirement
  deletes the egress gateway/proxy as a network control; the credential boundary
  is now a resident-hosted token service (access-in/refresh-out). Read ADR-022
  and §11.4's 2026-07-21 amendment first. This ADR is retained for history only —
  do not implement D1–D9.
- Date: 2026-07-13
- Where the spec delegates: §11.4 fixes two independent egress planes,
  three HTTP modes, resident-held runtime auth, raw host:port allowances, and
  fail-closed applicability. It does not fix Docker Desktop enforcement,
  DNS-rebinding posture, policy grammar, launch authentication, or audit
  ownership. S4 proves the CA/header-injection and internal-network relay
  shape; ADR-016 fixes the candidate-bound preclaim handshake.

## Context

Proxy environment variables do not prevent direct sockets, and Docker's
`--internal` network cannot also provide arbitrary raw host:port access.
Conversely, giving an agent firewall capability lets it remove the policy.
The boundary needs one network namespace whose policy is installed by a
short privileged bootstrap and then used by an unprivileged agent.

The native gateway must not become a LAN proxy. A 2026-07-13 capability probe
on the primary Docker Desktop runtime proved that a cached Alpine container
can reach an HTTP server bound only to host `127.0.0.1` through
`host.docker.internal`. That loopback-only topology is a permanent onboarding
and Docker-suite canary; wildcard host binding is not a fallback.

## Decision 1 — one guarded namespace per pipeline run or Homie launch

Each pipeline run and each Homie `session_id + launch_id` gets one native-arm64
`mc-netguard` container. The guard alone joins an isolated routed Docker
bridge. The agent joins its network namespace with
`NetworkMode=container:<guard-id>` and has no independent endpoint. PID,
mount, user, IPC, and capability namespaces remain separate.

The static guard binary is PID 1 and starts as root with only
`NET_ADMIN,SETPCAP,SETUID,SETGID,NET_BIND_SERVICE`, never privileged. These
are bootstrap capabilities, not the guard's live posture. In this order it:

1. receives a one-shot launch credential as described in Decision 6;
2. resolves `host.docker.internal` through the guard container's Docker
   bootstrap DNS and pins the complete answer set plus the resident's current
   gateway port;
3. binds loopback DNS port 53 and fixed loopback proxy ports while still root,
   then immediately removes `NET_BIND_SERVICE` from its effective, permitted,
   inheritable, ambient, and bounding sets;
4. atomically installs the exact IPv4/IPv6 nftables policy and digest while it
   still holds `NET_ADMIN`;
5. drops supplementary groups, uid/gid 0, and all remaining effective, permitted,
   inheritable, ambient, and bounding capabilities; and
6. enumerates `/proc/*/status` and becomes ready only when every process in its
   PID namespace is uid/gid 10003 with all capability sets zero.

There is no shell wrapper, sidecar, Docker `HEALTHCHECK`, or production
`docker exec` into the root-configured guard. The resident retains Engine
authority but never exposes it to an agent.

The output policy is default-drop and every owner predicate is an nftables
`meta skuid` match against the socket's kernel credential. It never infers an
owner from a process listing, a userspace-supplied uid, or the packet payload:

- uid 10003 (the live guard) may connect only to the freshly pinned
  `host.docker.internal` address and native gateway port; it cannot use raw
  allowances;
- uid 10002 (agent/runner/harness/tools) may connect on loopback only to the
  guard's exact proxy TCP ports and its `127.0.0.1`/`::1` UDP+TCP DNS port,
  plus the plan's exact raw destination-prefix + TCP-port tuples;
- euid 10001 (setuid `mc`) and every other uid get no network egress at all;
  setuid spine authority never becomes network authority.

Established replies and the minimum matching input traffic are admitted. UDP
outside the local DNS service, QUIC, ICMP egress, undeclared IPv6, public DNS,
and all other loopback ports are denied. Docker embedded DNS
`127.0.0.11:53` is denied over both UDP and TCP for every live uid. Tests probe
all three uids independently; a positive for one is a negative for the other
two.

Socket ownership is fixed when the socket is created. To prevent a setuid
invocation from borrowing an already-open uid-10002 socket, `mc` in container
setuid scope first requires descriptors 0–2 to be the expected non-socket
pipe/terminal/file classes, closes every descriptor above stderr, and only
then parses a command; any socket it subsequently opens has euid/fsuid 10001.
The trusted runner applies the same inherited-descriptor check and close before
it starts a harness or makes model tools available. The Darwin dispatch broker
is the sole exception: it validates and retains fixed descriptor 3 only for
Decision 6, marks it close-on-exec immediately, and closes every other
inherited non-stdio descriptor. It never passes descriptor 3 to the container
runtime. Guard stdin is closed after the startup frame. Thus neither a model
nor setuid `mc` can retain a socket or host-control descriptor created under a
more permissive identity. Capability probes assert the kernel's `skuid`
behavior, including attempted inherited-socket sends through an extra fd and
through stdio under setuid `mc`.

The guard mounts no Worksource, spine, session, runtime control directory, CA
key, injection table, or runtime socket. It receives only its non-secret policy
projection and resolver inputs plus the private one-shot launch credential.

## Decision 2 — DNS and canonical raw-rule storage

The profile gains `egress_allow TEXT NOT NULL DEFAULT '[]'`, a canonical JSON
array of exact domain names for HTTP policy. Names have no wildcard, scheme,
path, userinfo, port, or IP literal; IDNs normalize to lowercase A-labels and
one trailing dot is removed before duplicate checks.

Existing `network_allow` becomes a canonical JSON array:

```json
[{"target":"db.internal.example","port":5432},
 {"target":"10.42.0.0/24","port":443}]
```

`target` is one normalized exact DNS name or canonical IPv4/IPv6 CIDR; `port`
is an integer 1..65535. Unknown keys, duplicates, wildcards, ranges, schemes,
URLs, and firewall fragments reject. Profile save validates syntax. Spawn uses
the production host resolver and freezes every usable A/AAAA answer. Tests use
an explicit constructor-injected local resolver; no env switch or private-IP
exception exists in the production binary.

The guard serves a non-recursive DNS view on `127.0.0.1`/`::1` containing only
the frozen hostname answers from `network_allow`. The agent receives a
generated RO `/etc/resolv.conf` naming only that service. Docker documents
that `container:` network mode disallows per-agent DNS/extra-host options, so
the RO resolver bind is a permanent Docker Desktop canary. The guard—not the
agent—resolves `host.docker.internal` before firewall installation; it is
re-resolved and re-attested for every launch because Docker's host-gateway IP
may change.

The local DNS service never re-resolves. An active run therefore cannot be
rebound; the next launch resolves afresh. A zero/mixed-forbidden answer set or
unsupported family rejects the candidate. HTTP proxy clients do not need
container-side origin DNS.

Raw policy is TCP only. An explicit raw rule to a non-control destination on
port 53 permits that exact DNS-over-TCP socket; UDP DNS remains denied. The
Docker embedded resolver is a non-removable control endpoint even if named in
a rule.

## Decision 3 — exact destination floor

Every DNS answer and every address in a candidate CIDR is normalized before
policy comparison. IPv4-mapped `::ffff:0:0/96` is reduced to IPv4. The
well-known `64:ff9b::/96` and local-use `64:ff9b:1::/48` NAT64 forms are
evaluated against their embedded IPv4; any other transition form whose
effective destination cannot be determined unambiguously rejects.

The non-removable floor is:

- IPv4 `0.0.0.0/8`, `127.0.0.0/8`, `169.254.0.0/16`, `224.0.0.0/4`, and
  `240.0.0.0/4` (including unspecified, loopback, link-local/metadata,
  multicast, reserved, and limited broadcast);
- IPv6 `::/128`, `::1/128`, `fe80::/10`, and `ff00::/8`;
- Docker embedded DNS `127.0.0.11`, current bridge gateways, runtime/Engine
  endpoints, the resident gateway/admin endpoints, and every Mission Control
  control address discovered by the launch capability snapshot.

The one exception is guard uid 10003's exact pinned host-gateway address and
gateway port; the agent never receives that exception. A CIDR intersecting any
floor range is rejected whole—there is no subtraction. A hostname with even
one forbidden answer is rejected whole. RFC1918, unique-local, and CGNAT
addresses are not blanket-forbidden: the raw plane may reach them only through
an explicit rule. Tests pin first/last-address intersections, mixed DNS
answers, mapped/NAT64 forms, and current Docker-control endpoints.

## Decision 4 — the native HTTP gateway is parsed, loopback-only, and never a raw tunnel

The resident binds an OS-assigned port on host loopback only. The guard reaches
it through the freshly resolved Docker Desktop host gateway. If the permanent
loopback canary fails, onboarding/doctor is red; Mission Control never binds
`0.0.0.0`, a LAN address, or a Docker socket as a fallback. Every connection
also presents the launch credential before any proxy bytes are accepted.

The agent sees only guard-loopback proxy/base-URL coordinates. The guard relays
authenticated framed traffic to the native gateway and strips launch framing
from origin traffic. It cannot choose a different upstream.

The native gateway accepts parsed HTTP/1.1, HTTP/2, recognized WebSocket
upgrades, and TLS intercepted with the Mission Control CA. Standard HTTP
`CONNECT` is accepted only to begin interception: the gateway validates the
authority, returns success, requires a timely TLS ClientHello, requires SNI to
match CONNECT and later HTTP authority, and terminates TLS itself. Non-TLS
CONNECT, missing/mismatched SNI or authority, CONNECT-UDP, SOCKS, opaque data,
and interception failure reject; there is never a blind-tunnel fallback.

Modes are:

- `none`: only the selected provider's versioned authorities;
- `allowlist`: provider authorities plus `egress_allow` domains;
- `open+audit`: arbitrary public HTTP domain or public IP-literal authorities,
  plus private-domain authorities only when explicitly named in
  `egress_allow`.

The exact address floor still applies after gateway resolution. Thus an
`egress_allow` domain may deliberately resolve to RFC1918/ULA but never to a
control-floor address. `egress_allow` affects `allowlist` and supplies the
private-domain exception in `open+audit`; it adds nothing in `none` beyond
provider authorities.

For every upstream connection the gateway normalizes authority, resolves once,
rejects a mixed forbidden answer set, and dials the already-checked address
while preserving the original host for SNI/authority. The dialer cannot look
up again. Redirects and reconnects repeat the check. Public IP literals are
accepted only in `open+audit` and are audited; direct IP bypass from the agent
remains denied unless independently present in `network_allow`.

This floor conservatively clarifies §11.4's literal `open+audit: everything
passes`: metadata, loopback, and Mission Control control endpoints never pass,
and private HTTP requires explicit domain intent. The deviation is recorded in
`IMPLEMENTATION-NOTES.md`.

## Decision 5 — exact credential injection scope

The native resident alone owns the CA private key and static-token table.
Containers receive the CA certificate, proxy coordinates, and no key or static
credential.

For MiniMax, the S4-proven reverse/base-URL leg authenticates the launch,
matches selected binding and exact execution identity, provider authority and
port, and allowed path prefix, then removes inbound `Authorization` and
`x-api-key` before installing the real bearer. Pipeline identity is
`{run_id,worksource,binding}`; Homie identity is
`{session_id,launch_id,binding}`. No token is injected for another tier,
binding, Worksource, session, launch, host, port, forward-proxy leg, path, or
failed-policy request.

Codex/Claude remain `materialized`: only the selected canonical control
directory is mounted RW. Traffic still crosses the same gateway policy, but
the gateway does not copy, replace, or log OAuth headers. Missing static auth
never falls back to materialized/API-key delivery; missing OAuth state never
falls back to gateway/API-key delivery.

Production logs never contain credentials, authorization headers, launch or
probe nonces, queries, bodies, supplied secret prefixes, or runtime-control
contents. S4's redacted-prefix evidence logging does not ship.

## Decision 6 — one-use resident control, launch credentials, and registration lifecycle

The native gateway lives inside the resident, while ADR-016's Darwin
`mc dispatch` broker is a short-lived child process. For each resident tick the
resident creates one connected local `AF_UNIX` stream socketpair and passes the
child endpoint as fixed descriptor 3 with explicit descriptor inheritance.
There is no socket path, listener, reconnect, environment selector, command-line
token, or durable control record. The child marks descriptor 3 close-on-exec at
entry and closes it before returning; Docker/runtime subprocesses cannot
inherit it. The resident closes its endpoint when that one dispatch finishes.
This channel does not cross into the lock domain and carries neither an `mc`
verb nor spine data; it exists only because the resident-owned gateway must
attest and temporarily register the preclaim proof that the Darwin broker
performs.

Before `__dispatch-prepare`, both ends exchange a closed `hello`/`hello_ack`
containing the exact release build id, gateway-control protocol version,
spine/config schema versions, and MC_HOME deployment UUID. Every value must
match. Frames are a four-byte big-endian length followed by the same
map-free, closed canonical encoding rules as ADR-016; a frame is at most 64
KiB, non-inventory operations carry at most 16 frames, sequence numbers are monotonic, and
unknown operations/fields, trailing bytes, oversized strings, duplicate
sequences, or early EOF close the channel and refuse the candidate. The hello
deadline is two seconds, each expected request or response has a five-second
no-progress I/O deadline. After both registration-inventory sides announce
their finite counts, the one-use channel wall allowance is
`120 seconds + 5 seconds * total_page_count`; there is no fixed total deadline
that turns a sufficiently large but progressing finite inventory into a
permanent cleanup wedge. Non-inventory work, including the Docker probe,
remains within the base 120 seconds. Timeout closes the channel and revokes its
registrations. After the
handshake the only request operations are
`registration_inventory`, `binding_attest`, `probe_register`, `probe_revoke`,
and `close`.

`registration_inventory` streams sorted pages of at most 128 exact managed
guard identities and `mc-gateway-registration` generations observed by the
broker, with monotonic cursor, total count, rolling digest, and explicit EOF.
The resident returns a corresponding page for each and, at EOF, paged
secret-free descriptions of any extra in-memory rows. For each item it returns
only whether an exact live
in-memory row exists plus a digest of that row's non-secret
`{plan_digest,tier,execution_identity,binding,gateway_handle,generation}`
projection. Missing, extra, or mismatched rows are explicit; the response can
never reconstruct a credential. This one pre-prepare exchange becomes the
guard-registration portion of ADR-016's stable runtime snapshot. Each page
obeys the ordinary 64-KiB frame cap, but there is no fixed total-item or page
cap; count/digest/cursor disagreement and incomplete EOF refuse. Thus
registration residue cannot cross a cardinality threshold that prevents the
cleanup scan intended to remove it.

The broker obtains this ordering with ADR-016's bounded-memory, bounded-fan-in
external sort; the resident's in-memory registration table is emitted through
fixed 128-item sorted runs and the same merge discipline rather than copied
into a second unbounded array. Each side retains only the rolling digest,
cursor, deterministic best mismatch, and one bounded merge window. Tests use
multiple pages in both directions and force a count whose count-derived
allowance exceeds the old 120-second ceiling.

`binding_attest` supplies the preparation token, execution identity, selected
binding, declared auth-delivery mode, normalized provider authorities/ports
and path prefixes, HTTP policy digest, and expected deterministic gateway
handle. The resident checks those values against the currently loaded gateway
configuration and returns a secret-free attestation containing the handle,
selected binding and delivery mode, public CA certificate fingerprint and CA
generation id, normalized matcher projection digest, policy compiler version,
and a ready/refused code. The matcher digest covers only authority, port,
path, tier, execution-identity, and delivery predicates; it never hashes a
credential-bearing row. The response proves that the selected MiniMax matcher
can be compiled for the exact candidate, not merely that a generic gateway is
listening. ADR-016 includes the response digest in the host attestation and
final plan digest and requires the selected binding and delivery mode to match
at commit.

No control frame contains a runtime credential, OAuth/control-directory
content, CA private key, static-token table, launch nonce, raw probe nonce, or
injection-table contents. For a probe, the broker generates a distinct 32-byte
random nonce locally, keeps the raw bytes in locked non-dumpable memory, and
sends only
`SHA-256("MC-PROBE-VERIFIER-V1\0" || nonce)` in `probe_register`. The resident
registers that verifier in memory against the preparation token, plan digest,
gateway-attestation digest, and a resident-selected deadline no more than 30
seconds away. It returns only a non-secret probe handle and deadline. The
gateway hashes the nonce presented by the temporary guard and compares it to
the verifier; this probe registration selects only the local fixture policy
and can never select a runtime binding or credential-injection matcher. The
broker supplies the raw nonce only through the probe guard's attached stdin,
then zeroes it.

`probe_revoke` is idempotent and required before a successful commit, stale
return, or classified refusal. EOF/child exit causes the resident to revoke
every probe registered by that socket immediately. Independently, the gateway
sweeps expired registrations on its own timer and before every registration,
so a broker crash after registration but before container creation cannot
leave an accepted verifier indefinitely. A resident restart drops the
in-memory table and therefore invalidates all registrations. Labeled runtime
objects are a separate cleanup concern handled by Decision 8; registration
expiry does not pretend to remove Docker objects.

On Darwin, ordinary `mc dispatch` requires this valid inherited channel and
completes the handshake before preparing or mutating state. A direct operator
shell invocation has no descriptor and is refused; no environment variable or
filesystem socket can opt it in. On native Linux the resident calls the same
gateway interface in process, with the identical typed requests and checks but
without a socketpair.

After a successful ADR-016 commit and before an actual guard is created, the
parent resident's in-process gateway generates the runtime launch credential
and registers it against
`{plan_digest,tier,execution_identity,binding,gateway_handle}`. The immutable
effect carries only the deterministic handle/reference, never the credential.
The Darwin broker is already gone from this path. Immediately before
registration the resident recomputes the secret-free binding/CA/matcher
attestation and requires exact equality with the committed plan; drift refuses
guard creation as post-commit infrastructure rather than silently wiring the
new gateway state.

At resident start the gateway also creates one process-local locked boot key
and a monotonic registration counter. For each runtime registration it derives
`generation = HMAC-SHA-256(boot_key,
"MC-LAUNCH-REGISTRATION-V1\0" || gateway_handle || plan_digest || counter)` and
uses the full lowercase-hex digest as a non-secret attestation value, never as
authentication. This is a registration-generation attestation, not a runtime
object handle; every object handle/name remains the deterministic value from
ADR-016. The gateway stores the generation with the in-memory row and the
resident places it in the guard's exact `mc-gateway-registration` label before
create. The generation may appear in inspect and the secret-free runtime
snapshot, but not in agent env/argv or a durable control file. A restarted
resident has neither the boot key nor the row, so it cannot attest an old
guard even if all Docker labels still look structurally correct.

The runtime credential is exactly 32 random bytes. The resident streams one
bounded binary startup frame over the newly created guard's attached stdin
into guard-private `mlock`ed memory before readiness; the guard sets itself
non-dumpable and closes stdin after that frame. It writes no credential file.
The bytes are absent from Docker env, argv, labels, inspect, host files,
plan/projection bytes, and logs. The guard never exposes them to the agent's
mount or process namespace. Normal cleanup, failed start, orphan cleanup, and
gateway restart zero memory and revoke the registration. Reuse against another
plan/identity rejects.

## Decision 7 — audit ownership

The gateway owns host-only mode-0600 JSONL files outside every mounted root:

- pipeline: `MC_HOME/egress-audit/<run_id>.jsonl`;
- Homie: `MC_HOME/egress-audit/homie-<session_id>-<launch_id>.jsonl`.

These are non-authoritative evidence; dispatch never reads them. Before first
dial to an authority, the gateway appends and `fdatasync`s only execution and
binding ids, mode, normalized authority/port, pinned address, protocol class,
static-injection boolean, timestamp, and decision. Append/sync failure refuses
the dial in every mode.

At registration it appends `kind=declared_raw_allowance` for each normalized
raw rule and frozen hostname set. That is rule-level evidence only; the guard
does not claim per-connection raw audit. Health reads standing policy from the
spine, never these files.

## Decision 8 — mandatory preclaim proof and failure classes

Every spawn/wake candidate gets exactly one temporary guard and two separate
capless started clients, plus the never-started mount canary and the Git-setup
canary below: five containers total. The guard alone joins a deterministic
isolated bridge. Each network client uses
`NetworkMode=container:<probe-guard-id>` and shares only the guard's network
namespace; PID, mount, user, IPC, and cgroup membership remain separate.
Neither client mounts a Worksource, session, spine, runtime-control directory,
host socket, CA key, or injection data.

For preparation token `<p>`, names are deterministic from its first 12
lowercase hexadecimal characters:

- `mc-probe-guard-<p12>` — the exact candidate guard image digest, resource
  envelope, bootstrap capabilities, security flags, resolver projection,
  gateway endpoint, public CA, and candidate policy compiler;
- `mc-probe-agent-<p12>` — uid/gid 10002 with `CapDrop=ALL`; and
- `mc-probe-mc-<p12>` — starts as uid/gid 10002 with `CapDrop=ALL` and invokes
  the baked real setuid `mc`, whose private fixed network-proof path runs as
  euid/fsuid 10001 and accepts no destination arguments;
- `mc-probe-mount-<p12>` — ADR-016's never-started exact candidate mount
  application/inspect canary; and
- `mc-probe-git-setup-<p12>` — the fixed setup image exercising repository
  extraction, object-format, relative-worktree, index, and ref operations only
  against a token-derived sacrificial repository/tree under the probe root.
  It never mounts or mutates the real target repository before claim.

Every probe container has ADR-019 hard limits. The probe guard uses the resolved
`network-guard` tuple; both started `agent` and `mc` clients use the selected
candidate's resolved `pipeline` or `homie` tuple; the never-started mount
canary carries that same candidate tuple; and the Git-setup canary uses the
resolved `setup` tuple. Creation and inspect must prove exact `NanoCpus`,
memory, no-extra-swap, PID, OOM, and class security fields before any started
probe runs. A missing, zero, unbounded, substituted, or inspect-mismatched
limit is deployment health and triggers the same exact cleanup before claim.

The bridge, all five containers, generated resolver/probe projections, and
any other temporary object have deterministic token-derived names. Every
label-capable runtime object carries
`mc-managed=true,mc-component=boundary-probe,mc-prepare-token=<p>` plus a
closed
`mc-probe-role=network|guard|agent|mc|mount|git-setup`; none carries
`mc-tier`.
Generated files live only under the exact token-derived probe-projection root,
and their closed manifest embeds the same component, preparation token, and
role rather than pretending ordinary files have Docker labels. The private
setuid proof reads only a root-owned RO fixed-schema probe projection at its
fixed path; that projection and private path are absent from production agent
containers, and the proof opens no spine. The mc client deliberately permits
setuid (`no-new-privileges` is off) but still has no capability; both clients
close unexpected inherited descriptors before testing.

Remote service liveness is not a spawn prerequisite. The probe guard installs
the candidate's production nftables chains byte-for-byte and inspects their
canonical digest for every declared tuple. To exercise a positive raw match
without contacting a declared remote service, probe mode also creates a
non-routed local dummy interface, an integrated TCP echo listener, and a
separately named `mc_probe_fixture` chain. That single exact TCP tuple is
emitted by the same rule compiler and match template as a raw allow rule, but
has a separate probe digest and is not part of the candidate policy digest.
The production subgraph and probe-only subgraph have distinct canonical names
and inspection digests. The final guard plan contains neither the fixture
address, listener, nor fixture subgraph. Tests compare the production subgraph
against the final plan and the fixture subgraph against the independently
compiled synthetic rule, so a functional positive cannot mask a changed or
missing candidate rule.

The topology proves, before claim:

- guard uid 10003 reaches only the authenticated loopback native gateway;
- the capless uid-10002 client reaches local proxy/DNS and the controlled raw
  fixture, while direct DNS, undeclared IPv4/IPv6, UDP/QUIC, and wrong ports
  fail;
- the setuid-`mc` client, including an attempted inherited uid-10002 socket,
  reaches neither gateway, proxy/DNS, nor raw tuples as euid/fsuid 10001;
- the resident-control binding attestation, gateway challenge, public-CA
  HTTPS, HTTP authority policy, resolver view, `meta skuid` nftables digest,
  exact candidate-chain digest, bootstrap capability sequence, final live
  uid/capability posture, every probe's ADR-019 hard-resource tuple, and
  generated resolver bind all match; and
- clients are stopped and confirmed absent before the guard; then the guard,
  never-started mount canary, Git-setup canary, bridge, and projections are
  removed and confirmed absent; then the probe registration is revoked and
  confirmed absent.

Any failed cleanup, ambiguous container/network absence, or lost control
channel is deployment health and prevents claim. If the broker dies, the next
tick sweeps temporary objects by the exact component+preparation-token labels,
clients first; Decision 6 independently expires the in-memory verifier even
when no Docker object was ever created.

No Phase-3 probe spends a real credential. A production candidate cannot skip
this fixture merely because a read-only policy calculation succeeded.

Failure classes are exact:

- **Deployment health/no claim or task charge:** runtime/image unavailable;
  missing nf_tables or inability to create any guard; shared DNS/guard binary
  failure; loopback host-gateway canary failure; globally dead/misconfigured
  gateway or CA; missing/mismatched resident-control descriptor, build,
  schema, protocol, or deployment UUID; expired/failed probe registration; no
  cgroup/network capability.
- **Current candidate policy:** malformed/unresolved rule, forbidden/mixed
  destination, unsupported requested family, candidate-specific compiled-rule
  refusal, exact binding/delivery/CA mismatch, or candidate policy-digest
  mismatch. ADR-016 blocks a subject task, records subjectless health, or
  applies its exact first-launch/established Homie consequence without a
  claim.
- **Post-commit infrastructure:** a previously proved runtime changes, actual
  guard cannot reach ready, plan/inspect mismatch, or guard/gateway is lost.
  Pipeline follows spawn-grace/reap; Homie follows its fenced end transition.

## Decision 9 — guard loss and exact reconciliation

If guard PID 1 dies, its proxy and DNS services disappear, but nftables in the
shared namespace can remain while the agent holds it. An already-running agent
may therefore open **new** raw TCP connections to still-declared tuples, and
existing allowed raw flows may continue, until that agent is stopped; the ADR
does not claim immediate fail-closed raw revocation on guard death. New proxy
and DNS traffic fails because their listeners died. The missing
container/readiness registration makes the agent envelope invalid at the next
tick. Reconciliation selects the agent stop first, confirms absence on a later
snapshot, then removes guard/network/registration artifacts. It never starts a
replacement beside an ambiguous survivor.

Actual guards carry `mc-managed=true,mc-component=network-guard`, their
`mc-tier=pipeline|homie`, and exact pipeline run+Worksource or Homie
session+launch labels. Resolver files, networks, registrations, and projections
carry the same execution identity. Boundary probes have no tier; helpers have
no tier. Session folders are never cleanup targets.

## Consequences and acceptance

Named Docker tests prove:

- all three HTTP modes, explicit-private-domain behavior, public IP literals
  in open+audit, HTTPS/HTTP2/WebSocket MITM, and strict CONNECT rejection;
- exact MiniMax dummy-auth scope across second origin, binding, Worksource,
  run/session/launch, path, bad credential, and OAuth leg, with secret-free
  audit/logs and pre-dial audit failure;
- hostname/port and CIDR positives, wrong host/port negatives, floor
  intersections, mapped/NAT64 cases, in-run rebinding resistance, next-launch
  re-resolution, explicit raw TCP 53, and unconditional Docker-DNS denial;
- exact `meta skuid` separation for 10003/10002/10001, including inherited
  uid-10002 socket refusal through an extra fd and redirected stdio after
  setuid `mc`, runner/harness startup with no unexpected inherited descriptor,
  and agent `NetworkMode=container:<guard>` with no independent
  endpoint/capability; exact
  guard labels/rules/digest, bootstrap's exact five-capability allowlist,
  successful port-53 bind before `NET_BIND_SERVICE` drop, and final all-process
  uid/cap shedding, plus the RO resolver canary;
- resident-control missing-fd/direct-shell refusal, hello build/schema/protocol/
  deployment mismatch, frame bounds and unknown fields, bounded
  multipage registration-inventory exact/missing/mismatched/extra results
  beyond 256 items with count/digest/cursor/EOF faults, selected
  binding+delivery+CA+matcher attestation, and captured secret-free frames;
- probe-verifier registration/revocation, raw nonce absence from control
  frames, broker death both before and after Docker create, immediate
  connection cleanup, independent TTL expiry, resident restart, and no stale
  verifier reuse;
- resident death after runtime registration plus Homie agent create but before
  start: the old labeled generation cannot attest in the new process, so exact
  agent-first/guard-next cleanup completes before the same launch generation
  receives a wholly new envelope and registration;
- the one-guard/two-capless-client topology, network-only namespace sharing,
  deterministic names and labels, exact candidate chain inspection, positive
  local synthetic raw match, fixture-chain absence from the final guard, and
  client-first confirmed cleanup/crash sweep, plus the common closed
  mount/git-setup probe-role names/manifests and sacrificial-only Git mutation;
- global-health versus candidate-policy consequences, stale no-mutation, and
  postclaim infrastructure failure; and
- guard death followed by agent-first cleanup, explicitly demonstrating that
  an already-running agent can create a new declared raw connection and keep
  an existing declared raw flow until the agent is stopped, while new proxy
  and DNS traffic fails.

All origins, resolvers, echo servers, credentials, and CA material are local
fixtures. Test-only resolver/private-address seams cannot compile into the
untagged binary.

## Alternatives rejected

- `HTTPS_PROXY` alone is bypassable by direct sockets and `NO_PROXY`.
- One internal bridge/dual-homed proxy cannot express general raw CIDRs.
- Per-destination relays do not preserve arbitrary CIDR semantics.
- Global `DOCKER-USER` or host firewall mutation is cross-run global state and
  unreliable inside Docker Desktop's VM.
- A wildcard host listener creates a LAN-facing proxy; the proven loopback
  host-gateway route avoids it.
- A credential-bearing gateway container contradicts native resident
  ownership.
- Blind CONNECT collapses HTTP and raw enforcement planes.
- Proxy-owned or copied OAuth state is rejected by §11.4/S3.
