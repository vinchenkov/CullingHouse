# IMPLEMENTATION-NOTES — the deviation log

Append-only, newest last. Addressed to the operator: every judgment call
the agents made that the spec didn't cover. *Planned* designs the spec
delegates live in `docs/adr/`, not here.

**Write-mostly. This file is NOT a startup read** (AGENTS.md §1). Append
your deviations; read it only when a task names an entry or you are about
to re-litigate a decision you suspect was already made. If you are here to
"get context", you are in the wrong file: `PROGRESS.md` is the state,
`docs/ledger/` is the history.

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

## Entries (2026-07-10 → 2026-07-17)

Markers: **[S]** superseded/corrected by a later entry; **[owed]** carries
a live obligation, detailed under Standing obligations below; **[op]** has
an operator-facing item.

2026-07-10
- OPERATOR-INPUTS.md was committed with live secrets [S: closed 07-15, history was scrubbed]
- db_schemas.sql not seeded
- docs/priors/ evidence reconstructed, not copied
- dev MC_HOME scratch path chosen by agent (`~/.mc-dev-home`)

2026-07-11
- Phase 1b pins a skeleton-only `mc init` provisioning verb
- Worker records `tasks.branch`; Verifier records `verified_sha`
- Skeleton grade of §11.5 enforcement
- Skeleton routing: resident test config stands in for routing.md
- fake-harness tool-use truncation cap redefined as UTF-8 bytes with boundary back-off
- Two additive spine columns the contract's own pins require
- `mc init` takes no tick-interval flag
- MC_RUN_JSON env override for the run.json path
- Console schedule pinned "not configured" (hour-24 sentinel)
- Strategist proposals insert origin='autonomous', not 'agent'
- session_path is MC_HOME-relative ("sessions/<run_id>")
- mc/e2e (docker_e2e) not built in this work order
- TickDeps extends the contract's pinned bundle with `fs` and `config`
- MC_SPINE reaches the runner via container env at docker run
- run.json materialized inside the session dir [S: relocated 07-12]
- skeleton brief is a deterministic placeholder string
- no lockfile: bun refuses to write one for a zero-dependency package
- `mc lock get` + `mc run list` read verbs; contract §2 read-channel list corrected
- untagged doc.go in mc/e2e so the gate reads cleanly from the untagged toolchain
- image pins: Bun exact, tini exact-prefix via apt, git pinned as an enforced floor
- mc-land resolves the worktree path from git's own registration, not an argv path
- runner emits the first heartbeat at session-start, then every interval

2026-07-12
- run.json relocated outside the session folder; normal-exit removal deferred [owed]
- agent container named mc-run-<run_id>, not §11.1's mc-<worksource>-<run_id>
- Codex takeover audit found four Phase 1 invariant defects (all repaired red-first)
- Phase 2 wave-1 adds three temporary carrier fields
- Refinement judgment applies at the rally-ending verdict
- Refiner re-entry uses `mc complete --status seeded`
- `mc complete --correction-count` is accepted grammar with no writer
- Routing closure crosses the Phase 2 wave-1 directory fence
- Canonical routes refuse the fake-only execution skeleton
- Phase 2 Console targets the core dashboard pending surface config
- Active Homie bindings are unique per concrete surface place

2026-07-13
- Cross-harness takeover review of the Codex wave-2 surface (7 gaps, all fixed red-first)
- Corrupt stored Console timezone halts all free-lock dispatch (kept fail-closed)
- Cross-midnight Console publish consumes the next day (spec-inherent, log-and-go)
- Console content path is lexically validated; serving seam owes containment [owed]
- Homie/pipeline id disjointness is mint-time only
- A promoted operator initiative dead-ends while the wave verb is parked [op]
- Homie-issued interrupt leaves the container to the future orphan sweep [owed]
- Worksource add ships without the §5 connect-time advisory
- The only image build path bakes the fake-routing tag [owed]
- Quota-boundary onboarding was red and weakened §16.4/§17 (all defects closed red-first)
- Ambiguous container-stop failure awaits the orphan sweep [owed]
- Generic worktree assignment is not implemented by the skeleton [owed]
- Landing cleanup debt is visible but not yet a durable health record [owed]
- Forbidden-env wildcard is a scan shape, not the shipped deny floor
- Homie historical trace access preserves folder exclusivity [S: projection, same day]
- `open+audit` retains a control-address floor
- Homie trace projection supersedes individual file mounts [S: pipeline-only, same day]
- Helper uses a component label, not an agent tier
- A null-locator Homie preflight refusal is non-terminal [S: row resume, same day]
- Explicit row resume supersedes null-locator refusal suppression
- Shared trace projection contains pipeline traces only
- Standalone tasks use a sanitized task-local Git repository
- One stale-writer cleanup may precede Console or landing

2026-07-14
- Mount targets reject Unicode format/line separators, not just ASCII controls
- Takeover-review residue on the pure mount policy (informational)
- Standalone-task `/workspace` is an RO 0555 task root, not RW scratch
- ADR-017 mandates an initiative/child refusal ADR-016 cannot classify [owed]
- The ledger's "two explicit spec deviations" undercounts 20a1a50 (informational)
- ADR-018 describes separate user namespaces the target does not have [owed]
- ADR-018's preclaim probe budget permits a tick to outlast the reap bound [owed]
- ADR-019 sources its machine budget to the spec, not the handoff [owed]
- ADR-017's privileged seal/attachment roots required a host inode nothing could create (ADR amended in place)
- A new §4-class substrate rule: wave children work only after the plan review
- The Editor is the first non-operator-rooted writer of `decision = 'cancelled'`
- Role terminals open the spine before their role check (logged, not fixed)

2026-07-15
- Cross-harness takeover found ADR-021 steps 1–6 green over six missing witnesses (acceptance revoked, repaired)
- HOME volume-root detection keeps both device and Darwin mount-point evidence
- D8 generalizes absent-path protection beyond profile `denied_paths`
- Generated cover and sealed-pack kinds extend ADR-017's closed source-kind list [owed]
- Own-control ancestry requires explicit ownership association
- D2's hex key fences are pinned by two lengths, not one
- Spine uniqueness is declared as named indexes so it can migrate
- `meta.schema_version` must be stamped from the constant
- PROGRESS.md split into state + ledger; handoff §5 now describes a file that no longer exists
- D4's `field`/`summary` enumerations are authored; detail carries no free text [owed]
- `Authority` is modelled as a mount-namespace concept, not a field on every refusal
- The repo must never live in macOS's TCC-protected triad (moved to ~/dev/ai/homie)
- D4's Homie arm lands unfenced; the fence is vacuous, not missing
- `dispatch_key` is an input to the D4 transaction, not derived
- the D4 health action is one activity row; no outbox fan-out [owed]
- `homie.preflight_health` belongs to D3, not D4
- latent race: concurrent `mc onboard home` can refuse with "restore from backup" [owed]
- both harness autonomy postures declined by the operator; do not re-park [op]
- the secrets-in-history worry is closed; measured, not assumed [op]
- the Docker VM is 100 MiB under the spec's floor and ~600 MiB under ADR-019's peak [op: parked]
- correction: the Docker update pin was already satisfied

2026-07-16
- D3 pairing rules read as iff, not may-carry
- the launch-fence miss applies NO consequence (stale posture)
- homie.preflight_health: four interpretations under one sentence
- v2→v3 corrected in place after adversarial review; D2's fences share the BLOB hole [owed]
- ADR-016 D1 landed in its native single-process form
- the D1 frame narrows ADR-016 D2 where its inputs do not exist yet
- cross-harness takeover rejects the D1 frame's deferred fences
- fixed helper coordinates and timeout values for the private dispatch crossing
- private Worksource projection reserves a fixed frame partition
- mount attest lands fail-closed before its authorization effector [owed]
- jurisdiction errors carry source provenance from the constructor
- takeover review of the Codex range a1767cd..e423780 (partial: quota) [owed]
- authorization-carrier slice: representation choices and residuals [owed]
- resident binds ride docker CLI `-v` strings, not structured objects [owed]

2026-07-17
- the Git registry resolves live; no spine table
- four pins inside the typed task-local plan class [owed]
- the fake lane keeps an empty Git-control registry

## Standing obligations

Live debts assigned to named future slices. Each cites its entry by
date/title; delete a line here when its slice lands.

- **Production image build** (07-13, "The only image build path bakes the
  fake-routing tag"): the Phase 3 production image needs its own untagged
  `mc` build path; the fake tag stays confined to images named for it.
- **content_path containment** (07-13, "Console content path…"): every
  consumer of an outbox `content_path` must resolve-and-contain under
  `MC_HOME` before serving.
- **§11.6 orphan sweep** (07-13, "Homie-issued interrupt…" and "Ambiguous
  container-stop failure…"; 07-12 run.json normal-exit removal): the
  Phase 3 sweep closes the stranded-container window, the ambiguous
  docker-stop composed-failure window, and leftover `runs/<id>.json` files.
- **Durable cleanup-debt record** (07-13, "Landing cleanup debt…"): the
  System Health implementation must surface persistent Git residue durably;
  never turn a successful landing into failure meanwhile.
- **Worktree assignment + retry-safe e2e Worker** (07-13, "Generic worktree
  assignment…"): Phase 3 mounts must make "assigned worktree" concrete
  before canonical harness acceptance; the Docker e2e behavior must become
  retry-safe when crash recovery is promoted there.
- **ADR text fixes owed** (07-14/07-15 entries): strike "user" from
  ADR-018's separate-namespace lists; the preclaim-proof slice must measure
  the probe cost against §16.2's tick bound and amend one or the other with
  evidence; ADR-019's header should cite the handoff row and say GB;
  ADR-017 must reconcile its closed source-kind list with the generated
  cover/sealed-pack kinds ADR-021 provisionally defined; the
  initiative/child unsupported refusal needs a stable code (or a stated §10
  selection filter) before the boundary tier is wired.
- **D2 BLOB hole** (07-16, "v2→v3 corrected in place…"): the shipped
  activity/outbox hex fences (`dispatch_key`, `dispatch_request_id`,
  `event_destination_key`) lack `typeof` checks; close with a fence trigger
  in a later migration. Do not copy that CHECK shape without `typeof`.
- **Refusal.Message is never-log by documentation only** (07-15, "D4's
  `field`/`summary` enumerations…"): nothing enforces it yet.
- **Health/blocked outbox fan-out** (07-15, "the D4 health action…"):
  §15.5's "everything the system pushes at the operator becomes rows here"
  is not yet true of health events or blocked alerts; the alert-class
  resolver owner closes it for all paths at once.
- **Onboard concurrency guard** (07-15, "latent race…"): whoever next
  touches onboarding should make the ambiguous `bytes>0 && tables==0` spine
  state await/retry like the existing concurrent-provision paths, refusing
  only if it stays table-less.
- **Takeover findings (6)–(9) of a1767cd..e423780** (07-16): bound
  brief/description at admission and the mount plan bytes at attest (an
  oversized result can wedge dispatch after the 64 KiB broker cap); add
  skew tolerance to the helper's absolute cross-clock deadline; implement
  commit-side same-key result replay (ADR-016 D2); close the named coverage
  gaps (host-file recheck wiring, real capture snapshot, production
  helper/spine scope, fd-3 CloseOnExec, capture-stage attribution).
- **Authorization-carrier residuals** (07-16, "authorization-carrier
  slice…" and "resident binds ride docker CLI `-v` strings…"): D5's
  ACL-snapshot and protected-containment predicate halves; the D6 row for
  production non-repo Worksources' RO workspace bind; the D6 launch
  receipts (`mc run launch-bind`/`runner-started`); structured Docker
  Engine `Mounts` objects plus the after-create inspect verification —
  all owed by the production resident/selector/effector slices.
- **mount-attest health stops** (07-16, "mount attest lands fail-closed…"):
  the `mount.runtime_unappliable` stops for valid nonempty plans and real
  Git candidates are deleted when their named prerequisites (closed
  carrier/effector, typed registry arms) land.
- **Task-local plan pins** (07-17, "four pins…"): `git/config` stays an
  empty regular file until the setup slice's sanitized grammar;
  verifier/packager/refiner/editor/projection arms refuse until seal/setup
  materialization exists; the helper's closed destination set relaxes on
  D6's named edges only.
- **Operator items**: delete the stale "Secrets-in-history decision"
  paragraph in `OPERATOR-INPUTS.md` (07-15); raise the Docker VM allocation
  (12 GiB recommended; parked in PROGRESS.md, 07-15); do not file
  initiatives until the parked wave-model decision resolves (07-13).