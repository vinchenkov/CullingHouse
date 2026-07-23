// runner-process.test.ts — subprocess-level tests for the agent runner's
// pinned PROCESS behaviors (docs/phase1b-contract.md §4; spec §11.5), which
// no other suite reaches (the Docker e2e only ever sees exit-0 harness runs):
//
//   - exit-code passthrough: the runner "waits for harness exit, exits with
//     it" and never converts an ordinary harness exit into success;
//   - `mc run register-session` fires exactly once, with the registered
//     (never assumed) locators;
//   - failing `mc` invocations are logged and never fatal (§10: the lease
//     fencing in the spine is the authority on staleness).
//
// main() runs as a real `bun` subprocess against a temp run.json and the
// real fake harness (via the test-tier MC_RUN_JSON / MC_HARNESS_CLI
// overrides), with a stub `mc` on PATH that records its argv. Docker-free,
// token-free: part of the fast suite.

import { afterAll, describe, expect, test } from "bun:test";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const MAIN = join(import.meta.dir, "main.ts");
const HARNESS_CLI = join(import.meta.dir, "..", "fake-harness", "cli.ts");

const roots: string[] = [];
afterAll(() => {
  for (const r of roots) rmSync(r, { recursive: true, force: true });
});

interface RunResult {
  exitCode: number;
  stderr: string;
  mcCalls: string[];
}

/** Materialize a temp envelope + behavior + stub mc, run main() for real. */
async function runRunner(opts: {
  behavior: unknown;
  mcExit?: number;
  delayRegistration?: boolean;
	  harness?: string;
	  modelBinding?: string;
	  agentRunnerRoutes?: string;
	  claudeAdapterBody?: string;
}): Promise<RunResult> {
  const root = mkdtempSync(join(tmpdir(), "mc-runner-test-"));
  roots.push(root);
  const sessionDir = join(root, "session");
  const binDir = join(root, "bin");
  mkdirSync(sessionDir);
  mkdirSync(binDir);

  const behaviorPath = join(root, "behavior.json");
  writeFileSync(behaviorPath, JSON.stringify(opts.behavior));

  const runJsonPath = join(root, "run.json");
  writeFileSync(
    runJsonPath,
    JSON.stringify({
      run_id: "proc-test-run",
      tier: "pipeline",
      role: "worker",
      subject_id: 7,
      worksource: "ws-test",
	      harness: opts.harness ?? "fake",
	      model_binding: opts.modelBinding ?? "fake",
      mode: "fresh",
      brief: "process-level runner test",
      pool_ids: [],
      heartbeat_interval_s: 3600, // only the immediate post-session-start beat
      harness_config: { behavior: behaviorPath },
      mounts: { session: sessionDir, workspace: "/workspace/source" },
    }),
  );

  // Stub mc: append argv to a log, exit as scripted (0 or nonzero).
  const mcLog = join(root, "mc-calls.log");
  const mcStub = join(binDir, "mc");
  writeFileSync(
    mcStub,
    `#!/bin/sh\n` +
      (opts.delayRegistration
        ? `if [ "$1 $2" = "run register-session" ]; then sleep 0.2; fi\n`
        : "") +
      `echo "$@" >> "${mcLog}"\n` +
      (opts.mcExit ? `echo "stub mc: scripted failure" >&2\nexit ${opts.mcExit}\n` : "exit 0\n"),
  );
  chmodSync(mcStub, 0o755);

  const claudeAdapter = join(root, "claude-adapter.ts");
  if (opts.claudeAdapterBody !== undefined) writeFileSync(claudeAdapter, opts.claudeAdapterBody);

  const proc = Bun.spawn(["bun", MAIN], {
    stdin: "ignore",
    stdout: "ignore",
    stderr: "pipe",
    env: {
      ...process.env,
      PATH: `${binDir}:${process.env["PATH"] ?? ""}`,
      MC_RUN_JSON: runJsonPath,
      MC_HARNESS_CLI: HARNESS_CLI,
	  ...(opts.claudeAdapterBody !== undefined ? { MC_CLAUDE_ADAPTER: claudeAdapter } : {}),
      ...(opts.agentRunnerRoutes !== undefined ? { MC_AGENT_RUNNER_ROUTES: opts.agentRunnerRoutes } : {}),
    },
  });
  const [exitCode, stderr] = await Promise.all([
    proc.exited,
    new Response(proc.stderr).text(),
  ]);

  let mcCalls: string[] = [];
  try {
    mcCalls = readFileSync(mcLog, "utf-8").split("\n").filter((l) => l !== "");
  } catch {
    // no mc call ever landed
  }
  return { exitCode, stderr, mcCalls };
}

// The sleep before the terminal step gives the void-fired register-session /
// heartbeat subprocesses time to land in the log before the runner exits.
const crashBehavior = {
  steps: [{ do: "sleep", seconds: 0.5 }, { do: "crash", code: 17 }],
};

describe("agent runner process behaviors (contract §4, spec §11.5)", () => {
	  test("refuses an unknown route before invoking any adapter", async () => {
	    const res = await runRunner({
	      behavior: { steps: [{ do: "succeed", output: "must not run" }] },
	      harness: "unknown",
	      modelBinding: "unknown",
	    });
	    expect(res.exitCode).toBe(2);
	    expect(res.stderr).toContain("unsupported runtime route");
	    expect(res.mcCalls).toEqual([]);
	  }, 20_000);

	  // Design B: the deployment authorizes its one fake adapter to stand in for
	  // named non-fake routes (MC_AGENT_RUNNER_ROUTES), so a production
	  // completion-seal Worker can run through it. Unset stays fake-only above.
	  test("an authorized non-fake route runs through the fake adapter", async () => {
	    const res = await runRunner({
	      behavior: { steps: [{ do: "sleep", seconds: 0.5 }, { do: "succeed", output: "ok" }] },
	      harness: "codex",
	      modelBinding: "chatgpt",
	      agentRunnerRoutes: "codex/chatgpt",
	    });
	    expect(res.exitCode).toBe(0);
	    expect(res.stderr).not.toContain("unsupported runtime route");
	    expect(res.mcCalls.some((c) => c.startsWith("run register-session"))).toBe(true);
	  }, 20_000);

	  test("a canonical route absent from the fake stand-in list uses its real adapter", async () => {
	    const res = await runRunner({
	      behavior: { steps: [{ do: "succeed", output: "unused" }] },
	      harness: "claude-sdk",
	      modelBinding: "claude",
	      agentRunnerRoutes: "codex/chatgpt",
	      claudeAdapterBody: `console.log(JSON.stringify({event:"session-start",session_id:"claude-native",native_file:"native.jsonl"}));`,
	    });
	    expect(res.exitCode).toBe(0);
	    expect(res.stderr).not.toContain("unsupported runtime route");
	    expect(res.mcCalls).toContain(
	      "run register-session proc-test-run --native-ref claude-native --file native.jsonl",
	    );
	  }, 20_000);

  test("a crashed harness's exit code passes through — never converted into success", async () => {
    const res = await runRunner({ behavior: crashBehavior });
    expect(res.exitCode).toBe(17);
  }, 20_000);

  test("register-session fires exactly once, with the registered locators", async () => {
    const res = await runRunner({ behavior: crashBehavior });
    const regs = res.mcCalls.filter((c) => c.startsWith("run register-session"));
    expect(regs).toEqual([
      "run register-session proc-test-run --native-ref fake-session --file native.jsonl",
    ]);
    // Heartbeating begins only after session-start (§10), immediately.
    expect(res.mcCalls).toContain("heartbeat proc-test-run");
  }, 20_000);

  test("awaits locator registration when the harness exits immediately", async () => {
    const res = await runRunner({
      behavior: { steps: [{ do: "succeed", output: "ok" }] },
      delayRegistration: true,
    });
    expect(res.exitCode).toBe(0);
    expect(res.mcCalls.filter((c) => c.startsWith("run register-session"))).toEqual([
      "run register-session proc-test-run --native-ref fake-session --file native.jsonl",
    ]);
  }, 20_000);

  test("failing mc calls are logged and never fatal; harness exit still passes through", async () => {
    const res = await runRunner({ behavior: crashBehavior, mcExit: 1 });
    expect(res.exitCode).toBe(17);
    expect(res.mcCalls.some((c) => c.startsWith("run register-session"))).toBe(true);
    expect(res.stderr).toContain("exited 1");
  }, 20_000);

  test("a clean harness turn exits 0", async () => {
    const res = await runRunner({
      behavior: { steps: [{ do: "sleep", seconds: 0.5 }, { do: "succeed", output: "ok" }] },
    });
    expect(res.exitCode).toBe(0);
  }, 20_000);
});
