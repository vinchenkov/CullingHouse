// Deterministic split-brain convergence fixtures (phase-2 wave-2 contract
// §4). These are deliberately cross-boundary tests: the real mc binary
// commits the record/lease decision, while only the resident's injectable
// dependencies fail. Restart always uses the ordinary tick loop. No fault or
// test clock exists in mc; fixture-only SQLite surgery ages the committed
// lease, matching the CLI integration tier's accepted watchdog setup.

import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { Database } from "bun:sqlite";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { startTickLoop } from "./tick-loop";
import { fail, makeRig, ok, testConfig } from "./test-helpers";
import type { Effect, Exec, ExecResult, TickDeps } from "./types";

type JsonObject = Record<string, unknown>;

const repoRoot = resolve(import.meta.dir, "..", "..");
const mcSourceDir = join(repoRoot, "mc", "cmd", "mc");
const suiteRoot = mkdtempSync(join(tmpdir(), "mc-split-brain-suite-"));
const mcBin = join(suiteRoot, "mc");

const fakeRouting = `# Mission Control routing

| role | harness | binding |
| --- | --- | --- |
| strategist | fake | fake |
| editor | fake | fake |
| worker | fake | fake |
| verifier | fake | fake |
| packager | fake | fake |
| refiner | fake | fake |
| homie | fake | fake |
`;

interface ProcessOptions {
  cwd?: string;
  env?: Record<string, string>;
}

async function runProcess(argv: string[], options: ProcessOptions = {}): Promise<ExecResult> {
  const proc = Bun.spawn(argv, {
    cwd: options.cwd,
    env: options.env,
    stdin: "ignore",
    stdout: "pipe",
    stderr: "pipe",
  });
  const [exitCode, stdout, stderr] = await Promise.all([
    proc.exited,
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
  ]);
  return { exitCode, stdout, stderr };
}

beforeAll(async () => {
  const built = await runProcess(
    ["go", "build", "-tags", "test_fake_routing", "-o", mcBin, "."],
    { cwd: mcSourceDir },
  );
  if (built.exitCode !== 0) {
    throw new Error(`build real mc test binary: ${built.stderr || built.stdout}`);
  }
});

afterAll(() => {
  rmSync(suiteRoot, { recursive: true, force: true });
});

function mcEnv(home: string, spine?: string): Record<string, string> {
  const env: Record<string, string> = {};
  for (const [key, value] of Object.entries(process.env)) {
    if (value === undefined) continue;
    if (["MC_HOME", "MC_SPINE", "MC_HELPER", "MC_RUN_JSON"].includes(key)) continue;
    env[key] = value;
  }
  env.MC_HOME = home;
  if (spine !== undefined) env.MC_SPINE = spine;
  return env;
}

async function runMc(
  home: string,
  spine: string | undefined,
  argv: string[],
): Promise<ExecResult> {
  return runProcess([mcBin, ...argv], { env: mcEnv(home, spine) });
}

async function mcJson(
  home: string,
  spine: string | undefined,
  argv: string[],
): Promise<JsonObject> {
  const result = await runMc(home, spine, argv);
  if (result.exitCode !== 0) {
    throw new Error(
      `mc ${argv.join(" ")} exited ${result.exitCode}: ${result.stderr || result.stdout}`,
    );
  }
  return JSON.parse(result.stdout) as JsonObject;
}

interface Fixture {
  root: string;
  home: string;
  spine: string;
  workspace: string;
  taskId: number;
  config: TickDeps["config"];
}

async function makeFixture(): Promise<Fixture> {
  const root = mkdtempSync(join(suiteRoot, "case-"));
  const home = join(root, "home");
  const spine = join(root, "spine.db");
  const workspace = join(root, "workspace");
  mkdirSync(home);
  mkdirSync(workspace);

  await mcJson(home, undefined, [
    "init",
    "--spine", spine,
    "--worksource", "ws-test",
    "--workspace-root", workspace,
    // Keep the retry comfortably live through the real subprocess reads at
    // the end of the test; fixture surgery ages only the original claim.
    "--spawn-grace-s", "300",
  ]);
  writeFileSync(join(home, "routing.md"), fakeRouting, { mode: 0o600 });
  const added = await mcJson(home, spine, [
    "task", "add", "split-brain subject", "--worksource", "ws-test",
  ]);
  const taskId = added.task_id;
  if (typeof taskId !== "number") throw new Error(`task add returned no numeric id: ${JSON.stringify(added)}`);

  return {
    root,
    home,
    spine,
    workspace,
    taskId,
    config: { ...testConfig, mcHome: home, workspaceRoot: workspace },
  };
}

interface RecordingFs {
  events: string[];
  fs: TickDeps["fs"];
}

function recordingFs(failBeforeFirstWrite = false): RecordingFs {
  const events: string[] = [];
  let shouldFail = failBeforeFirstWrite;
  return {
    events,
    fs: {
      mkdir: async (path) => {
        events.push(`mkdir:${path}`);
        await mkdir(path, { recursive: true });
      },
      writeFile: async (path, data) => {
        events.push(`write:${path}`);
        if (shouldFail) {
          shouldFail = false;
          throw new Error("simulated resident death before run.json");
        }
        await writeFile(path, data);
      },
      rm: async (path) => {
        events.push(`rm:${path}`);
        await rm(path, { force: true });
      },
    },
  };
}

function recordingMc(
  fixture: Fixture,
  effects: Effect[],
  killAfterFirstDispatch: boolean,
): Exec {
  let shouldKill = killAfterFirstDispatch;
  return async (argv) => {
    const result = await runMc(fixture.home, fixture.spine, argv);
    if (argv.length === 1 && argv[0] === "dispatch" && result.exitCode === 0) {
      effects.push(JSON.parse(result.stdout) as Effect);
      if (shouldKill) {
        shouldKill = false;
        throw new Error("simulated resident death after committed dispatch");
      }
    }
    return result;
  };
}

type FirstRunMode = "ordinary" | "die-before-start" | "die-after-start";

/**
 * Stateful Docker seam shared across resident restarts. The two injected
 * modes model which side of container creation became durable; every later
 * run/stop uses the ordinary behavior. This is test-side host state, never a
 * production fault hook.
 */
class RecordingDocker {
  calls: string[][] = [];
  live = new Set<string>();
  private firstDetachedRun = true;

  constructor(private readonly firstRunMode: FirstRunMode) {}

  exec: Exec = async (argv) => {
    this.calls.push([...argv]);
    if (argv[0] === "run" && argv.includes("-d")) {
      const nameAt = argv.indexOf("--name");
      const name = nameAt >= 0 ? argv[nameAt + 1] : undefined;
      if (name === undefined) throw new Error("test Docker received a detached run without --name");

      const isFirst = this.firstDetachedRun;
      this.firstDetachedRun = false;
      if (isFirst && this.firstRunMode === "die-before-start") {
        throw new Error("simulated resident death before container start");
      }
      this.live.add(name);
      if (isFirst && this.firstRunMode === "die-after-start") {
        throw new Error("simulated resident death after container start");
      }
      return ok(`${name}-container-id\n`);
    }

    if (argv[0] === "stop") {
      const name = argv[1];
      if (name === undefined) throw new Error("test Docker received stop without a name");
      if (!this.live.delete(name)) return fail(1, "No such container");
      return ok("");
    }
    return ok("");
  };
}

async function lock(fixture: Fixture): Promise<JsonObject> {
  return mcJson(fixture.home, fixture.spine, ["lock", "get"]);
}

async function task(fixture: Fixture): Promise<JsonObject> {
  return mcJson(fixture.home, fixture.spine, ["task", "get", String(fixture.taskId)]);
}

async function runs(fixture: Fixture): Promise<JsonObject[]> {
  const listed = await mcJson(fixture.home, fixture.spine, ["run", "list"]);
  return listed.runs as JsonObject[];
}

function ageLeasePastSpawnWatchdog(spine: string): void {
  const db = new Database(spine);
  try {
    const changed = db.run(
      "UPDATE lock SET acquired_at = datetime('now', '-600 seconds') WHERE id = 1 AND run_id IS NOT NULL",
    );
    if (changed.changes !== 1) throw new Error(`aged ${changed.changes} lease rows, want 1`);
  } finally {
    db.close();
  }
}

const boundaries = [
  {
    name: "action selected / before effect",
    killAfterDispatch: true,
    failBeforeWrite: false,
    oldFolderExists: false,
    oldEnvelopeExists: false,
    oldContainerExists: false,
    firstRunMode: "ordinary",
  },
  {
    name: "session folder / before run.json",
    killAfterDispatch: false,
    failBeforeWrite: true,
    oldFolderExists: true,
    oldEnvelopeExists: false,
    oldContainerExists: false,
    firstRunMode: "ordinary",
  },
  {
    name: "run.json / before container",
    killAfterDispatch: false,
    failBeforeWrite: false,
    oldFolderExists: true,
    oldEnvelopeExists: true,
    oldContainerExists: false,
    firstRunMode: "die-before-start",
  },
  {
    name: "container start / before first heartbeat",
    killAfterDispatch: false,
    failBeforeWrite: false,
    oldFolderExists: true,
    oldEnvelopeExists: true,
    oldContainerExists: true,
    firstRunMode: "die-after-start",
  },
] as const;

describe("split-brain convergence: committed spawn before first heartbeat", () => {
  for (const boundary of boundaries) {
    test(boundary.name, async () => {
      const fixture = await makeFixture();
      const effects: Effect[] = [];
      try {
        // Commit the decision, then lose the first resident at the selected
        // non-atomic boundary. All failures are outside mc.
        const firstFs = recordingFs(boundary.failBeforeWrite);
        const docker = new RecordingDocker(boundary.firstRunMode);
        const first = makeRig({
          runMc: recordingMc(fixture, effects, boundary.killAfterDispatch),
          docker: docker.exec,
          fs: firstFs.fs,
          config: fixture.config,
        });
        const firstLoop = startTickLoop(first.deps);
        await first.timer.fire();
        await firstLoop.stop();

        expect(effects).toHaveLength(1);
        const selected = effects[0]!;
        expect(selected).toMatchObject({
          action: "spawn",
          role: "editor",
          subject_id: fixture.taskId,
        });
        if (selected.action !== "spawn") throw new Error("first dispatch did not spawn");
        const oldRun = selected.run_id;
        const oldSession = join(fixture.home, "sessions", oldRun);
        const oldEnvelope = join(fixture.home, "runs", `${oldRun}.json`);
        const oldContainer = `mc-run-${oldRun}`;

        const initialDockerCalls = boundary.oldEnvelopeExists ? 1 : 0;
        expect(docker.calls).toHaveLength(initialDockerCalls);
        if (initialDockerCalls === 1) {
          expect(docker.calls[0]![0]).toBe("run");
          expect(docker.calls[0]).toContain(oldContainer);
          expect(docker.calls[0]).toContain(`${oldSession}:/mc/session`);
          expect(docker.calls[0]).toContain(`${oldEnvelope}:/mc/run.json:ro`);
        }
        expect(docker.live.has(oldContainer)).toBe(boundary.oldContainerExists);
        expect(first.logs.some((line) => line.includes("simulated resident death"))).toBe(true);
        expect(existsSync(oldEnvelope)).toBe(boundary.oldEnvelopeExists);
        if (boundary.oldEnvelopeExists) {
          expect(JSON.parse(readFileSync(oldEnvelope, "utf8"))).toMatchObject({
            run_id: oldRun,
            subject_id: fixture.taskId,
            role: "editor",
          });
        }
        expect(existsSync(oldSession)).toBe(boundary.oldFolderExists);
        if (boundary.oldFolderExists) expect(readdirSync(oldSession)).toEqual([]);
        if (boundary.killAfterDispatch) expect(firstFs.events).toEqual([]);

        // The left-side decision is durable even though its effect was not:
        // one live Run and the matching lease, with the task unchanged.
        expect(await lock(fixture)).toMatchObject({
          run_id: oldRun,
          worksource: "ws-test",
          subject: fixture.taskId,
          owner: "editor",
          last_heartbeat_at: null,
        });
        const beforeRuns = await runs(fixture);
        expect(beforeRuns).toHaveLength(1);
        expect(beforeRuns[0]).toMatchObject({
          id: oldRun,
          role: "editor",
          subject: fixture.taskId,
          outcome: null,
          ended_at: null,
          session_path: `sessions/${oldRun}`,
          binding: "fake/fake",
        });
        expect(await task(fixture)).toMatchObject({
          id: fixture.taskId,
          status: "proposed",
          dispatch_retries: 3,
          blocked: 0,
        });

        ageLeasePastSpawnWatchdog(fixture.spine);

        // A new resident with ordinary dependencies runs the normal loop.
        // Reap is one committed action; reselection therefore takes the next
        // tick. An absent container is expected and cannot prevent cleanup.
        const recoveredFs = recordingFs();
        const recovered = makeRig({
          runMc: recordingMc(fixture, effects, false),
          docker: docker.exec,
          fs: recoveredFs.fs,
          config: fixture.config,
        });
        const recoveredLoop = startTickLoop(recovered.deps);

        await recovered.timer.fire();
        expect(effects).toHaveLength(2);
        expect(effects[1]).toEqual({
          action: "reap",
          run_id: oldRun,
          reason: "spawn-watchdog",
          stop_container: true,
        });
        expect(docker.calls).toHaveLength(initialDockerCalls + 1);
        expect(docker.calls.at(-1)).toEqual(["stop", oldContainer]);
        expect(docker.live.has(oldContainer)).toBe(false);
        expect(recovered.logs.some((line) => line.includes("No such container"))).toBe(
          !boundary.oldContainerExists,
        );
        expect(existsSync(oldEnvelope)).toBe(false);
        expect(existsSync(oldSession)).toBe(boundary.oldFolderExists);
        if (boundary.oldFolderExists) expect(readdirSync(oldSession)).toEqual([]);

        expect(await lock(fixture)).toMatchObject({
          run_id: null,
          worksource: null,
          subject: null,
          owner: null,
          acquired_at: null,
          last_heartbeat_at: null,
          hard_deadline_at: null,
        });
        const reapedRuns = await runs(fixture);
        expect(reapedRuns).toHaveLength(1);
        expect(reapedRuns[0]).toMatchObject({ id: oldRun, outcome: "reaped" });
        expect(reapedRuns[0]!.ended_at).not.toBeNull();
        expect(await task(fixture)).toMatchObject({
          status: "proposed",
          dispatch_retries: 2,
          blocked: 0,
        });

        await recovered.timer.fire();
        expect(effects).toHaveLength(3);
        const retry = effects[2]!;
        expect(retry).toMatchObject({
          action: "spawn",
          role: "editor",
          subject_id: fixture.taskId,
        });
        if (retry.action !== "spawn") throw new Error("retry dispatch did not spawn");
        const newRun = retry.run_id;
        expect(newRun).not.toBe(oldRun);

        const newSession = join(fixture.home, "sessions", newRun);
        const newEnvelope = join(fixture.home, "runs", `${newRun}.json`);
        expect(existsSync(newSession)).toBe(true);
        expect(existsSync(newEnvelope)).toBe(true);
        expect(JSON.parse(readFileSync(newEnvelope, "utf8"))).toMatchObject({
          run_id: newRun,
          subject_id: fixture.taskId,
          role: "editor",
        });
        expect(existsSync(oldEnvelope)).toBe(false);
        expect(existsSync(oldSession)).toBe(boundary.oldFolderExists);
        if (boundary.oldFolderExists) expect(readdirSync(oldSession)).toEqual([]);

        expect(docker.calls).toHaveLength(initialDockerCalls + 2);
        const retryDocker = docker.calls.at(-1)!;
        expect(retryDocker[0]).toBe("run");
        expect(retryDocker).toContain(`mc-run-${newRun}`);
        expect(retryDocker).toContain(`${newSession}:/mc/session`);
        expect(retryDocker).toContain(`${newEnvelope}:/mc/run.json:ro`);
        expect([...docker.live]).toEqual([`mc-run-${newRun}`]);
        expect(await lock(fixture)).toMatchObject({
          run_id: newRun,
          worksource: "ws-test",
          subject: fixture.taskId,
          owner: "editor",
          last_heartbeat_at: null,
        });
        const convergedRuns = await runs(fixture);
        expect(convergedRuns).toHaveLength(2);
        expect(convergedRuns.find((row) => row.id === oldRun)).toMatchObject({
          outcome: "reaped",
        });
        expect(convergedRuns.find((row) => row.id === newRun)).toMatchObject({
          role: "editor",
          subject: fixture.taskId,
          worksource: "ws-test",
          outcome: null,
          ended_at: null,
          session_path: `sessions/${newRun}`,
          binding: "fake/fake",
        });
        expect(await task(fixture)).toMatchObject({
          status: "proposed",
          dispatch_retries: 2,
          blocked: 0,
        });

        // Another ordinary tick sees the live retry lease. It cannot open a
        // duplicate Run, re-charge the retry, or repeat any host effect.
        await recovered.timer.fire();
        expect(effects).toHaveLength(4);
        expect(effects[3]).toEqual({ action: "idle", reason: "lease-held" });
        expect(docker.calls).toHaveLength(initialDockerCalls + 2);
        expect(await runs(fixture)).toHaveLength(2);
        expect(await task(fixture)).toMatchObject({ dispatch_retries: 2 });

        await recoveredLoop.stop();
      } finally {
        rmSync(fixture.root, { recursive: true, force: true });
      }
    }, 30_000);
  }
});
