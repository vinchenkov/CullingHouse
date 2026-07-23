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

## 2026-07-22 — private path-free spine initializer (`e0a0397`)

The Linux same-binary crossing now exposes strict-decoded
`__onboard-spine` only under the helper's fixed scope. Its 64-KiB-bounded frame
carries protocol/schema identity plus mirror present/absent evidence and no
path, home, config, routing, Worksource, or credential field. The helper
computes all database facts from its fixed spine path.

The test matrix proves fresh initialize, committed-meta/absent-mirror crash
repair, matching idempotence, mismatch refusal, empty-volume spine loss,
foreign non-meta refusal, known-old migration, and newer-schema refusal. The
initializer secures the volume root/database modes and never initializes over
ambiguous bytes. The six-leg fast suite is green.

NEXT: build the Darwin exact helper manager and compose home mirror publication
plus the existing kernel capability probe around this crossing.

## 2026-07-22 — production helper + Home crossing (`ca4eae4`)

The production image now carries the required general setuid `mc-real` (uid
10001, mode 6755); privileged invocations fix identity at `/mc/run.json` and
cannot be redirected by agent environment. The warm helper runs as uid 10002
with network none, CapDrop=ALL, the ADR-019 500m/512MiB/128 bounds, and only
its deployment-derived named spine volume. Its manager validates the native
arm64 image ID and full container/volume envelope, refuses unmanaged name
collisions, replaces only stale managed stateless helpers, and never deletes
the volume.

Production `onboard home` now stays split at the kernel boundary: Darwin runs
the git/canonical-home fence and observes/publishes only `deployment.uuid`;
Linux alone initializes/migrates/compares the named-volume spine through the
strict private frame. `install.sh` builds the exact production image before
the crossing. A disposable real-Docker walk initialized schema 13 and the
capability probe proved uid-10002 direct-open EACCES, brokered read, honored
setuid, NoNewPrivs=0, identity uid_map, and native arm64. Focused image Docker
tests and the six-leg fast suite are green; the known resident fd-3 intermittent
appeared twice and then passed focused and full reruns. The disposable helper,
volume, and home were removed.

NEXT: compose production doctor across host and helper facts, make Container
own exact image/helper health, then split the remaining onboarding sections so
no helper ever needs an MC_HOME bind. Keep live-token and launchd-load legs
parked.

## 2026-07-22 — composed production doctor (`d6c6384`)

Production doctor no longer delegates a host-shaped verb into the spine-only
helper. A version/schema-fenced, bounded private command returns exactly the
four runtime-authoritative findings and spine UUID; Darwin computes the four
host-authoritative findings, compares only the UUID mirror, and merges the
fixed nine-row order. Missing Docker/helper or invalid helper output stays an
exit-0 `ok:false` report with every diagnosis present. `onboard container` and
`onboard verify` reuse the same in-process composition, and the unnamed wizard
now refuses until every remaining mixed-authority section is split.

Unit tests pin the closed grammar/order, output bound, private scope, UUID
comparison, and helper namespace envelope. A disposable real deployment
proved the merged host/runtime report and returned Container `ok`; no home,
routing, config, or credential bind entered the helper. The rebuilt native
image passed focused Docker tests and the six-leg fast suite. Disposable
container, volume, and home were removed.

NEXT: split Routing, Worksource, Tunables, Surfaces, Runtime-auth, and
Supervision into host/path-free-helper halves, then make the unnamed production
wizard run the complete ordered composition. Keep live-token and launchd-load
acceptance legs parked.

## 2026-07-22 — split production onboarding state (`bf5981d`)

Production Routing now remains wholly on Darwin after the fixed helper proves
the mirrored deployment identity. Worksource input is validated, symlink-
resolved, and rechecked on Darwin; only its canonical schema value crosses to
the helper, which never stats or mounts it. The helper returns every registered
root so the host can prove inputless replay reachability. Tunables and Surfaces
cross as bounded scalar answers and mutate only through the helper's fixed
spine lock domain. The shared private command rejects unknown fields, mixed
section arms, trailing data, build/schema/deployment drift, and malformed
responses.

Focused tests prove the authority split, canonicalization, pre/post host checks,
closed identity grammar, and exact replay. A disposable native arm64 helper
completed first-run and idempotent Routing, Worksource, Tunables, and Surfaces;
composed doctor reported Worksource and Surfaces healthy. The helper, volume,
home, workspace, and host binary were removed. The six-leg fast suite is green.

NEXT: implement the fail-closed Runtime-auth import/health section and the
unloaded Supervision artifact/health section, then make the unnamed production
wizard run the complete ordered composition. Keep live-token and launchd-load
acceptance legs parked.

## 2026-07-22 — binding-owned credential policy (`675cbe0`)

The production binding catalog now closes the three shipped bindings over
harness, projection/static channel, provider credential names, declared static
key, and OAuth authority. The routing registry derives from that catalog.
Host-side pre-claim env attestation uses the binding resolved from the attested
routing table rather than the contradictory Worksource-profile delivery field.
OAuth provider/agent-identity keys, refresh material, the shipped floor, and
foreign static keys refuse before claim; MiniMax alone admits its declared
static token. The test-fake catalog remains tag-scoped. The six-leg fast suite
is green.

NEXT: add the closed OAuth/static grant union and fail-closed resident coverage,
then build the atomic Runtime-auth importer. Production adapters/live no-op and
launchd activation remain operator-present acceptance gates.

## 2026-07-22 — closed resident runtime grants (`8886c09`)

The resident now accepts exactly the three production binding grant shapes:
pinned Codex and Claude OAuth grants, and MiniMax's declared materialized
`ANTHROPIC_AUTH_TOKEN`. It rejects authority/client drift, cross-binding
channels, unknown or extra fields, malformed scope/account evidence, filename
confusion, duplicates, missing grants, and unknown production routes. Static
grants never enter the refresh service or broker. An absent projector refuses
before launch artifacts or Docker; only fake/fake remains token-free.

Focused resident tests and the six-leg fast suite are green.

NEXT: build the atomic Runtime-auth importer with isolated provider homes,
forbidden-env gating, exact binding ownership, and no partial publication.
Production adapters/live no-op and launchd activation remain operator-present
acceptance gates.

## 2026-07-22 — atomic Runtime-auth import (`556dc1e`)

Runtime-auth accepts only owner-owned, mode-0600, singly linked provider-native
files below owner-only `MC_HOME/runtime-auth-sources`; it cannot import the
operator's personal harness homes. Codex/Claude source evidence is reduced to
the pinned OAuth grant shape and MiniMax to its one declared static key. Every
selected binding's forbidden-env and live verifier gate sees the full private
staged set while the old canonical directory remains unchanged. Publication is
one durable directory rename/exchange on Darwin and Linux; failed gates publish
nothing, rotation cannot expose a mixed binding set, and identical replay keeps
the canonical directory identity.

The real verifier currently refuses by construction because production
adapters do not yet exist, so no unverified real grant can be published.
Focused importer/broker tests and the six-leg fast suite are green.

NEXT: implement the real Codex and Claude-SDK adapter launch paths, use them for
staged per-binding no-op verification, then wrap the importer with isolated
provider-owned login acquisition and source cleanup. Live token spend and
launchd activation remain operator-present acceptance gates.

## 2026-07-22 — real production adapters (`3007478`)

The runner now selects the closed `codex/chatgpt`, `claude-sdk/claude`, and
`claude-sdk/minimax` routes intrinsically; the old route list survives only as
an explicit fake-adapter stand-in for token-free Docker acceptance. Unknown
routes, malformed resume modes, missing native handles, and escaping trace
locators refuse. Codex receives only its projected auth/broker channel and
writes its dated rollout under the run's durable session mount. Claude and
MiniMax use the locked Agent SDK with no inherited settings, closed preset
tools, isolated config, and an eager mode-0600/fsynced SessionStore supporting
resume and subagent transcripts. MiniMax's compatible base URL is pinned in
both catalogs.

The production Docker build installs `@openai/codex` 0.145.0 and
`@anthropic-ai/claude-agent-sdk` 0.3.217 outside the bind-mounted source path;
the fake image installs neither. The adapter directly executes the locked
Linux-arm64 Codex binary, avoiding a redundant Node runtime for npm's thin
launcher. An unprivileged production-container probe and focused Docker
boundary tests proved both runtime pins and fake-route rejection. Adapter,
resident, catalog, and native-store focused tests plus the six-leg fast suite
are green.

NEXT: make Runtime-auth's staged verifier launch each real adapter through the
production image with a fixed no-op prompt and exact staged grant, then wrap
the importer with isolated provider-owned login acquisition and source cleanup.
Live token spend and launchd activation remain operator-present acceptance gates.

## 2026-07-22 — production-adapter Runtime-auth verifier (`4fa0dee`)

Runtime-auth now constructs its production verifier from canonical MC_HOME,
requires the owner-only installed runner tree, exchanges OAuth refresh material
only for short-lived adapter credentials, and runs a fixed no-tool prompt
through the selected real adapter in `mc-prod`. Publication requires both a
successful adapter exit and the regular native trace named by its safe
`session-start` locator. Provider refresh rotation is atomically adopted only
inside the private stage; the exact grant set is revalidated and fsynced after
all verifier calls so verifier mutation cannot be smuggled into publication.

Codex and Claude now fail closed on their own inner Linux sandboxes. Only real
agent/verifier containers lift Docker's outer seccomp filter so bwrap can create
its namespace; fake, setup, and landing containers retain the default profile.
Codex's locked custom permission profile denies its projected auth tree while
admitting workspace and network, and Claude denies its session/config paths and
projected token variables. A production-image boundary executes the actual
locked Codex sandbox and proves the projected auth file is unavailable.

The production image was rebuilt arm64-native, focused verifier/adapter/resident
tests pass, and the fixed six-leg fast suite is green.

NEXT: install the owner-only release runner assets under
`MC_HOME/release/runner`, then wrap Runtime-auth with isolated provider-owned
login acquisition and source cleanup. Live token spend and launchd activation
remain operator-present acceptance gates.

## 2026-07-22 — owner-only installed runner release (`6202498`)

The install front door now passes the repository runner tree as source evidence
to Home onboarding; it performs no copy itself. The host-side Home section
admits only the fixed five-file production manifest, rejects symlinked or
group/other-writable source entries, writes owner-only files/directories into a
sibling stage, fsyncs it, and atomically publishes
`MC_HOME/release/runner`. Tests, fake harnesses, dependency metadata, and
provider state never enter the runtime source mount. Byte-identical replay
preserves directory identity; changed or corrupted owned trees are replaced as
one unit.

The Runtime-auth verifier now validates the entire exact installed tree before
mounting it. Deployment tests cover publication, modes, replay, upgrade,
symlink refusal before writes, and unexpected-entry rejection; a CLI test
crosses the Home flag. Production/fake-tag builds, the install shell contract,
and the fixed six-leg fast suite are green.

NEXT: wrap Runtime-auth with isolated provider-owned login acquisition and
source cleanup, using only `MC_HOME/runtime-auth-sources` as the flow home.
Live token spend and launchd activation remain operator-present acceptance gates.

## 2026-07-22 — isolated provider-owned subscription acquisition (`bd4385b`)

`mc onboard runtime-auth --acquire` now runs plain `codex login` and
`claude auth login --claudeai` in unique owner-only flow homes beneath
`MC_HOME/runtime-auth-sources`. Each subprocess receives only a small
transport/locale/custom-CA environment plus its disposable HOME/config path;
personal harness homes, provider endpoint overrides, and ambient credentials
cannot enter. Claude's explicit subscription flag excludes the metered Console
login. A selected MiniMax binding must have its isolated owner-only key source
before browser consent begins.

Provider-native files feed the existing structural importer and real-adapter
live gates. The exact created flow root is removed after success, login
failure, or import failure; cleanup identity drift is refused and surfaced
rather than followed. Unit tests pin argv/environment, early ambient-key
refusal, cleanup, and cleanup-race behavior. A broker test crosses acquisition
through verified grant publication and proves no flow source survives. The
fixed six-leg fast suite is green; no live provider turn was spent.

NEXT: package the production resident/dashboard payloads under the installed
release, then generate and verify unloaded supervision units. Live token spend
and launchd activation remain operator-present acceptance gates.

## 2026-07-22 — installed native host payload (`1513fe3`)

Home onboarding now admits the repository root only as source evidence for a
separate `MC_HOME/release/host` publication. Its sixteen-file manifest is the
exact transitive resident/dashboard TypeScript and dashboard UI graph. Tests,
lockfiles, package metadata, runners, and provider state are excluded. Keeping
this payload beside rather than inside the runner tree prevents agent
containers from seeing native host code through `/app/src`.

The payload is owner-only, fsynced, idempotent by directory identity, and
atomically exchanged on change. Deployment tests cover closed publication,
test exclusion, replay, and replacement; the Home CLI crossing validates both
installed payloads. The fixed six-leg fast suite is green.

NEXT: generate the resident/dashboard configs and per-user LaunchAgents from
installed payloads, verify their exact unloaded state, and do not bootstrap
them. Live token spend and launchd activation remain operator-present gates.

## 2026-07-22 — immutable production release identity (`73b710b`)

Production installation now resolves one exact repository commit and embeds
it in both the native host `mc` and the Linux helper `mc-real`. Standalone
production image builds derive the same identity from HEAD; malformed caller
overrides refuse before compilation. Development and fake binaries retain the
deliberate `development` identity.

The installer contract and image fast suite are green. The production image
was rebuilt arm64-native from the committed boundary, and both it and a native
release build contain the exact full commit
`73b710b9e2aa575c4928bc0bf6816c83ec0418d4`. The new image is
`sha256:13321fc21132515cc6be84a4f3d09c2e0a3940f0ca581709470926142aaa6993`.

NEXT: generate the resident/dashboard configs and per-user LaunchAgents from
installed payloads, pin the immutable release identity, verify their exact
unloaded state, and do not bootstrap them. Live token spend and launchd
activation remain operator-present gates.

## 2026-07-22 — unloaded native supervision bundle (`89c6f75`)

The production Supervision section now reads the complete Worksource catalog
through a path-free helper frame and host-rechecks every root before publishing
anything. It atomically installs the exact resident/dashboard JSON and two
per-deployment per-user LaunchAgent plists under `MC_HOME/supervision`, with
absolute executable and installed-release paths, immutable release/schema
identity, the derived spine volume, loopback dashboard bind, RunAtLoad,
KeepAlive, throttling, and owner-only log roots.

Preparation checks both labels before and after publication and refuses if
either is loaded. It never writes the user's LaunchAgents directory and never
bootstraps a service. Tests parse both JSON configs, lint both plists, validate
the closed owner-only tree, prove loaded-state refusal and inode-stable replay,
and exercise host rechecks. The resident now uses the absolute Docker path,
mounts every Worksource read-only for Homie, and no longer requires fake
behavior fixtures for real production adapters. Full mc and resident checks
are green. The production image was rebuilt arm64-native from this commit as
`sha256:a3fed3e1ab83456db379aca0ccce3210fc35ced7f2160193b71ffac8e3ee37f9`.

NEXT: implement the operator-present activation transaction and real-tick
receipt plus supervision doctor probe, testing the machinery without loading
launchd. Then compose the whole wizard; live activation remains gated.

## 2026-07-22 — transactional supervision activation (`825f258`, `f10ddfc`)

The resident now atomically publishes an owner-only health receipt after a
dispatch result parses and its effect completes. The receipt binds the exact
release, config schema, and completion time; failed or unparseable dispatches
do not produce it.

The explicit operator-present `supervision --activate` arm installs the two
prepared plists into the user's LaunchAgents directory, bootstraps dashboard
then resident, verifies both exact definitions are loaded, and waits up to 90
seconds for a new matching tick receipt. Any partial first activation boots
out jobs in reverse order and removes only the exact plists that transaction
created. Replays require both loaded jobs, exact installed bytes, and a
matching receipt. Production doctor now reports Supervision healthy only when
both jobs are loaded from the prepared definitions and the receipt is less
than two minutes old.

All activation tests use an injected launchd controller; no real launchd call
was made. Full mc and resident checks are green. The production image was
rebuilt arm64-native from `f10ddfc` as
`sha256:a1f1529f9e433ba79f17d73f3acd3a1290172cc88cc437132e3b1468c098ddf5`.

NEXT: compose the production whole wizard over the completed sections without
implicitly activating launchd or spending tokens; then implement and verify
backup/restore and remaining Phase-5 real-runtime obligations.

## 2026-07-22 — production whole-wizard composition (`291aca8`)

The production no-section onboarding command now runs the completed sections
in their contract order and returns one ordered aggregate receipt. It forwards
the caller's Worksource, schedule, runtime-auth, and activation inputs without
reinterpreting them. A structurally healthy canonical grant store permits a
token-free replay; otherwise the wizard stops at Runtime Auth without invoking
an adapter. Supervision is prepared but reported `needs-operator` unless the
caller explicitly supplies `--activate`, after which final Verify runs.

Tests cover the no-spend stop, unloaded preparation, explicit activation path,
invalid nested receipts, and shell-level preservation of arguments containing
spaces. Full `mc/check.sh` is green. No provider turn was spent and no launchd
job was loaded. The arm64 production image was rebuilt from `291aca8` as
`sha256:bffdaa99d6d29690049fede4fb6dec27eaed7ff37033055a9283bec9ab827260`;
its helper contains exact release identity
`291aca8f349b7548855cfae5ba901bf66aca8eae`.

NEXT: implement and verify production backup/restore before continuing the
remaining Phase-5 real-runtime obligations.

## 2026-07-22 — production backup/restore crossing (`072061f`)

`mc backup` now keeps filesystem authority on Darwin and spine authority in
the exact warm helper. The helper creates a `VACUUM INTO` copy on its local
volume and emits a bounded identity header plus raw snapshot bytes; the host
publishes only after exact length/digest, SQLite integrity, schema, deployment
UUID, owner, mode, and directory checks pass. Publication is mode 0600,
file/directory-synced, atomic, and prunes only recognized snapshot names to the
48-copy default. No host path enters the helper.

`mc onboard home --restore-latest` is the dual-input recovery arm. It requires
both native jobs unloaded, chooses the newest owner-only single-link matching
snapshot without corrupt-newest fallback, streams it into a same-volume stage,
and atomically fills only a missing/empty spine slot. Foreign, corrupt, newer,
truncated, trailing, widened, and symlinked state refuses without target or
temp residue; healthy replay verifies the exact stream without overwriting the
spine. The ordinary Home pass then performs any supported migration.

The resident now requires a successful startup snapshot before its first
dispatch and runs a serialized hourly due-backup chore. A backup failure skips
dispatch and retries at the next tick. Tests pin ordering, replay, sentinel
record preservation, retention, all failure classes above, private fixed-scope
CLI round-trip, and front-door flag preservation. The complete fixed six-leg
suite is green. No launchd job was loaded and no live deployment was changed.
The arm64 production image rebuilt from `072061f` is
`sha256:9f93779d7285cc99796b51dfe635c1f409dd7afc94736a837b56cdfad7079d5c`
and embeds `072061f64a51e0b0f9f57a8535c261800625b0ff`.

NEXT: finish production reset/runtime-volume lifecycle, then the remaining
Phase-5 real-runtime obligations.
