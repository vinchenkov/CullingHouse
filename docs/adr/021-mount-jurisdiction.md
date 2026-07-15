# ADR-021 — Mount jurisdiction: the protected set and cross-Worksource law

## Status

**Accepted. Reworked TWICE by adversarial review** (2026-07-14): once as a
Proposed draft, then again as an Accepted document by a pre-TDD implementation
probe. TDD next, in the order at the end of this document.

**The second review (`docs/reviews/2026-07-14-adr-021-implementation-probe.json`)
raised 6, confirmed 5 (all major) and 1 partial.** Unlike the first, it found
the ADR *salvageable* — every defect is local to one decision and the predicate
stays parametric — but **not TDD-able as written**. Each finding went to an
independent skeptic in its own git worktree, told to refute by implementing the
design verbatim against the real `mc/boundary` code and running it. What it
changed:

- **D10/D1 — the claim was self-certifying.** `TypedClaim{Kind, Root}` let the
  *caller* supply both the source and the root it is checked against, so
  `Rejects` computed `os.SameFile(x, x)` and `claim.Kind` was **provably inert**
  (a skeptic drove all ten kinds through one fixed pair: every one permitted).
  D10's own sentence — *"the claim is not trusted, it is checked"* — was false.
  The kind→root binding now lives in the host-resolved `Jurisdiction`.
- **D4 — `broad_root` had a measured fail-open, and it leaked a real key.**
  `/Users` is an APFS **firmlink**. HOME's `filepath.Dir` chain is
  `(HOME, /Users, /)`, but HOME lives on `/System/Volumes/Data`, which is in no
  chain and `os.SameFile` with nothing in it. The blocked floor is blind to it
  (0 of 3). The skeptic's witness: `/System/Volumes/Data` **AUTHORIZED rw**,
  reading 419 bytes of the operator's real OpenSSH private key. `broad_root` is
  now computed from HOME's chain **union its alias routes**, via `statfs` (D4);
  D5 supplies and validates the HOME it anchors on.
- **D10 — the nine-item permit list was not the closed set.** ADR-017:380-381
  governs (*"except each exact typed own-source grant in Decisions 4, 6, and
  8"*); :396-401's four items are **illustrative**. Roughly thirteen grants had
  no kind and would have rejected — the Verifier unable to write its correction,
  no run writing output, Codex never launching — with a green suite. This is the
  *same misreading of :396-401* that killed the first draft, surviving one
  rework because the fix was applied to the mechanism and not to the list.
- **D5 — unimplementable against D1's own types.** `ResolveJurisdiction` had no
  `ownerUID` parameter to check ownership against, and a pre-resolved
  `ProtectedID` destroys the symlink leg's only evidence. D5's *legs* were
  correct and do accept the real 0750 ACL-carrying HOME — but D5's lone code
  citation aimed the implementer at `TrustHomeDir`, whose mode leg rejects every
  real macOS HOME.
- **D3/D1 — the fifteen-class belt is inert** and is deleted (0 of 10 verdicts
  changed with it removed). Its stated reason for existing was false: D10 reads
  `TypedRoots`, never the belt.

One finding is worth recording for how it went: F6's skeptic **predicted** a
fail-open in the absent-member handling, wrote the test to prove it, and the
test **failed** — refuting its own finding with running code. That is the
decorrelated producer/judge principle working in the direction that is easy to
fake and hard to do.

**The park list is empty.** The unifying insight, which collapses every alleged
blocker: `mc/boundary` is **pure and receives every root pre-resolved**. D10's
predicate is `source SameFile one-of j.TypedRoots[claim.Kind]` — parametric in
the enum, deriving no host path ever. So a missing host-path *formula* belongs
to the host planner's later slice and to fixture realism; it is not a TDD
blocker and does not park this slice.

Resolves the nine gaps ADR-017 leaves in its own Decision 5 + Decision 3 step 5,
enumerated in `docs/reviews/2026-07-14-adr-017-jurisdiction-extract.md` §8. It
**adds nothing to ADR-017's policy** — every rule below is ADR-017's, and this
ADR only decides the things ADR-017's text left undecided. ADR-017 remains the
authority; where this ADR and ADR-017 disagree, ADR-017 wins, and where ADR-017
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
// TypedClaim names WHICH KIND a typed system source claims to be. It does NOT
// carry the root it is checked against — that binding lives in the Jurisdiction
// (D10), because a claim that supplies its own root certifies itself.
// The ZERO value means "no claim" — an ordinary/profile-requested mount, which
// is what D10's union governs.
type TypedClaim struct {
    Kind TypedKind // see D10a's derivation rule; domain is ADR-017:634-702's host-bind rows
}

type ProtectedID struct {
    Canonical string      // the resolved path, OR the declared cleaned path if absent (D8)
    Info      fs.FileInfo // the stat object os.SameFile requires; NIL when the member is absent
    IsDir     bool
}

// Present reports whether this root exists on disk. An absent member is still a
// member (D8): only the ancestor direction can fire for it.
func (p ProtectedID) Present() bool { return p.Info != nil }

type JurisdictionInput struct {
    // RAW, resolved by ResolveJurisdiction — never by the caller.
    //
    // DeniedPaths: operator-authored and may not exist yet, so no caller can
    // pre-resolve them (D8 resolves through the nearest existing ancestor).
    // Bounded at 512 (ADR-017:167-168), checked before any stat.
    DeniedPaths []string
    // Home: raw because its RAW SPELLING IS THE EVIDENCE. D5 must see the
    // symlink, and EvalSymlinks destroys exactly that. Injected and
    // pre-resolved are ORTHOGONAL: the caller still supplies the value, so
    // mc/boundary never reads $HOME — but the package does not trust the
    // caller's resolution of it.
    Home string

    // MC-derived and spine-derived, PRE-RESOLVED by the host caller. Any of
    // these EXCEPT HomeClassRoots may be ABSENT: encode an absent member as
    // ProtectedID{Canonical: <declared cleaned path>, Info: nil}. Absence is
    // the HAPPY PATH, not an edge case: MC_HOME/seals and most runtime-control
    // dirs do not exist at boundary-check time, and another Worksource's
    // artifact root routinely does not either (D8).
    //
    // HomeClassRoots is the ONE carve-out: ADR-017:383 says "when present", so
    // an absent ~/.aws is simply not a member and is omitted, never encoded
    // with a nil Info (D8).
    MCHome          ProtectedID   // the whole tree (D3)
    HomeClassRoots  []ProtectedID // ~/.ssh etc., only those present (D8)
    GatewaySecrets  []ProtectedID
    RuntimeControls []ProtectedID // EVERY runtime control dir, not just non-selected
    OwnWorksource   WorksourceRoots   // identity-based own/other discrimination (D7)
    OtherWorksources []WorksourceRoots // workspace/worktree/artifact/state/cache/tool-home
    GitControls     []ProtectedID // pre-resolved: this package owns no Git resolver
    MissionControlRoots []ProtectedID // <workspace_root>/.mission-control

    // The kind -> authorized-root binding (D10). THIS is what makes a typed
    // claim checkable rather than self-certifying. A kind absent from this map
    // has no authorized root and denies.
    TypedRoots map[TypedKind][]ProtectedID

    resolved bool // set only by ResolveJurisdiction; see the zero-value law
}

// ownerUID is an explicit parameter, not a field: it matches TrustPolicyFile /
// TrustHomeDir, the compiler finds all four D11 call sites, and it cannot be
// silently zero.
func ResolveJurisdiction(in JurisdictionInput, ownerUID int) (Jurisdiction, error)

func (r ResolvedAllowlist) Authorize(source string, requested Access,
                                     blocked BlockPolicy, j Jurisdiction) (Authorization, error)

// Rejects is callable WITHOUT an allow-root match — typed system sources need
// exactly this. A zero TypedClaim means an ordinary mount (D10's union); a
// non-zero claim selects D10's kind-specific predicate, whose authorized roots
// are read from j.TypedRoots — never from the caller.
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
non-existent `denied_paths` — the two could not both be true). Two members are
raw, for **two different reasons**, and conflating them is how the second review
found D5 unimplementable:

- `DeniedPaths` is raw because ADR-017:275-276 explicitly contemplates a deny
  path *that does not yet exist*, which no caller can pre-resolve.
- `Home` is raw because **its raw spelling is the evidence**. D5 must refuse a
  symlinked HOME; `EvalSymlinks` destroys the only proof that it was one. You
  cannot validate a value someone else already resolved — and the threat D5
  itself names then lands verbatim: a `$HOME` pointed at an attacker-controlled
  directory leaves the real `~/.ssh` unprotected and protects the attacker's
  instead.

Everything else is pre-resolved — and **may be absent**, which is the happy path
(D3, D8). An absent member is encoded `{Canonical: <declared cleaned path>,
Info: nil}` and `ResolveJurisdiction` must not error on it. **`HomeClassRoots` is
the one exception**: ADR-017:383 scopes it *"when present"*, so an absent
`~/.aws` is simply **not a member** and is omitted, never encoded with a nil
`Info`. The two rules disagree on exactly one case — a source that is an
*ancestor* of an absent HOME-class root — and ADR-017 settles it: that source is
not rejected on `~/.aws`'s account, because `~/.aws` is not a member at all.

**The law that makes a nil `Info` safe** (verified by a skeptic who predicted the
opposite and was refuted by his own test): `Rejects`'s **ancestor** branch walks
`P.Canonical` upward and stats the *ancestors* — it **must never read `P.Info`**,
because that is the only direction that can fire for an absent `P`. The equality
and descendant branches do read `P.Info`, and their correct answer for an absent
`P` is already `false`: `os.SameFile(nil, x)` returns false without panicking,
and `ResolveSource` returns `mount.source_missing` for anything under an absent
root, so no descendant can exist to test.

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
members (`denied_paths`) and the MC-derived ones (**HOME itself** — D4's
`broad_root` anchor, validated per D5 — plus the MC_HOME tree (whole, D3), the HOME-class
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

### D3. `MC_HOME` is protected whole, and the enumeration is inert — not registered

**Resolves gap (f).**

ADR-017:376-380 enumerates fifteen root classes; ADR-017:345 says *"complete
`MC_HOME`"* is absent; `phase3-contract.md:178` requires *"broad `MC_HOME`"* be
absent from in-container probes. The three are consistent only if `MC_HOME` is
protected **as a whole tree**.

Decision: **the whole `MC_HOME` tree is one protected root**, and the fifteen
enumerated classes are **not** additionally registered — the whole-tree root
subsumes every one of them, and `MC_HOME/sessions` (:374-375) with them.
Rationale, on AGENTS.md §6's three tests:

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

**The enumeration is NOT registered as a belt, and the first draft's reason for
keeping one was false.** That draft said the belt "is not redundant in the
descendant direction for D10's typed confinement". D10 reads
`TypedRoots[claim.Kind]` and never consults an enumeration. A skeptic measured
it: registering the fifteen classes alongside the whole-tree root changes **0 of
10 verdicts**, including every unenumerated path. `JurisdictionInput` therefore
carries no `MCHomeClasses`.

**The stronger reason is that the fifteen are not transcribable at all**:
ADR-017:376-380's fifteen words are **class** names, not directory names. Four
have no `MC_HOME` directory whatsoever — `config` is `config.toml` + `routing.md`
(spec:707, :718); `landing` is ADR-017:699-702's no-agent effect class;
`runtime-auth` is spec:816's `mc onboard` section name; `control` is
ADR-017:382's runtime control dirs, already a separate union member. The rest are
spelled inconsistently, so no mechanical pluralization is right in either
direction: `cache` is singular (spec:763) while `backups` is plural (spec:777,
`onboard.go:282`); `context` is singular (ADR-017:673) while `corrections` and
`revisions` are plural (:671-672). And **0 of the 15 exist as `MC_HOME` children
after the real `scaffoldOnboardHome` runs** — only `attachments`, `backups`,
`outputs`, `sessions`, `workflows` are scaffolded at all.

This is D3's own drift argument landing on its own repository, and it is
**evidence for the whole-tree rule**: `MC_HOME/runs/<run_id>.json`
(`phase1b-contract.md:221`), `MC_HOME/egress-audit/<run_id>.jsonl`
(ADR-018:378-379), and `MC_HOME/deployment.uuid` are already outside all fifteen
classes today. D3's hypothetical `MC_HOME/quarantine/` was never hypothetical.
The whole-tree root catches all of them; an enumeration catches none.

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

- `denied_paths`, the whole `MC_HOME` tree (which subsumes its classes and
  `MC_HOME/sessions`, D3),
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
wrong for any non-`/Users` HOME and would silently under-protect it.

**But "HOME's own resolved ancestor chain" — what the first rework of this ADR
said — is itself wrong on macOS, and the second review measured the leak.** The
rule is computed from HOME's resolved ancestor chain **union the alias routes to
HOME**:

> For each ancestor `A` of HOME (HOME included), `statfs(A).f_mntonname` is the
> mount point of the volume hosting `A`. That mount point and its own ancestor
> chain are ancestors of HOME **by reachability**, even though no `filepath.Dir`
> walk from HOME ever reaches them.

`/Users` is an APFS **firmlink**, not a symlink: `lstat` reports a plain
directory, `ModeSymlink` is clear, and `filepath.EvalSymlinks` neither sees
through it nor normalizes it. So HOME has a second, longer, fully live ancestor
chain that `filepath.Dir` never produces. `os.SameFile` catches the aliases *of*
HOME and *of* `/Users` (same inode, measured: both `ino=16962` / `ino=266855`) —
but `/System/Volumes/Data`, `/System/Volumes`, and `/System` are `SameFile` with
**nothing** in the `Dir` chain, and each exposes the whole of HOME. The compiled
blocked floor is blind to all three.

The measured witness, and the reason this is stated at length rather than left
to a test: with `/` allowlisted, `/System/Volumes/Data` was **AUTHORIZED rw**,
and the resulting container path read **419 bytes of the operator's real OpenSSH
private key**. On this machine `statfs("/Users").f_mntonname ==
"/System/Volumes/Data"`, which adds exactly the three roots that leak.

Use **`statfs`** — not `/usr/share/firmlinks` parsing, and not a hardcoded
`/System/Volumes/Data`. It is the kernel's own mount table; it needs no
macOS-specific literal; it is correct for a HOME on an external volume or a
relocated non-`/Users` HOME; and it degrades to a no-op on Linux (the mount point
is already a member of the path's own chain), so the rule stays **one rule on
both platforms**. This is D4's own "identity, never strings" idiom applied to the
one place a `Dir` walk cannot reach.

**The fix is not `broad_root`-only.** A skeptic proved every ancestor-direction
member has the identical blind spot: one `/System/Volumes/Data` source slipped
past `MC_HOME`, `~/.ssh`, and another Worksource's root simultaneously. So the
ancestor direction is factored through **one** helper —

    ancestorRoutes(P) = P's Dir chain ∪ each chain member's statfs mount-point chain

— consumed by **both** `broad_root` and the "S is an ancestor of P" mirror walk.
The descendant and equality directions need no change: an alias of a chain member
is caught free by `os.SameFile` inode equality. The gap is exactly the volume
mount point and its ancestors, which are aliases of nothing in the chain.

**Fail closed:** a `statfs` error on any ancestor is ambiguity, and D8's
"ambiguity denies" governs it — `ResolveJurisdiction` returns an error rather
than silently producing a short `broad_root` set.

**`broad_root` reports `mount.denied_root`** (gap (a): the rule is named at :390
but the closed code list at :1146-1163 has no `mount.broad_root`). It is the only
fit, and :1173 files the case beside the other jurisdiction cases. The rejection
*message* says `broad_root` so the operator sees which rule fired; only the slug
is shared. Inventing `mount.denied_root`'s sibling would widen the closed list
this ADR has no mandate to widen.

### D5. HOME is injected, not ambient

**Resolves gap (e).**

`JurisdictionInput` carries the operator's real HOME as an **explicit, RAW**
value (`Home string`). It is never read from `$HOME` inside `mc/boundary`.

`$HOME` is caller-influenceable, and this package already refuses ambient
identity for exactly this reason: it takes an explicit `ownerUID int`
(`identity.go:55` — cited for the **signature** precedent only, never for the
validation body; see the warning below) rather than calling `os.Getuid()`. A
boundary that can be relocated by an environment variable is not a boundary.

**Injection relocates trust, so the injected value is validated, not trusted —
and raw, because the raw spelling is the evidence.** The first rework typed
`Home` as a pre-resolved `ProtectedID`, which made its own symlink leg
unfireable: a canonical path's final component is by construction never a
symlink. D1's zero-value law covers "nobody constructed it"; this covers
"someone constructed it wrong".

`validateHome(path string, ownerUID int) error` — **five legs, in this order**,
all reporting `mount.denied_root`:

1. **`os.Lstat`** — error ⇒ refuse (absent). **Never `os.Stat`**: `Stat` follows
   the link and destroys leg 2's evidence.
2. **`info.Mode()&os.ModeSymlink != 0`** ⇒ refuse. *Refuse, not resolve-through*
   — matching `trustedLstat`'s seam (`identity.go:81-90`: *"a symlink to an
   otherwise-trusted object is not itself trusted"*). A symlinked HOME silently
   relocates the entire `~/.ssh`-class member set: the real `~/.ssh` goes
   unprotected and the attacker's is protected instead.
3. **`!info.IsDir()`** ⇒ refuse.
4. **Filesystem root** ⇒ refuse: `filepath.Dir(clean) == clean` **or**
   `os.SameFile(info, parentInfo)` **or** `parentStat.Dev != stat.Dev`. Placed
   *before* ownership so `/` refuses for the right reason. The three tests are
   complementary — the `Dir`-self test alone misses real roots like
   `/System/Volumes/VM`. A HOME of `/` would make `broad_root` reject every
   source on the machine: fail-closed, but useless.
5. **`int(stat.Uid) != ownerUID`** ⇒ refuse; a failed `Sys()` assertion ⇒ refuse.

Then `EvalSymlinks` for the canonical identity `broad_root` walks (D4).

**NO mode leg. NO ACL leg. This is NOT `TrustHomeDir`** — and that sentence is
in the ADR because the first rework's only code citation pointed straight at it:

> `validateHome` **must not** route through `TrustHomeDir` (`identity.go:68`) or
> `trustedOwnerMode` (`identity.go:92-104`). That function is **`MC_HOME`'s**
> seam: MC creates `MC_HOME` itself at 0700, so it enforces `perm&0o077 == 0`.
> The operator's **real HOME is 0750 on stock macOS** (measured here:
> `drwxr-x---+ /Users/vinchenkov`) and 0755 elsewhere. `0750 & 0o077 = 0o050` ⇒
> `TrustHomeDir` **rejects the real HOME and nothing can ever plan**. HOME's mode
> is not MC's business, which is why D5 has five legs and no mode leg. D5 also
> **refuses the pending macOS ACL obligation** (`identity.go:52-54`): the real
> HOME carries an ACL today (`group:everyone deny delete`), and a managed or
> network HOME may carry allow ACEs. Routing HOME through the `Trust*` seam
> would silently inherit that check the day the obligation lands. Reuse the
> `lstat` seam only, and report `mount.denied_root`, never
> `mount.allowlist_untrusted`.

The failure shape this warning exists to prevent is the one this project keeps
re-deriving: a 0700 `t.TempDir()` HOME fixture makes every test green while
`ResolveJurisdiction` rejects every real HOME on macOS. **Green suite, dead
product.** The test list below therefore *forbids* a 0700-only HOME fixture.

The synthetic `tool_home_dir` (spec §5:114) is a *different* thing and must never
be mistaken for it — ADR-017:390 says *"the operator's real HOME"*, and :345
*"operator's real HOME"*, precisely to distinguish them.

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
  control dirs, `.mission-control` roots, `MC_HOME` (whole, D3, which subsumes its
  enumerated classes and sessions), runtime-control dirs, gateway secret and CA
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
`(identity, claim, jurisdiction)` and is re-evaluated at every call site: profile save
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
| typed system (non-zero `TypedClaim`) | **kind-specific confinement**: `claim.Kind` is looked up **in the Jurisdiction**, and the source must be `os.SameFile`-identical to one of the roots *that kind* authorizes there. A kind with no authorized root **denies**. Any sibling, ancestor, descendant, or other identity is denied. |

**The lookup is in `j.TypedRoots`, never in the claim — and that mechanism is the
whole decision.** The first rework wrote `TypedClaim{Kind, Root}`, letting the
caller supply both the source *and* the root it is checked against. `Rejects`
could then only compute `os.SameFile(source, claim.Root)`, which catches a
planner passing a mismatched pair and can **never** catch one passing a
consistent-but-wrong pair. A skeptic measured the consequence: with a fixed
source/root pair, **all ten kinds permitted** — `claim.Kind` was compared to
nothing, and the natural planner shape degenerates to `os.SameFile(x, x)`. The
`Jurisdiction` contributed nothing but its `resolved` flag, which also made D9
and D11 vacuous on the typed path.

So D10's own sentence — *"the claim is not trusted, it is **checked**"* — was
false as written. It is true now, and the mechanism is named rather than
asserted, exactly as D1's zero-value law was made to name its own (an ADR that
states a property without its mechanism is how the `applySpawn` seam shipped).

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
member and D4 rejects descendants. (That probe registered the classes
explicitly; **this ADR does not** — D3's whole tree subsumes them and
`MC_HOME/sessions` with them. The probe's point survives the change: sessions
rejects a typed grant's source either way, which is why the kind is what rescues
it.) The kindless seam was the defect; D3 only widened its blast radius. Both are
fixed here, and the order matters: **D10 must exist for D3 to be safe.**

`Authorize` handles ordinary mounts and therefore always passes the zero
`TypedClaim`. The resident's typed-mount planner calls `Rejects` directly with a
populated claim. A typed source that claims a kind it does not own fails the
identity comparison against **the Jurisdiction's** roots for that kind — the
claim is not trusted, it is *checked*.

#### D10a. `TypedKind`'s domain is DERIVED, not enumerated here

**The normative rule**, because a hand-copied list has now drifted twice:

> `TypedKind`'s domain is exactly the **host-bind rows of ADR-017 Decision 6's
> destination table (:634-702)**, including the setup/landing tables at
> :686-702. ADR-017:380-381 sweeps them in — *"except each exact typed
> own-source grant in Decisions 4, 6, **and 8**"* — and :373 names *"trusted
> setup/landing"*. **:396-401's four items ("own session, derived own state,
> selected runtime-control directory, or generated projection") are
> ILLUSTRATIVE, not the domain.** Misreading them as the domain is what killed
> the `c120c5c` draft; a nine-item list copied from them is what the second
> review caught here, one rework later, in the same document.

Three row classes are **not** binds and carry no kind: image-rootfs rows
(`/mc/private` :665 — *"never a bind"*, `/mc/private/attachments` :667,
`/workspace`'s rootfs arm :636); the named volume (`/mc/spine` :679, whose
boundary is setuid `mc` ownership); and **allowlisted** rows, which are ordinary
sources taking the union predicate with a zero claim (`/workspace/artifacts/…`
:651, `/workspace/references/…` :652 — both say *"allowlisted"*). Reaching
`Rejects` with a non-bind means the planner is confused: it **denies**, loudly,
rather than silently permitting.

Resident-materialized rows (`/mc/run.json` :661, `/mc/sandbox.json` :662,
`/mc/network/policy.json` :676, `/mc/gateway/ca.crt` :677, `/etc/resolv.conf`
:678) **are** binds of resident-written host files and **do** get kinds —
ADR-017:354-362's closed source-kind list names *"envelope, sandbox,
resolver/network projection, CA certificate, workflow capture, and current
correction output"* explicitly.

Granularity: **one kind per destination row, merged only where ADR-017 merges
them** (`.codex`/`.claude` are two arms of one *"selected runtime-control dir"*;
the four package caches are one row). Setup/landing never share a kind with the
agent table — ADR-017:686-687 says so verbatim (*"their own complete mount
tables; they never inherit the agent table"*), and the collision is not
theoretical: setup `/repo/source` is **RO** (:691) while landing `/repo/source`
is **RW** (:699), *"intentionally including its primary checkout"*. A
destination-keyed table that merged them would silently fuse the least- and
most-privileged grants in the system.

**No kind is blocked on a missing formula.** Every root marked *host-side* below
arrives pre-resolved in `TypedRoots`; `mc/boundary` compares identities and
**never constructs a path**. This sentence is normative: it is what makes the
absent formulas (the correction base, the record-input base, the runtime-control
selection, the runner tree, ADR-018's projections) the host planner's later
slice rather than this slice's blocker.

```go
// TypedKind is DERIVED from ADR-017 Decision 6's destination table (:634-702).
// The domain is closed. A kind absent from Jurisdiction.TypedRoots has no
// authorized root and denies (D10, fail-closed).
type TypedKind uint8

const (
	KindNone TypedKind = iota // zero value: NOT a claim. Selects D10's union predicate.

	// Task-local skeleton (:636-650; host formulas :713-714)
	KindTaskRoot   // :636  <workspace_root>/.mission-control/tasks/task-<task-id>
	KindTaskSource // :637  <task-root>/source
	KindTaskGit    // :640  <task-root>/git
	KindSealedPack // :645  <task-git>/objects/pack      [see Sharp Edge 6]
	KindInertCover // :638,:641-644,:646-650,:654,:656-658,:692,:700  [see Sharp Edge 6]

	// Projections and spine-registered roots (:637,:653,:655,:659,:660,:697)
	KindCommittedProjection // :637,:653   host-side
	KindExecutionProjection // :637        host-side
	KindRegisteredRoot      // :637,:653   spine-registered, host-side
	KindOperatorWorksource  // :655        spine-registered, host-side
	KindOperatorArtifact    // :659        spine-registered, host-side
	KindTraceProjection     // :660        ADR-017 Decision 7's root, host-side

	// MC_HOME typed own-source grants (:663-684)
	KindOwnSession       // :663  MC_HOME/sessions/<run-or-session-id>/   NOTE: run-OR-SESSION
	KindOwnState         // :681  MC_HOME/state/worksources/<scope-id>/home
	KindPackageCache     // :682  the four exact derived package-cache roots (spec:763)
	KindRuntimeControl   // :683,:684  selected canonical control dir, host-side
	KindRunOutput        // :669  MC_HOME/outputs/<run-id>/
	KindCompletionSeal   // :666  MC_HOME/seals/<run-id>/
	KindAttachmentIn     // :664  MC_HOME/attachments/<session-id>/in/published
	KindAttachmentOut    // :668  MC_HOME/attachments/<session-id>/out
	KindWorkflowCapture  // :675  MC_HOME/workflows/<run-id>-plan.js       NOTE: a FILE
	KindCorrectionOutput // :674  exact NEXT corrections/mc-<task-id>-corrections<n>, RW

	// Prior record inputs (:670-673) — base is host-side
	KindRecordInputOutput     // :670  one exact completed output/evidence file or dir
	KindRecordInputCorrection // :671  brief-referenced corrections<n>, RO
	KindRecordInputRevision   // :672  revisions/<task-id>-OP-REVISION.md
	KindRecordInputContext    // :673  context/<task-id>-STEER.md

	// Resident-materialized non-secret projections (:661,:662,:676,:677,:678)
	KindEnvelope           // :661
	KindSandbox            // :662
	KindNetworkProjection  // :676  ADR-018
	KindGatewayCA          // :677  ADR-018 — the CERTIFICATE, never the private-key root
	KindResolverProjection // :678  ADR-018

	KindRunnerSource // :680  release runner tree, host-side install path

	// Setup/landing effect classes (:691-702). Separate by ADR-017:686-687.
	KindSetupWorksource   // :691  RO
	KindSetupTaskRoot     // :693
	KindSetupTaskSource   // :694
	KindSetupTaskGit      // :695
	KindSetupSeal         // :696
	KindSetupProjection   // :697
	KindSetupEnvelope     // :698
	KindLandingWorksource // :699  RW — the ONLY grant that gets a real Worksource repo RW
	KindLandingTaskRoot   // :701
	KindLandingEnvelope   // :702

	kindMax
)
```

`KindRecordInputCorrection` and `KindCorrectionOutput` share a formula *string*
and **must stay two kinds**: collapsing them on the shared-formula rule would
authorize a Verifier to overwrite a prior correction. The formula is the same;
the resolved root is not.

**The guard test, which is the structural fix.** Go cannot make this a build-time
check, so it is a required test, and it must run in **both** directions: a static
`adr017Rows` table (destination + line cite + kind) fails if any row maps to
`KindNone`, **and** fails if any kind in `[KindNone+1, kindMax)` appears in no
row. One-directional coverage is exactly how a nine-item list survived a full
adversarial review.

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

**`TypedRoots` is part of the drift surface too.** Once the kind→root binding
lives in the `Jurisdiction` (D10) rather than in the caller's claim, a typed
root whose identity changed between plan and start is drift on exactly the same
terms, and removes the unstarted container the same way. Under the first
rework's self-certifying claim this was unreachable — the typed path never
consulted the `Jurisdiction` at all, so D9 and D11 were vacuous for every typed
source. That is a second reason the seam had to move.

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
2. **Every grant inside `MC_HOME` needs an explicit typed kind with its own
   authorized root in `TypedRoots` — enumerated class or not.** Membership in
   ADR-017's fifteen classes buys nothing: D3 protects the tree whole. The first
   rework's version of this edge warned about *future* grants outside the
   fifteen; the live failure was the **opposite** and the second review found it
   — `correction`, `revision`, `context`, and `cache` are *named* at :377-379 and
   still had no kinds. It is the intended direction of failure, but it will look
   like a regression to whoever hits it first, so: it is deliberate, and this
   line is the warning.
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

6. **OUT OF CHARTER, stated rather than smuggled.** This ADR's Status claims it
   *"adds nothing to ADR-017's policy."* One item breaks that claim:
   **`KindInertCover` and `KindSealedPack` are not in ADR-017:354-362's
   source-kind list, and that list is declared CLOSED** (*"Allowed source kinds
   are closed: …"*). The generated inert covers (:638-650, :654, :656-658, :692,
   :700) and the setup-generated sealed pack directory (:645) are real host binds
   with real host sources, and ADR-017 names no kind for either. **Adding a kind
   to a list ADR-017 declares closed is an ADR-017 amendment, not an ADR-021
   resolution.**

   Handling, per AGENTS.md §6 (log-and-go — defining them is *stricter* than
   omitting them, so no invariant breaks): this ADR defines both provisionally,
   the deviation is logged in `IMPLEMENTATION-NOTES.md`, and a separate item
   opens for an ADR-017 amendment asking whether the closed source-kind list is
   *meant* to exclude generated inert covers or is simply incomplete. It **does
   not block this slice**: the predicate is parametric in the enum, so the two
   kinds can be added, renamed, or removed without touching `Rejects`.

7. **`/System`, `/System/Volumes`, and `/System/Volumes/Data` now reject as HOME
   ancestors** (D4's alias routes). This is correct — each genuinely exposes the
   whole of HOME — but it will read as "system paths, nothing to do with HOME"
   to whoever hits it first.

8. **`boundary.Jurisdiction` collides in name with the unrelated free-text
   `Jurisdiction` Worksource column** (`mc/verbs/worksource.go:12`, and
   `main.go`'s `-jurisdiction` flag). Different packages, no compile conflict, a
   real reader trap. One doc comment on the type.

### Tests that pin it

Fast lane, red-first, `mc/boundary`:

- **Union membership**: `MC_HOME` (whole, D3), sessions, the `.mission-control`
  and Git-control roots, gateway/CA private-key roots, every runtime-control dir
  (selected *and* other), each HOME-class root when present and its absence when
  not, and profile `denied_paths` — each rejects when allowlisted (D6). **Not**
  the fifteen classes: they are not registered (D3), and a test that pinned them
  would pin an inert belt.
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
  parenthetical is not a literal set). **Plus the alias routes, guarded by
  `runtime.GOOS == "darwin"`**: each of `/System/Volumes/Data`,
  `/System/Volumes`, and `/System` rejects with `mount.denied_root` — the three
  the `Dir`-walk chain misses and the blocked floor does not see. **And the
  permit side in the same fixture**: an allowlisted descendant of HOME still
  authorizes after the expansion. A deny-only test here passes trivially and
  proves nothing; the expansion must not swallow the mount the system exists to
  make.
- **Codes and precedence** (D7): cross-Worksource → `mount.cross_worksource`;
  everything else → `mount.denied_root`; a path that is both reports
  `cross_worksource`, deterministically; own roots do **not** trip it.
- **Ordering** (D6): jurisdiction beats `ResolveAccess` (an RW request on a
  protected path reports `denied_root`, not `rw_not_permitted`).
- **The enum derivation guard** (D10a), **both directions**: the `adr017Rows`
  table fails if any host-bind row of :634-702 maps to `KindNone`, AND fails if
  any kind in `[KindNone+1, kindMax)` appears in no row. This test is the thing
  that stops the next hand-copied list.
- **Typed sources** (D1, D10) — **the permit side first, because pinning only
  the deny side is exactly how the first draft's fatal defect stayed
  invisible**: **every host-bind row of :634-702** PLANS SUCCESSFULLY through
  `Rejects` with its own claim and a populated `TypedRoots` — not the nine the
  first rework listed, which is how ~13 grants nearly shipped unplannable. A
  suite that cannot launch a container is not green, whatever it reports. THEN
  the deny side:
  - **the kind-inertness sweep** — one fixed correct source, every *other* kind
    denies. This is the test that dies on the self-certifying claim: under it,
    all ten kinds permitted.
  - **jurisdiction-dependence** — the same source and the same claim flip
    PERMIT→DENY when `TypedRoots` changes. Under a caller-supplied root the
    `Jurisdiction` was inert and this could not move.
  - **the sibling scope-id** (ADR-017:399's own case) — the own scope's
    `/home/agent` permits, a sibling scope's home denies, with the planner
    deriving both from one expression.
  - a sibling, an ancestor, a descendant, and another kind's root all reject for
    the same claim; a kind with **no** authorized root denies; a non-bind row
    denies; `Rejects` is callable with no allowlist at all.
- **Unenumerated `MC_HOME` child** (D3): `MC_HOME/quarantine` — a path in no
  enumerated class — rejects, which is the entire point of whole-tree
  protection and the one case the enumeration cannot catch.
- **Own vs other Worksource ancestry** (D4): the own `workspace_root` is an
  ancestor of its own `.mission-control` and **is still authorized**; the parent
  of ANOTHER Worksource rejects. An unqualified ancestor rule passes the second
  and fails the first — this pair is what distinguishes them.
- **The zero value** (D1): a bare `Jurisdiction{}` rejects every source,
  including one that intersects nothing.
- **Injected HOME validation** (D5) — **permit side first, and a 0700
  `t.TempDir()` HOME is FORBIDDEN for this test**: the **real
  `os.UserHomeDir()`, at its real 0750 ACL-carrying mode, is ACCEPTED**; 0755
  and 0700 are accepted too. This is the test that kills the `TrustHomeDir`
  trap, and a 0700-only fixture makes the suite structurally incapable of
  catching it (every other HOME in the package is a 0700 temp dir, and
  `identity_test.go:118` already pins 0750 as a *reject* for the MC_HOME seam —
  the exact confusion). THEN **all five** deny legs, one test each: an absent
  path (leg 1), a symlink (leg 2), a regular file (leg 3), `/` (leg 4, plus a
  volume root that the `Dir`-self test alone misses), and **a HOME owned by
  another uid** (leg 5). Then the end-to-end
  that would have caught the pre-resolution defect: **a `$HOME` symlinked at a
  *same-uid-owned* directory is refused and the `~/.ssh`-class roots do not
  relocate** — same-uid, or the ownership leg masks the symlink leg and the test
  proves nothing.
- **Absence** (D8): a non-existent deny path resolves through its nearest
  existing ancestor; ambiguity denies. **And an absent OTHER-Worksource artifact
  root still rejects its parent** — the current list pins only `DeniedPaths`,
  the one member already typed raw, so nothing pins D8's generalization to the
  pre-resolved members, which is the half D1's type actually strains.
- **No caching** (D9, D11): the same source identity with a changed jurisdiction
  flips the verdict — **including a changed `TypedRoots` on the typed path**.
- **Planted mutants** (the `e01a2af` precedent), each dying with an exercised
  witness: identity→prefix comparison; ancestor direction removed; `broad_root`
  as a literal `/Users` list; **`broad_root` computed from `filepath.Dir` alone**
  (witness: `/System/Volumes/Data`); jurisdiction after `ResolveAccess`;
  `denied_paths` made subtractable; **`claim.Kind` ignored / source compared
  against a caller-supplied root**; **HOME validation given a mode leg**; **HOME
  validation routed through `TrustHomeDir`**. The last two die only against the
  real-HOME permit test.

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

## The TDD order

Authoritative; it replaces the five-step order the ledger carried before the
second review, which was written against the defective D1/D5/D10.

1. **`TypedKind` + the derivation guard** (D10a). Pure data, no dependencies.
   RED first, **both directions**: a kindless `:634-702` row fails, and an
   orphaned kind fails.
2. **`ProtectedID.Present()` + the absent-member law; `ResolveJurisdiction(in,
   ownerUID)`** + the zero-value law (unexported `resolved`; a bare
   `Jurisdiction{}` rejects everything) + D2 non-subtractability + the
   512-`DeniedPaths` bound checked **before any stat**.
3. **D5 `validateHome` — PERMIT side first**: the real `os.UserHomeDir()` at
   0750, ACL-carrying, is ACCEPTED. Then the five deny legs (leg 4 at
   `ownerUID = 0`), then the same-uid symlinked-HOME end-to-end. Before D4,
   because `broad_root` needs a validated HOME.
4. **D4 `ancestorRoutes`** — permit side first (an allowlisted HOME descendant
   stays eligible), then `$HOME`/`/Users`/`/` and the three firmlink routes,
   then the helper shared with the union's ancestor mirror.
5. **D10 typed confinement — PERMIT side first**: every host-bind row of
   :634-702 plans with a populated `TypedRoots`. Then the kind-inertness sweep,
   the sibling scope-id, the jurisdiction-dependence flip, and kind-with-no-root.
6. **D4 union bidirectionality + D7 codes/precedence + D6 ordering**, including
   the `Authorize` fourth-parameter migration (13 call sites, all in
   `identity_test.go`; each needs a real constructed empty-but-resolved
   `Jurisdiction`, never a zero value — D1's law turns a bare `Jurisdiction{}`
   into a *runtime* rejection, so the compiler will not find these for you).
7. **D8 absence/suffix** (component-wise, case, ambiguity-denies) **+ D9/D11
   drift**, including `TypedRoots` drift.
8. **The planted mutants**, each dying with an exercised witness.

Order note: (3) before (4) because `broad_root` consumes a validated HOME; (1)
before everything because the enum is the parameter every later predicate is
written against.
