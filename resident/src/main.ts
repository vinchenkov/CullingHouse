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

  const deps: TickDeps = {
    intervalMs,
    setTimer: (fn, ms) => setInterval(fn, ms),
    clearTimer: (h) => clearInterval(h as ReturnType<typeof setInterval>),
    runMc: execMcVia([config.mcPath], {
      mcHome: config.mcHome,
      releaseBuildId: config.releaseBuildId,
      configSchemaVersion: config.configSchemaVersion,
    }),
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
