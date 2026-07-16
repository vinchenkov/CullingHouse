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
