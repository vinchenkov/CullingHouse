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
- Correction, same day, before this entry was ever read: its first draft recorded an "owed" item claiming `verifyAcceptedSealIdentity` (`verbs/acceptedsealrebuild.go:163`) makes the same doomed comparison inside the rebuild setup container. It does not. `RunAcceptedSealRebuildSetup` (`setupenvelope.go:224`) already passes `verifyIdentity=false` for exactly this reason, and has since the rebuild-core slice. So the container side reached this conclusion first; this slice only brings the host side into agreement with it. The stale comment there — which asserted the resident still re-attests the host tuple — is corrected in the same commit.
- Spec impact: ADR-017's seal re-attestation should say the host-side repeat is path derivation + non-symlink directory + host-operator ownership, and name the manifest/pack digest chain as the identity guarantee across the namespace boundary.
- Needs your decision: no.

## 2026-07-19 — the setup-record spine half trusts host-attested identity
- Where: Phase 3, the carry-through slice layer (c); `mc/cmd/mc/setup_record_frame.go`, `mc/verbs/setuprecordsplit.go`. Surfaced by `TestProductionWorkerCompletionSealDockerBoundary`.
- Gap: `mc task setup-record` and `mc task accepted-seal-record` must read HOST files and write the spine, but on Darwin the spine is reachable only inside the helper container, which has no Worksource bind. The spec/ADRs describe each as one verb and do not say which side of the delegation boundary its filesystem attestation lives on.
- Choice: split each verb — the host frame attests (canonical Worksource, the fixed non-symlink mode-0555 operator-owned task root, `crossCheckLandedStore`, and the 15-row skeleton walk for the first task) and hands the delegated spine frame device/inode/owner identity and never a path. This is the conservative option: (a) it preserves every existing check and the fail-closed posture — the spine half still binds the identity to the live run/task lease, to `receipt.TaskID`, to `receipt.Root`, and (for the rebuild) to the task-pointed accepted seal, so a wrong or hostile `--task` only fails closed; (b) it deviates least, because it is the idiom `mc task setup-register` and `mc __mount-recheck` already use, and the in-process compositions keep their names, messages and behavior (the verbs suite passed unchanged); (c) it is reversible — the split is two functions and a routing `if`. Rejected: binding the Worksource into the helper, which is not merely worse but unsound, since Docker Desktop exposes namespace-local device/inode across a bind and an identity attested in the helper could never match the resident's host registration (the defect 690fb08 fixed for the accepted seal).
- Cost, stated plainly: the spine half no longer independently proves the landed store exists. A host-scope caller can invoke `task setup-record-attested` directly with `--device/--inode/--owner-uid` and, if those equal the durable receipt, record a closure assignment without any store having been materialized — the `crossCheckLandedStore` proof is host-side only. This does not widen the trust boundary (`mc task setup-register` already accepts raw identity with no filesystem proof at all, and `RequireHostScope` still rejects every run.json pipeline identity, so no agent container reaches it), but it does move one proof from "the spine verified it" to "the host asserted it". The TOCTOU window between attest and commit also widens from in-process to one `docker exec`; the class of exposure is unchanged, since attest and `RegisterFirstTaskAssignment` were already separate transactions.
- Also: `mc task setup-record`'s success envelope no longer carries `rows` (the 15-row walk is host-side and its count is not a spine fact). Nothing consumed it — the resident checks only the exit code, and the D5 boundary test uses the Go API, which still returns the rows.
- Spec impact: ADR-016 D5/D6 should say that a setup-record crossing is a host attest frame plus a path-free spine frame, and that filesystem identity is observed only on the host — the same rule the accepted-seal recheck entry (2026-07-19) asks for at the seal crossing.
- Needs your decision: no.

## 2026-07-20 — the lock-domain guard's filesystem allowlist excludes ZFS and friends
- Where: Phase 3, `mc/substrate/lockdomain.go` (Inv. 24 guard, carried from
  spike S5 row 5)
- Gap: S5 proved the guard shape and named four filesystems —
  ext4/ext3/xfs/btrfs with a `/dev/` source — but neither the spike nor the
  spec says what to do about single-kernel filesystems outside that list.
  On a Linux HOST (spec §12: no helper, `mc` runs natively) a ZFS root
  reports `fstype=zfs source=rpool/ROOT/...`: no `/dev/` source, so the
  guard refuses it. Same for f2fs and bcachefs. All are single-kernel and
  perfectly SQLite-safe, so this is a false refuse — and by design there is
  no escape hatch, so such a host has no way forward.
- Choice: keep the four. It (a) preserves the fail-closed posture — the
  cost of a wrong ACCEPT here is spine corruption, the cost of a wrong
  REFUSE is a startup error naming the mount; (b) deviates least from the
  spike's proven text; and (c) is trivially reversible — widening a
  map literal, with the existing table tests as the harness. The primary
  target is this macOS machine (handoff §4.3), where the spine is inside
  the Docker Desktop VM on ext4 and the question does not arise.
- Spec impact: none yet. If Linux-host support becomes a real target, the
  allowlist needs a considered answer for pooled/COW filesystems whose
  source is not a `/dev/` node — note that the `/dev/` requirement is doing
  independent work (it is what rejects a bind of somebody else's ext4), so
  widening cannot just drop it.
- Needs your decision: no

## 2026-07-20 — a sealed task cannot land, and approving one archives it silently
- Where: Phase 3, extending `TestProductionWorkerCompletionSealDockerBoundary`
  past `verified` (the Packager mount-arm slice). Surfaced while scoping the
  second half of the outgoing `NEXT:` — "carry the E2E through the packet
  decision and land".
- Gap: the whole landing path is still the legacy `.mc-worktrees` model, and
  the seal pipeline never joins it. Two separate facts, the second worse than
  the first.

  (1) **`mc-land` can only merge a ref that already exists in the real repo.**
  The `Land` payload is four scalars (`dispatch.go:303-308` — task, branch,
  verified_sha, target_ref); the resident binds exactly one mount, the real
  repo root RW (`resident/src/effects.ts:696-711`); and `runner/image/mc-land`
  hard-fails `missing branch` (`mc-land:278-295`) when `refs/heads/mc/task-<id>`
  is absent. For a sealed task the reviewed commit lives ONLY in the task-local
  bare store at `<worksource>/.mission-control/tasks/task-<id>/git`, which
  mc-land never mounts or reads. ADR-017:1226-1240 specifies the replacement
  (import the reviewed closure, CAS-create the real ref, SHA-fence, merge in
  the primary checkout, exact-clean); NOTHING implements it. Its four typed
  mount kinds are declared with zero producers — `KindLandingWorksource`,
  `KindLandingMissionControlCover`, `KindLandingTaskRoot`, `KindLandingEnvelope`
  (`boundary/typedkind.go:110-113`), referenced only by a string-table test.

  (2) **A sealed task never reaches landing-pending at all, so approving one
  archives it as if it had landed.** `LandingPending()` requires
  `tasks.branch != ""` (`dispatch.go:129-132`). `tasks.branch` has exactly one
  writer, `complete.go:163`, reachable only through the `--status worked
  --branch` Worker terminal — which `complete.go:128-134` closes to assigned
  (sealed) tasks by design. The sealed branch name lives in
  `task_assignments.branch`, a different table `LandingPending()` never reads.
  So a sealed task is `branch = NULL` forever, and `domain.Approve`
  (`task.go:422-427`) treats a branchless task as an artifact-plane deliverable
  and archives it synchronously. Net effect: the operator approves a merge,
  the task disappears, main is never touched, and nothing errors. Inv. 25 says
  merging always requires operator approval; it is silently the case here that
  approval never produces a merge.
- Choice: log and stop at `packaged`. The E2E now proves the Packager arm
  end-to-end and ends there; it deliberately does NOT drive `packet decide
  --approve`, because asserting today's behavior would encode the silent
  archive as expected and make the real fix a test-breaking change. This is the
  conservative option: it (a) preserves the fail-closed posture by not building
  a landing path on a guessed design, (b) deviates least — sealed landing is
  ADR-017's delegated design and deserves its own red-first slice, and (c) is
  trivially reversible, being an absence of code.
- Spec impact: none to the spec. ADR-017:1226-1240 is unimplemented, not wrong.
  The `LandingPending`/`tasks.branch` join is the part no document covers: the
  seal pipeline introduced a second branch home and nothing reconciled the two.
  The landing slice must decide whether the sealed branch is projected into
  `tasks.branch` at acceptance or whether `LandingPending()` learns to read the
  assignment — and until then, `Approve` on an assigned task should arguably
  refuse rather than archive.
- Needs your decision: no — but the silent archive is the sharpest edge found
  this phase, and the landing slice is now the top `NEXT:`.

## 2026-07-20 — the landing mount table has two producible rows, not four [S: the ROOT-RESOLUTION half stands; the plan-grammar half is superseded by the entry below]
- Where: sealed-landing steps 1-2 (`mc/verbs/landingplan.go`), ADR-017:699-702.
  The outgoing `NEXT:` in PROGRESS.md planned "producers for the four typed
  kinds `KindLandingWorksource`, `KindLandingMissionControlCover`,
  `KindLandingTaskRoot`, `KindLandingEnvelope`".
- Gap: two of those four rows have no host source at attest time. ADR-017:700
  calls `/repo/source/.mission-control` a GENERATED empty directory — the same
  word :692 uses for the setup class's cover, which `resident/src/effects.ts`
  creates per run as `<run-id>.setup-cover` immediately before the container
  launch. `/mc/landing.json` is generated the same way. Dispatch attest captures
  device/inode/owner evidence for a plan entry; it creates nothing and runs
  before the resident does. So neither row can be produced as a typed root, and
  a fourth-kind producer would have had to either fabricate an identity or
  create host state inside attest.
- Choice: mark both rows `ResidentMaterialized` in `landingPlanRows()` — the
  table stays ADR-017's faithful four — and exclude them from
  `validLandingPlanDestination`, so dispatch never carries them as bind entries.
  `resolveLandingRoots` produces the two host-backed kinds only. This is the
  conservative option: (a) fail-closed is preserved, since a request for either
  generated kind finds no authorized root and denies; (b) it deviates least,
  because it is what the ADR's own word "generated" already means and it copies
  the division `/mc/setup.json` has had since the D5 slice; (c) it is trivially
  reversible — a later slice that finds an attest-time identity for the cover
  clears one bool and adds a producer arm.
- Consequence, stated rather than hidden: because the cover is not a plan entry,
  the PLAN cannot express that the sealed task bytes are unreachable through the
  RW `/repo/source` alias (ADR-017:700). The plan authorizes the grant; only the
  realized mount table establishes containment. This does not weaken anything
  that exists — the RO-alias property was already a Docker-lane obligation for
  exactly this reason (a plan-level `:ro` assertion cannot prove it) — but it
  does mean the resident's obligation to PLACE the cover has to be carried by
  the landing instruction and validated at the helper boundary. That is owed to
  the envelope slice (step 3) and must not be skipped: a landing container run
  without the cover would expose the sealed root RW through the source alias.
- Spec impact: none. ADR-017's table is right; `NEXT:` was a plan written before
  the resident's cover mechanism was re-read.
- Needs your decision: no.

## 2026-07-20 — the `/repo` plane is not plan-addressable, and asking whether it should be was the wrong question
- Where: adversarial review of the sealed-landing mount slice (7fee4e4..8273616),
  fixed in 55c2949. Supersedes the plan-grammar half of the entry above.
- Gap: the entry above asked "does a host identity exist at attest time?" and
  concluded two of the four landing rows were plan-bindable, so it taught the
  ADR-016 D5 plan grammar the `/repo` plane. The question the codebase actually
  answers is different: `resident/src/effects.ts:90-95` refuses EVERY plan entry
  whose destination falls outside `/workspace`. The plan is an agent-plane
  carrier. Every `/repo` row of the sibling setup class is composed by the
  resident from paths it derives itself (effects.ts:598-603). Applied
  consistently, landing produces ZERO plan cells, not two.
- Choice: revert both seam widenings; keep the table and the host-anchor
  resolver, repurposed as the landing CLASS's grammar for validating the landing
  instruction. This is the conservative option on all three counts: (a) it
  restores a fail-closed posture that the widening had actually weakened, (b) it
  deviates least, being the division `/mc/setup.json` has had since the D5 slice,
  and (c) it is a deletion.
- Why this is worth an entry rather than a quiet fix: the widening bought NO
  capability — the resident would have refused the spawn the moment step 5 turned
  the lane on — while costing two real guards, because both predicates gate more
  than their names suggest. `validatePrivateMountPlan` began accepting plans that
  mixed agent-table `/workspace` rows with landing `/repo` rows, which ADR-017:686-687
  forbids ("never inherit the agent table") and which nothing else enforces; and
  the task-precreate fabrication guard, which keys off `validTaskPlanDestination`
  alone, began admitting a precreate plan carrying RW to the real Worksource
  checkout. Both were LIVE on the helper boundary — an incoming-plan re-validator,
  not a producer — so the slice was not as inert as its own tests asserted. The
  generalisable lesson: widening a predicate that several guards share is not a
  local change, and "the producer is inert" does not make a validator inert.
- Also fixed, found by the same review: the RW landing anchor had no shape fence
  (now refuses group/world-writable, though not an exact mode — a real repository
  is commonly 0755); no check that it is a "real Git Worksource root" per
  ADR-017:699 (now requires an administrative `.git` entry); the task-root arm's
  fences were unreachable because a foreign uid tripped the Worksource arm first;
  and the canonical-ancestry fence was VACUOUS — mutating it to `if false` left
  the suite green, because the symlink checks caught every shape it was meant to
  refuse. That last one is the fourth time this phase a fence has been found
  passing for the wrong reason; the smell is a negative test whose scenario is
  reachable by an earlier fence, and mutation is the only thing that finds it.
- Spec impact: none. ADR-017's table is right. The unwritten fact is that the D5
  plan carrier is agent-plane-only — true in `effects.ts` since Phase 1b, stated
  in no document, and re-derived twice now. ADR-016 D5 or D1 should say it.
- Needs your decision: no.

## 2026-07-20 — the landing envelope carries eight of ADR-017:702's nine facts
- Where: Phase 3, sealed landing step 3 (`verbs/setupenvelope.go`,
  `verbs/mountplan.go`, `verbs/dispatchprivate.go`)
- Gap: ADR-017:702 enumerates what `/mc/landing.json` carries: "exact task,
  local/real branch, verified SHA, target ref, pre-merge SHA, closure digest,
  landing action identity, expected Git topology, and cleanup path". Two of the
  nine have no realizable form in this slice.
- Choice, "expected Git topology": carried STRUCTURALLY rather than as its own
  serialized field. The topology the lander revalidates (ADR-017:741-743) is the
  branch, the verified/pre-merge/base SHAs, the closure digest and the local
  repository UUID — every one of which the envelope already carries as its own
  fenced field. The ADR specifies no serialization, so inventing an opaque blob
  would add a parser at the most dangerous boundary in the system without adding
  a fence. This is the conservative option under §6(c): a later slice that finds
  a topology fact not already covered adds that field, and nothing has to be
  un-invented first.
- Choice, "cleanup path": NOT carried. ADR-017:757-759 defers cleanup to "a
  later trusted landing/setup action" *after the spine records success* — it is
  not this container's operation. Its only in-container target is the task root,
  which the envelope already carries as `TaskRoot` and which is mounted RO.
  Carrying a path for an operation this class cannot perform would be authority
  without a consumer. PROGRESS's step 4 stops at the merge for the same reason:
  cleanup has no mount and no owner yet.
- Also decided: the landing arm refuses a run id. Landing holds no lease and
  opens no Run (§7, and `LandReport` takes no run), so the envelope preamble's
  "names no live run/task" check split — TaskID is required of every arm, RunID
  of every arm *but* landing, which refuses it. This is cross-arm bleed in the
  direction that is easy to miss: the setup arms all have a run, so a landing
  envelope quietly carrying one would look ordinary.
- Also logged, not fixed: the two sides of this crossing disagree about
  canonical decimals. The envelope's `decimalIdentity` refuses a leading zero;
  the helper boundary's `validDecimalText` accepts one. Landing inherits the
  looser predicate rather than tightening it, because `validDecimalText` also
  gates task precreate, the completion seal, the accepted-seal rebuild and the
  verifier projection — tightening it inside this slice is exactly the
  shared-predicate mistake the 2026-07-20 review above reversed. Not a hole at
  this layer: the resident compares these against `strconv.FormatUint` output,
  which never emits a leading zero, so a leading-zero identity fails the later
  comparison instead of passing. Pinned by
  `TestPrivateMountPlanLandingInheritsTheSharedDecimalGrammar` so it is visible
  rather than silent. Owner: whoever next unifies the private-scalar grammar.
- Spec impact: ADR-017:702 should say that the topology is the enumerated
  identity fields rather than a distinct payload, and that the cleanup path
  belongs to the cleanup action's own instruction, not the landing container's.
- Needs your decision: no.

## 2026-07-20 — the landing target is a bare branch name, correcting a guess
- Where: Phase 3, sealed landing step 4 prep (`verbs/landingplan.go`,
  `verbs/setupenvelope.go`, `verbs/dispatchprivate.go`)
- Gap: step 3 validated the landing target as merely non-empty, and its fixture
  used `refs/heads/main`. That shape was GUESSED, not read off the spine.
  `tasks.target_ref` is free-form text (schema.sql:786, length 1..512) and its
  real values are bare names like `main`; the first-task setup arm additionally
  treats it as a rev to resolve, where even `HEAD` is legitimate. Landing cannot
  inherit that looseness — it constructs `refs/heads/<target>` in the REAL
  operator repository, so `refs/heads/main` would yield
  `refs/heads/refs/heads/main`, `HEAD` is meaningless as a merge destination,
  and option- or glob-shaped names turn a ref into an argument or a pattern.
- Choice: a closed bare-local-branch grammar (`validLandingTargetBranch`)
  enforced on BOTH sides of the crossing, plus a refusal when the target equals
  the task's own sealed branch — landing merges the task branch INTO the
  target, so identity would make ADR-017:748's CAS ref creation create the very
  ref it then merges from. The grammar restates git's check-ref-format rules for
  a branch in pure Go rather than shelling out, because the helper boundary must
  not spawn a git process on caller-supplied bytes.
- Why this is conservative: it only ever REFUSES MORE. Nothing produces a
  landing instruction yet, so tightening is free in the fail-closed direction,
  and doing it now means step 4's lander is written against the real shape
  instead of inheriting a fixture's invention. The looser sibling arms are
  untouched — `target_ref` stays free-form for first-task setup, where a rev is
  correct.
- Spec impact: none, but worth noting the ADRs never say what form the landing
  target takes; ADR-017:748-751 says only "the target ref". A future edit should
  state that landing's target is a bare local branch.
- Needs your decision: no.

## 2026-07-20 — the composed lander leaves the closure digest unverified and retry non-adopting

- Where: Phase 3 step 4 composition (`mc/verbs/landsealedrun.go`), against
  ADR-017:740-756.
- Gap: two things the ADR names that the composed lane does NOT do.
  (1) The landing instruction carries `pinned_closure_digest`, and ADR-017:741
  lists "exact reachable closure/digest" among what landing revalidates. The
  lane never checks it. (2) ADR-017:750-753 wants retry to ADOPT an already-made
  merge when its action trailer, parents, tree, target preimage, worktree/index
  state, and verified SHA all match, blocking only on ambiguity. The lane has no
  adoption path: a retry after a successful merge refuses at the pre-merge SHA
  fence, because the target has moved to the merge commit.
- Choice: leave both unbuilt and log them, rather than invent semantics.
  On (1) the digest is not merely unimplemented, it is UNDEFINED at landing
  time. `task_assignments.closure_digest` is frozen at FIRST-TASK setup and
  describes that pack; by landing, the task store has been rebuilt from the
  accepted completion seal, so its pack is a different artifact with a different
  digest. Verifying the carried digest against the landing-time store with the
  existing `digestLandedPack` would therefore refuse every real landing. Which
  of the two digests the field denotes is an operator/design question, and no
  production producer populates it yet (grep: only tests), so nothing is being
  broken by waiting. What the lane DOES bind is stronger than a digest over pack
  bytes anyway: `revalidateSealedTaskStore` proves HEAD is the exact frozen
  verified SHA under a sole-managed-branch, no-alternates, fsck-clean store, and
  the import is bounded to base..verified.
  On (2) adoption is a genuine slice of work (it needs the trailer parser and
  the preimage comparison), and its absence is FAIL-CLOSED: a retry refuses
  loudly instead of merging twice. Building it half-way is the failure mode the
  outgoing NEXT warned about — a half-built lane converts a loud refusal into a
  durable blocked row.
- Why this is conservative: both preserve the fail-closed posture, neither
  weakens an invariant, and both are purely additive later. Inventing a digest
  semantics now would be the irreversible move, because a wrong pin would be
  frozen into instructions before any producer exists to correct it.
- Spec impact: ADR-017:741 should say WHICH closure digest landing revalidates,
  or drop the digest from landing's revalidation list in favour of the verified
  SHA binding that actually holds. ADR-016 D5's assignment already fixes the
  first-task meaning of the field.
- Needs your decision: no — but the digest question is named in PROGRESS's owed
  list so it is not rediscovered.

## 2026-07-20 — two timing findings the corpus gap analysis surfaced

- Where: Phase 3 step 4, assessing legacy `mc-land.test.ts:683` and `:413`
  (hostile state introduced BETWEEN preflight and merge) against
  `mergeSealedLanding`.
- Gap: the merge stage rechecks ONLY the SHA fence (ADR-017:750). Two
  consequences, one a TOCTOU and one operational.

  (1) EXECUTABLE CONFIG IS NOT RECHECKED AT MERGE TIME. `refuseExecutableLandingConfig`
  runs in the repository stage. A merge driver inserted into repository-local
  config AFTER that stage — paired with an in-tree `.gitattributes` naming it,
  which `core.attributesFile=/dev/null` and `GIT_ATTR_NOSYSTEM=1` do NOT
  suppress — would be consulted by the merge. `-c merge.ours.driver=false` pins
  only the `ours` driver, not an arbitrary named one. Legacy pinned exactly this
  as `:683`. Reachability is narrow (it needs a concurrent writer to the
  operator repository during the landing window; the landing container carries
  no agent process), but it is not zero, and the whole class is fenced elsewhere.

  (2) A CONFLICTED MERGE HEAD-OF-LINE-BLOCKS. Verified empirically, not
  reasoned: `TestLandSealedLeavesAConflictedMergeInPlace` shows a conflicting
  `merge --no-ff` leaves `MERGE_HEAD` behind, and the next attempt refuses at
  the operator-merge-in-flight fence with "merge already in progress". A
  conflict is genuinely reachable despite the reviewed-path dirty fence and the
  pinned target tip, because the target may have ADVANCED from the frozen base
  to the pre-merge SHA touching the same paths the reviewed change touches. So a
  single conflicting landing wedges the lane until a human clears the checkout.
- Choice: neither fixed in this slice; both recorded, and (2) pinned by a
  CHARACTERIZING test so the behaviour cannot drift silently in either
  direction. For (1) the obvious repair is to re-run the executable-config and
  index-visibility fences immediately before the merge, mirroring what
  ADR-017:750 already does for the SHA — cheap, and it narrows the window to
  the merge invocation itself without pretending to close it (git reads config
  when it runs; only config isolation would truly close it, and the merge needs
  the repository's own config). For (2) the legacy answer is "abort only what we
  started" (`:368`): on merge failure, abort iff `MERGE_HEAD` is our verified
  SHA. That restores the pre-merge state and unwedges the lane, and it is
  careful not to touch an operator merge — but it IS a mutation on a failure
  path, which is exactly the kind of thing this lane has otherwise avoided.
- Why this is conservative: both are fail-closed today. (1) leaves a narrow
  TOCTOU rather than an open door, and (2) refuses loudly rather than merging
  into a conflicted tree. Fixing (2) means adding a mutating failure path, which
  deserves its own slice and its own adversarial review rather than being
  smuggled into the composition commit.
- Spec impact: ADR-017:750 specifies only the SHA recheck at merge time; if the
  other preflight fences are meant to be rechecked there it should say so.
  ADR-016:569-576 wants infra failure to record health and leave the tuple
  pending, which is the natural home for the conflict outcome — but `mc land
  report` has two statuses and no backoff, already noted as an owed decision.
- Needs your decision: yes for (2) → parked in PROGRESS. A conflicted landing
  wedging the single landing slot is an operator-visible behaviour, and whether
  the lander may mutate on a failure path is not mine to settle.

## 2026-07-20 (operator decision) — scoped self-abort on a conflicted landing

- Context: the 2026-07-20 entry above logged finding (2), A CONFLICTED MERGE
  HEAD-OF-LINE-BLOCKS, and closed with "Needs your decision". This resolves it.
- Decision (operator, 2026-07-20): **allow the scoped self-abort.** On merge
  failure the sealed lander aborts iff `MERGE_HEAD` is the SHA it verified and
  created — legacy `mc-land.test.ts:368`.
- Why this is conservative under §6's definition: it preserves the fail-closed
  posture (the landing still FAILS; nothing merges), it restores the pre-merge
  tree rather than advancing it, and it is trivially reversible — deleting the
  abort call returns the lane to today's behaviour exactly.
- What decided it: not the invariant argument but two operational consequences —
  (a) the leftover merge silently blocks every later landing, and (b) a human
  resolving the debris the obvious way lands reviewed work outside the lane and
  outside its record. Both are in docs/ledger/phase-3.md (2026-07-20 operator).
- Accepted cost: the conflicted tree is not preserved for inspection.
- Scope guard, to be honoured by the implementing slice: an operator's own merge
  in flight is refused at PREFLIGHT (`landsealed.go:225`) and the abort path is
  never reached; the `MERGE_HEAD`-identity check is a second, independent fence,
  not the primary one. Both must be tested.
- `TestLandSealedLeavesAConflictedMergeInPlace` is characterizing. The slice
  inverts it; it must not be deleted.
- Spec impact: ADR-016:569-576 wants infra failure to record health and leave the
  tuple pending — the conflict outcome's natural home. `mc land report` still has
  two statuses and no backoff (owed decision, unchanged by this).

## 2026-07-20 (finding) — the self-abort's ownership gate needed the landing id

- Context: implementing the operator-approved scoped self-abort. The decision
  entry above specifies the gate as "`MERGE_HEAD` is the SHA it verified and
  created", and the first implementation (`6463d8a`) took that literally.
- Finding, confirmed by a test that failed against the shipped code: a reviewed
  SHA in `MERGE_HEAD` does NOT prove the merge is ours. Stage (7) creates
  `refs/heads/mc/task-<id>` at the reviewed SHA by CAS, which publishes the
  reviewed commit under a name the operator can reach. An operator running
  `git merge mc/task-7` between that ref appearing and our merge completing
  produces `MERGE_HEAD == verifiedSHA`, and the SHA-only gate aborted their
  half-resolved conflict — the exact outcome the scope guard forbids.
- Note the shape of the mistake: the scope guard called the identity check "a
  second, independent fence, not the primary one", with preflight as primary.
  But preflight cannot see a merge started AFTER it ran, which is the only state
  the abort path ever meets. The second fence is the only fence here, and it was
  sized as though it were not.
- Fix (`64dc5de`): the gate additionally requires `MERGE_MSG` to carry
  `MC-Landing-Id: <this landing's id>`. Git preserves the `-m` message verbatim
  through a conflict; an operator's own merge carries git's default message.
  Writer and matcher share the `landingIDTrailer` constant, because a drift
  between them would silently stop the abort recognising its own merges.
- Spec impact: none — this moves TOWARD the ADR. ADR-017:752-753 says
  merge-in-progress files are "accepted or aborted only when they match this
  action", which is action identity, not SHA identity. The looser first reading
  came from the decision entry's shorthand, not from the ADR.
- Residual, deliberately not closed: the ADR's full match set (action trailer,
  parents, tree, target preimage, worktree/index state) is specified for the
  ADOPTION direction, where a merge COMMIT exists to match against. At abort
  time the merge failed, so there is no commit and no trailer — `MERGE_HEAD`
  plus `MERGE_MSG` is the whole of the available evidence. An unreadable or
  unrecognised message refuses to abort rather than guessing, so the failure
  direction is residue left behind, never an operator's merge destroyed.

## 2026-07-20 (review disposition) — the self-abort's adversarial review

Six defects were raised against the abort slice. Three were fixed (`2d2cffb`),
two are recorded below as accepted residuals, and one was REFUTED by
measurement. The refuted one matters most, because it was rated the most severe
and would otherwise be re-raised.

- REFUTED — "unrelated STAGED operator work is destroyed by the abort". The
  mechanism is real in isolation: `merge --abort` is a `reset --merge`, which
  resets the whole index to HEAD, so a staged unrelated file whose worktree and
  index agree is reverted with its content recoverable only as a dangling blob.
  The premise that it is reachable here is not. `git merge` REFUSES TO START
  with anything staged — "Your local changes to the following files would be
  overwritten by merge", exit 2, no MERGE_HEAD written — so a merge state cannot
  coexist with pre-existing staged work, and the abort path is never entered.
  Measured directly, twice, rather than argued. The same measurement refutes the
  companion claim that the abort would FAIL and wedge the slot when a file
  differs from both HEAD and the index: that merge never starts either.
  The reasoning error is worth naming: the review tested `reset --merge`
  standalone and inferred reachability from the path-scoped dirty fence
  permitting unrelated work. The fence does permit it — but git's own
  clean-index precondition, not our fence, is what excludes it here.
- ACCEPTED RESIDUAL — an operator who stages something in the window between
  our merge failing and the abort running would have it reverted. Milliseconds
  wide, and closing it costs the abort a reviewed-path set it does not have, for
  a guard no reachable test could exercise. Named, not guarded.
- ACCEPTED RESIDUAL — TOCTOU between reading MERGE_HEAD and running the abort:
  an operator concluding our merge and starting their own in between would have
  theirs aborted. Same class as the merge-time config recheck's documented
  residual, and closing it needs a lock this lane does not hold.
- DEFERRED, and it belongs to an already-owed decision — callers cannot
  distinguish "aborted, slot free, retry safe" from "foreign merge, wedged, do
  not retry" from "abort failed, residue present". All three are one
  `*DomainError` carrying prose, and the repo's own test substring-matches
  "abort" to tell them apart. This is the landing failure taxonomy already
  listed as unresolved in PROGRESS; the three post-conditions above are the
  concrete cases it must name. Do not invent the taxonomy inside the abort.

## 2026-07-20 — what "approved packet/run identity" denotes in the landing id

ADR-016:833 defines `landing_id` as the first 16 hex of a domain-separated
digest of "deployment, subject, and exact approved packet/run identity". That
phrase occurs ONCE in the whole corpus, echoed once as "approved-run identity"
(:846), and is never expanded. It also has no unique referent in the spine, so
it had to be resolved rather than read off. Resolution, with the evidence:

- The PACKET contributes no entropy. `review_packets.task_id` is both primary
  key and foreign key (`schema.sql:453-454`): one packet per task for life, no
  id of its own. "Packet identity" IS the subject, which the digest already
  names separately.
- The approved RUN is the accepted Worker seal's `(run_id, request_id)` pair
  from `tasks.accepted_completion_run_id` / `accepted_completion_request_id`
  (`schema.sql:116-120`), which the schema itself calls "the exact downstream
  authority: the currently accepted Worker seal for this task". Approval is an
  operator write with no run of its own (spec §5), and `runs.role` has no
  landing member (`schema.sql:694-695`), so there is no other run to mean. The
  pair rather than the run alone is what "exact" buys.

STABILITY ACROSS ATTEMPTS was the reason this needed settling, and the corpus
is SILENT on it — no sentence contains both `landing_id` and a retry qualifier.
It is nonetheless load-bearing under the abort slice landed at `2d2cffb`:
ADR-017:753-756 requires a retry to match "its exact action trailer" before
adopting a merge, and this lane's trailer is `MC-Landing-Id: <id>`. A
per-attempt id would leave a crashed attempt's merge state unrecognizable to
its own successor — exactly the state `abortOwnConflictedMerge` exists to
resolve. Every candidate referent above is attempt-stable; the only
attempt-varying ids in the system are `dispatch_request_id` and `dispatch_key`,
which ADR-016:110-111 marks "not canonical work state".

Worth recording because the corpus points the OTHER way on names generally:
`mc-land` "never guesses from a name" (ADR-016:378) and "name alone is never
authority" (:362-363), with cross-attempt recognition routed through the spine
row plus exact Git topology (ADR-017:739-740, :1235-1236). That is consistent —
the landing id is not authority here either; it is the discriminator INSIDE an
already-authorized trailer match. Do not promote it to authority later.

Deliberately NOT inputs: `verified_sha` and `target_ref`. Both are stable and
both would tighten replay identity, but ADR-016:831-833 names three inputs and
adding a fourth is a deviation with no stated need. If a future finding shows
the trailer must distinguish two landings of the same task at different
reviewed SHAs, that is the moment to add it — under an ADR, not silently.

## 2026-07-20 — the landing container envelope, and three things it does not do

The sealed landing's container envelope is almost entirely DECIDED by the ADRs;
the survey that established it is in the Phase 3 ledger. Two decisions inside
it are worth stating here because they look like mistakes otherwise.

**uid 10002:10002, not the operator.** Landing is the one class that writes into
a real operator-owned Worksource — it imports objects, creates a ref, and merges
in the primary checkout — so an unprivileged container uid looks wrong. It is
not: ADR-019:85 puts setup and landing on one row at 10002 with NNP on, and
ADR-017:76-86 deliberately asserts NO fact about how the VirtioFS share presents
host ownership inside a container, deferring the whole question to ADR-019's
final-uid canary (:183-188) rather than to an ADR claim. Nothing chowns a host
inode (ADR-017:68-71), and there is no root agent or permission-widening
fallback. If the canary fails, that is a red Phase-3 mechanism line, not a
licence to raise the uid.

**`mc-approved-run-id`, not `mc-run-id`.** ADR-016:846 requires landing labels to
carry "landing/subject/approved-run identity" but never spells the key strings.
`mc-run-id` was rejected: everywhere else that key means "the run this container
IS", so a landing tagged with the Worker run it landed FOR could read to a
liveness sweep as that Worker's own agent container — the exact masquerade
ADR-016:857 forbids. The carrier grew `ApprovedRunID` to supply it; the
companion request id stays out, because it buys exactness only in the landing-id
digest, where it already participates, and a label is a sweep key, not a fence.

Three gaps carried forward rather than closed:

- DEVIATION — ADR-016:350 gives landing a fixed 15-minute foreground wall
  deadline. Nothing enforces it. `Exec` (`types.ts:18`) takes an argv and
  nothing else; `main.ts:30` awaits `Bun.spawn` with no kill timer; the only
  `withTimeout` in the resident lives in `resident-control.ts:290` and is not
  reachable from `effects.ts`. Every other container class is equally
  unbounded, so this is a pre-existing hole the landing class inherits rather
  than one it opens. Closing it needs a deadline seam on `TickDeps.docker`,
  which is a change every class pays for — hence not smuggled in here.
- ADR-017:753-755 makes the merge topology (`SealedLandingResult` on the
  lander's stdout) "the durable merged marker reported into the spine". No
  spine consumer exists: `mc land report` takes a status and a reason and
  nothing else. The resident therefore reports status only, and the result JSON
  is currently discarded. This belongs to the same step that teaches
  `LandReport` about assignment-backed rows.
- PRE-EXISTING, found while matching the idiom, NOT fixed here: the setup
  containers emit `mc-tier=pipeline` where ADR-016:845 says setup has no tier,
  and omit `mc-component=setup` entirely; and the legacy `land()` emits a
  valueless `mc-managed` label where ADR-016:837 says `mc-managed=true`. All
  three are label-conformance bugs against Decision 7 in code the sealed lane
  does not own. Fixing them touches whole-argv assertions in three test files
  and belongs in its own change, not in a landing commit.

## 2026-07-21 — the sealed landing lane routed through prepare/attest/commit

Six delegated design decisions, all made because the corpus is SILENT and all
individually reversible. Recorded together because they were taken as one
coherent reading of ADR-016:369-379 rather than independently.

- **DELEGATED — the sealed landing is a SEPARATE lane, not a widened candidate.**
  ADR-016:369-379 requires a landing to form "an attested candidate rather than a
  bare effect" and to have commit "recheck the entire pending tuple and
  inventory". It does not say how. `preparedDispatch` gained a third variant
  (`landing`) beside `final` and `candidate`; `preparedCandidate` was NOT
  widened. The argument is type-level rather than aesthetic: the spawn seam
  dereferences `cand.spawn` unguarded in dozens of places, and a landing carried
  inside `preparedCandidate` would make every one of them reachable with a nil
  `Spawn`, correct only by ongoing audit. Two competing designs (a sibling
  pointer on `preparedCandidate`, and a kind-polymorphic ops table) were
  evaluated and lost on exactly this point. Reversal is mechanical but wide.

- **DELEGATED — no dispatch-side receipt and no `dispatch_key` on the landing
  success path.** ADR-016:255-257's prepare-side receipt rule reaches mutations
  that return DIRECTLY FROM PREPARE; a landing returns from commit, so the rule
  does not reach it. ADR-016:261-263 exempts a result that has caused neither a
  state mutation nor a host effect, and at the instant `dispatchCommitLanding`
  returns BOTH are true — the spine is untouched and the resident has not yet
  started the container. A `dispatch_key` would additionally be a fake fence:
  the token binds a per-command `RequestID`, so it could never dedupe across
  ticks. Cross-tick idempotency is assigned elsewhere by the corpus — the
  durable landing-pending row (ADR-016:571-573) and the receipt-idempotent
  `mc-land` keyed on the stable landing id (ADR-016:377-378). Adding a receipt
  later is additive; removing a shipped one is not.

- **DELEGATED — no new consequence identifier.** The question is made moot
  rather than answered: the success path derives no key and constructs no
  `canonicalAction` at all, so the only one the lane ever builds is the refusal
  action, reusing the existing `"refusal"` literal. The one new string anywhere
  is `canonicalCandidate.Kind == "landing"`, which is a candidate-identity tag
  inside the preparation token, not a consequence name. If a consequence name is
  ever needed, `"landing"` is free.

- **DEVIATION — the landing id is derived at PREPARE, not at attest.** This
  contradicts the header `landingid.go` shipped with, which sited it attest-side
  on an availability argument. ADR-016:371 names the deterministic id as a
  member of the candidate TUPLE, and a tuple member must be inside the
  preparation token or commit cannot detect its drift. All four inputs are in
  prepare's scope. The header was corrected in the same commit rather than left
  contradicting the code.

- **DELEGATED — landing refusals use `RefusalSubjectlessPipeline`, not
  `RefusalSubjectTask`.** `domain.Block` is reachable only from a
  candidate-class refusal carrying `RefusalSubjectTask`, so the subjectless kind
  makes a durable blocked row unreachable BY TYPE rather than by an enumeration
  of which refusal codes happen to be stale-class today. ADR-016:573-576
  reserves blocking for the fixed `mc-land` program's semantic Git refusal,
  reported through `mc land report failure`. The cost is real and accepted: a
  diverged target ref is loud only in the health detail text, not in an indexed
  subject column. Reversible by one field.

- **DELEGATED — the container exit-code classification.** ADR-016:576 forbids
  mislabeling runtime failure as a failed reviewed change but names no exit
  contract. Resolved against mc's own (`mc/cmd/mc/main.go:91-107`): 0 is
  success; 1 is domain rejection, meaning `mc-land` looked at the repository and
  refused, and is the ONLY case that reports failure and blocks; everything else
  (2 usage/environment, docker's 125/126/127) reports NOTHING and leaves the
  landing pending as deployment health. The previous code reported failure on
  any nonzero exit, which turned a broken image or a bad mount into a durable
  blocked row an operator had to clear by hand.

Two host-side facts settled while building, worth not re-deriving:

- `attestDeploymentPreamble` exists so both attest legs provably owe the same
  D1 deployment fence. The landing leg reads no routing at all, and the
  deployment check sits adjacent to the routing read in the original body, so
  splitting it out is what prevents a second leg from dropping the fence
  alongside the routing it genuinely does not need.
- `landingWorkspaceRoot` REFUSES rather than returning `""`.
  `captureLandingPlan` resolves every host anchor relative to the root it is
  handed, so an empty root resolves them against the process working directory —
  the single place in this lane where a landing could interrogate, and then
  write into, a repository that is not the operator's.

Closed since the previous entry: `LandReport` now accepts assignment-backed
rows, so the second bullet of the 2026-07-20 "three gaps" list is partly
addressed — the fence is open, though `SealedLandingResult` still has no spine
consumer and the topology JSON is still discarded. The 15-minute deadline and
the label-conformance bugs remain open exactly as recorded there.

## 2026-07-21 (cont.) — the Darwin landing carrier, and two reversed decisions

- **DELEGATED — the sealed landing crosses the Darwin private frame as its own
  kind.** ADR-016 D1 describes the broker/helper split as "a later slice over
  these same functions", which was true of the spawn path and became false for
  landing the moment the lane went live: `mc dispatch` on this platform
  self-delegates, so a landing that could not cross the frame could not dispatch
  at all. `PrivateDispatchLandingCandidate` is a sibling of the spawn carrier,
  and all three legs plus the broker CLI select on the frame KIND rather than on
  a nil check, so a malformed frame cannot take the other lane's attestation.
  The landing needed its own attestation validator because
  `validatePrivateAttestation` requires a resolvable routing digest on any
  non-refusal frame and a landing attests no routing.

  The helper re-validates every scalar it acts on but deliberately NOT the
  token's value: commit recomputes the token from the tuple it received, so a
  dropped or mangled field fails on evidence the helper computed itself rather
  than on a shape check a well-formed lie would satisfy.

- **REVERSED — `preparedLanding` no longer carries tunables.** They were written
  at prepare and never read; commit rebuilds the token from the FRESH
  selection's tunables. Found only because the Darwin carrier had to serialize
  them and the round-trip test failed a bound check on a field with no reader.
  Carrying dead state across a security boundary is strictly worse than
  carrying it in-process: it becomes something the helper must validate and an
  attacker can vary.

- **REVERSED — a closed landing now removes its own cover directory.** The prior
  code kept it, citing ADR-016:344-349 as making a later tick responsible for
  landing residue. On reading that clause it is about action CONTAINERS visible
  to a later tick, not about host directories the resident itself created, and
  the sweep it defers to was never written — so every landing leaked one
  directory under `MC_HOME/runs` permanently. Contract §3's orphan-sweep row is
  the governing text: "closed derived file artifacts have exact
  component/action liveness and cleanup".

  The infrastructure-failure path still keeps both cover and envelope, and that
  asymmetry is the point: the landing did not CLOSE, it stays pending, and the
  retry reuses those exact paths because the landing id is stable by
  construction. Removing them there would also delete a cover out from under a
  container whose absence has not been confirmed.

Closed since the 2026-07-20 entry: `mc land report` now accepts assignment-backed
rows, so the sealed lane can report its outcome. Still open from that entry: the
15-minute foreground landing deadline is unenforced (no timeout seam exists on
`TickDeps.docker` for any class), `SealedLandingResult` still has no spine
consumer because `mc land report` takes only a status and a reason, and the
setup/legacy-land containers remain non-conformant to ADR-016 Decision 7's label
rules in code the sealed lane does not own.

## 2026-07-21 — Free-internet credential projection supersedes the egress gateway (ADR-022)
- Where: spec §11.4, Inv. 16, Inv. 23; `docs/phase3-contract.md` §3; ADR-018.
- Gap: the spec built the boundary around a resident-hosted credential-injecting egress proxy that also enforced `egress_policy` and audited egress. The operator set a hard requirement that agent containers browse the internet freely, which deletes the proxy as a network control and leaves the credential boundary as the sole blast-radius property.
- Choice: keep exactly one property — access-token-in / refresh-token-out — enforced by a resident-hosted token service, not a proxy. Proven live for both runtimes in `scratchpad/oauth-poc/POC-RESULT-v2.md` (Claude 2.1.216 PUSH the file always-valid; Codex 0.144.6 PULL via `CODEX_REFRESH_TOKEN_URL_OVERRIDE`). This is the conservative option available *given* free internet: it preserves fail-closed posture (a lapsed/absent host service stalls the run, never leaks) and is the least machinery (deletes ADR-018 D1-D9 wholesale). Static API-key bindings cannot be split, so v1 materializes them as a declared per-binding downgrade (ADR-022 D5), bounded by no-metered-spend + the §11.3 advisory; the narrow header-injector hardening is deferred.
- Spec impact: §11.4 amended (2026-07-21 block); Inv. 16 reinterpreted as "the refresh/long-lived grant never enters the container" (access token in-container by design; static keys excepted); Inv. 23 reinterpreted as "resident hosts a token service, not an egress proxy". `docs/phase3-contract.md` head + §3 rows amended; ADR-018 marked Superseded; ADR-022 authored.
- Needs your decision: no (operator approved the Inv. 16/23 modifications and the loss of HTTP egress auditability in-conversation on 2026-07-21).

## 2026-07-21 — S9 spikes resolve the two ADR-022 residuals; both amend the writer contract
- Where: `spikes/09-credential-projection/RESULT.md`; ADR-022 D3, D4, and its Residual-risks list; `docs/priors/oauth-poc-result-v2.md` (the POC evidence, preserved from the volatile session scratchpad — it lived only in a prior session's `/private/tmp` scratchpad, which `docs/priors/README.md` directs surfaced POC material to be dropped into).
- Gap: ADR-022 listed two unproven residuals that gate the writer design — (1) whether `CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST` offers a cleaner Claude path than the D3 `.credentials.json` dummy-refresh rewrite, and (2) whether a real fresh Codex token skips the D4 startup refresh. Both are now resolved from source (static inspection only; no live provider calls, no real credentials read).
- Choice — DELEGATED, within ADR-022's own residual-spike mandate:
  - **D3 adopts the flag.** The Claude projection writer sets `CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST=1` and delivers the host-minted access token via `CLAUDE_CODE_OAUTH_TOKEN` (or the well-known file / `CLAUDE_CODE_HOST_CREDS_FILE` for in-session 401 rotation), instead of rewriting `.credentials.json` with a dummy refresh + future `expiresAt`. Under the flag Claude 2.1.217 bypasses the credential store entirely (`if(RE())return null` precedes the store read), treats the token as `refreshToken:null, expiresAt:null` (never self-refreshes), and the `invalid_grant` compare-and-swap wipe is structurally unreachable. This is *more* fail-closed than the POC path, not less: an absent token stalls the run with no wipe hazard. It is a conservative internal mechanism (ADR-022 §6 class), reversible to the POC's file-rewrite path if the flag proves unusable in-container.
  - **D4 keeps the broker.** A real signed JWT with ~10-day `exp` deterministically skips Codex's `should_refresh_proactively` startup gate (exp parsed without signature verification; refresh only within 5 min of expiry). But `CODEX_REFRESH_TOKEN_URL_OVERRIDE` stays MANDATORY as the reactive-401 / long-session / malformed-projection fallback — the exact path the POC exercised. "Fresh-token projection" is not a reason to drop the override.
- Spec impact: none. Both are writer-implementation choices inside the ADR-022 decisions; no invariant moves. ADR-022 D3/D4 and its Residual list are amended to record the resolution (the flag is now the primary D3 mechanism; the file-rewrite is the documented fallback).
- Still unproven (Docker acceptance lane, not a host-spike blocker): the exact in-container Claude delivery channel (env vs file-descriptor vs well-known file) and the host-creds-file 401 rotation under a non-desktop `local-agent` entrypoint; VirtioFS `mtime` propagation for the refresh-ahead rewrite.
- Needs your decision: no.

## 2026-07-21 — Schema v12 + forbidden-env builder: three delegated shapes
- Where: `mc/substrate/schema.sql` v12, `mc/verbs/onboard.go`, `mc/boundary/envpolicy.go`.
- Gap: ADR-022 orders the egress columns retired and the env builder built, but leaves three shapes undefined: the migration's data mapping, what remains of onboarding's "deny-by-default" profile health check, and the env-policy storage format (the columns had zero producers or readers since v1).
- Choice — all three conservative internal mechanisms, reversible:
  - **v12 maps `gateway`→`projection` and defaults new profiles to `projection`.** OAuth bindings are the projection class (D3/D4); `materialized` rows pass through untouched (D5). The rebuild stashes the nullable `worksources.sandbox_profile` references at NULL across the table swap so foreign keys stay enforced end to end — `PRAGMA defer_foreign_keys` was tried first and fails at commit, because the DROP's deferred violation count survives the rename that satisfies it.
  - **Onboarding's profile health check reduces to the §17 rebinding refusal.** The egress/network deny-by-default clause enforced a control ADR-022 struck; the workspace-root implicit-rebinding refusal is retained unchanged. The cli test now pins the rebinding arm instead.
  - **Env policy is a flat JSON object of name→string.** `BuildEnvPlan(policy, guard, binding, foreignStaticKeys)` validates one plane: declared-base-only (no ambient spread), `*_API_KEY` wildcard enumeration exposed via `APIKeyShaped()`, compiled-in §16.3 floor (`CODEX_API_KEY`, `ANTHROPIC_API_KEY`) with extend-only `NewEnvGuard` (the `BlockPolicy` idiom), and the D7 fence: `*_REFRESH_TOKEN` names and the binding's declared provider credential keys reject in projection planes; a D5 static key survives its own plane only and `foreignStaticKeys` keeps it out of every other. `EnvPolicyError` kinds map 1:1 onto `env.invalid`/`env.forbidden`; no wiring emits them yet (inert until step 3).
- Spec impact: none; all inside ADR-022's delegation. The binding catalog's `ProviderCredentialKeys`/`DeclaredStaticKey` sourcing lands with the step-3 wiring.
- Needs your decision: no.

## 2026-07-21 — ADR-022 step 3 wiring: two delegated choices, one logged deviation, one deferral
- Where: `resident/src/effects.ts` spawn(), `resident/src/types.ts`, `mc/verbs/mountattest.go`, `mc/verbs/ops.go`.
- Choice 1 — the projector seam owns channel knowledge. `TickDeps.credentials` is a `CredentialProjector` returning artifacts (`env`, optional `authJson`) | `{refused}` | `null`; the effector stays format-blind and resolves it BEFORE any launch file exists. A refusal is D8: log, no container, no files. An absent projector launches token-free — that keeps the docker_e2e stand-in adapter (MC_AGENT_RUNNER_ROUTES) working and puts fail-closed responsibility in the production wiring that declares a channel.
- Choice 2 — `MC_HOME/refresh-grants` is the canonical on-disk refresh-grant store, deny-mounted by the repurposed `resolveGatewaySecretRoots` (absent store registers nothing). The store's capture/format lands with install/onboarding (already a known later obligation).
- DEVIATION (logged per AGENTS.md §6): the fake family KEEPS `--network none` at agent-class spawn as hygiene. The §3 Credential-projection row's "no --network none" governs real agent containers; phase1-contract §1 names `--network none` for the deterministic, token-free fake family, and dropping it there would loosen containment for zero ADR-022 benefit. Real (non-fake) agent routes get the open network unconditionally.
- DEFERRAL: `main.ts` production wiring of token service + writers + broker into a live projector is blocked on the refresh-grant store capture (onboarding). The seam, writers, broker, and refusal path are all built and tested; wiring is a composition-only change.
- Refusal codes: CodeGateway*/CodeNetwork* stay in the closed set, inert (cheaper and reversible than a lockstep removal); `gateway_control_version` untouched (golden bytes).
- Needs your decision: no.

## 2026-07-22 — install.sh front door: production hand-off defers to the container section
- Where: `install.sh`, `.claude/skills/onboard/SKILL.md`.
- Gap: spec §17 has install.sh hand off to `mc onboard`, but the production
  darwin binary self-delegates every verb into the `mc-helper` container
  (runtime_scope_prod.go), and the helper is provisioned by the container
  onboarding section — itself deferred to Phase 5 (ADR-015 D5, ADR-022
  removed its gateway probe). Bootstrap order is thus unresolved for
  production: the wizard cannot run before the helper exists, and the helper
  is the wizard's own later section.
- Choice — conservative, reversible: install.sh detects the missing helper
  and reports the hand-off DEFERRED (exit 0, addressed to the shepherding
  agent) instead of inventing a host-side spine path that would violate
  Inv. 24 (the spine opens only inside the runtime kernel). The dev tier
  (`--dev`) builds the test_fake_routing binary and completes the full
  §17 walk against a direct scratch spine — proven green + idempotent.
  Resolving production bootstrap (helper provisioning before/within the
  hand-off) is Phase 5 front-door work and may need a small ADR if the
  section order moves.
- Spec impact: none yet; the §17 section order is unchanged. The deferral
  message is truthful per §17's fail-closed/verified-by-probe posture.
- Needs your decision: no.

## 2026-07-22 — Phase 5 cross-harness review: production onboarding tail is not a usable front door
- Where: `install.sh`, `.claude/skills/onboard/SKILL.md`, `mc/verbs/onboard.go`;
  implementation-handoff Phase 5; spec §17.
- Gap: the required adversarial review of `4bc6977..HEAD` found three blockers
  in the Phase 4-authored front door: production bootstrap is cyclic
  (`install.sh` needs `mc-helper`, whose provisioning belongs to the wizard it
  cannot enter); Docker preflight warns and continues instead of failing
  closed; and production parses but drops the wizard's dual-input answers
  while `/onboard` incorrectly labels operator-owned auth/routing/surface
  decisions deterministic. A fresh-clone run can therefore exit 0 without a
  deployment and cannot distinguish success from deferral.
- Choice: treat all three as Phase 5 defects, not accepted deferrals. Repair
  test-first in dependency order: fail closed on missing runtime/helper first;
  add a bootstrap-safe helper provisioning and capability probe without ever
  opening the spine on the host; then define and forward the complete
  interactive/answer-file input contract. Missing operator answers remain an
  explicit stop, never an implicit default. This preserves Inv. 24 and the
  §17 section order; no ADR is needed unless implementation evidence shows
  helper provisioning requires moving a section or changing an invariant.
- Spec impact: none. The existing code failed the already-binding §17 front
  door, fail-closed, verified-by-probe, and dual-input requirements.
- Needs your decision: no. The operator authorized Phase 5; live subscription
  spend and the one-time launchd load remain separately gated by their parked
  inputs.

## 2026-07-22 — Phase 5 production bootstrap uses a path-free private spine crossing
- Where: spec §16.4 and §17; ADR-016 private same-binary composition and D7;
  `runtime_scope_prod.go`, `onboard.go`, production image/helper lifecycle.
- Gap: the final helper must exist before ordinary Darwin `mc` can delegate,
  but Home initialization needs both the runtime-local spine and host-side
  `MC_HOME`. Mounting `MC_HOME` into the long-lived helper would expose config,
  credentials, and host paths; running existing `onboardHome` on Darwin would
  open the spine in the wrong kernel. A second bootstrap image would create a
  same-release drift surface.
- Choice: the final, deployment-derived helper is also the provisional Home
  crossing. Darwin `mc` canonicalizes and fences the real home, derives one
  domain-separated 12-hex deployment suffix, ensures the exact production
  image, volume `mc-spine-<suffix>`, and helper `mc-helper-<suffix>`, then sends
  a bounded path-free `__onboard-spine` frame. The helper mounts only the named
  spine volume at `/mc/spine`; it receives mirror/build/schema state but never
  a host path or config byte. Linux `mc` performs meta-first initialize/migrate/
  compare and returns the UUID; Darwin `mc` creates the home scaffold and
  atomically publishes/repairs `deployment.uuid`, then capability-probes the
  exact helper envelope. Home may provision this crossing as its prerequisite;
  Container remains the section that declares image/helper health and pinning.
- Idempotency: empty volume+absent mirror initializes; current meta+matching
  mirror skips; current meta+absent mirror repairs the mirror; empty volume+
  present mirror is spine loss; non-meta/non-empty, newer schema, mismatch,
  unmanaged name collision, or failed capability probe all refuse without
  deleting the volume or unrelated container. A stale exact managed helper is
  replaceable because it is stateless; the volume is never auto-recreated.
- Rejected: host SQLite/docker-copy, host bind-mounted spine, any `MC_HOME`/
  Worksource/socket mount into the helper, fixed `mc-helper`, a second bootstrap
  image/binary, root helper as a shortcut, or raw schema SQL in `install.sh`.
- Spec impact: none. This is the conservative internal composition of existing
  ADR-016 decisions and §17; no invariant or section order changes, so no new
  ADR is warranted.
- Needs your decision: no.

## 2026-07-22 — final helper requires the general setuid mc gate, not a root process
- Where: Phase 5 production Home bootstrap; spec §11.5 and §16.4;
  `docs/phase3-contract.md` §§4/6; ADR-019 D3; `runner/image/Dockerfile`,
  `mc/verbs/verbs.go`, and the helper manager.
- Gap: the current image elevated only the sealed-completion publisher. The
  ordinary `mc-real` was root-owned mode 0755 and the image had no default
  user, so a warm helper could reach the spine only by running its whole
  process as root. That contradicted the Phase-3 contract's general privileged
  `mc`, ADR-019's fixed helper uid 10002, and §11.5's kernel gate for ordinary
  agent/helper verbs. Merely changing the helper user would make every
  delegated command unable to open the spine.
- Choice: `mc-real` is now owned by uid 10001 and mode 6755; helpers and agents
  run as uid 10002. A privileged invocation ignores the agent-controlled
  `MC_RUN_JSON` override and resolves only the immutable `/mc/run.json`; its
  absence is host scope only in the spine-only helper/setup classes, while its
  presence keeps every agent role-scoped. The completion publisher remains a
  separate narrow wrapper for its filesystem operation. `/mc/spine` is baked
  uid-10001/mode-0700 so Docker's first named-volume copy-up establishes the
  gate without root helper startup or a CHOWN capability.
- Evidence: the production image rebuilt native arm64; focused Docker tests
  prove `mc-real` is `10001:10001` mode 6755 and an agent cannot redirect an
  injected run identity. A fresh derived helper ran as `10002:10002`, with
  network none, CapDrop=ALL, finite 500m/512MiB/128 bounds, and exactly one
  named-volume mount. `mc onboard home` initialized schema 13 through the
  path-free crossing, and the in-helper capability probe proved uid-10002
  direct-open EACCES, brokered read success, honored suid, NoNewPrivs=0,
  identity uid_map, and native arm64. The disposable helper/volume/home were
  removed after the proof.
- Spec impact: none. This removes the root-helper shortcut and restores the
  already-binding general setuid boundary; it does not broaden an agent's verb
  authorization or add a new behavior.
- Needs your decision: no.

## 2026-07-22 — production doctor is composed, never whole-verb delegated
- Where: Phase 5 `mc doctor`, `mc onboard container`, and `mc onboard verify`;
  spec §16.4/§17; the exact spine-only helper from `ca4eae4`.
- Gap: delegating the old whole doctor into the helper made its host checks
  inspect `/home/mc-model/.mission-control`, which the helper deliberately
  cannot mount, while a missing helper turned doctor into exit 2 instead of
  the required complete exit-0 diagnostic report. Running doctor on Darwin
  would instead violate Inv. 24 by opening the runtime-local spine.
- Choice: a version-fenced, 64-KiB-bounded private runtime-doctor frame returns
  exactly four helper-authoritative findings (spine, Worksources, surfaces,
  and the kernel capability probe) plus the spine UUID. Darwin computes only
  MC_HOME, routing, runtime-auth, and supervision facts, compares the UUID
  mirror, and merges the fixed nine-row order. Helper unavailability or a
  malformed/mismatched response becomes closed runtime findings in the same
  exit-0 report, never a transport-level doctor failure. Container and Verify
  reuse the in-process composition. The unnamed production wizard is now
  explicitly refused until every mixed-authority section has its own split.
- Evidence: unit tests pin the closed finding grammar/order, mismatch and
  missing-field refusals, path-free frame, fixed private scope, output bound,
  and expanded helper namespace inspection. A disposable real deployment
  returned host Home/routing facts alongside helper spine/capability facts;
  deployment identity and container-runtime were both `ok`, and `onboard
  container` returned `ok` without any MC_HOME bind. The disposable resources
  were removed; focused image Docker tests and the six-leg fast suite passed.
- Spec impact: none. This realizes the existing two-kernel ownership split and
  preserves doctor's total diagnostic contract.
- Needs your decision: no.

## 2026-07-22 — Worksource onboarding crosses canonical roots as data, not authority
- Where: Phase 5 production `mc onboard routing|worksource|tunables|surfaces`;
  spec §17 and Inv. 24; the spine-only helper boundary.
- Gap: Worksource onboarding must both prove a Darwin workspace directory and
  persist/compare its canonical root in the runtime-local spine. Whole-verb
  delegation lets the helper attempt host filesystem reads it cannot and must
  not gain; host-side execution would open SQLite in the wrong kernel. An
  inputless idempotent replay also owes reachability checks for every stored
  root, so a one-way mutation frame is insufficient.
- Choice: one closed, build/schema/deployment-bound private state frame admits
  only routing identity, Worksource schema data, tunable scalars, or console
  schedule scalars. Darwin validates and canonicalizes a supplied workspace,
  rechecks it immediately before helper execution, and rechecks every root in
  the bounded helper response before reporting success. The helper treats root
  strings only as SQLite values: it never stats, resolves, opens, or mounts
  them. Routing crosses only the deployment identity and stays wholly on the
  host; tunables and surfaces remain wholly in the helper transaction.
- Idempotency: exact Worksource/root replay is `ok`; a second ID, rebinding,
  missing profile, mismatched identity, malformed union arm, or unavailable
  returned root fails closed. A path disappearing in the unavoidable final
  syscall window can leave the already-attested schema row but cannot produce
  a healthy result; the section remains retryable and the next replay continues
  to fail until the same canonical root is restored or explicitly repaired.
- Evidence: unit tests prove canonicalization, no helper path lookup, host
  pre/post checks, identity/closed-frame refusal, and replay for all three
  spine sections. A disposable native production helper completed and replayed
  Routing, Worksource, Tunables, and Surfaces; composed doctor then reported
  both Worksource and Surfaces healthy. The exact disposable helper, volume,
  home, workspace, and host binary were removed after the proof.
- Spec impact: none. Canonical workspace roots are already required durable
  Worksource data; crossing them as inert values is the least-authority split
  that preserves the existing §17 reachability and idempotency behavior.
- Needs your decision: no.

## 2026-07-22 — credential delivery is cataloged per binding, never inferred from a Worksource profile
- Where: Phase 5 runtime-auth prerequisite; spec §11.4/§17.3; ADR-022 D2–D7;
  `routing/bindings.go` and the pre-claim env attestation.
- Gap: the v12 schema put `runtime_auth_delivery` on a sandbox profile, but the
  default route deliberately mixes MiniMax's materialized static binding with
  projected Codex/Claude OAuth bindings inside one Worksource. One profile value
  therefore cannot describe the selected run and could either forbid MiniMax's
  declared key or admit an OAuth provider key through the wrong plane. The
  provider credential-key catalog was also still absent, leaving ADR-022 D7's
  binding-specific fence inert.
- Choice: the closed production binding catalog now owns harness, credential
  channel, delivery class, provider credential env names, declared static key,
  and OAuth authority identity. Pre-claim env attestation uses the binding
  resolved from the already-attested routing table and supplies every foreign
  static key to the boundary builder. The legacy profile column remains for
  schema compatibility but grants no credential authority and is not consulted
  for the selected binding.
- Evidence: catalog closure tests bind all three production routes to exact
  credential metadata. Dispatch tests prove Codex provider/agent-identity keys,
  Claude provider keys, foreign static keys, refresh material, and the shipped
  floor refuse before claim, while MiniMax alone admits its declared
  `ANTHROPIC_AUTH_TOKEN` even when the legacy profile field says projection.
- Spec impact: none. §11.4 explicitly decides delivery per binding; this removes
  a contradictory implementation shortcut and activates the existing D7
  fence. A later schema cleanup may remove the now-derivative profile column.
- Needs your decision: no.

## 2026-07-22 — resident runtime grants are a closed production union
- Where: Phase 5 runtime-auth resident boundary; spec §11.4 amendment and
  §17.3; ADR-022 D2–D8.
- Gap: the resident accepted arbitrary binding ids, OAuth authorities, clients,
  and extra grant fields. A missing projector or missing binding then degraded
  a configured non-fake route into a token-free launch. The refresh-only shape
  also could not represent MiniMax's declared materialized static exception.
- Choice: mirror the host's three-entry production catalog at the resident
  trust boundary and parse an exact tagged union: pinned Codex/Claude OAuth
  metadata with the required account/scope evidence, or MiniMax's sole
  `ANTHROPIC_AUTH_TOKEN` static grant. Grant filenames must equal binding ids;
  duplicates, unknown fields, catalog drift, channel/harness mismatch, missing
  grants, and an absent projector refuse. Only `fake/fake` returns the
  token-free projection. Static grants bypass the token service and refresh
  broker and materialize only their declared env key.
- Evidence: resident tests cover the complete union, strict field and authority
  rejection, static projection without minting, missing/unknown binding
  refusal, absent-projector refusal before files or Docker, and fake-only
  token-free behavior. The resident fast suite passes.
- Spec impact: none. This implements the binding-specific channels and D8
  posture already decided. The TypeScript catalog mirror is derivative and
  deliberately closed; the importer will source the same pinned identities and
  live adapter no-op gates remain required before a binding is configured.
- Needs your decision: no.

## 2026-07-22 — Runtime-auth publishes one verified grant-directory transaction
- Where: Phase 5 `mc onboard runtime-auth`; spec §17.3 and §16.4's
  idempotent/fail-closed wizard posture; ADR-022 D2–D8.
- Gap: per-file rename would expose a mixed old/new binding set during key
  rotation, and accepting arbitrary credential paths could copy the operator's
  personal Codex/Claude login despite its independent refresh-token ownership.
  Provider-native auth files also carry fields the resident must never retain.
- Choice: provider flows may hand the importer only owner-owned mode-0600,
  singly linked files below owner-only `MC_HOME/runtime-auth-sources`; personal
  homes and symlink aliases refuse. The importer extracts only the closed
  refresh/static union into a mode-0700 staging directory, fsyncs every grant
  and the directory, runs forbidden-env plus every selected binding's live
  verifier against the complete staged set, and then publishes the directory
  in one filesystem operation. First publication is rename; rotation uses the
  host kernel's atomic directory exchange (`renameatx_np(RENAME_SWAP)` on
  Darwin, `renameat2(RENAME_EXCHANGE)` on Linux), followed by parent fsync and
  deletion of the displaced old store. Byte-identical replay reruns live gates
  but preserves the canonical directory identity.
- Evidence: tests prove full three-binding extraction, exact provider evidence,
  source ownership/mode/link/path fencing, ambient provider-key refusal,
  verifier-before-publication ordering, byte-for-byte rollback on a failed
  gate, atomic old-store replacement, no importer-stage residue, and healthy
  idempotent replay. Production broker tests cover flags through canonical
  publication. Darwin and Linux builds/vets plus the Go fast suite pass.
- Spec impact: none. The real verifier deliberately refuses until the
  production adapters can perform the mandated live no-op; therefore this
  micro-step cannot mark a real binding configured or publish unverified
  credentials. Provider-flow acquisition and source cleanup remain the next
  wrapper around this transaction.
- Needs your decision: no.

## 2026-07-22 — Production adapters own credentials and native transcripts at the run boundary
- Where: Phase 5 production runner/image; spec §9, §11.2/§11.4, §15.4,
  Inv. 20/26; ADR-022 D3–D8.
- Gap: the agent runner still selected only the fake harness and assumed every
  native trace was `native.jsonl`. The production image carried neither the
  Codex executable nor Claude Agent SDK, and Codex's dated rollout tree would
  otherwise land under an image-local home. The npm Codex launcher also needs
  Node even though its locked platform package already contains the native
  arm64 executable.
- Choice: close selection over `codex/chatgpt`, `claude-sdk/claude`, and
  `claude-sdk/minimax`; retain `agentRunnerRoutes` solely as an explicit fake
  stand-in list for token-free acceptance fixtures. Codex executes the exact
  locked Linux-arm64 native binary, receives only its projected auth/broker
  channel, and mounts its `sessions` tree onto the run's durable session root.
  Claude/MiniMax run through the exact locked Agent SDK with inherited settings
  disabled, closed preset tools, isolated config, and a mode-0600 fsynced
  SessionStore in that same run root. MiniMax's compatible base URL is pinned
  in both binding catalogs. Every real adapter emits the actual native handle
  and relative trace locator; the runner validates and registers that pair.
  The production Docker build installs locked dependencies outside `/app/src`
  so the source bind cannot hide them; the fake build installs none.
- Evidence: adapter tests cover exact argv/env, closed route/mode selection,
  unsafe locator refusal, durable Codex rollout discovery, Claude eager resume
  storage/idempotency/subagent traces, and SDK isolation. Resident tests prove
  the duplicate durable Codex session mount. The arm64 production image builds,
  and an unprivileged container probe reports Codex 0.145.0 and Agent SDK
  0.3.217; Docker boundary tests prove both pins and fake-route rejection. The
  six-leg fast suite passes.
- Spec impact: none. Directly invoking the locked platform executable avoids
  adding a second JavaScript runtime solely for its thin launcher; Bun remains
  the adapter runtime required by the image. Live no-op verification remains
  the import gate and is the next micro-step.
- Needs your decision: no.

## 2026-07-22 — Real-agent containers trade Docker seccomp for mandatory inner credential sandboxes
- Where: Phase 5 production adapters and Runtime-auth live no-op; spec
  §11.2/§11.4, Inv. 16/20; ADR-022 D3–D8.
- Gap: Docker's default seccomp blocks the namespace syscalls used by both
  Codex's and Claude's Linux command sandboxes. Leaving the just-added Codex
  `dangerously-bypass-approvals-and-sandbox` flag would let model-issued shell
  commands read `/mc/codex/auth.json`; likewise, merely passing Claude OAuth in
  the SDK subprocess env does not by itself strip it from Bash tools.
- Choice: real-agent and real-adapter-verifier containers alone use
  `--security-opt seccomp=unconfined`; fake, setup, landing, and other container
  classes retain Docker's default profile. In exchange, adapter startup is
  fail-closed on the inner sandbox. Codex uses a command-line custom permission
  profile extending `:workspace`, explicitly denies `/mc/codex`, admits full
  sandboxed network, and never loads project/user config or rules. Claude
  requires sandbox availability, forbids unsandboxed commands, denies its
  config/session paths, and removes both possible projected token variables
  from sandboxed commands. The production image installs only the two small
  Linux sandbox helpers required by the locked SDK/CLI.
- Evidence: unit tests pin both complete policy objects/argv and the resident's
  real-only Docker option. A production-image Docker boundary runs Codex's
  actual locked inner sandbox and proves `/mc/codex/auth.json` is absent to a
  model command; the same probe fails under default Docker seccomp, proving the
  option is causal rather than decorative. Runtime-auth verifier tests require
  the same real-only option.
- Spec impact: conservative internal mechanism. The outer container still owns
  mounts, capabilities, identity, and lifecycle; only its syscall filter is
  delegated to the mandatory inner runtime sandbox, which enforces the
  credential/tool split the outer container cannot express for a same-process
  harness. Reverting is straightforward if Docker gains a narrow shipped
  seccomp profile for the required namespace calls.
- Needs your decision: no.

## 2026-07-22 — Runtime-auth publication is gated by the installed production adapter
- Where: Phase 5 Runtime auth; spec §11.2/§11.4, Inv. 16/20; ADR-022
  D3–D8.
- Gap: structural import validation proves only grant shape and ownership. It
  cannot prove that the locked image, installed adapter source, credential
  projection, provider exchange, and native-session persistence work together.
- Choice: after writing the complete owner-only stage, mint only a short-lived
  access credential, run a fixed no-tool prompt through the selected production
  adapter in `mc-prod`, and require both a zero exit and the regular native
  trace named by its `session-start` event. Adopt a provider-rotated refresh
  token atomically inside the stage, then revalidate and fsync the exact closed
  grant set before rename publication. The verifier refuses missing or
  non-owner-only installed runner assets at `$MC_HOME/release/runner`.
- Evidence: focused tests cover all three binding argv/credential channels, the
  fixed prompt, missing native evidence, refresh-token rotation, and verifier
  mutation of the staged grant set. The fixed six-leg fast suite and production
  image sandbox boundary pass.
- Spec impact: conservative internal mechanism. It adds no credential source or
  route and makes the existing fail-closed publication rule observable at the
  actual adapter boundary.
- Needs your decision: no.

## 2026-07-22 — Home onboarding atomically owns the production runner release
- Where: Phase 5 install/Home and image source mount; spec §11.2, §17.2.
- Gap: production containers bind runner source read-only rather than baking it
  into the image, but the deployment had no canonical installed source tree.
  Running from the repository clone would couple a live deployment to mutable
  development bytes and left Runtime-auth's real verifier permanently refused.
- Choice: `install.sh` passes its absolute repository `runner/` path only as
  evidence to `mc onboard home`. The host-side Home section validates
  operator ownership, excludes symlinks/group-writable files, copies a fixed
  five-file production manifest into an owner-only sibling stage, fsyncs it,
  and atomically publishes `MC_HOME/release/runner`. Tests, fake harnesses,
  package metadata, and provider state are not copied. Replays preserve
  directory identity when bytes match; upgrades atomically exchange the whole
  tree. Runtime-auth validates that exact closed tree before mounting it.
- Evidence: deployment tests cover closed-manifest publication, file/directory
  modes, idempotent identity, changed-release replacement, symlink refusal
  before writes, and unexpected-entry refusal. A real CLI test crosses the
  Home flag, both production and fake-tag builds compile, the install shell
  contract passes, and the fixed six-leg fast suite is green.
- Spec impact: conservative internal mechanism. It implements the spec's
  runner-source bind while preserving the rule that setup writes through the
  wizard under MC_HOME rather than teaching the shell front door setup logic.
- Needs your decision: no.

## 2026-07-22 — OAuth acquisition runs only in disposable provider-owned homes
- Where: Phase 5 Runtime auth; spec §17.3, §11.4, Inv. 13/16.
- Gap: accepting already-written source paths made atomic import testable, but
  did not run the providers' own subscription login flows or prove that those
  flows were isolated from the operator's personal harness homes. Copying the
  existing personal Codex login would create a second owner of a rotating
  refresh token and was explicitly forbidden.
- Choice: `mc onboard runtime-auth --acquire` runs plain `codex login` and
  `claude auth login --claudeai` sequentially in unique mode-0700 flow homes
  below `MC_HOME/runtime-auth-sources`. The command environment is rebuilt
  from a small transport/locale/CA allowlist, with isolated `HOME`,
  `CODEX_HOME`, or `CLAUDE_CONFIG_DIR`; personal homes, provider endpoint
  overrides, and provider credentials cannot enter. Claude's explicit
  `--claudeai` form excludes its metered Console login. A selected MiniMax
  binding must have its owner-only subscription-key file before either OAuth
  flow starts. The exact created flow root is removed after success or failure;
  canonical grants survive only if every live gate passed.
- Evidence: acquisition tests assert exact provider argv, isolated environment
  and paths, ambient-key refusal before any write, failure cleanup, and success
  cleanup. A broker crossing builds provider-native fixtures through the login
  seam, publishes both OAuth grants, and proves the disposable flow is empty
  afterward. Current installed CLI help independently confirms both pinned
  command surfaces.
- Spec impact: conservative internal mechanism. It implements the mandated
  provider-owned subscription flows without adding credential formats,
  adopting personal state, or allowing a metered login mode.
- Needs your decision: no.

## 2026-07-22 — Native host units run from a closed installed payload
- Where: Phase 5 Home/supervision; spec §11.2, §12, §17.2/§17.9.
- Gap: resident and dashboard still existed only as repository TypeScript/UI
  sources. A LaunchAgent pointing into the clone would make production
  supervision depend on mutable development bytes and could accidentally
  include tests or package state in its execution surface.
- Choice: Home onboarding receives the repository root only as source evidence
  and atomically publishes a separate owner-only
  `MC_HOME/release/host`. Its closed manifest is the exact transitive
  resident/dashboard source and dashboard UI graph: sixteen files, no tests,
  lockfiles, packages, or runner/provider bytes. It is a sibling of the runner
  tree rather than part of `/app/src`, so agent containers gain no visibility
  into native-host code. Replay preserves directory identity and upgrades
  exchange the payload as one directory.
- Evidence: deployment tests prove closed publication, excluded test files,
  mode/ownership validation, idempotent identity, and changed-payload atomic
  replacement. The real Home CLI test crosses both installer source carriers
  and validates both installed trees; production/fake builds and the shell
  front-door contract pass.
- Spec impact: conservative internal mechanism. It supplies the immutable input
  required by the two native supervised units without changing either unit's
  state authority or container visibility.
- Needs your decision: no.
