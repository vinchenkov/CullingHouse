# ADR-021 — Mount jurisdiction: the protected set and cross-Worksource law

## Status

**Accepted after adversarial review — substantially reworked by it** (2026-07-14).
TDD next.

Resolves the nine gaps ADR-017 leaves in its own Decision 5 + Decision 3 step 5,
enumerated in `docs/reviews/2026-07-14-adr-017-jurisdiction-extract.md` §8. It
**adds nothing to ADR-017's policy** — every rule below is ADR-017's, and this
ADR only decides the things ADR-017's text left undecided. ADR-017 remains the
authority; where this ADR and ADR-021 disagree, ADR-017 wins, and where ADR-017
and the spec disagree, the spec wins (AGENTS.md).

**The review (`docs/reviews/2026-07-14-adr-021-review.json`) raised 16, confirmed
13 (7 major), refuted 3 — and the draft it judged (`c120c5c`) was not
salvageable by patching.** Four decorrelated lenses, each in its own worktree and
told to *drive real paths rather than reason from quotes*; then an independent
skeptic per finding. That instruction is what found the fatal defect:

> A skeptic implemented the draft's D1 + D3 + D4 **verbatim against the real
> `mc/boundary` code** and ran it. All seven of ADR-017's typed grants rejected —
> `/mc/session`, `/home/agent`, `/mc/records/output`, `/mc/workflow`, the
> completion seal, `/mc/attachments/in`, `/workspace/operator/traces`. **No
> container could ever have launched.** The draft's own test list would have
> stayed green, because it pinned only the deny side.

The root error was conceptual, and worth stating plainly so it is not repeated:
the draft read ADR-017:396-401 as a **hole in the union** and tried to carve typed
grants out of it. It is a **second predicate** — *"That union governs
ordinary/profile-requested mounts. A typed system mount is **instead** confined to
its one kind-specific authorized root"*. Confinement to one identity is stricter
than avoiding a set, not weaker. The draft's `Rejects(id SourceIdentity)` carried
no kind and so could not express it, and the draft twice deferred to "D5's
per-class check" — a mechanism that did not exist anywhere in the document (D5 is
about HOME). **D10 is that decision, now written.**

What the review changed:

- **D10 (new)** — typed grants are confined per class, a second predicate. The
  missing decision the draft twice promised.
- **D11 (new)** — D9 was a half-measure: re-running `Rejects` against a stale
  `Jurisdiction` reproduces the stale answer, so `ResolveJurisdiction` itself
  must re-run at every call site (ADR-017:1339-1341 makes the protected set
  itself drift).
- **D1** — `JurisdictionInput` is pinned as a real struct. Its input is *mixed*
  (raw `denied_paths`, which may not exist yet, vs pre-resolved everything else);
  the draft asserted "already-canonical identities" while D2/D8 required the
  opposite, and both could not be true. `ProtectedID` carries an `fs.FileInfo`,
  not a `(device,inode,type)` triple, because `os.SameFile` consumes stat
  objects. The zero-value law now names its mechanism (an unexported `resolved`
  flag) instead of asserting the property.
- **D4** — the ancestor direction is **qualified**. Unqualified, it rejects the
  own Worksource's own `workspace_root`, which is necessarily an ancestor of its
  own `.mission-control` — the draft would have rejected the one mount the system
  exists to make.
- **D5** — the injected HOME is validated, not trusted: injection relocates trust,
  it does not remove it.
- **D8** — says *how* the ancestor/suffix pair is compared (component-wise, not
  the lexical prefix ADR-017 rejects) and how case is handled on APFS.
- **Tests** — pin the **permit** side first, plus an unenumerated `MC_HOME` child
  and the own-vs-other ancestry pair.
- Five cite corrections, and the honest admission that the fourth-parameter
  migration is a real edit to ~a dozen tests, not a zero-risk rename.

Three findings were **refuted** and are recorded so they are not re-litigated: a
hardlink rule (adding one would breach this ADR's charter and reverse an ADR-017
alternative rejected on the merits); an alleged cite truncation in D3 (the
exception clause is inside the cited range); and the severity of the dangling D5
pointers (a documentation defect, fixed above, not a design hole).

An ADR rather than an in-place amendment (the `c6ca202` precedent) because
these are *implementation resolutions of an accepted design*, not corrections to
it: ADR-017's text stays true, and a reader who wants to know why the code
reads as it does gets one document instead of nine amendment scars.

## Context

ADR-017 Decision 5 defines a **non-subtractable protected set** and Decision 3
step 5 places its check in the authorization walk. Both are precise about
*policy* and silent about several things an implementer cannot avoid deciding.
The codes `mount.denied_root` and `mount.cross_worksource` have existed unused
since `4380e0d` (`mc/boundary/identity.go:24-25`), and `Authorize`'s own comment
reserves the seat:

> `// Protected-root and cross-Worksource jurisdiction (step 5) are the`
> `// following slice and are NOT applied here.` (identity.go:228-230)

The full requirements extract — every union member with line cites, the existing
code shape, and the nine gaps — is committed at
`docs/reviews/2026-07-14-adr-017-jurisdiction-extract.md` and is this ADR's
factual base. It is not restated here.

Constraints this design is written against: ADR-017's own text (the union at
:366-386, bidirectionality at :388-389, `broad_root` at :390-393,
non-subtractability at :393-401, the typed-system exception at :349-353 and
:396-401, step 7's recheck at :264-269, the bounds at :167-169); the Phase-3
contract's purity rule (`docs/phase3-contract.md:41-44` — the policy package
*"reads only canonical spine … state"* and both sides of `mc dispatch` must agree
on one plan *"produced by the same pure policy package"*); spec §11.3:453; and
`mc/boundary`'s established shapes — `BlockPolicy`'s compiled-in floor
(`blocked.go:72-76`), the injected `ownerUID` (`identity.go:55`), and the
`MountError`/`errors.As` slug convention.

## Decision

### D1. The seam: a `Jurisdiction` value, injected, mirroring `BlockPolicy`

```go
// TypedClaim names the kind-specific root a typed system source claims.
// The ZERO value means "no claim" — an ordinary/profile-requested mount,
// which is what D10's union governs. A non-zero claim selects D10's
// second predicate instead.
type TypedClaim struct {
    Kind  TypedKind // session | own-state | runtime-control | projection | …
    Root  ProtectedID // the exact authorized root this kind may occupy
}

type ProtectedID struct {
    Canonical string      // the resolved path — retained for suffix work (D8) and messages
    Info      fs.FileInfo // the stat object os.SameFile actually requires
    IsDir     bool
}

type JurisdictionInput struct {
    // Operator-authored, RAW: these may not exist yet, so they cannot be
    // pre-resolved by the caller (D8 resolves them through their nearest
    // existing ancestor). Bounded at 512 (ADR-017:167-168), checked before
    // any stat.
    DeniedPaths []string

    // MC-derived and spine-derived, PRE-RESOLVED by the host caller:
    Home            ProtectedID   // the operator's real HOME (D5)
    MCHome          ProtectedID   // the whole tree (D3)
    MCHomeClasses   []ProtectedID // the fifteen enumerated classes (D3's belt)
    HomeClassRoots  []ProtectedID // ~/.ssh etc., only those present (D8)
    GatewaySecrets  []ProtectedID
    RuntimeControls []ProtectedID // EVERY runtime control dir, not just non-selected
    OwnWorksource   WorksourceRoots   // identity-based own/other discrimination (D7)
    OtherWorksources []WorksourceRoots // workspace/worktree/artifact/state/cache/tool-home
    GitControls     []ProtectedID // pre-resolved: this package owns no Git resolver
    MissionControlRoots []ProtectedID // <workspace_root>/.mission-control

    resolved bool // set only by ResolveJurisdiction; see the zero-value law
}

func ResolveJurisdiction(in JurisdictionInput) (Jurisdiction, error)

func (r ResolvedAllowlist) Authorize(source string, requested Access,
                                     blocked BlockPolicy, j Jurisdiction) (Authorization, error)

// Rejects is callable WITHOUT an allow-root match — typed system sources need
// exactly this. A zero TypedClaim means an ordinary mount (D10's union); a
// non-zero claim selects D10's kind-specific predicate.
func (j Jurisdiction) Rejects(id SourceIdentity, claim TypedClaim) error
```

**Resolves gap (h)** (no seat in the signature) and **gap (g)** (the Git-control
resolver this package cannot own).

A fourth `Authorize` parameter, not a field on `ResolvedAllowlist`. Three
reasons, in order of weight:

1. **Typed system sources bypass `match()` entirely** but still owe a check —
   ADR-017:349-351: *"Every typed system source bypasses the external allowlist
   requirement only. It still passes its kind-specific source-type,
   non-symlink, identity, containment, and cross-Worksource checks at plan and
   spawn."* A jurisdiction that lives on the allowlist is unreachable for the
   sources that most need it. `Rejects` is therefore exported and total on its
   own. **Which** check they owe is D10's business, not the union's.
2. It mirrors `blocked BlockPolicy` exactly — one more injected policy value,
   `Authorize` still a pure function of its arguments.
3. It keeps `mc/boundary` pure. The MC/spine-derived members arrive
   **pre-resolved** from the host caller; the package never queries the spine,
   the Git registry, or the environment (`phase3-contract.md:41-44`, and the
   only way contract:41's *"the warm helper cannot see unmounted host Worksource
   paths"* warning can be honoured).

**The input is mixed by necessity, and the ADR says so rather than implying two
incompatible things** (an earlier draft asserted "already-canonical identities
(`device`, `inode`, `type`)" in D1 while D2/D8 required raw, possibly
non-existent `denied_paths` — the two could not both be true). `DeniedPaths` is
raw because ADR-017:275-276 explicitly contemplates a deny path *that does not
yet exist*, which no caller can pre-resolve. Everything else is pre-resolved.

**`ProtectedID` carries the canonical path AND the `fs.FileInfo`, not a
`(device,inode,type)` triple.** D4 mandates `os.SameFile`, whose signature is
`os.SameFile(fi1, fi2 fs.FileInfo) bool` — it consumes stat objects, not
tuples, and the existing `resolvedRoot` (`identity.go:156-160`) already retains
`info fs.FileInfo` for exactly this reason. A triple would force this package to
hand-roll device/inode comparison, which is precisely the "clever" reimplementation
ADR-017:247-250 exists to forbid. The canonical path is retained alongside it
because D8's suffix work and every rejection message need it.

**The zero-value law, and its mechanism.** A zero `Jurisdiction` must **fail
closed** — `Rejects` returns an error for every source — rather than be an empty
set that permits everything. `BlockPolicy` gets this free by compiling its floor
into a package-private array (`blocked.go:72-76`, *"even a zero-value policy
cannot omit it"*); `Jurisdiction` **cannot**, because its members are injected.
The mechanism is therefore explicit: an unexported `resolved bool` that only
`ResolveJurisdiction` sets, and a first line in `Rejects` that refuses when it is
false. Stating the property without the mechanism is the kind of hand-wave that
produced the `applySpawn` seam in ADR-020; it is named here so it is built, not
assumed.

### D2. Non-subtractability: constructed, validated, and unremovable

**Resolves nothing ADR-017 left open — it pins the mechanism** for :366's
"non-subtractable" and :393-394's *"Allowlist membership never overrides
jurisdiction."*

`ResolveJurisdiction` is the **only** constructor. It takes the operator-derived
members (`denied_paths`) and the MC-derived ones (**HOME itself** — D5's
`broad_root` anchor — plus the MC_HOME tree and its classes, the HOME-class
roots, gateway/CA, sessions, every runtime-control dir, other Worksources'
roots, Git control dirs, and the `.mission-control` roots) and returns a value
whose members cannot afterwards be removed: no setter, no
exported field, no negation form, no `config.toml` key. `denied_paths` is
**purely additive** on top — the operator may add jurisdiction, never subtract.

The 512-`denied_paths` bound (ADR-017:167-169) is checked **before any `os.Stat`
or identity walk** and rejects rather than truncates: *"A boundary excess rejects
before identity walking or allocation; none of these collections is truncated."*

An allow entry naming a protected path is not an error at `ResolveAllowlist`
time and does not authorize the path at `Authorize` time — it simply loses. That
is :393-394 verbatim, and D6's ordering is what delivers it.

### D3. `MC_HOME` is protected whole, and the enumeration is a redundant belt

**Resolves gap (f).**

ADR-017:376-380 enumerates fifteen root classes; ADR-017:345 says *"complete
`MC_HOME`"* is absent; `phase3-contract.md:178` requires *"broad `MC_HOME`"* be
absent from in-container probes. The three are consistent only if `MC_HOME` is
protected **as a whole tree**, with the enumeration as a belt.

Decision: **the whole `MC_HOME` tree is one protected root**, and the fifteen
enumerated classes are *additionally* registered. Rationale, on AGENTS.md §6's
three tests:

- **Preserves the invariants / fail-closed.** An enumeration silently drifts
  from the on-disk layout — the day someone adds `MC_HOME/quarantine/`, an
  enumerated-only rule permits it and no test fails. A whole-tree rule cannot
  drift. The two consequence statements (:345, contract:178) describe exactly a
  whole-tree rule.
- **Deviates least.** It adds no member ADR-017 does not already imply: every
  enumerated class is inside the tree, and bidirectionality (D4) already makes
  `MC_HOME` reject as an ancestor of `sessions/` — *but only if `sessions/`
  exists*, which is precisely the fragility this removes.
- **Reversible.** Deleting one root from the constructor.

The enumeration is kept anyway, because it is not redundant in the **descendant**
direction for D10's typed confinement: a typed source is checked against the
exact root its claimed kind authorizes, never merely "inside `MC_HOME`".

### D4. Bidirectional by identity; `broad_root` is HOME's directional weakening

**Pins ADR-017:388-389 and :390-393; resolves gap (a).**

For canonical source `S` and protected root `P`, reject when `S == P`, `S` is a
descendant of `P`, **or `S` is an ancestor of `P`**. Every comparison is
`os.SameFile` on resolved objects — never a string prefix (ADR-017:247-250, and
the rejected alternative at :1348-1349: *"Lexical prefix comparison fails under
symlinks, APFS case behavior, and sibling prefixes"*). `enclosesByIdentity`
(`identity.go:198-210`) already walks one direction; the ancestor direction is
the mirror walk over `P`'s ancestors.

**The ancestor direction is NOT unqualified, and an unqualified reading is
fatal.** ADR-017:370-374 makes `<workspace_root>/.mission-control` a protected
root — and a Worksource's own `workspace_root` is, necessarily, an **ancestor of
its own** `.mission-control`. An unqualified ancestor rule therefore rejects the
one mount the whole system exists to make: the own workspace, which ADR-017:302
*requires* to pass `Authorize` as an ordinary source. The first draft of this ADR
said "reject if `S` is an ancestor of `P`" with no qualification and would have
shipped exactly that.

The ancestor direction applies to:

- `denied_paths`, the whole `MC_HOME` tree and its classes, `MC_HOME/sessions`,
  every runtime-control dir, gateway secret and CA private-key roots, and the
  HOME-class roots — **yes**, reject on ancestry;
- **other** Worksources' roots (`mount.cross_worksource`) — **yes**: this is
  precisely ADR-017:388-389's stated purpose, *"so mounting a parent of another
  Worksource cannot expose a denied descendant"*;
- the **own** Worksource's own `.mission-control` / Git-control roots — **no**.
  ADR-017:371-373 already names what the own tree may reach: *"only the exact own
  task-local root, committed-tree materializations, trusted setup/landing, and
  Homie's type-matched inert nested covers"*. Own ancestry is governed by that
  clause, not by a blanket ancestor rejection.

Own/other is decided by **identity**, never by name (D7), so this qualification
cannot be spoofed by a path that merely looks like the own workspace. The
descendant direction is unqualified for every member: the own workspace being an
ancestor of `.mission-control` is legal; a mount *inside* `.mission-control` is
not, except through the :371-373 clause.

The ancestor direction is **load-bearing, not redundant with the blocked floor**.
Worked example, which is also a required test: `~/Library` is an ancestor of the
protected `~/Library/Keychains`. The floor does not match it (`library` is no
pattern, `blocked.go:28-69`); `broad_root` does not apply (`~/Library` is not an
ancestor of HOME); only bidirectionality rejects it. ADR-017:1172 names the class
(*"protected ancestors"*) and :1173 pairs it with *"own/other/parent
Worksource"* — mounting the **parent** of another Worksource is explicitly
required to reject.

**`broad_root`** (:390-393) is HOME's *directional weakening*, not an extra
rule: `S == HOME` or `S` a strict ancestor of HOME rejects, while descendants
stay eligible *"unless it hits another protected root"*. Every other root is
bidirectional; HOME alone is weak downward, because §5's Worksource model puts
workspaces under HOME.

The parenthetical `(`$HOME`, `/Users`, `/`)` is **illustrative of HOME's ancestor
chain on macOS, not a literal set**. Implementing it as a hardcoded list would be
wrong for any non-`/Users` HOME and would silently under-protect it. The rule is
computed from HOME's own resolved ancestor chain.

**`broad_root` reports `mount.denied_root`** (gap (a): the rule is named at :390
but the closed code list at :1146-1163 has no `mount.broad_root`). It is the only
fit, and :1173 files the case beside the other jurisdiction cases. The rejection
*message* says `broad_root` so the operator sees which rule fired; only the slug
is shared. Inventing `mount.denied_root`'s sibling would widen the closed list
this ADR has no mandate to widen.

### D5. HOME is injected, not ambient

**Resolves gap (e).**

`JurisdictionInput` carries the operator's real HOME as an **explicit,
already-resolved** value. It is never read from `$HOME` inside `mc/boundary`.

`$HOME` is caller-influenceable, and this package already refuses ambient
identity for exactly this reason: it takes an explicit `ownerUID int`
(`identity.go:55`, `:68`) rather than calling `os.Getuid()`. A boundary that can
be relocated by an environment variable is not a boundary.

**Injection relocates trust, so the injected value is validated, not trusted.**
`ResolveJurisdiction` refuses a HOME that is not an existing directory, is not
owned by `ownerUID`, is a symlink, or resolves to `/` or any filesystem root —
a HOME of `/` would make `broad_root` reject every source on the machine
(fail-closed but useless), and a symlinked or foreign-owned HOME would silently
relocate the entire `~/.ssh`-class member set. D1's zero-value law covers "nobody
constructed it"; this covers "someone constructed it wrong". Both are refusals
at construction, not at use. The synthetic
`tool_home_dir` (spec §5:114) is a *different* thing and must never be mistaken
for it — ADR-017:390 says *"the operator's real HOME"*, and :345 *"operator's
real HOME"*, precisely to distinguish them.

### D6. Where step 5 sits, and what that makes observable

**Resolves gap (c); documents gap (d).**

Order inside `Authorize`: (1) `ResolveSource` → steps 1-2; (2) `blocked.Rejects`
→ step 3; (3) `match` → step 4; (4) suffix grammar; **(5) `j.Rejects` → step 5**;
(6) `ResolveAccess` → Decision 4's table.

Step 5 goes **after step 4** (Decision 3's own numbering) and **before
`ResolveAccess`** (gap (c), unconstrained by ADR-017). Jurisdiction is
mode-independent — a protected path is protected for RO as much as for RW — so
resolving access first would let `mount.rw_not_permitted` mask
`mount.denied_root` and tell the operator to downgrade a mount that must never
exist at any access. Jurisdiction-first is the fail-closed choice.

**A consequence worth stating rather than discovering in a test** (gap (d)):
because step 4 precedes step 5, a protected path that is under **no** allow root
exits at `mount.not_allowlisted` and never reaches jurisdiction. So
`mount.denied_root` / `mount.cross_worksource` are observable only when the path
**is** allowlisted — which is exactly the case :393-394 exists to cover
(*"Allowlist membership never overrides jurisdiction"*) — or for a typed source
calling `Rejects` directly. **Test fixtures must allowlist the protected path to
see the code.** This makes :1173's *"real-HOME broad root vs an **allowed**
descendant"* pairing read as deliberate rather than incidental.

Similarly, the `~/.ssh`-class members (:383-386) are mostly shadowed by the
blocked floor at step 3, which returns `mount.source_blocked` first. The
protected set earns its keep on those members through the **ancestor** direction
(D4's `~/Library` case), which the floor cannot see. Both facts are stated so a
future reader does not "simplify" a rule whose value lives in a direction the
obvious test does not exercise.

### D7. `cross_worksource` vs `denied_root`: the split, and its precedence

**Resolves gap (b).**

- **`mount.cross_worksource`** — the ADR-017:369-370 member only: another
  Worksource's workspace / worktree / artifact / state / cache / tool-home root.
- **`mount.denied_root`** — every other member: profile `denied_paths`, Git
  control dirs, `.mission-control` roots, `MC_HOME` (whole, D3) and its
  enumerated classes, sessions, runtime-control dirs, gateway secret and CA
  private-key roots, the HOME-class roots, and `broad_root` (D4).

ADR-017 never states this split — Decision 5 puts other-Worksource roots *inside*
the one union and says the union rejects, which reads as one code. Three sources
support the split: Decision 3 step 7 lists *"protected/cross-Worksource roots"*
as two categories (:266); the code declares two constants; and spec §11.3:453
splits along the same seam — *"no resolved mount may fall under another
Worksource's roots or under a host credential directory"*.

**Precedence, which ADR-017 gives no rule for**: when a path is *both* (another
Worksource's workspace that is also a `denied_paths` entry),
**`mount.cross_worksource` wins**. It is the more specific statement of *why* the
path is denied, and it is the one an operator can act on — they can re-scope a
Worksource; "denied_root" tells them only that something, somewhere, said no.
Determinism matters more than the choice: the check order is fixed and pinned by
a test, so the same plan never reports two different codes on two runs.

**Own/other discrimination is identity-based, never name-based** (ADR-017:302-303
requires the *own* Worksource's roots to pass `Authorize` as ordinary sources, so
the predicate must distinguish own from other by resolved identity, not by
membership in `denied_paths`). `JurisdictionInput` therefore carries the own
Worksource's identity explicitly; a root matching it is not a cross-Worksource
rejection.

### D8. Existence: absent members are members, resolved through their nearest existing ancestor

**Resolves gap (i); implements ADR-017:275-276.**

ADR-017:275-276: *"A declared deny path that does not yet exist is compared
through its nearest existing canonical ancestor plus unresolved suffix;
ambiguity denies."* You cannot `os.SameFile` a path with no inode, so this is an
algorithm requirement, not a footnote.

- **`denied_paths` and any absent protected root**: resolve the nearest existing
  canonical ancestor, retain the unresolved suffix, and compare on that pair.
  **Ambiguity denies** — if it cannot be decided whether `S` intersects the
  would-be path, reject.

  **How the pair is compared, since "compare on that pair" is not an
  algorithm.** The two halves are compared differently, and conflating them
  would smuggle back the lexical comparison ADR-017:1348-1349 rejects:
  - the **ancestor half** is `os.SameFile` on resolved objects, exactly as
    everywhere else in D4;
  - the **suffix half** is compared **component-wise** against the
    corresponding components of `S`'s own unresolved remainder — never as a
    string prefix. `a/b` matches components `["a","b"]`, so `ab` and `a/bc` do
    not match, which a `strings.HasPrefix` would get wrong.
  - **Case**: APFS is case-insensitive-preserving by default, so a
    component-wise match that is case-sensitive would let `~/.SSH` slip a
    case-only variant past a `~/.ssh` deny. Suffix components are therefore
    matched case-insensitively, and — because the volume's behaviour is not
    guaranteed — a case-insensitive match on a case-sensitive volume is
    **ambiguity, and denies**. This is the one place the design cannot be exact,
    and it fails closed.
  - If the nearest existing ancestor is itself ambiguous (it does not exist
    either, or resolution races), **deny**.
- **The HOME-class roots** carry an explicit *"when present"* (:383), so an
  absent `~/.aws` is simply not a member — it cannot be intersected and it cannot
  be ambiguous.
- **Every other absent member** (an `MC_HOME/seals` that does not exist yet, an
  other-Worksource artifact root that does not) is treated as **present via the
  nearest-existing-ancestor rule**, not skipped. ADR-017 states this only for
  declared deny paths; extending it is the conservative reading, because the
  alternative — silently skipping — makes protection depend on directory
  creation order, which is precisely the fragility D3 rejects. Logged as a
  deviation, since it generalizes a rule ADR-017 scopes narrowly.

### D9. The verdict is never cached on source identity

**Implements ADR-017:264-269 and :1339-1341.**

Step 7 reruns the whole predicate before Docker create and again after
create/before start, and :1339-1341 is explicit: *"Permission/ACL,
blocked-policy, allow-root, and **protected-set** changes with unchanged source
bytes/inode also remove the unstarted container."*

So a **protected-set change alone, with the source inode unchanged, must
reject**. That forbids memoizing a jurisdiction verdict keyed on source identity
— the obvious optimization, and a wrong one. `Rejects` is a pure function of
`(identity, jurisdiction)` and is re-evaluated at every call site: profile save
(:237-238, spec:453), plan, pre-create, and post-create/pre-start.

Every rejection **aborts the whole plan** (:1165: *"Every code aborts the whole
plan; none drops one mount."*); nothing is downgraded or dropped
(contract:169).

### D10. Typed grants are confined per class — a second predicate, not a hole in the union

**This is the decision the first draft of this ADR twice deferred to and never
wrote.** It pointed at "D5's per-class check" from two places; D5 is about HOME.
The mechanism did not exist, and an implementer following the draft would have
shipped a boundary that rejects every typed grant.

ADR-017:396-401, in full:

> "That union governs ordinary/profile-requested mounts. A typed system mount is
> **instead** confined to its one kind-specific authorized root: the exact own
> session, derived own state, selected runtime-control directory, or generated
> projection **may be inside `MC_HOME`**, but any sibling/ancestor/other identity
> is still denied."

The load-bearing word is **instead**. This is not an exception carved out of the
union — it is a *different predicate*, selected by what the source is:

| source | predicate |
|---|---|
| ordinary / profile-requested (zero `TypedClaim`) | **the union** (D3, D4, D7): reject on any intersection, either direction |
| typed system (non-zero `TypedClaim`) | **kind-specific confinement**: the source must be `os.SameFile`-identical to the exact root its claimed kind authorizes. Any sibling, ancestor, descendant, or other identity is denied. |

So a typed source is **not** asked "do you intersect `MC_HOME`?" — it always
does, by design: ADR-017:663 grants `/mc/session` = `MC_HOME/sessions/<run-id>/`
RW to every owner, :681 grants `/home/agent` = `MC_HOME/state/worksources/
<scope-id>/home` RW, and :669/:666/:664/:684 grant others. It is asked "**are you
exactly the root your kind may occupy?**" — a stricter question, not a weaker
one. Nothing is exempted: the typed path is confined to a single identity, while
an ordinary path merely has to avoid a set.

This also disposes of the draft's claim that D3's whole-tree rule is what
endangers typed grants. It is not: a skeptic's control probe removed D3's
whole-tree root, registered only the enumerated classes, and `/mc/session` **still
rejected** — because ADR-017:375-376 makes *"all `MC_HOME/sessions`"* a union
member and D4 rejects descendants. The kindless seam was the defect; D3 only
widened its blast radius. Both are fixed here, and the order matters: **D10 must
exist for D3 to be safe.**

`Authorize` handles ordinary mounts and therefore always passes the zero
`TypedClaim`. The resident's typed-mount planner calls `Rejects` directly with a
populated claim. A typed source that claims a kind it does not own fails the
identity comparison against that kind's authorized root — the claim is not
trusted, it is *checked*.

### D11. `ResolveJurisdiction` re-runs; the verdict is never cached — and neither is the input

**Extends D9 to close the gap that made D9 a half-measure.**

D9 forbids caching the *verdict* keyed on source identity. That is necessary and
insufficient: re-running `Rejects` against a **stale `Jurisdiction`** reproduces
the stale answer exactly. ADR-017:1339-1341 is explicit that the protected set
itself is drift:

> "Permission/ACL, blocked-policy, allow-root, and **protected-set** changes with
> unchanged source bytes/inode also remove the unstarted container."

So `ResolveJurisdiction` itself — not just `Rejects` — re-runs at **every** call
site: profile save (ADR-017:237-238, spec:453), plan, immediately before Docker
create, and again after create/before start (ADR-017:264-269). A protected root
whose identity changed between plan and start is drift, and drift removes the
unstarted container. Neither the input nor the verdict may be memoized across
those points.

## Consequences

### What this buys

- The two unused codes become reachable, and the four acceptance classes at
  ADR-017:1169-1180 (*"protected ancestors"*, *"real-HOME broad root vs an
  allowed descendant"*, *"selected vs other runtime-control dirs"*, *"own/other/
  parent Worksource"*) become testable.
- `Authorize` may finally be wired into production planning — the ledger has
  gated that on this slice since `e01a2af`.
- Jurisdiction is reachable by typed system sources (D1), which ADR-017:349-351
  requires and which a field-on-allowlist design would have silently denied.
- `mc/boundary` stays pure and injectable, so profile save and both sides of
  `mc dispatch` share one policy — the contract's requirement (:41-44, :169).

### Sharp edges

1. **The observable-code surface is narrower than it looks** (D6). Most exact
   `~/.ssh`-class sources report `mount.source_blocked`, and any non-allowlisted
   protected path reports `mount.not_allowlisted`. A reader auditing "does
   `denied_root` fire?" against naive fixtures will conclude the rule is dead
   code. It is not — its value is in the ancestor direction and in the
   allowlisted case. Pinned by tests that construct both.
2. **D3's whole-tree `MC_HOME` protection is stricter than ADR-017's
   enumeration.** If some future typed grant needs a path inside `MC_HOME` that
   is not one of the fifteen enumerated classes, it must be added as an explicit
   typed kind with its own authorized root (D10's per-class predicate) rather
   than working by default. That is the intended direction of failure, but it
   will look like a regression to whoever hits it first, so: it is deliberate,
   and this line is the warning.
3. **D8 generalizes ADR-017:275-276 beyond declared deny paths.** A
   deviation, logged. The narrower reading (skip absent non-deny members) is
   available and is a one-line change, but it makes protection depend on
   directory creation order.
4. **`JurisdictionInput` is a wide value** — every other Worksource's six root
   classes, resolved. Building it is host-side work with real `os.Stat` cost at
   every spawn, and D9 forbids caching it away. Judgment: correctness over
   cost; the walk is bounded by :167-169's limits, and a spawn already pays for
   container creation. If it ever matters, the fix is caching keyed on the
   *jurisdiction's* own generation, never on source identity.
5. **`mount.gate_unhealthy` is missing from `identity.go`'s code block**
   (ADR-017:1162, added by `c6ca202`). Named here so it is not lost; it is an
   adjacent gap, **not** this slice's, and must not be swept in.

### Tests that pin it

Fast lane, red-first, `mc/boundary`:

- **Union membership**: every one of the fifteen `MC_HOME` classes, sessions,
  the `.mission-control` and Git-control roots, gateway/CA private-key roots,
  every runtime-control dir (selected *and* other), each HOME-class root when
  present and its absence when not, and profile `denied_paths` — each rejects
  when allowlisted (D6).
- **Non-subtractability** (D2): no constructor path yields a `Jurisdiction`
  missing a non-operator member; an `[[allow]]` entry naming a protected path
  loses; `denied_paths` adds and never removes; 512+1 deny paths rejects before
  any stat.
- **Bidirectionality** (D4): descendant, equal, and **ancestor** all reject;
  `~/Library` (ancestor of `~/Library/Keychains`) rejects though the floor does
  not match it; the **parent of another Worksource** rejects; symlink aliases of
  a protected root reject (identity, not string).
- **`broad_root`** (D4): `$HOME`, `/Users`, `/` reject; `~/src/project` stays
  eligible; a non-`/Users` HOME still rejects its own ancestors (proving the
  parenthetical is not a literal set).
- **Codes and precedence** (D7): cross-Worksource → `mount.cross_worksource`;
  everything else → `mount.denied_root`; a path that is both reports
  `cross_worksource`, deterministically; own roots do **not** trip it.
- **Ordering** (D6): jurisdiction beats `ResolveAccess` (an RW request on a
  protected path reports `denied_root`, not `rw_not_permitted`).
- **Typed sources** (D1, D10) — **the permit side first, because pinning only
  the deny side is exactly how the first draft's fatal defect stayed
  invisible**: every one of ADR-017's typed grants PLANS SUCCESSFULLY through
  `Rejects` with its own claim — `/mc/session` (:663), `/home/agent` (:681),
  `/mc/records/output` (:669), `/mc/workflow` (:675), the completion seal
  (:666), `/mc/attachments/in` (:664), `/workspace/operator/traces` (:660), the
  selected runtime-control dir (:684), and `/mc/gateway/ca.crt` (:677). A suite
  that cannot launch a container is not green, whatever it reports. THEN the
  deny side: a sibling, an ancestor, a descendant, and another kind's root all
  reject for the same claim; a typed source claiming a kind it does not own
  rejects; `Rejects` is callable with no allowlist at all.
- **Unenumerated `MC_HOME` child** (D3): `MC_HOME/quarantine` — a path in no
  enumerated class — rejects, which is the entire point of whole-tree
  protection and the one case the enumeration cannot catch.
- **Own vs other Worksource ancestry** (D4): the own `workspace_root` is an
  ancestor of its own `.mission-control` and **is still authorized**; the parent
  of ANOTHER Worksource rejects. An unqualified ancestor rule passes the second
  and fails the first — this pair is what distinguishes them.
- **The zero value** (D1): a bare `Jurisdiction{}` rejects every source,
  including one that intersects nothing.
- **Injected HOME validation** (D5): a HOME that is `/`, a symlink, foreign-owned,
  or absent is refused at construction, not at use.
- **Absence** (D8): a non-existent deny path resolves through its nearest
  existing ancestor; ambiguity denies.
- **No caching** (D9): the same source identity with a changed jurisdiction
  flips the verdict.
- **Planted mutants** (the `e01a2af` precedent): identity→prefix comparison;
  ancestor direction removed; `broad_root` as a literal `/Users` list;
  jurisdiction after `ResolveAccess`; `denied_paths` made subtractable. Each
  must die with an exercised witness.

### What gets harder

- **`Authorize` grows a fourth parameter**, touching every existing call site
  and test in `mc/boundary` — and the migration is **not** the zero-risk
  compile-time chore an earlier draft claimed. The compiler finds the call
  sites, but D1's zero-value law then turns every one of them that passes a
  bare `Jurisdiction{}` into a **runtime** failure: the tests compile and then
  reject everything. That is the law working as designed, and it means each
  existing test must be given a real constructed `Jurisdiction` (most want an
  empty-but-resolved one over a temp dir), not a zero value. Budget for it as a
  real edit to roughly a dozen tests, not a rename.
- **The host side owes `JurisdictionInput`** — resolving other Worksources'
  roots and Git control identities is real work this ADR does not do; it defines
  the seam and the slice after it fills it. Until then `Authorize` still must
  not be wired into production planning.
- **Reversal cost is low**: the whole slice is one value, one method, one
  parameter, and one call in `Authorize`. Dropping it restores today's
  behaviour exactly.
