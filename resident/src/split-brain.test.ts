// Deterministic split-brain convergence fixtures (phase-2 wave-2 contract
// §4). These are deliberately cross-boundary tests: the real mc binary
// commits the record/lease decision, while failures enter only through the
// resident/runner/native-surface injection seams. Restart uses the ordinary
// tick loop or the ordinary outbox poll/send/ack client, respectively. No
// fault or test clock exists in mc; fixture-only SQLite surgery ages the
// committed lease, matching the CLI integration tier's watchdog setup.

import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { Database } from "bun:sqlite";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  realpathSync,
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
const agentRunnerMain = join(repoRoot, "runner", "agent-runner", "main.ts");
const fakeHarnessCli = join(repoRoot, "runner", "fake-harness", "cli.ts");
const mcLandScript = join(repoRoot, "runner", "image", "mc-land");
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
  stdin?: string;
}

async function runProcess(argv: string[], options: ProcessOptions = {}): Promise<ExecResult> {
  const proc = Bun.spawn(argv, {
    cwd: options.cwd,
    env: options.env,
    stdin: options.stdin === undefined ? "ignore" : Buffer.from(options.stdin),
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

async function gitAt(path: string, ...argv: string[]): Promise<string> {
  const result = await runProcess(["git", "-C", path, ...argv]);
  if (result.exitCode !== 0) {
    throw new Error(`git ${argv.join(" ")} exited ${result.exitCode}: ${result.stderr}`);
  }
  return result.stdout.trim();
}

async function git(fixture: Fixture, ...argv: string[]): Promise<string> {
  return gitAt(fixture.workspace, ...argv);
}

async function initializeGitWorkspace(fixture: Fixture): Promise<string> {
  await git(fixture, "init", "-q", "-b", "main");
  await git(fixture, "config", "user.name", "split-brain fixture");
  await git(fixture, "config", "user.email", "fixture@mc.invalid");
  await git(fixture, "config", "worktree.useRelativePaths", "true");
  writeFileSync(join(fixture.workspace, ".gitignore"), ".mc-worktrees/\n");
  writeFileSync(join(fixture.workspace, "README.md"), "split-brain fixture\n");
  await git(fixture, "add", ".gitignore", "README.md");
  await git(fixture, "commit", "-q", "-m", "fixture base");
  return git(fixture, "rev-parse", "main");
}

/** Advance only the setup Editor through its real fenced CLI terminal. */
async function seedStandaloneTask(fixture: Fixture): Promise<string> {
  const selected = await mcJson(fixture.home, fixture.spine, ["dispatch"]);
  if (
    selected.action !== "spawn" || selected.role !== "editor" ||
    selected.subject_id !== fixture.taskId || typeof selected.run_id !== "string"
  ) {
    throw new Error(`setup Editor dispatch was not the fixture task: ${JSON.stringify(selected)}`);
  }
  const runId = selected.run_id;
  const identity = join(fixture.root, `setup-editor-${runId}.json`);
  writeFileSync(identity, JSON.stringify({
    run_id: runId,
    tier: "pipeline",
    role: "editor",
    pool_ids: [fixture.taskId],
  }));
  const env = mcEnv(fixture.home, fixture.spine);
  env.MC_RUN_JSON = identity;
  const decided = await runProcess(
    [mcBin, "editor", "decide", "--run", runId, "--batch", "-"],
    {
      env,
      stdin: JSON.stringify({
        verdicts: [{
          task: fixture.taskId,
          decision: "promote",
          reason: "split-brain Worker fixture",
        }],
      }),
    },
  );
  if (decided.exitCode !== 0) {
    throw new Error(`setup Editor terminal failed: ${decided.stderr || decided.stdout}`);
  }
  return runId;
}

async function runScopedMc(
  fixture: Fixture,
  runId: string,
  role: string,
  argv: string[],
): Promise<JsonObject> {
  const identity = join(fixture.root, `setup-${role}-${runId}.json`);
  writeFileSync(identity, JSON.stringify({
    run_id: runId,
    tier: "pipeline",
    role,
    pool_ids: [],
  }));
  const env = mcEnv(fixture.home, fixture.spine);
  env.MC_RUN_JSON = identity;
  const result = await runProcess([mcBin, ...argv], { env });
  if (result.exitCode !== 0) {
    throw new Error(
      `scoped ${role} mc ${argv.join(" ")} exited ${result.exitCode}: ${result.stderr || result.stdout}`,
    );
  }
  return result.stdout.trim() === "" ? {} : JSON.parse(result.stdout) as JsonObject;
}

function requireSpawn(
  effect: JsonObject,
  role: string,
  taskId: number,
): string {
  if (
    effect.action !== "spawn" || effect.role !== role ||
    effect.subject_id !== taskId || typeof effect.run_id !== "string"
  ) {
    throw new Error(`wanted ${role} spawn for task ${taskId}, got ${JSON.stringify(effect)}`);
  }
  return effect.run_id;
}

interface PackagedGitTask {
  baseSHA: string;
  branch: string;
  worktree: string;
  verifiedSHA: string;
}

/** Drive one standalone task to a real branch-carrying Review Packet. */
async function packageStandaloneGitTask(fixture: Fixture): Promise<PackagedGitTask> {
  const baseSHA = await initializeGitWorkspace(fixture);
  await seedStandaloneTask(fixture);

  const branch = `mc/task-${fixture.taskId}`;
  const worktree = join(fixture.workspace, ".mc-worktrees", `task-${fixture.taskId}`);
  await git(fixture, "worktree", "add", worktree, "-b", branch);
  writeFileSync(join(worktree, "landing.txt"), `reviewed landing for task ${fixture.taskId}\n`);
  await gitAt(worktree, "add", "landing.txt");
  await gitAt(
    worktree,
    "-c", "user.name=mc worker",
    "-c", "user.email=worker@mc.invalid",
    "commit", "-q", "-m", `${branch}: reviewed landing`,
  );
  const verifiedSHA = await gitAt(worktree, "rev-parse", "HEAD");

  const worker = await mcJson(fixture.home, fixture.spine, ["dispatch"]);
  const workerRun = requireSpawn(worker, "worker", fixture.taskId);
  await runScopedMc(fixture, workerRun, "worker", [
    "complete", String(fixture.taskId),
    "--run", workerRun,
    "--status", "worked",
    "--branch", branch,
  ]);

  const verifier = await mcJson(fixture.home, fixture.spine, ["dispatch"]);
  const verifierRun = requireSpawn(verifier, "verifier", fixture.taskId);
  await runScopedMc(fixture, verifierRun, "verifier", [
    "verifier", "verdict", String(fixture.taskId),
    "--run", verifierRun,
    "--outcome", "pass",
    "--evidence", "evidence/landing.md",
    "--sha", verifiedSHA,
  ]);

  const packager = await mcJson(fixture.home, fixture.spine, ["dispatch"]);
  const packagerRun = requireSpawn(packager, "packager", fixture.taskId);
  await runScopedMc(fixture, packagerRun, "packager", [
    "complete", String(fixture.taskId),
    "--run", packagerRun,
    "--status", "packaged",
    "--outputs", `packets/${fixture.taskId}.html`,
  ]);

  return { baseSHA, branch, worktree, verifiedSHA };
}

function ageHeartbeatedLease(spine: string): void {
  const db = new Database(spine);
  try {
    const changed = db.run(`
      UPDATE lock
      SET acquired_at = datetime('now', '-7200 seconds'),
          last_heartbeat_at = datetime('now', '-7200 seconds')
      WHERE id = 1 AND run_id IS NOT NULL AND last_heartbeat_at IS NOT NULL
    `);
    if (changed.changes !== 1) {
      throw new Error(`aged ${changed.changes} heartbeated lease rows, want 1`);
    }
  } finally {
    db.close();
  }
}

type WorkspaceKillBoundary = "workspace-bytes" | "git-commit";

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function workerBehavior(
  fixture: Fixture,
  boundary: WorkspaceKillBoundary,
  attempt: number,
): JsonObject {
  const branch = `mc/task-${fixture.taskId}`;
  const worktree = join(fixture.workspace, ".mc-worktrees", `task-${fixture.taskId}`);
  const content = `partial workspace bytes for task ${fixture.taskId}`;
  const create = [
    "set -eu",
    `cd ${shellQuote(fixture.workspace)}`,
    `git worktree add ${shellQuote(worktree)} -b ${shellQuote(branch)}`,
    `cd ${shellQuote(worktree)}`,
    `printf '%s\\n' ${shellQuote(content)} > split-brain.txt`,
  ];
  const commit = [
    "git add split-brain.txt",
    "git -c user.name='mc worker' -c user.email='worker@mc.invalid' " +
      `commit -q -m ${shellQuote(`${branch}: split-brain work`)}`,
  ];

  if (attempt === 1) {
    const command = boundary === "git-commit"
      ? [...create, ...commit].join("; ")
      : [...create, "git add split-brain.txt"].join("; ");
    return {
      steps: [
        // The runner's immediate lifecycle heartbeat must land before this
        // post-start kill point; a second pause lets the Git boundary settle.
        { do: "sleep", seconds: 0.2 },
        { do: "exec", command },
        { do: "sleep", seconds: 0.2 },
        { do: "crash", code: 17 },
      ],
    };
  }

  const inspect = [
    "set -eu",
    `cd ${shellQuote(worktree)}`,
    `test "$(cat split-brain.txt)" = ${shellQuote(content)}`,
    `test "$MC_SUBJECT_ID" = ${shellQuote(String(fixture.taskId))}`,
  ];
  const reconcile = boundary === "workspace-bytes"
    ? [...inspect, ...commit]
    : [
        ...inspect,
        "test -z \"$(git status --porcelain)\"",
        "test \"$(git rev-list --count main..HEAD)\" -eq 1",
      ];
  reconcile.push(
    `mc complete "$MC_SUBJECT_ID" --run "$MC_RUN_ID" --status worked --branch ${shellQuote(branch)}`,
  );
  return {
    steps: [
      { do: "exec", command: reconcile.join("; ") },
      { do: "succeed", output: "reconciled existing task worktree" },
    ],
  };
}

function mountSource(argv: string[], target: string, readOnly = false): string {
  const suffix = `:${target}${readOnly ? ":ro" : ""}`;
  for (let i = 0; i < argv.length - 1; i++) {
    if (argv[i] === "-v" && argv[i + 1]!.endsWith(suffix)) {
      return argv[i + 1]!.slice(0, -suffix.length);
    }
  }
  throw new Error(`test Docker argv carries no ${target} mount: ${JSON.stringify(argv)}`);
}

interface RunnerResult extends ExecResult {
  runId: string;
  events: JsonObject[];
}

/** Translate one resident Docker spawn into the real host runner process. */
class LocalWorkerDocker {
  calls: string[][] = [];
  runnerPromises: Promise<RunnerResult>[] = [];
  private runnerKills: Array<() => void> = [];
  private workerAttempt = 0;

  constructor(
    private readonly fixture: Fixture,
    private readonly boundary: WorkspaceKillBoundary,
  ) {}

  waitForRunner(index: number): Promise<RunnerResult> {
    const promise = this.runnerPromises[index];
    if (promise === undefined) throw new Error(`runner ${index} was never launched`);
    return new Promise((resolveRunner, rejectRunner) => {
      const timeout = setTimeout(() => {
        this.runnerKills[index]?.();
        rejectRunner(new Error(`runner ${index} did not exit within 10 seconds`));
      }, 10_000);
      promise.then(
        (result) => {
          clearTimeout(timeout);
          resolveRunner(result);
        },
        (error) => {
          clearTimeout(timeout);
          rejectRunner(error);
        },
      );
    });
  }

  killAll(): void {
    for (const kill of this.runnerKills) kill();
  }

  exec: Exec = async (argv) => {
    this.calls.push([...argv]);
    if (argv[0] === "stop") return fail(1, "No such container");
    if (argv[0] !== "run" || !argv.includes("-d")) {
      throw new Error(`local Worker Docker received an unexpected command: ${JSON.stringify(argv)}`);
    }

    this.workerAttempt++;
    if (this.workerAttempt > 2) throw new Error("local Worker Docker received a third spawn");
    const hostEnvelope = mountSource(argv, "/mc/run.json", true);
    const hostSession = mountSource(argv, "/mc/session");
    const envelope = JSON.parse(readFileSync(hostEnvelope, "utf8")) as JsonObject;
    if (envelope.role !== "worker" || typeof envelope.run_id !== "string") {
      throw new Error(`local Worker Docker received a non-Worker envelope: ${JSON.stringify(envelope)}`);
    }
    const runId = envelope.run_id;
    const behaviorPath = join(this.fixture.root, `worker-${this.workerAttempt}-${runId}.json`);
    const localEnvelope = join(this.fixture.root, `local-run-${runId}.json`);
    writeFileSync(
      behaviorPath,
      JSON.stringify(workerBehavior(this.fixture, this.boundary, this.workerAttempt)),
    );
    const harnessConfig = envelope.harness_config as JsonObject;
    const mounts = envelope.mounts as JsonObject;
    harnessConfig.behavior = behaviorPath;
    mounts.session = hostSession;
    mounts.workspace = this.fixture.workspace;
    writeFileSync(localEnvelope, JSON.stringify(envelope));

    const env = mcEnv(this.fixture.home, this.fixture.spine);
    env.MC_RUN_JSON = localEnvelope;
    env.MC_HARNESS_CLI = fakeHarnessCli;
    env.PATH = `${suiteRoot}:${env.PATH ?? ""}`;
    const proc = Bun.spawn(["bun", agentRunnerMain], {
      cwd: this.fixture.workspace,
      env,
      stdin: "ignore",
      stdout: "pipe",
      stderr: "pipe",
    });
    this.runnerKills.push(() => {
      try {
        proc.kill("SIGKILL");
      } catch {
        // Already-exited subprocesses need no cleanup.
      }
    });
    const completion = Promise.all([
      proc.exited,
      new Response(proc.stdout).text(),
      new Response(proc.stderr).text(),
    ]).then(([exitCode, stdout, stderr]) => {
      const result = { exitCode, stdout, stderr };
      const events = result.stdout
        .split("\n")
        .filter((line) => line.startsWith("{"))
        .map((line) => JSON.parse(line) as JsonObject);
      return { ...result, runId, events };
    });
    this.runnerPromises.push(completion);

    // `docker run -d` reports container creation, not the eventual runner
    // exit. The test observes exit separately through this injected seam.
    return ok(`local-container-${runId}\n`);
  };
}

/** Real mc-land behind the resident's Docker-shaped injection seam. */
class LocalLandDocker {
  calls: string[][] = [];
  results: ExecResult[] = [];
  private readonly wrapperBin?: string;

  constructor(
    private readonly fixture: Fixture,
    failCleanupOnce = false,
  ) {
    if (!failCleanupOnce) return;
    const realGit = Bun.which("git");
    if (realGit === null) throw new Error("git not found for landing fixture");
    const bin = join(fixture.root, "land-bin");
    const marker = join(fixture.root, "cleanup-failed-once");
    mkdirSync(bin);
    const wrapper = join(bin, "git");
    writeFileSync(
      wrapper,
      "#!/bin/sh\n" +
        "previous=\n" +
        "for arg do\n" +
        `  if [ \"$previous $arg\" = \"worktree remove\" ] && [ ! -e ${shellQuote(marker)} ]; then\n` +
        `    : > ${shellQuote(marker)}\n` +
        "    echo 'forced one-shot cleanup failure' >&2\n" +
        "    exit 77\n" +
        "  fi\n" +
        "  previous=$arg\n" +
        "done\n" +
        `exec ${shellQuote(realGit)} \"$@\"\n`,
    );
    chmodSync(wrapper, 0o755);
    this.wrapperBin = bin;
  }

  exec: Exec = async (argv) => {
    this.calls.push([...argv]);
    if (argv[0] !== "run" || argv.includes("-d")) {
      throw new Error(`local land Docker received an unexpected command: ${JSON.stringify(argv)}`);
    }
    if (!argv.includes("--rm") || !argv.includes("--network") || !argv.includes("none")) {
      throw new Error(`local land Docker did not receive the confined one-shot shape: ${JSON.stringify(argv)}`);
    }
    if (mountSource(argv, "/workspace/source") !== this.fixture.workspace) {
      throw new Error(`local land Docker received the wrong Worksource mount: ${JSON.stringify(argv)}`);
    }
    const imageAt = argv.indexOf(this.fixture.config.image);
    const command = imageAt < 0 ? [] : argv.slice(imageAt + 1);
    if (command.length !== 4 || command[0] !== "mc-land") {
      throw new Error(`local land Docker received the wrong command: ${JSON.stringify(argv)}`);
    }
    const env = mcEnv(this.fixture.home, this.fixture.spine);
    env.MC_LAND_WORKSPACE = this.fixture.workspace;
    if (this.wrapperBin !== undefined) {
      env.PATH = `${this.wrapperBin}:${env.PATH ?? ""}`;
    }
    const result = await runProcess(["sh", mcLandScript, ...command.slice(1)], {
      cwd: this.fixture.workspace,
      env,
    });
    this.results.push(result);
    return result;
  };
}

/** Commit real dispatch/report calls while injecting only resident death. */
class LandingMc {
  calls: string[][] = [];
  private killAfterLandDispatch: boolean;
  private dieBeforeLandReport: boolean;

  constructor(
    private readonly fixture: Fixture,
    readonly effects: Effect[],
    options: { killAfterLandDispatch?: boolean; dieBeforeLandReport?: boolean } = {},
  ) {
    this.killAfterLandDispatch = options.killAfterLandDispatch ?? false;
    this.dieBeforeLandReport = options.dieBeforeLandReport ?? false;
  }

  exec: Exec = async (argv) => {
    this.calls.push([...argv]);
    if (argv[0] === "land" && argv[1] === "report" && this.dieBeforeLandReport) {
      this.dieBeforeLandReport = false;
      throw new Error("simulated resident death before mc land report");
    }
    const result = await runMc(this.fixture.home, this.fixture.spine, argv);
    if (argv.length === 1 && argv[0] === "dispatch" && result.exitCode === 0) {
      const effect = JSON.parse(result.stdout) as Effect;
      this.effects.push(effect);
      if (effect.action === "land" && this.killAfterLandDispatch) {
        this.killAfterLandDispatch = false;
        throw new Error("simulated resident death after land selection, before effect");
      }
    }
    return result;
  };
}

interface SurfaceOutboxRow extends JsonObject {
  id: number;
  kind: string;
  session_id: string | null;
  surface: string;
  channel_ref: string | null;
  payload: JsonObject;
  created_at: string;
}

function requireSurfaceRows(result: ExecResult, surface: string): SurfaceOutboxRow[] {
  if (result.exitCode !== 0) {
    throw new Error(`outbox poll ${surface} exited ${result.exitCode}: ${result.stderr}`);
  }
  const parsed = JSON.parse(result.stdout) as JsonObject;
  if (parsed.surface !== surface || !Array.isArray(parsed.rows)) {
    throw new Error(`malformed ${surface} outbox poll: ${result.stdout}`);
  }
  return parsed.rows.map((value) => {
    if (
      value === null || typeof value !== "object" || Array.isArray(value) ||
      typeof (value as JsonObject).id !== "number" ||
      (value as JsonObject).surface !== surface
    ) {
      throw new Error(`malformed ${surface} outbox row: ${JSON.stringify(value)}`);
    }
    return value as SurfaceOutboxRow;
  });
}

/** One native-surface pass: read durable cursor, push, then ack confirmation. */
async function drainSurfaceOnce(
  surface: string,
  runSurfaceMc: Exec,
  deliver: (row: SurfaceOutboxRow) => Promise<void>,
): Promise<SurfaceOutboxRow[]> {
  const rows = requireSurfaceRows(
    await runSurfaceMc(["outbox", "poll", "--surface", surface]),
    surface,
  );
  for (const row of rows) {
    await deliver(row);
    const ack = await runSurfaceMc([
      "outbox", "ack", String(row.id), "--surface", surface,
    ]);
    if (ack.exitCode !== 0) {
      throw new Error(`outbox ack ${row.id} exited ${ack.exitCode}: ${ack.stderr}`);
    }
  }
  return rows;
}

/** External API model: attempts are at-least-once; durable outbox id dedups. */
class DeduplicatingSurface {
  attempts: number[] = [];
  logicalPosts = new Map<number, SurfaceOutboxRow>();

  deliver = async (row: SurfaceOutboxRow): Promise<void> => {
    this.attempts.push(row.id);
    if (!this.logicalPosts.has(row.id)) this.logicalPosts.set(row.id, row);
  };
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

const workspaceBoundaries = [
  {
    name: "workspace bytes / before git commit",
    boundary: "workspace-bytes",
    checkpointCommits: 0,
    checkpointStatus: "A  split-brain.txt",
  },
  {
    name: "git commit / before complete",
    boundary: "git-commit",
    checkpointCommits: 1,
    checkpointStatus: "",
  },
] as const;

describe("split-brain convergence: standalone scripted Worker Git boundary", () => {
  for (const boundary of workspaceBoundaries) {
    test(boundary.name, async () => {
      const fixture = await makeFixture();
      const effects: Effect[] = [];
      let docker: LocalWorkerDocker | undefined;
      try {
        const baseSHA = await initializeGitWorkspace(fixture);
        await seedStandaloneTask(fixture);
        expect(await task(fixture)).toMatchObject({
          status: "seeded",
          branch: null,
          dispatch_retries: 3,
          correction_count: 0,
        });
        expect(await lock(fixture)).toMatchObject({ run_id: null, owner: null });

        const branch = `mc/task-${fixture.taskId}`;
        const worktree = join(fixture.workspace, ".mc-worktrees", `task-${fixture.taskId}`);
        const localDocker = new LocalWorkerDocker(fixture, boundary.boundary);
        docker = localDocker;
        const firstFs = recordingFs();
        const first = makeRig({
          runMc: recordingMc(fixture, effects, false),
          docker: localDocker.exec,
          fs: firstFs.fs,
          config: fixture.config,
        });
        const firstLoop = startTickLoop(first.deps);
        await first.timer.fire();

        expect(effects).toHaveLength(1);
        const selected = effects[0]!;
        expect(selected).toMatchObject({
          action: "spawn",
          role: "worker",
          subject_id: fixture.taskId,
          worksource: "ws-test",
        });
        if (selected.action !== "spawn") throw new Error("Worker checkpoint did not spawn");
        const oldRun = selected.run_id;
        const oldSession = join(fixture.home, "sessions", oldRun);
        const oldEnvelope = join(fixture.home, "runs", `${oldRun}.json`);
        expect(localDocker.calls).toHaveLength(1);
        expect(localDocker.calls[0]).toContain(`mc-run-${oldRun}`);
        expect(localDocker.calls[0]).toContain(`${oldSession}:/mc/session`);
        expect(localDocker.calls[0]).toContain(`${oldEnvelope}:/mc/run.json:ro`);
        expect(localDocker.calls[0]).toContain(`${fixture.workspace}:/workspace/source`);

        const firstRunner = await localDocker.waitForRunner(0);
        await firstLoop.stop();
        expect(firstRunner.runId).toBe(oldRun);
        expect(firstRunner.exitCode).toBe(17);
        const firstTools = firstRunner.events.filter((event) => event.event === "tool-use");
        expect(firstTools).toHaveLength(1);
        expect(firstTools[0]).toMatchObject({ exit_code: 0 });
        expect(firstRunner.events.some((event) => event.event === "turn-complete")).toBe(false);

        // The resident-authored envelope remains canonical; path translation
        // happened only in the injected local-Docker copy.
        expect(JSON.parse(readFileSync(oldEnvelope, "utf8"))).toMatchObject({
          run_id: oldRun,
          role: "worker",
          harness_config: { behavior: "/mc/behaviors/worker.json" },
          mounts: { session: "/mc/session", workspace: "/workspace/source" },
        });
        const oldTrace = join(oldSession, "native.jsonl");
        expect(existsSync(oldTrace)).toBe(true);
        expect(readFileSync(oldTrace, "utf8")).toContain('"event":"session-start"');
        expect(readFileSync(oldTrace, "utf8")).not.toContain('"event":"turn-complete"');

        const held = await lock(fixture);
        expect(held).toMatchObject({
          run_id: oldRun,
          worksource: "ws-test",
          subject: fixture.taskId,
          owner: "worker",
        });
        expect(typeof held.last_heartbeat_at).toBe("string");
        expect(await task(fixture)).toMatchObject({
          status: "seeded",
          branch: null,
          dispatch_retries: 3,
          correction_count: 0,
        });
        const checkpointRuns = await runs(fixture);
        const oldRow = checkpointRuns.find((row) => row.id === oldRun);
        expect(oldRow).toMatchObject({
          role: "worker",
          subject: fixture.taskId,
          worksource: "ws-test",
          outcome: null,
          ended_at: null,
          native_session_ref: "fake-session",
          trace_filename: "native.jsonl",
        });

        // Git is the second half of the checkpoint: main and the primary
        // checkout are untouched; all partial state is isolated in the one
        // canonical standalone-task worktree.
        expect(existsSync(worktree)).toBe(true);
        expect(await gitAt(worktree, "branch", "--show-current")).toBe(branch);
        expect(await gitAt(worktree, "rev-parse", "--show-toplevel")).toBe(realpathSync(worktree));
        expect(
          (await git(fixture, "worktree", "list", "--porcelain"))
            .split("\n")
            .filter((line) => line.startsWith("worktree ")),
        ).toHaveLength(2);
        expect(
          (await git(fixture, "for-each-ref", "--format=%(refname:short)", "refs/heads/mc/task-*"))
            .split("\n")
            .filter((line) => line !== ""),
        ).toEqual([branch]);
        expect(await git(fixture, "rev-parse", "main")).toBe(baseSHA);
        expect(await git(fixture, "status", "--porcelain")).toBe("");
        expect(await gitAt(worktree, "status", "--porcelain")).toBe(boundary.checkpointStatus);
        expect(readFileSync(join(worktree, "split-brain.txt"), "utf8")).toBe(
          `partial workspace bytes for task ${fixture.taskId}\n`,
        );
        expect(Number(await gitAt(worktree, "rev-list", "--count", `${baseSHA}..HEAD`))).toBe(
          boundary.checkpointCommits,
        );
        const checkpointSHA = await gitAt(worktree, "rev-parse", "HEAD");
        expect(checkpointSHA === baseSHA).toBe(boundary.checkpointCommits === 0);

        ageHeartbeatedLease(fixture.spine);

        const recoveredFs = recordingFs();
        const recovered = makeRig({
          runMc: recordingMc(fixture, effects, false),
          docker: localDocker.exec,
          fs: recoveredFs.fs,
          config: fixture.config,
        });
        const recoveredLoop = startTickLoop(recovered.deps);
        await recovered.timer.fire();
        expect(effects).toHaveLength(2);
        expect(effects[1]).toEqual({
          action: "reap",
          run_id: oldRun,
          reason: "lease-timeout",
          stop_container: true,
        });
        expect(localDocker.calls.at(-1)).toEqual(["stop", `mc-run-${oldRun}`]);
        expect(existsSync(oldEnvelope)).toBe(false);
        expect(existsSync(oldSession)).toBe(true);
        expect(await lock(fixture)).toMatchObject({
          run_id: null,
          worksource: null,
          subject: null,
          owner: null,
          acquired_at: null,
          last_heartbeat_at: null,
          hard_deadline_at: null,
        });
        expect(await task(fixture)).toMatchObject({
          status: "seeded",
          branch: null,
          dispatch_retries: 2,
          correction_count: 0,
        });
        const afterReapRuns = await runs(fixture);
        const reapedRow = afterReapRuns.find((row) => row.id === oldRun)!;
        expect(reapedRow).toMatchObject({
          outcome: "reaped",
        });
        expect(reapedRow.ended_at).not.toBeNull();

        // Reap cannot touch Git. The exact dirty/committed checkpoint must
        // remain available for a fresh Worker to inspect.
        expect(await git(fixture, "rev-parse", "main")).toBe(baseSHA);
        expect(await git(fixture, "status", "--porcelain")).toBe("");
        expect(await gitAt(worktree, "rev-parse", "HEAD")).toBe(checkpointSHA);
        expect(await gitAt(worktree, "status", "--porcelain")).toBe(boundary.checkpointStatus);
        expect(Number(await gitAt(worktree, "rev-list", "--count", `${baseSHA}..HEAD`))).toBe(
          boundary.checkpointCommits,
        );

        await recovered.timer.fire();
        expect(effects).toHaveLength(3);
        const retry = effects[2]!;
        expect(retry).toMatchObject({
          action: "spawn",
          role: "worker",
          subject_id: fixture.taskId,
          worksource: "ws-test",
        });
        if (retry.action !== "spawn") throw new Error("Worker recovery did not respawn");
        const newRun = retry.run_id;
        expect(newRun).not.toBe(oldRun);
        const newSession = join(fixture.home, "sessions", newRun);
        const newEnvelope = join(fixture.home, "runs", `${newRun}.json`);
        const retryDocker = localDocker.calls.at(-1)!;
        expect(retryDocker).toContain(`mc-run-${newRun}`);
        expect(retryDocker).toContain(`${newSession}:/mc/session`);
        expect(retryDocker).toContain(`${newEnvelope}:/mc/run.json:ro`);
        expect(retryDocker).toContain(`${fixture.workspace}:/workspace/source`);

        const retryRunner = await localDocker.waitForRunner(1);
        await recoveredLoop.stop();
        expect(retryRunner.runId).toBe(newRun);
        expect(retryRunner.exitCode).toBe(0);
        const retryTools = retryRunner.events.filter((event) => event.event === "tool-use");
        expect(retryTools).toHaveLength(1);
        expect(retryTools[0]).toMatchObject({ exit_code: 0 });
        expect(retryRunner.events.some((event) => event.event === "turn-complete")).toBe(true);

        expect(await lock(fixture)).toMatchObject({
          run_id: null,
          worksource: null,
          subject: null,
          owner: null,
          acquired_at: null,
          last_heartbeat_at: null,
          hard_deadline_at: null,
        });
        expect(await task(fixture)).toMatchObject({
          status: "worked",
          branch,
          dispatch_retries: 2,
          correction_count: 0,
        });
        const convergedRuns = await runs(fixture);
        const workerRows = convergedRuns.filter((row) => row.role === "worker");
        expect(workerRows).toHaveLength(2);
        const finalOldRow = workerRows.find((row) => row.id === oldRun)!;
        const finalNewRow = workerRows.find((row) => row.id === newRun)!;
        expect(finalOldRow).toMatchObject({ outcome: "reaped" });
        expect(finalOldRow.ended_at).not.toBeNull();
        expect(finalNewRow).toMatchObject({
          subject: fixture.taskId,
          worksource: "ws-test",
          outcome: "completed",
          session_path: `sessions/${newRun}`,
          binding: "fake/fake",
          native_session_ref: "fake-session",
          trace_filename: "native.jsonl",
        });
        expect(finalNewRow.ended_at).not.toBeNull();
        expect(readFileSync(join(newSession, "native.jsonl"), "utf8")).toContain(
          '"event":"turn-complete"',
        );

        const finalSHA = await gitAt(worktree, "rev-parse", "HEAD");
        expect(await git(fixture, "rev-parse", "main")).toBe(baseSHA);
        expect(await git(fixture, "status", "--porcelain")).toBe("");
        expect(await gitAt(worktree, "branch", "--show-current")).toBe(branch);
        expect(await gitAt(worktree, "status", "--porcelain")).toBe("");
        expect(
          (await git(fixture, "worktree", "list", "--porcelain"))
            .split("\n")
            .filter((line) => line.startsWith("worktree ")),
        ).toHaveLength(2);
        expect(
          (await git(fixture, "for-each-ref", "--format=%(refname:short)", "refs/heads/mc/task-*"))
            .split("\n")
            .filter((line) => line !== ""),
        ).toEqual([branch]);
        expect(Number(await gitAt(worktree, "rev-list", "--count", `${baseSHA}..HEAD`))).toBe(1);
        expect(finalSHA).not.toBe(baseSHA);
        if (boundary.boundary === "git-commit") expect(finalSHA).toBe(checkpointSHA);
        expect(readFileSync(join(worktree, "split-brain.txt"), "utf8")).toBe(
          `partial workspace bytes for task ${fixture.taskId}\n`,
        );
      } finally {
        docker?.killAll();
        rmSync(fixture.root, { recursive: true, force: true });
      }
    }, 30_000);
  }
});

const landingBoundaries = [
  {
    name: "operator approve / before land",
    killAfterLandDispatch: true,
    dieBeforeLandReport: false,
    failCleanupOnce: false,
    mergedAtCheckpoint: false,
    residueAtCheckpoint: true,
  },
  {
    name: "merge success / cleanup gap",
    killAfterLandDispatch: false,
    dieBeforeLandReport: true,
    failCleanupOnce: true,
    mergedAtCheckpoint: true,
    residueAtCheckpoint: true,
  },
  {
    name: "merge success / report gap",
    killAfterLandDispatch: false,
    dieBeforeLandReport: true,
    failCleanupOnce: false,
    mergedAtCheckpoint: true,
    residueAtCheckpoint: false,
  },
] as const;

describe("split-brain convergence: approval and landing boundary", () => {
  for (const boundary of landingBoundaries) {
    test(boundary.name, async () => {
      const fixture = await makeFixture();
      try {
        const gitTask = await packageStandaloneGitTask(fixture);
        const beforeApproval = await task(fixture);
        expect(beforeApproval).toMatchObject({
          status: "packaged",
          branch: gitTask.branch,
          verified_sha: gitTask.verifiedSHA,
          target_ref: "main",
          decision: null,
          decided_at: null,
          archived: 0,
          blocked: 0,
        });
        expect(await lock(fixture)).toMatchObject({
          run_id: null,
          worksource: null,
          subject: null,
          owner: null,
          acquired_at: null,
          last_heartbeat_at: null,
          hard_deadline_at: null,
        });
        expect(await git(fixture, "rev-parse", "main")).toBe(gitTask.baseSHA);
        expect(await gitAt(gitTask.worktree, "rev-parse", "HEAD")).toBe(gitTask.verifiedSHA);
        const beforeLandingRuns = await runs(fixture);

        // The operator commits only the durable approval. It neither moves
        // Git nor creates an effect, Run, or lease; landing is derived on a
        // later resident tick.
        const approved = await mcJson(fixture.home, fixture.spine, [
          "packet", "decide", String(fixture.taskId), "--approve",
        ]);
        expect(approved).toEqual({
          archived: false,
          decision: "approved",
          task_id: fixture.taskId,
        });
        expect(approved.action).toBeUndefined();
        const approvedTask = await task(fixture);
        expect(approvedTask).toMatchObject({
          status: "packaged",
          branch: gitTask.branch,
          verified_sha: gitTask.verifiedSHA,
          target_ref: "main",
          decision: "approved",
          archived: 0,
          blocked: 0,
        });
        expect(typeof approvedTask.decided_at).toBe("string");
        const decidedAt = approvedTask.decided_at;
        expect(await runs(fixture)).toEqual(beforeLandingRuns);
        expect(await git(fixture, "rev-parse", "main")).toBe(gitTask.baseSHA);

        const effects: Effect[] = [];
        const localDocker = new LocalLandDocker(fixture, boundary.failCleanupOnce);
        const firstMc = new LandingMc(fixture, effects, {
          killAfterLandDispatch: boundary.killAfterLandDispatch,
          dieBeforeLandReport: boundary.dieBeforeLandReport,
        });
        const first = makeRig({
          runMc: firstMc.exec,
          docker: localDocker.exec,
          fs: recordingFs().fs,
          config: fixture.config,
        });
        const firstLoop = startTickLoop(first.deps);
        await first.timer.fire();
        await firstLoop.stop();

        const expectedEffect = {
          action: "land",
          task_id: fixture.taskId,
          branch: gitTask.branch,
          verified_sha: gitTask.verifiedSHA,
          target_ref: "main",
        } as const;
        expect(effects).toEqual([expectedEffect]);
        expect(first.logs.some((line) => line.includes("simulated resident death"))).toBe(true);
        expect(localDocker.calls).toHaveLength(boundary.mergedAtCheckpoint ? 1 : 0);

        // Until the separate report commits, canonical state remains the
        // same derived landing-pending tuple and landing never owns a lease.
        expect(await task(fixture)).toMatchObject({
          status: "packaged",
          branch: gitTask.branch,
          verified_sha: gitTask.verifiedSHA,
          target_ref: "main",
          decision: "approved",
          decided_at: decidedAt,
          archived: 0,
          blocked: 0,
        });
        const checkpointPackets = await mcJson(fixture.home, fixture.spine, ["packet", "list"]);
        expect(checkpointPackets.packets).toEqual([
          expect.objectContaining({ task_id: fixture.taskId, archived: 0 }),
        ]);
        expect(await lock(fixture)).toMatchObject({
          run_id: null,
          worksource: null,
          subject: null,
          owner: null,
          acquired_at: null,
          last_heartbeat_at: null,
          hard_deadline_at: null,
        });
        expect(await runs(fixture)).toEqual(beforeLandingRuns);

        let checkpointMain = gitTask.baseSHA;
        if (boundary.mergedAtCheckpoint) {
          expect(localDocker.results[0]!.exitCode).toBe(0);
          checkpointMain = await git(fixture, "rev-parse", "main");
          expect(checkpointMain).not.toBe(gitTask.baseSHA);
          expect(await git(fixture, "rev-parse", "main^1")).toBe(gitTask.baseSHA);
          expect(await git(fixture, "rev-parse", "main^2")).toBe(gitTask.verifiedSHA);
          expect(await git(fixture, "rev-list", "--count", "--merges", `${gitTask.baseSHA}..main`))
            .toBe("1");
          expect(existsSync(gitTask.worktree)).toBe(boundary.residueAtCheckpoint);
          const branchRef = await runProcess([
            "git", "-C", fixture.workspace,
            "show-ref", "--verify", `refs/heads/${gitTask.branch}`,
          ]);
          expect(branchRef.exitCode === 0).toBe(boundary.residueAtCheckpoint);
          if (boundary.failCleanupOnce) {
            expect(localDocker.results[0]!.stderr).toContain("cleanup debt");
            expect(first.logs.some((line) => line.includes("cleanup debt"))).toBe(true);
          }
        } else {
          expect(await git(fixture, "rev-parse", "main")).toBe(gitTask.baseSHA);
          expect(existsSync(gitTask.worktree)).toBe(true);
        }

        // A fresh ordinary resident recomputes the identical effect. The
        // report-gap arms recognize the exact first landing receipt and skip
        // a second merge; the cleanup-gap arm also removes only exact residue.
        const recoveredMc = new LandingMc(fixture, effects);
        const recovered = makeRig({
          runMc: recoveredMc.exec,
          docker: localDocker.exec,
          fs: recordingFs().fs,
          config: fixture.config,
        });
        const recoveredLoop = startTickLoop(recovered.deps);
        await recovered.timer.fire();
        await recoveredLoop.stop();

        expect(effects).toEqual([expectedEffect, expectedEffect]);
        expect(localDocker.calls).toHaveLength(boundary.mergedAtCheckpoint ? 2 : 1);
        expect(localDocker.results.every((result) => result.exitCode === 0)).toBe(true);
        const finalMain = await git(fixture, "rev-parse", "main");
        if (boundary.mergedAtCheckpoint) expect(finalMain).toBe(checkpointMain);
        expect(finalMain).not.toBe(gitTask.baseSHA);
        expect(await git(fixture, "rev-parse", "main^1")).toBe(gitTask.baseSHA);
        expect(await git(fixture, "rev-parse", "main^2")).toBe(gitTask.verifiedSHA);
        expect(await git(fixture, "rev-list", "--count", "--merges", `${gitTask.baseSHA}..main`))
          .toBe("1");
        if (boundary.mergedAtCheckpoint) {
          expect(recovered.logs.some((line) => line.includes("cleanup/report debt"))).toBe(true);
        }

        expect(existsSync(gitTask.worktree)).toBe(false);
        expect(
          (await git(fixture, "for-each-ref", "--format=%(refname:short)", "refs/heads/mc/task-*"))
            .split("\n")
            .filter((line) => line !== ""),
        ).toEqual([]);
        expect(
          (await git(fixture, "worktree", "list", "--porcelain"))
            .split("\n")
            .filter((line) => line.startsWith("worktree ")),
        ).toHaveLength(1);
        expect(await git(fixture, "status", "--porcelain")).toBe("");

        const landedTask = await task(fixture);
        expect(landedTask).toMatchObject({
          status: "packaged",
          branch: gitTask.branch,
          verified_sha: gitTask.verifiedSHA,
          target_ref: "main",
          decision: "approved",
          decided_at: decidedAt,
          archived: 1,
          blocked: 0,
          blocked_reason: null,
        });
        const landedPackets = await mcJson(fixture.home, fixture.spine, ["packet", "list"]);
        expect(landedPackets.packets).toEqual([
          expect.objectContaining({ task_id: fixture.taskId, archived: 1 }),
        ]);
        expect(await lock(fixture)).toMatchObject({
          run_id: null,
          worksource: null,
          subject: null,
          owner: null,
          acquired_at: null,
          last_heartbeat_at: null,
          hard_deadline_at: null,
        });
        expect(await runs(fixture)).toEqual(beforeLandingRuns);

        const after = await mcJson(fixture.home, fixture.spine, ["dispatch"]);
        expect(after.action).not.toBe("land");
      } finally {
        rmSync(fixture.root, { recursive: true, force: true });
      }
    }, 30_000);
  }
});

describe("split-brain convergence: message and outbox delivery boundary", () => {
  test("message/outbox insert / delivery", async () => {
    const fixture = await makeFixture();
    try {
      const started = await mcJson(fixture.home, fixture.spine, [
        "homie", "start", "--from", "dashboard:console",
      ]);
      const session = started.session_id;
      if (typeof session !== "string") {
        throw new Error(`homie start returned no session: ${JSON.stringify(started)}`);
      }
      await mcJson(fixture.home, fixture.spine, [
        "homie", "bind", session, "--from", "discord:ops",
      ]);
      const baselineLock = await lock(fixture);
      const baselineTask = await task(fixture);
      const baselineRuns = await runs(fixture);

      // One real CLI transaction appends the source conversation row and
      // resolves its one non-origin destination into the outbox.
      const sent = await mcJson(fixture.home, fixture.spine, [
        "homie", "send", session,
        "--from", "dashboard:console",
        "--body", "deliver this durable turn",
      ]);
      expect(sent).toMatchObject({ session_id: session, seq: 1, echoes: 1 });
      const messageID = sent.message_id;
      expect(typeof messageID).toBe("number");

      const realSurfaceMc: Exec = (argv) => runMc(fixture.home, fixture.spine, argv);
      const initialRows = requireSurfaceRows(
        await realSurfaceMc(["outbox", "poll", "--surface", "discord"]),
        "discord",
      );
      expect(initialRows).toHaveLength(1);
      const row = initialRows[0]!;
      expect(row).toMatchObject({
        kind: "homie_echo",
        session_id: session,
        surface: "discord",
        channel_ref: "ops",
      });
      expect(row.payload).toMatchObject({
        message_id: messageID,
        seq: 1,
        body: "deliver this durable turn",
      });

      const committed = new Database(fixture.spine);
      try {
        const message = committed.query(`
          SELECT id, session_id, seq, direction, surface, channel_ref, body
          FROM conversation_messages WHERE session_id = ?`).get(session) as JsonObject;
        expect(message).toEqual({
          id: messageID,
          session_id: session,
          seq: 1,
          direction: "inbound",
          surface: "dashboard",
          channel_ref: "console",
          body: "deliver this durable turn",
        });
        const outbox = committed.query(`
          SELECT id, session_id, surface, channel_ref, delivered_at
          FROM outbox WHERE session_id = ?`).get(session) as JsonObject;
        expect(outbox).toEqual({
          id: row.id,
          session_id: session,
          surface: "discord",
          channel_ref: "ops",
          delivered_at: null,
        });
      } finally {
        committed.close();
      }

      const external = new DeduplicatingSurface();
      let diedBeforeAck = false;
      const firstSurfaceMc: Exec = async (argv) => {
        if (argv[0] === "outbox" && argv[1] === "ack" && !diedBeforeAck) {
          diedBeforeAck = true;
          throw new Error("simulated native-surface death after delivery, before ack");
        }
        return realSurfaceMc(argv);
      };
      await expect(
        drainSurfaceOnce("discord", firstSurfaceMc, external.deliver),
      ).rejects.toThrow("after delivery, before ack");

      // Physical delivery succeeded, but no canonical cursor advanced. A
      // restart sees the byte-identical durable row with the same dedup id.
      expect(external.attempts).toEqual([row.id]);
      expect([...external.logicalPosts.keys()]).toEqual([row.id]);
      const checkpointRows = requireSurfaceRows(
        await realSurfaceMc(["outbox", "poll", "--surface", "discord"]),
        "discord",
      );
      expect(checkpointRows).toEqual(initialRows);
      const checkpoint = new Database(fixture.spine);
      try {
        const delivery = checkpoint.query(
          "SELECT delivered_at FROM outbox WHERE id = ?",
        ).get(row.id) as JsonObject;
        expect(delivery.delivered_at).toBeNull();
        expect((checkpoint.query(
          "SELECT COUNT(*) AS n FROM conversation_messages WHERE session_id = ?",
        ).get(session) as JsonObject).n).toBe(1);
      } finally {
        checkpoint.close();
      }
      expect(await lock(fixture)).toEqual(baselineLock);
      expect(await task(fixture)).toEqual(baselineTask);
      expect(await runs(fixture)).toEqual(baselineRuns);

      // The restarted surface re-polls and retries the same id. Its external
      // API suppresses a second logical post. This time ack commits, but its
      // response is lost as the native process dies again.
      let lostAckResponse = false;
      const secondSurfaceMc: Exec = async (argv) => {
        const result = await realSurfaceMc(argv);
        if (
          argv[0] === "outbox" && argv[1] === "ack" &&
          result.exitCode === 0 && !lostAckResponse
        ) {
          lostAckResponse = true;
          throw new Error("simulated native-surface death after ack commit");
        }
        return result;
      };
      await expect(
        drainSurfaceOnce("discord", secondSurfaceMc, external.deliver),
      ).rejects.toThrow("after ack commit");
      expect(external.attempts).toEqual([row.id, row.id]);
      expect([...external.logicalPosts.keys()]).toEqual([row.id]);

      const afterAck = new Database(fixture.spine);
      let deliveredAt: string;
      try {
        const state = afterAck.query(`
          SELECT delivered_at,
                 (SELECT COUNT(*) FROM conversation_messages WHERE session_id = ?) AS messages,
                 (SELECT COUNT(*) FROM outbox WHERE session_id = ?) AS outbox_rows
          FROM outbox WHERE id = ?`).get(session, session, row.id) as JsonObject;
        expect(typeof state.delivered_at).toBe("string");
        deliveredAt = state.delivered_at as string;
        expect(state.messages).toBe(1);
        expect(state.outbox_rows).toBe(1);
      } finally {
        afterAck.close();
      }

      // A second restart sees no pending delivery. Direct ack replay is an
      // inert success with the first timestamp, and neither bookkeeping path
      // deletes the source message or outbox history.
      expect(await drainSurfaceOnce("discord", realSurfaceMc, external.deliver)).toEqual([]);
      const ackReplay = await mcJson(fixture.home, fixture.spine, [
        "outbox", "ack", String(row.id), "--surface", "discord",
      ]);
      expect(ackReplay).toEqual({
        id: row.id,
        surface: "discord",
        acked: false,
        delivered_at: deliveredAt,
      });
      expect(external.attempts).toEqual([row.id, row.id]);
      const history = await mcJson(fixture.home, fixture.spine, [
        "homie", "history", session,
      ]);
      expect(history.messages).toEqual([
        expect.objectContaining({
          id: messageID,
          seq: 1,
          direction: "inbound",
          body: "deliver this durable turn",
        }),
      ]);
      expect(await lock(fixture)).toEqual(baselineLock);
      expect(await task(fixture)).toEqual(baselineTask);
      expect(await runs(fixture)).toEqual(baselineRuns);
    } finally {
      rmSync(fixture.root, { recursive: true, force: true });
    }
  }, 30_000);
});
