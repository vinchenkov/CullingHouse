// main.ts — real-world wiring for the skeleton resident.
//
// Usage:   bun src/main.ts --config <path-to-json>
// Env:     MC_TICK_INTERVAL_MS overrides the config's tickIntervalMs
//          (contract §5; default tick_interval_s 60 → 60000 ms).
//
// The config file is ResidentConfig plus:
//   mcPath          — host mc binary (self-delegates via MC_HELPER, §11.5;
//                     the spawner sets that env, the resident inherits it)
//   tickIntervalMs  — optional, default 60000
//
// The e2e (contract §7) spawns this process, points it at its test config,
// and only ever observes — advancement stays timer-driven.

import { mkdir, rm, writeFile } from "node:fs/promises";
import { startTickLoop } from "./tick-loop";
import type { Exec, ResidentConfig, TickDeps } from "./types";
import { CONFIG_SCHEMA_VERSION, execMcVia } from "./resident-control";
import { precreateTaskSkeleton, recheckAcceptedSeal } from "./task-skeleton";

interface MainConfig extends ResidentConfig {
  mcPath: string;
  tickIntervalMs?: number;
  releaseBuildId: string;
  configSchemaVersion: number;
}

function execVia(binary: string): Exec {
  return async (argv) => {
    const proc = Bun.spawn([binary, ...argv], {
      stdin: "ignore",
      stdout: "pipe",
      stderr: "pipe",
    });
    const [stdout, stderr, exitCode] = await Promise.all([
      new Response(proc.stdout).text(),
      new Response(proc.stderr).text(),
      proc.exited,
    ]);
    return { exitCode, stdout, stderr };
  };
}

async function main(): Promise<void> {
  const idx = process.argv.indexOf("--config");
  const configPath = idx >= 0 ? process.argv[idx + 1] : undefined;
  if (!configPath) {
    console.error("usage: bun src/main.ts --config <path>");
    process.exit(2);
  }
  const config = (await Bun.file(configPath).json()) as MainConfig;

  if (config.configSchemaVersion !== CONFIG_SCHEMA_VERSION ||
      typeof config.releaseBuildId !== "string" ||
      !/^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$/.test(config.releaseBuildId)) {
    console.error("resident: config release/schema identity is missing or unsupported");
    process.exit(2);
  }

  const envInterval = process.env["MC_TICK_INTERVAL_MS"];
  const intervalMs = envInterval
    ? Number.parseInt(envInterval, 10)
    : (config.tickIntervalMs ?? 60_000);
  if (!Number.isInteger(intervalMs) || intervalMs <= 0) {
    console.error(`resident: invalid tick interval ${JSON.stringify(envInterval)}`);
    process.exit(2);
  }
	const runMc = execMcVia([config.mcPath], {
		mcHome: config.mcHome,
		releaseBuildId: config.releaseBuildId,
		configSchemaVersion: config.configSchemaVersion,
	});
  const deps: TickDeps = {
    intervalMs,
    setTimer: (fn, ms) => setInterval(fn, ms),
    clearTimer: (h) => clearInterval(h as ReturnType<typeof setInterval>),
		runMc,
    docker: execVia("docker"),
    log: (msg) => console.error(`[resident ${new Date().toISOString()}] ${msg}`),
    fs: {
      mkdir: async (path) => {
        await mkdir(path, { recursive: true });
      },
      writeFile: (path, data, opts) =>
        opts?.mode !== undefined ? writeFile(path, data, { mode: opts.mode }) : writeFile(path, data),
      rm: (path) => rm(path, { force: true }),
    },
		precreateTaskSkeleton,
		recheckTaskParent: async (step) => {
			const frame = JSON.stringify({
				child_mode: step.child_mode,
				// JSON.stringify drops undefined members, matching the Go
				// struct's omitempty: fresh carries no pins, retry no target.
				setup: {
					mode: step.setup.mode,
					object_format: step.setup.object_format,
					pinned_base_sha: step.setup.pinned_base_sha,
					pinned_closure_digest: step.setup.pinned_closure_digest,
					pinned_local_repo_uuid: step.setup.pinned_local_repo_uuid,
					target_ref: step.setup.target_ref,
				},
				task_id: step.task_id,
				tasks_parent: {
					canonical: step.tasks_parent.canonical,
					device: step.tasks_parent.device,
					inode: step.tasks_parent.inode,
					owner_uid: step.tasks_parent.owner_uid,
				},
				...(step.recover_root === undefined ? {} : {
					recover_root: {
						canonical: step.recover_root.canonical,
						device: step.recover_root.device,
						inode: step.recover_root.inode,
						owner_uid: step.recover_root.owner_uid,
					},
				}),
				workspace_root: step.workspace_root,
			});
			const result = await runMc(["__task-parent-recheck", frame]);
			if (result.exitCode !== 0) {
				throw new Error(`task parent recheck refused (exit ${result.exitCode}): ${result.stderr.trim()}`);
			}
		},
		recheckAcceptedSeal: async (seal) => recheckAcceptedSeal(config.mcHome, seal),
		recoverTaskSkeleton: async (step) => {
			const result = await runMc(["__task-skeleton-recover", JSON.stringify({
				child_mode: step.child_mode,
				recover_root: step.recover_root,
				setup: step.setup,
				task_id: step.task_id,
				tasks_parent: step.tasks_parent,
				workspace_root: step.workspace_root,
			})]);
			if (result.exitCode !== 0) {
				throw new Error(`task skeleton recovery refused (exit ${result.exitCode}): ${result.stderr.trim()}`);
			}
			const identity = JSON.parse(result.stdout) as import("./task-skeleton").PathIdentity;
			if (step.recover_root === undefined || identity.canonical !== step.recover_root.canonical ||
				identity.device !== step.recover_root.device || identity.inode !== step.recover_root.inode ||
				identity.owner_uid !== step.recover_root.owner_uid) {
				throw new Error("task skeleton recovery returned different identity evidence");
			}
			return identity;
		},
		registerTaskRoot: async (runId, taskId, identity) => {
			const result = await runMc([
				"task", "setup-register", "--run", runId, "--task", String(taskId),
				"--device", identity.device, "--inode", identity.inode,
				"--owner-uid", String(identity.owner_uid),
			]);
			if (result.exitCode !== 0) {
				throw new Error(`task setup registration refused (exit ${result.exitCode}): ${result.stderr.trim()}`);
			}
		},
    config,
  };

  const handle = startTickLoop(deps);
  deps.log(`started: tick every ${intervalMs} ms, mc=${config.mcPath}`);

  const shutdown = (signal: string) => {
    deps.log(`${signal} received; stopping after any in-flight tick`);
    void handle.stop().then(() => process.exit(0));
  };
  process.on("SIGINT", () => shutdown("SIGINT"));
  process.on("SIGTERM", () => shutdown("SIGTERM"));
}

if (import.meta.main) {
  void main();
}
