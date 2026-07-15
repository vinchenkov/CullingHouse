# ADR-017 jurisdiction slice — requirements extract (2026-07-14)

Read-only extract produced before implementing the protected-set /
cross-Worksource slice (ADR-017 Decision 5 + Decision 3 step 5). Committed
because it is the input to the design decisions that follow, and because the
ledger's `NEXT` line — written from memory — **understates the slice**.

Sources: `docs/adr/017-*.md` (current post-`c6ca202` text),
`specs/mission-control-spec.md`, `docs/phase3-contract.md`,
`mc/boundary/{allowlist,identity,blocked}.go`.

**Scope note.** `c6ca202` amended Decisions 4/5/6/8's ownership/gate text and
added `mount.gate_unhealthy`. It did **not** touch Decision 5's protected-set
paragraphs (ADR-017:364-406) or Decision 3 step 5 (ADR-017:258-259). This slice
owns 364-406, 258-259, 264-269.

## 1. The protected union — every member (ADR-017:366, "the union of")

- **Profile `denied_paths`** (:367) — from the *selected effective* profile.
  `sandbox_profiles.denied_paths`, JSON array (`schema.sql:41`). Bounded at
  **512** (:167-168). Operator-authored, so the only member that may not exist
  yet → see the non-existent-deny-path rule (§7.5).
- **Every *other* Worksource's roots** (:368-369) — *"workspace/worktree/
  artifact/state/cache/tool-home roots"*. **Six root classes.** From spine:
  `sandbox_profiles.workspace_root/.artifact_roots/.tool_home_dir` joined via
  `worksources.sandbox_profile`; state/cache are MC-derived
  (`MC_HOME/state/worksources/<scope-id>/home`, `MC_HOME/cache/<worksource>` —
  spec:763); worktrees are `<workspace_root>/.mission-control/tasks/task-<id>/source`.
- **Registered real Git control dirs AND `<workspace_root>/.mission-control`
  task/projection roots** (:370-373) — **two members in one bullet**. Permitted
  carve-outs: *"only the exact own task-local root, committed-tree
  materializations, trusted setup/landing, and Homie's type-matched inert
  nested covers"*. Identification is by spine **registration** + resolution,
  never a `.git` basename guess.
- **All `MC_HOME/sessions`** (:374-375). Own session RW at `/mc/session` is a
  typed grant (:663), not a carve-out here.
- **The `MC_HOME` record/control roots** (:376-380) — **fifteen classes, not
  six**: attachment, output, **workflow, correction, revision, context,
  projection, seal, landing, state, cache**, config, control, backup,
  runtime-auth — *"including the allowlist"*, except each exact typed own-source
  grant in Decisions 4/6/8 (a carve-out available only to typed system mounts).
- **Every runtime control dir, gateway secret root, CA private-key root**
  (:382). **Every** runtime control dir, not just non-selected. The CA
  *certificate* is by contrast an RO typed mount (`/mc/gateway/ca.crt`, :677) —
  do not conflate with the private-key root.
- **The `~/.ssh`-class home roots** (:383-386), *"when present"*, **anchored at
  the operator's real HOME** (unlike the blocked floor, which matches the same
  names as any component anywhere): `~/.ssh`, `~/.aws`, `~/.azure`,
  `~/.config`, `~/.docker`, `~/.gnupg`, `~/.kube`, `~/.codex`, `~/.claude`,
  `~/Library/Keychains`, `~/.netrc`, `~/.npmrc`, `~/.pypirc`,
  `~/.git-credentials`.

Decision 4's absence list (:344-347) is a *consequence* statement about the
mount table, not a second union — but see gap (f).

## 2. "Non-subtractable" (:366, defined by consequence)

- :393-394 **"Allowlist membership never overrides jurisdiction."**
- :400-401 the typed-system confinement is *"a closed exception, not a way for
  an allowlist entry to override `MC_HOME` protection."*
- :396-397 *"That union governs ordinary/profile-requested mounts."*

**No operator-reachable input may remove a member.** No profile field, no
`config.toml` key, no allowlist entry, no negation form. The blocked floor
already models the shape: `shippedBlockedPatterns` is a package-private array
compiled into `Rejects`, not a `BlockPolicy` field, so *"even a zero-value
policy cannot omit it"* (`blocked.go:72-76`). The protected set must follow it:
non-operator members unremovable by construction, `denied_paths` purely
additive.

## 3. Bidirectional intersection (:388-389, :258-259)

> "A source intersecting a protected root in either direction rejects, so
> mounting a parent of another Worksource cannot expose a denied descendant."
> "5. Reject when the source equals/is under a protected root **or is an
> ancestor that would expose it**."

Reject if `S == P`, `S` under `P`, **or `S` an ancestor of `P`**. Both
directions by **filesystem identity**, never string prefixes (:247-250;
rejected alternative :1348-1349). `enclosesByIdentity` (`identity.go:198-210`)
already does one direction; the ancestor direction is the mirror walk.

**The ancestor direction is load-bearing, not redundant with the floor.**
Worked example: `~/Library` is an ancestor of protected `~/Library/Keychains`.
The floor does not match it (`library` is no pattern). `broad_root` does not
apply. Only the bidirectional rule rejects it. Acceptance names the class
(:1173 *"protected ancestors"*, :1174 *"own/other/parent Worksource"*).

## 4. The directional `broad_root` rule (:390-393)

> "The operator's real HOME has an additional directional `broad_root` rule: a
> source may not equal or be an ancestor of HOME (`$HOME`, `/Users`, `/`),
> while an allowlisted descendant such as `~/src/project` remains eligible
> unless it hits another protected root."

- Rejects: `S == HOME`; `S` a strict ancestor of HOME. The parenthetical is
  **illustrative of HOME's ancestor chain on macOS, not a hardcoded literal
  list** — a literal set would be wrong for a non-`/Users` HOME.
- Eligible: descendants (`~/src/project`), still subject to the full union,
  the bidirectional rule, and the floor.
- **"Directional"** = for HOME alone the descendant direction is not a
  rejection. Every other root is bidirectional. HOME is deliberately weaker
  downward because §5's Worksource model puts workspaces under HOME.

## 5. `cross_worksource` — trigger, and knowledge at Authorize time

Trigger (by inference — gap (b)): the :368-369 member. Spec §11.3:453 states
the same predicate and splits along the same seam: *"no resolved mount may fall
under another Worksource's roots or under a host credential directory"*.

Reinforcing registry invariants (:403-405): workspace roots pairwise
non-overlapping; each artifact root has one owner; duplicate/overlapping
sources **within one plan** reject — that third one is `mount.source_alias`,
not this predicate.

**How the roots are known: they are not.** Nothing in `mc/boundary` can see
them today. They must be **injected**, pre-resolved to `(device,inode,type)`.
`docs/phase3-contract.md:41-44` constrains it: the lock-domain side *"reads
only canonical spine Worksource/Profile/Homie state"*, and both sides must
agree on one plan *"produced by the same pure policy package"* — so
`mc/boundary` stays pure and must not query the spine or the filesystem
registry. Contract:41 also warns *"this contract does not pretend the warm
helper can see unmounted host Worksource paths"* — canonicalization of other
Worksources' roots is **host-side**.

The two closed cross-Worksource RO exceptions (Strategist(propose) seeding,
Homie operator scope — :586-609) are typed system mounts, outside this
ordinary-mount predicate but still subject to it per :349-353.

## 6. Existing code shape, and where step 5 sits

```go
func ResolveAllowlist(allowlist MountAllowlist) (ResolvedAllowlist, error)          // identity.go:173
func (r ResolvedAllowlist) Authorize(source string, requested Access,
                                     blocked BlockPolicy) (Authorization, error)    // identity.go:231
func ResolveSource(source string) (SourceIdentity, error)                           // identity.go:121
func enclosesByIdentity(ancestor resolvedRoot, canonical string) bool               // identity.go:198
func (r ResolvedAllowlist) match(canonical string) []rootMatch                      // identity.go:284
```

Rejection convention (identity.go:14-45): closed `Code*` slug block mirroring
:1146-1163; one `MountError{Code, Msg}` + `mountErrf`; tests assert via
`errors.As` (`identity_test.go:12-22`). Already declared and **unused**:
`CodeDeniedRoot = "mount.denied_root"` (:24), `CodeCrossWorksource =
"mount.cross_worksource"` (:25).

`Authorize` today: (1) `ResolveSource` → steps 1-2; (2) `blocked.Rejects` →
step 3; (3) `match` → step 4 (allow-root identity ancestry); (4) suffix
grammar; (5) `ResolveAccess` → Decision 4 table. Decision 3 numbers **step 5
after step 4**. The code already reserves the seat (identity.go:228-230):

> `// Protected-root and cross-Worksource jurisdiction (step 5) are the`
> `// following slice and are NOT applied here.`

Consequence of ordering: the floor (step 3) fires first on `~/.ssh`-class
members, returning `mount.source_blocked`, so `mount.denied_root` is nearly
unreachable for *exact* `~/.ssh`-class sources — the protected set earns its
keep on those via the **ancestor** direction (§3's `~/Library` case).

## 7. Requirements the ledger's NEXT line missed

1. `<workspace_root>/.mission-control` task/projection roots are a member in
   their own right (:370-371), with four permitted shapes (:371-373).
2. **The MC_HOME list is fifteen classes, not six** (:376-380), *"including
   the allowlist"*.
3. *"Every runtime control dir"* (:382), not just non-selected. The selected
   one is *"the sole kind-specific exception to generic blocked-name matching;
   that exception cannot be requested as an ordinary profile mount"*
   (:351-353). Acceptance: *"selected vs other runtime-control dirs"* (:1174).
4. **The typed-system closed exception (:396-401) is part of this slice's
   design surface.** :349-351: *"Every typed system source bypasses the
   external allowlist requirement only. It still passes its kind-specific
   source-type, non-symlink, identity, containment, and cross-Worksource checks
   at plan and spawn."* → **the jurisdiction predicate must be callable
   independently of an allow-root match**, because typed sources never reach
   `match()`. Do not bury it in `Authorize`'s tail.
5. **The non-existent-deny-path rule** (:275-276): *"A declared deny path that
   does not yet exist is compared through its nearest existing canonical
   ancestor plus unresolved suffix; ambiguity denies."* A real algorithm
   requirement — you cannot `os.SameFile` a path with no inode.
6. **Step 7's recheck obligation** (:264-269, :1339-1341): rerun the whole
   predicate before Docker create and again after create/before start;
   *"Permission/ACL, blocked-policy, allow-root, and protected-set changes with
   unchanged source bytes/inode also remove the unstarted container."* → **a
   protected-set change alone, source inode unchanged, must reject.** Forbids
   caching a verdict keyed on source identity.
7. **Profile save is a call site, not just spawn** (:237-238; spec:453;
   contract:169).
8. **Bounds reject before the walk** (:167-169): the 512-`denied_paths` bound
   must be checked before any `os.Stat`; *"none of these collections is
   truncated"*.
9. **Every code aborts the whole plan** (:1165): *"none drops one mount."*
10. Decision 4's cross-check (:302-303): the *own* Worksource's roots go
    through `Authorize` as ordinary sources → own/other discrimination must be
    **identity-based**, not "is it in denied_paths".
11. `mount.gate_unhealthy` (:1162, added by `c6ca202`) is **missing** from
    `identity.go`'s code block. **Adjacent gap, NOT this slice** — flagged so
    it is neither "fixed" here nor mistaken for a jurisdiction concern.
12. Acceptance rows to turn green (:1169-1180): *"protected ancestors"*,
    *"real-HOME broad root vs an allowed descendant"*, *"selected vs other
    runtime-control dirs"*, *"own/other/parent Worksource"*.

## 8. Ambiguities an implementer must resolve (flagged, not papered over)

- **(a) `broad_root` has a name but no code.** :390 names the rule; the closed
  list :1146-1163 has no `mount.broad_root`. It must borrow one.
  `mount.denied_root` is the only fit and :1174 groups the case with the other
  jurisdiction cases — but the ADR never says so.
- **(b) The `denied_root`/`cross_worksource` split is never stated.** Decision
  5 puts other-Worksource roots *inside* the union and says the union rejects
  (one code); but step 7 lists *"protected/cross-Worksource roots"* as two
  (:266), the code declares two constants, and spec:453 splits along the same
  seam. Most defensible: `cross_worksource` for :368-369, `denied_root` for
  everything else incl. `broad_root`. **Also unresolved:** which code wins when
  a path is *both* (another Worksource's workspace that is also a
  `denied_paths` entry)? The ADR gives no precedence.
- **(c) Step 5's position vs Decision 4's access table is unspecified.** Order
  changes the observed code for an RW request on a protected path.
  Jurisdiction-first is the fail-closed choice.
- **(d) A non-allowlisted protected path returns `mount.not_allowlisted`, not
  `mount.denied_root`** — step 4 precedes step 5. So `denied_root`/`broad_root`
  are only observable when the path **is** allowlisted (exactly what :393's
  *"Allowlist membership never overrides jurisdiction"* is written to cover) or
  for typed sources that skip step 4. **Test fixtures must allowlist the
  protected path to see the code**, which makes :1174's *"real-HOME broad root
  vs an allowed descendant"* pairing read as deliberate.
- **(e) How is "the operator's real HOME" obtained?** :390/:345 say *"real
  HOME"* and contrast it with the synthetic `tool_home_dir` (spec:114), but
  never say whether from `$HOME`, `os.UserHomeDir()`, or the passwd DB for the
  trusted operator uid. `mc/boundary` already takes an explicit `ownerUID int`
  (`identity.go:55`) rather than trusting ambient identity; env `HOME` is
  caller-influenceable.
- **(f) Is `MC_HOME` protected in whole, or only its enumerated roots?**
  Decision 5 enumerates fifteen classes; Decision 4:345 says *"complete
  `MC_HOME`"* is absent, and contract:178 requires *"broad `MC_HOME`"* be
  absent from in-container probes. An enumeration can drift from the on-disk
  layout in a way a whole-tree rule cannot. The bidirectional rule makes
  `MC_HOME` reject anyway as an ancestor of `sessions/` — **but only if
  `sessions/` exists.** Whole-`MC_HOME` is the fail-closed reading.
- **(g) "Registered real Git control directory" needs a resolver this package
  does not have** (:370, :446-450, :631-632). Filesystem+Git work, not pure
  policy → must arrive **pre-resolved**, which means defining a jurisdiction
  input type carrying canonicalized identities. The ADR never draws that seam.
- **(h) `ResolveAllowlist`'s signature has no seat for jurisdiction.** Either a
  `ResolveJurisdiction(...) (Jurisdiction, error)` + a fourth `Authorize`
  parameter (mirrors `blocked BlockPolicy`, keeps `Authorize` pure in its args,
  and — critically — lets typed sources call the predicate standalone per §7.4),
  or a field on `ResolvedAllowlist` (couples jurisdiction to an allowlist typed
  sources bypass — probably wrong). The `BlockPolicy` precedent is the closest
  existing model and satisfies §2.
- **(i) Existence-conditionality is stated only for the HOME-class roots**
  (*"when present"*, :383). Whether an absent `MC_HOME/seals` or an absent
  other-Worksource artifact root is silently skipped or is itself an error is
  unstated; :275-276's *"ambiguity denies"* argues for deny but is written
  about declared deny paths specifically.
