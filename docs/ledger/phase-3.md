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
