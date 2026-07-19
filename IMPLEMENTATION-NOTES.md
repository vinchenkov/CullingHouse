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

## 2026-07-17 — takeover review of durable setup receipt slice
- Where: Phase 3, `d2f3e68..c27616e`, resident control handshake and first-task setup receipt.
- Gap: the new v5 receipt migration did not update the separately compiled resident handshake constant; receipt registration also trusted a caller-supplied owner UID.
- Choice: pin the resident handshake to schema v5 and require the receipt/attested root owner to equal the current host operator UID. The alleged empty-skeleton launch gap was refuted: `resolveTaskLocalSkeleton` requires all fifteen populated rows, while the resident creates only `source/` and `git/`, so the next dispatch health-refuses before emitting a Worker plan.
- Spec impact: none.
- Needs your decision: no.

## 2026-07-17 — takeover review of the setup inspection range
- Where: Phase 3, `c27616e..9c5d6c3`, first-task setup repair and inspection seam; spawned two-lens review with adversarial verification (5 confirmed, 0 refuted, deduping to 3 defects).
- Gap: (1) major — `InspectFirstTaskSetup` never re-bound the walked fifteen-row table to the durable receipt's device/inode, so a same-path root swapped between the attest stat and the resolver walk returned as receipt-attested; (2) minor — the new inspection test's un-restored `chmod 0700` made the mode gate refuse first, leaving the deleted-cover arm untested (a delete-mutant of the `os.Remove` survived); (3) minor — the repair commit's attest-side `os.Getuid()` clause has no killing test.
- Choice: (1) fixed — the walk half is now `inspectFirstTaskTable`, which refuses unless the walked `KindTaskRoot` row carries the receipt's exact device/inode/owner tuple, with a same-path swap test that fails without the check; (2) fixed — the test no longer chmods the root, so the missing-cover refusal is the operative trigger; (3) retained and logged only: the clause differs from the receipt-equality check only when the process uid changes between register and attest, which an unprivileged test cannot construct; adding a uid-function seam to a trust check for testability was judged the less conservative option. The residual attest-to-walk stat window inside one call remains the documented ADR-016 D5 TOCTOU posture (narrowed, not closed).
- Spec impact: none.
- Needs your decision: no.

## 2026-07-17 — first-task setup closure writer pins
- Where: Phase 3, `WriteFirstTaskSetupClosure` (`mc/verbs/taskclosure.go`), ADR-016 D5 / contract's trusted-setup population.
- Gap: D5's closure extraction runs in a setup container and its pins live on the immutable Run record; neither exists yet, and the spec does not say what the host-side writer half may accept meanwhile.
- Choice: the writer consumes an already-extracted closure as exactly one `pack-<hex>.pack` + matching `.idx` pinned by a canonical sha256 digest (golden-vectored, name-covering, recomputed over the landed bytes after write); the digest pin is CALLER-SUPPLIED until the Run-recorded pin columns land [owed: the setup-container extraction slice must replace the caller pin with the Run's]; any residue in the resident skeleton refuses without cleanup — D5's exact retry-residue acceptance is owed to the same later slice, since its proof set (store identity, repository UUID, sole branch, base SHA) does not exist yet and a weaker partial proof would pretend to be the real one; generated covers are written 0444/0555, interior directories 0755 (spec silent on host modes; reversible); admitted closure bytes are fenced at 1 GiB before hashing. The writer has no production caller yet, like `applyRefusal` before it.
- Spec impact: none.
- Needs your decision: no.

## 2026-07-17 — dispatch-attest task skeleton gated by a task-keyed receipt, not run-keyed InspectFirstTaskSetup
- Where: Phase 3, `mc/verbs/mountattest.go:489` (the standalone-Worker typed-plan arm), against the standing NEXT "route the typed task-plan derivation through the receipt-fenced inspection."
- Gap: NEXT named `InspectFirstTaskSetup` as the gate, but that wrapper (`ReadFirstTaskSetup`, `tasksetup.go:122-132`) fences the receipt to the CURRENT lock holder — a live, non-ended pipeline/worker run whose id equals `lock.run_id`. At spawn dispatch-attest the lock is FREE and the candidate run id (minted by `newRunID()` at prepare) is not yet a live run, so neither the candidate nor any prior setup run's id can satisfy it. The run-keyed wrapper is the resident/setup-container's post-claim consumer; it is unsatisfiable at dispatch-attest. The line-489 arm is the retry/resume-residue case: a skeleton already materialized by a prior (now-ended) setup run, whose durable `task_setup_receipts` rows persist but hold no lease.
- Choice: the conservative realization of NEXT's intent ("only receipt-bound, setup-completed roots enter an agent plan"). A new task-keyed projection `substrate.LoadSubjectTaskSetupRoots(taskID)` reads the DISTINCT receipt identities for the subject task (no live-lease fence — appropriate for a projection), frozen into `DispatchMountState.SubjectTaskSetupRoots` at prepare (`loadDispatchMountState`) so it rides the existing token / commit-`DeepEqual` / `plan_digest` fences with NO unlocked spine read added to the host-file-only attest seam. The arm admits the resolved `KindTaskRoot` only when its device/inode/owner is a member of that frozen set (reusing `inspectFirstTaskTable`'s exact tuple encoding); a materialized-but-unattested skeleton (e.g. an attacker-planted well-formed tree at the expected path) health-refuses `mount.runtime_unappliable`/deployment. This only tightens what enters an agent plan (fail-closed), keeps every spine read under the flock, and adds no invariant surface. `InspectFirstTaskSetup` stays the resident/setup-container caller. The gate narrows but does not close the documented ADR-016 D5 attest→bind TOCTOU (a same-path swap after the frozen identity remains the pre-existing residual, still fenced by the resident's `__mount-recheck`). The helper-boundary validator (`validatePrivateMountState`) mirrors the receipt table's CHECKs (canonical decimal device/inode <=20 bytes, uid >=0, sorted+deduped, bounded at 64) so a hostile private frame cannot smuggle the set past the token.
- Spec impact: none.
- Needs your decision: no.

## 2026-07-17 — first-task setup-container extraction: architecture of record
- Where: Phase 3, the standing NEXT (first-task setup-container extraction slice; the closure writer's first production caller). Design locked via a decorrelated 3-proposal + judge deliberation before any code.
- Gap: two things the spec constrains but does not mechanize. (1) ADR-017:437-478 has the setup action write the full task-local store IN PLACE into the mounted-RW `source`/`git` children, in a `network=none` container with NO spine; but the existing host-side `WriteFirstTaskSetupClosure` needs the spine (`db`), writes only a 15-row skeleton + a caller-supplied pack O_EXCL into EMPTY children, and pins `git/config` empty — all incompatible with the ADR end state. (2) NEXT's parenthetical says to record "plan_digest/store identity/UUID/branch/base-SHA columns", but ADR-016 D6:733-747 explicitly DEFERS `runs.plan_digest`/`current_container_id`/launch-bind/runner-started to the later launch-fencing slice.
- Choice, container/host division (the ADR-least-deviation reading): the sanitized closure extraction AND the full-store materialization run in the spineless setup container, writing the store in place (pack, `refs/heads/mc/task-<id>` at the pinned SHA, `HEAD`, generated non-empty RO config, index, materialized `source/` tree; fsck-clean self-check — ADR-017:456-475). After the container exits, the HOST does all spine work: re-attest the receipt-fenced root, re-digest the landed pack, register the durable assignment, run the joined receipt+row inspection. `/mc/setup-result.json` is a claim the host re-verifies against the landed bytes, never the trust root. The staging-plane alternative (borrowing the completion-seal idiom for setup) was rejected: ADR-017 uses staging for completion (:500), not setup. `WriteFirstTaskSetupClosure` splits into a spine-free in-container `MaterializeFirstTaskStore` and a host `RecordFirstTaskSetupClosure`; `firstTaskClosureDigest`/`AttestFirstTaskSetupRoot`/`recheckLandedClosure`/`InspectFirstTaskSetup` are kept and shared. The 15-row mount table is unchanged (HEAD/refs/index/source-tree are content inside the RW binds, not new mount rows); only `git/config` flips empty->closed-grammar generated config, validated by parse (not a byte-pin) so the spine-free dispatch-attest resolver stays self-contained while the spine-present inspector does the exact ref==base_sha / HEAD / digest / object-format checks.
- Choice, pins (fail-closed reading of D5:620 over NEXT's parenthetical): the closure/assignment identity is recorded in a new TASK-keyed immutable `task_assignments` table (v5->v6), not `runs` columns and not run-keyed. D5:620 ("a retry reuses that assignment rather than rebasing to a moved target") is the priority-1 fail-closed invariant; keying by the entity that outlives a run (the task) structurally forbids the rebase a run-keyed copy-forward would reintroduce. D5:617 ("the immutable Run records ... assignment identity") is honored because each run's existing run-keyed `task_setup_receipts` binds run->task->root and the fenced inspection cross-checks the landed store against the task assignment. NEXT's "plan_digest ... columns" is superseded by D6:733-747; this slice adds NO launch columns to `runs` and leaves `mountPlanDigest` in-memory. Reversible: the table swap is isolated to `taskassignment.go` + the migration.
- Deferred to named later slices (safe — each is a distinct D6:724 closed-union member or has its own NEXT): actual in-container execution under Docker (phase-completion lane; the fast lane proves the identical Go via a host-git seam), accepted-seal rebuild, Worker-retry reconciliation beyond first-task residue, Verifier disposable-source, committed-tree projection, structured Engine-API binds, launchd, and the launch-fencing `runs` columns. D5 exact retry-residue acceptance lands WITH this slice.
- Spec impact: none (ADR-016 D5's "immutable Run records" is realized as a task-keyed assignment cross-checked by the run-keyed receipt; if operator review later prefers run-keyed, note it here).
- Needs your decision: no.

## 2026-07-18 — D6 mounted completion-seal publication marker
- Where: Phase 3, ADR-017:497-507 and :666; `SealTaskCompletion`'s privileged Worker wrapper path.
- Gap: ADR-017 requires the exact pre-created `MC_HOME/seals/<run-id>/` directory to be bind-mounted at `/mc/private/completion-seal`, while also describing an atomic publication of that directory. A bind mount is a mount point and cannot be replaced by `rename(2)`, so the directory-rename publisher cannot run at that exact destination.
- Choice: retain the existing directory-rename publisher for non-mounted callers, and add the fixed-root form for the actual Worker mount: build and fsync the exact verified pack/index and manifest in a private child staging directory; make files read-only; move only the pack/index into the already-gated root; fsync; move `manifest.json` last as the sole completion marker; then make the root read-only and fsync it. The model cannot traverse `/mc/private`; no setup consumer can inspect the root until the subsequent receipt is accepted; any interrupted publish has no manifest and no durable publication row, so it is non-authoritative and cleanup-eligible. This is the least deviation compatible with Docker's mount semantics and preserves the receipt/identity/digest fences.
- Spec impact: clarify ADR-017's "atomically publishes the seal" at :503 to mean atomic consumer admission through the final manifest plus accepted receipt when the exact seal directory is a bind mount; a directory rename remains the implementation for an unmounted source.
- Needs your decision: no.

## 2026-07-18 — production Worker completion-seal E2E: adapter authorization + image uid reachability
- Where: Phase 3, the standing NEXT (dispatch a production Worker through the real resident on the run-keyed completion-seal plan carrier; `mc/e2e/e2e_test.go` `TestProductionWorkerCompletionSealDockerBoundary`). Design grounded in ADR-016 D6, `docs/phase3-contract.md:178/207/242-251`, and a spawned read-only design-intent review.
- Gap: the seal + typed task-store plan attach ONLY on a non-fake (production) route (`mountattest.go:859`, gated by `route.Harness != "fake"`), and the ledger (`docs/ledger/phase-3.md` 2026-07-18) deliberately excludes the fake compatibility route from the seal descriptor. But the shipped image carries only the fake agent-runner adapter, and BOTH the resident (`effects.ts:392`) and the in-container agent-runner (`runner/agent-runner/main.ts:97`) refused every non-fake route, so no launchable route could carry the seal end-to-end. Three concrete blockers surfaced only by driving the full loop, none reachable by the resident's mocked-Docker unit tests: (1) resident launch gate refuses non-fake; (2) in-container agent-runner refuses non-fake; (3) `bun` — the runner runtime — is installed under `/root/.bun`, and `/root` is `0700`, so the model uid (10001/10002) that a production Worker runs as cannot exec it (`exec bun failed: Permission denied`). Blocker 3 had never been exercised because every walking-skeleton role runs as root.
- Choice: Design B (authorize the fake adapter to stand in for named non-fake routes), NOT Design A (let the fake route carry the seal). B is what the ledger's repeated "production (non-test-fake) resident Worker path" intent requires, and it keeps the deliberate fake-route seal exclusion intact. Every authorization is fail-closed by default: (1) a new `ResidentConfig.agentRunnerRoutes` allowlist — absent ⇒ only `fake/fake` launches, exactly as before; a malformed value is rejected at load, never degraded; (2) the resident passes the same allowlist into the container as `MC_AGENT_RUNNER_ROUTES`, and the agent-runner honours it identically (unset ⇒ fake-only); (3) `bun` installs to `/opt/bun` (world-traversable) with `chmod -R a+rX`, symlinked at `/usr/local/bin/bun`. Fix 3 is not merely a test affordance: a real production Worker runs its harness as the model uid, so a `/root`-hosted runtime is a genuine image defect this E2E caught. The E2E routes the Worker to `codex/chatgpt` (a real non-fake binding, so the Go attest is unchanged and the seal attaches on its own), authorizes that one route in both allowlists, and seeds the resident's own spine (a new `withHostBindSpine()` `setup()` option, bind spine instead of a named volume) with a materialized canonical store + receipt + assignment so the real timer dispatches the sealing Worker directly. Because task 7 is assigned, reaching `worked` is unreachable via the legacy unsealed `--status worked` bypass — so `status=worked` alone proves the sealed acceptance fence; the immutable 0444 manifest is confirmed inside the Linux namespace (Desktop projects a different host-side mode). No Go dispatch/attest change was needed.
- Spec impact: none for the boundary contract. ADR-019/017 uid posture is honoured more faithfully (the runtime is now reachable by the model uid). `agentRunnerRoutes` / `MC_AGENT_RUNNER_ROUTES` are test/dev deployment affordances for the fake adapter; a production deployment leaves them empty and ships real per-harness adapters (consistent with ADR-007 keeping the fake family out of production).
- Owed to later slices (out of scope here, logged not fixed): the resident-driven Verifier accepted-seal REBUILD after the Worker seals refuses `spawn refused: accepted-seal rebuild has no canonical task-root bind (fail-closed)` — the resident-effected rebuild path is not yet wired (it was proven directly by `TestAcceptedSealRebuildDockerBoundary`, not through the resident). This test asserts only the Worker completion fence and deliberately does not assert a free lock at rest, since that downstream churn owns the lease afterward.
- Needs your decision: no.

## 2026-07-19 — the accepted seal's recorded identity is namespace-local: host recheck moves to custody
- Where: Phase 3, the carry-through slice step (a); `resident/src/task-skeleton.ts` `recheckAcceptedSeal`, consumed by `effects.ts:301` before the accepted-seal rebuild setup container binds `MC_HOME/seals/<run>`.
- Gap: ADR-017's completion-seal crossing has the resident repeat the receipt's exact `device/inode/owner_uid` immediately before that bind. But those bytes are recorded by the in-container setuid publisher (`completionsealpublish.go`'s `os.Lstat` of the `/mc/private/completion-seal` bind), so they are namespace-local — the probe on 2026-07-18 read `seal_dev=48 seal_ino=… seal_uid=10001` while the host sees its own device numbers and operator uid 501. A host-side lstat therefore can NEVER equal them on Docker Desktop, so the non-fake path refused every rebuild at re-attestation. `TestAcceptedSealRebuildDockerBoundary` dodged this only because it publishes host-side in-process (`verbs.SealTaskCompletion`).
- Choice: the resident now proves *custody* rather than *identity* — the path is still derived locally (`MC_HOME/seals/<run>`, never accepted from an effect), must `realpath` to itself, must be a non-symlink directory, and must be owned by the uid this resident runs as (`process.getuid()`, the same host-operator test `precreateTaskSkeleton` already applies). The receipt's `device`/`inode`/`owner_uid` are still grammar-validated (well-formed decimals, non-negative uid) and still travel in the plan/envelope; they are simply not compared against a host stat. This is the conservative option of the three available: (a) it deviates least — one comparison drops, no schema, plan, envelope, or verb signature changes, and the check is restored by reverting a single `if`; (b) it preserves the fail-closed posture, because seal integrity was never carried by that stat — it is carried by the immutable manifest digest bound in `completion_seals` plus the in-image pack/index digest verification, which is unchanged; (c) it mirrors the precedent already set at the task-root crossing on 2026-07-18, where Docker Desktop's namespace-local device/inode values moved identity comparison to the resident's pre-bind boundary. Rejected: teaching the publisher to record host identity (it cannot see it), and passing host identity inward (a spine-blind resident would then be the authority for bytes the spine records — strictly worse).
- Cost, stated plainly: this weakens defense-in-depth. A host-operator-owned same-path replacement of the seal directory between acceptance and the rebuild bind is no longer caught by the resident. It is still caught downstream — the rebuild verifies the manifest digest, the pack/index digests, the sealed SHA/tree/object-count, and fsck — so a swapped seal cannot produce an accepted rebuild receipt; it fails later and louder instead of earlier.
- Owed: `verifyAcceptedSealIdentity` (`verbs/acceptedsealrebuild.go:163`) makes the identical comparison INSIDE the rebuild setup container, against the same namespace-local receipt from a different container's namespace. Whether that survives is unknown until step (c) drives the resident-effected rebuild under Docker; it is left untouched here deliberately, so the E2E rules on it with evidence rather than this slice ruling on it by argument.
- Spec impact: ADR-017's seal re-attestation should say the host-side repeat is path derivation + non-symlink directory + host-operator ownership, and name the manifest/pack digest chain as the identity guarantee across the namespace boundary.
- Needs your decision: no.
