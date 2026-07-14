# ADR-016 — Cross-boundary prepare/attest/commit dispatch

- Status: Accepted
- Date: 2026-07-13
- Where the spec delegates: §§10–12 place canonical selection and mutation
  inside the lock domain while only the native resident can see the host file
  plane and container runtime. §§11.3–11.4 require host-path and runtime
  applicability before claim. ADR-009/012 make Homie start/resume record-only
  and delegate the later eligible wake to the resident tick.

## Context

On the primary macOS target, one process cannot both open the runtime-local
spine and inspect native Worksource paths and Docker state. The Linux `mc` in
the helper sees the spine; Darwin `mc` sees the host file plane and runtime.
Mounting all of `MC_HOME`, arbitrary host roots, or the Docker socket into the
helper would expand its authority and expose credentials. Letting the resident
write state would break Inv. 2. Claiming before boundary validation would break
the Phase-3 fail-closed inversion.

The ordinary external contract remains one real `mc dispatch` command with one
final JSON result. There is no service, mailbox, durable plan file, or third
source of truth.

## Decision 1 — one external command, private same-binary composition

Darwin `mc dispatch` is a trusted broker around two private invocations of the
same release's Linux `mc` in the helper. Production dispatch is resident-only:
the resident gives the Darwin child ADR-018's one-shot inherited control file
descriptor, and the broker refuses to prepare without the matching
build/schema/deployment handshake. That channel carries only secret-free
gateway capability/binding attestations and temporary probe registration; it
is not a spine RPC and is never inherited by an agent. The helper composition
is:

1. **Prepare.** Before invoking the helper, the broker reads the fixed
   non-symlink regular `MC_HOME/deployment.uuid` mirror through the safe-root
   opener and includes that UUID in the closed request. As its first inert
   precondition, before selection or mutation on *every* branch, the helper
   requires the supplied UUID to equal `meta.deployment_uuid` in the mounted
   spine. The broker then supplies a strict non-secret runtime/capability
   snapshot and exact labeled inventory to `mc __dispatch-prepare`. Under the
   dispatch flock and `BEGIN IMMEDIATE`, the helper reads only spine Records,
   Lock, Profile, Homie, schedule, and tunable state. It first executes the
   single-action reconciliation order in Decision 3. A branch whose entire
   consequence is lock-domain-owned commits where required and returns the
   final result immediately with Decision 2's request receipt. Spawn, wake,
   and land require native file/runtime authority, so each returns a bounded
   logical candidate and preparation token for the remaining two steps.
2. **Attest.** The Darwin broker reopens the non-symlink regular
   `MC_HOME/deployment.uuid` mirror and again requires it to equal the spine
   deployment UUID supplied by prepare. Only then may it read the rest of that
   root. It reads authoritative host-side `config.toml`, `mount-allowlist`,
   and, for a pipeline spawn candidate, `routing.md`; resolves the native file
   plane; and applies the shared pure boundary planner. An existing Homie resolves its frozen historical
   `harness/binding` directly and never parses or depends on current
   `routing.md`. For an agent start the broker performs the mandatory exact
   candidate probe described by ADR-018. A land candidate instead attests
   ADR-017's exact task-store/real-repository Git views, landing image/resource/
   security tuple, and fixed receipt plan without a gateway probe. It then
   re-reads the candidate-specific host files,
   rechecks the deployment mirror and complete file trust predicates, and
   obtains a fresh runtime/inventory snapshot. Any relevant byte, trust,
   identity, deployment, or inventory change abandons the candidate as stale.
3. **Commit.** The broker sends the candidate, attested host projection, exact
   plan or classified refusal, and the fresh snapshot to
   `mc __dispatch-commit`. Under the same flock and a new `BEGIN IMMEDIATE`, the
   helper reloads and re-decides lock-domain truth, requires the fresh snapshot
   to equal the prepared snapshot byte-for-byte, verifies the preparation
   token, and applies exactly the reselected candidate's consequence. It never
   falls through to another candidate.

Prepare exits and releases both its transaction and process flock before any
host or runtime I/O. Commit is a separate invocation that reacquires the flock
and redecides; no claim relies on one lock surviving attest. Concurrent
prepares may do redundant probes, but only one commit can match current state.

The helper does not and cannot reread host files. Raw `config.toml`,
`routing.md`, and allowlist bytes may contain operator or credential material
and never enter a helper/private/final frame. The trusted host broker instead
attests each relevant raw-file byte length and digest plus a closed,
explicitly non-secret effective planning projection. It rechecks raw bytes and
trust immediately before commit. The lock side authenticates those digests and
the projection to the prepared candidate and rechecks only the spine
projection it owns. The helper mounts the spine named volume and nothing from
`MC_HOME` or the host credential/file plane. `mc backup` may separately
receive only its fixed backup-target bind. Tests prove dispatch route
validation works while the helper has no config, allowlist, routing,
credential, Worksource, runtime socket, or run-envelope mount, and prove a
credential-shaped config value never appears in a frame or log.

The broker suppresses private frames. The resident receives one ordinary JSON
object and exit status. All other verbs keep S2's whole-command byte/exit
passthrough. Private verbs require fixed host scope, a matching build/schema
identity, and are absent from agent capabilities and operator documentation.
Production ignores `MC_RUN_JSON`, `MC_SPINE`, helper-name, and internal-mode env
overrides; test seams use constructors or test tags.

On native Linux the resident calls the same prepare/attest/commit functions
locally in one process, deliberately releasing the transaction/flock across
attest I/O just as Darwin does. It does not runtime-exec itself. Darwin alone
uses same-binary CLI self-delegation over the runtime exec channel. Neither
path adds a persistent protocol endpoint or durable handoff.

## Decision 2 — complete, replay-safe tokens and bounded schemas

At the start of one external command the broker allocates a
`dispatch_request_id` of exactly 16 lowercase hex and reuses it across any
transport retry within that command. It is included in every prepare/action
token but is not canonical work state. A later resident tick receives a new
id. Prepare allocates a candidate identity before claim only when canonical
state has none:

- pipeline `run_id`: exactly 16 lowercase hexadecimal characters, unique at
  commit;
- Homie `launch_id`: exactly 16 lowercase hexadecimal characters, scoped to
  one launch generation of the immutable `h-<16-lower-hex>` session, and
  allocated only when `current_launch_id IS NULL`. Recovery tokens bind and
  reuse the stored id/mode rather than sampling a replacement.

The random candidate is not state during prepare. It is bound into the token
and becomes canonical only when a pipeline claim or Homie wake commits; a
stale attempt is discarded. A Homie wake transaction persists the selected
launch id on its registry row before returning any effect, as Decision 3 pins.
Container, network, resolver, projection, envelope, and gateway handle names
are deterministic functions of the deployment identity plus `run_id`, or the
Homie `session_id + launch_id`. No post-commit random handle is invented by the
resident. ADR-018's secret launch nonce is deliberately not a plan field.

The preparation token is SHA-256 over the domain-separated bytes
`MC-DISPATCH-PREPARE-V1\0 || canonical_prepare`. Its closed typed projection
contains:

- deployment UUID and build, protocol, schema, and config versions;
- the canonical Records/Lock/Profile/Homie projection and selected action;
- every selected Worksource/Profile field relevant to confinement, including
  the complete cross-Worksource and protected-root catalog;
- selected role/session, immutable route locator or requested route role,
  brief/continue locator, mount/env/auth/network/resource inputs, candidate id,
  and tier; and
- the prepared capability and labeled-inventory snapshot.

The host attestation carries raw-file byte lengths and SHA-256 digests, the
closed non-secret effective planning projection, the complete validated plan,
and native path/trust evidence. A pipeline **agent** token binds all three
host-file digests; a land token binds deployment config/allowlist plus its Git
trust/effect projection but no routing or model binding. A Homie token binds
config and allowlist but excludes current `routing.md` entirely. It instead binds the registry's frozen historical
harness/binding and its exact resolved capability/auth/image projection. The
plan digest uses
`MC-DISPATCH-PLAN-V1\0 || canonical_plan`. Canonical encoding is the
version-matched Go encoder over closed structs only: UTF-8 strings, decimal
integers, explicit booleans/zero values, declared struct-field order, no maps,
interfaces, floats, insignificant whitespace, or duplicate keys. Semantically
unordered arrays are sorted by their declared key before encoding; ordered
argv is not. Unknown fields and trailing data reject.

Every private or final frame is at most 1 MiB. A plan permits at most 256
mounts, 256 environment entries, 64 argv entries, 32 labels, and 128 network
rules. Identifier, enum, path, environment, label, hostname, and argv fields
are bounded UTF-8 text: one such field is at most 16 KiB, an ordinary scalar
is at most 4 KiB, and a container-path component is at most 255 bytes. NUL and
ASCII control characters reject in those structural fields.

Opaque documents are a different closed type: `{byte_length,sha256,base64}`
with canonical padded base64. Briefs, `run.json`, sandbox/network projections,
and other authored document payloads may contain LF and TAB. Each is at most
512 KiB decoded, their aggregate decoded bytes are at most 640 KiB, and the
outer 1 MiB frame cap still wins. Raw config/routing/allowlist documents are
digest-only and are never document payloads. Environment and label names,
mount destinations, logical mount ids, and network-rule keys are unique.
Numeric/type confusion, duplicate entries, bad length/digest/base64, oversize
input, version/digest substitution, and hostile structural text all reject
before mutation. Tests accept multiline documents at the declared edge and
reject the next byte.

The runtime snapshot is a sorted bounded stable projection, never raw Docker
inspect output. Each managed object contributes container id, exact name,
image/config digest, exact Mission Control labels, and the closed Docker state
`created|running|paused|restarting|removing|exited|dead`; it also includes
guard/gateway registration and stable runtime/network/resource capability
epochs. `paused`, `restarting`, and `removing` are ambiguous non-replaceable
states: reconciliation may exact-stop/remove them but cannot treat them as
healthy or absent and cannot create a replacement until confirmed absence.
`exited` and `dead` require exact cleanup before confirmed absence. Volatile
timestamps, uptime, counters, retry counts, and health-check samples are
excluded, so
harmless observation drift does not stale a candidate while replace, relabel,
image/config, lifecycle, or capability changes do. A second closed inventory
lists only derived ephemeral envelope, sandbox, resolver, network, setup, and
landing artifacts by logical id and filesystem identity; it never scans or
lists `sessions/`.

Inventory is bounded in memory, not truncated by count. The broker streams
native runtime and derived-file observations into 128-item sorted runs in the
first of exactly two request-local spool files. Bounded fan-in merge passes
alternate between those files; embedded run-length headers and offset-based
reads allow at most 16 current run cursors plus one 128-item output page, not
one descriptor or in-memory index per run. Both files are created `O_EXCL`
mode 0600 beneath the identity-checked operator-only
`MC_HOME/state/dispatch-spool`, opened without following symlinks, and
immediately unlinked; a crash closes the two descriptors and leaves no named
artifact. They contain only the same closed non-secret inventory projection,
never config, credentials, session bodies, or raw inspect data.

The broker makes one bounded sequential pass over the final run to compute the
exact item count and domain-separated rolling SHA-256 root, rewinds it, emits
that header, then streams canonical pages of at most 128 bounded items per
private frame through the already-open helper invocation. The helper
processes pages incrementally, retains only the deterministic best action
candidate and rolling hash state, and requires count/root/EOF equality before
mutation. There is no “257th object” refusal and no fixed total page count.
After the count is known, the helper invocation receives a finite
count-derived wall allowance of `120 seconds + 5 seconds * page_count`, while
each expected page has a five-second no-progress deadline; local enumeration
and external sorting likewise use per-read/no-progress deadlines, not a fixed
total deadline that a sufficiently large but progressing finite inventory can
always exceed. Commit carries the recomputed count/root plus the selected exact
object rather than replaying an unbounded array. Changed order/content/count is
`preflight.inventory_changed`. Thus every finite progressing accumulation can
be scanned and one exact cleanup selected without an unbounded frame,
unbounded broker memory/file descriptors, or a count-threshold liveness wedge.

The 256-mount and 1-MiB plan bounds are also admission invariants, not a new
dispatch-time trap for otherwise valid state. The same pure worst-case plan
budgeter runs before Worksource add/update, Sandbox Profile save/update,
artifact/reference registration, and `homie start`. It materializes every
pipeline role shape plus the all-Worksource Homie shape under the resulting
catalog and refuses the canonical mutation if any closed plan would exceed a
collection, field, or frame bound. A mutation that enlarges one Worksource is
checked against existing Homies and cross-Worksource propose scope too. Phase-3
schema migration and onboarding apply the same check before accepting an
existing deployment; `doctor` names the offending logical entries. Raw
deployment-file drift can still produce health until repaired, but no state
accepted by these writers first becomes undispatchable merely because its
valid cardinality crosses a hidden plan threshold.

Every committing branch carries
`dispatch_key = SHA256("MC-DISPATCH-ACTION-V1\0" || preparation_token ||
canonical_action)`. Phase 3 adds a nullable unique dispatch key to the
append-only activity record. It adds nullable
`outbox.source_activity_id REFERENCES activity(id)` plus a nullable unique
`event_destination_key`, computed over the activity id and a tagged
null-or-value destination tuple. The transaction first returns an existing
same-key result or inserts the activity, state consequence, and outbox fan-out
once. Thus replaying a matched private prepare/commit cannot
duplicate health/activity/outbox even when work state itself is unchanged. A
separately prepared later tick is a new observation and follows the ordinary
health-event policy; it is not a replay of the lost response.

Prepare-side mutations need a second, earlier fence because their first write
can change what re-selection would choose. Phase 3 also adds paired nullable
`dispatch_request_id` and `dispatch_result` fields to append-only activity;
the request id is unique in this deployment, exactly 16 lowercase hex, and the
closed canonical final result is at most 64 KiB. Reap, recovered health,
Homie end, reenter, and every other mutation that returns directly from
prepare atomically insert their exact request receipt with the state/activity/
outbox consequence. Before reading selection state, prepare looks up that id
and returns the stored result byte-for-byte. Thus a lost response cannot use
the same external command to reap and then claim, end two Homies, or reenter
twice. A non-mutating prepare result needs no receipt: if its response is lost,
an immediate retry has not yet caused a state mutation or host effect and may
re-evaluate once. `dispatch_key` remains the commit-side idempotency fence for
attested spawn, wake, land-refusal, block, and health consequences.

Commit reconstructs its owned canonical bytes, never trusts a caller's hash,
and requires the candidate action/identity to match. Ordinary state, config,
or host-file drift is `preflight.stale`; inventory drift is
`preflight.inventory_changed`. Both mean no block, health write, Run, lease,
registry mutation, or effect. A concurrent operator edit therefore never
falsely punishes a task. Malformed, replayed, substituted, or
version-mismatched private frames are protocol errors with the same inert
consequence.

## Decision 3 — exact one-action ordering

Phase 3 extends the canonical `homie_sessions` registry with launch fencing,
not with a plan queue:

- `current_launch_id TEXT NULL`, exactly 16 lowercase hex when present;
- `current_launch_mode TEXT NULL`, exactly `fresh|native|rows`;
- `current_prime_through_seq INTEGER NULL`, non-negative;
- `current_prime_row_count INTEGER NULL`, non-negative;
- `current_container_id TEXT NULL`, exactly one 64-lowercase-hex Docker id
  when present;
- `launch_bound_at TEXT NULL`;
- `launch_started_at TEXT NULL`;
- `resume_owed INTEGER NOT NULL DEFAULT 0`, exactly `0|1`;
- `resume_mode TEXT NULL`, exactly `native|rows`; and
- `resume_prime_through_seq INTEGER NULL`, non-negative; and
- `resume_prime_row_count INTEGER NULL`, non-negative.

Current launch id and mode are both null or both present. Container id and
bound time are paired and require a current launch; a start time requires the
bound pair. Only `rows` mode carries the paired prime cutoff/count. Resume debt is represented
by `resume_owed=1` plus a mode, with a paired cutoff/count only for `rows`, and is mutually
exclusive with a current launch. `homie start` initializes every launch/debt
field empty/zero. A successful native or explicit row-fallback resume
atomically clears the prior launch/container/times, sets the typed resume
debt, and thereby supersedes every old generation before a new one can be
selected. This is canonical liveness fencing only: no argv, mount, credential,
nonce, or launch plan is persisted.

Each external call returns immediately after the first selected branch. It
never commits/effects two actions:

1. **Pipeline lease reconciliation.** This is spec §10 step 0 and therefore
   first. “Fresh” means both temporally fresh under §10 and consistent with an
   exact healthy execution envelope: during the first-heartbeat
   `spawn_grace_s`, confirmed absence or an exact created/startup shape may be
   an incomplete effect, but an observed foreground setup action from a prior
   single-flight tick is explicit crash residue and makes the envelope
   unhealthy immediately. Once running, agent, guard, gateway registration,
   config digest, and labels must all match. A mismatched existing agent or
   lost/unhealthy guard/gateway is immediate post-commit infrastructure reap,
   even before the temporal timeout: commit the ordinary retry charge/freeing
   semantics and return the exact agent stop. Otherwise a §10-temporally
   reapable lease commits the reap and returns its exact stop/cleanup effect.
   Only a temporally and envelope-fresh held lease returns
   `idle(lease-held)` immediately. No recovered diagnostic, Homie transition,
   landing, wake, or second cleanup action passes that “Fresh → return.”
2. **Recovered-crossing diagnostic.** If Docker previously made the helper
   unreachable, the broker supplies one bounded local failure record on the
   first recovered crossing. The helper appends that health event and returns;
   selection resumes next tick.
3. **Orphan and ephemeral cleanup.** Against the supplied exact container and
   derived-artifact inventory plus spine liveness, select at most one orphan in
   deterministic `(component,tier,identity,name)` order. Pipeline identity must
   match canonical Run/lease residue, Worksource, name, labels, and healthy
   guard/gateway envelope. Every existing Homie container is live for orphan
   classification only when its exact session/launch/container id and envelope
   match an active registry row; an exact active row remains live here even at
   the idle threshold, because branch 6 alone owns `active -> ended`. An exact
   `created`, `paused`, `restarting`, `removing`, `exited`, or `dead` container
   bound to a current launch is
   reserved for branches 4–7 rather than treated as an orphan only while its complete
   original guard, namespace, resolver, projection, resource tuple, and
   ADR-018 runtime-registration handle/generation all attest unchanged and
   live. If any part is absent or mismatched, cleanup owns that pre-start
   infrastructure here: exact-stop/remove the agent first, then its guard and
   derived artifacts on later confirmed snapshots, while retaining the same
   canonical launch id/mode for branch 7 to recreate only after the old
   envelope is confirmed absent.
   A boundary probe is live only for its unexpired prepare token. Because one
   resident tick runs at a time, any setup or landing action container visible
   to a later tick is recovery residue from a dead/interrupted foreground
   effector, even if its Run or landing fact remains current. Branch 3 always
   exact-stops/removes that object and confirms absence before setup is rerun
   or receipt-idempotent landing is retried; no invented durable “active
   action” state can keep it alive. Landing has a fixed 15-minute foreground
   wall deadline. Pipeline setup is bounded by
   `min(15 minutes, remaining first-heartbeat spawn_grace_s)` and must abandon
   before that grace expires; Homie setup is bounded by
   `min(15 minutes, remaining homie_idle_timeout)`. Timeout performs exact
   cleanup rather than starting stale work. Agent
   containers sort before their network guards, then files sort after their
   owning container. An `--rm` disappearance does not hide a closed derived
   envelope/sandbox/resolver/network/setup/landing artifact set: when its
   execution identity is not live, return one exact safe cleanup effect. The
   inventory never includes or removes a session folder. Helpers and unrelated
   containers are excluded exactly as Decision 7 states. Every stop names the
   observed Docker container id, exact name, and complete identity labels; name
   alone is never authority. A failed or ambiguous stop/cleanup keeps selecting
   this branch and can never fall through to replacement. This stale-writer
   safety cleanup remains ahead of Console/landing despite §7's nominal
   one-tick landing latency; the bounded one-object cleanup deviation is logged
   and cannot be displaced by ordinary Homie exit/idle housekeeping.
4. **Pipeline control priorities.** Evaluate §10's table without claiming. A
   due Console returns its step-0b spawn candidate for the ordinary
   prepare/attest/commit path immediately, ahead of every Homie action. A
   landing-pending row is next. Its deterministic `landing_id`, task-store
   identity, accepted base, verified SHA, target ref, and current pending
   receipt form an attested candidate rather than a bare effect. Any observed
   landing action object was already owned by branch 3 cleanup; only confirmed
   absence returns the candidate for host Git/mount/image/resource attestation. Commit
   rechecks the entire pending tuple and inventory before returning the frozen
   landing plan. The receipt-idempotent `mc-land` may then adopt an exact
   prior Git receipt or retry; it never guesses from a name. A queue result
   requiring pure `reenter` commits it with the request receipt and returns
   before Homie. An all-saturated result is retained as the eventual idle
   result rather than hiding housekeeping. Otherwise retain the one ordinary
   pipeline spawn candidate (including Refiner or propose) for branch 7. A
   standalone downstream role that consumes Worker output is not a candidate
   until the accepted completion references the exact privileged seal and the
   runtime inventory confirms the producer agent/guard envelope absent; a
   surviving producer is branch-3 cleanup first. Verifier additionally binds
   the disposable-source materialization operation to that accepted seal.
5. **Homie launch/exit reconciliation.** Rows at the idle threshold are not
   actionable here; branch 6 owns them even when their launch is absent,
   created, or starting. Among the remaining rows choose at most one actionable
   case in deterministic `(case_rank,last_activity_at,id)` order: (0) an exact
   `paused|restarting|removing` current container returns its exact-id
   stop/remove effect and remains non-replaceable until confirmed absent, (1)
   an exact `exited|dead` current container returns its exact-id remove/cleanup
   effect, (2) a `launch_started_at` generation whose exact container is then
   confirmed absent atomically applies launch-fenced `active -> ended` with
   reason `exited`, or (3) an unstarted exact running container older than the
   existing deployment `spawn_grace_s` returns its exact-id stop. After
   transitional/terminal cleanup, confirmed absence ends a started generation
   and leaves an unstarted generation as branch-7 effect debt. The resident
   normally invokes the same lifecycle transition immediately when it observes
   exit; the tick is recovery for a resident outage. A current launch that was
   never runner-started remains durable effect debt even after resume debt was
   cleared. An exact `created` container is eligible in branch 7 to re-inspect
   and start without creating another only when its full original envelope and
   live runtime registration still attest exactly. An unstarted exact running
   container still within `spawn_grace_s` is skipped as non-actionable so it
   cannot hide another exit, idle end, or pipeline control action. A generation
   marked started is never silently restarted.
6. **Idle Homie end.** Choose at most one active row at or beyond
   `homie_idle_timeout`, ordered `(last_activity_at,id)`. In one transaction
   apply the existing `active -> ended`, binding deactivation, and
   `homie.ended` activity semantics, return its stop/cleanup effect, and stop.
   The stop, when a matching container exists, is fenced by its persisted
   launch plus observed container id. The row stays resumable and cannot
   immediately wake. Threshold equality belongs here, never branch 3.
7. **Lease-free spawn choice.** First choose the oldest
   `(last_activity_at,id)` active, non-idle Homie that either
   carries an unstarted current-launch effect debt or has no launch and has a
   pending/incompletely claimed inbound turn or `resume_owed=1`.
   `homie start` without later inbound traffic is not eligible. An exact
   running current container is not eligible except for branch 5's starting
   grace. If the row has no launch, commit persists the prepared candidate id,
   mode (`fresh` for inbound, otherwise the resume mode), and optional prime
   cutoff/count pair, then clears resume debt before returning `wake`. If it already has
   an unstarted launch, recompute and attest the same mode/id; only after the
   old agent, guard, registration, namespace, resolver, projection, and other
   derived artifacts are all confirmed absent does it clear a stale bound pair
   and return create. An exact `created` container returns an adopt-and-start
   effect only when its complete original resource/network envelope and the
   resident's live ADR-018 registration handle/generation match the snapshot;
   otherwise branch 3 must dismantle that envelope first. A Homie-only
   deployment-health refusal appends a `homie.preflight_health` activity whose
   closed detail carries `candidate_key = SHA256` over only the pre-prepare
   canonical session id, current launch/input-or-resume debt, frozen binding,
   and relevant conversation sequence, plus `defer_pipeline=true`. If an
   ordinary pipeline candidate is retained on a later free tick and the latest
   unconsumed marker has the same candidate key, that pipeline candidate wins
   unconditionally before the Homie is attested again. Its later claim or
   terminal candidate disposition consumes the marker by activity order. With
   no pipeline candidate, the Homie may retry every tick; a changed canonical
   session/input key is also immediately eligible. This deliberately costs at
   most one unnecessary pipeline turn after a host-only repair, while ensuring
   a broken projection/attachment mechanism cannot wedge the pipeline tier.
   If no Homie is eligible, commit/effect the retained ordinary
   pipeline candidate or return the retained all-saturated idle result.
   Existing Homies use only their frozen historical binding; malformed or
   changed current `routing.md` cannot make them stale or ineligible.

After create and exact inspect, but before start, the resident invokes a
private `homie.launch-bind` receipt that CASes
`active + current_launch_id + current_container_id IS NULL` and stores the
exact Docker id plus `launch_bound_at`; it also requires lock-domain `now` to
remain strictly before `last_activity_at + homie_idle_timeout`. Repeating the same active launch plus
same Docker id is an idempotent success that returns the original bound
receipt/time; a different id is fenced. A lost bind response therefore never
forces a new container. `homie.runner_started` is written
through the runner's private lifecycle scope only after the native harness has
successfully started or resumed; it CASes active session, launch id, and bound
container id, repeats the non-idle time check, and stamps
`launch_started_at` idempotently. All later runner
transport, locator registration, model-scoped Homie verbs, launch failure, exit, liveness, and
stop decisions carry `run.json`'s launch id and must match the canonical
current launch. Transport/model calls also retain their existing own-session
and frozen-allowlist checks.

The resident-observed exit verb is private host scope:
`mc homie exit <session> --launch <id> --container-id <docker-id>
--reason exited`. It CASes active/current launch/current container and is
idempotent. If `launch_started_at` exists or native locators are established,
it ends the exact session. For a truly pre-runner-start null-locator launch it
instead clears the confirmed-exited bound pair and retains the same launch/mode
as effect debt. An ordinary operator or allowlisted model `homie end`
intentionally ends the current session without accepting caller-supplied
fencing; subsequent physical stop is still exact-id/launch fenced by dispatch
inventory. A late bind/start/transport/failure/exit from an old launch returns
`fenced:false`, cannot end a resumed launch, and cannot stop the new container.

ADR-012's explicit conversation-row fallback is completed here as host-only
`mc homie resume <session> --from <surface:channel_ref> --from-rows`. Native
resume remains the default and still requires immutable locators; there is no
implicit downgrade. The v1 row fallback is accepted only when both locators
are null. In its resume transaction it captures the highest sequence in the
fully completed conversation prefix (never a pending inbound) as
`resume_prime_through_seq` plus its exact row count as
`resume_prime_row_count`, sets mode `rows`, and otherwise follows ADR-012's
binding/status/idempotency rules. An empty completed prefix has the single
closed encoding `(resume_prime_through_seq=0,resume_prime_row_count=0)`; zero
is a sentinel below every stored conversation sequence, not a row identity.

For a rows launch, the runner uses its private own-session/launch-fenced scope
to fetch at most the newest 127 row-metadata entries at or below that fixed
cutoff, in pages of exactly 64 (the final page may be short), before starting
a fresh harness. The `(0,0)` prefix fetches no metadata or bodies and emits no
aggregate omission marker; the original pending inbound remains claimed only
through the ordinary post-prime path. Conversation insertion stores each
body's byte length and SHA-256, so an omitted body need not be fetched.

The runner first constructs the mandatory loss-only representation: one
`{seq,sha256,omitted:true}` marker for each selected metadata row plus, when
the captured count has older rows, exactly one leading
`{through_seq,row_count,omitted:true}` aggregate marker. The bounded structural
grammar guarantees this at-most-128-entry baseline is below 512 KiB. Walking
selected rows from newest to oldest, it replaces a row marker with the
canonical full-body entry only when the metadata length proves the replacement
keeps the **entire decoded transcript document**, including every marker and
delimiter, at or below 512 KiB. Only then does it fetch and verify that body's
length/digest. A body that individually or cumulatively does not fit retains
its marker; later smaller bodies may still replace their own markers. Final
serialization is chronological: aggregate marker first, then selected rows in
ascending sequence. V1 deliberately does not hash or read aggregate-omitted
contents at launch. Bodies are never truncated, and omission cannot expand the
transcript beyond either cap.

The adapter supplies that closed transcript document as priming context, then
the runner claims the still-pending inbound turn normally. Newer rows are not
silently folded into a replay. The new harness registers its first immutable
locator pair before `homie.runner_started`. Thus a candidate-policy refusal may
atomically end even a never-launched Homie without permanently stranding its
conversation; repair plus an explicit row resume starts from durable visible
history. A future native-format fallback for a non-null but incompatible
locator pair requires a new lineage ADR and is not inferred here.

A fresh session remains driven by its durable incomplete inbound row. Tests
pin start-without-send, send, claimed-but-incomplete, resume, resume after an
old container survives, active-with-no-debt, threshold equality, multiple idle
rows, fresh-held lease, lost resume-wake response with no inbound, exact-created
adoption, starting-grace expiry, first-launch refusal/end then explicit rows
resume with only its original pending inbound and the `(0,0)` prefix, priming
cutoff/size/omission, same-generation pre-start retry, stale
old-launch exit/transport/model verbs, and repeated ticks.

Broken spawn routing/gateway inputs cannot suppress lease/orphan cleanup,
reenter, or an already-pending land. Spawn-only inputs are read only after a
higher-priority branch selects an actual spawn/wake candidate. Land itself now
attests its own config, image/resource/security, allowlist, file-plane, and Git
dependencies: their failure records deployment health and leaves the pending
fact intact rather than emitting a bare or weakened land effect. A due Console
is intentionally still §10 step 0b and can produce its own health refusal
before landing, exactly as the winning control table specifies.

## Decision 4 — exact refusal consequences and stable codes

The v1 consequence classes are closed:

| Class | Stable codes | Consequence |
|---|---|---|
| Stale/protocol | `preflight.stale`, `preflight.frame_invalid`, `preflight.version_mismatch`, `preflight.candidate_mismatch`, `preflight.inventory_changed`, `preflight.oversize` | Error/retry; no durable mutation or effect. |
| Deployment health | `health.runtime_unavailable`, `health.helper_unavailable`, `health.image_unavailable`, `health.gateway_unavailable`, `health.network_capability_unavailable`, `health.resource_capability_unavailable`, `health.resource_config_invalid`, `health.file_plane_unavailable`, `health.projection_unavailable`, `health.config_invalid`, `health.routing_invalid`, `mount.allowlist_untrusted`, `mount.allowlist_invalid` | One health action; no claim or task charge/block. If the crossing is down, record it on the first recovered tick. |
| Candidate policy | ADR-017's exact `mount.*` set, except the two deployment allowlist-trust codes above, only when the failing source authority is the candidate's profile/Worksource/record reference or the candidate introduces the destination collision; `env.invalid`, `env.forbidden`; `auth.binding_invalid`, `auth.delivery_invalid`, `auth.ca_binding_mismatch`; `network.rule_invalid`, `network.rule_unresolved`, `network.destination_forbidden`, `network.policy_unappliable`, `network.policy_mismatch`; `identity.name_invalid` | Subject task: atomically block with code. Subjectless pipeline: health. Homie: launch-fenced end with `confinement:<code>`; a null-locator conversation remains explicitly resumable through Decision 3's `--from-rows` arm after repair. Never claim/spawn. |

Global absence of nf_tables, the shared DNS/probe binary, cgroup controls, the
base image, or a correctly configured shared gateway/CA is deployment health.
A failure of a closed typed-system source or mechanism—including the release
runner source, run/session envelope, generated resolver/network roots, trusted
global file plane, clean committed projection builder, or the required
same-filesystem hardlink capability—is translated to
`health.file_plane_unavailable` or `health.projection_unavailable`; it never
charges, blocks, or ends the selected subject. The same low-level
`mount.runtime_unappliable` is candidate policy only when Docker rejects that
candidate's profile/Worksource-authorized mount rather than a shared typed
system mount.
A current candidate's invalid rule, resolved forbidden destination, mount,
environment, auth binding, or exact compiled-policy refusal is candidate
policy. The deployment-only resource tuple and allowlist file trust are health.
A wrong global CA is health; a CA/binding mismatch in one candidate is policy.
No class downgrades or silently drops a rule.

Landing has a narrower consequence boundary. Its never-started mount/resource
application canary uses the landing image with no gateway; shared Docker,
typed-file-plane, image, resource, create/inspect, or fixed foreground-deadline
failure is deployment/infrastructure health and leaves `landing-pending`
intact for exact cleanup/retry. Only the fixed `mc-land` program's semantic
Git refusal after a valid container starts—SHA/base/target mismatch, dirty
conflict, or receipt inconsistency—invokes `mc land report failure` and blocks
the task with the existing landing reason. Runtime failure is never mislabeled
as a failed reviewed change.

Stored/public error detail is the closed object `{code,field,item_index,
summary}`: `field` and `summary` are enumerated identifiers, `item_index` is a
bounded integer or null, and the whole canonical JSON is capped at 512 bytes.
It never contains a supplied path/value, home/username, env value, credential,
nonce, URL query/body, header, raw hostname answer, or secret-derived prefix.
Logs use the same sanitizer. Tests assert exact code/detail bytes against
hostile path, env, and credential-shaped inputs.

A policy-invalid Homie is ended in the same transaction before returning, so
it cannot remain the repeatedly selected oldest active/absent row and starve
pipeline work. Null-locator recovery is explicit row resume, never an implicit
native downgrade. A pipeline subjectless health candidate may recur until its
global configuration is repaired because there is no subject to block.

## Decision 5 — path evidence, preclaim proof, and honest residuals

For every existing bind source, the host attests its resolved canonical path
and the `(device,inode,type)` identity of every existing path component. It
also captures the complete ADR-017 trust predicate: real operator uid/owner,
mode grant bits, macOS ACL grants, non-symlink/type, protected containment, and
kind-specific authority. It repeats that full predicate—not merely stat or
file bytes—immediately before commit, immediately before Docker create, and
immediately after create before start. Any owner, mode, ACL, type, identity, or
containment change refuses/removes the confirmed-unstarted container. Docker
inspect verifies only source spelling, destination, and access mode; it cannot
prove host inode, owner, mode, or ACL identity.

Git shape is role/stage-specific, and Darwin never invokes an
operator-installed host Git:

- A first standalone Worker accepts either an absent exact ADR-017 task path,
  which the resident post-claim creates as the fixed RO-parent/RW-children
  skeleton and setup fills as the isolated
  `.mission-control/tasks/task-<id>/{source,git}` repository/worktree from the
  pinned target/base SHA's **reachable closure only**, or exact registered
  retry residue proving task-store identity, object format, local repository
  UUID, sole branch, base/accepted SHA, closure digest, fixed controls, and
  relative worktree links. No agent ever receives the real repository common
  object/config/ref store. The immutable Run records only Worksource, task,
  deterministic task-root key, logical branch, target ref/base SHA, object
  format, and closure/assignment identity—never an absolute host path or
  mutable plan. Retry reuses that assignment rather than rebasing to a moved
  target.
- A correction Worker requires the exact sealed accepted task-local closure
  and gets only that local repository/worktree RW. Verifier receives the
  canonical task store RO plus ADR-017's execution-scoped disposable RW source
  materialized from the same sealed SHA; its verdict-time tracked-tree fence
  runs inside the container before the one-phase verdict. Packager and Refiner
  receive only the exact sealed local view RO when needed. Absence, foreign
  controls, a different local branch/store identity, or a SHA/closure/seal
  mismatch is infrastructure refusal; no later role reconstructs from the live
  primary checkout. Landing alone separately pins and attests the current real
  repository/target before importing the reviewed closure and CAS-creating the
  real task ref.
- A Git-backed pipeline role that has no task worktree receives a clean
  committed-tree projection at its pinned SHA, not the primary checkout. In
  particular, Strategist(propose) seeding never binds a live Git primary. For each
  registered Git Worksource, a fixed setup container verifies the selected
  committed SHA and, after claim, materializes a short-lived clean detached
  committed-tree projection with no Git control/config files. The agent sees
  that projection RO. A dirty/untracked sentinel in the primary checkout is
  therefore invisible. Registered non-Git seeding sources may use their actual
  roots RO under ADR-017. Homie's explicit operator scope may see a live
  registered workspace RO only with every descendant Git administrative
  identity covered; bare/uncoverable Git shapes use ADR-017's clean projection.
  It is the spec's separate operator exception.

Preclaim validates the real Worksource/object-directory identity only as the
trusted source for closure extraction (or later landing), plus the task-root
parent, role-appropriate absent-or-exact-task-local condition, and a short-lived
containerized Git/setup capability probe. That probe is exactly
`mc-probe-git-setup-<p12>` with ADR-018's shared boundary labels/manifest and
closed `mc-probe-role=git-setup`; its mutating arm receives only a
token-derived sacrificial repository/sibling under the already-proved parent,
never the real candidate repository or task store before claim. The
digest-covered post-claim effect alone creates the fixed task skeleton, and
its operation-specific setup plan populates, seals, or reconciles only the
authorized children/store/disposable projection. A collision,
changed parent/head/metadata, or setup failure is post-claim infrastructure
failure; it never falls back to live primary bytes. The same
parent/expected-absence proof covers newly derived
session/envelope/network/projection paths. Initiative shared-worktree behavior
remains Parked and is not generalized from these standalone rules.

The exact candidate guard/gateway probe is mandatory before claim and carries
`mc-managed=true`, `mc-component=boundary-probe`, and the preparation token,
with no tier label. It uses a distinct probe credential, never a runtime
credential. Success or refusal removes it before commit; crash residue is the
first orphan selected on a later tick, and ADR-018's registration TTL expires
even if no container was created. A failed cleanup or ambiguous absence
prevents claim.

Pure path checks do not prove Docker Desktop file-sharing applicability. The
same preclaim proof therefore creates, inspects, and removes one **unstarted**
`mc-probe-mount-<p12>` canary container from the candidate image with the candidate's exact
existing structural bind/volume set and access flags. It carries the same
managed/component/prepare-token labels plus ADR-018's closed
`mc-probe-role=mount`, no tier,
no launch credential, no command capable of reading the binds, and is never
started. An expected-absent derived source is represented by a token-scoped
sacrificial sibling under the same already-proved parent/filesystem; the real
source remains absent until post-claim effect. Docker create must accept every
source, inspect must equal the ordered source/destination/mode plan, removal
must be confirmed, and host identities/trust must still match afterward. This
proves sharing/application without exposing dirty primary or credential bytes
to a process and without writing an agent-visible source. Broker death residue
is swept by the exact boundary-probe role/token taxonomy before any later
candidate; ambiguous absence prevents claim.

These rechecks narrow but do not “close” Docker bind TOCTOU: Docker accepts path
strings, not already-open file descriptors, so a malicious same-host actor able
to rename/mount components between the final stat and daemon bind remains a
documented residual. Mission Control never grants such an actor authority; it
fails closed on every observable swap. Tests cover swaps before commit, before
create, during the post-create check, expected-target races, mode
`0600 -> 0666`, owner/ACL change with unchanged inode/bytes, first create then
spawn failure/reap/retry, normal Worker-to-Verifier reuse, a dirty-primary
sentinel, and cleanup.

## Decision 6 — immutable effects and post-commit failures

A successful pipeline commit inserts the Run and claims the lease in one
transaction. A Homie wake relies on its already-active registry row, persists
its current launch generation in that transaction, and returns an effect
fenced by `session_id + launch_id`; it creates no Run or lease. Both return
`plan_version:1` and a digest-covered immutable plan containing:

- exact tier, role/allowlist grain, run/session/launch id, subject,
  Worksource, fresh/resume mode, route, and historical binding;
- exact names, image digest, user, labels, resources, security options,
  entrypoint, argv, and ordered environment;
- ordered `{logical_id,source,destination,kind,access}` mounts with captured
  host identity evidence;
- deterministic gateway/network/projection handles and audited policy
  references, never a credential or launch nonce;
- exact `run.json`, sandbox, and resolver projection bytes; and
- ADR-017's closed role/run/task file plane: own session folder, exact
  attachment roots, one reconciled Homie historical-pipeline-trace projection mount,
  and only the needed outputs/corrections/revisions/context/workflow paths at
  their fixed destinations; and
- exact setup and cleanup steps for task-local store/seal/disposable or
  committed-tree projection, session,
  attachment, trace-projection, envelope, and network artifacts.

Setup steps are a closed operation-specific union rather than a generic
container request: resident-created fixed skeleton, first-task closure extraction, Worker-retry reconciliation,
accepted-seal rebuild, Verifier disposable-source materialization, committed
projection materialization, and exact cleanup each carry only ADR-017's rows
for that operation. A seal-consuming step names the accepted completion
activity, seal identity, manifest digest, and SHA; landing separately carries
only its attested task-store/real-repository rows and pending landing identity.
The resident refuses a downstream create until the producer is confirmed
absent and the named seal/setup result has been inspected successfully.

Phase 3 adds the pipeline Run's `plan_digest`, `current_container_id`,
`launch_bound_at`, and `runner_started_at` launch receipts. Immediately after
final create/inspect/trust checks and immediately before Docker start, the
resident invokes private host-scope `mc run launch-bind` with the exact run,
plan digest, and Docker id. Under `BEGIN IMMEDIATE` it CASes the still-held
same-run lease and matching Run/plan with no bound id, requires lock-domain
`now` to be strictly before both `acquired_at + spawn_grace_s` and the hard
deadline, then stores the id/time.
Repeating the same tuple returns the original receipt idempotently; a different
id, lost/interrupted lease, or changed plan is fenced. On refusal the resident
removes the confirmed-unstarted agent first and then its guard/envelope. At
container entry the trusted pipeline runner invokes private
`mc run runner-started` before launching any adapter, harness, or model; it
CASes the same lease/run/plan/container, repeats both temporal checks using
lock-domain time, and stamps `runner_started_at`.
Thus an operator interrupt during setup or the create→start gap cannot launch
stale model work: a late Docker start reaches only the trusted runner, whose
fence fails and exits. Neither receipt advances the task.

The task-local Git boundary is also fenced before a role-side state mutation,
not by a fictional post-exit rollback. For Worker completion, the in-container
trusted `mc` wrapper validates the sole local branch/index/tree and creates an
fsynced privileged immutable pack+manifest of exactly the proposed reachable
closure; its SHA/closure digest is included in the helper request and accepted
completion record together with ADR-017's exact completion request/result
receipt. Model uid cannot alter that seal after the helper commits,
so post-stop setup can rebuild the canonical local store despite late loose
residue. An unaccepted seal is cleanup-eligible only after exact producer
absence, lease/run fencing, and a serialized no-accepted-request/receipt check;
response loss cannot race it. For Verifier verdict, the same wrapper compares the disposable
tracked tree and RO task-local controls to the sealed reviewed SHA immediately
before delegating the one-phase verdict; later writes affect only the disposable
source that is removed after exit. Packager/Refiner never receive a mutable
canonical repository view.

The trace projection is a directory namespace of hard links to finalized,
writer-closed pipeline trace files on the same `MC_HOME` filesystem. Each entry
must prove `os.SameFile` with its spine-located source; no trace bytes are
copied, no symlink is accepted, and active/writer traces are excluded. Homie
native traces are not projected: they are not Worksource session files, may
become writable again on resume, and unlink cannot revoke another warm Homie's
open descriptor. The one projection root is mounted RO into Homie and
reconciled by polling. Its directory entries are derived/rebuildable and are
never canonical records or cleanup authority over source session folders.
Homie separately receives only its own stable
attachment `in` root RO and `out` root RW. Pipeline role file mounts are exact
prior inputs/current outputs; no plan binds a broad `MC_HOME` subtree or a
sibling run's writable records.

The resident verifies the closed schema/digest and mechanically performs
persistent-own-folder validation → trace/attachment reconciliation → ephemeral
launch projections → setup → gateway/guard → agent create → complete host
trust recheck → inspect → pipeline `run.launch-bind` or Homie
`homie.launch-bind` receipt → start. Pipeline runner-start authorization and
Homie native-locator/runner-start registration then occur before model work. It
does not resolve policy, synthesize flags, or change order. A final fresh
inventory assertion immediately before agent create refuses a
second/mismatched pipeline or Homie instance; ambiguous absence is not absence.

Pipeline create/inspect/start failure removes only confirmed-unstarted
ephemeral envelopes, guards/networks, clean seeding projections, and exact safe
setup residue; it never removes the permanent session folder, durable task
worktree, authored record, or source trace and never treats an ambiguous start
as absence. It leaves the claimed lease without a heartbeat and is charged
only by the ordinary spawn-grace reaper on a later tick. The resident does not
invent an immediate task transition.

A Homie launch failure is reported through a private
session/launch/container-fenced lifecycle verb. With `launch_started_at` or
existing native locators, it atomically ends the session with sanitized
`spawn_failed:<stable-code>`. Before the first locator/start receipt, a
confirmed pre-start failure clears only the absent container binding,
preserves the launch id/mode and durable pending turn, and becomes retryable
after cleanup. A candidate-policy refusal happens before effect and ends the
session; Decision 3's explicit row resume is its recovery path. Ambiguous
start/presence does neither until exact inventory resolves it. The runner
registers the native locator pair before stamping `homie.runner_started`; both are launch
fenced. A lost wake response reuses the durable generation, while a live exact
container suppresses duplication.

The result union is exactly `idle`, `orphan-stop`, `homie-end`, `reap`,
`health`, `land`, `reenter`, `blocked`, `spawn`, or `wake`. Unknown actions or
versions fail closed.

## Decision 7 — names, labels, and exact liveness

Worksource ids admitted to a container name are lowercase ASCII matching
`[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?` (1–32 bytes). Pre-existing ids outside
that grammar yield `identity.name_invalid`; they are never sanitized or
collapsed. Pipeline run ids are 16 lowercase hex; Homie session ids are
`h-` plus 16 lowercase hex; launch ids are 16 lowercase hex.

Names are therefore exact:

- pipeline agent: `mc-<worksource>-<run_id>`;
- Homie agent: frozen `mc-homie-<session_id>`;
- helper: `mc-helper-<12 lowercase hex from real MC_HOME>`;
- setup: `mc-setup-<run_id>`;
- landing: `mc-landing-<landing_id>`, where `landing_id` is the first 16
  lowercase hex of the domain-separated digest of deployment, subject, and
  exact approved packet/run identity; and
- guards and probe clients: deterministic ADR-018 names from execution
  identity.

Every managed container has `mc-managed=true`. Agent and network-guard
containers add `mc-tier=pipeline|homie`; pipeline agents add `mc-run-id` and
`mc-worksource`; Homie agents add `mc-session-id` and `mc-launch-id`. A guard has
`mc-component=network-guard`, its owning tier, and the same run/Worksource or
session/launch identity, plus ADR-018's non-secret
`mc-gateway-registration` generation. The helper has `mc-managed=true,
mc-component=helper` and **no `mc-tier`**. A boundary probe has only its
component, bounded expiry, and prepare token, never a tier. Setup has
`mc-component=setup`, its exact run/Worksource/action identity, and no tier;
landing has `mc-component=landing`, its exact landing/subject/approved-run
identity, and no tier. Neither can masquerade as an agent or guard.

Pipeline liveness requires exact name, tier, run, and Worksource equality with
the live lock. Homie liveness requires exact name, tier, session, launch, and
container id envelope plus the active registry row; idle age is reserved
for Decision 3 branch 6 and is not orphanhood. A guard is live only for its
exact live agent envelope. Any setup or landing action object observed by a
later single-flight tick is exact recovery debt, never “live” merely because
its Run/landing fact remains current. Helper health
requires exact derived name, image/build identity, fixed volume, resource
tuple, and helper labels. Sweeps query agent tiers/components explicitly;
helper/setup/landing/boundary-probe labels can never masquerade as agent
liveness. Unrelated containers lacking `mc-managed=true` are invisible.

## Crash and acceptance matrix

- Broker death before a spawn commit, after prepare, or after a host refusal
  leaves no work mutation; candidate probes are revoked by EOF/TTL or swept by
  exact labels even when no probe container was created.
- A prepare-side reap/end/reenter/health response lost after commit is returned
  byte-for-byte from the unique request receipt before re-selection and cannot
  cause a second action/activity/outbox/state mutation.
- A pipeline commit whose response is lost leaves exactly one Run/lease and is
  recovered by the spawn watchdog. A Homie wake response lost before effect
  retries its persisted launch generation; one successfully created exact
  container suppresses a duplicate.
- Pipeline interrupt/lease loss during setup, after create, after a lost
  same-id launch-bind response, and immediately before Docker start is fenced
  by `run.launch-bind` plus runner-start authorization; no adapter/model starts
  and confirmed-unstarted cleanup is agent-first. Both verbs reject exact
  spawn-grace/hard-deadline equality, and setup cannot outlive the remaining
  grace. Homie bind/start equivalently rejects idle-threshold equality.
- Resident death after Homie guard/registration and exact agent create but
  before start cannot adopt against the restarted gateway's empty registration
  table. Reconciliation stops/removes that exact agent, then its guard and
  derived envelope, confirms complete absence, and recreates the whole envelope
  with the same canonical launch id/mode and a new live registration generation.
- End → resume while an old fixed-name Homie container survives first returns
  an exact-id stop for the old launch, then wakes the new generation. A late
  old exit, failure, locator, transport, runner, or model verb is fenced and
  cannot mutate/end/stop the new launch.
- Normal observed Homie exit and resident-outage absence each end the exact
  started generation once. A bind-without-start crash retries only after exact
  absence; an ambiguous start never does.
- Duplicate/replayed private frames, candidate/token substitution, build or
  schema mismatch, a swapped `MC_HOME`/spine UUID at prepare (including a
  non-spawn branch) or attest, stale snapshots, and changed host
  bytes/owner/mode/ACL are inert.
- Reap, orphan, Homie-exit, idle-end, land, reenter, health, block, spawn, and
  wake branches each prove exactly one final action; cleanup/exit/idle/reap
  never fall through. An exact active idle Homie is not orphaned before its
  atomic idle end, including threshold equality. Fresh/reapable lease cases
  return before every later branch; Console/land/reenter return before ordinary
  Homie exit/idle/spawn, after at most one stale-writer cleanup action.
- Invalid plan tests prove zero Run/lease/agent container; pipeline subject,
  subjectless pipeline, established Homie, and null-locator first-launch Homie
  consequences are tested separately. A first-launch refusal ends once, does
  not starve subsequent pipeline work, and after repair explicitly resumes
  from bounded conversation rows without pretending native continuation.
- Final-effect tests cover duplicate mount/env/label keys, path/control chars,
  multiline near-limit briefs/documents, type confusion, canonical ordering,
  size bounds, digest/version substitution, normalized Docker-inspect equality,
  and secret-free frames/errors/logs.
- Existing Homie wake/resume succeeds under its frozen binding when current
  `routing.md` is malformed. Pipeline routing remains current and fail-closed.
- A Homie-only deployment-health refusal plus a dispatchable pipeline candidate
  writes one canonical deferral marker; the next matching free tick claims or
  terminally disposes the pipeline candidate before retrying that Homie.
- Mount-plan tests cover attachments, every authored file-plane kind, the
  one-mount same-inode finalized-pipeline-trace projection, clean committed
  seeding views, exact sealed task-local reuse/disposable Verifier source, and the absence of broad
  `MC_HOME` or live dirty-primary binds. The unstarted token-labeled mount
  canary proves actual Docker application/inspect/cleanup for the exact
  candidate sources and modes before claim.
- Runtime inventory changes prepare→commit yield `preflight.stale`; a pipeline
  appearing commit→effect refuses create; replace/relabel/config changes stale,
  harmless uptime drift does not, and ambiguous stop never permits a
  replacement. Paused/restarting/removing objects stay ambiguous and
  non-replaceable through exact cleanup; exited/dead objects are removed before
  absence permits retry.
- Inventory/registration tests stream at least 257 objects over multiple
  pages, detect missing/duplicate/reordered pages, and still select the one
  deterministic exact cleanup without truncation or count-based refusal.
- Landing tests attest the exact task-store/real-repository mounts and tuple,
  keep pending on canary/create/inspect/deadline infrastructure failure, block
  only on fixed `mc-land` semantic refusal, and recover every import/ref/merge/
  response-loss point from the exact receipt. Any setup/landing survivor seen
  by a later tick is stopped/removed before retry.
- Rows fallback tests use 64-row pages, one aggregate older-range omission
  marker, stored per-row length/digest metadata, bounded per-row markers, the
  empty `(0,0)` prefix, and exact 128-entry/512-KiB edges. Mixed body sizes
  prove newest-first marker replacement is byte-deterministic when individually
  valid rows cumulatively exceed the document budget. A history far beyond the
  bound proves launch reads O(tail) rows and reaches bind/start within grace. Boundary
  probe tests cover the shared closed mount/git-setup roles and prove the Git
  mutating canary touches only its sacrificial token root.
- Worktree/setup, gateway/network handles, create, inspect mismatch, start,
  broker death, setup/landing auto-remove residue, and host-file cleanup races
  are tested for both tiers.

S2 separately pins ordinary self-delegation bytes, exit codes, concurrency,
cancellation, and helper recreation.

## Alternatives rejected

- **Validate after claim.** Leaves a Run/lease for a known-invalid plan.
- **Let the resident write/block.** Breaks Inv. 2 and duplicates domain law.
- **Mount Docker, all host roots, or `MC_HOME` into the helper.** Expands the
  privileged helper and exposes credential/config roots.
- **Persist a pending plan.** Adds forbidden handoff state and drift.
- **Hold `BEGIN IMMEDIATE` during host/Docker I/O.** Couples the sole writer to
  unbounded external latency.
- **Wake Homie under a fresh pipeline lease.** Conflicts with §10's explicit
  `Fresh → return`; lease-free describes Homie's runtime liveness, not an
  exemption from the single dispatch order.
- **Treat every active absent Homie as eligible.** Launches empty sessions and
  contradicts ADR-009's inbound eligibility rule.
- **Claim symlink-race closure.** Docker's path-string bind API cannot offer
  that guarantee; identity rechecks plus an explicit residual are honest.
