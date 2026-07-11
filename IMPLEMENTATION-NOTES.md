# IMPLEMENTATION-NOTES — the deviation log

Append-only, newest last. Addressed to the operator: every judgment call
the agents made that the spec didn't cover. *Planned* designs the spec
delegates live in `docs/adr/`, not here.

Entry template:

```
## <date> — <one-line title>
- Where: <phase/test/spec § that surfaced it>
- Gap: <what the spec didn't cover or got wrong>
- Choice: <the conservative option taken, and why it is the conservative one>
- Spec impact: <sections whose text should change, or "none">
- Needs your decision: no | yes → also parked in PROGRESS.md
```

---

## 2026-07-10 — OPERATOR-INPUTS.md was committed with live secrets
- Where: handoff §1.1 / §4 (pre-session seeding); found at session start
- Gap: the handoff requires `.gitignore` covering `OPERATOR-INPUTS.md`
  before the first commit; the seed commits tracked it instead (MiniMax
  key, Discord bot token now in local git history)
- Choice: untracked the file (`git rm --cached`), added `.gitignore`,
  kept history intact after a history-rewrite attempt was declined; no
  remote will be created until the operator scrubs history or rotates the
  keys. Conservative: reversible, fail-closed (nothing leaves the machine).
- Spec impact: none
- Needs your decision: yes → parked in PROGRESS.md

## 2026-07-10 — db_schemas.sql not seeded
- Where: handoff §1.1 scaffold table
- Gap: the "substrate starting point" file is absent from the seed folder
- Choice: derive the substrate schema directly from spec §4/§5/§6 — the
  handoff already marks the file "starting point, spec wins", so building
  from the spec deviates least
- Spec impact: none
- Needs your decision: no (informational; supply the file if it exists)

## 2026-07-10 — docs/priors/ evidence reconstructed, not copied
- Where: handoff §1.1 (docs/priors/), §4.3 (trusted priors)
- Gap: no `poc/` folder exists anywhere findable and the Claude Code
  memory directory is empty; the trusted priors could not be copied
- Choice: wrote the three §4.3 priors as short notes marked RECONSTRUCTED
  from the handoff/spec text, without inventing detail beyond what those
  documents state. Conservative: preserves the "do not re-derive" intent
  while flagging reduced evidentiary weight.
- Spec impact: none
- Needs your decision: no (informational; drop original POCs in if found)

## 2026-07-10 — dev MC_HOME scratch path chosen by agent
- Where: handoff §4.3 (environment facts); OPERATOR-INPUTS.md "Paths"
- Gap: OPERATOR-INPUTS.md references "the scratch path above" but records
  none
- Choice: `~/.mc-dev-home` (outside every git tree, never
  `~/.mission-control`); recorded in OPERATOR-INPUTS.md. Conservative:
  out-of-tree avoids the §16.1 git-working-tree fence entirely.
- Spec impact: none
- Needs your decision: no
