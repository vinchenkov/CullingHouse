// effects.ts — the resident's effectors: apply one `mc dispatch` effect
// JSON, in order, exactly as docs/phase1b-contract.md §5 pins:
//
//   spawn   → mkdir MC_HOME/sessions/<run_id>; write MC_HOME/runs/<run_id>.json
//             (the launch envelope lives OUTSIDE the trace-only session
//             folder, spec §4/Inv. 26, and is the only thing mounted RO at
//             /mc/run.json — no RW alias through the session mount, §11.3);
//             docker run
//   land    → docker run --rm mc-land; then `mc land report` from exit code
//   reap    → docker stop mc-run-<run_id> (exact name, strict charset, §11.6),
//             then remove the run's launch envelope ("removed with the
//             container", §11.3 materialize-at-spawn)
//   idle / reenter → nothing
//
// Fail-closed throughout: anything unrecognized or malformed is logged and
// skipped, never guessed at; a failed effect never crashes the loop (the
// lease machinery in the spine is the recovery path, §10).

import { posix } from "node:path";
import type { Effect, TickDeps } from "./types";

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

type SpawnEffect = Extract<Effect, { action: "spawn" }>;
type LandEffect = Extract<Effect, { action: "land" }>;
type ReapEffect = Extract<Effect, { action: "reap" }>;

/** §10 effect order, literally: folder → run.json → container. */
async function spawn(effect: SpawnEffect, deps: TickDeps): Promise<void> {
  const { config, log } = deps;
  const { run_id, role } = effect;
  if (!SAFE_ID.test(run_id)) {
    log(`spawn refused: run_id ${JSON.stringify(run_id)} fails the container-name charset`);
    return;
  }
	if (effect.harness !== "fake" || effect.model_binding !== "fake") {
		log(
			`spawn refused: unsupported route ${JSON.stringify(`${effect.harness}/${effect.model_binding}`)}; ` +
				"the current resident image contains only the explicitly test-tagged fake adapter (fail-closed)",
		);
		return;
	}
	if (typeof effect.brief !== "string" || effect.brief.length === 0) {
		log("spawn refused: dispatch effect carries no immutable brief (fail-closed)");
		return;
	}
  const behavior = config.roleBehaviors[role];
  if (behavior === undefined) {
    log(`spawn refused: no behavior mapped for role ${JSON.stringify(role)} (fail-closed)`);
    return;
  }

  // 1. folder — the trace-only session folder (Inv. 26) plus the sibling
  // envelope dir: run.json must live OUTSIDE the session folder so the RW
  // session mount never aliases the RO /mc/run.json mount (spec §4, §11.3).
  const sessionDir = `${config.mcHome}/sessions/${run_id}`;
  await deps.fs.mkdir(sessionDir);
  await deps.fs.mkdir(runsDir(config.mcHome));

  // 2. run.json (contract §6 fixed schema; materialize-at-spawn, §11.3)
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
    mounts: { session: "/mc/session", workspace: "/workspace/source" },
  };
  const runJsonPath = runJsonHostPath(config.mcHome, run_id);
  await deps.fs.writeFile(runJsonPath, JSON.stringify(runJson, null, 2) + "\n");

  // 3. container (contract §1 row "agent container": --rm, --network none,
  // labels, exact name; runner source RO; spine volume — the fake agent's
  // exec steps invoke the real scoped mc, contract §4)
  const res = await deps.docker([
    "run", "-d", "--rm",
    "--network", "none",
    "--name", `mc-run-${run_id}`,
    "--label", "mc-managed",
    "--label", "mc-tier=pipeline",
    "-v", `${sessionDir}:/mc/session`,
    "-v", `${runJsonPath}:/mc/run.json:ro`,
    "-v", `${config.behaviorsDir}:/mc/behaviors:ro`,
    "-v", `${config.runnerSrcDir}:/app/src:ro`,
    "-v", `${config.workspaceRoot}:/workspace/source`,
    "-v", `${config.spineVolume}:${posix.dirname(config.spineDbPath)}`,
    "-e", `MC_SPINE=${config.spineDbPath}`,
    config.image,
    ...config.agentCmd,
  ]);
  if (res.exitCode !== 0) {
    log(`spawn ${run_id}: docker run failed (exit ${res.exitCode}): ${res.stderr.trim()}`);
  }
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
 * committed; the resident only stops the exact-named container, then removes
 * the run's launch envelope ("removed with the container", §11.3). */
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
  const res = await deps.docker(["stop", `mc-run-${effect.run_id}`]);
  if (res.exitCode !== 0) {
    // Already-gone containers are expected (crash path); log and continue.
    log(`reap ${effect.run_id}: docker stop exited ${res.exitCode}: ${res.stderr.trim()}`);
  }
  // The envelope is ephemeral launch input (spec §4, §11.3): remove it with
  // the container. Normal-exit removal awaits a container-exit hook (see
  // deviation note); missing files are fine (force semantics).
  await deps.fs.rm(runJsonHostPath(deps.config.mcHome, effect.run_id));
}
