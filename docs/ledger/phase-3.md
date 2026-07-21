# Ledger — chronology, Phase 3

Append-only. Never a startup read (AGENTS.md §5); grep it for a rationale.

Phase 3 entries written before 2026-07-15 live in `chronology-phase-0-2.md`
despite its name — the 2026-07-15 compaction moved the whole of PROGRESS.md's
history into that one file, Phase 3 included. Grep both. This file opens the
per-phase convention §5 describes and carries Phase 3 from here.

---

- 2026-07-15 — **ADR-016 D4 refusal taxonomy green; the repo moved off macOS's
  TCC-protected triad after fan-out silently revoked the session's own
  filesystem access** (`315e932`).

  The session resumed clean: fast lane green on all five legs, `ec362d6` docs-
  only, previous session Claude, so no §2 takeover review. Mapped the four
  surfaces the refusal transaction must join with a read-only fan-out, then
  wrote the D4 classifier red-first.

  **The mapping corrected the incoming `NEXT:` on two load-bearing points**, and
  both were measured rather than argued. First: `NEXT:` said D2's fences "exist
  and are enforced, so the transaction can use them rather than re-deriving
  idempotency." True at the substrate layer and false at every layer above it —
  `grep -rn "MC-DISPATCH"` over mc/ returns **zero hits**, `sha256`/`Sum256`
  returns **zero files**, and no Go code writes any of the five columns; all six
  `INSERT INTO activity` and all three `INSERT INTO outbox` sites leave them
  NULL. `48eaf63` landed D2's storage backstop only. The real `dispatch_key`
  derivation depends on a preparation token from a prepare step that does not
  exist, so this slice takes the key as an input. Second: `NEXT:` said to derive
  the refusal input "at the commit seam" — there is no commit seam.
  `verbs.Dispatch` is still the Phase-2 single-transaction `Decide()`. The next
  slice creates it rather than extending it.

  The taxonomy itself: `mc/refusal` holds D4's consequence table whole and
  imports `mc/boundary` for ADR-017's mount.* constants rather than restating
  them, so the two ADRs keep one vocabulary and boundary/domain/dispatch stay
  leaves. **Authority is modelled as a mount-namespace concept** because that is
  the only place D4 gives it meaning: it decides the class for the fourteen
  candidate-ownable mount codes, is deliberately *irrelevant* to the two
  allowlist trust codes, and is a category error anywhere else. The carve-out is
  fail-closed on purpose — a mislabeled frame must never make the deployment's
  own allowlist file a task's fault. Unknown codes and incoherent
  (code, authority) pairs refuse rather than default: an underivable class
  applies no consequence, where a guessed one could block a task for the
  deployment's mistake.

  **`Detail` has no free-text member at all.** D4 forbids the stored detail
  carrying supplied paths, env values, credentials, or nonces; rather than scrub
  free text, the type leaves nowhere to put any — `Field`/`Summary` are
  enumerated identifiers, so leakage is impossible by construction instead of
  one regex away. `Refusal.Message` carries the raw producer text for host-side
  diagnosis and `DetailFor` drops it; that drop **is** the sanitizer. The field
  and summary enumerations are authored here (D4 mandates "enumerated
  identifiers" without enumerating them); `item_index` is bounded at 255,
  derived from D2's largest plan collection (256 mounts / 256 env entries).

  The anti-drift guard went into `boundary/codes_test.go` rather than
  `mc/refusal`: that test already asserts "the mount.* set is exactly these
  sixteen" and warned in prose that a new code has no ADR-016 consequence. It
  now enforces that against the AST-declared constants, so inventing a
  seventeenth fails where the mistake is made, not one package downstream at run
  time. The test-only `boundary_test → refusal` edge is deliberate and keeps the
  production leaf property.

  Four planted mutants, all dead, zero survivors, each killed by its own test:
  carve-out dropped, candidate refusal downgraded to health, unknown code
  defaulting to stale, field validation defeated. One test bug was caught by the
  suite itself — a "hostile" fixture value that was in fact a legitimate enum
  member.

  **The environment finding, which cost most of the session.** ~40 minutes in,
  immediately after a 4-subagent fan-out, every read under the repo began
  returning `Operation not permitted`; git died with `Unable to read current
  working directory`. Two wrong diagnoses were published before the right one:
  "creating a new directory re-prompted TCC" (false — TCC grants are
  per-resource-class, not per-path) and "the sandbox is not involved because it
  fails with the sandbox disabled" (not a valid test — a child cannot drop an
  inherited Seatbelt profile). The decisive evidence was the failure boundary
  (`~/Documents`, `~/Desktop`, `~/Downloads` denied; `~/.claude`, `/etc`, `/tmp`
  fine — exactly macOS's TCC triad) and then the unified log:

      tccd: proc_pidpath_audittoken() failed from PID[8587]: (#3) No such process
      tccd: Failed to create attribution chain from message.
      sandboxd: kTCCErrorDomain Code=2 "missing 'auth_value' in reply message"

  PID 8587 was the live `claude` process. `sandboxd` asks `tccd` to authorize a
  protected-folder access; under fan-out the attribution chain cannot be built,
  `tccd` replies with no `auth_value`, and sandboxd **fails closed**. The policy
  table is never consulted, so no prompt appears and the denial sticks for the
  process's life. This is **anthropics/claude-code#59065** (open since
  2026-05-14), which names agent fan-out and `/loop` as triggers, reproduces
  *with* Full Disk Access granted, and recommends exactly one fix: move the repo
  out of the triad. Our case is a variant worth noting — #59065 says foreground
  terminal sessions are safe, but this was foreground with an intact
  `zsh → claude → login → Terminal.app` ancestry, and it broke anyway after
  background fan-out. Related: #64685, #60211 (auto-update mints a new TCC
  identity per version, breaking grants ~daily), #76615.

  The repo now lives at `~/dev/ai/homie`; `PROGRESS.md`'s header records the
  hazard, and its Parked Codex `[projects."…"]` path was corrected — a live
  directive that the move had silently falsified. Historical references to the
  old path in `spikes/`, `docs/reviews/`, and the chronology are left alone:
  they record what was true then. Nothing uncommitted was lost — the red test
  file survived at 12,941 bytes byte-for-byte, verified after the copy. The
  poisoned tree could not even `cp` itself out; only a fresh terminal could.

NEXT: Apply the ADR-016 D4 consequence router at the dispatch seam, red-first —
the impure half of the invalid-plan/no-claim transaction, whose input
(`refusal.Refusal`/`Classify`, 315e932) now exists and is proven. Route
ClassHealth to one health action, ClassCandidate by subject (block the subject
task with the code / subjectless pipeline → health / Homie → end with
`confinement:<code>`), and ClassStale to no mutation at all. Prove every arm
leaves zero new Run rows, a free lock, no spawn effect, and no fall-through to
another candidate. Note the three measured corrections in PROGRESS.md's NEXT:
there is no commit seam yet, D2's fences have no Go writer (take `dispatch_key`
as an input), and D3's launch columns do not exist so the Homie arm cannot be
launch-fenced yet. Do not load launchd.

## 2026-07-15 — ADR-016 D4's consequence router: the invalid-plan/no-claim transaction closes

The outgoing `NEXT:` was right about the shape and right about all three of its
measured corrections. It was wrong about one thing, and finding that out cost
nothing because the recon was fanned out and discarded.

**What landed** (8aa679e). `verbs.applyRefusal` — the impure half. `mc/refusal`
(315e932) already decided *which* class a `(code, authority)` pair carries; this
applies the class. Stale → nothing at all. Health → one `dispatch.health`
activity carrying the closed canonical detail. Candidate → subject task blocked
with `confinement:<code>`, subjectless pipeline → health, Homie → ended in the
same transaction with `confinement:<code>`. 20 tests, 109 subtests, and D4's
four-part invariant asserted on every arm.

The fall-through half of that invariant had no obvious fixture, so the fixture
*is* the proof: every case seeds a second, perfectly dispatchable task as bait.
If an arm ever fell through to ordinary selection, that task gets claimed and
`rrAssertInert` catches it as a Run row and a held lease. Cheap, and it fails
loudly for the right reason.

**The one thing the outgoing NEXT had wrong.** It listed
`homie.preflight_health` as needed for this slice. Read at ADR-016 line 433, it
is D3's, not D4's: a starvation-avoidance marker whose `candidate_key` hashes
the *current launch/resume debt* — the very columns the same NEXT correctly
noted do not exist. It is unbuildable here and, more to the point, pointless
here: its whole job is ordering a Homie candidate against a pipeline candidate,
and `mc/dispatch` selects no Homie candidates at all (`grep -rn homie
mc/dispatch` → zero). Corrected in place rather than parked; the ledger was the
thing that was wrong.

**Where the ADR and the contract disagreed, and who won.** D4's table says the
task arm blocks "with code"; phase3-contract §58 calls it a "stable sanitized
confinement reason". The code settled it: `blocked_reason` is free text with no
grammar CHECK, and the substrate's auto-unblock trigger fires only on the
`blocked child #%` prefix — so a `confinement:`-prefixed reason carries the
code, is sanitized by construction, and cannot silently auto-clear. One grammar
serves both arms.

Scope ran the other way once. Spec §15.5 lists `health` among the outbox kinds
"written by `mc` inside the same transaction that produced the event", which
reads like this slice owes an outbox row. It does not: D4's own mechanism text
appends an *activity*, and no health or `blocked_alert` outbox writer exists
anywhere in the tree — every existing block path records state without pushing
an alert. Making D4 alone fan out would have been the deviation. Logged for
whoever builds the alert-class resolver, which `console.go` already flags as a
Phase-5 placeholder.

**The mutant sweep earned its keep.** Ten planted, nine dead — and the two
survivors were the interesting part.

M10 (delete the router's own `dispatch_key` validation) survived, and the reason
was a hole in the *test*, not the code: the test only exercised a health code,
and the health arm writes an activity row, so the substrate's own CHECK was
failing the insert. The test was asserting someone else's guarantee. Only the
health arm writes a row — on the stale and block arms nothing but this router
would ever catch a malformed key. Widened to every class; M10 died. This is the
second time in this phase that a guard looked proven while something underneath
it was doing the work.

M6 (ignore `Classify`'s error) survives and always will: `DetailFor` calls
`Classify` and returns its error *verbatim* (`refusal.go:370-372`), so the
mutant is indistinguishable by construction — an equivalent mutant, not a hole.
The guard stays as deliberate fail-closed redundancy, in the same spirit as the
schema's dual-length NUL fence. Recorded here so the next sweep does not
re-litigate it.

**A real bug found, not fixed, on purpose.** The fast suite went red once on
`TestOnboardConcurrentFreshHomeNeverDeletesTheWinner` — nowhere near this slice.
It is a genuine latent race at `onboard.go:446`: a spine that `exists && bytes >
0` with no meta identity is treated as corruption, but that is also exactly the
transient state of a spine another caller is provisioning (SQLite writes its
first 4096-byte page before the schema transaction commits). Seven of eight
callers came back ok/done; one hard-failed with `restore from backup (§16.4)` —
a terrifying and wrong message for an operator who merely ran two onboards.

The first instinct — "my tests add CPU load and widen the window" — was wrong,
and measuring said so: 1 failure in 21 working-tree runs, 0 in 21 at HEAD, but a
clean worktree at HEAD ran the full suite 15/15 and the test alone 60/60 green.
Go runs a package's tests sequentially in file order and `onboard_test.go` sorts
*before* `refusalroute_test.go`, so the new tests cannot influence it at all.
The rate gap is chance; the race is real by inspection either way. Logged, not
fixed: it is fail-closed, breaks no invariant, and belongs to onboarding, not to
D4 (§6 log-and-go, §3 do not invent scope). The diagnosis and the fix direction
are in IMPLEMENTATION-NOTES.md so it is a short job for whoever takes it.

Deviations: five entries in IMPLEMENTATION-NOTES.md (556968c) — the vacuous
launch fence, `dispatch_key` as an input rather than a forged one, the health
action's shape, `homie.preflight_health`'s true owner, and the onboard race.

`PROGRESS.md` was 228 lines by the end of this entry's work and is back to 205:
Phases 0–2 are complete, and their per-item detail had long stopped being state.
It is compacted to what is still live (S3's consumable refresh token and its
recovery copy, the deferred spike legs, S6's NOTE(S6.n) citations); the rest is
here, in `spikes/*/RESULT.md`, and in the phase contracts.

NEXT: Land ADR-016 D3's launch-fencing columns as the v2→v3 migration,
red-first — eleven columns on `homie_sessions` with the pairing rules as CHECKs,
not as Go politeness. It is the smallest slice that unblocks the most: the D4
Homie arm's vacuous fence, and `homie.preflight_health`. Copy the dual-length
NUL-fenced hex CHECK shape from the D2 fences at `schema.sql:742-757` and follow
`migrationV1ToV2` (`substrate.go:111-120`). Still true, do not re-derive: there
is no prepare/attest/commit seam, nothing produces a `refusal.Refusal`, and so
`applyRefusal` has no caller but its tests — making it reachable is D1/D5's
slice, not this one. Do not load launchd. Full text in PROGRESS.md.

## 2026-07-15 — Parked reconciled a second time: four items left, none blocking

The operator asked what the Parked items were actually *for*, and whether they
blocked anything. Answering it honestly shrank the list from six to four and
turned the largest remaining item from a research task into two clicks.

**Nothing parked blocks Phase 3.** The `NEXT:` slice (D3's migration) is Go and
schema; it touches none of them. Two items blocked nothing at any point and are
now closed by operator decision; two are structural and can never be agent-done;
two are operator-hardware legs.

**Docker (handoff §4.1 row 4) is now measured, not researched.** Server 29.4.0,
ECI `false`, ResourceSaver `false`, AutoStart `true`, VirtioFS backend on, no
userns remap, VM 14 CPU / 8092 MiB / 1024 MiB swap / 122880 MiB disk. Every row-4
requirement passes **except the pin**, and the gap is exactly one key:
`DisableUpdate: false`. Updates do not auto-download, but the version is not
frozen, which is precisely what row 4 means. Two findings the old entry missed:
the VirtioFS backend is confirmed on (which is *why* AGENTS.md's environment fact
says bind-mount watchers must poll — the two now corroborate instead of merely
coexisting), and the VM's 8092 MiB is 7.9 GiB, at or just under row 4's own
`≥8 GB` floor and thin for Phase 4's six scenario families.

**The two autonomy items are structural, and the classifier proved it live.**
Asked to check whether the Codex/Claude autonomy keys were already set, the
auto-mode classifier denied the read as permission-widening reconnaissance. That
denial *is* the items' justification: an agent cannot inspect, let alone grant,
its own approval gates. They are not oversights. Their end (handoff §1.4/§1.5) is
not the product's autonomy at all but the *build's* throughput: permissions are
"the #1 autonomy killer", every prompt is a stall until the operator returns, and
Codex's default sandbox blocks the Docker socket that Phases 3–4 require — so
without its profile Codex cannot do the container phases at all, and the two-pool
quota failover AGENTS.md §8 depends on does not exist. The handoff's own warning
is the argument for doing it early or not at all: "a quota outage is the wrong
moment to debug config."

**Closed: `db_schemas.sql`.** The spec wins over it by the handoff's own rule, and
the schema is now v2 with a migration, a 155-case backstop, and the trigger
lattice. A v0 seed file dropped in at Phase 3 would confuse, not inform.

**Closed: the `docs/priors/` originals — the notes stay.** The distinction matters
and nearly went the wrong way. The *entry* was a standing request for the lost
`poc/` material; the three *notes* are trusted and load-bearing, carrying Inv. 14
(the SDK's depth-1 subagent cap the spec treats as enforced "by construction"),
Inv. 13 and the never-cut forbidden-env scan, and spec §11.4/S3's credential-home
rule — plus live Phase 5 regression obligations. Only the request is closed. Cost:
Phase 5 builds its sharp-edge regressions from the reconstructed one-liners rather
than original repros, and re-observes the detail empirically.

Discipline note, self-inflicted: closing the two entries was first written *as an
explanation of why they were closed*, left in `PROGRESS.md`. That is the tombstone
anti-pattern §5 bans, in spirit if not in strikethrough — dead content that still
costs tokens and still parses as instruction. Deleted; the rationale is here,
which is where history goes. The older "reconciled 2026-07-15, eleven entries
removed, two were lying" paragraph went the same way for the same reason.

## 2026-07-15 — handoff §4.1 row 4 closed; Parked is down to one item

The operator raised the Docker VM to 12288 MiB (11.7 GiB live — Docker restarted
into it, verified via `docker info`, not just the settings file), pushed `main`,
and the row 4 entry leaves Parked entirely. **One parked item remains: the S7
sleep drill**, which is agent-impossible by nature.

ADR-019's concurrent peak — pipeline 4096 + guard 512 + lease-free homie 2048 +
guard 512 + helper 512 = 7680, plus a transient setup/landing 1024 = 8704 — now
fits with ~3 GiB spare. It did not fit before: 8092 MiB left 412 MiB for dockerd,
containerd, and the VM kernel, and went 612 MiB over the moment a setup or
landing container appeared. Phase 4's six scenario families would have been the
first thing to put three containers in the air at once, so this was an OOM
waiting at exactly the next phase.

**Three claims were overturned by the operator in one exchange, all by looking at
the thing rather than reasoning about it.** Worth recording as a pattern, not as
three incidents:

1. *"The freeze gap is `DisableUpdate: false`."* Wrong. That is an admin-policy
   key (Docker's Settings Management), not the user auto-update preference. The
   operator's settings pane showed "Always download updates" and "Automatically
   update components" already unchecked — `AutoDownloadUpdates: false`,
   `SilentModulesUpdate: false`. Nothing auto-installs; row 4's pin was already
   satisfied. A key was read straight out of JSON and mapped to a requirement
   without checking it against the UI that owns it.
2. *"8 GiB is thin because the Playwright image is 1–2 GB."* Wrong — that is disk,
   not RAM, and the 122 GiB disk was never in question. The conclusion survived
   only because the real derivation (ADR-019's envelopes) happens to be worse.
   Right answer, wrong reason, which is indistinguishable from luck.
3. *"`git pull`."* Wrong three times running. The operator and the agent share one
   working copy on one machine; there is nothing to pull, ever. `git push` is the
   only operation that moves anything, and only the operator may run it.

The common failure is reasoning from names and defaults instead of measuring —
the exact thing this repo's discipline exists to prevent, and which the same
session had just applied correctly to the D4 router (where a planted mutant
exposed a test asserting SQLite's CHECK rather than the router's own guard). The
lesson does not transfer by itself: it has to be applied to the boring
config-shaped claims too, not only to the interesting code-shaped ones.

Also closed: the secrets-in-history worry, verified rather than assumed. A
`filter-repo` scrub ran 2026-07-10 (reflog d14578b) *after* the
IMPLEMENTATION-NOTES entry that recorded the rewrite as declined, and that entry
was never corrected — so the file's own paragraph still claimed live secrets sat
in the seed commits and pre-accepted them travelling to any future remote.
`git rev-list --objects --all` returns nothing for the path: the 2026-07-15 push
to `git@github.com:vinchenkov/CullingHouse` carried no secrets. The operator moved
to delete the stale paragraph and deleted the agent's *note about* it instead —
an understandable miss, since the flag was the thing labelled STALE and sat at the
file's end while the falsehood sat mid-file. Net effect had been the worst
combination: the false claim alive, the warning gone. Both are now removed; the
verified truth lives in IMPLEMENTATION-NOTES, which is why deleting the paragraph
costs nothing.

## 2026-07-16 — ADR-016 D3 lands: the v2→v3 launch-fencing migration, the D4 launch fence, the preflight marker

The outgoing `NEXT:` this session consumed:

> Land ADR-016 D3's launch-fencing columns as the v2→v3 migration, red-first
> (docs/adr/INDEX.md → 016 D3, line 275; the column list is lines 280–302) …
> Then close the two things this unblocks, in order: 1. The D4 Homie arm's
> launch fence … 2. homie.preflight_health (ADR-016 line 433).

All three landed red-first, then an adversarial review pass fixed what it
confirmed. Four commits: 5fb4221 (migration), 0394169 (launch fence),
6008b4c (marker), 747f077 (review findings).

**The migration (5fb4221).** Eleven columns on `homie_sessions`, every
pairing rule a CHECK: launch id/mode both-null-or-both-present, container id
paired with bound time and requiring a launch, start time requiring the
bound pair, typed resume debt mutually exclusive with a current launch, the
rows-only non-negative prime pair on both sides. `CurrentSchemaVersion = 3`;
the v2 schema is frozen in `substrate/testdata/schema-v2.sql`; migrated v1
and v2 spines are held to the same fence set and column/index shape as a
fresh spine by the same assertion helper; a planted duplicate column proves
mid-step DDL failure rolls back to an untouched v2. Two iff readings logged
in IMPLEMENTATION-NOTES (prime pair present exactly when mode is `rows`;
resume_mode present exactly when `resume_owed=1`).

**The D4 launch fence (0394169).** `RefusalCandidate` carries the
`current_launch_id` observed at selection; the RefusalHomie arm ends the
session only when the row is still on that generation. A fence miss applies
NO consequence (`consequence:"none"`, `launch_superseded:true`) — the stale
class's posture, on the reasoning that a superseded generation is
`preflight.candidate_mismatch` discovered at commit time; the current
generation earns its own verdict next tick, so no starvation. Logged in
IMPLEMENTATION-NOTES (D4 never says what a fence miss does).

**The preflight marker (6008b4c).** `homie.preflight_health` is D3 selection
bookkeeping, not a D4 consequence: closed detail exactly
`{candidate_key, defer_pipeline:true}`, key = SHA256 over a frozen canonical
JSON of the pre-prepare state (session id, whole typed launch/resume debt,
frozen binding, highest PENDING inbound seq). A golden vector pins the
serialization as a cross-harness wire contract. No caller but tests — the
consume half belongs to the future selector. Four interpretations logged.

**The review (747f077).** Three read-only lenses (ADR conformance, SQLite
semantics, test integrity), each finding adversarially verified by an
independent agent told to refute it; 6 confirmed, 2 refuted. The confirmed
set was humbling and specific — every one proven by a scratch-DB probe or a
mutant run outside the repo:

1. *(major)* A NUL-embedded 16-byte **BLOB** passed the launch-id fence: TEXT
   affinity never converts BLOBs, `length(blob)` counts bytes, GLOB reads
   only to the first NUL. The schema comment's "admits only NUL-free ASCII"
   was simply false for BLOBs. Fixed with a `typeof = 'text'` conjunct; same
   for the container id.
2. *(major)* The marker write half was never tied to the read half — a
   mutant hashing a drifted serialization passed everything. Fixed: the
   stored key is cross-checked against recomputation AND the golden vector.
3. *(minor)* `'abc' >= 0` is TRUE in SQLite (TEXT sorts above numbers), so
   the four INTEGER prime fences admitted junk; `typeof = 'integer'` pins.
4. *(minor)* The ADR closes the empty prefix to exactly (0,0); the schema
   stored (0,41) and a test canonized it. Coherence CHECK added, test fixed.
5. *(minor)* resume_owed's IN (0,1) and NOT NULL were each shadowed by a
   co-located CHECK in the only aimed test; mutants dropping either passed
   the full suite. Each now has a case only it can reject.
6. *(minor)* The claimed-but-incomplete inbound window (ADR names it
   selection-relevant) was never sampled; a `claimed_at`-for-`completed_at`
   mutant passed. Now sampled between claim and completion.

The two refutations were also work: the "health arm never appends the
marker" claim indicted unwritten selector code (the D3-not-D4 split is the
logged design), and the "empty-to-empty fence ABA" claim dissolved because
the candidate-key/stale machinery — not the launch fence — owns that seam,
and the proposed fix wouldn't have closed its own scenario.

The v2→v3 text was corrected in place — it had never run outside throwaway
test dirs, so freezing starts now, at its first real shape. D2's
activity/outbox hex fences share the BLOB hole (they were the copied
template, and they DID ship in frozen v1→v2): closing them needs a fence
trigger in a later migration; the schema comment now warns against copying
the shape. Logged in IMPLEMENTATION-NOTES.

Still true at session end: there is NO prepare/attest/commit seam —
`verbs.Dispatch` is Phase 2's single-transaction `Decide()` → `applyAction`.
Nothing produces a `refusal.Refusal`; `applyRefusal` and the marker have no
caller but tests. The launch/debt columns have no production writer either
(`homie start` relies on their defaults; resume does not yet clear/set
them). D1/D5 is what makes all of it reachable.

## 2026-07-16 (later) — ADR-016 D1: the dispatch seam exists; a refusal is producible

Outgoing NEXT (from PROGRESS.md): *Begin ADR-016 D1's dispatch seam — the
slice that makes a `refusal.Refusal` producible and everything parked behind
it reachable: the D4 router, its launch fence, the preflight marker, and
D2's stored receipt fences all have no production caller until this exists.*

Landed as five commits, 49e29d1..8ad73d6, TDD red-first throughout:

- **49e29d1** — the frozen wire half: closed canonical prepare/action
  structs (golden-vectored like homieCandidateState, separators pinned as
  test literals), `preparationToken`, `deriveDispatchKey`, request ids, and
  `loadHomieProjection` (D3's launch-generation observation, held to
  `homiePrePrepareState` by key equality in tests).
- **5df4970** — `verbs.Dispatch` became prepare → attest → commit (D1's
  native single-process form; deviation logged). Prepare: deployment-mirror
  precondition (strict no-follow), D2 receipt fence before selection state,
  lock-domain consequences (reap/reenter) committed atomically with their
  `dispatch_request_id` receipt and replayed byte-for-byte on retry; spawn
  returns candidate + token. Attest: routing.md read/digest/parse/resolve,
  failures classified as `health.routing_invalid` refusals instead of
  command errors (contract row 174). Commit: token byte-equality →
  `preflight.stale`; re-decide equality → `preflight.candidate_mismatch`;
  then exactly one consequence — claim-and-spawn with a derived
  dispatch_key receipt row, or `applyRefusal` with the key finally DERIVED
  (token + canonical action). The D4 router, the launch-fence read path,
  and the receipt columns all have production callers now. CAS losers that
  prepared before the winner committed stale inertly (refused/
  preflight.stale/none) — the CLI test accepts both loser shapes.
- **7b1add3** — `planMounts`: the boundary composition the package
  deliberately does not ship (trust → parse → resolve → authorize →
  cross-request destination uniqueness, raising the previously-unraised
  `mount.target_collision`; nested destinations reject whole), plus
  `refusalForMountError` shaping all sixteen codes into the closed enums
  with caller-supplied Authority and Msg confined to the dropped Message.
  No production candidate carries requests yet; attest skips an empty set.
- **747f077-style review, then 8ad73d6** — adversarial review of the whole
  range (single consolidated reviewer after two 4-lens workflow attempts
  died on API ENOTFOUND/529 storms; findings identical in kind): ONE
  confirmed minor — `ResolveAllowlist`-stage failures adapted with empty
  Authority, unclassifiable for its authority-decides codes; latent until
  the planner slice, then a per-tick hard error instead of D4 health. Fixed:
  that stage carries `AuthorityDeployment`. Held under attack: projection
  completeness vs `Decide`'s inputs (clock excluded-and-re-decided), the
  independent isolation of the stale vs candidate-mismatch fences in tests,
  CAS single-winner, receipt atomicity, mirror no-follow. Refuted: the
  missing attest-time mirror re-read (within the native-form deviation) and
  LimitReader truncation.

Phase-completion obligation recorded: the Docker e2e fixture now writes the
deployment mirror to the host side of the MC_HOME bind — verify at the
tagged-suite run that containerized dispatch resolves it across VirtioFS
(no-follow + regular-file checks) before trusting the lane green.

Deviations: two entries in IMPLEMENTATION-NOTES (native single-process form;
the D2 narrowings — no runtime inventory snapshot yet, routing digest bound
in canonical_action rather than token variants, spawn replay return-arm
deferred, land still bare effect, no token TTL invented).

Still true at session end: the eleven launch/debt columns have NO production
writer (the frame *observes* them; wake/resume writers are the selector/
effector slices). The preflight marker still has no production caller —
branch-7 Homie candidate selection does not exist. Routing failures now
surface as health refusals with derived keys; `mc dispatch` on an
un-onboarded MC_HOME refuses inertly (run `mc onboard home`).

## 2026-07-16 (later still) — schema v4: the D2 BLOB fence triggers

The small queued item behind the D1 frame, landed red-first. The current
schema.sql was frozen as `testdata/schema-v3.sql` (at b9bff07) BEFORE any
edit, per the fixture discipline. RED: BLOB forgeries added to the two D2
backstop helpers — a fully-hex BLOB (a UNIQUE value distinct from its TEXT
twin, invisible to its own receipt lookup) and a NUL-embedded one, for
dispatch_key, dispatch_request_id, dispatch_result, and
event_destination_key — all of which the v3 spine happily stored, proving
the hole; plus `TestMigrateV3ToCurrentMatchesAFreshSpine` (fence parity,
trigger parity via a new `triggersOf` helper, version stamp, v3 history
survival, and §16.4 atomic-rollback with a planted conflicting trigger
name). GREEN: `migrationV3ToV4` — two INSERT triggers
(`activity_receipt_keys_are_text`, `outbox_event_destination_key_is_text`);
activity is append-only and `outbox_content_immutable` already refuses key
changes on UPDATE, so INSERT is the only write surface. Same triggers in
schema.sql's fresh shape; the KNOWN GAP comment at the D2 columns now points
at the trigger instead of promising one. dispatch_result got the typeof pin
too — json_valid(BLOB) is version-dependent SQLite behavior, and the closed
posture costs one WHEN clause.

## 2026-07-16 (Codex takeover) — the outgoing D1 frame was not safe to extend

Outgoing NEXT (from PROGRESS.md): *derive candidate mount requests and
JurisdictionInput, then wire the already-tested mount planner into attest.*

The required cross-harness review instead stopped that line. A read-only
reviewer compared `ed55b2c..a1767cd` to the Phase 3 contract and found four
majors: Darwin still delegates the entire dispatch into the helper, attest did
not reopen the deployment mirror, mutating attested commits lacked exact-result
request receipts, and the mount planner remained test-only. It found no
schema-v4 defect and no hole in the mount-code/refusal adapter. The startup fast
lane independently exposed the fifth, minor finding: all nine resident
split-brain cuts failed because their fixture had not materialized the new
deployment mirror.

Three green commits closed the independently repairable findings red-first:

- `96fffbf` propagates `mc init`'s deployment UUID into the resident fixture;
  resident returned to 42/42 and the full five-leg fast lane passed.
- `891bf2f` reopens the strict mirror during attest, binds that second
  observation into the attested frame, and makes commit compare it to both the
  prepared identity and spine. The old code accepted a mirror swap in the
  released-lock window; the new focused test proves it inert.
- `add7f2e` stores one atomic candidate/action key plus exact request/result
  receipt for every mutating attested consequence. Lost-response replay is
  byte-exact for spawn, deployment health, task block, and launch-fenced Homie
  end. Health writes the receipt on its original row because activity is
  append-only; the other consequences insert their receipt in the same
  transaction. A first attempted UPDATE correctly died on the append-only
  trigger and was replaced rather than weakening history.

The host/helper major remains the prerequisite for mount wiring. ADR-016 D1
and ADR-018 D6 require a resident-only one-shot AF_UNIX control channel at
inherited fd 3, closed bounded hello frames, private same-binary prepare/commit
helper calls, and host-side attest. A local Bun 1.3.9 probe established that an
extra `Bun.spawn` stdio slot is a full-duplex inherited descriptor (write then
read reply through fd 3), so the signed design needs no native launcher. The
next slice starts with that control handshake/direct-shell refusal and preserves
ordinary verbs' existing whole-command passthrough before adding the private
helper frames.

## 2026-07-16 (Codex takeover closure) — the host/helper crossing is real

Outgoing NEXT: close takeover major 1 with ADR-018 D6's resident-only fd 3
control handshake, direct-shell dispatch refusal, and ADR-016 D1's private
prepare/commit helper frames around host-side attest; only then return to mount
wiring.

Two green commits closed it. `f4341dd` gives only resident-owned `mc dispatch`
an inherited AF_UNIX fd 3, validates closed bounded hello/hello_ack identity
(release, control, spine schema, config schema, deployment), marks the child
descriptor close-on-exec, and leaves every ordinary verb on its byte/exit-exact
whole-command path. `06406df` adds the separate private prepare and commit
invocations, each under its own helper flock transaction with host attest in
between; canonical 1 MiB frames, 64 KiB final results, token/release/request
identity, final routing/mirror recheck, and fixed production helper/spine scope
all fail closed. The container-side helper receives an absolute deadline fixed
before Docker startup, so a timed-out broker cannot leave a later helper able
to commit. D2's 4 KiB scalar bounds now have canonical task/wave title
admission and migration-time legacy validation rather than a dispatch-only
wedge.

The read-only adversarial reviewer rejected two intermediate versions: first
for a Docker-CLI-only kill plus unbacked collection caps, then for a relative
helper timer and missing scalar admission. Both rounds were fixed and retested;
the third verdict was ready with no blockers. The five-leg fast lane passed
after the final edits: mc, resident 45, fake harness 43, agent runner 13, and
runner image 40. The fixed helper coordinates/deadline values are recorded in
IMPLEMENTATION-NOTES; no operator decision is required.

NEXT: wire the already-tested mount planner into the corrected host attest
crossing, red-first. Derive candidate mount requests and complete
`boundary.JurisdictionInput` from the selected Worksource/sandbox profile and
protected host roots; carry only the closed authorization/refusal result back
to commit, and prove invalid plans claim no Run and emit no spawn. Do not load
launchd.

## 2026-07-16 (Codex) — mount authority inputs are frozen before host attest

Outgoing NEXT: wire the tested mount planner into the corrected host attest
crossing, beginning with candidate mount derivation and the complete
`boundary.JurisdictionInput`.

The first green prerequisite is `36fc91f`. Prepare now selects the subject's
Worksource from its already-read task projection, loads every Worksource and
sandbox profile in canonical order, normalizes the three path arrays, and binds
that closed state into both the preparation token and private candidate.
Commit reloads the projection against the prepared spawn and returns stale on
any drift. Migration, init, onboarding, Worksource add, Worksource status
changes, and private decode share the exact canonical projection validator, so
the added frame input cannot become a latent oversized-dispatch wedge.

A read-only reviewer first rejected the slice because individually bounded
profile arrays could exceed the private frame and Worksource cardinality was
unbounded. After the shared 256 KiB aggregate fence it found one remaining
transactional hole: `active` to `archived` adds two canonical bytes. The final
version validates that writer in-transaction; an exact-limit test proves the
archive is rejected and rolled back. The reviewer then returned READY. The
full five-leg fast lane passed: mc, resident 45, fake harness 43, agent runner
13, runner image 40.

The static resident effect still asks for a direct Git workspace bind, which
ADR-017 forbids. That argv is not evidence of mount authority and must be
refused or replaced, never authorized by adapting the planner input to it.

NEXT: derive ordinary selected-profile mount requests and the full host
jurisdiction input red-first, then invoke `planMounts` only during host attest.
Carry its closed authorization/refusal result through private commit and prove
invalid plans claim no Run and emit no spawn. Do not load launchd.

## 2026-07-16 (Codex) — ordinary profile requests reach host mount attest

Outgoing NEXT: derive ordinary selected-profile requests, assemble complete
host jurisdiction, call `planMounts` only in attest, and prove invalid plans
claim no Run and emit no spawn.

Commit `d7babcb` closes that refusal half. Artifact roots become candidate RW
requests, readonly mounts become candidate RO requests, and no other
Worksource contributes a request. Host attest associates the frozen projection
with the operator account HOME from the account database, trusted MC_HOME,
present HOME credential classes, every runtime-control path, explicit own/other
Worksource roots, Git/.mission-control ownership sets, gateway-secret inventory,
and typed roots. The fixed policy path is `MC_HOME/mount-allowlist`. One
`ResolveJurisdiction` call marks only selected-profile denied-path construction
errors as candidate-authored; deployment/protected-root failures remain health
even under filesystem races.

Invalid requested sources and an invalid nonempty denied policy now flow
through the private attestation to the existing D4 router: zero Runs, free lock,
no spawn, sanitized candidate consequence. A real Git Worksource is never
translated into an ordinary direct workspace bind. Because the authoritative
Git control/projection registry does not exist yet, production Git candidates
fail with deployment-owned `mount.runtime_unappliable`. Likewise a valid
nonempty ordinary authorization set fails deployment health rather than being
silently dropped until its closed carrier and resident effector exist. The
Phase-1 fake route alone retains its static workspace fixture; an untagged
production binary cannot parse that route.

The read-only reviewer rejected three intermediate versions: aggregate/root
inventory and ambient-HOME gaps (five blockers), then repo authority plus a
double-resolution race (two blockers). Account-database HOME, zero-request
denied-policy validation, fixed allowlist naming, authoritative empty in-memory
gateway inventory, fail-closed Git inventory, and single-pass error provenance
closed them. Final verdict READY. The five-leg fast lane passed: mc, resident
45, fake harness 43, agent runner 13, runner image 40.

NEXT: replace the valid-plan `mount.runtime_unappliable` stop with a bounded
canonical authorization carrier through private attest, spawn effect, and
resident structured binds. Recheck host identity/trust immediately before
Docker create and after create/before start; remove the unstarted container on
drift. Keep production Git health-refused until its authoritative typed
control/projection registry exists. Do not load launchd.

## 2026-07-16 — takeover review lands partial on quota; carrier slice designed

Claude session, resuming after the Codex range a1767cd..e423780. Per AGENTS.md
§2 the range got an adversarial takeover review before any building-on: four
spawned lenses (contract, fail-closed, test-honesty, regression) over the diff
vs docs/phase3-contract.md, each finding then handed to a spawned skeptic.
Three lenses returned 17 findings; the regression lens and all verifiers died
on the session usage limit, so triage fell to the session agent by direct code
reading. Full disposition in IMPLEMENTATION-NOTES.md (2026-07-16 takeover
entry): five confirmed items fold into the carrier slice (vacuous
structural-bounds test, untested commit-side mount drift fence, untested
health stops, profile-less Worksource misclassified as candidate authority,
assembly-stage MountError escaping D4), four confirmed items recorded for
later slices (64 KiB post-commit result wedge, helper clock-skew intolerance,
missing commit-side replay return, five coverage gaps), one alleged major
refuted (the resident's fake/fake gate keeps the static bind out of every
production route). No finding blocks the range.

The authorization-carrier slice (the standing NEXT) is fully designed against
ADR-016 D2/D5/D6 and ADR-017 D6; design pins for the implementing session:

- Carrier: closed `PrivateDispatchMountPlan{version:1, entries[]}`, each entry
  `{logical_id, source (canonical host path), destination (absolute container
  path), kind dir|file, access ro|rw, device, inode (decimal strings — JS
  number-safety), owner_uid, mode (perm bits)}`. Entries sorted by
  destination; ≤256; destinations unique and non-overlapping; whole plan
  byte-bounded at attest (health refusal if it could push the committed spawn
  effect past the broker's 64 KiB result cap — never wedge post-commit).
- Destinations per ADR-017 D6 class prefixes: artifact requests
  `/workspace/artifacts/<target>[/<suffix>]`, reference requests
  `/workspace/references/<target>[/<suffix>]`; collision checked on the final
  absolute destination. The test-fake legacy workspace request (repo kind
  under fake routing only) authorizes source=profile workspace_root through
  the same allowlist/jurisdiction pipeline to `/workspace/source` RW.
- attestCandidateMounts returns the plan (empty entries for subjectless or
  empty-profile candidates); mountattest.go:306's runtime_unappliable stop is
  deleted. Both repo-kind production stops stay. Profile-less selected
  Worksource reclassifies to AuthorityDeployment (health). Assembly-stage
  MountError adapts to health instead of erroring.
- attestedDispatch and PrivateDispatchAttestation carry the plan; the
  canonical private attestation includes it, so DispatchRecheckPrivate stales
  on any evidence drift (chmod/chown/inode swap between attest and commit —
  ADR-016 D5's before-commit repeat). validatePrivateAttestation: refusal XOR
  (route+plan). canonicalAction gains plan_digest =
  SHA256("MC-DISPATCH-PLAN-V1\0" || canonical_plan) (golden vectors updated
  deliberately). applySpawn's effect gains mount_plan; the receipt replays it
  byte-exact.
- Resident: spawn validates the plan (absolute paths, no colons, access enum),
  writes it host-side as `<mcHome>/runs/<run_id>.mounts.json` (0600 sibling of
  the envelope, removed with it at reap; NEVER mounted into the container — the
  agent-visible run.json keeps only mounts.session, which is all the runner
  reads), then `mc __mount-recheck <plan-file>` → docker create (plan-derived
  -v binds, no config.workspaceRoot) → recheck again → docker start; any drift
  or refusal removes the unstarted container. Rechecks skip when entries are
  empty. land() keeps config.workspaceRoot until the Git registry slice.
- `mc __mount-recheck`: host-scope read-only private verb; trusts the plan
  file (operator-owned 0600 non-symlink), re-resolves each source, requires
  canonical-path equality and (device,inode,kind,owner,mode) evidence
  equality. ACL-snapshot and containment rechecks at create/start are logged
  residuals (need the jurisdiction carrier).
- e2e obligations (Docker lane): f.home to 0700, mount-allowlist
  `path=f.ws target="source" access="rw"` at 0600, `mc init --workspace-root`
  becomes the host f.ws. Known lockstep TS updates: effects.test.ts exact
  argv/run.json shape, tick-loop single-docker-call queue and failure wording,
  split-brain RecordingDocker/LocalWorkerDocker seams keyed on `run -d`.

Quota note: the session usage limit was hit mid-review (resets 2026-07-16
20:40 PT). Per §8 this session banks the review + design at a green docs
commit; implementation proceeds in micro-steps only while budget lasts.

## 2026-07-16 — the authorization carrier lands: attest → frames → effect → resident

Claude session, continuing from the same-day design entry. The standing NEXT
is implemented across six green commits (acf78f0..b1de870), every fast-lane
leg passing after each:

- acf78f0 — attestCandidateMounts returns the bounded evidence-backed
  PrivateDispatchMountPlan (canonical source, ADR-017 D6 class-prefixed
  destination, access, decimal-string device/inode + kind/owner/mode
  evidence, sorted by destination, 32 KiB attest-side byte bound) and the
  runtime_unappliable stop at the old mountattest.go:306 is gone. The
  test-fake legacy workspace request now rides the same
  allowlist/jurisdiction pipeline to exactly /workspace/source RW. Folded
  takeover fixes: absent profile and assembly-stage MountErrors are
  deployment health.
- f2f899d — the private attestation frames the plan (helper re-validates:
  refusal XOR route+plan), canonicalAction gains plan_digest under
  MC-DISPATCH-PLAN-V1 bound into dispatch_key (golden vectors updated
  deliberately; the canonical plan bytes have their own frozen vector), and
  the committed spawn effect carries mount_plan with byte-exact replay —
  carrier fields are declared in alphabetical json order because the D2
  replay path round-trips maps. DispatchRecheckPrivate stales on evidence
  drift, proven end-to-end with a chmod between attest and commit. The
  vacuous structural-bounds test and the untested commit-side mount-state
  drift fence from the takeover review are both fixed.
- d0ae10d — `mc __mount-recheck`: ADR-016 D5's launch-time identity/trust
  legs as a host-scope read-only private verb handled before
  self-delegation. Trusts the plan file (operator-owned 0600 non-symlink),
  strict-decodes the closed carrier, and requires canonical-path plus
  (device,inode,kind,owner,mode) equality; five drift classes proven.
- 582ae9d — the resident consumes only the committed plan: ordinary binds
  derive from mount_plan, the static workspaceRoot spawn bind is gone
  (land keeps it until the Git slice), the plan rides a host-only 0600
  sibling of the envelope (never mounted; the agent-visible run.json
  carries no host source paths), and launch is recheck → docker create →
  recheck → docker start with drift removing the unstarted container. The
  split-brain acceptance suite drives the whole carrier through the real
  binary; its docker seams model create/start and its fixtures gained the
  trusted MC_HOME, the workspace allowlist, and a canonicalized suite root.
- 40fe18e — Docker-lane e2e fixture obligations (0700 MC_HOME, workspace
  allowlist, host-path --workspace-root). The docker_e2e suite is compile-
  checked every commit and runs at phase completion.
- b1de870 — hardening from this slice's own adversarial review (one major,
  four verified minors, three empirical mutants): destination namespace
  confinement (/workspace/ only) and colon refusal and logical-id
  uniqueness in the helper validator, the same namespace rule resident-side,
  mutant-killing tests for the destination sort and both consumer-side byte
  bounds, and the absent-profile health arm proven inert through
  dispatchCommit. The major — concatenated `-v` binds vs ADR-017 D3's
  structured objects — is logged with a named owner (the production
  resident's Engine-API effector); colon refusals at three layers carry the
  posture meanwhile.

Review verdicts on the remaining hunt categories: no hostile input reaches a
spawn or bind without a typed refusal; dispatch_key/replay coherence holds
(distinct plans cannot share a key; a nonempty-plan spawn replay is verified
analytically, driven end-to-end only for the empty plan); the second-recheck→
start window is exactly ADR-016 D5's documented Docker path-string residual.
The fake-lane e2e trace found no breaker but the Docker lane has not run in
this range — a phase-completion obligation alongside the D1 deployment-mirror
check.

Outgoing NEXT (superseded): implement the authorization-carrier slice as
pinned in the same-day design entry. Done as above.

## 2026-07-17 — the Git control/projection registry and the typed task plan class

Claude session, continuing the standing NEXT from the carrier slice. Five
green commits (6d07b79..c24e319), the full five-leg fast lane passing at
each boundary:

- 6d07b79 — `mc/verbs/gitregistry.go`: live per-attest resolution of a repo
  Worksource's Git administrative identities. A `.git` directory is one
  control; a linked-worktree pointer file chases gitdir + commondir with
  bounded single-line reads; an absent `.git` stays an absent-encoded
  protected member (ADR-021 D8); symlinked/unparsable/oversized/dangling
  shapes deny typed wrong-kind/missing. No spine table, deliberately:
  D9/D11 forbid cached jurisdiction inputs, so registration is resolution
  (IMPLEMENTATION-NOTES 2026-07-17).
- dab9e5f — the typed plan class. `taskPlanRows` pins the closed 15-row
  ADR-017 D6 standalone-task table (root RO at /workspace, source/git RW
  for the Worker, twelve RO covers; worktree name pinned `mc-task-<id>`);
  `resolveTaskLocalSkeleton` validates the exact skeleton (constructed
  canonical paths, operator owner, 0555 root, fixed pointer bytes, empty
  generated covers, `git/config` pinned EMPTY until setup owns the
  sanitized grammar). planMounts gains the typed arm — claims checked
  against jurisdiction typed roots, allowlist bypassed per ADR-017:349-351,
  blocked floor kept, fixed destinations, nesting only on D6's named
  edges. The derivation emits the row set only for a production repo
  Worker WITH a subject; verifier/packager/refiner/editor and every
  projection arm refuse `runtime_unappliable` deployment health naming the
  missing materialization. `captureDispatchMountHostSnapshot` loses the
  "registered Git control identities are not yet available" stop: registry
  controls and the resolved `.mission-control` root feed jurisdiction, the
  subject's skeleton resolves into TypedRoots, and the whole arm is proven
  through the REAL capture (unstubbed) — including an allowlisted artifact
  inside `.git` refusing denied_root. The helper boundary re-validates the
  closed destination set and the same edges. The fake lane keeps empty
  GitControls: registering even the absent `.git` member would kill the
  sanctioned legacy workspace bind through D8 absent-member protection.
- 4191618 — the resident's -v grammar admits exactly `/workspace` (the
  task-root row) alongside `/workspace/` descendants.
- 9575733 — end-to-end proof: full native Dispatch (prepare → attest →
  commit) for a repo Worker over an exact task-7 skeleton commits one
  spawn effect carrying the 15-row mount_plan, task root first.
- c24e319 — session self-review fix: the helper's destination-overlap
  check was adjacent-only after sorting, so a sibling interleaving between
  an ancestor and its descendant ('-' < '/') hid a forbidden nesting from
  a hostile broker. Pre-existing (predates the typed rows); proven red,
  fixed by scanning every prior entry.

Review honesty: the spawned adversarial review (three lenses + verifiers,
run as a workflow) died entirely on the session usage limit (resets
2026-07-17 01:50 PT) — zero lenses returned. Per the 2026-07-16 precedent
verification fell to the session agent by direct code reading across five
risk surfaces (blocked-floor interaction, capture→plan TOCTOU, the
"/workspace" edge closure, helper bypasses, cover completeness vs D6);
finding: the c24e319 gap above, everything else held. A spawned re-review
of 6d07b79..c24e319 is the next session's first act.

Still-open arms this slice deliberately did not touch: skeleton
creation/population (resident precreate + setup closure extraction,
ADR-016 D5), seal/disposable/projection materializations, preclaim
guard/mount canaries, ACL/containment recheck halves, the production
resident's structured binds, and the Docker-lane phase-completion run.

Outgoing NEXT (superseded): build the authoritative Git control/projection
registry and its typed plan class red-first. Done as above.

## 2026-07-17 — registry re-review and identity-fenced skeleton precreate

Codex session, continuing the standing two-part NEXT. The registry/typed-plan
range was reviewed by three spawned read-only lenses against phase-3 contract
§4, ADR-017 D6, ADR-016 D5, and ADR-021 D8-D11, then cross-verified. Six
confirmed findings were closed red-first; two alleged helper-boundary findings
were refuted against ADR-016 D1's trusted Darwin-broker boundary:

- aded102 refuses initiative children before standalone-task mount derivation;
  the token-bound mount state now preserves the task's initiative identity.
- cbc6244 protects a Worksource root carrying bare-repository markers instead
  of misregistering its Git control as an absent `.git` member.
- d0aa1d0 binds the protected-set membership identity into the canonical plan
  digest, so unchanged mount rows cannot hide jurisdiction drift.
- e967341 carries fixed Git-control byte digests and empty-directory predicates
  into both launch rechecks.
- 1826490 resolves declared protected and denied paths through the exact D8
  effective-ancestor algorithm, binding anchor identity plus absent suffix.
- 0733f7b preserves candidate authority when denied-path evidence changes
  between jurisdiction construction and digesting.

The first skeleton-materialization slice then landed as 31e1127. The resident
now has an exclusive primitive for creating an absent
`task-<id>/{source,git}` beneath the exact preclaim tasks-parent identity. It
creates children before fixing the root 0555, applies only the canary-supplied
final-uid writable child mode, returns the registered root identity, and never
repairs or cleans up an unexpected existing/residual path. A spawned verifier
found that the first version did not recheck child identity and emptiness at
the end of precreate; a concurrent test proved the gap red, the primitive now
rechecks both children, and the verifier returned VERIFIED. The full fast lane
is green: mc, fake-harness 43, agent-runner 13, runner/image 40, resident 57.

The human operator's IMPLEMENTATION-NOTES compaction is recorded in 87ac29a;
its stale Parked request was therefore removed from live state. No launchd,
setup closure fill, fixed covers, seal, projection, or Engine-API work occurred
in this slice.

Outgoing NEXT (superseded): re-run the registry/typed-plan review with spawned
lenses and verifiers, then precreate the empty identity-fenced task skeleton.
Done as above.

NEXT: integrate `precreateTaskSkeleton` red-first into the digest-covered
post-claim effect path. Carry the exact preclaim tasks-parent identity and
canary-proved child mode, invoke precreate immediately after a repo Worker's
claim, and register the returned task-root identity before first setup. Resolve
the current prepare-time demand for an already populated skeleton without
weakening D8's absent-path fence. Keep setup fill/covers, seals, projections,
structured Engine-API binds, and launchd in later slices.

## 2026-07-17 — absent first-task state crosses claim and reaches the resident

The outgoing NEXT above is complete in 8aea935. A first standalone repo Worker
no longer needs a populated task skeleton at prepare: attest distinguishes the
exact absent task root from existing retry residue, binds the canonical
mode-0700 `.mission-control/tasks` parent `(device,inode,owner)` identity and
closed child mode into the plan digest, filters only the not-yet-existing typed
task rows, then commits the ordinary claim. The committed effect replays
byte-for-byte with one Run and one `dispatch.spawn` activity.

The resident performs the closed post-claim prefix only: repeat parent
identity, exact mode, native ACL trust, and absence through host `mc`; call the
exclusive precreate primitive; validate its returned identity; register that
exact object in the per-resident run context; stop. It never launches an agent
against the empty skeleton and never guesses the future 15-row plan. Creation,
registration, and trust failures leave no session/run envelope or Docker
effect. The in-memory registration is intentionally not represented as crash-
durable: the next setup slice owns that receipt/recovery boundary before it
adds a setup consumer.

The session-start cross-harness review found `31e1127..d2f3e68` was
administrative-only and passed. The post-implementation spawned review found
one blocker—the first carrier captured inode/owner but omitted exact mode and
native ACL repetition. Red tests and the host-local task-parent recheck closed
it; the final verdict was PASS. It also prompted hostile candidate/step tests,
exact replay/no-duplicate coverage, the JS-safe task-id bound, and an exact
0700 anti-widening fence at helper and resident. All five fast-lane legs pass:
mc, fake-harness 43, agent-runner 13, runner/image 40, resident 63.

NEXT: implement the first-task setup transaction red-first. Replace the
per-resident task-root registration with a durable run/task-fenced setup
receipt that recognizes only the exact returned identity on retry; consume it
in the first fixed setup action to populate the pinned reachable closure and
relative Git controls; inspect/recheck the result before the 15 task mount rows
can enter an agent plan. Keep accepted seals, downstream reconciliation,
disposable/committed projections, structured Engine-API binds, and launchd in
their named later slices.

## 2026-07-17 — durable first-task setup registration

The first green micro-step of the setup transaction replaces the resident's
process-local `registeredTaskRoots` map. Schema v5 adds
`task_setup_receipts`, an immutable/no-delete receipt keyed by run and carrying
only `{task_id, root_device, root_inode, root_owner_uid}`. It intentionally
does not persist a host path: the fixed setup action must derive that from the
registered Worksource and re-attest it.

`verbs.RegisterFirstTaskSetup` takes `BEGIN IMMEDIATE`, requires a live
pipeline Worker run whose subject matches the claimed task and whose lock row
still names the same run/task, then inserts the receipt. A lost-response retry
with exactly the same identity succeeds; a changed identity, ended/wrong role
run, or lost lease refuses. The resident now calls host-scoped `mc task
setup-register` after local skeleton creation, before it can return with setup
pending, so a resident restart cannot rebind a new task root in memory.

Focused Go/substrate/CLI tests and the five-leg fast lane passed. The direct
`bun test` invocation from `resident/` is not a valid lane on this host because
it lacks `go` in PATH for split-brain fixtures; `resident/check.sh` supplies
the project environment and passed all 63 resident tests.

NEXT: implement the fixed first-task setup action red-first. It must consume
only the durable exact root receipt, populate the pinned reachable closure and
relative Git controls, then inspect/recheck the result before the 15 task mount
rows can enter an agent plan. Keep accepted seals, downstream reconciliation,
disposable/committed projections, structured Engine-API binds, and launchd in
their named later slices.

## 2026-07-17 — setup receipt consumer seam, held red on deadline tests

Started the fixed first-task setup action with the narrow persistence seam:
`ReadFirstTaskSetup` returns only a live standalone Worker's exact durable
root receipt, after rechecking the matching active lock/run/task fence. It
returns no host path and refuses absent receipts, ended runs, and stale leases;
the future fixed setup effector must construct and re-attest its path rather
than accepting one from the database. Focused receipt tests pass.

This micro-step is deliberately uncommitted. The required mc fast lane now
fails its two known deadline timing tests even in isolation, five consecutive
runs: `TestPrivateHelperSelfDeadlineTerminatesContainerSideProcess` and
`TestPrivateHelperAbsoluteDeadlineIncludesBrokerStartup` take roughly
550–660ms while their assertion is 500ms. Both still observe the required
timeout exit code. The four non-mc lanes passed. Preserve the red state and
diagnose that unrelated test/process-start bound before committing; do not
weaken the setup receipt fence to route around it.

## 2026-07-17 — helper deadline tests fence helper time, not Go test startup

The two deadline tests were red because their fixed 500 ms outer wall-clock
limit included launching a fresh Go test binary, which takes roughly 550–660 ms
on this Mac under current load. The child still returned the required timeout
exit code; the failure never demonstrated a late in-scope deadline.

Each test now first measures the same test-binary's immediate-exit startup,
then permits only 250 ms beyond that baseline for the helper process. This
keeps the asserted property at the correct seam: once the helper is executing,
an already-expired or self-expiring absolute deadline terminates promptly. The
two focused tests passed five consecutive runs.

NEXT: implement the fixed first-task setup action red-first. It must consume
only the durable exact root receipt, populate the pinned reachable closure and
relative Git controls, then inspect/recheck the result before the 15 task mount
rows can enter an agent plan. Keep accepted seals, downstream reconciliation,
disposable/committed projections, structured Engine-API binds, and launchd in
their named later slices.

## 2026-07-17 — first-task setup receipt-attested entry gate

The first setup micro-step is green. `AttestFirstTaskSetupRoot` consumes the
existing live run/task-fenced receipt, derives the one task root from its task
id beneath a canonical Worksource root, then proves that the root remains a
non-symlink mode-0555 directory with the receipt's exact device/inode/operator
owner identity. It accepts no caller-provided task path and creates no Git
state or mount rows. The focused refusal test covers the mode drift arm; the
full five-leg fast lane is green.

NEXT: implement the fixed first-task setup closure materialization red-first.
It must build from that receipt-attested root, populate only the pinned
reachable closure and relative Git controls, then inspect/recheck the result
before the 15 task mount rows can enter an agent plan. Keep accepted seals,
downstream reconciliation, disposable/committed projections, structured
Engine-API binds, and launchd in their named later slices.

## 2026-07-17 — takeover repair before closure materialization

The required cross-harness review of `d2f3e68..c27616e` found two genuine
repairs: the resident control channel still announced schema v4 after the
receipt's v5 migration, and a host-scope caller could register a root receipt
for a foreign uid. `3473492` pins the resident handshake and test fixture to
v5, and makes registration plus root re-attestation require `os.Getuid()`.
The alleged empty-skeleton-to-Worker-launch path was refuted by the existing
fifteen-row resolver: resident precreate leaves only `source/` and `git/`, so
the absent fixed cover rows refuse before a Worker plan can be emitted.
Focused `mc/verbs` and full resident lanes are green.

NEXT: implement the fixed first-task setup closure materialization red-first.
It must build from that receipt-attested root, populate only the pinned
reachable closure and relative Git controls, then inspect/recheck the result
before the 15 task mount rows can enter an agent plan. Keep accepted seals,
downstream reconciliation, disposable/committed projections, structured
Engine-API binds, and launchd in their named later slices.

## 2026-07-17 — first-task setup inspection seam

The first closure-materialization micro-step is green. `InspectFirstTaskSetup`
now joins the durable receipt-attested root gate with the complete closed
fifteen-row task-table resolver. It returns only after the root still matches
the live run/task/lease receipt and every generated fixed cover exists with its
required kind, trust, and content pin. A focused test proves that deleting a
fixed cover after successful receipt attestation refuses the inspection.

No closure writer or agent-plan admission was added: an empty resident
skeleton remains unplannable. The remaining writer must create only the pinned
reachable object closure and relative Git controls, then call this inspection
before it makes the rows usable. The five-leg fast lane passes.

NEXT: implement the fixed first-task setup closure writer red-first. It must
build from the durable-receipt-attested root, populate only the pinned
reachable closure and relative Git controls, then call the completed
receipt-plus-15-row inspection before those rows can enter an agent plan. Keep
accepted seals, downstream reconciliation, disposable/committed projections,
structured Engine-API binds, and launchd in their named later slices.

## 2026-07-17 — takeover review closed, receipt-bound inspection, closure writer

Claude session, resuming after the other harness's c27616e..9c5d6c3. Per
AGENTS.md §2 the range got a spawned two-lens adversarial review with
per-finding adversarial verification (a dynamic workflow: mechanism +
hostile/test-strength lenses, then one verifier per finding). Five findings
confirmed, none refuted, deduplicating to three defects. The major: the new
inspection seam never re-bound the walked fifteen-row table to the durable
receipt, so a task root swapped at the same path between the attest stat and
the resolver walk returned as receipt-attested — both lenses found it
independently, and a probe demonstrated the swap passing. Repaired by
extracting `inspectFirstTaskTable`, which refuses unless the walked
KindTaskRoot row carries the receipt's exact device/inode/owner tuple; a
same-path rebuild test (via the new `tsBuildAt`) fails without the check
(7a5c4e8). The masked test minor: the un-restored chmod-0700 made the mode
gate refuse before the deleted-cover arm ever ran; the chmod is gone, so the
missing-row refusal is now the operative trigger. The third (attest-side
os.Getuid() clause has no killing test) is retained defense-in-depth, logged
as untestable without a uid seam (IMPLEMENTATION-NOTES 2026-07-17).

The standing NEXT then landed: `WriteFirstTaskSetupClosure` (8f896a9)
materializes the fixed first-task closure from the receipt-attested root —
exactly one digest-pinned pack/index pair (canonical name-covering sha256,
golden-vectored, recomputed over the landed bytes after write), the
generated covers and relative Git controls derived from taskPlanRows so the
writer cannot drift from the inspector, every create O_EXCL beneath the
resident's verified-empty source/ and git/ only, and success returned only
through the joined receipt-plus-15-row inspection. Residue refuses without
cleanup; D5's exact retry-residue acceptance and the Run-recorded pin
columns are owed to the setup-container extraction slice, which will also be
the writer's first production caller. Deviations and mode pins are in
IMPLEMENTATION-NOTES (2026-07-17).

One suite intermittent surfaced and is recorded in the PROGRESS header: the
resident control identity-mismatch test can lose its hello bytes to an
fd-3 double-ownership close race under load (EBADF instead of the mismatch
refusal), test-only shape, fail-closed either way, 16/16 green idle. The
five-leg fast lane is green at 8f896a9.

NEXT (moved to PROGRESS.md): route the standalone Worker's typed task-plan
derivation through the receipt-fenced inspection, proven through full
Dispatch over a writer-materialized skeleton.

## 2026-07-17 — dispatch-attest typed task plan gated by the setup receipt

Claude session, resuming at cf2ab7f. The standing NEXT was to route the
standalone Worker's typed task-plan derivation (mountattest.go:489) through
the receipt-fenced inspection so only receipt-bound, setup-completed roots
enter an agent plan. Before writing code, a decorrelated read-only fork
mapped the wiring against the actual code and found NEXT's literal instruction
unrealizable: `InspectFirstTaskSetup` → `ReadFirstTaskSetup` fences the receipt
to the CURRENT lock holder (a live, non-ended pipeline/worker run whose id
equals `lock.run_id`, tasksetup.go:122-132), but the line-489 arm runs at
spawn dispatch-attest where the lock is FREE and the candidate run id (minted
by `newRunID()` at prepare, canonical only at commit) is not yet a live run.
Neither the candidate nor any prior setup run's id can satisfy that fence at
attest; the run-keyed wrapper is the resident/setup-container's post-claim
consumer. The line-489 arm is the retry/resume-residue case (ADR-016:609-621):
a skeleton already materialized by a prior, now-ended setup run whose durable
`task_setup_receipts` rows persist but hold no lease.

Realized NEXT's intent the conservative way. Added a task-keyed projection
`substrate.LoadSubjectTaskSetupRoots(taskID)` returning the DISTINCT receipt
identities for the subject task, sorted for canonical determinism, no
live-lease fence (appropriate for a projection), bounded at 64. It freezes at
prepare into a new `DispatchMountState.SubjectTaskSetupRoots` field
(`loadDispatchMountState`), so it rides the existing preparation token,
commit-time `reflect.DeepEqual`, and `plan_digest` fences — no unlocked spine
read is added to the deliberately host-file-only attest seam (dispatchseam.go
:554). The arm now admits the resolved `KindTaskRoot` only when its
device/inode/owner is a member of the frozen set, reusing `inspectFirstTaskTable`'s
exact tuple encoding (`requireTaskSetupReceiptVouch`); a materialized-but-
unattested skeleton (e.g. an attacker-planted well-formed tree at the expected
path) health-refuses `mount.runtime_unappliable`/deployment. The change only
tightens what enters an agent plan (fail-closed) and adds no invariant surface.
`InspectFirstTaskSetup` stays the resident/setup-container caller. The
helper-boundary validator `validatePrivateMountState` mirrors the receipt
table's CHECKs (canonical decimal device/inode ≤20 bytes, uid ≥0,
sorted+deduped, bounded) so a hostile private frame cannot smuggle the set past
the token.

Red-first: `TestAttestCandidateMountsRefusesSkeletonWithoutSetupReceipt` and
`…RejectsReceiptForADifferentRoot` (unit), and the full-Dispatch
`TestDispatchRepoWorkerRefusesSkeletonWithoutSetupReceipt` (deployment-health
refusal, inert commit, one dispatch.health row) drove the gate; the positive
full-Dispatch proof `TestDispatchRepoWorkerCommitsTaskLocalMountPlan` and the
real-capture attest test were updated to seed a matching receipt (the new
contract). `maRepoCandidate` seeds the subject task-root identity by default.
The canonical-prepare golden vector gained `subject_task_setup_roots`. A focused
`substrate.LoadSubjectTaskSetupRoots` test pins distinct/sorted/empty. All five
fast-lane legs green; the full mc check (fmt+vet across all build tags + tests)
passes. Deviation logged (IMPLEMENTATION-NOTES 2026-07-17).

NEXT (moved to PROGRESS.md): implement the first-task setup-container
extraction slice red-first — the closure writer's first production caller —
running the sanitized pinned-SHA reachable-closure extraction in a short-lived
network=none setup container, registering the durable receipt, and replacing
the caller-supplied digest pin with the Run's recorded pins (D5/D6). That
closes the production loop the dispatch gate now requires.

## 2026-07-17/18 — first-task setup-container closure extraction (Go core)

Claude session, resuming at 7cf86ed (phase 3 dispatch setup-receipt gate). The
standing NEXT was the first-task setup-container extraction slice — the closure
writer's first production caller.

Design was locked before code via a decorrelated 3-proposal + judge
deliberation (a dynamic workflow). Plan of record: the sanitized extraction AND
the full-store materialization run IN the spineless network=none setup
container writing the store in place into the mounted-RW source/git children
(ADR-017:437-478); the HOST, holding the spine, re-attests the receipt-fenced
root, re-verifies the landed store, records the assignment, and inspects. The
staging-plane alternative (borrowing the completion-seal idiom for setup) was
rejected — ADR-017 uses staging for completion (:500), not setup. The judge
also caught that NEXT's parenthetical "plan_digest columns" is superseded by
ADR-016 D6:733-747 (launch columns are a later slice); this slice records the
closure/assignment identity in a new TASK-keyed table, no launch columns.
Architecture-of-record and both interpretation calls are in IMPLEMENTATION-NOTES
(2026-07-17).

The exact git store recipe was established empirically (a spawned agent built
real stores and iterated git plumbing to green), which corrected two of my
assumptions: git's relative-worktree mechanism is `extensions.relativeWorktrees`
(v1), NOT ADR-017:466's `worktree.useRelativePaths` (only a `git worktree add`
trigger git rewrites into the extension); and an empty `git/shallow` cover makes
git report is-shallow=true. The committed 15-row mount table pins empty
shallow/packed-refs covers, so is-shallow=true is kept as a documented harmless
consequence (fsck rc 0 for a complete store; the object-set==closure proof, not
fsck leniency, is the completeness guard). Both deviations logged.

Landed red-first, ten commits, fast lane green at each (a3c0bf2..bd478f0):
- `task_assignments` (v5→v6), task-keyed immutable (a retry reuses it, never
  rebases a moved target — D5:620); typeof-fenced hex columns; base_sha length
  checked against the row's own object_format. `Register/ReadFirstTaskAssignment`
  under the shared live-Worker+lease fence.
- `extractClosurePack`: synthetic `env -i`/GIT_DIR/GIT_OBJECT_DIRECTORY/
  GIT_NO_REPLACE_OBJECTS context over the real object dir; verify-pack proves
  object-set == reachable closure; refuses alternates/grafts/replace/shallow/
  partial-clone (each OR'd for promisor).
- `MaterializeFirstTaskStore`: full in-place store + fsck-clean self-check;
  fresh resolves the target ref, retry reuses the exact pinned OID.
- git/config empty→closed-grammar (`generatedTaskGitConfig`/
  `validateTaskGitConfig`); dispatch-attest resolver does grammar-only (spine-
  free), the config cover's mount evidence is pinned to the actual landed bytes.
- `RecordFirstTaskSetupClosure` supersedes `WriteFirstTaskSetupClosure` (removed
  with its caller-supplied `FirstTaskClosure` pin); `firstTaskClosureDigest` /
  `FirstTaskClosureFile` kept, shared with the materializer.
- `/mc/setup.json` SetupEnvelope + `mc __setup-first-task` (host-scope,
  spineless, delegation-bypassed) + `mc task setup-record` host verb.
- D5 exact retry-residue acceptance (`verifyLandedStoreMatches`).

The mc fast lane now shells to host git (git 2.50) for these tests; production
runs the identical Go inside the setup container against the pinned image git.

NOT DONE — the resident wiring is the one remaining piece and its blocker is
the dispatch plan carrying a setup step (the resident cannot derive mode/
target_ref/object_format without reading the spine). Deferred to a fresh
red-first step because it changes the frozen plan/plan_digest and the private-
frame validator; details in PROGRESS.md NEXT. Checkpointed here at a green
five-leg boundary rather than rushing a security-critical attest-path change.

NEXT (moved to PROGRESS.md): wire the resident's post-claim setup step — write
/mc/setup.json, spawn the network=none setup container, invoke `mc task
setup-record` on success — after first extending the dispatch plan to carry the
setup step (mode/target_ref/object_format/pins).

## 2026-07-18 — resident setup effect and result-handoff correction

Codex session, resuming at `8faf1a8` with the outgoing NEXT already present in
the committed resident implementation. The five-leg fast suite was rerun; its
first `mc` attempt hit the documented pre-existing concurrent-onboard race, and
the immediate retry plus every other lane passed.

The resident carries the frozen fresh/retry setup instruction in the
`task_precreate` plan, writes the credential-free `/mc/setup.json`, masks the
source `.mission-control` tree with an empty RO cover, and runs the exact
network-none, uid-10002, finite-resource setup container with only the source
and git task children writable. On a zero exit it invokes host `mc task
setup-record`, whose existing host-side fence re-attests and validates the
landed store before recording the immutable assignment. The fake Docker seam
proves argv, mount table, envelope shapes, success/failure retention, and the
record invocation.

Read-only fan-out found an inconsistency in that handoff: `effects.ts` called
`trim()` on setup stdout while claiming byte-exact transport. The red test
requires the `--result` argv value to retain the executor's trailing newline;
`3793fee` preserves all stdout bytes, using trim only to reject
whitespace-only output. The complete fast lane is green at `3793fee`.

The session also found the still-open next state-machine seam. A setup-bearing
plan intentionally contains no task mount rows, so it cannot launch an agent.
After successful recording the claimed setup run remains held; ordinary lease
reconciliation can only idle a fresh lease or reap an unhealthy no-container
lease. The next named D6 slice is therefore Worker-retry reconciliation: define
and prove the fenced handoff to a newly attested 15-row Worker plan without a
spurious retry charge/block or premature agent start. No attempt was made to
invent that transition here.

Outgoing NEXT (moved to PROGRESS.md): resolve the D6 post-setup Worker
continuation / retry-reconciliation state machine red-first.

## 2026-07-18 — successful first-task setup continuation

Codex resumed the D6 state-machine seam at `3793fee`; `5861175` is the green
checkpoint. The accepted design is
a distinct setup-only terminal rather than mutating the immutable zero-row
precreate plan: `mc task setup-continue --run` checks the exact standalone
Worker run, lease, durable root receipt, and immutable task assignment in one
transaction, stamps `setup-complete`, then fenced-releases the lease. It
spends neither quality nor dispatch budget. An exact lost-response retry reads
the terminal evidence and returns idempotently; unrecorded, stale, wrong-role,
or other terminal runs refuse without mutation.

The resident invokes that host-scoped continuation only after `setup-record`
succeeds, and preserves the setup envelope if it refuses. A direct lifecycle
test begins with a real materialized closure, records it, continues the setup
run, then runs prepare/attest/commit and proves exactly two Worker Run rows
(setup + new agent run) and the second's 15-row plan. The original zero-row
plan never launches an agent. The dispatch attest guard now also rejects a
receipt-backed on-disk skeleton without an immutable assignment, closing the
lost-record/partial-setup admission gap.

The full five-leg fast lane is green. This closes only the successful handoff;
failure/crash residue cleanup and pinned retry remain the next D6 half.

Outgoing NEXT (moved to PROGRESS.md): implement failure-side Worker-retry
reconciliation for interrupted setup containers and partial receipt/root
residue, red-first.

## 2026-07-18 — recovery carrier foundation

`d2ef90d` adds the digest-covered closed `recover_root` field to the immutable
task-precreate carrier. The private helper validates that it is the exact
derived task-root spelling with canonical identity evidence; hostile roots,
non-decimal identities, and ordinary precreate frames remain refused. This is
only the carrier foundation: host capture, predecessor setup-container
reconciliation, and inode-preserving child scrub remain required before any
recovery plan is emitted. The full Go fast lane is green.

## 2026-07-18 — D6 receipt-vouched failed-setup recovery

The recovery carrier is now consumed end-to-end at the host/resident seam.
When an existing task root has no immutable closure assignment, host attest
accepts it only if its exact device/inode/owner tuple occurs in the frozen
durable setup-receipt projection. It emits a zero-row recovery setup plan with
the same root identity; any unvouched or swapped root remains a deployment
health refusal. The private commit boundary independently confirms the tuple
against the candidate's frozen receipt set.

The resident uses a separate recovery primitive rather than weakening ordinary
precreate. `mc __task-skeleton-recover` repeats parent/root evidence checks,
opens parent and root with `O_NOFOLLOW`, recursively unlinks through directory
descriptors, rechecks the original root inode, restores 0555, and returns only
that original identity. It then registers the replacement Worker receipt and
runs the existing fresh setup → record → continue handoff. Its tests cover a
nested partial tree and a symlink entry, both removed without following the
link; the ordinary precreate primitive still rejects an existing root.

The setup container now has the deterministic `mc-setup-<run>` name. Reap
stops it before attempting `mc-run-<run>`, so a lease reaper serializes a
stale writer before the next tick can dispatch recovery. Split-brain and
resident effect tests were updated to prove the two exact stops. The full
five-leg fast suite is green. Remaining D6 work is an explicit timeline test
that joins reaping, recovery dispatch, fresh record, and the later 15-row
agent plan across crash/lost-response cuts.

## 2026-07-18 — D6 recovery timeline acceptance

`92b7047` closes the stated recovery acceptance seam with a full host-side
timeline test. A failed pre-record setup run is reaped under the ordinary
lock-domain consequence (one retry charge), then the next dispatch produces
only the zero-row receipt-vouched recovery plan. The descriptor helper removes
all partial children and recreates the fixed empty `source`/`git` pair before
the fresh materializer runs; the root inode is preserved and re-registered.

The same test models a lost `setup-record` response after the closure and its
immutable assignment have already committed. Dispatch remains `idle(lease-
held)`, so it cannot launch an agent from the zero-row setup plan. If that
setup run later reaps, the frozen assignment bypasses recovery/scrub entirely
and the subsequent claim attests the ordinary 15-row Worker plan. The two
reaps spend exactly two retry charges; no duplicate run is opened at either
cut. The full five-leg fast suite is green.

Outgoing NEXT (moved to PROGRESS.md): begin ADR-016 D6 accepted-seal rebuild
with producer/lease/request cleanup fencing.

## 2026-07-18 — completion-seal durable state foundation

`4c04e7a` advances the spine to v7 with `completion_seals`. It pins a
run/task-fenced completion request, object format, sealed SHA, closure digest,
and exact seal filesystem identity. The state machine distinguishes
`published`, `accepted`, cleanup-pending, and removed history; content is
immutable, invalid transitions are refused, and rows are never deleted. Fresh
and v1-migrated spines are structurally equivalent and the full fast suite is
green. The next slice must make Worker completion write the published receipt
and atomically accept it with the terminal consequence.

## 2026-07-18 — completion-seal acceptance transaction

Codex resumed from the durable v7 foundation. `AcceptCompletionSeal` now makes
the small authority transition explicit: it requires an existing pipeline
Worker with a task subject, an exact published run/request receipt, and the
same live singleton lease subject. In one `BEGIN IMMEDIATE` transaction it
advances the task `seeded → worked`, transitions only that immutable receipt to
`accepted`, records the Worker's ordinary `completed` terminal, and releases
the lease. The exact accepted run/request pair is a lost-response replay only
after it proves the matching durable terminal; it never attempts to reacquire
or reuse the released lease. A wrong request or non-Worker producer rolls back
without changing the task, seal, run, or lock.

Red-first tests cover the joined acceptance/terminal/release fact, exact replay,
and wrong producer/request inertness. The five-leg fast lane is green when run
sequentially; parallel execution reproduced the ledger's pre-existing
load-sensitive image/resident timeouts, then both passed serially.

NEXT (moved to PROGRESS.md): implement the accepted-seal canonical-store
rebuild consumer red-first, with exact accepted identity/manifest and producer
absence fences.

## 2026-07-18 — completion-seal manifest identity

The accepted-seal rebuild requires an explicit manifest digest; v7 retained
only the closure digest, which is insufficient to identify the manifest D6
requires a setup plan to name. `a906f9c` advances the spine to v8. Fresh
completion rows require a canonical TEXT sha256 `manifest_digest`, and the
normal immutable transition trigger freezes it. The additive v7→v8 migration
leaves historical rows NULL (it never fabricates evidence) while a new-insert
trigger makes every v8 publication non-null and typed. Consumers will refuse
that NULL legacy state rather than infer a manifest from mutable seal bytes.

Focused schema/verb tests, then the full five-leg fast lane, are green; the
parallel resident-control flake recurred once and passed on the mandated idle
rerun. The canonical-store rebuild consumer remains NEXT.

## 2026-07-18 — accepted-seal authority reader

`5f79940` adds the spine-side D6 rebuild fence. A setup caller can load only a
named `run_id + completion_request_id` that is an accepted, completed pipeline
Worker receipt and carries a canonical immutable manifest digest. It returns
only logical and filesystem identity facts—never a host path. Published or
wrong-terminal receipts are inert. The next slice consumes this authority to
open/re-attest the seal and reconstruct the task-local store.

## 2026-07-18 — manifest-verified sealed-pack reconstruction

`11c867f` adds the pure setup executor core. It hashes and strict-decodes the
accepted manifest, matches every logical field to the accepted receipt, hashes
each named pack/index byte stream, and forms a throwaway bare Git source only
from those verified bytes. The established materializer then builds and fsck
checks the canonical task-local store. A real Git fixture proves no original
Worksource read is needed after seal creation. Resident filesystem identity and
producer-absence rechecks remain the next integration seam.

## 2026-07-18 — seal-root identity re-attest

`09dd8c2` closes the filesystem half of the pure consumer: it LSTATS the seal
root before opening any manifest or pack bytes and requires a real directory
with the accepted receipt's decimal device/inode/operator uid. A swapped path,
symlink, ownership drift, or object-type change refuses before reconstruction.

`17d8ab7` adds the hostile-order proof: a mismatched receipt identity rejects
before the consumer attempts to parse even malformed manifest bytes.

`21e84df` closes the strict-decoding gap: a manifest with a valid first JSON
document plus any second document now refuses after digest verification.

## 2026-07-18 — sealed pair completeness

`84178c3` narrows the immutable seal manifest to exactly one matching
`pack-*.pack` and `pack-*.idx` basename pair. It first validates both named
records, their digest grammar, uniqueness, pair stem, and cardinality, then
opens either seal path. The regression rewrites only the accepted manifest and
its receipt digest to name a mismatched index; reconstruction refuses before
that attacker-named file can be read. The full five-leg fast lane is green
serially.

## 2026-07-18 — atomic completion-seal filesystem publisher

`d6f3729` adds the privileged completion publisher's pure filesystem half. It
requires a clean task worktree, its exact sole managed branch, closed generated
Git config/UUID, no alternates, fsck-clean objects, and stable HEAD/tree across
packing. It creates one synthetic-context verified reachable-closure pack and
index in an exclusive staging directory, binds their digests plus SHA, tree,
object count, and local UUID into the manifest, fsyncs all files/directories,
atomically renames the run-keyed seal, and freezes the root and leaves read-only.
The returned receipt is path-free and feeds `PublishCompletionSeal`.

The accepted rebuilder now rejects a manifest without canonical tree/count and
proves both against the sealed commit and reconstructed closure. Publisher →
accepted-rebuild is covered with real Git stores; dirty worktree, replacement,
manifest-tree, and manifest-count attacks refuse. The full five-leg fast lane
is green serially. The trusted completion wrapper and resident plan/confirmed-
producer-absence integration are the remaining D6 crosses.

## 2026-07-18 — strict setup-envelope framing

`c533534` makes the shared setup-envelope reader consume exactly one JSON
document. A second document now fails closed rather than being overlooked by
the decoder's array-only `More` predicate. The full five-leg fast lane is
green serially. This is the framing prerequisite for D6's accepted-seal setup
operation.

## 2026-07-18 — accepted-seal setup executor

`3305595` adds the accepted-seal arm to D6's closed `/mc/setup.json` union and
the host-scope `mc __setup-accepted-seal` entrypoint. Its required facts are
exactly the accepted Worker run/request, task, object format, sealed SHA,
closure and manifest digests, seal device/inode/owner, and fixed in-container
`/repo/seal` and `/repo/task` paths. It rejects first-task fields (including a
source path) and the first-task arm rejects sealed authority in return. The
executor maps only the frozen receipt to the already-tested manifest verifier;
it is not resident-reachable yet, because committed setup-plan attestation and
confirmed producer absence are the next distinct D6 boundary. The full
five-leg fast lane is green serially.

## 2026-07-18 — fenced completion-seal publication

`45c2aba` adds `PublishCompletionSeal`, the durable precursor to acceptance.
It admits a path-free run/task/request receipt only while the exact seeded
pipeline Worker owns the lease, and validates the object format, sealed SHA,
closure/manifest digests, and decimal filesystem identity before inserting the
immutable `published` row. A duplicate with byte-identical facts is an inert
lost-response replay—even after acceptance has completed the Worker and freed
its lease—while any changed fact refuses. Wrong role, stage, or lease makes no
row. The privileged filesystem publisher and resident producer-absence/setup
plan integration remain later D6 slices. The full five-leg fast lane is green
serially.

## 2026-07-18 — sealed Worker terminal and mounted seal root

`fa097b8` wires the trusted `mc complete` wrapper to the D6 filesystem and
spine halves. `mc complete <task> --run <run> --seal-request <16-hex>` admits
only the exact pipeline Worker identity before it touches the gated fixed
`/workspace` and `/mc/private/completion-seal` paths, publishes the immutable
receipt, then calls the receipt-bound acceptance transaction. An assigned
standalone task now refuses the older generic `--status worked` arm, closing
the model-visible bypass; legacy unassigned Phase-2 tasks retain that arm.

The actual Docker source is the already-mounted exact run seal directory, so
it cannot be replaced with the ordinary atomic directory rename. The mounted
form stages privately under that root, fsyncs/freeze-checks the pack pair,
moves it first, and moves `manifest.json` last as the consumer-visibility
marker before root fsync/read-only. The private gate plus accepted-receipt
fence make an interrupted root non-authoritative; the bounded textual
deviation is in IMPLEMENTATION-NOTES.md. Focused and complete serial five-leg
fast lanes are green. Resident plan construction and confirmed producer
absence remain the next D6 boundary.

## 2026-07-18 — task-pointed accepted seal authority

`b2e6aff` makes D6's named completion activity unambiguous across task
re-entry. Schema v9 adds a paired task pointer to the exact Worker run and
completion request. `AcceptCompletionSeal` writes it in the same transaction
as `seeded → worked`, accepted state, terminal receipt, and lease release;
the task trigger allows only a pointer that names that task's accepted seal in
worked state. Dispatch's frozen mount-state projection reads the pointer and
all receipt facts, rather than selecting a historical seal by time or run-id.
Focused and full serial fast lanes are green. The next slice turns this
path-free authority into an attested resident setup instruction and proves the
producer absent before execution.

## 2026-07-18 — accepted-seal rebuild plan carrier

`ac4b7c9` adds a closed `accepted_seal_rebuild` member to the digest-bound
mount plan. A verifier candidate with the task-pointed accepted receipt carries
the exact run/request, object format, SHA, both digests, and seal filesystem
identity; the helper repeats the full grammar. It grants no inferred task bind
before setup. The resident must next re-attest `MC_HOME/seals/<run>`, require
the producer artifacts absent, and run the fixed rebuild executor.

## 2026-07-18 — resident accepted-seal launch fence

`3add3d3` makes the resident recognize an accepted-seal rebuild plan and stop
before any filesystem, `mc`, Docker, or verifier-agent effect. This closes the
unsafe fall-through while the dedicated producer-absence and rebuild executor
is implemented next. Full serial fast lane green.

## 2026-07-18 — verifier accepted-seal plan binds the canonical task store

`07615df` makes the verifier accepted-seal carrier attest the complete
15-row task plan as read-only rather than guessing a host task-root path in
the resident. That gives the later fixed setup executor the one canonical
task-root source it may bind at `/repo/task`, while keeping the verifier launch
fenced: its disposable RW source remains a separate D6 operation. The full
serial fast lane was green.

## 2026-07-18 — resident accepted completion-seal re-attestation foundation

`5c4c2a8` validates every accepted-seal receipt field at the resident boundary
and adds the real host recheck primitive. It derives only
`MC_HOME/seals/<run-id>`, rejects a noncanonical/symlinked/different
directory, and repeats the receipt device/inode/owner proof immediately before
the future trusted setup bind; the in-container executor independently repeats
that proof. The plan remains a no-effect launch fence until the dedicated
accepted-seal setup operation gains its durable replay/terminal fence; the
Verifier disposable RW source remains the separately named later D6 arm.
Full serial fast lane green.

## 2026-07-18 — accepted-seal producer-absence fence

`c54188d` makes the resident’s accepted-seal fence inspect the exact prior
Worker agent and setup container names before it can even retain the setup
carrier. Only Docker’s exact absent result for both succeeds; a live,
exited-but-not-removed, malformed, mislabeled, or inspect-unavailable object
refuses. The producer identity requires a full 64-hex Docker ID, exact name,
and `mc-managed=true`, pipeline-tier, and run-id labels; ordinary agent and
first-task setup containers now receive those labels. On confirmed absence the
resident re-attests the derived run-keyed seal identity, then remains an
intentional no-create fence until the closed setup executor has a durable
response-loss/lease continuation. Network guards are not present until the
separately named ADR-018 runtime slice and will join this inventory there.
The full serial fast lane is green.

## 2026-07-18 — durable accepted-seal rebuild receipt

`b204ace` raises the spine to schema v10 with
`accepted_seal_rebuild_receipts`. A row is immutable/undeletable and repeats
the exact accepted Worker run/request, SHA, closure and manifest digests, the
registered task-root identity, and the rebuilt local-store result. Its insert
trigger independently joins the currently live verifier run/lease, worked
task pointer, accepted seal contents, and a prior task-root receipt; all other
shaped rows refuse. Fresh-versus-v1-migrated schema parity and live/tampered
receipt cases are covered. The host record and run-continuation verbs are next;
the resident remains unable to run the rebuild until they exist. Full serial
fast lane green.

## 2026-07-18 — accepted-seal rebuild host record and continuation

The host-facing D6 half now exists. `RecordAcceptedSealRebuild` first derives
the task root only from the live Verifier lease and selected Worksource,
re-attests its fixed 0555 operator-owned identity against a prior task-root
receipt, and verifies the reconstructed store bytes against the setup result.
Its transaction then requires the task's exact accepted Worker pointer and
matches object format, sealed SHA, and closure digest before inserting the v10
receipt. Exact lost-response replay returns only an identical durable receipt.

`ContinueAcceptedSealRebuild` requires that receipt, fences the same live
Verifier lease, writes only `accepted-seal-rebuilt`, and releases only that
lease; replay proves the durable terminal. Dedicated host-scope `mc task
accepted-seal-record` and `accepted-seal-continue` commands expose the two
operations for the later resident handoff. Focused re-attestation, mismatch,
lease, replay, and CLI framing tests plus the full serial five-leg fast lane
are green.

NEXT (moved to PROGRESS.md): implement the resident accepted-seal rebuild
executor handoff, retaining the exact seal re-attestation and producer-absence
fences and preventing any ordinary Verifier launch fall-through.

## 2026-07-18 — resident accepted-seal rebuild execution

The resident now turns the completed D6 carrier into one closed setup action:
after exact former-Worker absence and seal identity re-attestation, it derives
the Worksource parent only from the canonical task-root plan bind, mounts the
seal RO and task-root RO with only its setup children writable, and runs
`__setup-accepted-seal` networkless. The executor result crosses unchanged to
the host record and continuation verbs; refusal retains the envelope and no
Verifier agent is created. Full serial fast lane green.

NEXT (moved to PROGRESS.md): implement the sealed Verifier disposable-source
materialization arm.

## 2026-07-18 — sealed Verifier disposable-source core

`MaterializeVerifierDisposableSource` is the pure setup primitive for the
execution-scoped Verifier tree. It accepts only a canonical sealed commit,
requires the canonical task source clean, refuses a nonempty/symlinked
projection root, and uses the task-local sanitized Git store to read-tree and
checkout only that sealed commit into the projection. It never copies the
canonical worktree or consults a primary checkout. A focused test proves the
sealed tree appears and a late canonical write prevents a second projection.
The mc fast lane is green.

NEXT (moved to PROGRESS.md): wire the sealed materializer through the closed
setup envelope, resident bind, and exact cleanup.

## 2026-07-18 — Verifier projection envelope and executor

The projection core now has its own closed `SetupEnvelope` operation and a
host-scope `mc __setup-verifier-projection` entrypoint. It accepts the exact
accepted-seal identity fields plus only fixed `/repo/task` and
`/repo/projection` destinations; the first-task and accepted-rebuild arms
explicitly reject projection authority. Focused envelope and projection tests,
then `mc/check.sh`, are green. The resident carrier/bind/cleanup remains next.

## 2026-07-18 — sealed Verifier projection delivery and verdict fence

The accepted-seal rebuild receipt now freezes an exclusive Verifier projection
carrier. The resident materializes its run-keyed disposable source only through
the closed networkless setup container, mounts that tree RW at
`/workspace/source`, then covers `.git` and `.mission-control` with the
canonical task controls RO. Reaping stops either setup/agent container and
removes only the matching projection root. The materializer writes the fixed
relative worktree Git pointer, so the covered controls still resolve solely to
the task-local store.

Immediately before `mc verifier verdict` can enter its one-phase terminal
transaction, the wrapper reads the current projection with
`GIT_OPTIONAL_LOCKS=0`, refuses tracked or staged drift, and requires HEAD plus
any asserted SHA to equal the task-pointed accepted seal. A focused clean/dirty
projection test, `mc/check.sh`, and `resident/check.sh` are green. The next
slice adds red-first resident effects coverage for the projection setup/bind
ordering and refusal cleanup.

NEXT (moved to PROGRESS.md): add red-first resident effects coverage for the
sealed Verifier projection setup, bind ordering/control covers, and refusal
cleanup.

## 2026-07-18 — Verifier projection resident effects coverage

Red-first effects tests now prove the setup container is fully labeled and
executes before generic agent creation, then inspect the bind sequence: the
disposable projection overlays `/workspace/source` RW before the canonical
`.git` and `.mission-control` controls cover it RO. A setup refusal retains the
credential-free envelope but recursively removes only the freshly allocated
run-keyed projection tree. Allocation itself is exclusive; an existing root is
refused without an adoption, a Docker call, or a delete. `resident/check.sh`
and the remaining four fast-lane checks are green.

NEXT (moved to PROGRESS.md): add the D6 Docker-boundary proof for the sealed
Verifier projection.

## 2026-07-18 — Verifier projection carrier type completion

The resident runtime validator and setup envelope already consumed the frozen
seal device/inode/owner tuple, but the TypeScript `MountPlan` carrier had
omitted those three members. The declared carrier now matches the closed
dispatch JSON shape, preventing a future type-checked build from accepting a
producer that omits the runtime authority. The behavioral fast lane remains
green.

## 2026-07-18 — Docker E2E baseline

The tagged Docker walking skeleton is green at `db77926`:
`mise exec -- go test -tags docker_e2e -timeout 15m ./e2e/...`. It confirms
that the active Phase-1 route still crosses the real resident/helper/image
boundary. That fixture deliberately follows its legacy fake-route completion
arm, so it is a regression baseline rather than evidence for the new sealed
Verifier projection; the dedicated D6 setup/bind/control/verdict probe remains
the next acceptance slice.

## 2026-07-18 — sealed Verifier projection Docker boundary

`TestVerifierProjectionDockerBoundary` now uses the real image and fixed
`__setup-verifier-projection` command over a materialized, immutable sealed
task-local store. Its first run found that `git read-tree` attempted to create
the canonical task store's `index.lock` even though that bind is intentionally
RO. The materializer now directs its temporary Git index to the disposable
projection and removes it before agent exposure.

The tagged probe inspects `/workspace` RO, `/workspace/source` RW, and the
canonical `.git`/`.mission-control` RO covers. It then mounts a minimal live
accepted-seal/verifier spine, dirties the disposable README in the image, and
asserts that the real `mc verifier verdict` refuses with the tracked-tree
fence. The full five-leg fast lane and full tagged Docker E2E suite are green.

NEXT (moved to PROGRESS.md): add the D1 deployment-UUID mirror Docker proof.

## 2026-07-18 — deployment-mirror Docker crossing

`TestDeploymentMirrorDockerBoundary` reads the helper's RO
`/mc/home/deployment.uuid` bind, changes the host mirror to a foreign value,
and starts the real resident. The foreign identity reaches the private helper
prepare step but creates no Run, lease, or task mutation; restoring the exact
mirror causes the next timer-driven dispatch to create its Editor Run.

The probe exposed an unrelated but critical release-pin drift: resident control
still advertised schema v5 while the current spine is v10, which made every
real control hello fail before dispatch. The constant and its identity test now
pin v10. The full five-leg fast lane and full Docker E2E suite are green.

NEXT (moved to PROGRESS.md): add the D5 real first-task setup/closure Docker
fixture.

## 2026-07-18 — first-task setup Docker boundary

`TestFirstTaskSetupDockerBoundary` runs the shipped image's closed
`__setup-first-task` operation over the production D5 bind arrangement:
network none, source Worksource RO with the `.mission-control` cover, a 0555
task root, and only its `source` and `git` children writable. The returned
result crosses the durable Worker receipt and host re-attestation gate, which
records the canonical task root, all 15 typed task-plan rows, and the immutable
assignment.

Its first run found that `pack-objects` writes its temporary pack under
`GIT_OBJECT_DIRECTORY`; the prior context pointed that at the intentionally
read-only source object store. Closure extraction now uses the writable
task-local object store as primary and the source objects solely as a
non-persistent alternate. An untagged regression test makes source objects
read-only and confirms the extracted pack lands in the task store. All five
fast-lane legs (the documented resident FD-3 flake passed on its immediate
idle rerun) and the full tagged Docker E2E suite are green.

NEXT (moved to PROGRESS.md): add the D6 accepted-seal rebuild Docker fixture.

## 2026-07-18 — accepted-seal rebuild Docker boundary

`TestAcceptedSealRebuildDockerBoundary` creates a genuine canonical task store,
publishes its immutable completion seal, and accepts that exact Worker receipt
before entering the D6 setup image. The setup container receives only the
sealed pack/index/manifest RO, the 0555 canonical task root with its two RW
children, and the closed envelope. Its result then passes the live Verifier
record and continuation fences, yielding the immutable rebuild receipt and a
free lease.

The real Desktop crossing found two defects hidden by host-only tests. First,
the bind's device/inode are namespace-local, so identity comparison belongs at
the resident's host re-attestation immediately before bind creation; the image
retains complete manifest and pack-byte verification. Second, the envelope had
used the current Verifier setup run where the seal manifest requires the
accepted Worker producer run. The new closed `completion_run_id` carrier keeps
those identities distinct. The five-leg fast lane and full tagged Docker E2E
suite are green.

NEXT (moved to PROGRESS.md): add the D6 sealed Worker completion publisher
Docker fixture.

## 2026-07-18 — sealed Worker completion image boundary

`TestSealedWorkerCompletionDockerBoundary` materializes and records the
canonical task store, arms a live Worker lease, then runs the shipped image as
model uid 10002 with the real run identity, task root, private run-keyed seal
root, and lock-domain spine. The public `mc` dispatcher admits only the sealed
completion grammar to its setuid/setgid wrapper. That wrapper runs at uid/gid
10001, permanently drops its saved ids, and executes only the real completion
terminal with fixed spine and run-identity paths.

The probe proves the immutable seal's publication reaches the accepted Worker
terminal and releases its lease. It separately proves uid 10002 cannot traverse
the 0700 `/mc/private` parent while uid 10001 sees the final manifest mode as
0444. The Docker run found that `pack-objects` first creates its temporary pack
under `GIT_OBJECT_DIRECTORY`; a task-store primary tried to rename across the
private-seal bind. The publisher now uses a disposable staging-local primary
object directory with task objects only as an alternate, and removes that
scratch before the closed seal grammar is inspected.

The current real resident has not yet derived/mounted the run-keyed completion
seal row or made the narrow setuid exception to `no-new-privileges`; that is the
next integration slice. All five fast-lane legs and the full tagged Docker E2E
suite are green.

NEXT (moved to PROGRESS.md): wire the D6 completion-seal root through the
resident Worker plan and mount table.

## 2026-07-18 — completion-seal plan authority and resident mount crossing

The Worker completion root is now an explicit post-claim plan descriptor, not
an effect-supplied host path. Attestation proves the operator-owned
`MC_HOME/seals` parent and the exact run child absent; the resident derives and
creates that child, rechecks it before Docker create and after create, and
binds it only at `/mc/private/completion-seal`. The private helper rejects
parent identity/mode/owner drift and an unexpected or malformed child.

The exception is constrained to this descriptor: it must match the claimed
Worker task, current run, and resident `MC_HOME`; it runs as model uid 10002
with all capabilities dropped, while the normal test-fake compatibility route
does not receive the production seal descriptor. The existing walking-skeleton
Docker path remains green, as does the full tagged suite. This slice exposed
that the legacy route cannot run its broad workspace bind as the final model
uid, so the final-uid/no-new-privileges envelope remains scoped to the
production completion-root carrier pending the production resident E2E.

NEXT (moved to PROGRESS.md): exercise that production Worker carrier through
the real resident and accepted completion fence.

## 2026-07-18 — production Worker completion seal proven through the real resident

The completion-seal Docker line is closed.
`TestProductionWorkerCompletionSealDockerBoundary` dispatches a production
(non-fake, `codex/chatgpt`) Worker
through the live resident timer on the run-keyed completion-seal plan carrier;
the image's setuid publisher reaches the same accepted immutable seal fence the
direct sealed-completion probe proves. No Go dispatch/attest change — the seal
already attaches on a non-fake route; the whole slice is authorization +
image + fixture.

Design B, not A. The seal deliberately never rides the fake compatibility
route (2026-07-18 entry above), so the E2E routes the Worker to a real non-fake
binding and teaches the one shipped adapter to stand in for it. Two fail-closed
allowlists, default-off: the resident's `agentRunnerRoutes` (launch gate) and
the symmetric in-container `MC_AGENT_RUNNER_ROUTES` env the resident passes
through (execution gate). A production deployment leaves both empty and ships
real per-harness adapters (ADR-007).

Driving the full loop caught three gaps the resident's mocked-Docker unit tests
never could: the two adapter gates above, and — a real image defect — `bun`
installed under `/root/.bun` while `/root` is `0700`, so the model uid a
production Worker runs as could not exec it (`exec bun failed: Permission
denied`). `bun` now installs to `/opt/bun`, world-traversable. The fixture
seeds the resident's own spine (new `withHostBindSpine` setup option) with a
materialized store + receipt + assignment so the timer dispatches the sealing
Worker directly; because the task is assigned, `status=worked` is unreachable
via the legacy unsealed bypass and alone proves acceptance. The 0444 manifest
is confirmed inside the Linux namespace (Desktop projects a different host-side
mode). Full tagged suite green; the run-keyed E2E is deterministic across
repeated runs.

Spawned adversarial review (read-only, two allowlists + E2E soundness +
image/uid + invariants): no blocking findings. It confirmed both allowlists
fail-closed in every branch (undefined/empty/malformed all refuse; `includes`
is exact, not substring), `status=worked` is a genuine proxy for the accepted
immutable fence (the assigned-task legacy `--status worked` refusal at
`complete.go:133` leaves `CompleteSealedWorker` the only path to `worked`), the
`/opt/bun` move adds only read/traverse (no write/setuid) and does not touch the
`/mc/private` gate, and no production invariant is weakened (the host-bind spine
and worker→codex/verifier→fake routing are test-only). Two non-blocking
residuals recorded: (a) the fake adapter has no defense-in-depth self-check —
the empty-allowlist default plus operator discipline is the sole barrier against
a mis-shipped prod image sealing fake output; worth a self-assert if a real
adapter image is ever built; (b) a non-fake, seal-bearing route that is NOT
allowlisted precreates an orphaned `seals/<run>` dir before refusing (the
precreate runs before the launch gate — pre-existing ordering, resource-leak
only, no fail-open).

Owed (logged, not fixed): the resident-driven Verifier accepted-seal REBUILD
refuses `accepted-seal rebuild has no canonical task-root bind` — the
seal-consumer mount plan lacks the `/workspace` RO task-root entry the resident
effector requires. The direct rebuild path is already proven; this closes the
resident-effected half.

NEXT (moved to PROGRESS.md): wire the resident-driven Verifier accepted-seal
rebuild, then carry the production-Worker E2E through Verifier→Packager→land.

## 2026-07-18 — the resident-driven accepted-seal rebuild was already wired; the "owed" refusal was a stale premise

Resumed clean: all five fast-lane legs green at `b47a2f5`, previous session
Claude (no §2 takeover review). The NEXT/Parked note claimed the resident-driven
Verifier accepted-seal REBUILD refuses `accepted-seal rebuild has no canonical
task-root bind` because the seal-consumer mount plan lacks the `/workspace` RO
task-root entry the resident effector (`resident/src/effects.ts:286`) requires.

That premise is false at HEAD. Traced the two halves and reproduced the Go
attest output directly (throwaway `TestReproSealConsumerPlan`): for a Verifier
candidate carrying a frozen `SubjectAcceptedCompletionSeal` over a materialized,
receipt-vouched, assigned task skeleton, `attestCandidateMounts` already emits a
plan with `AcceptedSealRebuild` set AND all 15 task-local rows — the first being
`logical_id=task-root`, `/workspace`, RO, source
`<worksource>/.mission-control/tasks/task-<id>`, mode 0555 — exactly the entry
the resident finds and strips to recover the Worksource root. `07615df` ("bind
verifier seal setup to task plan", 14:57) landed the `sealConsumer` derivation
that produces those rows RO; it was authored to feed the later verifier
projection and incidentally closed the rebuild arm's task-root bind. The
production-Worker seal session (`b47a2f5`, 22:58, 8 h later) logged the refusal
as "owed, not fixed" without re-checking against `07615df`; the production
E2E stops at `worked` and never dispatches a Verifier, so the refusal was never
actually observed — only assumed.

The genuine gap `07615df` left was test coverage: no attest-level test locked in
the seal-consumer arm's resident-required plan shape (grep confirmed no
`SubjectAcceptedCompletionSeal`/`AcceptedSealRebuild` reference in
`mountattest_test.go`). Added `TestAttestCandidateMountsSealConsumerCarries
ResidentTaskRootBind` (+ helper `maSealConsumerCandidate`): it asserts no
refusal, `AcceptedSealRebuild` is the arm set (never the projection arm), the
frozen accepted-seal identity is carried, every one of the 15 entries is RO, and
the task-root entry is `/workspace`/RO/mode-0555 with the exact canonical suffix
the resident strips. The resident half was already independently proven
(`effects.test.ts:415` consumes precisely this shape); the two halves connect at
the same entry, which is the closure of "the resident-effected half". No
production Go/TS change — the wiring existed; only its guard test was missing.

`mc/verbs` ran fresh (20.5s) green; the untouched TS legs stay green from
startup.

NEXT (moved to PROGRESS.md): carry the production-Worker E2E through the
resident-driven Verifier accepted-seal rebuild and on through
Verifier→Packager→land.

## 2026-07-18 (cont.) — the live refusal IS real (correcting the prior entry): fake-verifier routing, plus a latent seal-identity crossing

The prior entry's claim that the accepted-seal-rebuild refusal "was never
actually observed — only assumed" is **wrong**, and this entry corrects it.
Drove the production-Worker E2E past `worked` with throwaway instrumentation
(a resident-log/spine probe + an `MC_ATTEST_DEBUG` dump inside
attestCandidateMounts; all reverted). The live resident refuses
`accepted-seal rebuild has no canonical task-root bind` on a loop — three
Verifier runs dispatched, each refused, lease-held then reaped.

Root cause, proven by the attest dump: the E2E routes `verifier | fake | fake`.
A fake route sets `allowLegacyFakeWorkspace=true`, so
`deriveDispatchMountRequests` takes the legacy-workspace branch and returns
ONLY `workspace:source` — never the seal-consumer 15 task rows. The worker
(routed `codex/chatgpt`, non-fake) got all 15 including `task-root`; the
verifier got `entries=1 ids=[workspace:source]` with `rebuildStep=true`. So the
committed plan carried `accepted_seal_rebuild` with no `/workspace` task-root
bind — exactly the resident refusal. The isolated Go guard test from the prior
entry is still valid: it exercises the NON-fake path
(`allowLegacyFakeWorkspace=false`), which real production always uses and which
does carry the task-root entry. The refusal is therefore a test-routing
artifact, not a real-production attest defect — but it IS observed, and the
plan it produces is genuinely incoherent.

Fixed that incoherence (this slice, green):
`TestAttestCandidateMountsFakeVerifierNeverCarriesAcceptedSealRebuild`
(red-first) then gated both downstream setup steps
(`AcceptedSealRebuild`, `VerifierProjection`) on `!allowLegacyFakeWorkspace` in
attestCandidateMounts. A fake-routed verifier over a sealed task now launches as
an ordinary legacy verifier instead of receiving an unsatisfiable step it
refuses every tick. Conservative: it matches the exact condition that produces
the steps' required binds; the non-fake path is unchanged (both sibling guard
tests stay green); the fake walking-skeleton never seals, so nothing else moves.

Second defect found, DEFERRED to the carry-through slice (design work, not
fixed here): the completion seal's device/inode/owner are recorded by the
setuid publisher INSIDE the container (`completionsealpublish.go` `os.Lstat` of
the `/mc/private/completion-seal` bind), so they are namespace-local
(probe: `seal_dev=48 seal_ino=… seal_uid=10001`). The resident's
`recheckAcceptedSeal` (task-skeleton.ts) compares those against the HOST-side
lstat of `MC_HOME/seals/<run>` (host operator uid 501, host device), which on
Docker Desktop differs — so even the non-fake path would refuse at
re-attestation. The direct `TestAcceptedSealRebuildDockerBoundary` dodged this
by publishing the seal host-side (`verbs.SealTaskCompletion` in-process). The
conservative fix direction: `recheckAcceptedSeal` cannot trust the
namespace-local recorded identity; it should verify the locally-derived
canonical path + non-symlink dir + host-operator ownership, and rely on the
in-image immutable manifest/pack-byte verification (the manifest digest is
bound in `completion_seals`) as the integrity guarantee — mirroring how the
task-root crossing already moved identity comparison to the resident's pre-bind
boundary. This weakens a defense-in-depth check, so it needs a logged deviation
(§6) and belongs in its own slice.

Full mc fast lane green (verbs fresh, 19.8s). The production-Worker E2E baseline
passes unchanged (probe reverted).

Third layer, found while probing the carry-through (throwaway, reverted):
routing the E2E verifier to the worker's own non-fake binding (`codex/chatgpt`)
makes routing.Parse fail Inv. 9 (routing.go:119: worker↔verifier must use
decorrelated harness families; the fake lane is exempt). Every dispatch then
returns action `refused` and the resident logs `unknown action "refused";
ignored`, so `worked` is never reached. The carry-through verifier must route to
the OTHER production family — `verifier | claude-sdk | claude` (registry:
chatgpt→codex, claude→claude-sdk, minimax→claude-sdk). The rebuild returns
before agent launch so no adapter is needed for the receipt; the later
VerifierProjection launch needs the worker's fake-adapter stand-in.

NEXT (moved to PROGRESS.md): the carry-through slice — (a) fix the
`recheckAcceptedSeal` seal-identity crossing, (b) route the E2E verifier
non-fake AND decorrelated (`claude-sdk/claude`), (c) assert the resident-driven
rebuild receipt lands; then Verifier→Packager→land. Ordered detail in PROGRESS.

## 2026-07-19 — the carry-through slice: seal custody, the rebuild's empty-root defect, and the host-path/helper-scope crossing beneath it

Three layers of the carry-through, two fixed and one diagnosed.

**(a) `recheckAcceptedSeal` now proves custody, not identity** (690fb08). The
completion seal's device/inode/owner are recorded by the in-container setuid
publisher, so a host lstat can never equal them on Docker Desktop. The resident
now verifies the locally derived `MC_HOME/seals/<run>` path, realpath identity,
a non-symlink directory, and host-operator ownership (`process.getuid()`), and
leaves seal integrity to the immutable manifest/pack digest chain. Deviation
logged. Note the container half had already reached this conclusion:
`RunAcceptedSealRebuildSetup` passes `verifyIdentity=false` and has since the
rebuild-core slice — this only brought the host half into agreement, and the
stale comment there claiming the resident still re-attests is corrected.

**(b) the rebuild refused the only state it exists for** (b31a038). With (a)
fixed, the live loop reached the setup container and died on
`first-task setup child source already holds residue; setup never overwrites`.
A spawned read-only ADR review settled the design question decisively and
against my initial framing: ADR-017:533-537 has the rebuild discard "late branch
moves and dangling objects even if the Worker damaged its local repository after
completion", ADR-016:758-760 rebuild the canonical store "despite late loose
residue", and ADR-017:1208-1211 make a damaged-store fixture a mandatory D6
acceptance criterion. The rebuild is a laundering step over an untrusted
populated store — an empty-root rebuild is the case with no specified purpose.
The executor had borrowed `requireEmptyChild`, which ADR-017:439-441 scopes to
"the first setup action", for an operation ADR-017:694 lists as a distinct peer
already holding RW authority on those children. Fixed with `ReplaceExisting` +
`exactEmptyChild` (removes entries directly beneath the child, never traversing
out, child left exactly as bound). Both prior rebuild tests — unit
(`acceptedsealrebuild_test.go`, rebuilding into a fresh `mkTaskChildren`) and
Docker (`e2e_test.go:552`, "the resident-shaped empty canonical root") — had
constructed an empty target, so the specified path had never once run. The
resident's wiring was correct throughout; only the executor was wrong.

*Mechanism choice, ADR-worthy and not yet formalized:* the ADRs mandate that
clearing happen but never say by what mechanism. In-container replace was taken
as least-deviation (the RW authority already exists for this exact operation, it
needs no new plan-carrier field, no new host mutation step, and reverts to one
flag) over the host descriptor-based exact-empty primitive `recoverTaskSkeleton`
already implements, and over staging-then-swap. If the operator wants that
pinned, it belongs in an ADR under §6 rather than only the code comment it has
now.

**(c) OPEN — host-path record verbs self-delegate into the helper.** With (b)
fixed the setup container now succeeds and the failure moved one layer later:
`accepted-seal setup record refused: source "<ws>" does not exist`.
`cmd/mc/main.go:49` enumerates the verbs that must `runLocal` because they read
host files (`__mount-recheck`, `__setup-first-task`, `__setup-accepted-seal`, …);
`task accepted-seal-record` is not among them, so with `MC_HELPER` set it
self-delegates into the helper container, which carries only spine and home
binds and cannot see the Worksource. `RequireHostScope` does not catch this — it
rejects only run.json (pipeline) identities, and the helper is not one.
`RecordAcceptedSealRebuild` -> `attestAcceptedSealRebuildRoot` ->
`boundary.ResolveSource(workspaceRoot)` then resolves a host path inside a
container. `task setup-record` has the identical shape and the same latent bug;
it has simply never been driven through the resident under Docker (the D5
boundary test records in-process, host-side). Making these verbs `runLocal` is
not open on its own: they need the spine, and host mc deliberately has no spine
path (§11.5, the spine never leaves the lock domain). So the fix is either to
give the helper the Worksource, or — matching the pattern the rest of the
boundary already uses — to split the verb so the host attests the filesystem and
passes device/inode/owner identity to the delegated spine half, which records
identity and never a path.

**Diagnostics that made this findable** (kept, in `mc/e2e`): the resident-log
dump elides idle ticks and collapses consecutive identical lines — a stalled
resident emits two ticks a second and a 500ms failure loop repeated its line 284
times, so the raw 8000-char tail was pure noise and every decision had scrolled
off. The rebuild wait now prints the whole lock row and every Verifier run's
outcome. One diagnostic was tried and reverted: `docker logs` on the helper is
useless for a private-helper refusal, because helper verbs run as `docker exec`
and their stderr never reaches the container log; a comment records that so it
is not retried.

Fast lane green at b31a038 (mc + all four TS legs). The Docker carry-through
E2E is knowingly red at (c) — the red is data per §4, and its exact refusal and
cause are above.

NEXT (moved to PROGRESS.md): close (c), then re-run the carry-through E2E to the
rebuild receipt, then on through Verifier→Packager→land.

## 2026-07-19 (later) — the carry-through's third layer: the setup-record crossing splits

Layer (c) is closed. `TestProductionWorkerCompletionSealDockerBoundary` now
passes end to end, and the deliberate red at HEAD is gone.

**The bug, restated from the fix side.** `mc task setup-record` and `mc task
accepted-seal-record` each did two things one process could not do on Darwin:
read HOST files (the Worksource task root, and the store the setup container
landed in it) and write the spine. Host mc has no spine path (§11.5), so it
self-delegates the whole invocation into the helper — which carries spine and
MC_HOME binds and no Worksource. `boundary.ResolveSource(workspaceRoot)` then
resolved a host path from inside that container and refused on a 500ms loop with
`mc: source "<ws>" does not exist`, churning the Verifier lease.

**Why the other option was rejected outright.** The previous entry left two
candidates: bind the Worksource into the helper, or split the verb. Binding it
in is not merely inelegant, it is unsound — Docker Desktop exposes
namespace-local device/inode values across a bind, so an identity attested
inside the helper could never equal the resident's host registration. That is
the exact crossing defect layer (a) fixed for the accepted seal (690fb08). The
helper must not be an observer of filesystem identity at all.

**The split.** The host frame (`cmd/mc/setup_record_frame.go`) attests
everything filesystem: the canonical Worksource, the fixed non-symlink
mode-0555 operator-owned task root, `crossCheckLandedStore`, and for the first
task the 15-row typed skeleton walk. It then invokes the path-free spine half
(`task setup-record-attested` / `task accepted-seal-record-attested`) with
device/inode/owner and nothing else — through `delegate` when the build
delegates, in-process when it does not, so the two lanes cannot diverge. The
spine half (`verbs/setuprecordsplit.go`) binds that identity to authority it
alone holds: the live run/task lease, `receipt.TaskID == --task`,
`receipt.Root == the attested identity`, and for the rebuild the task-pointed
accepted seal.

**On the new `--task` input.** The host used to derive the task id from the
spine; it now takes it from the resident's argv. That is an input, not an
authority — every spine half refuses unless it equals its live lease's task AND
the identity reproduces a durable receipt, so a wrong or hostile id can only
fail closed. The one property genuinely given up is logged in
IMPLEMENTATION-NOTES (2026-07-19): the spine half no longer independently proves
the landed store exists, because the cross-check is host-side. That matches the
trust boundary `mc task setup-register` already sits on.

`task setup-record` carried the identical latent bug — it had simply never been
driven through the resident under Docker — and is fixed in the same commit.

**Evidence.** A control worktree at `d3471f5^` fails with exactly the documented
refusal loop; at `d3471f5` the test passes in ~4s. The ledger's earlier "~3 min"
was image-build time. Full Docker suite: 7/7 green. Full five-leg fast lane:
green. The in-process compositions kept their names, messages, and behavior, so
the verbs suite passed unchanged throughout.

**A pre-existing flake, measured, not inherited silently.** While confirming the
fix, `TestProductionWorkerCompletionSealDockerBoundary` failed 2 of 10 runs —
always *before* the Worker completes, at `mc dispatch failed: private helper
__dispatch-prepare failed` / `resident control hello timeout`, never in the
rebuild. Measured at the parent commit with the rebuild wait shortened: 1 of 10
flakes the same way. Same population; the split did not cause it, and
`TestWalkingSkeleton` was 10/10. It belongs to the load-sensitive resident
control family already recorded as KNOWN-FAILING (2). Recorded in PROGRESS.md
rather than fixed here.

The slice review was self-performed against the six risk lenses (authority
laundering, lost checks, spine-half reachability, TOCTOU, argv passthrough,
resident side); a spawned reviewer was launched but never returned a verdict, so
no external verdict backs this slice — the next takeover review should treat it
as unreviewed by a second party.

NEXT (moved to PROGRESS.md): carry the E2E on through Verifier→Packager→land.

## 2026-07-19 (later still) — the split's adversarial review

The spawned reviewer that went silent during the slice was re-issued the task
once it reported idle, and returned: **PASS-WITH-MINORS**.

All four majors REFUTED, each with the trace that kills it:
- A hostile `--task` cannot mutate the spine or launder wrong-root evidence, on
  either verb. `RecordFirstTaskSetupClosureAttested` derives the task from
  `runs.subject` of a live standalone Worker AND re-proves the lock row names
  that exact run/subject pair before comparing `receipt.TaskID` and
  `receipt.Root`; the accepted-seal half opens with `liveAcceptedSealRebuildTask`
  plus a `domain.Fence` match. The host frame is stat/read-only, so a wrong
  `--task` costs a refusal and nothing else.
- No lost check: the reviewer traced the union and found every pre-commit check
  present in exactly one of the two halves, no orphan. The 15-row walk and the
  same-path-swap guard both survive; the guard's baseline moved from the durable
  receipt to the host-observed identity, but the spine half then equates that
  identity with the receipt, so the equality is transitive.
- No widened principal set: `RequireHostScope` refuses anything carrying
  `/mc/run.json`, and the reviewer confirmed no agent container binds the Docker
  socket. What remains is the host and the helper — exactly the set that could
  already run the whole verb.
- Two of the new checks are *stricter* than what they replaced:
  `ReadFirstTaskSetup` is run-fenced where the old accepted-seal path was only
  task-keyed, and the accepted-seal half now does its identity comparison,
  seal comparison and insert inside one transaction where the old form used two.

Five minors confirmed. Three needed no code — the dropped `rows` field and the
widened attest→commit TOCTOU window were already logged in
IMPLEMENTATION-NOTES, and the usage line had been corrected in 485a7f2, which
the reviewer's d3471f5-only scope could not see. Two were real and are fixed in
ac6e0fa: `validateSetupResult` had drifted behind the root attest in both
in-process compositions (so a call with both a malformed result and an absent
root named the root — making d3471f5's "keep their names, messages and
behavior" claim untrue), and the spine half let `--device`/`--inode` fall
through empty to surface as a receipt mismatch rather than the usage error they
are. The usage line now also names the `-attested` halves it actually
dispatches.

Worth recording for the next reviewer: the one thing this review could not do
was run anything — it read statically, by its task prompt. The two confirmed
minors were both ordering/validation drift invisible to the passing suites,
which is exactly what static reading is good at and what the green lane was
never going to catch.

## 2026-07-19 (later still) — the sealed pipeline reaches `verified`

Two production defects stood between the accepted-seal rebuild and a Verifier
verdict. Both were invisible to every existing test and reachable only by
driving the real resident against real containers.

**Method note, because it is the reusable part.** The E2E's own failure output
could not see either bug: the agent containers are `--rm`, so their logs were
gone before anything could read them. What worked was attaching
`docker logs -f` from a `docker events --filter event=start` stream, so a
short-lived container still left its complete output behind. That capture is
what turned "the Verifier run was reaped" into an exact refusal string. Two
earlier attempts failed and are worth not repeating: polling `docker ps` + `docker
logs` loses the race with removal, and dropping `--rm` changes semantics — the
surviving container trips `requireAcceptedSealProducerAbsent`, which is a
designed fail-closed check, so the run refuses for a different reason.

**Defect 1 — the projection duplicated the canonical source instead of
replacing it.** The attested plan for a sealed Verifier carries the full typed
task table, which already names `/workspace/source` and both covers
(`taskskeleton.go:65-67`). The resident then pushed its projection overlay at
the same three destinations, and Docker refused the create outright with
`Duplicate mount point: /workspace/source`. The containment reading matters
more than the crash: had Docker allowed it, the Verifier would have held the
canonical task store RW — exactly what the disposable projection exists to
prevent. The resident now drops plan rows inside the projected subtree and
leaves everything outside it (`/workspace`, `/workspace/git`) alone.

The resident's unit test could not have caught this: its fixture plan had ONE
entry, the `/workspace` task root. The real plan has fifteen. The test now uses
a representative multi-row plan.

**Defect 2 — the verdict fence resolved the projection by ambient CWD.** The
image sets no `WORKDIR` and the resident passes no `-w`, so an agent container's
CWD is `/`. `fenceVerifierProjectionTree` ran `git diff --quiet` with `dir=""`,
which in `/` fails as "not a repository" — and the fence reported that error as
tracked-tree drift. So it refused a CLEAN projection exactly as it refused a
dirty one, and the sealed verdict was unreachable on every real path. A second
layer sat behind it: `sourceGitEnv()` sets `GIT_CONFIG_NOSYSTEM=1`, which
switches off the image's `git config --system safe.directory '*'`
(`runner/image/Dockerfile:41`), so the operator-owned projection read by the
model uid also tripped Git's dubious-ownership guard.

The fence now takes the fixed projection path from the CLI — the idiom
`mc complete` already uses for its fixed task/seal roots — re-grants
`safe.directory` for that one exact path per command (so the sanitized-config
posture is kept; no system, no global, no wildcard), and names a missing or
unmounted projection distinctly from real drift.

**Why the suites were green through all of it.** The fence's unit test
`os.Chdir`'d into the projection, manufacturing the single CWD production never
has. And every test of this fence — unit and Docker — asserted only that a
DIRTY projection is refused. An always-erroring fence satisfies that assertion
perfectly. The missing direction, "a clean projection is ADMITTED", is the one
that would have failed from the day the fence was written. It is now pinned in
both the unit test and the E2E, along with the absent-path and non-repository
arms.

A one-sided fence test is the generalizable lesson here: for any gate, assert
the admit direction too, or the gate can be broken in the safe direction
forever without a single red test.

**State.** The E2E is extended through the sealed verdict and asserts
`verified_sha` equals the accepted sealed commit, so both fixes have end-to-end
cover rather than unit cover alone. Five-leg fast lane green; Docker suite 7/7.

**Next blocker, already located.** With the Verifier through, the resident
dispatches a Packager, whose scripted `mc complete --status packaged` refuses
with `locking protocol (15)`. Its behavior also still writes its packet to
`/workspace/source/.mc-worktrees/…`, a legacy-workspace path that does not
exist in a sealed task's mount table — so the Packager leg needs both a
diagnosis of the lock refusal and a sealed-appropriate output location.

NEXT (moved to PROGRESS.md): diagnose the Packager's `locking protocol (15)`,
then carry the E2E on through Packager→land.

## 2026-07-19 (correction, same session) — the Packager is not blocked

The previous entry recorded the Packager as refusing with `locking protocol
(15)` and set NEXT to "diagnose that refusal". Both were wrong, and the entry
above should be read with this correction attached.

**`locking protocol` is not a Mission Control domain code.** The string appears
nowhere in the Go source. It is SQLite result code 15, `SQLITE_PROTOCOL` — a
transient locking failure from the spine, surfaced through the CLI's generic
domain-rejection envelope. Reading it as a §18 guard, as the previous entry
did, invented a design blocker that does not exist.

**The Packager leg works.** Re-running the E2E, a Packager container executed
the same `mc complete --status packaged` and returned exit 0 with
`{"outputs":"/workspace/source/.mc-worktrees/task-7.packet.md","status":
"packaged","task_id":7}`. So the sealed pipeline reaches `packaged` unaided,
and the legacy `--outputs` path the previous entry flagged as unwritable is in
fact accepted on this route. There is no sealed-Packager gap to build.

**A hypothesis, tested and NOT supported.** `SQLITE_PROTOCOL` plus the fact
that this test alone uses `withHostBindSpine()` (a VirtioFS host bind, where
every other e2e test uses a Docker named volume) suggested that SQLite WAL's
shared-memory locking was broken across the bind — which would have explained
both this error and the long-standing `__dispatch-prepare failed` flake in one
root cause. A direct probe refutes it: six concurrent writers, 40 inserts each,
against a WAL database on that same VirtioFS bind mount completed 240/240 with
no error. WAL concurrency over the bind is fine. The shared-spine-locking
theory needs a different mechanism, and the honest state is that the cause of
these intermittent SQLite locking failures is still unknown.

So the intermittent `SQLITE_PROTOCOL` here is one more instance of the
already-recorded KNOWN-FAILING (3) family — spine/helper access failing
transiently under this test's configuration — not a distinct Packager defect.

**Lesson worth keeping:** an unrecognized error string got promoted to a
diagnosis on a single observation. The check that would have caught it costs
one grep — if the message is not in our source, it is not our guard. A second
run would also have caught it; one sample is not a defect.

NEXT (moved to PROGRESS.md): carry the E2E through the packet decision and
land, and treat the intermittent SQLITE_PROTOCOL as part of KNOWN-FAILING (3).

## 2026-07-19 (second correction) — the VirtioFS diagnosis is RIGHT; my refutation was a bad experiment

The correction above claimed the split-kernel/VirtioFS explanation was refuted
by a direct probe. **That claim is withdrawn.** The probe tested the wrong
thing, and a decorrelated review (a spawned agent reading the driver source and
the spec) reached the correct diagnosis independently.

**What the bad probe did.** It ran six concurrent SQLite writers *all inside a
single container* against a WAL database on the VirtioFS bind, got 240/240
clean, and concluded WAL-over-VirtioFS was fine. But every writer was behind
ONE kernel — the VM's. Inv. 24's claim is not about VirtioFS bandwidth or
concurrency; it is that a HOST process and CONTAINER processes on one WAL
database are arbitrated by TWO kernels that cannot see each other's locks. The
probe never involved a host opener, so it could not have reproduced the failure
it claimed to rule out. Refuting a hypothesis requires reproducing its
mechanism, not merely exercising nearby machinery.

**The corrected probe.** Container-side writers plus concurrent host-side
readers on the same bind-mounted WAL database: **13 container-side `Bus error`
crashes in 400 writes**, against 0 in the single-kernel run. SIGBUS on the
mmap'd wal-index is the same family as `SQLITE_PROTOCOL` — the wal-index
handshake failing across the boundary — and it is a *harder* failure than the
error we were chasing.

**The citations were there the whole time.** `specs/mission-control-spec.md:69`
(Inv. 24): SQLite "is only safe when one operating-system kernel arbitrates its
locks, so the spine lives on a runtime-local named volume … never on a shared
host path … making the spine reachable to the host as a plain local file would
split it across two kernels and corrupt it." And
`docs/phase1b-contract.md:30` names the mechanism outright: "Host processes
must not open it (also technically unsound: WAL across the VirtioFS/VM kernel
boundary)."

`withHostBindSpine()` (`e2e_test.go:1198-1212`) puts the spine on a host bind,
and `acceptedSealRebuildReceipt` (`:1483-1499`) then opens it host-side with
`sql.Open` while the resident and its containers write — polled in a `waitFor`
loop. That is the forbidden configuration, in the shipped test, by
construction. Every other e2e test uses a named volume, which is why
`TestWalkingSkeleton` is 10/10 while this one flakes.

So KNOWN-FAILING (3) is no longer "root cause unknown". The cause is the
fixture's spine placement violating Inv. 24, and the fix is to return the
sealed E2E's spine to a named volume — seeding production task state *through
the helper* (`docker exec mc …`) instead of host-side through the verbs
package. That is a real slice: the current seeding calls
`MaterializeFirstTaskStore`, `RegisterFirstTaskSetup`,
`RecordFirstTaskSetupClosure` and raw SQL against a host-opened spine.

**Landed now, because it is what cost the cycle** (`classify`, `verbs.go:82`):
a SQLite driver error was being reported to agents and operators as
`domain-rejection` — indistinguishable from "the spine refused your request on
the merits". Driver faults now carry `spine-fault` instead. The message is
unchanged; only its classification is honest.

**Two lessons, both mine.** First: an unrecognized error string was promoted to
a diagnosis on one observation — one grep would have shown it is not our text.
Second, and worse: the correction to that mistake was itself asserted from an
experiment that did not test the stated mechanism. A refutation deserves at
least the scrutiny of the claim it overturns; "I ran a probe" is not evidence
unless the probe reproduces the mechanism.

NEXT (moved to PROGRESS.md): move the sealed E2E's spine to a named volume
(seed through the helper), which should also close KNOWN-FAILING (3); then
carry the E2E through the packet decision and land.

## 2026-07-19 (third correction) — two flake modes, and I attributed one's cause to both

The entry above declared "ROOT CAUSE FOUND" for KNOWN-FAILING (3) and pinned it
on the fixture's spine placement. **That over-claimed.** Measured after removing
the offending access: 2 failures in 12 runs, against 2 in 10 before —
statistically identical. The spine placement is not what makes this test flake.

**There are two distinct failure modes under this fixture, and I merged them.**

*(a) The dominant one, and the only one that ever appeared in a measured flake
run:* `mc dispatch failed (exit 1): mc: private helper __dispatch-prepare
failed`, accompanied by `resident control hello timeout` and `tick failed:
Failed to connect`. That is the AF_UNIX fd-3 resident control crossing — the
mechanism KNOWN-FAILING (2) already describes — and it remains **unexplained**.
Every flake measured (2/10 at HEAD, 1/10 at the parent commit, 2/12 after the
change) was this shape.

*(b) A single observed `SQLITE_PROTOCOL` from a Packager's spine write.* The
two-kernel mechanism behind it is real and independently demonstrated
(container writers + concurrent host readers on the VirtioFS bind: 13 `Bus
error` crashes per 400 writes, versus 0 single-kernel). But one observation of
(b) does not make it the cause of the (a) population.

**The change is kept anyway, on its own merits, not as a flake fix.** The E2E no
longer opens the host-bound spine with `sql.Open` alongside the running
resident — an access Inv. 24 forbids outright (`spec:69`) and
`phase1b-contract:30` names as unsound. The substitute is exact rather than
weaker: `ContinueAcceptedSealRebuild` refuses without the durable receipt, so a
Verifier run carrying the `accepted-seal-rebuilt` outcome proves the receipt
exists, and that outcome is readable through the lock domain via `mc run list`.
Removing a known-unsound access is correct regardless of what it does to the
flake rate; crediting it with the cure was the error.

So KNOWN-FAILING (3) returns to **root cause unknown**, with mode (b) now
eliminated by construction and mode (a) still open, owned by whoever next
touches the resident control crossing.

**The pattern, three times in one session.** Each mistake had the same shape:
one observation promoted to a cause. The unrecognized error string read as our
guard; the probe that did not test its own mechanism; and now a fix credited
with an improvement it never produced. The discipline that catches all three is
identical and cheap — state the population, measure before and after, and say
"unexplained" when the numbers do not move.

## 2026-07-19 — Packager arm and packet-output claims, checked

A follow-up review checked two claims from the earlier Packager report.

**The Packager's missing production mount arm: real, but LATENT.**
`mountattest.go:267-278` health-refuses every repo-Worksource role except the
standalone-task Worker and the seal-consuming Verifier — and that function's own
doc comment (`:233-236`) already names "the sealed views Packager/Refiner read"
as deliberately unbuilt. The refusal is gated on repo Worksources only
(`:254-256` returns early otherwise), so the earlier "every production-lane
role" phrasing was too broad. It does not bite today because the production-seal
E2E routes `| packager | fake | fake |` (`e2e_test.go:943`), so the Packager
takes the legacy-workspace branch and gets the operator Worksource RW at
`/workspace/source` — never the sealed task store. It bites the moment a
Packager is routed non-fake, which carrying the E2E through Packager→land
requires.

**The packet-output concern: REFUTED.** The E2E's fake Packager writes no file
at all — its behavior (`e2e_test.go:1290-1292`) only passes a string to
`--outputs`, and nothing on the `mc complete` path stats it
(`complete.go:215-230` is a bare `UPDATE runs SET output_path`; `domain.Birth`
stores it as `render_path`). Even had it written, `mc-land`'s `task_untracked`
check runs inside the registered task worktree discovered from git
(`mc-land:430-435`), and the `.mc-worktrees/task-N.packet.md` convention is a
deliberate SIBLING of that directory (`e2e_test.go:1281-1284`) precisely so it
is never seen. So there is no pending land refusal and no worktree dirt.
`/workspace/artifacts` is indeed absent from the 15-row task table
(`taskskeleton.go:63-79`) — but correctly so: artifact roots are a separate
request family from the profile's `ArtifactRoots` (`mountattest.go:216-222`),
currently empty because `mc init` never sets that column.

NEXT (moved to PROGRESS.md): give the Packager a production mount arm — most
likely artifact-root-only, since it mutates no repository state — then carry the
E2E through the packet decision and land.

## 2026-07-19 (fourth correction) — the flake was never one failure, and never fd-3

A decorrelated static read demolished the framing I had been carrying, and a
three-line diagnostic change then produced the data to settle it.

**`__dispatch-prepare failed` is not the fd-3 crossing.** `brokerDispatch`
(`resident_control.go:46-61`) is strictly ordered: adopt fd 3 → resolve the
deployment → `exchangeControlHello` → `brokerPrepareCommit`. The string
"private helper __dispatch-prepare failed" is produced inside
`brokerPrepareCommit`, so **reaching it requires the hello to have already
succeeded**. Every time PROGRESS said "every measured flake is the AF_UNIX fd-3
control crossing", the evidence said the opposite: the fd-3 handshake worked and
the `docker exec` into the warm helper is what failed. Two different crossings.

The two log lines are also two different events in two different ticks —
`mc dispatch failed (exit 1)` is a child exit (`tick-loop.ts:55`), while
`tick failed: resident control hello timeout` is an exception before any exit
code (`tick-loop.ts:35`). They cannot co-occur in one tick, so reporting them
as one population was wrong.

**And KNOWN-FAILING (2)'s mechanism cannot be KNOWN-FAILING (3)'s.** KF(2) is a
Bun unit test whose child exits immediately with `waitForAck=false`; PROGRESS
itself records that "production mc waits for the ack, so the immediate-exit
shape is test-only". Production blocks in `readCanonicalControlFrame`, so the
reap-vs-poller race has no window. I had imported a neighbouring entry's
mechanism as if it were evidence.

**Why it stayed unknown for so long: the evidence was being destroyed, twice.**
`private_dispatch.go` set `cmd.Stderr = io.Discard` and collapsed every nonzero
exit into a bare "private helper <verb> failed"; the helper then printed
"private prepare refused" without its own error. The fixture's own comment
("diagnosable only by reproducing the prepare host-side") was describing a
self-inflicted wound. Both layers now report (36604f7).

**The payoff was immediate.** Running to reproduction, the same flake now names
two DISTINCT causes that were previously one opaque string:

- `exit status 124` — the container-side absolute deadline expiring.
  `private_dispatch.go:201` fixes that deadline BEFORE `exec.CommandContext`
  starts Docker, and the code's own comment says the consequence: a slow
  `docker exec` startup leaves the helper only the remainder, "or exits 124
  immediately when the deadline has already passed". `privateHelperSelfTimeout`
  is 4s. So this is a fixed budget sized for an idle machine, not a race.
- `exit status 1: mc: private prepare refused` — a real prepare refusal, whose
  reason is now visible for the first time.

**What is now known vs still open.** Known: the dominant symptom is downstream
of a successful hello; at least two distinct causes exist; one of them is the 4s
budget. Still open: the split between them, and what produces
`tick failed: Failed to connect` (from `Bun.connect`, before the hello window
opens — a likely third mode, and the only one that is genuinely fd-3-adjacent).
A genuine fd-3 defect is NOT ruled out; only the specific mechanism cited for it
is.

**Also still present, and I had thought otherwise:** removing the host-side
`sql.Open` did not remove the VirtioFS spine. `e2e_test.go:1199-1201` still puts
the spine file on a host bind and `:1215` still binds it into the helper, so
every helper spine read — and `__dispatch-prepare` is spine-read-heavy, building
the dispatch mount projection — still crosses VirtioFS. That is a *latency*
effect, entirely separate from the *correctness* (two-kernel WAL) effect that
was removed, and it plausibly feeds the 4s deadline. It is also the variable
that still distinguishes this test from `TestWalkingSkeleton`, which uses a
named volume and is 10/10. The controlled experiment that discriminates latency
from an fd-3 defect is to move this test's spine to a named volume.

Ruled out by reading: tick overlap. `tick-loop.ts:22,29-32` has an in-flight
guard that skips rather than overlaps, so the resident cannot amplify its own
load.

**Fourth correction in one session, same shape as the other three.** Each time,
a single observation or a borrowed mechanism was promoted to a cause. What
finally moved this forward was not a better theory but making the system report
its own reasons — three lines that turned a mystery into two named causes. When
a failure is undiagnosable, fixing the diagnosis is the work; theorising is not.

NEXT (moved to PROGRESS.md): move this test's spine to a named volume (seed
through the helper) as the controlled experiment on the 4s-deadline hypothesis;
then the Packager production mount arm, then the packet decision and land.

## 2026-07-19 (fifth correction) — the flake was corruption, and Phase 0 had already told us

The named-volume experiment ran as a controlled A/B on one idle machine and then
interleaved under 6-way CPU load. It refuted its own hypothesis and produced the
cause.

**Latency is refuted.** Passing runs take ~5s in BOTH arms. There is no
steady-state VirtioFS penalty for the 4s helper deadline to absorb, so the
"spine-read-heavy `__dispatch-prepare` crossing VirtioFS feeds the deadline"
story — carried in PROGRESS since the fourth correction — is wrong. I nearly
believed the opposite: the first named-volume run passed in 4.5s and I called
that implausible for three container round-trips. Running the control is what
corrected me — the old arm passes in ~5s too. **The pass duration was never the
signal; only the failures were.**

**The cause is corruption.** With the spine on a host bind, several containers
(helper, agent, setup) open one SQLite database across VirtioFS, where WAL/shm
coordination is unsound. Two signatures, both only ever on the bind arm:

- `sql: Scan error on column index 2, name "subject": converting driver.Value
  type string ("ws-e2e") to a int64` — a **misaligned row read**. Column index 2
  of `SELECT role, tier, subject, ended_at FROM runs` is `subject`; a worksource
  string surfaced there. No writer can produce that row: the only production
  INSERT (`domain/lease.go:90`) is correctly ordered, and I checked it before
  suspecting the storage.
- `database disk image is malformed (11)` — SQLITE_CORRUPT outright.

That first signature is why this cost so much. A torn read **presents as a
domain refusal**, so every previous session read it as logic and went hunting
fd-3, tick overlap, and deadline budgets. 36604f7 (make failures report their
own cause) is what finally made it legible — the diagnostic change paid for
itself one session later.

| arm | runs | failures |
| --- | --- | --- |
| named volume | 42 (10 idle + 12 interleaved under load + 20 extended) | 0 |
| host bind | 22 (10 idle + 12 interleaved under load) | 2, both the above |

**a3928f1 was half a fix.** Removing the host-side `sql.Open` removed one
kernel, but not the sharing. One kernel was never the requirement — a filesystem
with correct shared-memory and locking semantics is. Inv. 24's lock domain was
being violated BY THE FIXTURE, which abandoned the named volume purely so the
test could seed host-side. That convenience cost four corrections.

**Seeding did not actually need the bind.** The fixture now builds and seeds the
spine in a plain temp dir that is never mounted, closes it (checkpointing the WAL
away), and `docker cp`s the closed file into the volume through a never-started
stager before any container opens it. No process ever shares the file. Seeding
logic is unchanged; it moved into a `withSeededSpine` hook.

**The finding that outlives the fix: Phase 0 already proved the guard, and it
was never carried into `mc`.** S5's RESULT.md row 5 is "Fail-closed guard: DB on
a bind-mounted (VirtioFS) path is REJECTED — PASS", with a `/proc/self/mountinfo`
longest-prefix allowlist of block-device-backed local filesystems, running
before `sql.Open` in every subcommand. S5's own note is that a denylist keyed on
"virtiofs" would have ACCEPTED the bind, because Docker Desktop surfaces it as
`fuse.` — so the allowlist shape is load-bearing. `grep -ri mountinfo|virtiofs|
ext4 mc/` returns **nothing**: the guard exists only in the spike. Had it
shipped, the fixture could not have created this spine at all, and none of the
five corrections would have happened. This is not a live production break —
production takes its spine from a named volume via resident config — so it is
defense-in-depth that is absent, not an invariant broken today. It is the
strongest candidate for the next slice.

**Owed, found in passing:** the fixture chmods the volume root 0777 because
Docker materializes a fresh volume as root:root 0755, while the seal path runs
agent containers `--user 10002:10002` and the completion wrapper drops to 10001
— and both write the spine plus its `-wal`/`-shm` siblings. Nothing outside the
spikes creates the spine volume, so **production's spine-volume ownership is
unspecified**; it belongs to the install.sh/onboarding deliverable (spec §17).

**What is now closed vs still open.** Closed: KNOWN-FAILING (3), and with it the
"every measured flake is the fd-3 crossing" framing. Open and NOT claimed fixed:
the `exit status 124` mode and `tick failed: Failed to connect` were not
observed in 42 post-fix runs, but 42 runs is weak evidence for modes that were
always rare. If either recurs it is a separate defect, on a spine that is no
longer corrupting.

**Five corrections, one shape.** Every one promoted a single observation to a
cause. What ended it was not a better theory but two mechanical habits: making
the system report its own reasons, and **running the control**. The experiment
that settles a question is worth more than the theory that motivated it — this
one refuted its own hypothesis and was still the most valuable thing in the
session.

NEXT (moved to PROGRESS.md): carry S5's fail-closed bind-mount spine guard into
`mc`; then the Packager production mount arm, then the packet decision and land.

## 2026-07-20 (Claude) — the lock-domain guard, and what it caught on arrival

**Carried S5's guard into `mc` (`substrate/lockdomain*.go`).** Phase 0 proved it
and it was never implemented. `substrate.Open` and the onboard read-only
inspection — the only two spine opens in the codebase — now refuse before
`sql.Open` unless the spine's directory sits on a block-device-backed local
filesystem.

**The allowlist shape earned its keep immediately.** On its first Docker run the
guard refused two probes with the exact string S5 predicted:
`fstype=fakeowner source=/run/host_mark/private`. Not `virtiofs`. A denylist
keyed on the obvious name would have accepted both.

**Structure: pure decision, platform-split entry point.** The mountinfo parse and
the accept/refuse rule are platform-neutral, so the darwin fast lane proves them
against captured mountinfo text (13 cases); only the `/proc/self/mountinfo` read
is behind `//go:build linux`. Off Linux the guard accepts, and that is scope, not
weakening: per Inv. 24/§11.5 a native darwin process cannot open the spine at
all. `lockdomain_other.go` records the argument and warns off the env-var escape
hatch a future session will be tempted by. `check.sh` gained a `GOOS=linux` vet,
because the guard's only production implementation now lives on a platform the
fast lane otherwise never compiles — the invisible-rot shape the tagged vets
already guard against.

**What it caught: two E2E probes still outside the lock domain.**
`TestVerifierProjectionDockerBoundary` and
`TestSealedWorkerCompletionDockerBoundary` both bound a host directory at
`/mc/spine`. The second was worse — it held the database open on the darwin
host across that bind WHILE a container wrote it. The precise two-kernel
sharing Inv. 24 forbids, inside the suite meant to be proving the boundary.
`a3928f1` removed one kernel from the shared fixture; these two were never
touched. Both now use a per-probe named volume, and the one that asserts on
what the container committed copies the database back OUT instead of sharing
it.

**The probe found a hole in the guard that the fixtures never would have.**
Writing `TestSpineLockDomainGuardDockerBoundary` — which pins BOTH directions
against a real container's real `/proc/self/mountinfo` — surfaced that the
guard judged only the spine's DIRECTORY. A single file bound over `spine.db`
*inside* a legitimate named volume therefore passed: the directory reports
ext4 while the database itself is on VirtioFS. Now both paths are checked. The
unit test first asserts the directory alone passes, so the case cannot quietly
stop being meaningful.

**Why both arms of that probe exist.** Every other Docker test proves
ACCEPTANCE implicitly — they would all fail if the guard refused a named
volume. So acceptance can keep passing indefinitely while the refusal rots into
a no-op, and nothing goes red. That is exactly how the sealed verdict became
unreachable while both suites stayed green (6657541): every test of that fence
asserted only that a DIRTY projection is refused, never that a clean one is
admitted. The lesson generalizes — a fence needs both directions pinned, and
the direction the rest of the suite exercises for free is the one that needs
the explicit test.

Full Docker suite 8/8; five-leg fast lane green.

NEXT (moved to PROGRESS.md): the Packager production mount arm, then carry the
E2E through the packet decision and land.

### Same session — the review of the guard

Spawned adversarial review of `b1c6187..34fc63b`. It refuted more than it
confirmed, which is the useful outcome: no spine opener in the module bypasses
the guard (it enumerated all of them, including the resident's TypeScript,
which never touches sqlite), the darwin no-op is unreachable from any real
spine because `helperSpinePath()` is a fixed constant rather than env-driven,
and the literal-`-`-in-optional-fields, `..`, relative-path, and
not-yet-created-spine attacks all hold up.

Three findings landed:

1. **Major, symlinked spine file.** `GuardLockDomain` resolved symlinks on the
   DIRECTORY and then rejoined the un-resolved basename. So `/mc/spine` could
   be a legitimate ext4 volume while `/mc/spine/spine.db` was a symlink onto a
   bind — longest-prefix saw only the volume, and `sql.Open` followed the link.
   The comment directly above that line claimed to prevent this laundering; it
   did, one path component too high.
2. **Unparseable lines were skipped**, which generates false accepts rather
   than avoiding them: drop the line describing the bind and the longest
   remaining prefix becomes its parent, which on a Linux host is an ext4 root
   that passes. The guard would approve the mount it could not read. One bad
   line now poisons the whole table.
3. **The wiring was unpinned.** The decision function was well covered, but
   deleting the guard call from `substrate.Open` turned no test red — the fast
   lane is darwin, where the platform source is inert. This is the *third*
   appearance of one failure shape in this phase: a check that has stopped
   running looks exactly like a check that is passing. substrate now reads its
   mount table through an unexported package variable so the refusal is proven
   THROUGH `substrate.Open`; `onboard.go`, the one deliberate bypass, gets a
   source-level drift guard in the shape `boundary/typedkind_internal_test.go`
   already established.

**Found separately, before the review returned:** the equal-length tie-break in
`longestPrefixMount` kept the FIRST matching entry. mountinfo is ordered and
mounting twice at the same point shadows rather than replaces, so a bind
stacked directly over the named volume — same path, same length — was judged on
the volume's ext4 line while every read and write went to the bind (6abedb8).
The review independently confirmed it in the reviewed range.

**Worth keeping.** Four of the five real defects in this guard were false
ACCEPTS, and every one of them was a place where the guard looked at something
adjacent to the thing being opened: the directory instead of the file, the
parent instead of the unreadable line, the first mount instead of the
effective one, the link instead of its target. For a fail-closed check, "what
exactly am I judging, and is it the same object the caller will use?" is the
question that finds bugs.

NEXT (unchanged, in PROGRESS.md): the Packager production mount arm.

## 2026-07-20 — the Packager's arm, and the landing path that was never joined

The outgoing `NEXT:` proposed the Packager's production mount arm and guessed
its shape: "likely artifact-root-only — it mutates no repository state". The
guess was reasonable from the spec alone (§57: "From the durable record only",
"Read-only w.r.t. workspace") and wrong. ADR-017 is explicit three times over:
`/workspace/source` and `/workspace/git` are both "inherited through the RO
task-root bind for Packager/Refiner" (:637, :640), and the acceptance text has
them "receive canonical source/control RO and fail representative writes while
their separate record outputs remain writable" (:1218). ADR-016:765 forbids
only a MUTABLE canonical view. "Read-only w.r.t. workspace" is satisfied by an
all-RO bind, not by no bind — the packet embeds repository evidence, and RO is
how it reads it without being able to touch it.

So the arm is the seal-consumer row shape with every row forced RO, gated on
the accepted completion seal per phase3-contract:249 ("no downstream role
starts until the accepted seal exists"), and carrying no setup step of any
kind. The Verifier CONSUMES the seal by rebuilding the store; the Packager only
READS what that rebuild produced. One predicate now names both, and the RO
downgrade keys off the union.

**The absent-root case found a real defect.** Writing the negative test — a
Packager whose task root does not exist — surfaced that
`captureDispatchMountHostSnapshot` chooses precreate-vs-resolve from the task
root's PRESENCE alone. The role is not in that predicate. Attest then strips
the typed rows and keeps the step (`mountattest.go:821`), so a reader arriving
before its store existed would have been handed first-task setup authority and
run a mutating setup container. This was not introduced by the Packager arm: it
was already reachable for a seal-consuming Verifier whose canonical root
vanished. First-task setup and its recovery are Worker-only (ADR-016 D6), now
fenced, with the Worker's own precreate path pinned in the same table test so
the guard cannot over-fence. Same shape as the four false ACCEPTs in the
lock-domain guard the session before: the code was looking at something
adjacent to the thing it was deciding about.

**The E2E, and a control run.** The Packager now routes `claude-sdk/minimax`
— its canonical spec §9.1 route; Inv. 9 decorrelation binds only
strategist↔editor and worker↔verifier (`routing.go:117`), so sharing the
Verifier's family is legal. Its in-container behavior refuses to complete
unless both canonical children are present AND unwritable, which makes the
assertion load-bearing in both directions: a plan that regressed a row to `rw`,
or dropped a child, fails there rather than passing silently. That direction
matters — 6657541 was exactly a fence asserted only in the negative, where a
CLEAN projection was refused like a dirty one for weeks and both suites stayed
green. So the arm was disabled and the E2E re-run as a control: it stalls at
`verified` and times out after 126s never reaching `packaged`. Enabled, it
passes in 5.2s. Full Docker suite 8/8.

**What stopped the slice.** The second half of the outgoing `NEXT:` — "carry
the E2E through the packet decision and land" — turns out not to be reachable,
and the reason is worth stating plainly. The whole landing path is still the
legacy `.mc-worktrees` model: `mc-land` merges a ref that must ALREADY exist in
the real repo (`mc-land:278-295`), the resident binds one mount (the real repo
root RW), and a sealed task's reviewed commit lives only in its task-local bare
store. ADR-017:1226-1240 specifies the replacement and nothing implements it —
its four typed mount kinds have zero producers.

Worse, and separately: a sealed task never reaches landing-pending at all.
`LandingPending()` requires `tasks.branch != ""`, and `tasks.branch` has exactly
one writer, reachable only through the `--status worked --branch` terminal that
`complete.go:128-134` closes to assigned tasks by design. The sealed branch
lives in `task_assignments.branch` — a different table `LandingPending()` never
reads. So `domain.Approve` sees a branchless task, classifies it as an
artifact-plane deliverable, and archives it synchronously. The operator
approves a merge, the task disappears, main is never touched, and nothing
errors. The seal pipeline introduced a second home for the branch and no
document reconciled the two.

The E2E therefore stops at `packaged` deliberately. Driving `packet decide
--approve` today would encode the silent archive as expected behavior and make
the real fix a test-breaking change.

NEXT (in PROGRESS.md): sealed landing, starting red-first with the
`LandingPending`/`tasks.branch` hole.

## 2026-07-20 (later) — closing the silent archive, red-first

Step 1 of the sealed-landing NEXT:, taken immediately because it is small and
the hole is live. `domain.Approve` classified a task by `tasks.branch`: branch
present ⇒ hold for landing, branch absent ⇒ artifact-plane deliverable, archive
synchronously. A sealed task is branchless in `tasks` BY CONSTRUCTION — its
branch lives in `task_assignments.branch`, because the only writer of
`tasks.branch` is the `--status worked --branch` terminal that D6 deliberately
closes to assigned tasks. So every sealed task took the archive arm.

The red test made the failure mode legible in one line: `want DomainError
"landing-fence", got commit`. "Got commit" IS the bug — the transaction
succeeded, the task archived, and nothing anywhere errored.

The fence refuses an assigned task at approve with `landing-fence`, naming
ADR-017:1226-1240 as what is missing. Nothing is stranded: the seal, the packet
and the task all survive for the landing slice. Deliberately NOT chosen: the
alternative repair of projecting `task_assignments.branch` into `tasks.branch`
at acceptance, which would make the task landing-pending and let `mc-land` fail
it into `blocked`. That is louder still, but it asserts a ref exists in the
real repo when it does not — fabricating state to get a better error message.
The refusal fabricates nothing and is a one-line revert once landing exists.

The fence keys on the assignment, not the status, so legacy Phase-2 branchless
rows keep their original synchronous archive. That direction has its own test:
the failure this whole session kept finding is a check that stopped running or
started running too widely, and a fence with only its positive arm pinned is
the same hazard wearing different clothes.

NEXT (in PROGRESS.md): the design question this forces — project the sealed
branch at acceptance, or teach `LandingPending()` to read the assignment — and
then sealed landing itself.

## 2026-07-20 (Claude) — the branch-home question, answered; the fence, widened

The outgoing `NEXT:` left one design question gating the whole sealed landing
slice: project `task_assignments.branch` into `tasks.branch` at acceptance, or
teach the landing predicate to read the assignment. A four-reader fan-out
mapped ADR-017's landing design, the legacy `.mc-worktrees` path, the
branch/`LandingPending`/`Approve` data surface, and the four producerless typed
mount kinds, then synthesized a recommendation.

**Answer: read the assignment; do not project.** The previous session had
already rejected projection on the grounds that it fabricates state — asserting
a ref exists in the real repo when it does not. The mapping adds the sharper
reason, which is reversibility. `tasks.branch != ""` is not a name, it is an
assertion that a mergeable ref exists in the real Worksource. Projecting arms
`LandingPending()`, which emits the frozen four-scalar `KindLand`, which routes
to legacy `mc-land`, which hard-fails `missing branch` because a sealed task's
commit lives only in the task-local bare store — and that failure writes
`blocked_reason` on the task. So projection converts "unimplemented" into a
durable task-level block wearing a Git-sounding reason, and unwinding it needs
a backfill rather than a revert. AGENTS.md §6(c) decides it. Reading costs one
LEFT JOIN whose revert is a deletion.

Projection also does not buy the join it appears to buy: a sealed landing needs
a different container, mount table, and envelope, so the lane selector must
consult `task_assignments` regardless. Projection adds a denormalization and
keeps the join.

**The plan's step order was wrong in one place, and it is the same hazard this
phase keeps producing.** The synthesis put "`Approve` holds a sealed task
instead of refusing" second. But until dispatch is wired — nine steps later — a
held task sits `approved`/`packaged` with nothing that can ever merge it. That
is the Inv. 25 hole from 2026-07-20 rebuilt one layer up: the operator approves,
nothing errors, nothing merges, and this time the task does not even vanish to
make it noticeable. The loud refusal is strictly safer than that intermediate,
so the relaxation moves to sit beside the dispatch wiring and everything built
before it is inert by construction.

Two steps landed.

**The approve landing fence now covers both branch homes (schema v11).** The §7
fence keys on `NEW.branch`, so it skipped exactly the rows the sealed lane will
consume — the substrate would accept an approved sealed task with no verified
SHA at all. `domain.Approve` refuses it in Go, but the substrate is the layer
that has to hold when a new writer reaches the column another way, and this
slice adds writers. Either home now arms the fence; a row with neither is an
artifact-plane deliverable with no landing facts to require, pinned by its own
case so the fence cannot over-reach onto the Phase-2 rows.

Writing that test found all three negative cases aborting on the paired
`decision`/`decided_at` CHECK rather than on the fence — they would have passed
against the unwidened trigger. They now set both columns and assert the abort
REASON. This is the third time this phase that a test asserted only "refused"
and would have stayed green through the defect; `wantAbort` without a reason is
now a known smell here.

The resident's duplicated `SPINE_SCHEMA_VERSION` moved to 11 with it. That pin
is a hand-maintained cross-language literal, it has rotted before (v5 against a
v10 spine, caught only in Docker), and the fast lane still cannot catch it.
Worth deriving at some point; logged, not fixed.

**Dispatch reads the second branch home, inert.** `dispatch.Task` gains
`Sealed *SealedAssignment`, `loadRecords` LEFT JOINs `task_assignments`
(task-keyed PK, so no fan-out), and `SealedLandingPending` is the sealed twin
of `LandingPending`. Two non-conjuncts are deliberate: a row carrying BOTH
homes is refused by both predicates rather than served down the wrong lane
(that structural exclusivity is what lets one `KindLand` step serve both, so it
is asserted rather than assumed); and an assignment whose frozen `target_ref`
has drifted from the task's current one STAYS in the lane, so landing refuses
it loudly instead of it becoming silently unlandable forever.

NEXT (in PROGRESS.md): the landing mount plan — destination grammar, the four
typed kinds' producers, the envelope arm, the carrier — then the lander, then
the wiring that turns the lane on.

## 2026-07-20 — the landing lane's mount grammar and typed roots (steps 1-2)

**The `/repo` plane existed only as a protocol error.** `mountplan.go`'s typed
arm asked one question — `validTaskPlanDestination` — and every ADR-017 landing
cell answered no, which surfaced as `Domainf("... outside the closed D6 task
table")`. That is a confused-planner diagnosis, correct for a broker inventing a
destination and wrong for a class the ADR defines. Nothing downstream of the
grammar could be written until it knew two tables.

It now knows two, and they are pinned as a PARTITION rather than a union. That
distinction earns its test: `dispatchprivate.go`'s task-precreate fabrication
guard keys off `validTaskPlanDestination` ALONE (a precreate plan may not
fabricate a not-yet-existing task mount row), so a landing cell drifting into
the task grammar would silently widen that guard without touching its own code.

**The four-row table produces two roots.** The outgoing `NEXT:` planned
producers for all four typed kinds. Re-reading the ADR against the resident
killed that: :700 calls `/repo/source/.mission-control` a GENERATED empty
directory, the same word :692 uses for the setup class's cover — and
`effects.ts:576` shows what "generated" means operationally, a per-run
`<run-id>.setup-cover` the resident mkdirs immediately before launch. Dispatch
attest captures device/inode/owner evidence and creates nothing; it runs before
the resident. So the cover has no identity to capture, exactly like
`/mc/landing.json`. Both rows are marked `ResidentMaterialized` — the table
stays ADR-017's faithful four — and excluded from the bindable grammar.

The consequence is worth writing down because it is a real hole with a named
owner, not a tidy factoring. Because the cover is not a plan entry, the PLAN
cannot express that the sealed bytes are unreachable through the RW
`/repo/source` alias (:700). The plan authorizes the grant; only the realized
mount table establishes containment. This weakens nothing that exists — the
RO-alias property was already a Docker-lane obligation for precisely the reason
that a plan-level `:ro` assertion cannot prove it — but it moves the resident's
obligation to PLACE the cover onto the landing instruction, where the helper
boundary can validate it. Step 3 must carry it. A landing container that runs
without the cover hands the sealed root out RW through the source alias.

**`/repo/source` is the only RW grant of a real repository in the system**
(:699, "intentionally including its primary checkout"), which is why the setup
class's identically-spelled RO row (:691) cannot share a table with it — the
separation at :686-687 is load-bearing exactly here. So both landing anchors
must be unaliased operator-owned directories at their own exact canonical path:
an aliased Worksource would place the strongest grant in the system somewhere
the operator never registered. The sealed root additionally keeps its 0555
shape fence (:418).

**Inertness is asserted by a PAIR of tests that must both hold.** One proves a
landing cell denies (`denied_root`) through the ordinary jurisdiction, because
no landing kind enters `TypedRoots` anywhere in production; the other proves it
plans once the producer supplies its roots. Either alone is satisfiable by a
lane in the wrong state. Nothing is wired into `dispatchMountHostSnapshot`, so
the jurisdiction snapshot digest — keyed off `kind.String()` at
`mountattest.go:161`, and the thing the outgoing `NEXT:` warned would move —
has not moved yet. It moves when step 5 turns the lane on, and that is the
commit that must pin it.

NEXT (in PROGRESS.md): step 3, the landing envelope arm and the `Landing`
carrier field — carrying the cover obligation named above — then the lander,
then the wiring.

**The review reversed it.** The slice above was audited by a spawned
adversarial reviewer against ADR-017 and the resident, and its deciding
criterion did not survive. The slice asked "does a host identity exist at
attest time?"; the question that governs is "is the `/repo` plane
plan-addressable at all?" — and `resident/src/effects.ts:90-95` has refused
every plan entry outside `/workspace` since Phase 1b. The D5 plan is an
AGENT-plane carrier. Every `/repo` row of the sibling setup class is composed
by the resident from paths it derives itself. Landing produces ZERO plan
cells, not two, and both seam widenings are reverted (55c2949).

The widening bought no capability — the resident would have refused the spawn
the moment step 5 turned the lane on — and cost two guards, because both
predicates gate more than their names suggest. `validatePrivateMountPlan`
began accepting plans mixing agent-table `/workspace` rows with landing
`/repo` rows, which ADR-017:686-687 forbids and nothing else enforces; and the
task-precreate fabrication guard, keyed off `validTaskPlanDestination` alone,
began admitting a precreate plan carrying RW to the real Worksource checkout.
Both were live on the HELPER BOUNDARY — a re-validator of incoming plans, not
a producer — so the slice was not as inert as its own tests asserted. Its
inertness pair tested the planner and stopped there.

Two lessons worth carrying forward. Widening a predicate that several guards
share is never a local change; the diff touched one line and moved two fences
it never named. And "the producer is inert" says nothing about a validator:
inertness has to be asserted at every seam that reads the predicate, not at
the one the slice was thinking about.

The table and the host-anchor resolver survive, repurposed as the landing
CLASS's grammar for validating the landing INSTRUCTION — the division
`/mc/setup.json` has had since D5. `Generated` replaces `ResidentMaterialized`
(ADR-017's own word at :700, :702). The review also confirmed the parts that
held: ADR fidelity of all four rows, planner inertness, the unmoved
jurisdiction digest, and it refuted the case-insensitive-volume and bind-mount
attacks (authorization compares inodes via `SameFile`, so a case-variant
spelling still matches).

Four resolver defects came with it, all now mutation-verified. The RW anchor
had NO shape fence — it now refuses group/world-writable, though not an exact
mode, since a real repository is commonly 0755 — and no check that it is a
"real Git Worksource root" (:699), so landing would have imported a closure
and created a ref in a non-repository. The task-root arm's ownership and kind
fences were unreachable, because a foreign uid tripped the Worksource arm
first. And the canonical-ancestry fence was VACUOUS: mutating it to `if false`
left the suite green, because the symlink checks caught every shape it was
meant to refuse. That is the fourth fence this phase found passing for the
wrong reason, after 6657541, 83ed9e9 and the v11 trigger. The smell is stable
enough to name: a negative test whose scenario is reachable by an EARLIER
fence proves nothing about the fence it is written under, and only mutation
finds it.

NEXT (in PROGRESS.md): step 3, the landing envelope arm and the landing
instruction on the carrier — which is where the cover obligation and the
resolved anchors both belong — then the lander, then the wiring.

## 2026-07-20 — step 3 lands, and the legacy lander's corpus is triaged for step 4

Step 3 is in (`28c5df5`): the `sealed-landing` envelope arm and the `Landing`
instruction on the carrier, both inert. The shape follows the reversal's
lesson literally — nothing here teaches the plan grammar `/repo`. The envelope
restates the container destinations the RESIDENT will bind, and the plan
instruction carries only the two HOST-backed anchors plus the cover
obligation. Destinations are derived from `landingMountRows()` through named
constants rather than copied literals, so the table stays the single source.

Three things worth keeping:

**Landing refuses a run id.** It holds no lease and opens no Run (§7;
`LandReport` takes no run), so the envelope preamble's combined "names no live
run/task" check had to split — TaskID required of every arm, RunID of every arm
but landing, which refuses it outright. This is the bleed direction that hides:
all three setup arms legitimately carry a run, so a landing envelope quietly
carrying one reads as ordinary. The landing-only fields are refused by every
setup arm from ONE hoisted preamble check, so a fifth arm inherits the refusal
instead of forgetting it — the failure mode of per-arm bleed lists is that the
newest arm is the one nobody updates.

**Two of ADR-017:702's nine facts have no realizable form yet**, both logged.
"Expected Git topology" is carried structurally (the branch, the
verified/pre-merge/base SHAs, the closure digest, the repo UUID) because the
ADR specifies no serialization and an opaque blob would add a parser at the
most dangerous boundary in the system without adding a fence. "Cleanup path" is
absent because ADR-017:757-759 defers cleanup to a LATER trusted action after
the spine records success — carrying it would be authority with no consumer.

**A shared-predicate asymmetry was found and deliberately NOT fixed.** The
envelope's `decimalIdentity` refuses a leading zero; the helper boundary's
`validDecimalText` accepts one. `validDecimalText` also gates task precreate,
the completion seal, the accepted-seal rebuild and the verifier projection, so
tightening it inside this slice is exactly what 55c2949 reversed. Landing
inherits the looser predicate and the gap is pinned by a named test with an
owner. Not a hole at this layer: the resident compares against
`strconv.FormatUint` output, which never emits a leading zero.

### The legacy `mc-land` corpus, triaged (input to step 4)

A read-only scout mapped `runner/image/mc-land.test.ts`'s 38 tests and the
615-line `runner/image/mc-land` script. The corpus is more portable than
expected: **24 of 38 are generic Git-landing mechanics** that carry over
essentially unchanged — the stat-cache/`checkStat` evasions, the
`--assume-unchanged`/`--skip-worktree` index-visibility fences, untracked
collision walks, directory-rename inference not relocating reviewed paths, the
operator-owned-vs-ours `MERGE_HEAD` distinction (abort only what we started),
executable merge-driver and content-filter refusal, `mergeOptions` isolation
including the `main=evil` exact-key case, `core.worktree` redirect refusal,
forged-receipt rejection, and `GIT_NO_REPLACE_OBJECTS`. Those are the tests
worth retargeting first; they encode adversarial Git knowledge that would be
expensive to rediscover.

**10 depend on the legacy shape** — a linked worktree and a branch living in
the SAME repo — and need real rework, not a path swap: everything scoped to the
task worktree's own index/config (`--worktree` filters, `--worktree
core.worktree`), and the whole `mc/task-*` branch-NAMESPACE fence, which exists
only because the branch is caller-supplied and shares the operator's ref
namespace. The sealed shape fences that differently: the branch comes from the
immutable assignment, not from argv.

**Two structural mismatches matter for step 4:**

1. *There is no import.* Legacy never imports objects, because branch and
   worktree already live in the real store; it SYMLINKS `objects/`, `refs/`,
   `worktrees/`, `packed-refs` and `shallow` from the real common dir into a
   temp isolated view (`mc-land:134-143`). ADR-017:743-745 replaces that with an
   explicit verified import — no hardlink, no alternate, no speculative delete —
   plus fsync and verification, none of which exists anywhere today. And legacy
   never CAS-creates a ref at all (:748-750's durable import marker has no
   analogue); its only durable marker is the merge commit. Ref *deletion* is
   already CAS (`update-ref --no-deref -d … "$sha"`, :607) and that idea carries.

2. *Legacy's cleanup ordering violates ADR-017:756-758.* It removes the worktree
   (:596) and deletes the branch (:607) INSIDE the same invocation, BEFORE the
   resident's `mc land report`. The entire `cleanup_debt` apparatus exists to
   paper over that ordering. This independently confirms PROGRESS's instruction
   that step 4 STOPS at the merge — and it means the four cleanup tests (#35-38)
   have an invalidated premise for the sealed shape and must not be ported as
   written.

One more thing the scout surfaced that is not in any ADR: legacy runs a large
number of bare `git` calls OUTSIDE its two isolated wrappers, with the
operator's live config and hooks in scope (`core.worktree` probe, the whole
receipt scan, `symbolic-ref`/`show-ref`, `merge-base`, `diff-tree`, `worktree
list`, and the `cat-file` inside the `xargs` blocks). They are read-only
plumbing so no hook fires today, but the isolation is by accident rather than
by construction. The sealed lander should run EVERY git call through the fenced
wrapper; ADR-017:704-711's "cleared environment plus generated safe Git
configuration" reads as a whole-program property, not a per-command one.

## 2026-07-20 (later) — step 4 prep: the landing target grammar, and a guess caught

Before writing the lander, checking what `target_ref` actually holds turned up
that step 3 had encoded an invention. Step 3 validated the landing target as
merely non-empty and its fixture used `refs/heads/main`. The spine stores
`tasks.target_ref` as free-form text (schema.sql:786) whose real values are
bare names like `main` — and the first-task setup arm resolves it as a REV,
where "HEAD" is a legitimate value. Two arms, two meanings, one field.

Landing cannot inherit that looseness because it constructs
`refs/heads/<target>` in the real operator repository: the qualified form
yields `refs/heads/refs/heads/main`, "HEAD" is meaningless as a merge
destination, and option- or glob-shaped names turn a ref into an argument or a
pattern. `validLandingTargetBranch` is now a closed grammar on both sides of
the crossing, plus a refusal when the target equals the task's own sealed
branch (landing merges that branch INTO the target, so identity would make
ADR-017:748's CAS ref creation create the ref it then merges from).

Written in pure Go rather than shelling to `check-ref-format` because the
helper boundary must not spawn a git process on caller-supplied bytes.

The generalisable point is about WHEN this was caught. Step 3's tests were
green, its adversarial review returned PASS, and neither noticed — because
every one of them was written against the same fixture that carried the
invention. A fixture is not evidence: a test suite can only disagree with the
code, never with a shared premise both sides inherited. What caught it was
reading the schema and the real call sites while preparing the NEXT slice.
Cheap here (nothing produces a landing instruction yet, so tightening only
refuses more); it would not have been cheap after step 5 wired the lane on.

NEXT (in PROGRESS.md): step 4 proper — the lander, against the bare-branch
form. Take the fenced git wrapper first: the ledger's earlier pin (every git
call through one wrapper, not legacy's accidental isolation) is the foundation
every later stage sits on.

## 2026-07-20 (later still) — step 4a, and the sixth vacuous fence

The sealed lander's foundation is in (4d17602): the fenced git wrapper and the
real-repository stage.

The wrapper is a TYPE rather than a helper on purpose. Its environment is
constructed, never derived from `os.Environ()`, which is what makes a hostile
`GIT_DIR`/`GIT_INDEX_FILE`/`GIT_ALTERNATE_OBJECT_DIRECTORIES` or a forged
`GIT_AUTHOR_*` structurally unable to reach git — as opposed to legacy's shape,
where the merge overrides those four vars and everything else runs bare. The
legacy lander's isolation is accidental: its long tail of plumbing calls
(receipt scan, symbolic-ref, merge-base, diff-tree, worktree list, the cat-file
inside its xargs blocks) runs with the operator's live config and hooks in
scope, and only the fact that they are read-only keeps a hook from firing.
Routing everything through one entry point makes ADR-017:704-711 a property of
the program instead of a property of the commands someone remembered.

The repository stage deliberately applies NO dirty fence. ADR-017:742 scopes
that to the REVIEWED paths, which are unknown until the closure stage, and a
global check would refuse an operator who merely has unrelated work in
progress — legacy permits that explicitly and it is the difference between a
usable system and one that lands only on a pristine tree. Pinned by its own
test so a later slice does not helpfully add one.

**The finding: the sixth vacuous fence this phase.** All eight fences were
mutated to `false` individually. Seven died. The symbolic-target fence did not,
and two successive drafts of its test case failed to make it die. The reason is
a git semantic, not a test bug: git resolves symref chains TRANSITIVELY, so with
`refs/heads/T` symbolic, `symbolic-ref --short HEAD` reports the name at the end
of the chain and can never equal T. The HEAD-on-target fence therefore refuses
every input the symbolic fence would, and no scenario reaches the symbolic one
independently. Retained — it runs first, so it is the production refuser and its
message names the real problem, and it states the invariant directly rather than
relying on a coincidence of git's resolution order — but documented as redundant,
with the test case relabelled for what it actually exercises.

Worth noting how the first draft failed differently from the second. Draft one
left HEAD on `main`, so the case was refused by the HEAD fence for the obvious
reason. Draft two moved HEAD onto the aliased name specifically to isolate the
symbolic fence — and was STILL refused by the HEAD fence, for a reason I had
assumed away rather than measured. The fix was to stop reasoning about git's
behaviour and run it: three commands in a scratch repo settled it. The running
tally of this smell is now six, and the cheapest instrument remains the same —
mutate the fence, and when the suite stays green, go and measure the tool rather
than adjust the test.

NEXT (in PROGRESS.md): 4b the sealed task-store side (reuse
`inspectCompletableTaskStore`), then the reviewed-path set and path-scoped dirty
fence, then the import, then CAS ref + merge. Stop at the merge.

## 2026-07-20 (4b) — three more vacuous fences, and a taxonomy of why

The sealed task-store stage is in (f5a0129). It is deliberately NOT
`inspectCompletableTaskStore`, though the shapes rhyme: that function asks
whether a store is COMPLETABLE — its producer holds it RW and the identity facts
are OUTPUTS to record — while landing asks whether a store IS the exact sealed
artifact the assignment already named, held RO, in a different effect class,
with the identity facts as INPUTS to match. Sharing one function would mean a
parameter that selects which of two security questions is being asked. It also
runs under the landing fences rather than `sourceGitEnv()`, which lacks
GIT_NO_REPLACE_OBJECTS and a hooks override — and the sealed store is
attacker-shaped input to this class, not our own scratch space.

Mutation found three more vacuous fences. What is worth recording is that they
were vacuous for three DIFFERENT reasons, and only one of the three is the
failure mode this ledger has been naming:

1. **Tautological** — the object-format check. Git derives the reported format
   by reading `extensions.objectFormat` from the very config file the exact
   reproduction check had already pinned byte for byte. It was the same opinion
   laundered through a subprocess. REMOVED, not repaired: repairing it would
   have meant inventing a scenario where git disagrees with the file it reads
   the answer from. The danger of this shape is that it reads as an independent
   fence to an auditor counting fences.
2. **Non-isolating test** — the HEAD-on-branch check, the familiar one. The test
   checked out a NEW branch, which the sole-ref fence catches first. Detaching
   HEAD leaves the ref set untouched and isolates it. Test fixed, fence kept.
3. **No scenario at all** — fsck. Nothing in the suite produced a store that was
   object-level corrupt while passing every ref/config/status check. A truncated
   loose object does exactly that, which is what fsck exists for. Test added.

The tally across 4a and 4b: 17 fences written, 4 vacuous. About one in four, and
every one had a green test under it. That ratio is the argument for making
mutation a standing step on this lane rather than a thing done when suspicious —
it is now written into PROGRESS as such. The three-way split above is also worth
keeping, because the remedies differ: a tautological fence should be DELETED, a
non-isolating test should be REWRITTEN, and a fence with no scenario needs one
built or an explicit note that the fast lane cannot reach it.

NEXT (in PROGRESS.md): 4c, the reviewed-path set and the path-scoped dirty
fence. That is where the bulk of the 24 portable legacy tests land — the
stat-cache evasions, the untracked-collision walk, and the directory-rename
inference cases all attach to the reviewed path set.

## 2026-07-20 (4c) — the fence set is fine; my model of git was not

The reviewed path set and the path-scoped dirty fence are in (224b58a). The
scoping is the substance: a global dirty check makes landing usable only against
a pristine tree, so unrelated dirt is permitted and reviewed-path dirt refused,
with both directions pinned. The merge base is bound to the assignment's frozen
base, which lets the target ADVANCE freely while a rewritten target refuses
loudly — the "diverged target stays in the lane and refuses loudly" property
this lane was built for.

Mutation found four vacuous fences. One was the familiar shape (no criss-cross
history existed anywhere in the suite, so the merge-base uniqueness check had
nothing to kill it; built one). **The other three were something different and
worth naming: they were wrong ASSUMPTIONS ABOUT GIT, not weak test scenarios.**

- `--no-renames` on `diff-tree`: I carried over legacy's rename-inference
  concern and assumed a repository could switch detection on via `diff.renames`.
  It cannot — `diff-tree` is plumbing and ignores that config; with it set true
  both source and destination paths are still reported. The flag is inert.
- `core.checkStat`/`core.trustctime`: I asserted in a comment that these
  overrides are what defeat the stat-cache evasion. They are not, for the 4c
  fence: git 2.50 detects a same-length, mtime-restored edit through `git diff
  --name-only HEAD` with the hostile config in force and our overrides removed.
  The property is real; the explanation was invented.
- (4a's symref transitivity was the same species.)

Each took ONE scratch-repo probe to settle — under a minute, versus two wrong
test drafts in 4a before I stopped theorising. The rule now in PROGRESS: when a
fence survives mutation, measure git rather than adjust the test. A fence that
survives is telling you something is true that you did not know, and the useful
response is to find out what.

The cost of getting this wrong is not a failing test — it is a comment that
tells the next reader a flag is load-bearing when it is not, so they keep it
through a refactor for a reason that was never real, or drop the flag that IS
load-bearing because it looked like the same kind of decoration. Both overrides
are retained here, but labelled: expected to matter for 4e's read-tree/write-tree
comparison, where the stat cache genuinely is consulted. That claim is now
written down as something to VERIFY at 4e, not to assume a third time.

Tally across 4a-4c: 25 fences, 8 vacuous. One in three.

NEXT (in PROGRESS.md): 4d, the import — `pack-objects --revs --stdout` piped to
`index-pack`, fsync and verify, no hardlink/alternate/speculative delete.

## 2026-07-20 (4d) — the import, and dead code wearing the look of a fence

The import is in (505e6d1). The `pack-objects | index-pack` pairing is the
substance: it satisfies ADR-017:743-745's three prohibitions STRUCTURALLY rather
than by discipline. Objects cross as a byte stream over a pipe, so there is no
filesystem operation that could hardlink them; nothing writes
`objects/info/alternates`, so there is no alternate to leave the real repository
broken when the mount disappears; and index-pack only ever adds, so a crash
leaves unreachable-but-valid objects that a retry deduplicates by hash — exactly
the recovery ADR-017:745-746 describes. It creates no ref, because that is the
next stage's durable marker and doing it here would make a crash mid-import
indistinguishable from a completed one.

Mutation found five vacuous. The one worth recording is a shape this ledger has
not named before:

**`if len(pack) == 0` was DEAD CODE WEARING THE LOOK OF A FENCE.** An empty rev
range still emits a 32-byte pack — a 12-byte header plus a 20-byte trailer — so
the length is never zero and the branch cannot execute. This is worse than a
redundant fence. A redundant fence is a true check that something else also
catches; this was a check that CANNOT FIRE, sitting where a reader would count
it as coverage of the empty-closure case. And the empty-closure case is real:
a reviewed commit equal to the frozen base imports nothing, and the stages after
it would then create a ref and merge a no-op while reporting success. Replaced
with a pack-header object-count parse, which is reachable and now tested both
ways.

Two others were measured and demoted rather than repaired: `--fix-thin` is a
genuine no-op (pack-objects without `--thin` emits a self-contained pack, and
index-pack accepted it into a repository lacking the base entirely) and is
REMOVED; the sealed-store precheck is redundant with pack-objects' own rev
validation and is retained only to fail fast naming which rev is missing.

The last two are a category the taxonomy needed: **spec-mandated with no
reachable failure.** ADR-017:744 says the import "fsyncs and verifies the
imported objects", so the verification exists because the ADR requires it, not
because a test can drive it — index-pack validates what it writes, so producing
a store where it succeeded yet the objects are unreadable would mean corrupting
the pack between write and read. Retained and labelled. The label matters: it
tells the next reader not to go looking for the scenario that justifies it, and
not to delete it when they fail to find one.

Tally across 4a-4d: 32 fences, 13 vacuous. The rate is not falling, which is
itself informative — it is not carelessness that produces them but the ordinary
gap between "this check is correct" and "this check is reachable and load-bearing
given every check around it".

NEXT (in PROGRESS.md): 4e — CAS-create the ref with a zero old-value
update-ref, re-check the SHA fence, `git merge --no-ff`. STOP at the merge.
Verify there whether core.checkStat/core.trustctime actually matter for the
read-tree/write-tree comparison; that claim has now been assumed twice and
measured never.

## 2026-07-20 (4e) — step 4's stages are built, and a vacuous fence hid a real hole

The CAS ref and the merge are in (5c72585), which completes step 4's STAGES.
What remains of step 4 is composition: there is still no `mc __land-sealed`
verb and no CLI entry, so the five stages exist as tested functions that nothing
calls. That is small next to what is built, but it is not done, and PROGRESS
says so plainly rather than letting "step 4 done" imply a reachable lander.

**The probe promised at 4d settled the open stat-cache claim against me.**
`core.checkStat`/`core.trustctime` do not matter for `read-tree` + `add -u` +
`write-tree` either — the place the previous comment predicted they WOULD. The
reason is structural and worth carrying forward: read-tree into a FRESH index
produces no stat cache at all, so `add -u` has nothing to trust and must re-read
every file. A disposable index is what defeats stat-cache evasion, not those two
settings. Assumed twice, measured twice, wrong both times. They stay as cheap
defence for any future path that reuses the PERSISTENT index, labelled as live
nowhere today.

**The finding that matters: a vacuous fence was hiding a real hole.** The
symbolic-ref check in `createLandingRefCAS` survived mutation, and the obvious
reading was "redundant with the existence check, label it and move on" — the
disposition three earlier fences got. It was wrong. My scenario had placed the
aliased branch at the wrong SHA, so the existence check refused first for an
unrelated reason. Put the alias AT THE REVIEWED SHA and the hole opens: the
existence check sees exactly the value it expects, returns "replay accepted",
and landing proceeds believing it owns a ref that is really an alias onto an
operator branch. The merge runs; the eventual cleanup deletes the operator's
own work.

So the fence was correct and load-bearing all along, and the test was proving
nothing. That is the inverse of the 4b object-format case, where the fence
genuinely was tautological and deleting it was right. The two are
indistinguishable from the mutation result alone — both present as "survives
mutation" — and the only way to tell them apart is to ask what scenario would
make this fence the LAST line, then build it. Guessing "redundant" from the
survival is how a real guard gets deleted.

Also settled: the zero old-value in `update-ref <ref> <new> ""` is the fence,
and the rev-parse pre-check is the convenience. The race between them is exactly
what the CAS closes and is unreachable from the fast lane, so the MECHANISM is
now pinned directly (a zero old-value refuses an existing ref) rather than left
untested because the race is hard to stage.

Tally across 4a-4e: 40 fences, 16 vacuous.

NEXT (in PROGRESS.md): compose the stages into `mc __land-sealed` under
RequireHostScope reading /mc/landing.json, then step 5 turns the lane on.

## 2026-07-20 — step 4 composed: the lander exists, and the fence order was buildable after all

The five stages from 4a-4e became one lane (`landSealed`), an envelope
entrypoint (`RunSealedLanding`), and a host-scope verb (`mc __land-sealed`).
Step 4 is DONE.

**The ordering question, and why the first answer was wrong.** Reading the
stage signatures suggested the ADR's order was unbuildable. `landingMergeBase`
and `reviewedLandingPaths` both resolve `verifiedSHA` in the repository they are
handed, and the real repository does not have the reviewed commit until the
import — so the reviewed-path dirty fence appeared to be forced AFTER the
import, inverting ADR-017:741-743. That deviation was drafted and nearly logged,
with the justification that a refused landing would leave only unreachable
objects, which ADR-017:745-747 explicitly tolerates.

It was avoidable. The reviewed set is a diff from the FROZEN BASE to the
reviewed commit, and the sealed store holds `base..verified` by construction —
so the set can be derived there, where no import is needed, and the fence then
runs in the real checkout before anything crosses. `fenceReviewedPathsCleanAt`
is the additive split; 4c's own tests never move. The lesson is the one 4c
already taught in a different costume: the constraint that looked structural was
an artifact of which repository the derivation was pointed at. A refusal now
writes nothing at all, which is strictly better than a tolerated mess, and the
test asserts BOTH the refusal and the absence of the objects — so the order
cannot silently invert later.

**The cover obligation is closed.** Step 3 recorded it as OWED: a landing
container run without the `.mission-control` cover hands the sealed root out RW
through the source alias, defeating the separate RO `/repo/task` row. The lane
now proves the declared cover was realized — absent, populated, symlinked, and
foreign-path all refuse. A bind that silently did not happen is invisible from
inside the container except by what is visible underneath it, which is exactly
what this checks.

**Mutation, again, and the same two-in-five rate.** Nine fences, four survived
the first sweep. Three were REDUNDANT and are now labelled with what actually
catches them — the taxonomy's "keep if it fails faster or names the problem
better" arm. The fourth was neither redundant nor weak: it was a WRONG SCENARIO.
Setting the frozen base to the reviewed commit to make "base is not the merge
base" empties the sealed diff, so path derivation refused first and the base
binding was never reached; the test passed for a reason unrelated to the thing
it named. The real scenario is a rewritten target — the operator amends the tip,
the merge base slides back to c1 while the assignment still names c2 — and it
needs a TWO-COMMIT fixture, because with one commit a rewritten target shares no
history at all and `landingMergeBase`'s own fence fires instead. Three fences
deep, three different refusals, only one of them the one under test.

That is a fourth failure mode to sit alongside TAUTOLOGICAL / REDUNDANT / DEAD:
a fence can be live and load-bearing while its test never reaches it, because an
EARLIER fence refuses the scenario first. Mutation is what distinguishes it from
a healthy fence; nothing in the passing suite does. When a survivor appears, ask
which fence actually refused before assuming the scenario was too weak.

One DEAD guard was removed rather than kept: `fenceReviewedPathsCleanAt` does
not re-check for an empty path set, because `reviewedLandingPaths` is its only
producer and already refuses one.

**What the lane deliberately does not do**, both logged in
IMPLEMENTATION-NOTES (2026-07-20): it does not verify `pinned_closure_digest`
(the field is UNDEFINED at landing time — the assignment's digest describes the
first-task pack, but the store has since been rebuilt from the accepted seal, so
checking it would refuse every real landing), and it has no retry ADOPTION path
(ADR-017:750-753), so a retry after a successful merge refuses at the pre-merge
fence rather than adopting. Both are fail-closed and purely additive later.

It STOPS at the merge, which ADR-017:756-758 and legacy's own ordering violation
both independently argue for. Still inert end to end: nothing produces a landing
instruction and no resident arm invokes the verb.

NEXT (outgoing): Sealed landing, the rest of the lane — step 4's composition
(`mc __land-sealed`, RequireHostScope, reading the landing instruction).

## 2026-07-20 (later) — the corpus gap analysis, and a real hole in the sealed store

Step 4's owed work was "port the 24 portable adversarial Git tests from
`runner/image/mc-land.test.ts`". The port is under way, and the first finding is
about the estimate itself: **24 was too high**. Steps 4a-4e absorbed most of
those hazards organically while building the stages, so a blind port would have
added duplicates. The real job is a gap analysis against the sealed lane's
existing tests, and it should have been framed that way in the triage.

Three fan-out agents were spawned to do that analysis in parallel. Two idled
twice without returning anything, including after a direct request naming the
report shape. The channel did not deliver, and the correct response was to stop
spending turns on it: the two questions flagged as highest-value were cheap to
answer by reading the code directly, and doing so took less time than the
retries had. Recorded because the failure mode is invisible from the outside —
an agent that idles looks like an agent that is working.

**The one real defect the exercise found.** Legacy fenced index-visibility flags
on BOTH sides — `mc-land.test.ts:128` for the task worktree alongside `:198` for
the primary checkout. Only the real-repository side had been carried into the
sealed lane. `revalidateSealedTaskStore` proves the worktree is pristine with
`status --porcelain`, and `--assume-unchanged` or `--skip-worktree` makes status
report a TAMPERED reviewed file as clean. The store's exact-byte config
reproduction — which is genuinely strong, and is what makes a stat-cache evasion
structurally impossible there — does NOT generalize to this, because these flags
live in the index rather than the config. Mutation-proved: with the new check
removed, both subtests fail with "accepted a sealed store with a tampered
reviewed file hidden by --assume-unchanged".

That asymmetry is worth naming as a pattern. Two fences protected the same
property on two sides of a crossing; one side got a hardening the other did not,
and nothing in either file pointed at the other. The sealed store is the
attacker-shaped input of this class — it is the Worker's own output, under
review — so if anything the weaker side had it backwards.

`:90` (paths containing spaces) was ported STRUCTURALLY rather than as a case:
the landing fixture's repository now lives under a "work space" directory, so
every test in the file exercises the spaced path through every stage. `:882`
(already-ancestor reviewed SHA) was ported and doubles as the retry-after-success
case logged earlier — it refuses at the frozen-base binding, because once merged
the reviewed commit becomes the merge base.

Eleven further legacy tests were assessed as already COVERED and deliberately
not ported; the specific citations are in PROGRESS so the next session does not
re-derive them. Notably `:919` (refs/replace) is defended on BOTH sides already —
`GIT_NO_REPLACE_OBJECTS=1` in the constructed env for the wrapper, and the
sole-ref check for the sealed store.

What is left is where the value is now concentrated: the TIMING cases (`:683`,
`:413`), which concern hostile state introduced BETWEEN preflight and merge. The
honest question for the next session is whether `mergeSealedLanding` re-checks
anything at merge time beyond the SHA fence. If it does not, that is a gap with
a real attack, and it is exactly the kind the stage-by-stage tests cannot see.

The known onboard flake fired once during this session's full run and was
confirmed as the documented one (exact message, then 8/8 green isolated). Its
sighting is recorded in PROGRESS; the sealed landing work touches no onboarding
path.

## 2026-07-20 (later still) — the timing cases, and four knobs held by nothing

The corpus gap analysis reached the timing cases, and the answer to "does
`mergeSealedLanding` recheck anything at merge time?" was: only the SHA fence.

**:683 is now closed.** Preflight's executable-config fence cannot see a merge
driver inserted after it ran, and an in-tree `.gitattributes` naming that driver
survives BOTH `core.attributesFile=/dev/null` (which replaces the attributes
FILE, not the in-tree ones) and `GIT_ATTR_NOSYSTEM=1` (system scope only), while
`-c merge.ours.driver=false` pins only `ours`. The merge stage now re-runs the
executable-config and index-visibility fences itself. The residual TOCTOU is
named at the call site rather than papered over: git reads config when it runs,
so a writer racing the merge invocation still wins, and only config isolation
would truly close it — which the merge cannot have, because it needs the
repository's own config.

Writing that test corrected it, twice over. Of three arms, two die under
mutation (merge driver, index flag) and one SURVIVES: the clean filter is
refused by git's own checkout failing, not by our fence. It is relabelled as
measured-redundant, because an arm that reads as evidence for a fence it does
not exercise is worse than no arm at all.

**The finding that mattered more was not in the corpus.** Asking "is a test
actually holding this?" of the merge stage's pinned knobs: deleting
`merge.autoStash=false`, `--no-autostash`, `merge.renames=false` and
`merge.directoryRenames=false` TOGETHER left the entire mc/verbs suite green.
Four knobs, correct in code, reachable by no test. This is the third time this
exact rot has appeared (6657541's fence that only ever tested the REFUSED
direction, 83ed9e9's unpinned wiring hidden behind darwin's inert platform), and
it is worth stating as a standing habit rather than a lesson: after pinning a
behaviour with configuration, mutate the configuration away and confirm
something dies. Correct-and-unreachable is the default state of a config knob,
not an unusual one.

autostash is the one with teeth, because it MOVES OPERATOR BYTES: with it
enabled and a reviewed path dirty, git stashes the operator's uncommitted work,
merges, and pops — a landing quietly rewriting the working tree as a side
effect. Now pinned, mutation-confirmed. `merge.renames`/`merge.directoryRenames`
remain unpinned; the recipe for closing them is in PROGRESS rather than left to
be rediscovered, because it needs a bespoke fixture and a guessed one would
reproduce the very problem being fixed.

**Parked, not coded:** a conflicting merge leaves `MERGE_HEAD` and wedges the
single landing slot (verified, not reasoned). Unwedging it means "abort only
what we started" — a MUTATING failure path in a lane that has deliberately had
none. That is an operator decision, and the current behaviour is pinned by a
characterizing test so it cannot drift while the decision is outstanding.

NEXT (outgoing): the corpus port — timing cases assessed, autostash pinned,
rename inference still unpinned.

## 2026-07-20 (operator) — the sealed lander MAY abort its own conflicted merge

Operator answered the parked failure-path question: **Option B, the scoped
self-abort.** On merge failure the lander runs `git merge --abort`, gated on
`MERGE_HEAD` being the SHA it verified and created itself — legacy's "abort only
what we started" (`mc-land.test.ts:368`).

The question was framed to the operator as six end-to-end situations rather than
as a principle, and two of them carried the decision:

- A leftover `MERGE_HEAD` head-of-line-blocks the single landing slot, and
  nothing surfaces it. Later, unrelated, non-conflicting tasks refuse at the
  operator-merge-in-flight fence until a human clears the checkout by hand.
- The natural human response to finding conflict markers — resolve, `add`,
  `commit` — COMPLETES the lander's merge, putting reviewed work onto the target
  outside the lane and outside its record of what landed. It is indistinguishable
  from an ordinary conflict resolution, so nothing warns the operator.

The counter-cost is real and was stated: the conflicted tree is no longer
available for inspection, so diagnosing a collision means re-running the merge
against the two commits the error names. Accepted.

The invariant objection ("this lane has never mutated on a failure path") was
answered rather than overridden: `--abort` RESTORES the pre-merge tree, so it is
closer to "writes nothing on failure" than leaving a half-applied merge behind.
It is still a write, so the guard is what makes it safe, and the guard is
testable.

Also confirmed to the operator, and worth keeping: this does NOT teach the lander
to resolve conflicts. Landing fails under both answers; reconciliation stays with
the operator. The decision was only about what state the checkout is left in.

Not implemented in this session. Per the 2026-07-20 diagnosis it gets its OWN
slice and its own adversarial review, not a smuggled line in the composition
commit. `TestLandSealedLeavesAConflictedMergeInPlace` is characterizing, not
aspirational — the slice INVERTS it (assert no `MERGE_HEAD`, assert a later
unrelated landing succeeds) rather than deleting it, and adds the negative case:
an operator merge in flight is still refused at preflight and never aborted.

NEXT (outgoing): the corpus port — timing cases assessed, autostash pinned,
rename inference still unpinned; plus the now-unblocked self-abort slice.

## 2026-07-20 — the corpus closes, and the self-abort lands with a hole in it

Two slices, and the second one is the interesting entry.

**Rename inference (`96b86a2`) closes the corpus.** The last legacy hazard
(`mc-land.test.ts:289,328`) is now pinned by a bespoke fixture: the reviewed side
renames and edits a tracked file, the operator edits it where it still is. With
detection on git carries the operator's edit onto the renamed path and commits.
That is not merely surprising — it breaks an AGREEMENT between two halves of the
lane, because the reviewed-path dirty fence derives its set with `--no-renames`,
so the merge would write a path the fence never checked.

Mutation-confirmed both ways, which is the habit the previous session asked for:
dropping `merge.renames=false` makes the merge succeed and the test fail;
`merge.directoryRenames=false` is CORRECT AND UNREACHABLE, because git disables
directory-rename detection whenever rename detection is off. It is labelled as
such rather than counted. Three of the four knobs from `7364503` are now
accounted for honestly: two live, one measured-unreachable, one (autostash)
already pinned.

The fixture carries a guard asserting git actually infers the rename. A
similarity-based fixture that drifts below the detection threshold keeps passing
while testing nothing, and that failure mode is invisible without the guard.

**The self-abort (`6463d8a`) shipped with a real hole, found by reviewing it
(`64dc5de`).** The operator's decision was recorded as "abort iff `MERGE_HEAD` is
the SHA it verified and created", and it was implemented exactly that way.

The reading is wrong, and the reason is worth stating because it is a
lane-shaped mistake rather than a coding one. Stage (7) creates
`refs/heads/mc/task-<id>` at the reviewed SHA. That is the durable import marker
— and it is also a NAME, in the operator's own repository, for the commit whose
uniqueness the whole ownership argument rested on. `git merge mc/task-7`, typed
by an operator in the window before our merge completes, produces a `MERGE_HEAD`
equal to the reviewed SHA. The gate would then destroy their half-resolved
conflict: the precise outcome the operator's own scope guard forbade.

Note where the sizing error came from. The scope guard called the identity check
"a second, independent fence, not the primary one", with the preflight
merge-in-flight refusal as primary. But preflight cannot see a merge started
after it ran, and a merge started after it ran is the ONLY state the abort path
ever meets. The second fence is the only fence at that point, and it had been
sized as though something else were carrying the weight.

ADR-017:752-753 had the right standard all along — "accepted or aborted only
when they match THIS ACTION", which is action identity, not SHA identity. The
loose reading came from the decision entry's shorthand, not the ADR. The gate now
also requires `MERGE_MSG` to carry this landing's id, which git preserves
verbatim through a conflict and which an operator's merge does not have. Writer
and matcher share a constant, because a drift between them would silently stop
the abort recognising its own merges — failing in the quiet direction.

The abort's failure directions are deliberately asymmetric: an unreadable or
unrecognised message refuses to abort. The cost of being wrong is then residue
left behind and a wedged slot, never an operator's merge destroyed.

`TestLandSealedLeavesAConflictedMergeInPlace` was inverted rather than deleted,
as the decision entry required, and now also asserts that unrelated uncommitted
operator work survives the `reset --merge` and that a SECOND, unrelated sealed
landing runs to a merge afterwards — the slot really is free, which no assertion
about the first landing's own residue could establish.

NEXT (outgoing): corpus closed and the self-abort reviewed and fixed; the lane is
still inert. Remaining before Phase 3 completion: the four-seam activation, then
the real-mechanism/Docker acceptance lane.

## 2026-07-20 — the abort slice under adversarial review

Two independent read-only reviews of the self-abort. Both earned their cost, and
between them they changed the code three times. Worth recording is not only what
they found but which finding was WRONG, because it was the one rated most
severe.

The conformance review settled the question the slice was nervous about: the
lane is permitted to mutate here. ADR-017:752 — "Merge-in-progress files are
accepted or aborted only when they match this action" — both authorizes the
abort and supplies the gate, and the "later trusted action" constraint at :757
is scoped to deleting the task ref and emptying the task root, neither of which
the abort touches. It also confirmed the four seams remain unwired. That reading
is what turned up the ownership hole recorded above, because the ADR says match
THIS ACTION and the implementation matched a SHA.

The safety review then found the second forgery, which is subtler and better.
The landing-id gate assumed MERGE_MSG can only carry our trailer if we wrote it.
It cannot be assumed: the reviewed commit's message is authored by the task
agent, and `git merge --log` — or `merge.log=true`, an ordinary operator setting
— splices every merged commit's SUBJECT into MERGE_MSG as an indented shortlog.
An agent that titles a reviewed commit with the landing trailer forges the
match. Our own `--no-log` protects the message WE write and can say nothing
about the operator's. The fix is structural rather than lexical: our subject at
the start, the trailer as a line at column 0. A shortlog entry is indented under
a `* <branch>:` header and cannot present as one. Deliberately, this does not
rest on the landing id being secret, because nothing in the lane makes it so.

The pattern across both holes is the same and worth naming: each time, the lane
itself created the capability that defeated the gate. Stage (7) publishes the
reviewed commit under an operator-typeable name; the sealed store carries
agent-authored text into an operator's merge message. Asking "what did we hand
the other party?" would have found both faster than asking "is this check
correct?"

**And one finding was refuted by measurement.** The review's most severe defect
was that `merge --abort`, being a `reset --merge`, destroys unrelated STAGED
operator work — reverting it to HEAD, recoverable only as a dangling blob. The
mechanism is real; the reachability is not. `git merge` refuses to start with
anything staged, exit 2, no MERGE_HEAD written, so a merge state cannot coexist
with pre-existing staged work and the abort is never entered. The same fact
refutes the companion claim that the abort would fail and wedge the slot.

The review reached its conclusion by testing `reset --merge` standalone and
inferring reachability from our path-scoped dirty fence permitting unrelated
work. The fence does permit it. Git's own clean-index precondition — not our
fence — is what excludes it. The lesson is not that the review was careless; it
had verified its mechanism carefully. It is that a verified mechanism plus an
unverified path to it is still an unverified finding, and the path is the half
that decides whether to write code.

Two smaller items landed with it: the abort now asserts the target is still at
the frozen preimage (ADR-017 names target preimage, and `merge --abort` resets
to whatever HEAD is now — no reachable scenario today, labelled as such), and
`landSealed` now validates LandingID, which by that function's own comment is
the one guard that is not redundant and was the one missing.

Accepted residuals, recorded in IMPLEMENTATION-NOTES rather than guarded: the
millisecond window where an operator stages during a failed landing, the TOCTOU
between reading MERGE_HEAD and aborting, and the fact that three materially
different post-conditions are still one prose error. The last belongs to the
landing failure taxonomy already owed, and the three cases are now written down
as its concrete requirements.

NEXT (outgoing): abort slice reviewed, hardened, and still inert. The four-seam
activation is next, then the Phase 3 real-mechanism/Docker completion lane.

## 2026-07-20 — the activation is not four edits

A read-only survey of the four seams, verified by grep rather than taken on
report, found the activation plan under-scoped. The four seams are SWITCHES.
Three PRODUCERS they consume do not exist:

- Nothing constructs `PrivateDispatchLanding`. `mountattest.go:884` never sets
  `.Landing`; the only construction site is a test fixture whose header already
  says "INERT: no attester produces this yet". `dispatchprivate.go:460`
  validates a carrier that nothing emits — a validator with no producer, which
  is easy to mistake for a live seam when reading the file.
- The resident has no landing concept at all: zero occurrences of the word in
  `types.ts`, no `landing` member on `MountPlan`, no sealed arm on `Effect`.
  `effects.ts:696` `land()` is the legacy worktree lane and nothing else.
- `LandingID`, `PreMergeSHA` and `PinnedBaseSHA` have no host-side producer.

`SealedLandingPending` and `resolveLandingRoots` likewise have no production
caller. The Go executor and its CLI arm exist and are reachable only by hand.

This matters for sequencing rather than for design. The producers can be built
FIRST and stay inert on their own, because nothing reaches them until the
selector flips — so the atomicity requirement ("turn all four on together")
applies to the last step alone, not to the whole slice. That is what makes this
buildable incrementally without ever leaving a half-active lane.

The three missing facts were then settled from the ADRs, so the next session
implements rather than re-derives: `PinnedBaseSHA` is the assignment's frozen
`base_sha`; `PreMergeSHA` is the target tip observed AT ATTEST and carried
frozen, which is why attest must observe the target at all; and `landing_id` is
DETERMINISTIC — ADR-016:830-833's first 16 hex of a domain-separated digest of
deployment, subject and approved packet/run identity. That last one is
load-bearing in a way worth stating: the abort path added this session matches
`MERGE_MSG` on the landing id, and the container name is `mc-landing-<id>`, so
a random id would break replay identity in two places at once.

Two tests assert the inert behaviour AS CORRECT and must be inverted rather than
deleted; one of them (`TestStep0c_ApprovedBranchlessRow_NeverLands`) stays green
on its literal fixture, so it is its stated INTENT that goes stale — the quiet
kind of rot, and the reason it is named here.

NEXT (outgoing): activation step 1 — the deterministic landing_id, then the
attester's landing carrier. Both inert until the selector flips.

## 2026-07-20 — the landing id, the carrier, and what the seam actually costs

Two producers landed; the third turned out to be much larger than the
activation plan recorded, and that is the session's real finding.

**Producer 1, the landing id** (`landingid.go`). ADR-016 D7's first-16-hex of a
domain-separated digest, its own `MC-DISPATCH-LANDING-V1` domain beside the
dispatchseam constants. Its third input needed settling: "exact approved
packet/run identity" occurs ONCE in the corpus (:833, echoed :846) and is never
expanded, and it has no unique referent in the spine. Resolved to the accepted
Worker seal's `(run_id, request_id)` pair, because the packet contributes no
entropy — `review_packets.task_id` is both PK and FK, so packet identity IS the
subject the digest already names — and approval is an operator write with no
run of its own. Full reasoning, including the two deliberately excluded inputs,
is in `IMPLEMENTATION-NOTES.md`.

The corpus is SILENT on whether the id must be stable across attempts. It is,
under the abort slice landed at `2d2cffb`: ADR-017:753-756 requires a retry to
match "its exact action trailer", and this lane's trailer is the landing id.
Worth recording that the corpus points the other way on names generally —
`mc-land` "never guesses from a name" (:378), "name alone is never authority"
(:362-363). Consistent, because the id is the discriminator INSIDE an
already-authorized trailer match, not authority itself. Do not promote it.

**Producer 2, the carrier** (`landingcapture.go`). Asserted against
`validatePrivateMountPlan` rather than field-by-field, so a producer/validator
divergence fails at the producer instead of at arming time. `PreMergeSHA` is
observed at attest via the lander's own `revalidateLandingRepository`, which
means attest refuses exactly what the lander would; a landing that could not
succeed is never planned.

**Producer 3 is not a producer — it is a seam.** The activation plan said step
3 flips three switches. That is wrong, and the correction is authored rather
than delegated: ADR-016:369-379 says a landing-pending row's id, task-store
identity, base, verified SHA and target "form an attested candidate rather than
a bare effect", and "Commit rechecks the entire pending tuple and inventory
before returning the frozen landing plan". So the sealed landing must go
THROUGH prepare/attest/commit. It cannot today, because the seam is
spawn-shaped end to end:

- `preparedCandidate.spawn` is typed `*dispatch.Spawn` (`dispatchseam.go:465`)
  — there is no shape a `dispatch.Land` can occupy.
- Only the `KindSpawn` arm (`:509`) allocates a run id, loads mount state, and
  returns a candidate; every other kind falls through to `applyAction` and
  terminates the seam.
- Commit asserts `sel.action.Kind != dispatch.KindSpawn` (`:674`),
  `refusalCandidateFor(cand.spawn)` nil-derefs (`:652`), and the dispatch key's
  `canonicalAction` hardcodes `Consequence: "spawn"` with a role and run id
  (`:703-713`) that a landing does not have.
- `applySpawn` calls `domain.Claim` (`dispatchverb.go:471`). Landing holds no
  lease and opens no Run (§7; `setupenvelope.go:100-108`), and `runs.role` has
  no landing member (`schema.sql:694-695`). Routing a landing through it would
  consume the single global lease slot, insert a run with no heartbeat producer
  — a reap target with no container — and log the landing as `dispatch.spawn`.

So the landing needs its own candidate shape, its own canonical projection and
consequence name, and an apply that skips the claim entirely — structurally
like the `KindReap`/`KindReenter` arms, which mutate without touching the lock.

The resident half is a second, independent asymmetry: TypeScript `MountPlan`
(`types.ts:116-147`) has no `landing` member though the Go carrier declares
one; the `land` effect (`types.ts:169-175`) has no `mount_plan` though `spawn`
declares it non-optional; and `invalidMountPlanReason` (`effects.ts:90-95`)
refuses every destination outside `/workspace`, which is exactly why `Landing`
is a SIBLING of `Entries` and why `dispatchprivate.go:468-470` refuses a
landing plan carrying entries at all. `land()` (`effects.ts:696`) validates
nothing and binds a hardcoded `config.workspaceRoot`.

Also corrected: the activation plan claimed all three landing-id inputs were
already in attest scope. `DeploymentUUID` is NOT — it lives on
`preparedDispatch` one frame up (`dispatchseam.go:458`); `dispatchprivate.go:54`
is the request struct, not the attester's. `verified_sha` is likewise absent
from `DispatchMountState`.

Four tests pin the current land shape and will have to move together with the
switch, beyond the two inversions already named: `dispatchverb_test.go:187-190`
(effect-key oracle), `effects.test.ts:1080-1092` (full docker argv `toEqual`),
`split-brain.test.ts:1490-1497` (whole-object `toEqual`), and
`cli_test.go:1200-1204`, which requires the land effect to be emitted even
under a broken `routing.md` — sharp, because attest resolves routing, so a
landing routed through it would newly be suppressible by unrelated routing
breakage. That last one is a constraint on the design, not just a fixture.

NEXT (outgoing): the resident landing arm — `MountPlan.landing`, a sealed
`Effect` arm, execution beside `runAcceptedSealRebuild`, envelope to
`/mc/landing.json`. Inert on its own. The seam restructure above is the step
after, and it is the one that needs its own plan before code.

## 2026-07-20 (cont.) — the resident landing arm, and the uid that looks wrong

`runSealedLanding` plus the `MountPlan.landing` mirror. Tested directly rather
than through `applyEffect`: no effect arm dispatches it, which is deliberate, so
the seam step changes routing and nothing else.

The container envelope was surveyed rather than assumed, and almost all of it is
already decided — network `none`, `CapDrop=ALL` with NNP on, `1 cpu / 1024m /
128 pids` from ADR-019:30's own landing row, the `mc-landing-<id>` name.

Two points where the obvious guess is wrong, recorded so they are not "fixed"
later by someone who trusts intuition over the corpus:

- The uid is 10002:10002, NOT the operator's, even though landing is the one
  class that writes into an operator-owned repository. ADR-019:85 puts setup and
  landing on one row; ADR-017:76-86 refuses to assert anything about how the
  VirtioFS share presents host ownership and defers the whole question to
  ADR-019:183-188's final-uid canary. Nothing chowns a host inode, and there is
  no permission-widening fallback anywhere in the design. This was very nearly
  implemented as an operator-uid exception on intuition.
- The approved-run label key is `mc-approved-run-id`, not `mc-run-id`. ADR-016
  names the concept (:846) but never spells the key. `mc-run-id` means "the run
  this container IS" everywhere else, so a landing carrying the Worker run it
  landed FOR could read to a liveness sweep as that Worker's own agent
  container — the masquerade :857 forbids. The carrier grew `ApprovedRunID` to
  supply it.

Three gaps carried rather than closed, all in `IMPLEMENTATION-NOTES.md`: the
15-minute foreground deadline is unenforced (no timeout seam exists on
`TickDeps.docker` for ANY class, so this is inherited, not opened);
`SealedLandingResult` has no spine consumer because `mc land report` takes only
a status and a reason; and the setup/legacy-land containers are non-conformant
to ADR-016 Decision 7's label rules in ways this lane does not own.

NEXT (outgoing): activation step 3, the seam. Plan before code — it needs a
landing candidate shape, canonical projection, consequence name, and a
claim-free apply, plus the four whole-object test moves already catalogued.

## 2026-07-20 — step 3 planned: the landing is a separate lane, not a wider candidate

Step 3 was planned before code, as its own NEXT demanded. The plan came out of
three independent designs judged on three lenses (correctness, invariants,
conservatism); the winner on two of three is the SEPARATE LANE, and the two
runners-up contributed grafts that fixed real defects in it.

The three designs were: widen `preparedCandidate` with a sibling pointer; make
the seam kind-polymorphic with a per-kind ops table; or give landing its own
prepare/attest/commit trio. The deciding argument is a type-level one. There are
~35 unguarded `cand.spawn` derefs across the seam and its helpers. Under the
first two designs those become reachable-from-landing and stay correct only by
audit; under the separate lane they stay unreachable BY TYPE, because a landing
is never a `preparedCandidate` at all. `preparedCandidate`, `dispatchAttest`,
`dispatchCommit`, `refusalCandidateFor`, `spawnCandidateProjection`,
`attestCandidateMounts`, `applySpawn` and `loadDispatchMountState` keep their
exact signatures and bodies.

So the shape is: a THIRD variant of `preparedDispatch` — `landing
*preparedLanding`, sibling of `final` and `candidate` — with its own
`dispatchLandingPrepare`/`dispatchAttestLanding`/`dispatchCommitLanding` in a new
`dispatchlandingseam.go`, reusing five shared primitives (`preparationToken`,
`deriveDispatchKey`, `buildCanonicalPrepareWithIdentity`,
`readDeploymentMirrorStrict`, `applyAttestedRefusal`).

Grafts taken from the losing designs, each verified against the tree rather than
taken on report:

- The carrier rides `attestedDispatch.mountPlan.Landing`; there is no bespoke
  `attestedLanding` type. `canonicalPrivateAttestation` (`dispatchprivate.go:209`)
  already carries `MountPlan`, so `PreMergeSHA` lands inside the pre-commit drift
  frame with zero new code, and `dispatchRecheckAttestation` is reused via a
  two-line re-attest selector instead of being duplicated.
- The plan literal must be `{Version: 1, Entries: []{}, Landing: &plan}`.
  `validatePrivateMountPlan:393-395` hard-refuses `Version != 1 || Entries ==
  nil`, and one design had built it with a zero Version and a nil Entries.
- Landing refusals use `RefusalSubjectlessPipeline`, NOT `RefusalSubjectTask`.
  `refusalroute.go:145-151` reaches `domain.Block` only from `ClassCandidate` +
  `RefusalSubjectTask`, so the subjectless kind makes a durable blocked row
  unreachable by type rather than by an enumeration of today's refusal codes.
- `refusal.CodeProjectionUnavailable` (`refusal.go:79`, ClassHealth) for landing
  attest failures. One design named `refusal.CodeRuntimeUnappliable`, which does
  not exist in package `refusal` — only in `boundary`, where it maps to
  `authorityDecides` and would have `Domainf`'d inside `applyRefusal`.
- The canonical sibling is `omitempty`, so every frozen golden vector stays
  byte-identical and no spawn's preparation token moves. `dispatchseam.go:6-14`
  is what makes the alternative ADR-worthy.

Two judge splits, both resolved conservatively:

- Mount-state loader. Two judges preferred wrapping (`&dispatch.Spawn{SubjectID:
  &id}`), one called that a semantic fib and wanted `loadDispatchMountState`
  narrowed to `*int64`. Took the wrapper: it touches zero existing call sites and
  changes no shared signature. `loadDispatchMountState` is already role-BLIND —
  it reads only `sp.SubjectID` and never `sp.Role` — so the fib is neutralized by
  a comment, not by refactoring a function the spawn path depends on.
- Refusal subject kind. One judge wanted `RefusalSubjectTask` for the queryable
  subject column. Took subjectless: observability is recoverable from the health
  detail text; a durable blocked row written from the seam is not recoverable and
  violates ADR-016:573-576.

Four things the corpus is SILENT on, decided here and logged:

- **No dispatch-side receipt for a landing commit.** ADR-016:255-257's
  prepare-side rule reaches mutations returning DIRECTLY FROM PREPARE; a landing
  returns from commit. ADR-016:261-263 exempts a result that has caused neither a
  state mutation nor a host effect, and at the instant `dispatchCommitLanding`
  returns, NEITHER has happened — the spine is untouched and the resident has not
  yet started the container. A `dispatch_key` would additionally be a fake fence,
  because the token binds a per-command `RequestID` and can never dedupe across
  ticks; cross-tick idempotency is assigned elsewhere by the corpus (the durable
  landing-pending row, plus receipt-idempotent `mc-land` keyed on the stable
  landing id). Adding a receipt later is additive; removing a shipped one is not.
- **No new consequence identifier**, and the question is made moot rather than
  answered: the success path derives no key and builds no `canonicalAction` at
  all, so the only `canonicalAction` the lane constructs is the refusal one,
  reusing the existing `"refusal"` literal. The `// "spawn" | "refusal"` comment
  stays accurate. The one new string anywhere is `canonicalCandidate.Kind ==
  "landing"`, which is a candidate-identity tag inside the token, not a
  consequence name.
- **The landing id moves to PREPARE.** This contradicts `landingid.go:9-11`,
  which sited it attest-side on an availability argument. ADR-016:371 names the
  id as a member of the candidate TUPLE, and a tuple member must be inside the
  preparation token; that is the stronger argument. All four inputs are in
  prepare's scope. The header comment is corrected in the same commit rather than
  left contradicting the code.
- **Landing attest reads no routing.** ADR-016:53-60: a land candidate "instead
  attests ADR-017's exact task-store/real-repository Git views ... without a
  gateway probe", and spec §7:231 puts no agent in the landing path, so there is
  no role to resolve. `dispatchAttestLanding` contains no path to
  `routingRefusal` or `CodeRoutingInvalid`.

That last one is where `cli_test.go:1193-1206` turned out to be weaker than
feared AND weaker than assumed. It is green by construction — its fixture is
branch-carrying, so it satisfies `LandingPending` not `SealedLandingPending`,
returns as `prepared.final`, and never reaches any attest. It therefore does not
guard the sealed lane at all, which makes a sealed twin a required NEW test
rather than a fixture update. The constraint the earlier survey read into it is
real, but the test does not enforce it.

Two risks found that the design had to close rather than carry, both resident-side
and both capable of converting infrastructure trouble into a durable blocked row —
exactly what ADR-016:576 forbids:

- **The confirmed-absence gate (ADR-016:373-375) was open.** The container name
  `mc-landing-<id>` is stable by construction, so a crashed prior attempt's
  container makes a fresh plan collide on the name, exit non-zero, and — under
  today's `effects.ts:774-779` — report `land report --status failure`. Closed by a
  resident-side container-name pre-check, because `mc/verbs` has no Docker client.
  This is a HARD FLIP PRECONDITION: do not run step 16 until step 15 is green.
- **Exit-code conflation.** Today ANY non-zero exit reports failure. The new
  mapping, grounded in the exit contract at `mc/cmd/mc/main.go:91-107`: 0 →
  success; 1 (the fixed program's semantic Git refusal) → `land report failure`;
  anything else (2 usage/environment, docker's 125/126/127) → no report at all,
  logged as infrastructure health, pending landing retained.

The ordered plan is sixteen micro-steps. Steps 1-15 each leave the lane INERT and
the fast suite green; the lane is doubly inert throughout, because `nextLanding`
filters on `LandingPending()` only AND `domain.Approve` refuses assigned sealed
rows so `decision` never reaches `approved`. Step 16 is the atomic switch and it
is exactly two reachability edits: `nextLanding`'s filter gains
`|| t.SealedLandingPending()`, and `Approve` stops refusing an assigned sealed row
and takes the existing hold-for-landing arm instead. Neither works alone — A
selects nothing without B, and B marks rows pending that nothing selects.

1.  canonical sibling, `omitempty`, golden vectors provably unchanged
2.  `loadDispatchLandingMountState`, the subject-keyed entry point
3.  `landingWorkspaceRoot`, which refuses empty rather than returning `""`
4.  extract `attestDeploymentPreamble` (pure refactor, whole suite is the test)
5.  landing projections: candidate, tuple, canonical prepare
6.  `dispatchLandingPrepare` — freezes the tuple, id inside the token
7.  `dispatchAttestLanding` — routing-free
8.  reuse `dispatchRecheckAttestation` via the two-line selector
9.  `dispatchCommitLanding` — full-tuple recheck, writes nothing
10. driver hook + fail-closed `Domainf` on the private frame
11. fork the sealed landing out of prepare, ahead of the legacy land effect
12. the sealed twin of the broken-routing test
13. widen `LandReport` to assignment-backed rows
14. `LandingBranch()` — a provable no-op today
15. resident: routing discriminator, absence gate, exit-code classification
16. THE SWITCH

Step 3 is its own step for a reason worth keeping: `""` into `captureLandingPlan`
resolves the anchors against the process CWD, and that is the single place in
this lane where a landing could touch the wrong filesystem.

Two drift hazards get dedicated tests rather than comments. The five land-effect
keys will have TWO producers (`dispatchverb.go:418-427` and
`dispatchCommitLanding`), and the resident discriminates on the `landing` key, so
a legacy key drift would surface nowhere as a type error — step 9 asserts the two
producers agree. And the whole safety argument is that a landing is never a
`preparedCandidate`, so step 10 pins that the private frame refuses one.

Carried, not closed: `captureLandingPlan` runs through
`revalidateLandingRepository` in BOTH attest passes, so on a busy target ref the
recheck can stale-refuse permanently. Fail-closed, but a liveness hazard with no
spawn analogue, since routing.md bytes are static where a target tip is not. Only
observable after the flip. Same shape for a diverged target ref, which refuses
health every tick until an operator reads the signal — loud-and-safe over
silently-unlandable, and it obeys "only a semantic Git refusal blocks".

No operator input is required: the one obligation that would have needed sign-off
(the confirmed-absence gate) is closed in-tree by step 15.

NEXT (outgoing): execute micro-steps 1-15 in order, TDD, committing each green.
Do NOT run step 16 until step 15 is green — the absence gate is what keeps a
crashed prior attempt from becoming a durable blocked row.

## 2026-07-20 (cont.) — step 3 micro-steps 1-6: the lane's spine, still inert

Six of the fifteen inert micro-steps. Nothing selects a sealed row, so every
function below is reachable only from its own tests.

`preparedDispatch` now has its third variant and the comment on it is a
directive, not description: DO NOT simplify `landing` into `preparedCandidate`.
That edit is the one thing that would re-arm the seam's dozens of unguarded
`cand.spawn` dereferences with a nil Spawn, and it is exactly the edit a future
reader tidying up "two nearly-identical candidate types" would make.

Three things came out differently than the plan wrote them, all recorded here so
the next session does not read the plan and expect the code to match it:

- **Prepare is split** into `dispatchLandingPrepare` (loads mount state) and
  `landingPrepareFromState` (pure). The plan had one function. The reason is
  testability: exercising the freezing behaviour through the DB would have
  required standing up an accepted-seal spine fixture, and the loading half is
  already pinned by `TestLoadDispatchLandingMountStateMatchesSubjectSpawn`. The
  part with teeth — that every one of the tuple's members moves the preparation
  token — is now tested field by field over plain values.
- **`landingTupleProjection` returns an error.** The plan had it return the
  struct alone. Neither missing half has a safe default: with no assignment
  there is no branch, base, object format or repo identity to land against, and
  with no accepted seal the landing id loses its approved-run input, so an id
  derived over empty strings would be IDENTICAL for every task in the
  deployment — colliding both the container name and the `MERGE_MSG` trailer the
  abort path matches on. Defaulting there would have been a silent collision,
  not a loud refusal.
- **The role-blindness of `loadDispatchMountState` is now asserted**, not
  assumed. The landing wrapper synthesizes a role-less `dispatch.Spawn`, which is
  only honest while the loader ignores `sp.Role` entirely. That is true today
  (it reads `sp.SubjectID` at six sites and `sp.Role` at none), and
  `TestLoadDispatchMountStateIsRoleBlind` fails loudly if it stops being true.
  The disposition if it ever does: narrow the loader to `*int64`, do not teach
  the wrapper to lie.

The landing id moved from attest to prepare, and `landingid.go`'s header was
corrected in the same commit rather than left contradicting the code. The
earlier siting argued from availability; ADR-016:371 names the id as a member of
the candidate TUPLE, and a tuple member must be inside the preparation token or
commit cannot detect its drift.

Also worth not re-deriving: `landingWorkspaceRoot` is its own micro-step and its
own fence because `captureLandingPlan` resolves every host anchor RELATIVE to
the root it is handed. An empty root resolves them against the process working
directory — the single place in this lane where a landing could interrogate, and
then write into, a repository that is not the operator's. Letting a downstream
`Lstat` fail is not an acceptable substitute, because by then the probed paths
are real host paths.

Steps done: 1 canonical sibling (omitempty, golden vectors byte-identical);
2 `loadDispatchLandingMountState`; 3 `landingWorkspaceRoot`;
4 `attestDeploymentPreamble` (pure extraction); 5 the projections, the frozen
tuple, and `sealedLandingSubject`; 6 `dispatchLandingPrepare`.

NEXT (outgoing): micro-step 7, `dispatchAttestLanding` — routing-free, health
refusals via `refusal.CodeProjectionUnavailable`, plan literal
`{Version: 1, Entries: []{}, Landing: &plan}` because `validatePrivateMountPlan`
hard-refuses a zero Version or a nil Entries. Then 8-15, then the switch. Do NOT
run step 16 before 15 is green.

## 2026-07-21 — micro-steps 7-11: the lane is complete but unreachable

Attest, commit, the driver hook, the private-frame guard, and the prepare fork.
The sealed lane is now end-to-end code; the only thing missing is anything that
selects a sealed row, which is step 16.

**Attest reads no routing, and that had to be tested rather than asserted.**
`cli_test.go:1193-1206` looks like it guards this and does not: its fixture is
branch-carrying, so it satisfies `LandingPending` not `SealedLandingPending`,
returns from prepare as `final`, and never reaches any attest leg at all. It is
green by construction and would stay green if the sealed lane grew a routing
dependency tomorrow. `TestDispatchAttestLandingNeverReadsRouting` is the real
fence. `route` and `routingDigest` stay zero as the honest encoding of "no
routing input" — not an omission for someone to fill in later.

**Commit's fences are pure and tested one drift at a time.** The value is in each
firing for its OWN reason; a table that only proves "the set rejects garbage"
would pass with half the fences deleted. The host half is deliberately not
rechecked inside the transaction — the pre-commit re-attest covered it and D1
forbids a host read there. That is stated in the code because the absence looks
like an oversight.

**`RefusalSubjectlessPipeline`, not `RefusalSubjectTask`.** `domain.Block` is
reachable only from a candidate-class refusal naming a subject task, so the
subjectless kind makes a durable blocked row unreachable BY TYPE rather than by
an enumeration of which refusal codes happen to be stale-class today. The cost
is that a diverged target ref is loud only in the health detail text and not in
an indexed column; that trade was taken deliberately and is reversible.

Two things worth not re-deriving:

- **The pre-commit recheck's moved-tip test was passing for the wrong reason.**
  Before the selector landed, re-attesting a landing through `dispatchAttest`
  ALSO produced a stale refusal — so the test that was supposed to prove the
  fence proved only that something failed. `TestLandingRecheckAcceptsAnUnmovedHost`
  is the one that actually distinguishes them. A refusal-expecting test is not
  evidence until the success case is pinned beside it.
- **The first draft of the private-frame test was a tautology** — it
  re-implemented the guard inline and asserted on its own copy. The guard is now
  a named function (`privateFrameRefusesLanding`) so the test exercises real
  code. Worth remembering as a shape: a guard written inline at its call site is
  a guard that invites a fake test.

The private frame refuses a landing outright rather than serializing one.
`privateCandidateFromPrepared` is only ever handed `prepared.candidate`, which a
landing leaves nil, so the far side would receive a candidate whose `Spawn` is
nil and deref it unguarded. That guard is also the standing tripwire on the
lane's whole safety argument.

`dispatchLandingRound` is a separate function rather than conditionals inside the
spawn round, so no arrangement of flags can attest a landing and commit it as a
spawn.

Fixture note that cost a cycle: spine triggers require tasks to be born
`proposed` and to have a live Review Packet before approval. `dvInsertTask` plus
an explicit packet insert plus an UPDATE is the working recipe
(`dispatchverb_test.go:286-297`); a direct INSERT of an approved row is refused.

NEXT (outgoing): micro-step 12 (the sealed twin of the broken-routing test — no
production change), then 13 `LandReport` widening, 14 `LandingBranch()`, 15 the
resident's routing discriminator + absence gate + exit-code classification. Step
16 is the switch and MUST NOT run before 15 is green.

## 2026-07-21 (cont.) — micro-steps 12-15: the lane is built, the flip is unblocked

All fifteen inert steps are green. Everything except the two reachability edits
now exists, and nothing selects a sealed row.

**Step 12, the sealed routing twin.** Written with a NEGATIVE half: a spawn over
the same home must still refuse. Without it the test would pass just as happily
if the fixture's `routing.md` were valid, proving nothing about the landing.
Worth generalizing — a test asserting "X is not affected by Y" is only evidence
once something else in it demonstrates Y is really present.

**Step 13, `LandReport`.** The fence was "no branch ⇒ nothing was landing"; it is
now "no branch AND no assignment". The negative test is the load-bearing one: an
artifact-plane deliverable has neither home, approve archives it synchronously,
and no land effect ever exists for it — so the widening must not drift into
"anything approved can report a landing".

**Step 14, `LandingBranch()`.** A provable no-op today, since every row
`nextLanding` can select satisfies `LandingPending` and so has a non-empty
`tasks.branch`. It stops being one at the switch. Checked `mc/property` first
for a Land.Branch assertion, as the plan's risk 10 asked: there is none.

**Step 15 is the flip precondition and it closed two real holes**, both of which
would have converted infrastructure trouble into a durable blocked row —
precisely what ADR-016:576 forbids:

- The **confirmed-absence gate**. The landing id, and therefore the container
  name, is stable across attempts BY CONSTRUCTION — that stability is what lets
  a retry recognize its own merge — which is exactly why a crashed prior attempt
  leaves a container the next attempt collides with. The probe refuses to guess:
  name taken means this attempt never runs and never reports, and the row stays
  pending for a tick that finds it free. `mc/verbs` has no Docker client, so this
  could only be closed resident-side.
- **Exit-code classification.** The old code reported `--status failure` on ANY
  nonzero exit. Grounded in mc's contract (`main.go:91-107`), only exit 1 —
  domain rejection — is mc-land having looked at the repository and refused, and
  only that blocks. 2 is usage/environment; 125/126/127 are docker's own
  create/exec/not-found. None are evidence about the change.

The test that asserted the old any-nonzero-blocks behaviour was INVERTED rather
than deleted, because it encoded the bug. Same for the envelope tests' whole-object
`toEqual` on `docker.calls`: the absence probe adds a call, so they became a
per-call assertion plus a length check. The plan had listed those as "do not
touch", which was wrong once the gate existed — recorded so the next reader
trusts the code over that line of the plan.

NEXT (outgoing): step 16, the ATOMIC SWITCH, and nothing else in the same commit
beyond the tests that must move with it. Two edits:
  A. `mc/dispatch/dispatch.go` `nextLanding` filter → `t.LandingPending() ||
     t.SealedLandingPending()`
  B. `mc/domain` `Approve` stops refusing an assigned sealed row and takes the
     existing hold-for-landing arm instead
Neither works alone: A selects nothing while B keeps `decision` from reaching
approved, and B marks rows pending that nothing selects.
Tests that MUST move in that same commit:
  - `mc/domain/task_test.go:823` `assigned_sealed_task_refuses_rather_than_archiving`
    → INVERT to `assigned_sealed_task_holds_for_landing`. The fixture must gain
    `verified_sha` and `target_ref` or the substrate landing-fence trigger
    legitimately refuses it (shape: `mc/substrate/landing_fence_test.go:60-64`).
  - `mc/dispatch/dispatch_test.go:413-414` — COMMENT ONLY; assertions stay green
    because that fixture's `Sealed` is nil. Its stated INTENT is what goes stale.
  - ADD a `mc/cmd/mc/cli_test.go` end-to-end: assignment-backed approve →
    `mc dispatch` returns `action: land` with a `landing` key → `land report
    success` archives → re-dispatch returns no land.
MUST NOT RELAX: `TestNoLandingCellIsPlanAddressable`,
`TestPlanMountsRefusesEveryLandingCell`, `mc/dispatch/sealed_landing_test.go`,
`mc/substrate/landing_fence_test.go`.
Reversal is reverting the two edits; steps 1-15 all go inert again with nothing
to unwind, which is a direct consequence of the no-receipt policy.

## 2026-07-21 — step 16: the sealed landing lane is ON

Two reachability edits, in one commit with the tests that pin the behaviour they
change. `nextLanding` now selects `SealedLandingPending` rows alongside
`LandingPending` ones, and `Approve` no longer refuses an assigned sealed row.

The switch grew a third edit that the plan did not anticipate, and it matters.
`Approve`'s landing fence previously required `verified_sha` and `target_ref`
only of a BRANCH-CARRYING row. Opening the sealed arm without widening that
fence would have allowed an assigned row to be approved while missing the two
facts `SealedLandingPending` requires — leaving it approved, unarchived, and
permanently invisible to the landing lane. Silently unlandable is the exact
failure this whole slice exists to remove, so the fence now applies to both
branch homes. The v11 substrate trigger was already the backstop; the Go arm
names the rule and refuses with `CodeLandingFence` before reaching it.

`TestSealedLandingIsReachableEndToEnd` is the activation proof and it is the only
test that could have caught the two edits failing to meet in the middle. It
drives the REAL `Dispatch()` over a real spine and a real git Worksource:
approve through `domain.Approve`, then dispatch, and assert a land effect
carrying a `landing` key with the assignment's branch. It also asserts the two
properties a landing routed through the spawn machinery would have silently
broken — no lease taken, and no `runs` row beyond the accepted Worker seal's own.

Fixture facts that cost cycles and are worth keeping:

- The acceptance pointer is fenced to `status = 'worked'`
  (`tasks_accepted_completion_fenced`), so a sealed fixture must be walked
  through the real lifecycle — insert at `worked`, set the pointer, then advance
  verified → packaged. Dropping a row in at `packaged` and back-filling the
  pointer is refused.
- `workspace_root` lives on `sandbox_profiles`, not `worksources`.
- `runs` has `subject`/`tier`, not `subject_id`, and its `role` CHECK has no
  landing member — which is why the E2E asserts on the total run count rather
  than on `role = 'landing'`, a predicate that can never match by construction.

The inverted approve test is now `assigned_sealed_task_holds_for_landing`, and a
companion `assigned_sealed_task_without_landing_facts_refuses` covers the widened
fence in all three missing-fact shapes. `TestStep0c_ApprovedBranchlessRow_NeverLands`
kept its assertions and had its stated intent corrected: branchlessness alone no
longer implies the artifact plane, and that fixture qualifies only because its
`Sealed` is nil.

All five fast-lane legs green with the lane live. Reversal is still just
reverting the two reachability edits, with nothing to unwind — a direct
consequence of the no-receipt policy.

NEXT (outgoing): the Phase 3 completion lane (`PROGRESS.md` §2). The lane is
live in the fast suite but has never run a real container: prove the realized
landing mount table, RO alias/cover behaviour, the network-none/uid/capability
envelope, VirtioFS import durability, and the five ADR-017:758-760 crash cuts,
then carry the production E2E through packaged → approve → merge → archived and
satisfy `docs/phase3-contract.md` §8.

## 2026-07-21 — the completion lane opens, and two deferred canaries come back green

Docker E2E is 8/8 at `f21f11c` WITH the sealed lane live, and all three tag
compile/vet lanes are clean. Activation disturbed no existing crossing.

**Finding that changes the phase picture: `docker_boundary` has ZERO
implementing files.** No `//go:build docker_boundary` exists anywhere in the
tree; the only tagged suite is `docker_e2e` (`mc/e2e/e2e_test.go`). Verified by
grep, not taken on report. So the §8 lane
`go test -run '^$' -tags docker_boundary ./...` has been passing VACUOUSLY since
it was written — it compiles the untagged packages and finds no tests. Every §3
row whose required evidence names "Docker inspect", "in-container probes", or
kernel behaviour is therefore unproven, and §8's "run the real `docker_boundary`
suite at Phase 3 completion" names a suite that does not exist.

Spot-checked the rest rather than trusting the survey wholesale. Mixed picture:
`per-container scope` has a production owner (`runtime_scope_prod.go`), the warm
helper does (`main.go`, `resident_control.go`); but there is no forbidden-env
builder in `mc/boundary` and no gateway implementation anywhere — only refusal
codes and control-descriptor plumbing mention the word. Those are unimplemented
mechanisms, not merely untested ones.

**Two deferred canaries, run directly, both GREEN.** These are the landing
lane's architecture-kill risks and they were worth answering before building any
test scaffolding, because a red on either would have made the entire lane
non-functional on this Mac and every other completion test moot.

- **ADR-019:183-188's final-uid VirtioFS canary.** uid `10002:10002` CAN create
  a file inside a RW bind of an operator-owned host directory (0755, `501:0`)
  under Docker Desktop 29.4.0 / aarch64. This is the question ADR-017:76-86
  explicitly refused to assert anything about and deferred here.
  Better still, and NOT what the design assumed: the created file lands on the
  host owned by the OPERATOR (`501:0`), not by 10002. So a landing's merge
  commit produces operator-owned bytes with nothing chowning anything, which is
  why no permission-widening fallback was ever needed. `effects.ts:768-771`
  chose 10002 over an operator-uid exception on corpus grounds alone; that
  choice is now also empirically correct.
- **ADR-017:700's nested cover.** A RO bind of an empty directory at
  `/repo/source/.mission-control` genuinely SHADOWS the same path inside the
  already-RW `/repo/source` bind: a host sentinel under it is invisible from
  inside, a write into it is `EROFS`, and the host sentinel is intact
  afterwards. This is the single mechanism preventing the RW real-repository
  grant from exposing every sealed task store as writable, and it had been
  asserted by the ADR and verified by nothing.

That second one matters more than it looks. `fenceLandingCover`
(`landsealedrun.go:65-84`) only checks that the path is an EMPTY DIRECTORY — it
passes identically if the bind silently did nothing and the real
`.mission-control` happened to be empty. The fence is not evidence of shadowing;
the probe above is.

Neither canary is yet a test. They were run as direct `docker run` probes to get
the answer cheaply; committing them as `docker_boundary` tests is the next step,
and is what makes them regression evidence rather than one-time observations.

NEXT (outgoing): create the `docker_boundary` package — it does not exist — and
land the landing-lane rows in it, cheapest-first: the final-uid canary and the
resource-bounds/`--network none` inspect (one container each), then the cover
shadow and the realized mount table, then landing's host-scope/no-`run.json`
inversion. Two cheap host-side gaps can land first with no Docker at all: that
the landing class sets `no-new-privileges` while the setuid class must NOT
(a shared-envelope refactor would silently kill the setuid gate), and that
landing's own mount table is still governed by the shared blocked floor
(`landingplan.go:11-22` says outright that neither `planMounts` nor
`validatePrivateMountPlan` knows this table).

## 2026-07-21 (cont.) — the docker_boundary suite exists, and four rows are green

Created `mc/boundarydocker` (`//go:build docker_boundary`). It is the first file
in the tree carrying that tag, so the §8 lane for it stops being vacuous here.

Four landing rows green on Docker Desktop 29.4.0 / aarch64: the final-uid
VirtioFS canary, the nested cover's actual shadowing, the applied-not-requested
envelope inspect, and the `--network none` negative probe. The two canaries that
were run as ad-hoc probes last entry are now regression evidence.

The package's own reason for existing, stated in its doc comment so it is not
diluted later: an argv assertion proves a bound was REQUESTED; only an inspect
or an in-container probe proves it was APPLIED. The landing lane had argv-level
tests for its whole envelope and no evidence the kernel agreed with any of it.

**Fixture fact that cost a cycle and will cost another if forgotten.** Bind
mountpoint fixtures must NOT live under `t.TempDir()`. A directory under
`/var/folders` that has served as a nested bind mountpoint acquires a
`com.apple.provenance` xattr from Docker Desktop and then resists
`os.RemoveAll` with EPERM — so the test body passes and the harness fails the
test during teardown, which reads exactly like a real failure. Under
`/private/tmp` the same directory removes cleanly; emptying-then-`rmdir` works in
both. Verified in both locations rather than assumed. `sharedTempDir` exists for
this and says why. Deliberately NOT solved by ignoring the cleanup error, since
that would hide a real residue problem if one ever appeared.

Parked for the operator: `docs/phase3-contract.md` §3 requires a forbidden-env
builder and a gateway with three egress modes, and NEITHER is implemented — not
merely untested. There is no env builder in `mc/boundary`, and the only
occurrences of "gateway" in the tree are refusal codes and control-descriptor
plumbing. Every other §3 row has a real production owner. That is a scope
question, not an implementation choice, so it is a one-line request under
`## Parked` and the independent landing/envelope work continues meanwhile.

NEXT (outgoing): two cheap host-side rows that need no Docker — the landing
class sets `no-new-privileges` while the setuid class must NOT (a shared
envelope builder would silently kill the setuid gate), and landing's own mount
table is still governed by the shared blocked floor (`landingplan.go:11-22`
states outright that neither `planMounts` nor `validatePrivateMountPlan` knows
that table, so the sealed lane introduced a second validator row 1's named owner
does not govern). Then the remaining Docker rows: landing's host-scope /
no-`run.json` inversion, the realized four-row mount table against inspect, and
the sealed production E2E through `packaged -> approve -> merge -> archived`.

## 2026-07-21 (cont.) — the class divergence guard, and a row-1 hole under it

Row 7's divergence guard landed. The setuid gate depends on
`no-new-privileges` being ABSENT from the AGENT container so the setuid `mc`
binary can raise privilege to reach the spine; setup and landing never touch the
spine and are hardened by it (`effects.ts:334,678,774` set it, the agent
`create` at `:497-509` does not). Nothing asserted the two classes disagree, and
the refactor that would break it is an attractive one — three call sites repeat
the same hardening flags, so hoisting them into a shared envelope builder is the
obvious tidy-up, and it would kill the setuid gate with every test still green.

**The proposed row-1 test was based on a misreading, and checking first was the
right call.** The gap analysis proposed asserting that landing's mount table
runs through `planMounts`/`validatePrivateMountPlan`. `landingplan.go:11-22`
says the opposite deliberately: the `/repo` plane is NOT plan-addressable,
because the D5 carrier is an agent-plane carrier that structurally cannot reach
it, and teaching the validators a plane they cannot reach costs a
defence-in-depth layer for no capability. It also records that a first draft did
exactly that and review rejected it. Writing that test would have re-litigated a
settled decision from the losing side.

**But looking for the honest version of row 1 found a real hole.** Row 1 names
its production owner as "shared `mc/boundary` policy used by profile save AND
both sides of `mc dispatch`". The dispatch side exists (`mountattest.go:876`,
`mountplan.go:260` carry a `BlockPolicy`). The PROFILE-SAVE side does not exist
at all:

- `mc/verbs/init.go:85` inserts `sandbox_profiles.workspace_root` with no
  `boundary` call anywhere in the file — `grep 'boundary\.' init.go` is empty.
- There is no profile-save verb; `worksource.go` makes no `boundary` call either.
- `resolveLandingRoots` (`landingplan.go:177-217`) resolves the landing's host
  anchors through `boundary.ResolveSource` and its own canonical-path fence, but
  applies no `BlockPolicy`.

So the workspace root that anchors the landing's RW real-repository grant — the
strongest grant in the system — has never been checked against the shipped
blocked-pattern floor at any point in its life. This is pre-existing and not
landing-specific: every mount class uses that same root. Landing raises the
stakes because it is the one that makes it RW on the operator's real checkout.

Recorded rather than fixed in this pass, because the fix is a real behaviour
change (validate `workspace_root` against `BlockPolicy` at init/profile-save,
and decide what happens to an existing deployment whose root would now fail)
and it deserves its own red-first step rather than being smuggled into a
landing commit.

NEXT (outgoing): close the row-1 profile-save hole above, red-first — it is the
most valuable remaining non-Docker row and it is not landing-specific. Then the
remaining Docker rows: landing's host-scope / no-`run.json` inversion, the
realized four-row mount table against `docker inspect`, and the sealed
production E2E through `packaged -> approve -> merge -> archived`.

## 2026-07-21 (cont.) — the row-1 hole, correctly bounded and closed

**Correction to the previous entry.** It said the workspace root "has never been
checked against the shipped blocked-pattern floor at any point in its life".
That is wrong and was written without checking the agent plane. `planMounts`
DOES apply the floor: `in.Blocked.Rejects(id.RawClean, id.Canonical)` at
`mountplan.go:336`, and the policy is passed into `Authorize` at `:353`. The
agent plane has always been covered.

The real hole was narrower and sharper than the overstatement, and worth stating
precisely because the precise version is the one that explains WHY it matters:

- Agent plane: floor applied at dispatch. Covered.
- Landing plane: `resolveLandingRoots` applied no `BlockPolicy` at all.
- `mc init`: writes `sandbox_profiles.workspace_root` with no boundary call
  (`grep 'boundary\.' init.go` is empty), and there is no profile-save verb — so
  nothing vets the path at registration time either.

Which means the asymmetry ran exactly backwards. Landing binds "the only grant
in the system that gets a real Worksource repository RW, intentionally including
its primary checkout" (ADR-017:699) — the STRONGEST grant in the system — and it
had the WEAKEST source check. A Worksource registered under, say, a `secrets/`
or `.ssh/` component would be refused for every agent spawn and still bound RW
into a landing container.

Closed by applying the zero-value `BlockPolicy` in `resolveLandingRoots`,
matching `mountattest.go:876`: the shipped patterns are always evaluated, and a
landing has no operator extension of its own to honor. Red-first across four
blocked components, with a paired accept test — without that, a rejection bug
that refused every Worksource would have read as a passing fence.

Still open and deliberately not fixed here: `mc init` performs no boundary
validation of `workspace_root`. That is now defence-in-depth rather than the
only line, since both planes check at use time, and changing it is a real
behaviour change for existing deployments whose registered root would newly
fail. It belongs to the install/onboarding slice already named under "Known
later obligations".

The lesson worth keeping: the gap analysis proposed a row-1 test that would have
re-litigated a REJECTED design (teaching `planMounts` about the `/repo` plane),
and the previous entry then overstated the hole in the other direction. Both
were fixed by reading the actual call sites rather than reasoning from the
contract's prose. Check which plane a claim is about before writing it down.

NEXT (outgoing): the remaining Docker rows — landing's host-scope /
no-`run.json` inversion (`mc __land-sealed` calls `RequireHostScope`, and the
landing container deliberately carries no `run.json`, so its security argument
is the INVERSE of every other class: absent run.json means trusted; nothing
proves that holds inside the image, or that a run.json cannot be injected), the
realized four-row mount table against `docker inspect`, and the sealed
production E2E through `packaged -> approve -> merge -> archived`.

## 2026-07-21 (cont.) — the production image did not exist either

Row 8's landing scope inversion is green, and closing it required first building
an artifact §8 names and the tree did not have.

**Third structural §8 gap this session.** §8 requires "the production image
contains no fake route and has its own untagged build". The ONLY image in the
tree was `mc-fake-e2e`, built with `-tags test_fake_routing`
(`runner/image/build.sh:10`). So the row-7 setuid requirement — "under the real
untagged image and real production `mc`" — named an image that did not exist,
as did every other production-image claim. Added `build-prod.sh` and an
`MC_BIN` build arg, so the two images differ in exactly one thing: the tag on
the mc binary.

Evidence for §8's record:
- production image `mc-prod`: `sha256:8f12cc425a6d8f37e364b1627bb0e349a7fdbccf59035a25f58a57224a044a02`
- e2e image `mc-fake-e2e`: `sha256:04ab037a4039b3f4d7c546e8c9075c6b1dc93b187e3fdac281dc72de1ac57742`
- architecture: `arm64/linux` (native, no emulation, no --platform)
- Docker Desktop 29.4.0, aarch64

**A vacuous test caught before it shipped, and worth recording as a pattern.**
The first draft of the fake-route proof grepped the baked binary for a
`fake-decorrelation` symbol. It returned 0 for BOTH images, so it would have
passed for the fake build too — a green test proving nothing, of exactly the
kind this session has now hit three times. The real discriminator is behavioural
and self-validating:
- production: `invalid routing.md ... unresolved binding "fake"` — it refuses
  the binding, which is what denies fake spawn and mount authority.
- fake image: parses those same bindings and fails FURTHER ON, at
  `missing role "homie"`.
The control arm asserts the fake image does NOT reject the binding, so a fixture
malformed for an unrelated reason cannot make the production assertion pass.

**Row 8's inversion, now proved inside an image for the first time.** Every other
container class proves authority by the PRESENCE of `/mc/run.json`, which pins
the tier/role/run it may act as. The landing container's argument is the exact
inverse: it carries no run.json, `LoadIdentity` returns `(nil, nil)`
(`verbs.go:187-199`), and `RequireHostScope` passes only for nil. Three
directions pinned: no run.json ships in the image, host scope is actually
granted (a scope refusal would mean the landing class cannot execute at all),
and an injected run.json REVOKES it rather than being ignored.

**More evidence for the parked scope question.** §8 requires "`doctor` has no
Phase-3-owned deferred finding". Running `mc doctor` in the production image
returns three Phase-3-owned deferrals, verbatim:
- `container-runtime`: "capability probe (setuid gate, helper exec) runs in
  Phase 3 (§16.4, §17)"
- `gateway`: "egress gateway health probe runs in Phase 3 (§11.4, §17)"
- `runtime-auth`: "per-binding runtime-auth health runs in Phase 3/5"
(`supervision` is Phase 5 and correctly deferred.) So `doctor` itself agrees
that the gateway is Phase-3-owned and unbuilt — which is the parked question,
now with the tool's own words behind it rather than my inference from grep.

NEXT (outgoing): the realized four-row landing mount table against
`docker inspect` (the argv is pinned host-side but the applied table is not),
then the sealed production E2E through `packaged -> approve -> merge ->
archived`. The three §8 structural gaps found so far — no `docker_boundary`
suite, no production image, no gateway/forbidden-env — are all recorded; the
first two are now closed.

## 2026-07-21 (cont.) — the mount table is real, and the landing program has now run

Row 10 closed and the fixed landing program executed for the first time.

**Row 10, the realized mount table.** The resident compares docker ARGV to a
literal, which proves the table was REQUESTED. `docker inspect` now proves it
was APPLIED, and an in-container probe proves the access MODES actually govern a
process — the part argv can never show, since a bind requested `:ro` that the
runtime silently mounted RW is invisible to every host-side test in the tree.
The count assertion carries as much weight as the contents: "exact minimal
table" is a CEILING, so a fifth bind is a violation even with all four required
rows correct.

The mode probe is deliberately two-sided (it requires BOTH `SOURCE_RW` and
`TASK_RO`), so it cannot pass if the runtime made everything uniformly RO or RW.
After three vacuous-test traps this session, one-sided probes are no longer
trusted here.

**`mc __land-sealed` has now run inside a container.** It never had. Its entire
coverage was host-side unit tests of the functions it calls, which cannot show
that the baked binary, the mounted envelope, and the realized `/repo` plane
agree with one another. Walked by probe, it behaves exactly as designed: parses
the envelope, then refuses fail-closed at each fence with a diagnostic naming
what was missing.

  1. no cover → "landing cover is absent; the sealed bytes would be reachable
     through the source alias"
  2. cover bound → "sealed store config is unreadable: open /repo/task/git/config"

The test pins the ORDER, not merely the refusals. The cover is checked BEFORE
the sealed store is opened, so a landing never touches the reviewed bytes until
it has proved they are unreachable through the writable source alias. A
reordering would still refuse every malformed landing and would silently drop
that guarantee. The second arm asserts the cover fence STOPS firing and the
sealed-store fence takes over — which is how we know the first refused for its
own reason rather than because the envelope was malformed overall.

`mc/boundarydocker` now holds nine tests across four files, all green.

NEXT (outgoing): the one remaining landing obligation is the full
`packaged -> approve -> merge -> archived` walk with a REAL merge — the fences
above stop at the sealed store because building one needs a task-local bare
store carrying a reviewed commit whose closure digest and repo uuid match the
envelope. That machinery already exists in the e2e suite (`mc/e2e`, tag
`docker_e2e`), which drives the sealed pipeline to `packaged` today, so the walk
belongs there as an extension rather than as new fixtures in
`mc/boundarydocker`. It is the last row that would prove the lane does its JOB
rather than that it refuses correctly.

Still parked and unanswered: the gateway / forbidden-env scope question. `mc
doctor` in the production image names three Phase-3-owned deferrals
(`container-runtime`, `gateway`, `runtime-auth`), and §8 forbids any at
completion — so Phase 3 cannot be declared done regardless of how many landing
rows go green.

## 2026-07-21 — the lane is live on the wrong platform

The sealed landing walk found the thing every host-side test in this session was
structurally unable to see, and it is the most important finding of the slice.

**`mc dispatch` on Darwin routes through the PRIVATE HELPER FRAME, and that
frame refuses a landing candidate outright.** The resident log, verbatim, 240
consecutive ticks:

    mc dispatch failed (exit 1): mc: private helper __dispatch-prepare failed
    (exit status 1: mc: private prepare refused: dispatch: the private frame
    does not yet carry a landing candidate)

So the sealed landing lane — declared live at `d91e388`, green across the whole
fast suite and nine `docker_boundary` rows — is live on the NATIVE
single-process path only. On the platform this deployment actually targets it
cannot dispatch a landing at all. An approved sealed task sits at `packaged`
forever, burning one dispatch per tick.

The guard doing the refusing is correct and stays: `privateFrameRefusesLanding`
(step 10) exists because `privateCandidateFromPrepared` is only ever handed
`prepared.candidate`, which a landing leaves nil, so serializing one would hand
the far side a candidate whose `Spawn` is nil. Refusing loudly was the right
call. What was wrong was the ACTIVATION claim built on top of it: the seam
survey recorded the Darwin split as "a later slice over these same functions"
(dispatchseam.go:448-450), which is true of the SPAWN path and became false for
landing the moment the lane went live.

Worth stating plainly because it generalizes: every test that proved the lane
works called `Dispatch()` — the in-process entry point. The production path on
this platform is `DispatchPreparePrivate` over the one-shot control descriptor.
Both are "the seam", and a test suite can be exhaustive about one while the
other is dead. The E2E is the only thing in the tree that walks the real one.

**The Worker now commits.** Writing the walk exposed a second, quieter problem:
the sealed E2E's Worker sealed WITHOUT committing, so `verified_sha` equalled
the assignment's frozen base. Any landing merge would have been trivially
up-to-date, and the walk would have proved the lane runs without proving it
merges. The behavior now writes and commits a file, and the assertion requires
`verified_sha` to have ADVANCED past the base rather than to equal it — so the
behavior cannot silently stop committing and leave a no-op merge behind.

**Intended fix**, and it is a real slice, not a patch:
`PrivateDispatchCandidate` (`dispatchprivate.go:58-75`) gains a landing sibling
carrying `preparedLanding`'s fields — task id, landing id, the frozen
`landingCaptureInputs`, assigned ref, workspace root, tunables, token, mount
state. `PrivateDispatchPrepareResponse` gains `Landing *PrivateDispatchLanding
Candidate` beside `Candidate`, `privateCandidateFromPrepared` gains a landing
counterpart, and `preparedFromPrivate` a `"landing"` kind arm. Then
`DispatchAttestPrivate` and `DispatchCommitPrivate` must route the landing legs
(`dispatchAttestLanding` / `dispatchCommitLanding`) rather than the spawn ones.
`privateFrameRefusesLanding` inverts into the arm that BUILDS the carrier, and
its test inverts with it.

REPRO (~2 minutes, and it is the acceptance test for the fix):
re-add the landing walk after the packager assertions in
`TestProductionWorkerCompletionSealDockerBoundary` — approve, wait for archived,
assert `main` advanced, `main^1` is the prior main, `main^2` is the sealed
commit, and `main:WORKED.md` carries the Worker's bytes — then
`cd mc && mise exec -- go test -tags docker_e2e -run TestProductionWorkerCompletionSealDockerBoundary ./e2e/`

It is deliberately NOT in the tree as a red test: it lands in the same commit as
the carrier, because it is that carrier's acceptance evidence and passes only
with it.

NEXT (outgoing): build the private-frame landing carrier above, then land the
walk with it. Until that is done, "the sealed landing lane is live" is true only
of the native path and must not be stated without that qualifier.

## 2026-07-21 — the sealed landing lane MERGES, on the real platform

The private-frame carrier is built and the sealed landing walk passes. An
approved sealed task now goes `packaged -> approve -> merge -> archived` with a
real `--no-ff` merge into the operator's repository, dispatched through the
Darwin broker/helper split that production actually uses.

Three layers had to learn the landing, and the third is the one a
survey would have missed:

1. `PrivateDispatchLandingCandidate` — a SIBLING of the spawn carrier, for the
   same reason `preparedLanding` is a sibling of `preparedCandidate`.
2. All three private legs (`DispatchPreparePrivate`, `DispatchAttestPrivate`,
   `DispatchCommitPrivate`) select on the frame KIND rather than on a nil check,
   so a malformed frame cannot silently take the other lane's attestation. The
   landing needed its own attestation validator too:
   `validatePrivateAttestation` requires a resolvable routing digest on any
   non-refusal frame, and a landing attests no routing at all — requiring one
   would have been the same mistake as making the landing read `routing.md`.
3. **The BROKER CLI** (`mc/cmd/mc/private_dispatch.go:166`) had its own
   `Kind != "candidate"` gate. Nothing in the verbs package would have revealed
   it; the second E2E run failed with `malformed private candidate response`
   after the carrier itself was complete and correct.

**`preparedLanding` lost its tunables.** They were written at prepare and never
read — commit rebuilds the token from the FRESH selection's tunables, not the
frozen copy. Discovered only because the carrier had to serialize them and the
round-trip test failed on a bound check for a field with no reader. Carrying
dead state across a security boundary is worse than carrying it in-process: it
becomes something the helper must validate and an attacker can vary.

**What the helper does and does not re-validate**, stated because the asymmetry
is deliberate: it re-checks every scalar it will act on (task id, landing id
width, absolute workspace root, structural text, mount state) but NOT the
token's value. Commit recomputes the token from the tuple it received, so a
carrier that dropped or mangled a field fails on evidence the helper computed
itself, rather than on a shape check that a well-formed lie would satisfy.

**The lesson, and it is the session's most transferable one.** Every test that
proved this lane worked called `Dispatch()`, the in-process entry point. The
production path on this platform is `DispatchPreparePrivate` over the one-shot
control descriptor. Both are "the seam". A suite can be exhaustive about one
while the other is entirely dead, and no amount of host-side coverage will say
so — the fast lane, nine `docker_boundary` rows, and the whole sealed pipeline
were green while an approved task could never land. Only a test that walks the
REAL entry point can catch a dead entry point.

Also fixed in passing, and worth its own line: the sealed E2E's Worker used to
seal WITHOUT committing, so `verified_sha` equalled the frozen base and any
landing merge would have been trivially up-to-date. The walk would have proved
the lane runs without proving it merges. The Worker now commits, and the
assertion requires the SHA to have ADVANCED — so the behavior cannot silently
stop committing and leave a no-op merge behind. The walk additionally asserts
`main:WORKED.md` carries the Worker's bytes, because the merge-parent
assertions alone would pass on a merge carrying no tree.

Green at this commit: five-leg fast lane; `docker_e2e` 8/8 including the landing
walk; `docker_boundary` nine rows; all three tag vet lanes.

NEXT (outgoing): the remaining §3 row for landing is the orphan sweep's
`setup/landing/probe residue` clause — the `.cover` directory under
`MC_HOME/runs` is left behind by every landing on purpose (`effects.ts` defers it
to "a later tick responsible for landing residue", ADR-016:344-349) and that
sweep was never written, so every landing leaks one directory. Then §8's
remaining bookkeeping. STILL PARKED and still the completion blocker: the
gateway / forbidden-env question — `doctor` names three Phase-3-owned deferrals
and §8 forbids any at completion.
