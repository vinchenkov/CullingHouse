// test-helpers.ts — the fake timer, fake mc/docker/fs, and a deps factory.
// Tests using this complete fake rig touch no real filesystem or Docker. The
// split-brain acceptance suite overrides the injectable mc/fs seams with the
// real CLI and temp host state, but keeps this timer and fake Docker.

import type { ExecResult, ResidentConfig, TickDeps } from "./types";

/** setInterval-shaped fake: the test fires ticks by hand. */
export class FakeTimer {
  fn: (() => unknown) | null = null;
  intervalMs: number | null = null;
  cleared = false;

  setTimer = (fn: () => unknown, ms: number): unknown => {
    this.fn = fn;
    this.intervalMs = ms;
    return "the-timer";
  };

  clearTimer = (handle: unknown): void => {
    if (handle === "the-timer") this.cleared = true;
  };

  /** Simulate one timer firing (fires even mid-tick, like setInterval). */
  fire(): Promise<void> | void {
    if (this.cleared || !this.fn) return;
    return this.fn() as Promise<void> | void;
  }
}

/** Scripted fake exec: answers from a queue, records every argv. */
export class FakeExec {
  calls: string[][] = [];
  private queue: (ExecResult | Promise<ExecResult>)[] = [];
  /** Fallback when the queue is empty. */
  fallback: ExecResult = ok("{}");

  enqueue(...results: (ExecResult | Promise<ExecResult>)[]): void {
    this.queue.push(...results);
  }

  exec = async (argv: string[]): Promise<ExecResult> => {
    this.calls.push(argv);
    return this.queue.length > 0 ? this.queue.shift()! : this.fallback;
  };
}

/** In-memory fs recorder; `events` interleaves mkdir/write for order asserts. */
export class FakeFs {
  events: string[] = [];
  writes = new Map<string, string>();

  fs = {
    mkdir: async (path: string): Promise<void> => {
      this.events.push(`mkdir:${path}`);
    },
    writeFile: async (path: string, data: string, _opts?: { mode?: number }): Promise<void> => {
      this.events.push(`write:${path}`);
      this.writes.set(path, data);
    },
    rm: async (path: string): Promise<void> => {
      this.events.push(`rm:${path}`);
      this.writes.delete(path);
    },
  };
}

export function ok(stdout: string): ExecResult {
  return { exitCode: 0, stdout, stderr: "" };
}

export function fail(exitCode: number, stderr = ""): ExecResult {
  return { exitCode, stdout: "", stderr };
}

export function effectJson(effect: unknown): ExecResult {
  return ok(JSON.stringify(effect) + "\n");
}

export const testConfig: ResidentConfig = {
  mcHome: "/tmp/mc-home",
  image: "mc-fake-e2e:test",
  agentCmd: ["bun", "/app/src/agent-runner/main.ts"],
  landCmd: ["mc-land"],
  behaviorsDir: "/host/behaviors",
  runnerSrcDir: "/host/runner",
  workspaceRoot: "/host/workspace",
  spineVolume: "mc-spine-test",
  spineDbPath: "/mc/spine/spine.db",
  roleBehaviors: {
    editor: "/mc/behaviors/editor.json",
    worker: "/mc/behaviors/worker.json",
    verifier: "/mc/behaviors/verifier.json",
    packager: "/mc/behaviors/packager.json",
    strategist: "/mc/behaviors/strategist.json",
  },
};

export interface TestRig {
  deps: TickDeps;
  timer: FakeTimer;
  mc: FakeExec;
  docker: FakeExec;
  fakeFs: FakeFs;
  logs: string[];
}

export function makeRig(overrides: Partial<TickDeps> = {}): TestRig {
  const timer = new FakeTimer();
  const mc = new FakeExec();
  const docker = new FakeExec();
  const fakeFs = new FakeFs();
  const logs: string[] = [];
  const deps: TickDeps = {
    intervalMs: 500,
    setTimer: timer.setTimer,
    clearTimer: timer.clearTimer,
    runMc: mc.exec,
    docker: docker.exec,
    log: (msg) => logs.push(msg),
    fs: fakeFs.fs,
		precreateTaskSkeleton: async (request) => ({
			canonical: `${request.tasks_parent.canonical}/task-${request.task_id}`,
			device: request.tasks_parent.device,
			inode: request.tasks_parent.inode,
			owner_uid: request.tasks_parent.owner_uid,
		}),
		recoverTaskSkeleton: async (request) => {
			if (request.recover_root === undefined) throw new Error("missing recovery root");
			return request.recover_root;
		},
		recheckTaskParent: async () => {},
		recheckAcceptedSeal: async (seal) => `${testConfig.mcHome}/seals/${seal.run_id}`,
		registerTaskRoot: async () => {},
    config: testConfig,
    ...overrides,
  };
  return { deps, timer, mc, docker, fakeFs, logs };
}

/** A promise plus its resolver, for holding a tick open mid-test. */
export function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void } {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
