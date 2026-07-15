# ADR-017 — Mount authorization and deterministic container paths

- Status: Accepted
- Date: 2026-07-13
- Where the spec delegates: §§5, 11.1–11.3, 15.3, and 16 require an
  operator-owned allowlist, an extend-only blocked floor, bilateral RW
  consent, and two enumerated cross-Worksource RO profiles without fixing
  their grammar or collision-free container destinations.

## Amendment — 2026-07-14 (view discipline and the privileged-tree gate)

This ADR stays Accepted; this amendment corrects it in place rather than
superseding it, and quotes what it replaces so the correction is visible.

**What was wrong.** The review in
`docs/reviews/2026-07-14-adr-016-019-verification.json` (finding #7, CONFIRMED
major; finding #6, CONFIRMED minor — one root cause) established that
Decision 8's "its source root and parents are owned by the setuid uid at mode
0700", and the matching Decision 6 rows for `MC_HOME/attachments/<session-id>/
out` and `MC_HOME/seals/<run-id>/` ("privileged-owned mode 0700"), are claims
about **host** inodes. Decision 6's column is headed "Typed source and access",
and Decision 5 fixes that column's view by having the resident create the
`/workspace` skeleton "as the host operator" and then tune **modes** for the
container's final uid. Read in that view — the only view the text supports —
those claims were unrealizable and self-contradictory:

1. "source root and parents" reaches `MC_HOME` itself, which Decision 1
   independently requires to be a non-symlink operator-owned root and rejects
   on "Another owner". Both could not hold.
2. Spec §15.5 requires that "the native surface reads the file directly", and
   spec §12 with Inv. 23 fix that surface as an ordinary per-user LaunchAgent
   in the operator's GUI session. A host tree owned by uid 10001 at mode 0700
   denies the operator uid traversal, so the ADR made unreachable exactly what
   the spec mandates. The inbound arm died with it: an operator-uid surface
   cannot create under a 0700 `<session-id>/` it cannot traverse.
3. Nothing could create such an inode. Every host actor is the unprivileged
   operator (§12 forecloses LaunchDaemons), containers hold `CapDrop=ALL`
   (ADR-019) so the setuid wrapper has no CAP_CHOWN and cannot reach its own
   bind's unmounted parents, and this ADR authorizes no `chown`, `sudo`, or
   root step anywhere.

**What changed, and what did not.** The confinement goal is unchanged and is
not weakened: model/harness uid 10002 still may not traverse, create,
overwrite, or delete in the outbound attachment tree or the seal tree; §15.3's
Homie write boundary and the setuid publisher's exclusivity still hold. Two
things change:

1. **A view rule**: every ownership/mode claim now states which view it
   describes. The ambiguity between the two views is what let this defect hide.
2. **The uid-10002 denial moves from host ownership to the mount shape.** Host
   sources stay operator-owned, exactly as Decision 5 already had the resident
   create the task skeleton; the denial comes from a baked image-rootfs **gate
   directory** `/mc/private` (owner `10001:10001`, mode 0700) that the
   privileged RW binds now sit beneath. No host process chowns anything to uid
   10001, because none can.

Corrected below: Decision 4's seal and attachment bullets, Decision 5's seal
paragraphs and canary, Decision 6's destination table (two rows replaced, two
structural rows added), Decision 8's outbound paragraph and canary, one
rejection code, and the matching acceptance lines. The mount plane is otherwise
untouched.

### The view rule

Every ownership/mode statement in this ADR describes exactly one of two views,
and now says which:

- **Host view** — the macOS APFS inode of a bind *source*, created by
  onboarding or the resident acting as the host operator. Every `MC_HOME` inode
  is operator-owned. Nothing in this design chowns a host inode to a container
  uid.
- **Container view** — either a Linux inode of the container's own root
  filesystem, baked at image build where the build's root sets owner and mode,
  or the read-only/read-write property of a mount.

This ADR asserts **no** fact about how the Docker Desktop/VirtioFS share
*presents* a host inode's uid, gid, or mode inside the container. That
presentation is an unproven platform behavior — `docs/priors/` holds no
VirtioFS ownership prior, and none is invented here. No denial protecting the
seal tree or the outbound attachment tree rests on it. Where a *presented* mode
must permit an access, the required shape is whatever the mandatory canaries
prove, exactly as Decision 5 already says for the task skeleton's
"canary-proved final-uid writable shape". Where a *denial* is required, it is
taken from a mechanism the kernel enforces on an inode this design controls: an
image-rootfs gate directory's traversal bits, a read-only bind, or the absence
of a mount.

One pre-existing claim is presentation-dependent and is named here rather than
silently repaired: Decision 5's "uid 10002 cannot chmod the parent or create a
sibling" on the operator-owned mode-0555 task root holds only if the share
presents that host inode's owner and mode faithfully to the container. It is
therefore an arm of the existing ADR-019 final-uid VirtioFS canary, not an
assumption; a red arm refuses mutable dispatch like every other red arm. No
seal or attachment confinement depends on it.

### The privileged-tree gate

The seal staging root and the outbound attachment root are confined twice, in
different views, and neither layer depends on the share's uid presentation:

- **Host view**: both sources are operator-owned, created by the resident as
  the host operator before launch, beneath Decision 1's operator-owned
  mode-0700 `MC_HOME`. That root is what denies every host actor except the
  operator; the trees' own bits are not the host boundary. The operator uid —
  hence §15.5's native surface and the resident — keeps the direct outbound
  read and the inbound create the spec mandates.
- **Container view**: both bind beneath `/mc/private`, an ordinary directory
  baked into the base image at owner `10001:10001`, mode 0700 — image rootfs,
  never a bind, so the image build's root sets it and no host chown is needed.
  The kernel checks that gate's traversal bits against the process fsuid before
  any bind beneath it is reached, so uid 10002 fails at the gate and cannot
  traverse, create, overwrite, or delete anywhere under it whatever the share
  presents the bind itself as. Setuid `mc` at euid/fsuid 10001 passes; ADR-019
  keeps `no-new-privileges` absent for exactly that reason, and its
  `CapDrop=ALL` leaves no CAP_DAC_OVERRIDE or in-container mount road around
  the gate. The gate is the mechanism; presented ownership of the bind source
  is not, and is never relied on.

The gate is verified, not assumed: a launch whose image lacks `/mc/private` as
a directory owned `10001:10001` at mode 0700, or that would place any other
destination beneath it, refuses with `mount.gate_unhealthy`.

Because the denial now lives at the gate, each source's host mode must let
euid/fsuid 10001 create and write *through the share* — the canary-proved
privileged-publisher-writable shape, whatever the canary proves that to be.
That is not a host exposure: the only host actor that can reach the path is the
operator, who owns `MC_HOME`'s mode-0700 root, and the only container process
that can reach it has already passed the 0700 gate.

Inbound needs no gate. `in/published` denies uid 10002 writes through its
read-only bind — MS_RDONLY in the container's mount namespace, kernel-enforced
and equally independent of the share's presentation — and `in/staging` and
`in/journal` are denied by absence: they are never mounted.

## Decision 1 — strict allowlist grammar

`MC_HOME/mount-allowlist` is strict TOML v1:

```toml
version = 1

[[allow]]
path = "/Users/operator/src/acme"
target = "acme"
access = "rw"

[[allow]]
path = "/Volumes/reference-library"
target = "reference/library"
access = "ro"
```

Only `version` and `[[allow]]` exist. `version` is exactly integer `1`, once.
Every allow entry has exactly:

- `path`: an absolute host path;
- `target`: a stable relative POSIX container-path anchor; and
- `access`: the maximum grant, exactly `ro|rw`.

Unknown tables/keys, duplicate keys, an unsupported version, or empty values
reject the whole file. `version = 1` with zero `[[allow]]` entries is valid
deny-all policy: onboarding `home` must be able to create it before the first
Worksource exists. There is no shell/env/tilde expansion, interpolation, or
globbing in `path`. The file is at most 256 KiB with at most 256 allow entries;
one `path` is at most 4096 UTF-8 bytes. The effective operator extension has
at most 128 blocked patterns, each at most 255 ASCII bytes. One Sandbox
Profile has at most 64 artifact roots, 128 readonly mounts, and 512 denied
paths. A boundary excess rejects before identity walking or allocation; none
of these collections is truncated.

The allowlist is a non-symlink regular file owned by the real operator uid,
with owner read/write and no group/other permission bits, beneath a
non-symlink operator-owned `MC_HOME` with owner rwx and no group/other
permission bits. macOS ACLs are read through the native ACL API; any allow ACE
granting a non-owner access rejects. Harmless special bits or a stricter owner
mode are not treated as grants. Onboarding normalizes the ordinary form to
0600/0700. Another owner, a granting ACL, or group/world access rejects.

Canonically identical and ancestor/descendant-overlapping allow roots reject,
so every source has one authorization root and one maximum mode. `target`
must be nonempty, relative, already POSIX-clean, and contain no empty, `.`,
`..`, colon, backslash, NUL, control, or leading-slash component. It is at
most 1024 UTF-8 bytes and 255 bytes per component. Effective targets must be
byte-exact unique and ancestor-disjoint under Linux/POSIX path semantics
(`docs` and `docs/api` conflict; `docs` and `Docs` are distinct). No consumer
case-folds these container paths, and nothing auto-renames a collision.

Operator blocked-pattern extensions live in `config.toml`, never the
allowlist:

```toml
[container]
additional_blocked_patterns = [
  { kind = "component", pattern = ".terraform.d" },
  { kind = "basename-glob", pattern = "*.agekey" },
]
```

`component` is one literal ASCII component with no separator/wildcard.
`basename-glob` is lowercase ASCII with literals plus `*` only—no separator,
`?`, class, or escape syntax. Effective patterns are always embedded floor ∪
additions. There is no replace/delete/negate form; invalid additions reject.

## Decision 2 — shipped blocked floor

Matching is ASCII case-insensitive against every component of both the
raw-clean source spelling and the symlink-resolved source.

Blocked exact components:

```text
.ssh .aws .azure .config .docker .gnupg .kube .codex .claude
.env .netrc .npmrc .pypirc .git-credentials
credentials secrets keychains kubeconfig
```

Blocked basename globs:

```text
.env.*
*credentials*.json
auth.json
id_rsa* id_dsa* id_ecdsa* id_ed25519*
private_key* private-key* *private_key* *private-key*
*service_account*.json *service-account*.json
*.pem *.key *.p12 *.pfx *.ppk *.jks *.keystore *.kdbx *.keychain-db
```

These classify the requested mount **address**, not the recursive contents of
an ordinary Worksource. A workspace may intentionally contain its own `.env`
tool secret (§5); directly requesting that file/directory as another mount is
blocked. Selected runtime-control mounts are typed system exceptions below,
not allowlist bypasses available to a profile.

The floor is tracked/compiled product policy. Operator config can only add.

## Decision 3 — canonical, identity-based containment

Profile save and every spawn independently run the same algorithm:

1. Reject non-absolute, missing, NUL/newline-containing, or wrong-kind
   sources.
2. Compute `Abs → Clean → EvalSymlinks`, stat the resolved object, and retain
   `(device,inode,type)` with its canonical path.
3. Match the blocked floor/extensions against raw-clean and resolved
   components.
4. Walk the resolved source's ancestor chain and compare each object to each
   resolved allow root with filesystem identity (`os.SameFile`). Accept only
   when exactly one allow root is encountered. Derive the relative suffix
   from that same identity walk, never unchecked string-prefix arithmetic.
   Validate that suffix independently with the target-component grammar:
   every component is nonempty, neither `.` nor `..`, at most 255 UTF-8
   bytes, and contains no colon, backslash, NUL, or ASCII control. The whole
   suffix is at most 1024 bytes. A colon in the spelling of the matched macOS
   **allow root itself** is legal; a colon in any requested descendant suffix
   rejects. Thus an exact source root `/Volumes/ref:one` can be authorized,
   while `/Volumes/ref:one/child:two` cannot inherit that authority.
5. Reject when the source equals/is under a protected root **or is an
   ancestor that would expose it**.
6. Recompute at spawn. A changed symlink identity/destination is a new plan
   and must pass again.
7. Carry canonical path, component identities, and complete authorization
   evidence in the immutable plan. Immediately before Docker create and again
   after create/before start, rerun the whole predicate: allowlist and MC_HOME
   owner/mode/ACL/non-symlink trust; exact policy-file bytes; allow-root/source
   component identities; blocked additions; protected/cross-Worksource roots;
   kind, access, and final destination. Any drift removes the unstarted
   container. An unchanged inode/bytes does not excuse a changed owner, mode,
   or ACL. Docker inspect then verifies only canonical source spelling,
   destination, and mode; Docker does not expose host device/inode identity.

An in-root symlink is accepted; an escaping one rejects. `/safe-root-evil`
never satisfies `/safe-root`. Identity comparison also handles default
case-insensitive APFS without guessing its case mode. A declared deny path
that does not yet exist is compared through its nearest existing canonical
ancestor plus unresolved suffix; ambiguity denies.

After the closed destination constructor appends a validated target/suffix,
the complete absolute container destination is validated again. It must be
POSIX-clean, equal one fixed Decision-6 destination or be produced by exactly
one of its parameterized constructors (rootfs parents are not constructors),
contain no empty, `.`, `..`, colon, backslash, NUL, or ASCII-control
component, stay at or below 255 bytes per component and 4096 UTF-8 bytes
total, and equal the constructor's round-trip serialization. Validation is
not inferred from the parts. Docker gets structured bind objects with
separate source, destination, and read-only fields—never a shell or
concatenated `-v` string.

## Decision 4 — bilateral access and typed system mounts

The access truth table is exact:

| Request | Allow maximum | Result |
|---|---|---|
| RO | RO | RO |
| RO | RW | RO |
| RW | RW | RW |
| RW | RO | reject |

Rejection never downgrades to RO and never drops a requested mount. Every
externally configured `workspace_root`, `artifact_roots`, and
`readonly_mounts` source must match one allow entry.

System-generated sources have closed kinds; the external allowlist does not
authorize them:

- own run/session folder: RW into its owner only;
- exact run envelope, sandbox projection, release runner source, generated
  resolver, non-secret network policy, and gateway CA certificate: RO;
- one assigned mutable **standalone-task** worktree in its sanitized
  task-local repository, or one registered read-only non-repository
  Worksource root; the real primary checkout and real repository control/
  object store are never agent mounts for a leased Git-backed pipeline run;
- the task-local repository root from Decision 5: Worker may mutate its local
  branch/loose objects, Verifier may mutate only its disposable same-SHA
  source overlay, and Packager/Refiner receive canonical source/control RO;
  fixed pointer/config/control covers remain RO in every role;
- the current standalone Worker's exact completion-seal staging root: an
  operator-owned host source, Docker RW at its destination beneath the
  mode-0700 `/mc/private` image gate, usable only by the setuid completion
  wrapper; uid 10002 fails the gate and has no direct traversal/write
  authority;
- clean committed-tree single-Worksource and cross-Worksource seeding projections
  created from pinned committed refs: RO;
- own Worksource synthetic home and package caches: RW at MC-derived fixed
  roots; the profile's `tool_home_dir` names that derived home rather than
  arbitrary authority;
- selected OAuth binding's exact `runtime_control_dir`: RW only at its closed
  harness destination, as the declared materialized downgrade;
- current run's output directory and its exact workflow-capture file: RW only
  for that run; exact prior output/evidence, correction, revision, and steer
  inputs: RO only for the subject role whose canonical brief references them;
  and the exact current Verifier correction output: RW only for that
  `(task,correction_number,run)`;
- own Homie attachment published-input directory: RO; own outbound publisher
  root: an operator-owned host source, Docker RW at its destination beneath the
  mode-0700 `/mc/private` image gate, so uid 10002 direct access is denied at
  the gate and only the runner's private `mc` publisher can commit bytes; no
  pipeline or other Homie attachment root;
- the derived historical-trace projection root from Decision 7: one RO mount
  into Homie only; and
- spine named volume: the fixed setuid ownership boundary.

The gateway private key, injection table, launch nonce, runtime socket,
operator's real HOME, complete `MC_HOME`, config, routing, allowlist, sibling
record directories, primary checkout/index, repository-local config/hooks,
and every other binding control directory are absent.

Every typed system source bypasses the external allowlist requirement only. It
still passes its kind-specific source-type, non-symlink, identity, containment,
and cross-Worksource checks at plan and spawn. The exact selected runtime
control directory is the sole kind-specific exception to generic blocked-name
matching; that exception cannot be requested as an ordinary profile mount.
Allowed source kinds are closed: workspace/task-repository/worktree/
projection, artifact, synthetic home/cache, runtime-control, run-output,
completion-seal, and attachment roots are directories; a profile reference or
prior record input is one regular file or one non-symlink directory subtree;
runner source is its expected
regular-file/tree shape; envelope, sandbox, resolver/network projection, CA
certificate, workflow capture, and current correction output are non-symlink
regular files. Historical pipeline-trace entries are regular-file hardlinks
governed only by Decision 7; they are never independently mounted.

## Decision 5 — protected set and jurisdiction

The non-subtractable protected set is the union of:

- the selected effective profile's `denied_paths` entries;
- every other Worksource's workspace/worktree/artifact/state/cache/tool-home
  roots;
- every registered real Git control directory and every
  `<workspace_root>/.mission-control` task/projection root; Decision 5 permits
  only the exact own task-local root, committed-tree materializations, trusted
  setup/landing, and Homie's type-matched inert nested covers;
- all `MC_HOME/sessions`; Decision 7 authorizes only same-inode entries in a
  separate typed projection, never a session source as an ordinary mount;
- all `MC_HOME` attachment, output, workflow, correction, revision, context,
  projection, seal, landing, state, cache, config, control, backup, and
  runtime-auth roots,
  including the allowlist, except each exact typed own-source grant in
  Decisions 4, 6, and 8;
- every runtime control dir, gateway secret root, and CA private-key root;
- when present, `~/.ssh`, `~/.aws`, `~/.azure`, `~/.config`, `~/.docker`,
  `~/.gnupg`, `~/.kube`, `~/.codex`, `~/.claude`,
  `~/Library/Keychains`, `~/.netrc`, `~/.npmrc`, `~/.pypirc`, and
  `~/.git-credentials`.

A source intersecting a protected root in either direction rejects, so
mounting a parent of another Worksource cannot expose a denied descendant.
The operator's real HOME has an additional directional `broad_root` rule: a
source may not equal or be an ancestor of HOME (`$HOME`, `/Users`, `/`), while
an allowlisted descendant such as `~/src/project` remains eligible unless it
hits another protected root. Allowlist membership never overrides
jurisdiction.

That union governs ordinary/profile-requested mounts. A typed system mount is
instead confined to its one kind-specific authorized root: the exact own
session, derived own state, selected runtime-control directory, or generated
projection may be inside `MC_HOME`, but any sibling/ancestor/other identity is
still denied. This is a closed exception, not a way for an allowlist entry to
override `MC_HOME` protection.

Worksource workspace roots are pairwise non-overlapping. Each artifact root
has one owner and cannot overlap another Worksource's workspace/artifacts.
Duplicate/overlapping sources within one plan reject instead of creating
aliases.

For Git-backed mutable Worksources, **leased pipeline visibility is committed
state only**. A standalone task uses an isolated sanitized repository, not the
real Worksource object database:

```text
<workspace_root>/.mission-control/tasks/task-<task-id>/
  source/   # linked worktree
  git/      # bare task-local common repository
```

The task root has one registered identity, is operator-owned mode 0555, and
contains only the two resident-precreated children plus fixed structural
mount points. The root itself is mounted RO into every agent and setup action;
only exact `source` and `git` child overlays carry the operation/role-specific
grants below. Thus uid 10002 cannot chmod the parent or create a sibling even
though it may mutate an authorized child. It is
excluded through the real repository's local `info/exclude`, is never tracked,
and is never reused by another task. The relative linked-worktree topology is
identical on the host and in the container: the whole task root binds at
`/workspace`, so `source/.git`, `git/worktrees/<mc-task-name>/commondir`, and
`git/worktrees/<mc-task-name>/gitdir` resolve without host/container path
translation. Setup creates those three files with fixed relative contents and
covers them RO in every agent. An absent or nonempty
`config.worktree` is replaced by a generated empty RO file; it can never
reintroduce repository config. A generated empty RO cover also reserves
`source/.mission-control`; setup, seal, and landing reject any commit tree
that contains that root component, so task content can never collide with the
real Worksource's untracked MC storage.

The resident first creates and identity-registers the empty
`task-<id>/{source,git}` skeleton as the host operator, then fixes the parent
0555 and the children to the canary-proved final-uid writable shape. The first
setup action receives the registered real Worksource RO, the exact task root
RO, and only its empty `source` and `git` children RW; no model, harness,
credential, network, sibling-create, or primary working-tree write authority
is present. It validates a full pinned commit
OID and the resolved real object-directory identity. A v1 mutable Worksource
must be the primary non-bare checkout whose non-symlink `.git` directory,
common directory, object directory, index, and target ref are all contained
beneath that registered root; a linked-worktree gitfile, bare root, submodule
root, or external common/object directory is eligible only for read-only
registration, not mutation/landing. Setup then rejects source
alternates, grafts, replacement refs, shallow state, promisor/partial-clone
state, missing closure objects, or an unsupported repository extension. With
a cleared `GIT_*` environment, `GIT_NO_REPLACE_OBJECTS=1`, network disabled,
and a generated config containing only the validated object format and fixed
resource limits, a synthetic temporary Git context reads the exact real
object directory directly; it never loads the real repository config, index,
refs, hooks, attributes, or worktree. Fixed plumbing walks the complete
object closure reachable from the pinned commit and streams it into a new
task-local pack. A local clone, hardlink/reflink, persistent alternate, ref
wildcard, stash, or object-directory copy is forbidden. The helper verifies
every object hash/type and proves that the new store's object set is exactly
that closure before it creates any ref.

The local bare repository has no remote, credential/helper, include,
alternate, graft, replacement, promisor, executable hook, fsmonitor,
maintenance command, or clean/smudge/process/merge driver. Its generated RO
config pins MC identity and `worktree.useRelativePaths=true`; generated empty
`hooks`, `info`, and `objects/info`, an empty `packed-refs`/`shallow`, and the
initial reachable pack are RO covers. Setup creates only
`refs/heads/mc/task-<id>` at the pinned SHA. It creates the sibling linked
worktree without checkout, builds the index from
that exact tree, and uses the baked tree materializer to write regular blob
bytes, executable modes, symlinks, and gitlink placeholders without invoking
repository checkout hooks, filters, attribute encoders, or external commands.
Unsupported tree modes or executable checkout semantics reject. A final
`fsck`/tree/index/worktree comparison must be clean. Thus primary dirty files,
the primary index, stashes, aborted commits, manually hashed/dangling objects,
real refs/config/hooks, and other unreachable bytes are absent from every
pipeline container.

Access is role-specific. A standalone Worker, including a correction retry,
gets `source` and the task-local `git` store RW, except for the fixed RO covers
above; ordinary status/add/commit updates only this disposable repository.
Verifier gets the canonical task root and whole local `git` child RO. Setup
materializes the sealed commit's exact source tree into an execution-scoped
disposable directory and overlays that directory RW at `/workspace/source`,
with generated RO `.git` and `.mission-control` covers. Build/test tools can
therefore create or change derived files while Git reads the sealed task-local
index/objects/refs RO; no write reaches the canonical accepted worktree.
Packager and Refiner get both canonical children RO only when their immutable
brief needs repository evidence. Verifier, Packager, and Refiner receive
`GIT_OPTIONAL_LOCKS=0`, so inspection does not try to refresh the RO index. No
agent can mount or name the real Worksource Git directory or any other task
root.

Worker completion is sealed before its ordinary one-phase spine transition.
The setuid `mc complete` wrapper first requires the exact local task branch,
no second ref, safe fixed controls, a clean index/tracked tree, and stable
head, then uses fixed config-free plumbing to stream **only** that head's
complete reachable closure into the current
run's gated `/mc/private/completion-seal` staging root. It writes a manifest
binding a 16-lowercase-hex `completion_request_id`, run/task/branch, object
format, SHA/tree, object count, closure digest, and pack/index digests; verifies
the pack independently; fsyncs files and directories; atomically publishes
the seal; and makes it read-only at mode 0444. It rechecks the branch/head
around packing. The helper transaction stores that request id, exact seal
identity/digest, completion consequence, and result receipt atomically. Any
concurrent deletion, replacement, or incomplete closure makes completion
refuse before changing the spine. Model uid 10002 cannot traverse or mutate
the seal root: it fails the mode-0700 `/mc/private` gate its destination sits
beneath, so the denial holds whatever the share presents the operator-owned
host source as.

Mutable-task confinement is healthy only after the same-share VirtioFS
capability canary proves, with mounts live, that euid/fsuid 10001 can publish
and fsync a seal, uid 10002 gets `EACCES`, and a restarted setup container can
read that seal RO while the host verifies its identity/digest. The canary
proves the composed shape — operator-owned host source under `MC_HOME`, gated
container destination — and assumes no uid presentation; the host mode the
privileged publisher needs is whatever it proves. A red arm refuses mutable
dispatch; relaxing the gate, changing ownership, or widening modes/ACLs beyond
the proved shape is not a fallback.

Only then does the same invocation commit completion, including the immutable
seal identity/digest. A run-keyed unaccepted seal is not cleanup-eligible
merely because no completion row references it. Cleanup first requires exact
producer agent/guard/runner absence and that the run no longer owns a lease
which could accept completion. It then takes the same `BEGIN IMMEDIATE` lock
as completion and removes only if the matching request has neither an accepted
completion nor a result receipt. Any already-running helper transaction must
therefore commit or roll back before this check; a later request is run-fenced.
A committed completion with a missing/mismatched seal is infrastructure
corruption and never falls back to the mutable task store. No next-role effect
may be prepared while the producer container still exists. After confirmed absence, trusted setup
rebuilds the local pack/sole branch/index/worktree **only from the accepted
seal**, thereby discarding late branch moves and dangling objects even if the
Worker damaged its local repository after completion. A retry starts at the
last accepted sealed SHA, never a later filesystem observation.

Before each Verifier launch, setup requires that sealed SHA and a clean
canonical index/worktree, then creates the disposable source solely from that
sealed object closure. The verifier's ordinary verdict remains one-phase
because canonical source/control mounts are kernel-RO throughout its run.
After container absence, the resident removes only the exact execution-scoped
disposable source identity; its generated build residue is never canonical
input for another role or retry.

This topology is an explicit, conservative deviation from §§6.2, 7, and
11.1: before approval, `mc/task-<id>` exists only in the task-local repository
rather than the real Worksource repository. Exposing the real common object store would
violate §5 because committed-tree isolation cannot hide staged, stashed,
aborted, or manually hashed objects. Verifier's writable source is a
disposable same-SHA materialization rather than the canonical producer
worktree, preserving the required build-write capability without making the
one-phase verdict race host cleanup. On approved landing, the fixed
`mc-land` action imports and verifies only the exact reviewed closure and
CAS-creates the real `refs/heads/mc/task-<id>` at that SHA, applies the spec's SHA
fence, and immediately performs the required primary-checkout merge. This
minimal topology change preserves commit-before-complete, stable review,
host inspection, pure approval, and the real primary-checkout landing while
honoring §5's stronger invariant.

A Git-backed leased pipeline candidate that has one Worksource but no assigned
standalone-task worktree receives a short-lived clean committed-tree
projection of its pinned ref RO at `/workspace/source`; it never receives the
live primary checkout or any Git object/control store. The same config-free
closure reader and baked tree materializer create it, with an inert `.git`
cover. This is the ordinary single-Worksource arm for a role whose confinement
still follows that Worksource. A candidate whose canonical brief and identity
are explicitly Worksource-free and filesystem-free has no `/workspace/source`
mount. Cross-Worksource Strategist(propose) uses the plural committed-tree
projections below.

Every committed-tree projection, plural seeding or ordinary, needs no Git
mutation or metadata. Its nested `.git` entry is covered by a type-matched
inert generated RO marker, and no Git source, object store, or repository
config is mounted. It therefore exposes only the pinned committed tree, not
dirty/unreachable bytes or credential-bearing control state.

Initiative-wave/shared-worktree representation and mount semantics remain
Parked. This ADR accepts no initiative mutable-worktree or Git-control arm; an
initiative/child candidate needing that path is not eligible for the accepted
Phase-3 spawn path until the operator resolves the parked design and a
follow-on ADR extends this closed table. Nothing here treats a standalone
task worktree as that fallback.

The two cross-Worksource exceptions are closed:

- Strategist(propose) seeding gets only clean committed-tree projections of each
  registered mutable Git Worksource's pinned committed ref, and registered
  non-repository read-only roots, all RO—no primary checkout, artifacts,
  sessions, controls, credentials, or profile reference mounts. Projection
  setup is deterministic and residue is an execution-identity-scoped
  ephemeral artifact, never a durable alternative workspace.
- Homie operator scope gets registered workspace/artifact roots and eligible
  history through the single Decision-7 trace projection, all RO, plus only
  its own typed attachment pair. A non-bare Git workspace may use its live
  working root only when every resolved Git file/common/object/index/control
  identity is either outside that root or a strict, non-overlapping descendant
  that receives a type-matched inert RO nested cover. `.git` and the reserved
  `.mission-control` always receive covers. If a Git control identity equals
  or contains the workspace root, including a bare repository, the Homie gets
  a clean committed-tree projection (or an empty generated projection for a
  registered empty repository) at the same logical workspace destination
  instead of the live root. Thus linked/external Git shapes admitted for
  read-only registration cannot expose objects, refs, config, index, or hooks.
  Real repository controls and every task/projection root are absent even when
  primary working bytes are intentionally live. It gets no other session
  folder, other attachment root, Worksource control/runtime secret root, or
  pipeline-record write mount.

## Decision 6 — deterministic destinations

The validated allow `target` is the stable external-root name. A requested
descendant appends its separately validated identity-derived suffix. The two
forms are exact: a request equal to its allow root constructs
`<prefix>/<target>` with no trailing slash, while a descendant constructs
`<prefix>/<target>/<nonempty-suffix>`. Empty suffix is legal only for the
exact-root form and is not passed through as a component. No host basename
becomes authority. Pipeline ids are exactly 16 lowercase hex; Homie ids are
`h-` plus 16 lowercase hex; positive task ids use canonical decimal without
sign or leading zero. Correction `<n>` is exactly `1|2|3`; record-input
`item-<n>` is canonical decimal `1..128`. An invalid identifier rejects rather
than being encoded, truncated, or sanitized.

The agent container filesystem has this complete structural destination table.
`<target>` and `<suffix>` below are the validated external pieces; every other
variable is a grammar-checked canonical identity from the candidate.
`<registered-control-relative-path>` is not operator-chosen: it is the exact
POSIX-clean relative path of a resolved Git administrative identity strictly
inside its registered workspace. The planner reduces nested administrative
identities to the minimal non-overlapping ancestor cover set and rejects any
path that is not a strict descendant or collides with another destination.

| Destination | Typed source and access | Presence |
|---|---|---|
| `/workspace` | exact operator-owned mode-0555 task-local skeleton root, always RO; image-rootfs directory otherwise | standalone-task roles use the bind; every other agent gets only the structural rootfs directory |
| `/workspace/source` | actual nested bind of the canonical task child RW for Worker; inherited through the RO task-root bind for Packager/Refiner; execution-scoped sealed-tree materialization overlaid RW for Verifier; a separate clean pinned committed-tree projection RO for a Git-backed single-Worksource run without that task root; or registered non-repository root RO | ordinary single-Worksource pipeline only |
| `/workspace/source/.git` | fixed relative pointer to `../git/worktrees/<mc-task-name>` for a standalone task, or type-matched inert marker for a committed projection; RO cover | matching Git-backed arm only |
| `/workspace/source/.mission-control` | generated empty directory, RO cover over the reserved repository-root path | every standalone-task or Git committed-projection arm |
| `/workspace/git` | exact sanitized task-local bare repository sibling | actual nested bind RW for Worker; inherited through the RO task-root bind for Verifier/Packager/Refiner |
| `/workspace/git/config` | generated sanitized local config, RO cover | every standalone-task role |
| `/workspace/git/hooks` | generated empty directory, RO cover | every standalone-task role |
| `/workspace/git/info` | generated empty directory, RO cover | every standalone-task role |
| `/workspace/git/objects/info` | generated empty directory, RO cover | every standalone-task role |
| `/workspace/git/objects/pack` | setup-generated exact sealed reachable-object pack directory, RO cover | every standalone-task role; Worker adds only loose local objects |
| `/workspace/git/packed-refs` | generated empty regular file, RO cover | every standalone-task role |
| `/workspace/git/shallow` | generated empty regular file, RO cover | every standalone-task role |
| `/workspace/git/worktrees/<mc-task-name>/commondir` | generated relative pointer to the task-local `git` root, RO cover | every standalone-task role |
| `/workspace/git/worktrees/<mc-task-name>/gitdir` | generated relative pointer back to `/workspace/source/.git`, RO cover | every standalone-task role |
| `/workspace/git/worktrees/<mc-task-name>/config.worktree` | generated empty regular file, RO cover | every standalone-task role |
| `/workspace/artifacts/<artifact-target>/<suffix>` | one allowlisted artifact source, bilateral mode (normally RW) | ordinary owning Worksource only |
| `/workspace/references/<reference-target>/<suffix>` | one allowlisted profile reference, RO | ordinary owning Worksource only |
| `/workspace/seeding/<workspace-target>/source` | clean committed-tree projection of a pinned ref, or registered non-repository root, RO | Strategist(propose) only |
| `/workspace/seeding/<workspace-target>/source/.git` | inert generated marker, RO overlay | each Git-backed seeding projection only |
| `/workspace/operator/worksources/<workspace-target>/source` | registered live non-bare Worksource root with every descendant Git control covered, a clean committed-tree/empty projection for a bare or otherwise uncoverable Git shape, or a registered non-repository root; RO | Homie only |
| `/workspace/operator/worksources/<workspace-target>/source/.git` | generated type-matched inert RO cover over real Git controls, or inert marker for a projection | each Git-backed Homie workspace mount |
| `/workspace/operator/worksources/<workspace-target>/source/<registered-control-relative-path>` | generated type-matched inert RO cover over each additional resolved Git common/object/index/control descendant | each live Git Homie workspace mount; omitted when no additional descendant exists |
| `/workspace/operator/worksources/<workspace-target>/source/.mission-control` | generated empty directory, RO cover over every MC task/projection root | each mutable Git Homie workspace mount |
| `/workspace/operator/worksources/<workspace-target>/artifacts/<artifact-target>/<suffix>` | registered artifact source, RO | Homie only |
| `/workspace/operator/traces` | Decision-7 owner-keyed hardlink projection root, RO | Homie only; exactly one bind |
| `/mc/run.json` | resident-materialized exact launch envelope, RO | every agent |
| `/mc/sandbox.json` | validated non-secret sandbox projection, RO | every agent |
| `/mc/session` | `MC_HOME/sessions/<run-or-session-id>/`, RW | owner only; trace-only |
| `/mc/attachments/in` | `MC_HOME/attachments/<session-id>/in/published`, operator-owned host source, RO | owning Homie only |
| `/mc/private` | image-rootfs directory owned `10001:10001` at mode 0700, never a bind and never a bind's parent-by-accident; the kernel traversal gate for every privileged destination beneath it | every agent; uid 10002 fails the gate, so nothing beneath it is reachable to the model/harness |
| `/mc/private/completion-seal` | exact `MC_HOME/seals/<run-id>/`, operator-owned host source, Docker RW beneath the gate | current standalone Worker's setuid completion wrapper only; uid 10002 fails the gate |
| `/mc/private/attachments` | image-rootfs structural directory beneath the gate | owning Homie only |
| `/mc/private/attachments/out` | `MC_HOME/attachments/<session-id>/out`, operator-owned host source, Docker RW beneath the gate; published leaves immutable at mode 0444 | owning Homie runner's private setuid publisher only; uid 10002 fails the gate |
| `/mc/records/output` | `MC_HOME/outputs/<run-id>/`, RW | current authorized output-writing pipeline run only |
| `/mc/records/inputs/outputs/run-<producer-run-id>/item-<n>` | one exact completed output/evidence file or directory, RO | role/subject brief reference only |
| `/mc/records/inputs/corrections/correction-<n>` | exact `corrections/mc-<task-id>-corrections<n>`, RO | exact role/subject brief reference only; one destination per correction number |
| `/mc/records/inputs/revision` | exact `revisions/<task-id>-OP-REVISION.md`, RO | re-entered subject role only |
| `/mc/records/inputs/context` | exact `context/<task-id>-STEER.md`, RO | next subject role only |
| `/mc/records/correction` | exact next `corrections/mc-<task-id>-corrections<n>`, RW | current Verifier for that task only |
| `/mc/workflow/plan.js` | exact `MC_HOME/workflows/<run-id>-plan.js`, RW | current claude-sdk adapter only |
| `/mc/network/policy.json` | generated non-secret ADR-018 policy, RO | every agent |
| `/mc/gateway/ca.crt` | selected gateway CA certificate, RO | gateway-using agent only |
| `/etc/resolv.conf` | generated ADR-018 resolver file, RO | every guarded agent |
| `/mc/spine` | deployment-derived runtime-local named volume; RW only through setuid `mc` ownership | every agent and helper that needs the spine |
| `/app/src` | exact release runner tree, RO | every agent, never helper/setup/landing |
| `/home/agent` | `MC_HOME/state/worksources/<scope-id>/home`, RW synthetic home | every agent; Homie uses its operator-scope id |
| `/home/agent/.cache/pnpm`, `/home/agent/.cache/uv`, `/home/agent/.cache/pip`, `/home/agent/.cache/cargo` | the four exact derived package-cache roots, RW | declared tool caches only |
| `/home/agent/.codex` | selected Codex canonical control directory, RW | `materialized` Codex binding only |
| `/home/agent/.claude` | selected Claude canonical control directory, RW | `materialized` Claude binding only |

Setup and landing are separate no-agent effect classes with their own complete
mount tables; they never inherit the agent table:

| Class/destination | Typed source and access |
|---|---|
| setup `/repo/source` | one exact registered real Git Worksource root, RO; present only while reading a pinned closure for first-task or committed-projection materialization |
| setup `/repo/source/.mission-control` | generated empty directory, RO nested cover over all task/projection roots whenever `/repo/source` is present |
| setup `/repo/task` | exact mode-0555 task-local skeleton root, always RO |
| setup `/repo/task/source` | exact skeleton child, RW only for first create, accepted-seal rebuild, retry reconciliation, or exact pre-removal emptying; omitted otherwise |
| setup `/repo/task/git` | exact skeleton child, RW for the same closed task-repository operations; omitted otherwise |
| setup `/repo/seal` | exact completion-referenced seal root, RO; present only while rebuilding/reconciling the matching task-local repository. No gate: setup carries no model or harness, and the read-only bind is the whole denial |
| setup `/repo/projection` | exact precreated execution projection root, RW; present only while materializing a committed-tree or disposable Verifier source |
| setup `/mc/setup.json` | action-bound operation, pinned/accepted SHA, derived path, expected identities, and source-object-format evidence, RO |
| landing `/repo/source` | one exact registered real Git Worksource root, RW, intentionally including its primary checkout solely for import/ref/merge/branch-cleanup |
| landing `/repo/source/.mission-control` | generated empty directory, RO nested cover; the same task bytes are reachable only through separate RO `/repo/task`, never through a stronger alias |
| landing `/repo/task` | exact sealed task-local root and reviewed repository, RO |
| landing `/mc/landing.json` | exact task, local/real branch, verified SHA, target ref, pre-merge SHA, closure digest, landing action identity, expected Git topology, and cleanup path, RO |

Each action receives only the rows it names. Both use the baked fixed setup/
landing binary, `network=none`, a cleared environment plus generated safe Git
configuration, and no harness, runner-source bind, spine, session, HOME/cache,
runtime-control, attachment, output, or Docker-socket mount. They reject
repository includes, hooks, executable filters/processes/merge drivers,
replacements, grafts, alternates, and any Git environment influence; landing
also fixes author/committer/message inputs for replay. They perform only the
closed operation in the RO plan. No agent/model process is present.

For an accepted standalone task, the canonical host worktree is
`<workspace_root>/.mission-control/tasks/task-<task-id>/source`, and its
task-local repository is the sibling
`<workspace_root>/.mission-control/tasks/task-<task-id>/git`; a clean
projection is `<workspace_root>/.mission-control/projections/<execution-id>`.
Onboarding
creates the operator-owned non-symlink `.mission-control/{tasks,projections}`
roots and adds the literal `.mission-control/` root to the real repository's
local `info/exclude`; a pre-existing foreign, tracked, symlinked, or wrong-mode
root rejects. The resident precreates one empty identity-fenced task/projection
root; for a task this means the fixed empty `source`/`git` skeleton above,
whereas a projection root has no children until its one setup action. Setup
therefore never needs sibling authority. Relative pointers work unchanged
on the host and under the `/workspace` task-root bind; Git ≥2.48 is required.
The operator can run read-only `status`, `diff`, `log`, and `blame` at the
canonical host task path without access to the real repository. Clean
projections are execution-scoped residue and are removed exactly. A cancelled
pre-landing standalone task is stopped, exact-emptied by setup, and then its
still-matching empty root is removed by the resident; no real ref/object was
created. Run-keyed seal roots are removed only under the producer/lease/request
fence above and when no accepted completion, retry, review, or landing row
references their exact identity/digest.
Initiative paths are not allocated under this convention while their
representation is Parked.

Landing is topology-fenced and idempotent; it creates no third authoritative
receipt file. The canonical spine landing-pending/action identity plus exact
Git topology are the recovery truth. The fixed binary first revalidates the
sealed local branch, reviewed SHA, exact reachable closure/digest, safe local
controls, real repository/target identity, and the spec's primary dirty-tree
fence. It imports only that closure into the real object store without a
hardlink, alternate, or speculative deletion, then fsyncs and verifies the
imported objects. A crash here may leave unreachable but valid imported
objects; retry reuses them by hash and never guesses them safe to delete.

The CAS-created real `refs/heads/mc/task-<id>` at the verified SHA is the
durable import stage marker. Retry accepts it only for the same action/SHA,
rechecks the SHA fence, and runs the required
`git merge --no-ff <verified-sha>` in the primary checkout with disabled hooks/
external drivers and fixed metadata. Merge-in-progress files are accepted or
aborted only when they match this action. If the target ref moved but the
resident never received success, retry adopts the merge only when its exact
action trailer, parents, tree, target preimage, worktree/index state, and
verified SHA all match; ambiguity blocks. That exact merge topology is the
durable merged marker reported into the spine. Only after the spine records
success may a later trusted landing/setup action delete the exact real task
ref and exact-empty/remove the separate task-local root. Every import/ref/
merge/report/cleanup crash cut converges without a generic scan of another
task or landing identity.

MiniMax's gateway delivery has no control-directory mount. No profile may
choose a destination from this table or add another `/mc`, `/app`, `/home`,
`/etc`, seeding, operator, trace, attachment, or records destination.

The accepted non-initiative durable-record grant is also closed by role.
Initiative/child rows do not inherit this table while their mount design is
Parked:

| Pipeline role | RO durable-record inputs | RW durable-record outputs |
|---|---|---|
| Standalone-task Worker | only the immutable brief's exact latest output, current correction, revision, and steer references for its subject | own run output root |
| Verifier | only the subject's exact Worker output and prior criterion inputs named by the brief | own run output root plus the one next correction file |
| Packager | only the subject's exact output, verdict evidence, correction/refinement history files named by the brief | own run output root |
| Refiner | only the current packet/evidence and exact revision inputs named by the brief | own run output root |
| Strategist(console) | none; its board snapshot is inline in the immutable brief | own run output root |
| Editor or Strategist(propose) | none | none |

The current output root is created after claim for that run and is the only
place beneath `outputs/` it can write. A returned logical path must be
normalized beneath `outputs/<own-run-id>/`; `mc` maps it to the exact
mounted root and refuses a sibling or prior-run reference. Exact correction,
revision, context, and prior-output sources are derived from canonical spine
references and fenced task/run/correction identities, never accepted from a
model-supplied host path. An expected-absent workflow/correction/output source
is precreated or mkdir'd by the resident from the immutable effect; an
unexpected existing identity refuses or follows the ADR-016 exact-retry rule.
The workflow file is owned by the runner/adapter capability and is not a
general model artifact path.

The session-directory grant is additionally shape-checked. A fresh owner
starts from an exact empty, non-symlink directory; a Homie resume starts with
exactly its registered regular trace and no sibling. The adapter fixes the
harness's native-session destination beneath `/mc/session`, and the trusted
runner validates the harness-specific filename/identity as soon as it can
register the locator and again after the harness closes. Any extra entry,
symlink, second native candidate, or trace replacement makes the session
unfinalizable and ineligible for Decision 7. After the container is stopped,
the exact sanitizer may unlink only proven non-registered, execution-created
residue inside that owner folder; it never deletes/replaces the registered
trace or the folder. Crash reconciliation runs the same bounded directory
check. Tests do not claim the shared-uid model/runner grain prevents a model
from attempting a sibling write while live; they prove the retained durable
folder converges to trace-only and never treats that residue as a record.

`run.json` carries the exact logical identity→destination map. Consumers do
not infer it from host basenames, directory enumeration, or array order.
External content binds and exact record inputs are checked for source and
destination equality/ancestor overlap after construction. The only nested
Docker destinations are the four cache overlays and one selected harness
control overlay beneath `/home/agent`; the standalone task-root bind with its
explicit Worker RW source/Git child overlays, Verifier disposable-source
overlay, fixed Git/control covers, and external artifact/reference children; the inert
`.git` and `.mission-control` covers for committed projections/Homie; and the
Verifier's RO task root with an execution-scoped RW source child overlay.
HOME sources are distinct. Every one of these parent-before-child covers is a
named table edge with exact source identities and fixed order; no other
overlap is accepted. `/workspace` for non-standalone roles and unmounted
structural parents beneath `/mc` are rootfs directories, so their child bind
points are not aliases. No current output is simultaneously mounted as an
input.

A candidate has at most 256 total mounts and 128 exact durable-record input
mounts. Structural mounts, external roots, and record inputs all count. The
historical projection always counts as one mount regardless of entry count.
The canonical serialized mount plan is at most 1 MiB; one logical id is at
most 255 bytes and one source/destination obeys the 4096-byte path bound.
Exceeding any bound rejects the complete candidate; it never truncates, drops,
or coalesces entries. Final mounts sort by `(destination,logical_id)`;
reordering profile arrays produces identical plan bytes. Every collision or
overlap rejects rather than being repaired by suffixing or mount order.

## Decision 7 — Inv. 26 historical trace access

A run's own `sessions/<run_id>/` directory is mounted RW only into that run;
a Homie resume remounts its own folder. No other container gets that
directory.

A Homie gets **one** RO bind of a derived, owner-keyed hardlink tree containing
only finalized pipeline-run traces:

```text
MC_HOME/projections/homie-traces/v1/
  pipeline/<first-two-id-hex>/run-<16-lower-hex>/native.jsonl
```

This root is mounted at `/workspace/operator/traces`. The shard and run-owner
folder retain canonical ownership identity; there is no flattened basename
namespace. The original `sessions/<id>/` folder is never mounted, and no trace
entry is a Docker mount. Thus history growth does not consume the 256-mount
plan bound.

A projection entry is eligible only when:

- its pipeline Run row has finalized immutable locators and terminal writer
  state;
- no lease, active writer, or matching live container remains for that exact
  run;
- `session_path` is exactly the MC-derived folder for that id;
- `trace_filename` is a basename, not a path; and
- the resolved source is a regular non-symlink file inside that exact folder,
  with trusted source ancestors and stable `(device,inode,type)` evidence.

The projection root and `sessions/` must be on the same filesystem. The host
reconciler opens every validated source/target ancestor as a retained
no-follow directory fd, records each identity, and `fstatat(...,
AT_SYMLINK_NOFOLLOW)`s the source basename immediately before
`linkat(source_dirfd,source_basename,target_dirfd,target_basename,0)`. After
linking, it `fstatat`s **both** names again and requires each type/device/inode
to equal the retained pre-link source evidence as well as
`os.SameFile(source,entry)`. Thus swapping the source between eligibility and
link cannot bless the replacement merely because the two post-swap names
match. A failed post-link proof unlinks only the newly created target through
its retained target dirfd; it never acts on the source path.

A byte copy, clone/reflink, symlink, bind of the source folder, path-based
ancestor reopen, or fallback on `EXDEV` is forbidden. An existing target is
accepted only when it was already the exact retained hardlink; any conflicting
identity makes the projection unhealthy. Missing or unexpected entries are
linked/unlinked only through retained dirfds inside the derived projection
root. Reconciliation never writes, renames, chmods, or deletes a source
session file.

The tree is derived and rebuildable from canonical finalized locators, not a
second record or unredacted byte store. The projection root is created once
with a captured identity and reconciled **in place**; it is never replaced or
renamed while a Homie has it mounted. Missing entries can be rebuilt, but a
missing or identity-changed root while consumers are live is unhealthy: all
such Homie launches are fenced/stopped before a new root is created. Absence
cannot make the reconciler copy session bytes. A hardlink capability/
same-filesystem probe is mandatory before Homie confinement is healthy.
Canonical pipeline locators are read in stable id pages of at most 256; the
reconciler must reach and attest the current high-water mark before a Homie
wake may use the root, so the private dispatch frame never expands with
forever-growing trace history. Incremental reconciliation uses the same rules
and sharding.

Pipeline runs never resume, so an eligible inode can never become a live
writer later. **Homie-owned native traces are never linked into this shared
projection**, even while their rows are ended/reaped: those sessions are
resumable, and unlinking would not revoke an fd already opened by another warm
Homie. A Homie sees its own native session through `/mc/session`; it reads
other Homie conversations through canonical conversation rows (`mc homie
history`), not another Homie's unredacted native file. This matches §15.3's
cross-Worksource exception: pipeline runs belong to Worksources; Homie
sessions do not. The stable projection directory remains mounted into warm
Homies, so newly finalized pipeline entries appear without remounting.
Consumers enumerate by polling and never rely on fsnotify across the VirtioFS
bind.

## Decision 8 — per-session attachment file plane

Each Homie owns one stable, content-addressed two-direction tree, created
before its first launch and retained across warm turns and resume:

```text
MC_HOME/attachments/<session-id>/
  in/staging/<publication-id>.part
  in/journal/<publication-id>.json
  in/published/sha256/<first-two-hex>/<64-lower-hex>
  out/staging/<publication-id>.part
  out/journal/<publication-id>.json
  out/published/sha256/<first-two-hex>/<64-lower-hex>
```

Every publication belongs to a batch whose complete expected manifest is
sealed in spine metadata **before any durable attachment byte is staged**. The
manifest declares canonical contiguous ordinals `1..n`, `n <= 32`, exact byte
size, media type, and display name for each object. Each size is at most 256
MiB and their sum is at most 512 MiB. One transaction creates every
publication reservation and allocates those exact byte credits; a publisher
must produce exactly its declared size, and no undeclared/missing/extra ordinal
can be consumed. Parallel publishers therefore cannot stage more than the
batch budget before a later aggregate check. A previously learned digest for
an ordinal is immutable across attempt retries. Publication rows and journals
carry only identity/digest/size/state; attachment bytes never enter the spine
or dispatch frame.

Inbound intake is durable before file work. The native surface invokes
host-scoped `mc attachment ingress-enqueue` under `BEGIN IMMEDIATE` with the
exact active session/binding, stable `(surface,message_id)`, bounded message
body, and sealed attachment manifest. The stable ingress row is unique by
`(session_id,binding_id,surface,message_id)` and has
`queued|publishing|consumed|failed`; its immutable body/manifest are the native
inbox. Any finite queue is read in 128-row sequence pages, so a second
Discord/browser event is durably enqueued while an earlier large upload is
still publishing rather than held only in native memory or left to external
redelivery.

Only the oldest queued ingress for a session receives a 16-lowercase-hex
attempt generation and becomes `publishing`; at most one byte-producing
attempt is live per session, while later ingress rows remain durable and
byte-free. Retry while active returns the same attempt. Its renewable
publisher ownership lease is at most 30 seconds from native progress, and the
attempt's non-renewable hard deadline is 15 minutes after creation. Explicit
attempt abort or hard expiry
removes that attempt's liveness edges through the GC fence and returns the
same stable ingress row to `queued`; the next attempt atomically receives a
fresh generation. After three failed/expired attempts, the stable row becomes
`failed` with an immutable sanitized failure receipt, a surface-visible
error/outbox event is written in that transaction, and the next queued ingress
may proceed. The failed body/manifest is retained; an explicit host-scope retry
after repair may requeue it, but no automatic retry can recreate head-of-line
starvation. A consumed stable row instead returns its original
conversation result forever and can never allocate another attempt.

`mc homie send ... --ingress-attempt <id>` requires every manifest ordinal
materialized, appends the inbound conversation row/references, and marks the
stable ingress, attempt, batch, and publications consumed in one transaction.
Thus crash recovery cannot duplicate the conversation or discard a valid
concurrent surface event. The Homie receives only `in/published` at
`/mc/attachments/in` RO; it never sees inbound staging or journals.

Outbound uses the same sealed-batch reservation under the current runner-held
turn claim/generation. The runner may not release that claim or claim a later
turn until every declared publication is consumed in the reply transaction or
the batch is explicitly terminal-aborted. Stale/cross-session generations and
changed declarations refuse.

Both directions use the same crash-safe publisher. It writes only the exact
reserved staging name with `O_EXCL`, stops at the exact declared byte count,
hashes/fsyncs/closes it, and changes the stage leaf to read-only mode 0444. It
writes the journal under the one exact temporary name, fsyncs it, atomically
no-replace renames it to the final journal name, and fsyncs the journal
directory. The journal binds the staging identity, digest, size, canonical
destination, batch/attempt, and reservation key. Only after that durable final
journal exists does a short spine transaction bind this publication to an
`attachment_objects` row keyed by
`(session_id,direction,sha256,size)`. That row stores the registered canonical
device/inode/type once live, or the exact creator publication/journal/staging
identity while `publishing`, plus the exact state
`publishing|live|delete_pending` and a GC generation. A new object is
`publishing` and owned by this publication; reuse
of an exact `live` object adds this publication's liveness edge. A
`delete_pending`, unregistered, identity-changed, or digest-mismatched leaf
refuses rather than racing cleanup.

Each staging publication has its own random owner token, native boot or runner
launch generation, and 30-second lock-domain lease bounded by the batch/attempt
hard deadline. The private publisher heartbeats that exact row while streaming
and CAS-rechecks token/generation/state immediately before final-journal rename
and again in the object-binding transaction. Reconciliation may touch a
pre-journal stage only after `BEGIN IMMEDIATE` proves the publication lease
expired or its owner generation dead and CASes the row to `cleanup_pending`.
Once that CAS wins, a late publisher cannot renew, journal, or bind; it closes
its descriptor and returns fenced. An open-but-unlinked inode can therefore
never later become the identity recorded by a successful publication.

A crash before final-journal durability is normal reserved-state residue: if
no object binding exists and the cleanup-pending owner fence above has won,
reconciliation validates the privileged staging/journal parents, exact
reserved basename, and regular-file type, dirfd-unlinks the partial stage and
journal temp, and restarts that ordinal. A missing or partial final journal is
unhealthy only for an object-bound active publication; it is expected in
`cleanup_pending` and does not poison an otherwise untouched batch.

Only after that durable liveness edge exists does the publisher atomically
no-replace rename the stage to the digest path and fsync both directories. If
the exact registered live leaf already exists, the staging inode is
intentionally different and is discarded only after the canonical
device/inode/type/size and a fresh byte digest match. The materialization
transaction records the canonical identity, moves a newly published object to
`live`, and marks the publication materialized. The conversation row/reference
consumes those materialized publications in the same spine transaction; a
stale generation, missing ordinal, over-budget set, or partial set refuses the
whole message/reply.

The journal makes every filesystem/database cut convergent. For a `publishing`
object owned by its creator, recovery accepts exactly two locations: (a) the
journaled staging inode exists and the canonical leaf is absent, so it performs
the no-replace rename; or (b) staging is absent and the canonical leaf has that
same journaled device/inode/type, size, and digest, proving rename completed,
so it records `live`/materialized. Both locations present, both absent, a
different canonical identity, or any symlink/wrong type is unhealthy. For a
reuse publication, recovery freshly verifies the registered live canonical
leaf before discarding its distinct stage and marking materialized. An already
materialized key returns its receipt byte-for-byte. Aborting one publication
removes only its liveness edge. An
object is unlink-eligible only after a `BEGIN IMMEDIATE` query proves there is
no object-bound, materialized, or consumed publication for that canonical key
and no conversation reference. The same transaction CASes the exact live/
publishing object to `delete_pending` with a fresh GC generation and identity;
new publication binding refuses while that state is visible. The reconciler
then exact-unlinks that identity and records deletion under the same
generation. Crash before/after unlink replays from that row; it never lets a
new reuse race an old creator's cleanup. Ambiguous/missing journal or identity
evidence in an object-bound state makes the session unhealthy rather than
sweeping. A `delete_pending` retry from `live` probes the exact canonical leaf;
one originating from `publishing` probes both the journaled staging location
and canonical destination. Exactly one matching inode is unlinked and
directory-fsynced; both confirmed absent means a prior unlink completed, while
both present or any mismatched identity is unhealthy. It then deletes the
same-generation object row in one transaction.
Every conversation-referenced
canonical object remains immutable and retained forever; later turns cannot
replace/delete it and age-based cleanup never touches it.

Derived residue and metadata converge in bounded pages. The reconciler scans
ingress/batch/attempt/publication rows by stable id in pages of 128 and uses
only exact registered staging/journal leaves—never a generic attachment-tree
sweep. Before unlinking any terminal file, one transaction inserts the
immutable small batch receipt and CASes all exact terminal batch/attempt/
publication rows to `cleanup_pending`. For a consumed batch it first proves
the conversation/reply references now own every object; for an aborted batch
the same transaction removes its publication liveness edges. Missing exact
stages/journals are permitted only after this durable state.

The reconciler then unlinks and directory-fsyncs the registered stages/journals
and compacts the cleanup-pending attempt/publication rows into that receipt.
Crash at any cut repeats absent-or-exact unlink rather than treating normal
post-unlink absence as active-state corruption. After an aborted attempt, the
stable ingress remains queued with its immutable body/manifest and next
generation counter; after its third failure it retains the terminal failed
receipt instead. A consumed ingress is
compacted to a permanent minimal idempotency receipt containing the stable
key, body/manifest digest, and conversation sequence. Outbound keeps the analogous
turn/batch result receipt. Completed `delete_pending` rows are removed as
above. Thus canonical referenced objects and small idempotency receipts remain
forever by design, while stages, journals, terminal attempts, publication rows,
and deletion receipts do not grow without a terminating rule or require an
unbounded in-memory scan.

Outbound preserves §15.3's Homie write boundary. The Docker bind at
`/mc/private/attachments/out` is RW for the privileged publisher, but its
destination sits beneath the `/mc/private` image gate (owner `10001:10001`,
mode 0700), so model/harness uid 10002 fails the kernel's traversal check at
the gate and cannot traverse, create, overwrite, or delete there. Its host
source root and parents stay operator-owned beneath the operator-owned
mode-0700 `MC_HOME` of Decision 1 — no host actor can chown them to the setuid
uid, and none needs to — so §15.5's operator-uid native surface keeps the
direct read of published outbound leaves that the spec mandates, and can create
under the sibling `in/staging`. After a turn, the
trusted runner first closes the harness result's complete attachment manifest
and exact sizes, atomically reserves that bounded batch, then streams each
ephemeral result to a private own-session `mc attachment-publish` capability
that is absent from the model/subagent tool surface. Its reservation capability
is bound to the current runner-held turn claim/generation, batch manifest,
direction, ordinal, and allocated byte credit and is passed over private runner
transport, never through env, argv, a mounted file, or model output. The setuid binary applies the common protocol above only
inside the own-session `out` tree; the runner alone can consume materialized
rows in a reply transaction. It never writes bytes to the spine or a dispatch
frame. The within-container ability to discover or attempt an own-session
private verb is only the spec's accepted same-uid best-effort residue: without
the live reservation it refuses, and even a compromised current reservation
is capped/idempotent at 32 objects and 512 MiB. Direct durable filesystem
writing remains kernel-denied, stale/cross-session claims refuse, and
unreferenced bytes reconcile as above.

Attachments are enabled only after a mandatory Docker Desktop/VirtioFS canary
under a scratch subtree on the **same filesystem, Docker share, ownership, and
ACL regime as configured `MC_HOME`**. With both mounts already live, the
host-native surface publishes a new inbound object and the warm uid-10002
consumer reads it through the RO mount; setuid euid/fsuid 10001 then publishes
a new outbound object and the already-running host-native surface opens it.
The same warm uid 10002 receives `EACCES` for direct outbound traverse/open/
create/rename/unlink at the `/mc/private` gate. The canary restarts the
container and reopens both old objects to prove persistence. It proves the
composed shape — operator-owned host sources under `MC_HOME`, gated container
destinations — and assumes nothing about how the share presents a host inode's
uid, gid, or mode; the host modes the privileged publisher and the native
surface need are whatever it proves. Failure leaves attachment/Homie
confinement unhealthy and refuses launch; there is no fallback that relaxes the
gate, changes ownership, widens modes/ACLs beyond the proved shape, gives uid
10002 writable staging, or transports bytes through the spine.

An attachment reference is typed by `(session_id,direction,sha256,size,
media_type,display_name)`. The digest selects the fixed sharded path; no row
contains a caller-supplied filesystem path. `display_name` is metadata, at
most 255 control-free UTF-8 bytes, never joined to a path. The enforced
reservation bounds above are part of the typed-reference proof. Every
referenced leaf and ancestor is revalidated non-symlink; the leaf must be a
regular file whose identity, size, and digest match before runner/native open.
A mismatch refuses the whole turn/reply reference set rather than dropping an
item.

Neither direction lives under `sessions/`, so the trace-only invariant remains
literal. No pipeline run, other Homie, trace projection, or ordinary profile
mount can see the tree. The two published roots are stable per session, not
per launch or attachment, and retain the same identity across resume, so a warm
Homie/native surface sees newly published references without remounting. Both
sides poll canonical reference state and never trust VirtioFS fsnotify.

## Stable rejection codes

```text
mount.allowlist_untrusted
mount.allowlist_invalid
mount.source_missing
mount.source_wrong_kind
mount.source_blocked
mount.symlink_escape
mount.not_allowlisted
mount.denied_root
mount.cross_worksource
mount.rw_not_permitted
mount.target_invalid
mount.source_alias
mount.target_collision
mount.identity_changed
mount.runtime_unappliable
mount.gate_unhealthy
```

Every code aborts the whole plan; none drops one mount.

## Consequences and acceptance

The fast table covers: empty deny-all allowlist; owner/mode/ACL trust;
in-root/escaping/swapped symlinks; sibling-prefix and case-distinct paths;
every blocked component/glob class; direct `.env` vs an
ordinary workspace containing one; protected ancestors; all four RO/RW
cells; source kinds; real-HOME broad root vs an allowed descendant; selected
vs other runtime-control dirs; own/other/parent Worksource; and missing or
untrusted allowlist files. Target cases include absolute, `.`, `..`, colon,
backslash, control, overlong component/path, duplicates, and ancestor
collisions; the paired source case proves an allow root containing a colon is
accepted while a descendant suffix containing one is rejected. Every case
revalidates the complete constructed destination rather than trusting its
parts.

Committed-view tests place distinct uncommitted, staged, stashed,
aborted-commit, manually hashed, dangling, primary-index, and repository-config
sentinels in the real Worksource. First setup proves the task-local store
contains exactly the object closure reachable from the pinned commit and the
source tree contains only its bytes; every other sentinel is absent from that
store, ordinary committed projections, and Strategist(propose) views. The
suite covers exact retry reuse, foreign task/projection residue, ref drift,
read-only non-repository roots, and Homie's intentional live-workspace RO view
with `.git`, `.mission-control`, and an additional descendant common/object
directory all hidden. A read-only linked/external-control fixture proves an
outside control path is absent and every inside control path is covered; a
bare fixture receives only its clean committed projection and cannot enumerate
real refs/config/objects. A v1 mutable linked-worktree, bare, submodule,
external-common-dir, alternate, replace/graft, shallow, promisor,
executable-config/filter/hook, or unsupported tree-mode fixture refuses; the
same filesystem root may still register explicitly read-only under those
Homie projection/cover rules.

In the task-local Worker view, representative status/add/commit advances only
its local `refs/heads/mc/task-<id>` and new loose objects. The real Git store is
absent, while local `config`, pointer files, `objects/info`, and the sealed pack
reject writes. A task-local sibling ref, dirty index/tracked path, or unsafe
control makes the in-verb completion precondition refuse. Successful
completion creates a privileged immutable pack/manifest containing exactly
the accepted head closure and binds its identity/digest into the spine
transition. Crash cuts before seal publish, after publish/before transaction,
and after transaction converge; uid 10002 cannot open/mutate the seal. A
fixture moves/deletes the local branch/objects after successful completion and
before container exit, then proves post-stop setup rebuilds solely from the
seal and the next role sees the accepted SHA.

Verifier receives a disposable RW `/workspace/source`, successfully creates
and changes representative build/test files, and reads status/diff against the
sealed RO task-local Git store with optional locks disabled. Its add/commit/
control writes fail, its ordinary one-phase verdict succeeds, the canonical
task source remains byte/identity unchanged, and its entire disposable source
is removed after exit. Packager and Refiner receive canonical source/control
RO and fail representative writes while their separate record outputs remain
writable. Relative `.git`, `commondir`, and `gitdir` resolve in container and
on host; generated `config.worktree` is empty and RO. The host runs read-only
status/diff/log/blame successfully at the canonical task path.

Setup/landing inspect tests assert the exact operation-specific rows in their
tables—including the RO task skeleton, only the operation's RW source/Git
children, source/projection/seal mounts, both
`/repo/source/.mission-control` covers, and the RO landing task root—and absence
of every agent mount. Malicious source config/include/hook/filter/merge-driver
fixtures execute nothing. Final-uid setup writes inside the two precreated
children but cannot chmod the parent or create a sibling; cancellation empties
only those children before the native resident identity-checks and removes the
skeleton. Landing tests import only the reviewed closure,
CAS-create the exact real task ref, SHA-fence, merge in the primary checkout,
and exact-clean the real ref/local root after spine success. Every import/ref/
merge/report/cleanup crash cut replays from the canonical landing row plus
exact Git topology; no third receipt file appears, no imported object is
speculatively deleted, and the task root has no stronger RW alias through the
real workspace mount. No effect runs host Git and no failure falls back to an
agent-visible primary checkout.

A single-Worksource leased role without a task worktree receives the exact
pinned committed projection RO at `/workspace/source`; its dirty-primary and
Git-control sentinels are absent. A canonically filesystem-free candidate has
no source mount. An initiative/child candidate that needs the parked shared
worktree is refused as unsupported rather than receiving a standalone
worktree, committed projection, or live primary fallback.

The structural-plan table covers every **accepted non-initiative** role/tier
combination plus fail-closed initiative/child refusal. It proves exact
presence, mode, source identity, destination, and absence of all non-granted
siblings: task root/local Git/fixed covers; disposable Verifier source;
completion seal; output runs; correction/revision/context inputs; Verifier
correction output; workflow capture; own/other attachments; own session; one
trace root; runner; spine; sandbox/run/network/CA/resolver files; synthetic
home; all four cache overlays; selected/other/no harness control; Homie's Git/
MC covers; workspace, artifacts, and references. Role/run/task substitution
rejects. Current output paths cannot escape `outputs/<own-run-id>` or alias an
RO prior output. Only the explicitly enumerated parent/child covers may nest;
mount ordering cannot change the result.

Trace tests create source and projection entries on one filesystem and assert
`os.SameFile`, equal device/inode, the owner-keyed shard path, and exactly one
RO Docker bind. They reject copy, symlink, reflink/different inode, `EXDEV`,
active writer/live container, wrong owner/path/basename, and conflicting
projection residue. A swap precisely between pre-link evidence and `linkat`
fails the post-link comparison to retained evidence and unlinks only the new
projection entry through its dirfd. Deleting and rebuilding the derived tree
reproduces the same links without touching source bytes when no consumer is
mounted; a root identity loss with a warm consumer fences that consumer before
recreation. A warm-Homie polling test observes a later finalized pipeline trace without
remount. Ended/reaped/active Homie traces are always absent, and an open-fd
resume test proves no shared Homie-trace inode exists to observe later
appends. A pipeline history larger than both one 256-row page and the old
mount limit still produces one projection mount.

Attachment tests prove stable warm visibility, inbound RO, uid-10002 direct
outbound `EACCES` at the `/mc/private` gate, private own-session publisher
success, operator-uid host reads of published outbound leaves and creates under
inbound staging, cross-session and
direction/stale-claim denial, content-address/digest/size/canonical-identity
fencing, symlink/staging/journal-name refusal, atomic no-replace publication,
and old-reference bytes unchanged across later turns/restart. Every cut around
batch reservation, stage write, journal-temp fsync/rename/dir-fsync, object
binding, canonical rename, materialized metadata, conversation commit, and
cleanup is injected. Pre-journal residue restarts rather than poisoning the
session; a live slow publisher's stage survives reconciliation, while an
expired-owner cleanup CAS fences the late publisher before journal/object bind.
A `publishing` cut converges through each closed stage/canonical location.
Retry finishes the same idempotency key; changed bytes/metadata refuse.

Inbound tests durably enqueue two surface messages while the first large batch
publishes, recover the same active attempt after each crash cut, consume it
atomically with `homie send`, and then process the second without relying on
external redelivery. Explicit/hard-expiry abort returns the stable ingress to
queued with a fresh next attempt; three permanent failures produce one
surface-visible failed receipt and let the second ingress advance, while an
explicit repaired retry remains possible. Retry of a consumed message returns
the original conversation result. A 32-publisher race cannot allocate or stage
more than its predeclared 512-MiB credits; missing/extra ordinals or a mismatch
with the declared size refuse before consumption.

Two unconsumed publications reuse one digest; aborting its creator cannot
remove the other liveness edge. The final edge moves the object through exact
`delete_pending` fencing, while concurrent reuse refuses until unlink and state
completion are both exact. Reused, materialized, consumed, or conversation-referenced
objects remain. More than 128 mixed queued/terminal rows prove paged
reconciliation first marks exact consumed/aborted batches cleanup-pending,
then removes stages/journals and compacts receipts across every crash cut,
reaching a stable high-water without an unbounded scan. A hostile same-uid caller cannot
exceed the live claim's 32-object/512-MiB budget or reuse an ordinal, and a
256-MiB+1 declaration/stream refuses before publication.

The mandatory canary starts with mounts live on a scratch subtree under the
configured filesystem/share/ACL regime, observes host→warm-container inbound
publication and setuid→already-running-host outbound publication, proves warm
uid-10002 outbound denial, then restarts and reopens both old objects. Any red
arm rejects with no permission-widening fallback. Exact boundary rows cover 32
objects, 256 MiB each, and 512 MiB aggregate; no attachment lands beneath a
trace folder.

Plan/policy bounds accept exactly 256 allow entries, 128 additions,
128 record inputs, 256 total mounts, and 1 MiB canonical plan, reject the next
entry/byte without truncation, and prove array-order determinism and
source-alias rejection. Three correction inputs map to three numbered
destinations without collision.

The Docker mechanism test proves a source with spaces/colon applies through
structured mounts; an image whose `/mc/private` is missing, not a directory,
not owner `10001:10001`, or not mode 0700, and any plan placing a
non-privileged destination beneath the gate, reject with
`mount.gate_unhealthy` before start; Docker-unshared/unappliable sources abort
pre-claim; and
an inspect-reported RW/target/source mismatch, missing fixed destination,
extra destination, or host identity change is removed before start. In-agent
probes prove the dirty/real primary and object store, sibling task/projection/
seal roots, other session and attachment roots, complete `MC_HOME`, operator
HOME, runtime socket, gateway private material, primary index/config/hooks,
other worktree metadata, and non-selected controls are absent. Homie's nested
`.git` and `.mission-control` paths resolve only to inert covers. Permission/ACL, blocked-policy,
allow-root, and protected-set changes with unchanged source bytes/inode also
remove the unstarted container. A session-extra-file crash test never
finalizes or projects the residue and converges the retained folder to its one
registered trace. Inspect never claims to prove an inode.

## Alternatives rejected

- A line-oriented `mode path` format is ambiguous for spaces/escaping.
- Lexical prefix comparison fails under symlinks, APFS case behavior, and
  sibling prefixes.
- Nested allow roots with “most specific wins” still let a broad root expose
  the supposedly narrower one.
- Silent RW→RO downgrade or mount dropping changes run semantics.
- Host basenames, ordinal destinations, truncated hashes, or auto-renaming
  can collide and make mappings implicit.
- Recursively banning blocked names inside a Worksource contradicts §5's
  intentional Worksource tool-secret visibility.
- Mounting the sessions tree or each completed trace into Homie either breaks
  Inv. 26 or eventually exhausts the finite plan. Copying/reflinking traces
  makes a second unredacted byte store; symlinks re-expose source-directory
  traversal. The one same-inode, owner-keyed hardlink projection preserves a
  single byte record and a fixed mount count for immutable pipeline traces.
  Ended Homie traces are excluded because resume can append to their inode;
  unlink cannot revoke another warm Homie's already-open descriptor.
- Binding a mutable primary checkout RO for seeding still exposes the
  operator's uncommitted state. A pinned committed-tree projection is the only
  leased read view of that repository.
- Sharing the real common Git object store while hiding primary refs still
  exposes staged, stashed, aborted, manually hashed, and dangling operator
  bytes through object enumeration. The closure-only task repository plus
  landing-time exact import is the smallest design that makes §5 literal.
- Mounting all of `outputs/`, `corrections/`, `revisions/`, `context/`,
  `workflows/`, or `attachments/` is simpler but turns one referenced record
  into sibling authority and makes stale-run writes possible.
