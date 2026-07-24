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

## 2026-07-22 — production reset volume lifecycle (`28d6102`)

Production `mc reset --confirm` is now a host-brokered lifecycle operation
rather than a delegated database unlink. Both native jobs must be unloaded.
The broker commits and validates a durable host backup first, rechecks the
exact arm64 production image, managed least-privilege helper, and derived
option-free local volume, removes the helper by its immutable 64-hex container
ID, rechecks name absence, then removes and rechecks the volume. No raw volume
name supplied by the caller is accepted.

If teardown stops after the backup, the error names the durable snapshot and
leaves the volume bytes intact. A lost response after successful removal
replays as `already-reset` only when both helper and volume are absent and the
newest snapshot still passes owner/mode/link, integrity, schema, and deployment
identity checks. Missing confirmation refuses before any runtime probe. Full
`mc/check.sh` is green; no live deployment or launchd state was touched. The
arm64 production image rebuilt from `28d6102` is
`sha256:7b3dbd79f204038bc02dfd477ab2c3899dc535c4c0b1ba1f9a275982af0861ab`
and embeds `28d61022690cba057046ce666e66494b26e81024`.

NEXT: implement ADR-023's production real-harness initiative-child shared-
worktree mount rows, then continue the remaining Phase-5 runtime obligations.

## 2026-07-23 — ADR-025 accepted; initiative mount groundwork (`fc72175`)

Took over from codex's last green (`28d6102`, docs at `ff4371e`). A scoped
cross-harness takeover review of `d0ef4bb..b95df99` on the mount surfaces
(mountattest/dispatchseam/envattest/effects) returned no blockers: the
ADR-017 D6 initiative-child refusal is untouched and still fires under real
routing, env attestation was tightened, and the legacy land lane is intact.
Two concerns (seccomp=unconfined vs stale ADR-019 text; duplicated production
route table) were already logged deviations. Recorded at `75ded23`.

Designed the owed production real-harness initiative mount representation as
ADR-025, adversarially verified through three lenses (git-mechanics,
security/jurisdiction, state-machine) before acceptance. Every needs-changes
finding is folded in: containerized verified import instead of host git,
promotion-observable cut, seal-free child completion with suppressed seal
emission, host-unresolvable-but-container-correct pointer bytes, a
producer-absence/cleanliness fence, resident-owned teardown, and a six-slice
sequence (S1–S6) that keeps every existing refusal fail-closed until S6 lands.
Three deviations recorded in IMPLEMENTATION-NOTES (2026-07-23). Accepted at
`c516da2`.

Implementation began bottom-up, each micro-step green and inert:
- `a3c3f70` — `.mc-worktrees` joins `.mission-control`/`.git` as a reserved
  base-tree root component (ADR-025 D10), so no child can commit a path that
  collides with the live shared worktree at merge time.
- `e33fdfc` — the D6 worktree-name grammar generalized in place to the closed
  two-alternative `mc-task-<id> | mc-initiative-<id>` (distinct prefixes,
  collision-free); ADR-017 amended in place with the guard's `adr017Rows` keys
  and every duplicated grammar pin in lockstep. The `<mc-worktree-name>`
  amendment paragraph is placed AFTER both destination tables so the guard's
  table parser is not blinded.
- `fc72175` — `initiativePlanRows` derives the child table from `taskPlanRows`
  (so they cannot drift) with the shared worktree as the one row family whose
  host base is not the bound `/workspace` root; `resolveInitiativeSkeleton`
  resolves the two-base skeleton under exactly the standalone row discipline.
  Nothing yet calls it — the `mountattest.go:238-249` refusal still stands.

NEXT: wire `resolveInitiativeSkeleton` into `deriveDispatchMountRequests`
behind an initiative-child arm (ADR-025 D2/D5) with the snapshot-capture
`SubjectInitiativeID` gate and seal-emission suppression, replacing the S2
refusal for a receipt-vouched child Worker while keeping every other
combination refused.

## 2026-07-23 — ADR-025 S2: initiative-child Worker mount arm (inert)

S2 wires the dormant `resolveInitiativeSkeleton`/`initiativePlanRows` groundwork
into the live derivation, replacing the `mountattest.go` initiative-child
refusal for exactly one case — a receipt-vouched Worker child on a repo
Worksource under real routing — while every other combination stays
health-refused (S3–S6 owed). Still inert end-to-end: no S1 receipt producer
exists, so a real initiative child resolves an absent shared store and
health-refuses; the arm only comes alive when S1 lands the `InitiativeSetup`
cut.

- Carrier: `substrate.DispatchInitiativeSetup` (two vouched roots) +
  `DispatchMountState.SubjectInitiativeSetup` (`omitempty`), with a
  helper-boundary validator mirroring `validatePrivateTaskSetupRoots`. Nothing
  populates it yet (S1's job); the cut SHA is deferred to its consumer
  (IMPLEMENTATION-NOTES 2026-07-23).
- Derive: `deriveDispatchMountRequests` routes real-routed initiative children
  through `deriveInitiativeChildMountRequests` — the 15 D2 rows over the two
  host bases (store root + shared worktree), RW on source/git for the Worker.
  Fake routing is unchanged: children still fall through to the legacy
  whole-Worksource bind. Non-Worker/non-repo/no-subject retain today's refusal.
- Capture: `captureDispatchMountHostSnapshot` gates on `SubjectInitiativeID`
  BEFORE the task-skeleton arm (ADR-025 D5), so a child never triggers
  standalone task precreate; it resolves the skeleton, vouches BOTH roots
  against the frozen receipt (`requireInitiativeSetupReceiptVouch`), and
  populates `TypedRoots`. Absent store or absent/mismatched receipt → health.
- Seal suppression (ADR-025 D4): `CompletionSeal` (Worker) and the
  verifier-only `AcceptedSealRebuild`/`VerifierProjection` are gated on
  `SubjectInitiativeID == nil` — a shared-store child uses the plain unsealed
  terminal.
- Tests: derive rows + two-base sources; non-Worker/non-repo/no-subject
  refusal retention; the two-root vouch table (valid/nil/store-mismatch/
  worktree-mismatch); a full real-capture attest proving 15 rows, RW
  source/git, suppressed seal, no precreate; and the no-receipt attest health
  refusal. Fast suite green; `verbs`/`substrate` cold `-count=1` green.

NEXT: ADR-025 S3 — child Verifier/Packager get the D2 table with every row
forced RO (`readOnlyView`), gated on the vouched receipt, plus the D6
producer-absence/cleanliness fence for the shared store; Refiner over a child
keeps refusing.

## 2026-07-23 — ADR-025 S3a: child Verifier/Packager forced-RO arm (inert)

The initiative-child derive arm now admits three roles on a repo Worksource:
the Worker (row-declared access, RW source/git) and a Verifier or Packager,
which get the same 15 D2 destinations with EVERY row forced RO (D5 — the
branch-tip reader never writes the shared store; no completion-seal gate, D4).
A Refiner over a child and a Strategist/Editor keep refusing (S4 owed), as do
non-repo/profile-less/no-subject shapes. The capture-side resolve+vouch already
ran for any initiative role, so no capture change was needed; the receipt gate
holds for readers too. Still inert: no S1 receipt producer, so a real reader
resolves an absent store and refuses.

Tests: verifier/packager derive all-RO rows; refiner/editor/strategist/
no-subject/non-repo refusal retention; a full real-capture attest for a child
Verifier proving 15 rows all RO with no seal/rebuild/projection machinery.
Fast suite green.

STILL OWED in S3: the D6 producer-absence + store-worktree cleanliness fence.
D5 gates the reader arm on "the vouched initiative receipt AND D6's fence"; only
the receipt gate is implemented. Because the arm is inert until S1's receipt
exists, landing the RO arm ahead of the fence adds no reachable path — but the
fence must land before S6.

NEXT: ADR-025 S3b — the D6 fence: before any next initiative-family container
for initiative I is prepared, confirm the ABSENCE of every prior child
container of I (the ADR-017:533 producer-absence discipline extended per-
initiative — reap's best-effort `docker stop` is not confirmation), and
attestation asserts store-worktree cleanliness (working tree and index clean at
the branch tip). This is resident/attestation-side machinery; scope the exact
seam before implementing.

## 2026-07-23 — D6-fence seam scouting + S3b/S1 re-sequencing

Read-only scout of the D6-fence seam (recorded so S3b starts with the hooks
identified). Producer-absence precedent: `resident/src/effects.ts:380`
`requireAcceptedSealProducerAbsent` confirms a prior container's ABSENCE via
`docker inspect --type container mc-run-<runId>` / `mc-setup-<runId>` (exit-1
not-found is the only success — a positive absence, never reap's best-effort
`docker stop` at effects.ts:1085). Its sole caller is the accepted-seal-rebuild
arm at `effects.ts:452` inside `spawn()`. Child containers carry only labels
`mc-managed=true`, `mc-tier=pipeline`, `mc-run-id=<run>` (effects.ts:692-694) —
NO `mc-initiative-id` label yet; the `mc-approved-run-id` label on the sealed-
landing container (effects.ts:966) is the precedent for a task/initiative-scoped
label. So per-initiative absence needs either a new `mc-initiative-id=<I>` label
+ `docker ps --filter label` enumeration, or dispatch projecting the prior
child run-id set into the effect (the resident cannot query the spine).
Cleanliness precedent: every "clean tree/index" assertion runs IN-CONTAINER via
`git status --porcelain=v1 --untracked-files=all == ""` —
`verifierprojection.go:32` (closest analog), `landsealed.go:360`,
`completionsealpublish.go:216`; the host never runs Git (ADR-016 D5). mc-land's
host-side dirty fence (`runner/image/mc-land:433`) resolves empty for
store-linked worktrees (D8), so cleanliness must move into a store-mounted
`mc __…` subcommand launched like `__setup-verifier-projection`
(effects.ts:482). S1 producer is ENTIRELY owed: no receipt table in schema.sql,
no `mc __setup-initiative`, no InitiativeSetup dispatch step, no receipt loader
in mountprojection.go (contrast the task path's `LoadSubjectTaskSetupRoots`).

Re-sequencing decision (deviation logged in IMPLEMENTATION-NOTES 2026-07-23):
the D6 fence and its cleanliness subcommand are resident/container-prep
machinery that hook the initiative-child spawn path S1 establishes. Building it
now would invent the initiative-child resident effect shape and the
child-enumeration mechanism ahead of their producer. The ADR's own slice order
is S1 first; the earlier pull-forward of S2/S3a was safe only because those are
inert host-side attest derivation. So the next slice is S1
(`__setup-initiative` cut + receipts), after which S3b's fence has a real
lifecycle to guard. S2/S3a stay inert and correct in the meantime.

NEXT: ADR-025 S1 — `InitiativeSetup` cut. Skeleton precreate (store root 0555
with exactly {git, source}, worktree dir 0700 under `.mc-worktrees`),
`mc __setup-initiative` materializer (sanitized store cut from CURRENT main tip
+ checkout, generalizing `MaterializeFirstTaskStore` at setupenvelope.go:281),
initiative-keyed durable receipts carrying BOTH roots + the recorded cut SHA,
retry-reuse of the recorded cut SHA (never re-resolve main), the `.mc-worktrees`
discipline, and D10 reservations/covers. Then the receipt loader populates
`SubjectInitiativeSetup` (mountprojection.go), making S2/S3a's vouch reachable.
Scope the dispatch InitiativeSetup step emission (D3: first tick where promotion
is observable, before any other initiative-family spawn) against the existing
task precreate/setup step machinery before implementing.

## 2026-07-23 — ADR-025 S1.1: initiative_setup_receipts (read half of the D3 cut)

Bottom-up start on S1 (the InitiativeSetup cut). This slice lands the spine
table and the READ path; the register/write is deferred to S1.5 where the
lease/step context can fence it (the task analog `RegisterFirstTaskSetup`
fences on a live Worker run + lock lease — that fence has no S1.1 equivalent
yet). Still inert: with no producer, `initiative_setup_receipts` is empty, so
`LoadSubjectInitiativeSetup` returns nil, `SubjectInitiativeSetup` stays nil,
and an initiative child health-refuses on an absent store exactly as before.

- Schema (v13→v14): `initiative_setup_receipts` — `initiative_id PRIMARY KEY
  REFERENCES tasks(id)` (the scope='initiative' row; no `initiatives` table),
  two independent root triples (store + worktree, decimal-GLOB/typeof-fenced
  like `task_setup_receipts`; the worktree is not a descendant of the store,
  ADR-025 D1), `cut_sha` (git-hash hex, len 40|64 — kept simpler than
  `task_assignments.base_sha`: no separate `object_format` column, since S1.1
  does not consume it), `registered_at`, plus immutable + no-delete triggers.
  Keyed by initiative (one immutable row → retry reuses the recorded cut,
  mirroring `task_assignments`' keying, NOT run-keyed like
  `task_setup_receipts`). Added to schema.sql AND as `migrationV13ToV14`
  (byte-identical), registered in the steps map; `CurrentSchemaVersion`→14 and
  the resident `SPINE_SCHEMA_VERSION`→14 in lockstep (plus the resident
  handshake-test default 13→14 — this bun test catches the mismatch without
  Docker, unlike the LESSON's usual Docker-only catch).
- Read: `LoadSubjectInitiativeSetup` (single `*DispatchInitiativeSetup` or nil,
  modeled on `LoadSubjectTaskAssignment`). Loader wiring in
  `loadDispatchMountState` keys on `*state.SubjectInitiativeID` (the PARENT
  initiative id = child.initiative_id), NEVER the child subject id — the
  receipt belongs to the one shared store (ADR-025 D2). Carrier gained
  `CutSHA` (omitempty); helper-boundary validation checks it when present
  (git-hash hex). The mount vouch still reads only the two roots.
- Tests: immutable/typed/closed (PK one-row, cut_sha length/hex/BLOB fences,
  negative-uid, non-decimal device, immutable/no-delete); migrate-matches-fresh
  (via `migratedV1Spine`, no new testdata file); load projects both roots + cut
  and returns nil for an absent initiative. Full fast suite green;
  `substrate`/`verbs` cold `-count=1` green.

NEXT: S1.3 — the `mc __setup-initiative` materializer (see PROGRESS NEXT).

## 2026-07-23 — ADR-025 S1.3a: MaterializeInitiativeStore (the cross-base cut)

The git core of the InitiativeSetup cut, host-side unit-testable (parity with
MaterializeFirstTaskStore — no Docker lane). Generalizes the first-task
materializer to ADR-025 D1's TWO-base layout and is inert (no caller yet; the
`mc __setup-initiative` subcommand is S1.3b, the resident invocation S1.5).

Spike first (de-risking the hardest git-mechanics in ADR-025): a real-git
experiment proved the cross-base checkout. Because the sanitized store
(`.mission-control/initiatives/initiative-<id>`) and the shared worktree
(`.mc-worktrees/initiative-<id>`) are NOT siblings on the host, the
container-relative `.git`/`gitdir` pointers do not resolve host-side (ADR-025
D1). The checkout is therefore driven with an explicit `GIT_DIR` (the linked
worktree admin `git/worktrees/mc-initiative-<id>`, whose `commondir` = `../..`
DOES resolve within the store) and `GIT_WORK_TREE` (the separate worktree base),
never through the pointers. The spike confirmed: index lands in the worktree
admin, exec bit + symlink preserved in the checkout, bare store fsck-clean, and
the simulated container layout (store + worktree siblings under /workspace)
resolves HEAD with a clean `status`.

Divergences from the task materializer (all seven the plan flagged): two
separate roots (store + worktree), `store/source` stays the empty structural
mountpoint (never written), the `.git` pointer + empty `.mission-control` cover
move to the worktree base, the checkout uses explicit GIT_DIR/GIT_WORK_TREE,
branch `mc/initiative-<id>` / worktree `mc-initiative-<id>`, cut = current tip of
the caller-supplied target ref (fresh) or the recorded cut SHA (retry, never
re-resolves), and fsck runs against `<store>/git`. Reuses verbatim:
resolveBaseOID, rejectReservedTreeComponent (already reserves `.mc-worktrees`,
D10), requireEmptyChild, extractClosurePack, fsckClean, digestLandedPack,
generatedTaskGitConfig, sourceGitEnv, gitOutput.

Tests (host-side, real git): fsck-clean operable store (ref at cut, HEAD names
the shared branch, store/source empty, worktree pointer bytes exact,
.mission-control empty, checkout clean/exec/symlink, no loose objects/alternate,
closed config keys); retry pins the recorded cut (no rebase to a moved main);
object-format-mismatch, worktree-residue, and reserved-`.mc-worktrees` refusals.
Full fast suite green.

NEXT: S1.3b — the `mc __setup-initiative` subcommand: a SetupEnvelope
InitiativeSetup arm (store root + worktree root container dests),
RunInitiativeSetup (mint UUID, resolve object format, call
MaterializeInitiativeStore, emit the cut SHA; the roots are stat'd host-side by
the resident, so the container emits only the SHA), and the two main.go
registration sites. Then S1.4 (dispatch step), S1.5 (resident precreate +
RegisterInitiativeSetup write).

## 2026-07-23 — ADR-025 S1.3b: mc __setup-initiative subcommand + envelope

Wraps S1.3a's MaterializeInitiativeStore in the closed setup-envelope union so
the resident (S1.5) can launch the cut in a container. Still inert: nothing
invokes __setup-initiative yet.

- SetupEnvelope: new `SetupOperationInitiativeSetup` arm + a second container
  root `WorktreeRoot` (the external `.mc-worktrees/initiative-<id>` checkout;
  TaskRoot carries the store root). A single top-level guard keeps the union
  closed — `WorktreeRoot` may ride ONLY the initiative arm, mirroring the
  sealed-landing-authority guard. The arm is the first-task closure arm
  generalized: same fresh/retry discipline, the initiative branch/worktree
  grammar (`mc/initiative-<id>` / `mc-initiative-<id>`), and no accepted-seal
  authority.
- RunInitiativeSetup mirrors RunFirstTaskSetup: fresh mints a repo UUID; retry
  with a non-empty store accepts an exact-matching residue idempotently via
  `verifyLandedInitiativeStoreMatches` (store-only: ref at cut, config, pack
  digest, fsck) or refuses a divergent one; a clean retry re-cuts from the
  pinned SHA and proves the closure reproduces. It emits only the cut SHA
  (SetupResult.BaseSHA) — the resident stat's the two roots host-side for the
  receipt.
- main.go: `__setup-initiative` added to the local-run guard list and a
  host-scoped `case` mirroring `__setup-first-task`.
- Tests (host-side real git): fresh materializes + reports (store ref +
  worktree checkout); retry reproduces the pinned closure on a clean store;
  retry accepts exact residue idempotently and refuses a divergent digest; the
  envelope arm is closed (branch/worktree/target/worktree-root/seal-authority
  refusals, and WorktreeRoot rejected on a first-task envelope). Full fast suite
  green.

With S1.3a+S1.3b the container side of the InitiativeSetup cut is complete.
NEXT: S1.4 — dispatch emits the InitiativeSetup step (D3: for a seeded
scope='initiative' row whose branch is set but whose setup receipt is absent, at
the first promotion-observable tick, before any other initiative-family spawn),
studying the existing task precreate/setup step emission in the dispatch effect.
Then S1.5 (resident precreate + run the container + RegisterInitiativeSetup
write, deferred from S1.1).

Flake note (2026-07-23): during S1.3b's full-suite run the four `mc-land split
boundary` bun tests (runner/image/mc-land.test.ts) HUNG — each "timed out after
5000ms", exit 143, the file taking 3045s vs its normal ~25s. The tests exec a
`sh mc-land … main` git merge; the hang is a transient git-subprocess stall, not
a logic failure (S1.3b touches only mc/verbs + mc/cmd/mc/main.go, never mc-land).
An isolated re-run of runner/image was 41 pass/0 fail in 24.8s; verbs+cmd/mc are
green cold. No reliable repro, so not added to the PROGRESS intermittent list.

## 2026-07-24 — ADR-025 S1.4: design + S1.4a (inert predicate + data)

S1.4 (dispatch emits the InitiativeSetup step) is the most architecturally
loaded slice: the InitiativeSetup is the FIRST standalone setup-container
dispatch action (every existing setup container is an attest-time fold onto an
agent spawn, invisible to pure Decide; D3 requires a distinct action emitted
ahead of family spawns). A Plan-agent trace + verification settled the design
(full rationale in IMPLEMENTATION-NOTES 2026-07-23):

- Effect model: new KindInitiativeSetup (route-free, brief-free, Worker-tier,
  lease-claiming, agent-less); fresh/retry authored at attest from on-disk
  residue (no spine pin when the receipt is absent); avoids a new runs.role
  (closed schema CHECK).
- The fake-regression hazard (non-obvious): domain.Promote sets branch for EVERY
  initiative (fake or real), and fake/real is route.Harness=="fake" per candidate
  at attest, invisible to pure Decide. A naive predicate would emit into Phase
  4's fake lane and change its asserted sequence. Gate: Config.RealRouting =
  !allowFakeDecorrelation, threaded into Decide, DEFAULTING FALSE — every fake/
  unit fixture unchanged; production (allowFakeDecorrelation=false) fires it.

S1.4a landed the inert foundation (additive, zero behavioral change — Decide is
NOT yet wired): dispatch.Task.InitiativeSetupDone, Config.RealRouting (default
false), KindInitiativeSetup + the InitiativeSetup action arm, and the pure
nextInitiativeSetup(rec, cfg) predicate (seeded+scope=initiative+branch-set+
receipt-absent, lowest id, gated on RealRouting) — NOT called by Decide yet.
Unit tests exercise the predicate directly (promoted-uncut selects; fake never
fires; receipt/row-shape refusals; lowest-id determinism). Full fast suite green.

NEXT: S1.4b — wire nextInitiativeSetup into Decide (after step (0c) landing,
before (1) occupancy so it beats every spawn path), plumb Config.RealRouting from
!allowFakeDecorrelation at selectFromSpine, add the KindInitiativeSetup arm to
assertWellFormed, and add the route-free commit effect (open a Worker-tier run
keyed on the initiative id with no route/brief — the novel lease claim the seam
cannot yet express; study the KindSpawn prepare/commit path in dispatchseam.go/
dispatchverb.go). Then S1.4c (mount-plan step + attest arm) and S1.5 (resident).
Also owed: loadRecords LEFT JOIN initiative_setup_receipts to set
InitiativeSetupDone (fold into S1.4b with the RealRouting plumbing).

## 2026-07-24 — ADR-025 S1.4b: data plumbing + a build-tag correction

S1.4b landed the safe, fully-inert data foundation for the emission: the
loadRecords `LEFT JOIN initiative_setup_receipts isr ON isr.initiative_id = t.id`
setting `dispatch.Task.InitiativeSetupDone`, and `Config.RealRouting =
!allowFakeDecorrelation` in selectFromSpine. Both flow into Decide but stay
unused (nextInitiativeSetup isn't called yet — S1.4c), so zero behavioral change
and no latent trap. Test: loadRecords projects InitiativeSetupDone true for a cut
initiative, false for an uncut one, structurally false on a standalone task.

Build-tag correction to the S1.4a fake-safety framing (IMPLEMENTATION-NOTES
2026-07-23): `routing.ActiveRegistry()` is BUILD-TAG-selected —
`registry_active.go` (default, `!test_fake_routing`) returns allowFake=FALSE;
`registry_active_fake.go` (`test_fake_routing`) returns TRUE. The fast suite runs
`go test ./...` on the DEFAULT build, so RealRouting = !false = TRUE there. So:
(1) the Phase 4 FAKE lane (its E2E carries `//go:build test_fake_routing`, e.g.
mc/e2e/e2e_test.go) sees RealRouting=FALSE — the gate protects it, as designed.
(2) Default-build SEAM tests (dispatchverb/dispatchseam, production registry)
see RealRouting=TRUE — they are genuinely real-routing, so once S1.4c wires the
emission, any that promote an uncut initiative through the full
selectFromSpine→Decide path will CORRECTLY switch to KindInitiativeSetup and must
be updated to expect it (not a regression — the right behavior). Pure-dispatch
tests (dispatch_test.go) use DefaultConfig (RealRouting=false) and are unaffected
unless they set it. This nuance is the key S1.4c gotcha: audit default-build
full-path tests for promoted-uncut initiatives before wiring the emission.

NEXT: S1.4c — the atomic emission + route-free commit + mount-plan unit (see
PROGRESS NEXT).

## 2026-07-24 — ADR-025 S1.4c-1: the InitiativePrecreate mount-plan step

S1.4c (dispatch emits + commits the InitiativeSetup) is the largest slice in
ADR-025 — a whole new dispatch lane. A second Plan-agent trace mapped it fully
and, reassuringly, the build-tag test audit found NO existing default-build test
breaks: the only branch-set seeded initiative fixture
(TestDispatchLoadRecordsProjectsInitiativeSetupReceipt) calls only loadRecords,
and every other initiative fixture is branch-LESS so nextInitiativeSetup's
`Branch != ""` conjunct keeps it inert even under RealRouting=true. (One latent
oracle mismatch to fix in S1.4c-2: dvConfig leaves RealRouting false while the
real Dispatch sets it true.)

Effect-model decisions from the trace: the InitiativeSetup fuses the LANDING lane
(route-free, host-attested-for-its-mount-plan, subjectless-refusal-class) with
the SPAWN lane's lease claim — it is the only lane that claims the lease + opens
a run (tier='pipeline', role='worker'; "Worker-tier" = role worker, there is no
worker tier) while carrying NO route/brief/harness. The arc row's
SubjectInitiativeID is NIL (it IS the initiative), so it cannot reuse the S2
child attest arm; the lane needs its OWN route-free attest. The hardest part is
fresh/retry from ON-DISK residue: the receipt IS the assignment, so when absent
there is no spine pin, and the retry ref/OID is recovered from the on-disk store
and re-proven only through the DeepEqual(mountState)/token/recheck fences.

S1.4c-1 landed the inert carrier + validation (additive, omitempty keeps every
existing plan's mountPlanDigest byte-identical; nothing authors it yet):
- PrivateDispatchInitiativePrecreate (mountplan.go): two proven-absent parents
  (store `.mission-control/initiatives`, worktree `.mc-worktrees` — separate
  bases, D1), paired recovery roots, a fresh/retry Setup (reusing
  PrivateDispatchTaskSetup), + the omitempty field on PrivateDispatchMountPlan.
- validatePrivateMountPlan arm (dispatchprivate.go): closed setup class — mutual
  exclusion with every other setup step + no agent entries; fixed parent paths;
  decimal identity fences; derived recovery-child paths; paired-or-neither
  recovery. Tests cover valid fresh + valid retry + a malformed table
  (id/mode/parent-path/device/owner/setup/workspace/entries/sharing/lone-recover/
  wrong-recover-path).

NEXT: S1.4c-2 — the atomic lane (see PROGRESS NEXT): Decide emission +
assertWellFormed + dvConfig fix; preparedDispatch.initiativeSetup +
dispatchInitiativeSetupRound + prepare divert; route-free
dispatchAttestInitiativeSetup + captureInitiativePrecreate (two parents,
fresh/retry from residue); dispatchCommitInitiativeSetup + applyInitiativeSetup
(lease claim, worker/pipeline run, no route/brief; receipt). Then S1.5 (resident).

## 2026-07-24 — ADR-025 S1.4c-2a: captureInitiativePrecreate (the hard part)

The route-free InitiativeSetup lane (S1.4c-2) is large enough to split; this
landed its single hardest, most novel piece — the attest-side authoring of the
shared-store precreate step — as an inert helper (nothing calls it until the
S1.4c-2b lane wires in). It runs host-side and never runs Git (ADR-016 D5); it
only reads administrative files.

`captureInitiativePrecreate(ws, id, ownerUID, targetRef)`: proves the two
operator-owned mode-0700 parents (`.mission-control/initiatives` for the store,
`.mc-worktrees` for the shared worktree — separate host bases, D1), then decides
fresh-vs-retry from the `initiative-<id>` children's presence:
- both absent → FRESH: cut at the current tip of the arc target ref
  (probeRepoObjectFormat for the format).
- both present → RETRY over on-disk residue. This is the genuinely new logic:
  the initiative has NO spine assignment to restate (the setup receipt IS its
  assignment, D3), so the retry pins are RE-DERIVED from the landed bytes — cut
  SHA from `git/refs/heads/mc/initiative-<id>`, object format + repo UUID parsed
  from the closed-grammar `git/config` (validateTaskGitConfig first), closure
  digest from `digestLandedPack` — never re-resolving main. The two roots
  (store 0555, worktree 0700) are proven present and carried as recovery roots.
- partial/mixed (one child present) → refuse.

Tests (host-side real git): fresh cut from the target ref; a retry whose pins are
re-derived from a store built by MaterializeInitiativeStore and match its
SetupResult exactly; and refusals (partial state, non-0700 parent, absent
parent). Full fast suite green.

NEXT: S1.4c-2b — wire the lane (emission→route-free attest→commit) around
captureInitiativePrecreate; see PROGRESS NEXT.
