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
