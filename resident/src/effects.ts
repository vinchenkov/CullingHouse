// effects.ts — the resident's effectors: apply one `mc dispatch` effect
// JSON, in order, exactly as docs/phase1b-contract.md §5 pins:
//
//   spawn   → mkdir MC_HOME/sessions/<run_id>; write MC_HOME/runs/<run_id>.json
//             (the launch envelope lives OUTSIDE the trace-only session
//             folder, spec §4/Inv. 26, and is the only thing mounted RO at
//             /mc/run.json — no RW alias through the session mount, §11.3);
//             write the host-only mount-plan sibling; `mc __mount-recheck`;
//             docker create; `mc __mount-recheck` again; docker start —
//             ADR-016 D5's launch legs. Any drift removes the unstarted
//             container. Ordinary binds come ONLY from the effect's committed
//             mount plan, never from static config.
//   land    → docker run --rm mc-land; then `mc land report` from exit code
//   reap    → docker stop mc-run-<run_id> (exact name, strict charset, §11.6),
//             then remove the run's launch envelope and plan sibling
//             ("removed with the container", §11.3 materialize-at-spawn)
//   idle / reenter → nothing
//
// Fail-closed throughout: anything unrecognized or malformed is logged and
// skipped, never guessed at; a failed effect never crashes the loop (the
// lease machinery in the spine is the recovery path, §10).

import { posix } from "node:path";
import type { Effect, MountPlan, TickDeps } from "./types";

// Docker container-name charset (also excludes path separators, so the id
// is safe to embed in mount paths). §11.6: exact-name stop, strict charset.
const SAFE_ID = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/;

export async function applyEffect(effect: Effect, deps: TickDeps): Promise<void> {
  switch (effect.action) {
    case "idle":
      deps.log(`tick: idle${effect.reason ? ` (${effect.reason})` : ""}`);
      return;
    case "reenter":
      // [P2] per contract §2 — the skeleton resident does nothing.
      deps.log(`tick: reenter task ${effect.task_id} (no-op in skeleton)`);
      return;
    case "spawn":
      return spawn(effect, deps);
    case "land":
      return land(effect, deps);
    case "reap":
      return reap(effect, deps);
		case "interrupt":
			return reap(
				{ action: "reap", run_id: effect.run_id, stop_container: effect.stop_container },
				deps,
			);
    default:
      deps.log(
        `tick: unknown action ${JSON.stringify((effect as { action?: unknown }).action)}; ignored (fail-closed)`,
      );
      return;
  }
}

/** Host dir for materialized launch envelopes — a SIBLING of sessions/, never
 * inside a session folder (spec §4: run.json is "mounted … separately from
 * the session folder"; Inv. 26: the folder holds nothing but the trace). */
function runsDir(mcHome: string): string {
  return `${mcHome}/runs`;
}

/** Host path of one run's launch envelope. */
function runJsonHostPath(mcHome: string, runId: string): string {
  return `${runsDir(mcHome)}/${runId}.json`;
}

/** Host path of one run's mount-plan sibling. It is host-only recheck input
 * for `mc __mount-recheck` and is NEVER mounted into the container — the
 * agent-visible envelope carries no host source paths. */
function mountPlanHostPath(mcHome: string, runId: string): string {
  return `${runsDir(mcHome)}/${runId}.mounts.json`;
}

/** The resident consumes only what it binds: source/destination/access must
 * be sound for docker -v grammar; everything deeper is mc's recheck. Returns
 * a refusal reason or null. */
function invalidMountPlanReason(plan: MountPlan | undefined): string | null {
  if (plan === null || typeof plan !== "object" || plan.version !== 1 || !Array.isArray(plan.entries)) {
    return "dispatch effect carries no explicit mount plan";
  }
  for (const entry of plan.entries) {
    if (entry === null || typeof entry !== "object") return "mount entry is not an object";
    if (typeof entry.source !== "string" || !entry.source.startsWith("/") || entry.source.includes(":")) {
      return `mount source ${JSON.stringify(entry.source)} is not an absolute colon-free host path`;
    }
    if (
      typeof entry.destination !== "string" ||
      !(entry.destination === "/workspace" || entry.destination.startsWith("/workspace/")) ||
      entry.destination.includes(":")
    ) {
      return `mount destination ${JSON.stringify(entry.destination)} is outside the colon-free /workspace namespace`;
    }
    if (entry.access !== "ro" && entry.access !== "rw") {
      return `mount access ${JSON.stringify(entry.access)} is not ro|rw`;
    }
		if (plan.task_precreate !== undefined &&
			!entry.destination.startsWith("/workspace/artifacts/") &&
			!entry.destination.startsWith("/workspace/references/")) {
			return "task precreate plan fabricates a not-yet-existing task mount row";
		}
  }
	const step = plan.task_precreate;
	if (step !== undefined) {
		if (step === null || typeof step !== "object" ||
			!Number.isSafeInteger(step.task_id) || step.task_id < 1 ||
			step.child_mode !== 0o700 ||
			typeof step.workspace_root !== "string" || !step.workspace_root.startsWith("/") ||
			posix.normalize(step.workspace_root) !== step.workspace_root ||
			step.tasks_parent === null || typeof step.tasks_parent !== "object" ||
			step.tasks_parent.canonical !== posix.join(step.workspace_root, ".mission-control", "tasks") ||
			!(/^(0|[1-9][0-9]*)$/).test(step.tasks_parent.device) ||
			!(/^(0|[1-9][0-9]*)$/).test(step.tasks_parent.inode) ||
			!Number.isSafeInteger(step.tasks_parent.owner_uid) || step.tasks_parent.owner_uid < 0) {
			return "task precreate descriptor is malformed";
		}
		if (step.recover_root !== undefined &&
			(step.recover_root === null || typeof step.recover_root !== "object" ||
				step.recover_root.canonical !== posix.join(step.tasks_parent.canonical, `task-${step.task_id}`) ||
				!(/^(0|[1-9][0-9]*)$/).test(step.recover_root.device) ||
				!(/^(0|[1-9][0-9]*)$/).test(step.recover_root.inode) ||
				step.recover_root.owner_uid !== step.tasks_parent.owner_uid)) {
			return "task recovery root descriptor is malformed";
		}
		const invalidSetup = invalidTaskSetupReason(step.setup);
		if (invalidSetup !== null) return invalidSetup;
	}
	if (plan.accepted_seal_rebuild !== undefined) {
		const step = plan.accepted_seal_rebuild;
		const hex = /^[0-9a-f]+$/;
		const oidLength = step.object_format === "sha1" ? 40 : step.object_format === "sha256" ? 64 : 0;
		if (step === null || typeof step !== "object" ||
			!Number.isSafeInteger(step.task_id) || step.task_id < 1 ||
			typeof step.run_id !== "string" || !SAFE_ID.test(step.run_id) ||
			typeof step.completion_request_id !== "string" || !/^[0-9a-f]{16}$/.test(step.completion_request_id) ||
			(step.object_format !== "sha1" && step.object_format !== "sha256") ||
			typeof step.sealed_sha !== "string" || step.sealed_sha.length !== oidLength || !hex.test(step.sealed_sha) ||
			typeof step.closure_digest !== "string" || step.closure_digest.length !== 64 || !hex.test(step.closure_digest) ||
			typeof step.manifest_digest !== "string" || step.manifest_digest.length !== 64 || !hex.test(step.manifest_digest) ||
			typeof step.device !== "string" || !/^(0|[1-9][0-9]*)$/.test(step.device) ||
			typeof step.inode !== "string" || !/^(0|[1-9][0-9]*)$/.test(step.inode) ||
			!Number.isSafeInteger(step.owner_uid) || step.owner_uid < 0) {
			return "accepted seal rebuild descriptor is malformed";
		}
		if (plan.task_precreate !== undefined) {
			return "accepted seal rebuild cannot share a first-task setup plan";
		}
		// Spawn handles this closed setup carrier before ordinary route/container
		// work. It never reaches the generic agent-create path.
	}
  return null;
}

/** Mirrors mc's validatePrivateTaskSetup fail-closed: a closed mode pair, a
 * closed object-format set, fresh = target and no pins, retry = pins shaped
 * as the task_assignments CHECKs demand and no target. */
function invalidTaskSetupReason(setup: unknown): string | null {
	if (setup === null || typeof setup !== "object") {
		return "task setup instruction is malformed";
	}
	const s = setup as Record<string, unknown>;
	if (s.object_format !== "sha1" && s.object_format !== "sha256") {
		return "task setup object format is outside the closed set";
	}
	if (s.mode === "fresh") {
		if (typeof s.target_ref !== "string" || s.target_ref.length === 0 ||
			s.pinned_base_sha !== undefined || s.pinned_closure_digest !== undefined ||
			s.pinned_local_repo_uuid !== undefined) {
			return "task setup fresh instruction is malformed";
		}
		return null;
	}
	if (s.mode === "retry") {
		const shaLen = s.object_format === "sha1" ? 40 : 64;
		const hex = /^[0-9a-f]+$/;
		const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
		if (s.target_ref !== undefined ||
			typeof s.pinned_base_sha !== "string" || s.pinned_base_sha.length !== shaLen ||
			!hex.test(s.pinned_base_sha) ||
			typeof s.pinned_closure_digest !== "string" || s.pinned_closure_digest.length !== 64 ||
			!hex.test(s.pinned_closure_digest) ||
			typeof s.pinned_local_repo_uuid !== "string" || !uuid.test(s.pinned_local_repo_uuid)) {
			return "task setup retry instruction is malformed";
		}
		return null;
	}
	return "task setup mode is outside the closed pair";
}

type SpawnEffect = Extract<Effect, { action: "spawn" }>;
type LandEffect = Extract<Effect, { action: "land" }>;
type ReapEffect = Extract<Effect, { action: "reap" }>;

/** D6's former-producer fence. A missing exact container is the only success;
 * a live, exited-but-not-removed, malformed, or inspect-unavailable object
 * keeps downstream accepted-seal setup unprepared. Runner execution is inside
 * the pipeline agent container; network guards are added only with ADR-018's
 * guard runtime and must join this exact inventory then. */
export async function requireAcceptedSealProducerAbsent(runId: string, deps: TickDeps): Promise<void> {
	if (!SAFE_ID.test(runId)) throw new Error("accepted seal producer run id is malformed");
	for (const component of ["mc-run", "mc-setup"]) {
		const name = `${component}-${runId}`;
		const inspected = await deps.docker(["inspect", "--type", "container", name]);
		if (inspected.exitCode === 1) continue; // Docker's exact not-found result.
		if (inspected.exitCode !== 0) {
			throw new Error(`cannot confirm accepted seal producer absence for ${name}: docker inspect exited ${inspected.exitCode}`);
		}
		let records: unknown;
		try {
			records = JSON.parse(inspected.stdout);
		} catch {
			throw new Error(`accepted seal producer identity is ambiguous for ${name}`);
		}
		if (!Array.isArray(records) || records.length !== 1 || records[0] === null || typeof records[0] !== "object") {
			throw new Error(`accepted seal producer identity is ambiguous for ${name}`);
		}
		const record = records[0] as {
			Id?: unknown; Name?: unknown; Config?: { Labels?: Record<string, unknown> }; State?: { Status?: unknown };
		};
		const labels = record.Config?.Labels;
		if (typeof record.Id !== "string" || !/^[0-9a-f]{64}$/.test(record.Id) ||
			record.Name !== `/${name}` || labels === undefined ||
			labels["mc-managed"] !== "true" || labels["mc-tier"] !== "pipeline" || labels["mc-run-id"] !== runId ||
			typeof record.State?.Status !== "string" ||
			!(["created", "running", "paused", "restarting", "removing", "exited", "dead"] as string[]).includes(record.State.Status)) {
			throw new Error(`accepted seal producer identity is ambiguous for ${name}`);
		}
		throw new Error(`accepted seal producer is still present: ${name} (${record.State.Status})`);
	}
}

/** §10 effect order, literally: folder → run.json → plan sibling →
 * recheck → create → recheck → start (ADR-016 D5's launch legs). */
async function spawn(effect: SpawnEffect, deps: TickDeps): Promise<void> {
  const { config, log } = deps;
  const { run_id, role } = effect;
	if (!SAFE_ID.test(run_id)) {
    log(`spawn refused: run_id ${JSON.stringify(run_id)} fails the container-name charset`);
    return;
  }
	if (typeof effect.brief !== "string" || effect.brief.length === 0) {
		log("spawn refused: dispatch effect carries no immutable brief (fail-closed)");
		return;
	}
  const planRefusal = invalidMountPlanReason(effect.mount_plan);
  if (planRefusal !== null) {
    log(`spawn refused: ${planRefusal} (fail-closed)`);
    return;
  }
  const behavior = config.roleBehaviors[role];
  if (behavior === undefined) {
    log(`spawn refused: no behavior mapped for role ${JSON.stringify(role)} (fail-closed)`);
    return;
  }
	if (effect.mount_plan.accepted_seal_rebuild !== undefined) {
		const step = effect.mount_plan.accepted_seal_rebuild;
		if (role.split(":", 1)[0] !== "verifier" || effect.subject_id !== step.task_id) {
			log("spawn refused: accepted-seal rebuild does not match the claimed Verifier (fail-closed)");
			return;
		}
		const taskRoot = effect.mount_plan.entries.find((entry) =>
			entry.logical_id === "task-root" && entry.destination === "/workspace" && entry.access === "ro",
		);
		const suffix = `/.mission-control/tasks/task-${step.task_id}`;
		if (taskRoot === undefined || !taskRoot.source.endsWith(suffix)) {
			log("spawn refused: accepted-seal rebuild has no canonical task-root bind (fail-closed)");
			return;
		}
		const workspaceRoot = taskRoot.source.slice(0, -suffix.length);
		if (workspaceRoot === "" || posix.normalize(workspaceRoot) !== workspaceRoot) {
			log("spawn refused: accepted-seal rebuild task-root bind has no canonical Worksource parent (fail-closed)");
			return;
		}
		try {
			await requireAcceptedSealProducerAbsent(step.run_id, deps);
			const sealRoot = await deps.recheckAcceptedSeal(step);
			await runAcceptedSealRebuild(run_id, step, taskRoot.source, workspaceRoot, sealRoot, deps);
		} catch (err) {
			log(`spawn refused: accepted-seal setup precondition failed: ${(err as Error).message} (fail-closed)`);
			return;
		}
		log(`spawn ${run_id}: accepted-seal rebuild recorded and continued for task ${step.task_id}`);
		return;
	}
	if (effect.mount_plan.task_precreate !== undefined) {
		const step = effect.mount_plan.task_precreate;
		if (role.split(":", 1)[0] !== "worker" || effect.subject_id !== step.task_id) {
			log("spawn refused: task precreate does not match the claimed standalone Worker (fail-closed)");
			return;
		}
		await deps.recheckTaskParent(step);
		const registered = step.recover_root === undefined
			? await deps.precreateTaskSkeleton(step)
			: await deps.recoverTaskSkeleton(step);
		if (registered === null || typeof registered !== "object" ||
			registered.canonical !== posix.join(step.tasks_parent.canonical, `task-${step.task_id}`) ||
			!(/^(0|[1-9][0-9]*)$/).test(registered.device) ||
			!(/^(0|[1-9][0-9]*)$/).test(registered.inode) ||
			registered.owner_uid !== step.tasks_parent.owner_uid) {
			throw new Error("precreated task root returned invalid registration evidence");
		}
		await deps.registerTaskRoot(run_id, step.task_id, registered);
		log(`spawn ${run_id}: task root registered; running first-task setup`);
		await runFirstTaskSetup(run_id, step, registered.canonical, deps);
		// The agent launch stays gated on the setup receipt: the NEXT dispatch
		// resolves the now-materialized skeleton into the 15-row task plan.
		// Starting here, without those rows, would expose an empty workspace.
		return;
	}
	if (effect.harness !== "fake" || effect.model_binding !== "fake") {
		log(
			`spawn refused: unsupported route ${JSON.stringify(`${effect.harness}/${effect.model_binding}`)}; ` +
				"the current resident image contains only the explicitly test-tagged fake adapter (fail-closed)",
		);
		return;
	}

  // 1. folder — the trace-only session folder (Inv. 26) plus the sibling
  // envelope dir: run.json must live OUTSIDE the session folder so the RW
  // session mount never aliases the RO /mc/run.json mount (spec §4, §11.3).
  const sessionDir = `${config.mcHome}/sessions/${run_id}`;
  await deps.fs.mkdir(sessionDir);
  await deps.fs.mkdir(runsDir(config.mcHome));

  // 2. run.json (contract §6 fixed schema; materialize-at-spawn, §11.3).
  // Host source paths deliberately stay OUT of the agent-visible envelope;
  // they live only in the host-side plan sibling.
  const runJson = {
    run_id,
    tier: "pipeline",
    role,
    subject_id: effect.subject_id ?? null,
    worksource: effect.worksource,
    harness: effect.harness,
    model_binding: effect.model_binding,
    mode: "fresh",
		brief: effect.brief,
    pool_ids: effect.pool_ids ?? [],
    heartbeat_interval_s: effect.heartbeat_interval_s,
    harness_config: { behavior },
    mounts: { session: "/mc/session" },
  };
  const runJsonPath = runJsonHostPath(config.mcHome, run_id);
  await deps.fs.writeFile(runJsonPath, JSON.stringify(runJson, null, 2) + "\n");
  const planPath = mountPlanHostPath(config.mcHome, run_id);
  await deps.fs.writeFile(planPath, JSON.stringify(effect.mount_plan) + "\n", { mode: 0o600 });
  const removeLaunchFiles = async () => {
    await deps.fs.rm(runJsonPath);
    await deps.fs.rm(planPath);
  };
  const recheck = async (leg: string): Promise<boolean> => {
    if (effect.mount_plan.entries.length === 0) return true; // nothing can drift
    const res = await deps.runMc(["__mount-recheck", planPath]);
    if (res.exitCode !== 0) {
      log(`spawn ${run_id}: mount recheck refused ${leg} (exit ${res.exitCode}): ${res.stderr.trim()}`);
      return false;
    }
    return true;
  };

  // 3. container, in D5 order: recheck → create → recheck → start. Ordinary
  // binds come only from the committed plan; the Phase-1 static workspace
  // bind is gone.
  if (!(await recheck("before create"))) {
    await removeLaunchFiles();
    return;
  }
  const name = `mc-run-${run_id}`;
  const planBinds = effect.mount_plan.entries.flatMap((entry) => [
    "-v", `${entry.source}:${entry.destination}${entry.access === "ro" ? ":ro" : ""}`,
  ]);
  const created = await deps.docker([
    "create", "--rm",
    "--network", "none",
    "--name", name,
    "--label", "mc-managed=true",
    "--label", "mc-tier=pipeline",
		"--label", `mc-run-id=${run_id}`,
    "-v", `${sessionDir}:/mc/session`,
    "-v", `${runJsonPath}:/mc/run.json:ro`,
    "-v", `${config.behaviorsDir}:/mc/behaviors:ro`,
    "-v", `${config.runnerSrcDir}:/app/src:ro`,
    ...planBinds,
    "-v", `${config.spineVolume}:${posix.dirname(config.spineDbPath)}`,
    "-e", `MC_SPINE=${config.spineDbPath}`,
    config.image,
    ...config.agentCmd,
  ]);
  if (created.exitCode !== 0) {
    log(`spawn ${run_id}: docker create failed (exit ${created.exitCode}): ${created.stderr.trim()}`);
    await removeLaunchFiles();
    return;
  }
  if (!(await recheck("after create"))) {
    // Drift inside the create window: remove the confirmed-unstarted
    // container before its launch inputs (ADR-016 D5).
    const removed = await deps.docker(["rm", name]);
    if (removed.exitCode !== 0) {
      log(`spawn ${run_id}: docker rm of the unstarted container exited ${removed.exitCode}: ${removed.stderr.trim()}`);
    }
    await removeLaunchFiles();
    return;
  }
  const started = await deps.docker(["start", name]);
  if (started.exitCode !== 0) {
    log(`spawn ${run_id}: docker start failed (exit ${started.exitCode}): ${started.stderr.trim()}`);
  }
}

/** Post-registration first-task setup (ADR-016 D5): materialize the frozen
 * envelope, run the one-shot network=none setup container over ADR-017's
 * setup mount table inside ADR-019's setup-class envelope, and on success
 * hand the SetupResult byte-exactly to host `mc task setup-record`. Failures
 * throw (the tick loop logs them; the lease machinery is the recovery path)
 * and keep the envelope on disk as evidence. */
async function runFirstTaskSetup(
	run_id: string,
	step: NonNullable<MountPlan["task_precreate"]>,
	taskRoot: string,
	deps: TickDeps,
): Promise<void> {
	const { config, log } = deps;
	const setup = step.setup;
	// The envelope is credential-free and host-path-free by construction:
	// source_repo/task_root are the container destinations, branch and
	// worktree name are pure derivations of the task id (the executor refuses
	// anything else), and the pins restate the committed instruction.
	const envelope = {
		schema_version: 1,
		operation: "first-task-closure-extraction",
		run_id,
		task_id: step.task_id,
		mode: setup.mode,
		object_format: setup.object_format,
		...(setup.mode === "fresh"
			? { target_ref: setup.target_ref }
			: {
					pinned_base_sha: setup.pinned_base_sha,
					pinned_closure_digest: setup.pinned_closure_digest,
					pinned_local_repo_uuid: setup.pinned_local_repo_uuid,
				}),
		branch: `mc/task-${step.task_id}`,
		worktree_name: `mc-task-${step.task_id}`,
		source_repo: "/repo/source",
		task_root: "/repo/task",
	};
	const setupJsonPath = `${runsDir(config.mcHome)}/${run_id}.setup.json`;
	const coverDir = `${runsDir(config.mcHome)}/${run_id}.setup-cover`;
	await deps.fs.mkdir(runsDir(config.mcHome));
	// ADR-017: an empty RO cover masks every task/projection root whenever
	// /repo/source is present — the setup container reads the real Worksource
	// but never another task's bytes through it.
	await deps.fs.mkdir(coverDir);
	// 0644, not 0600: the envelope is mounted RO into a uid-10002 container
	// and carries no secret by construction.
	await deps.fs.writeFile(setupJsonPath, JSON.stringify(envelope, null, 2) + "\n", { mode: 0o644 });
	const run = await deps.docker([
		"run", "--rm",
		"--name", `mc-setup-${run_id}`,
		"--network", "none",
		"--label", "mc-managed=true",
		"--label", "mc-tier=pipeline",
		"--label", `mc-run-id=${run_id}`,
		"--user", "10002:10002",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges=true",
		"--cpus", "1",
		"--memory", "1024m",
		"--pids-limit", "128",
		"-v", `${step.workspace_root}:/repo/source:ro`,
		"-v", `${coverDir}:/repo/source/.mission-control:ro`,
		"-v", `${taskRoot}:/repo/task:ro`,
		"-v", `${taskRoot}/source:/repo/task/source`,
		"-v", `${taskRoot}/git:/repo/task/git`,
		"-v", `${setupJsonPath}:/mc/setup.json:ro`,
		config.image,
		"mc", "__setup-first-task", "/mc/setup.json",
	]);
	if (run.exitCode !== 0) {
		throw new Error(`first-task setup container exited ${run.exitCode}: ${run.stderr.trim()}`);
	}
	// Preserve the executor's exact result bytes when crossing back to the
	// host. `trim` is only a presence check: JSON permits its encoder's
	// terminal newline, and normalizing it would contradict this handoff's
	// byte-exact contract.
	const result = run.stdout;
	if (result.trim() === "") {
		throw new Error("first-task setup container returned no SetupResult");
	}
	// The SetupResult passes through byte-exactly: the resident never
	// reinterprets evidence, and the host verb re-verifies the landed store.
	const recorded = await deps.runMc([
		"task", "setup-record",
		"--run", run_id,
		"--workspace", step.workspace_root,
		"--result", result,
	]);
	if (recorded.exitCode !== 0) {
		throw new Error(`first-task setup record refused (exit ${recorded.exitCode}): ${recorded.stderr.trim()}`);
	}
	// The precreate-only Run is intentionally not an agent plan.  Once the
	// host has recorded the receipt-attested closure assignment, finish that
	// setup operation under its exact run/lease fence.  A later tick will make
	// a new claim and attest the now-existing 15-row Worker plan; it must never
	// launch an agent from this immutable zero-row plan.
	const continued = await deps.runMc(["task", "setup-continue", "--run", run_id]);
	if (continued.exitCode !== 0) {
		throw new Error(`first-task setup continuation refused (exit ${continued.exitCode}): ${continued.stderr.trim()}`);
	}
	await deps.fs.rm(setupJsonPath);
	log(`spawn ${run_id}: first-task setup recorded and continued for task ${step.task_id}`);
}

/** D6's closed accepted-seal setup crossing. The resident receives no source
 * repository or generic command: only the exact re-attested immutable seal,
 * the canonical task-root plan bind, and a credential-free envelope enter the
 * networkless setup class. The host record and continuation re-prove the live
 * Verifier lease before its terminal is exposed. */
async function runAcceptedSealRebuild(
	runId: string,
	step: NonNullable<MountPlan["accepted_seal_rebuild"]>,
	taskRoot: string,
	workspaceRoot: string,
	sealRoot: string,
	deps: TickDeps,
): Promise<void> {
	const { config } = deps;
	const envelope = {
		schema_version: 1, operation: "accepted-seal-rebuild", run_id: runId,
		task_id: step.task_id, object_format: step.object_format,
		completion_request_id: step.completion_request_id, sealed_sha: step.sealed_sha,
		closure_digest: step.closure_digest, manifest_digest: step.manifest_digest,
		seal_root: "/repo/seal", task_root: "/repo/task", seal_device: step.device,
		seal_inode: step.inode, seal_owner_uid: step.owner_uid,
	};
	const setupJsonPath = `${runsDir(config.mcHome)}/${runId}.setup.json`;
	await deps.fs.mkdir(runsDir(config.mcHome));
	await deps.fs.writeFile(setupJsonPath, JSON.stringify(envelope, null, 2) + "\n", { mode: 0o644 });
	const run = await deps.docker([
		"run", "--rm", "--name", `mc-setup-${runId}`, "--network", "none",
		"--label", "mc-managed=true", "--label", "mc-tier=pipeline", "--label", `mc-run-id=${runId}`,
		"--user", "10002:10002", "--cap-drop", "ALL", "--security-opt", "no-new-privileges=true",
		"--cpus", "1", "--memory", "1024m", "--pids-limit", "128",
		"-v", `${sealRoot}:/repo/seal:ro`, "-v", `${taskRoot}:/repo/task:ro`,
		"-v", `${taskRoot}/source:/repo/task/source`, "-v", `${taskRoot}/git:/repo/task/git`,
		"-v", `${setupJsonPath}:/mc/setup.json:ro`, config.image,
		"mc", "__setup-accepted-seal", "/mc/setup.json",
	]);
	if (run.exitCode !== 0) throw new Error(`accepted-seal setup container exited ${run.exitCode}: ${run.stderr.trim()}`);
	if (run.stdout.trim() === "") throw new Error("accepted-seal setup container returned no SetupResult");
	const recorded = await deps.runMc([
		"task", "accepted-seal-record", "--run", runId, "--workspace", workspaceRoot, "--result", run.stdout,
	]);
	if (recorded.exitCode !== 0) throw new Error(`accepted-seal setup record refused (exit ${recorded.exitCode}): ${recorded.stderr.trim()}`);
	const continued = await deps.runMc(["task", "accepted-seal-continue", "--run", runId]);
	if (continued.exitCode !== 0) throw new Error(`accepted-seal setup continuation refused (exit ${continued.exitCode}): ${continued.stderr.trim()}`);
	await deps.fs.rm(setupJsonPath);
}

/** §7 Landing: run the baked mc-land script, then report its exit code.
 * The land container gets only the workspace — landing holds no lease and
 * never touches the spine; the resident reports via host mc. */
async function land(effect: LandEffect, deps: TickDeps): Promise<void> {
  const { config, log } = deps;
  const res = await deps.docker([
    "run", "--rm",
    "--network", "none",
    "--label", "mc-managed",
    "-v", `${config.workspaceRoot}:/workspace/source`,
    config.image,
    ...config.landCmd,
    effect.branch,
    effect.verified_sha,
    effect.target_ref,
  ]);
  const report =
    res.exitCode === 0
      ? ["land", "report", String(effect.task_id), "--status", "success"]
      : [
          "land", "report", String(effect.task_id),
          "--status", "failure",
          "--reason", `mc-land exited ${res.exitCode}`,
        ];
  if (res.exitCode !== 0) {
    log(`land task ${effect.task_id}: mc-land failed (exit ${res.exitCode}): ${res.stderr.trim()}`);
  } else if (res.stderr.trim() !== "") {
    // mc-land may have moved main successfully but left removable worktree /
    // branch residue. Canonical state must report the landing truthfully;
    // keep the cleanup debt visible to the resident health log.
    log(`land task ${effect.task_id}: mc-land warning: ${res.stderr.trim()}`);
  }
  const reported = await deps.runMc(report);
  if (reported.exitCode !== 0) {
    log(
      `land task ${effect.task_id}: mc land report failed (exit ${reported.exitCode}): ${reported.stderr.trim()}`,
    );
  }
}

/** §11.6 decide-then-effect: the decision (and its writes) are already
 * committed. A run has at most one of the two exact derivable container
 * names: its setup-class predecessor or its ordinary agent container. Stop
 * both names so an expired setup writer cannot race D6 recovery cleanup.
 * Then remove the run's launch envelope ("removed with the container", §11.3). */
async function reap(effect: ReapEffect, deps: TickDeps): Promise<void> {
  const { log } = deps;
  if (!effect.stop_container) {
    log(`reap ${effect.run_id}: stop_container not set; nothing to do`);
    return;
  }
  if (!SAFE_ID.test(effect.run_id)) {
    log(`reap refused: run_id ${JSON.stringify(effect.run_id)} fails the container-name charset`);
    return;
  }
	for (const name of [`mc-setup-${effect.run_id}`, `mc-run-${effect.run_id}`]) {
		const res = await deps.docker(["stop", name]);
		if (res.exitCode !== 0) {
			// Already-gone containers are expected (crash path); log and continue.
			log(`reap ${effect.run_id}: docker stop ${name} exited ${res.exitCode}: ${res.stderr.trim()}`);
		}
	}
  // The envelope and its plan sibling are ephemeral launch input (spec §4,
  // §11.3): removed with the container. Normal-exit removal awaits a
  // container-exit hook (see deviation note); missing files are fine (force
  // semantics).
  await deps.fs.rm(runJsonHostPath(deps.config.mcHome, effect.run_id));
  await deps.fs.rm(mountPlanHostPath(deps.config.mcHome, effect.run_id));
}
