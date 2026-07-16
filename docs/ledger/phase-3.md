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
