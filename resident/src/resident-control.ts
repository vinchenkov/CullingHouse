import { constants } from "node:fs";
import { open } from "node:fs/promises";
import { spawn } from "node:child_process";
import type { Readable } from "node:stream";
import { join } from "node:path";
import type { Exec, ExecResult } from "./types";

export const GATEWAY_CONTROL_VERSION = 1;
// Keep this in lockstep with substrate.CurrentSchemaVersion. The resident is
// intentionally a separate TypeScript artifact, so this value is duplicated
// and its control-channel handshake test pins the deployed schema fence.
export const SPINE_SCHEMA_VERSION = 13;
export const CONFIG_SCHEMA_VERSION = 1;
const MAX_FRAME_BYTES = 64 * 1024;
const HELLO_TIMEOUT_MS = 2_000;

export interface ResidentControlIdentity {
  mcHome: string;
  releaseBuildId: string;
  configSchemaVersion: number;
}

interface HelloFrame {
  op: "hello" | "hello_ack";
  seq: number;
  release_build_id: string;
  gateway_control_version: number;
  spine_schema_version: number;
  config_schema_version: number;
  deployment_uuid: string;
}

/**
 * Bind one host mc command prefix. Only exact `mc dispatch` receives the
 * resident-owned one-use fd 3; every other verb keeps ordinary stdio and
 * whole-command byte/exit passthrough (ADR-018 D6; ADR-016 D1).
 */
export function execMcVia(command: string[], identity: ResidentControlIdentity): Exec {
  if (command.length === 0) throw new Error("resident: empty mc command");
  return async (argv) => {
    if (argv.length === 1 && argv[0] === "dispatch") {
      return execControlledDispatch([...command, ...argv], identity);
    }
    return execPlain([...command, ...argv], identity);
  };
}

async function execPlain(command: string[], identity: ResidentControlIdentity): Promise<ExecResult> {
  const child = spawn(command[0]!, command.slice(1), {
    // Explicitly occupy fd 3 with /dev/null: Bun otherwise preserves an
    // unrelated parent descriptor there. Ordinary verbs must never receive
    // a resident control socket.
    stdio: ["ignore", "pipe", "pipe", "ignore"],
    env: mcEnvironment(identity.mcHome),
  });
  const stdout = collect(child.stdout);
  const stderr = collect(child.stderr);
  const exitCode = await childExit(child);
  return { exitCode, stdout: await stdout, stderr: await stderr };
}

async function execControlledDispatch(command: string[], identity: ResidentControlIdentity): Promise<ExecResult> {
  const deploymentUUID = await readDeploymentUUID(identity.mcHome);
  const child = Bun.spawn(command, {
    stdio: ["ignore", "pipe", "pipe", "pipe"],
    env: mcEnvironment(identity.mcHome),
  });
  const controlFD = child.stdio[3];
  if (typeof controlFD !== "number" || controlFD < 0) {
    child.kill(9);
    throw new Error("resident: dispatch did not create control fd 3");
  }
  const control = await BunControlChannel.fromFD(controlFD);
  const stdout = new Response(child.stdout).text();
  const stderr = new Response(child.stderr).text();
  const exited = child.exited;
  try {
    await withTimeout(
      serveHello(control, {
        op: "hello",
        seq: 1,
        release_build_id: identity.releaseBuildId,
        gateway_control_version: GATEWAY_CONTROL_VERSION,
        spine_schema_version: SPINE_SCHEMA_VERSION,
        config_schema_version: identity.configSchemaVersion,
        deployment_uuid: deploymentUUID,
      }),
      HELLO_TIMEOUT_MS,
      "resident control hello timeout",
    );
    const exitCode = await exited;
    await closeControl(control);
    return { exitCode, stdout: await stdout, stderr: await stderr };
  } catch (err) {
    await closeControl(control);
    try {
      await withTimeout(exited, 1_000, "dispatch did not exit after control refusal");
    } catch {
      child.kill(9);
      await exited.catch(() => undefined);
    }
    await Promise.allSettled([stdout, stderr]);
    throw err;
  }
}

function mcEnvironment(mcHome: string): Record<string, string | undefined> {
  const env = { ...process.env, MC_HOME: mcHome };
  // A resident invocation is always the Darwin host scope. Ambient developer
  // variables must not turn it into a direct-spine or agent-scoped call and
  // bypass the resident-only dispatch broker.
  delete env.MC_SPINE;
  delete env.MC_RUN_JSON;
  return env;
}

async function serveHello(control: BunControlChannel, expected: HelloFrame): Promise<void> {
  const prefix = await control.readExactly(4);
  const length = prefix.readUInt32BE(0);
  if (length < 1 || length > MAX_FRAME_BYTES) {
    throw new Error(`resident control hello length ${length} is outside 1..65536`);
  }
  const body = await control.readExactly(length);
  let parsed: unknown;
  try {
    parsed = JSON.parse(body.toString("utf8"));
  } catch (err) {
    throw new Error(`resident control hello is not JSON: ${String(err)}`);
  }
  const got = canonicalHello(parsed, "hello");
  if (body.toString("utf8") !== JSON.stringify(got)) {
    throw new Error("resident control hello is not the closed canonical encoding");
  }
  if (JSON.stringify(got) !== JSON.stringify(expected)) {
    throw new Error("resident control hello identity mismatch");
  }
  control.writeFrame({ ...expected, op: "hello_ack" });
}

function canonicalHello(value: unknown, op: HelloFrame["op"]): HelloFrame {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("resident control hello must be an object");
  }
  const v = value as Record<string, unknown>;
  const frame: HelloFrame = {
    op: stringField(v, "op") as HelloFrame["op"],
    seq: integerField(v, "seq"),
    release_build_id: stringField(v, "release_build_id"),
    gateway_control_version: integerField(v, "gateway_control_version"),
    spine_schema_version: integerField(v, "spine_schema_version"),
    config_schema_version: integerField(v, "config_schema_version"),
    deployment_uuid: stringField(v, "deployment_uuid"),
  };
  if (frame.op !== op || frame.seq !== 1) {
    throw new Error(`resident control expected ${op} sequence 1`);
  }
  return frame;
}

function stringField(v: Record<string, unknown>, key: string): string {
  const value = v[key];
  if (typeof value !== "string" || value.length === 0 || Buffer.byteLength(value) > 4096) {
    throw new Error(`resident control ${key} must be a bounded non-empty string`);
  }
  return value;
}

function integerField(v: Record<string, unknown>, key: string): number {
  const value = v[key];
  if (!Number.isSafeInteger(value)) {
    throw new Error(`resident control ${key} must be an integer`);
  }
  return value as number;
}

async function readDeploymentUUID(mcHome: string): Promise<string> {
  const path = join(mcHome, "deployment.uuid");
  const handle = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await handle.stat();
    if (!stat.isFile() || stat.size < 1 || stat.size > 4096) {
      throw new Error(`resident: deployment identity mirror ${JSON.stringify(path)} must be a bounded regular file`);
    }
    const deploymentUUID = (await handle.readFile("utf8")).trim();
    if (deploymentUUID.length === 0) {
      throw new Error(`resident: deployment identity mirror ${JSON.stringify(path)} is empty`);
    }
    return deploymentUUID;
  } finally {
    await handle.close();
  }
}

class BunControlChannel {
  private socket?: Bun.Socket<undefined>;
  private buffered = Buffer.alloc(0);
  private ended = false;
  private closed = false;
  private failure?: Error;
  private waiters: (() => void)[] = [];
  private closeWaiters: (() => void)[] = [];

  static async fromFD(fd: number): Promise<BunControlChannel> {
    const channel = new BunControlChannel();
    await Bun.connect({
      fd,
      socket: {
        open(socket) {
          channel.socket = socket;
        },
        data(_socket, data) {
          channel.buffered = Buffer.concat([channel.buffered, Buffer.from(data)]);
          channel.wake();
        },
        end() {
          channel.ended = true;
          channel.wake();
        },
        close(_socket, error) {
          channel.ended = true;
          channel.closed = true;
          if (error) channel.failure = error;
          channel.wake();
          for (const resolve of channel.closeWaiters.splice(0)) resolve();
        },
        error(_socket, error) {
          channel.failure = error;
          channel.wake();
        },
      },
    } as Bun.FdSocketOptions<undefined>);
    if (!channel.socket) throw new Error("resident: could not bind dispatch control fd 3");
    return channel;
  }

  async readExactly(length: number): Promise<Buffer> {
    while (this.buffered.length < length) {
      if (this.failure) throw this.failure;
      if (this.ended) throw new Error("resident control early EOF");
      await new Promise<void>((resolve) => this.waiters.push(resolve));
    }
    const result = this.buffered.subarray(0, length);
    this.buffered = this.buffered.subarray(length);
    return result;
  }

  writeFrame(value: HelloFrame): void {
    const body = Buffer.from(JSON.stringify(value));
    if (body.length < 1 || body.length > MAX_FRAME_BYTES) {
      throw new Error(`resident control frame length ${body.length} exceeds the 64-KiB bound`);
    }
    const prefix = Buffer.allocUnsafe(4);
    prefix.writeUInt32BE(body.length);
    const frame = Buffer.concat([prefix, body]);
    if (!this.socket || this.socket.write(frame) !== frame.length) {
      throw new Error("resident control hello_ack write did not make bounded progress");
    }
  }

  async close(): Promise<void> {
    if (this.closed || !this.socket) return;
    const closed = new Promise<void>((resolve) => this.closeWaiters.push(resolve));
    this.socket.close();
    await closed;
  }

  private wake(): void {
    for (const resolve of this.waiters.splice(0)) resolve();
  }
}

async function collect(stream: Readable | null): Promise<string> {
  if (!stream) return "";
  const chunks: Buffer[] = [];
  for await (const chunk of stream) chunks.push(Buffer.from(chunk));
  return Buffer.concat(chunks).toString("utf8");
}

function childExit(child: ReturnType<typeof spawn>): Promise<number> {
  return new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (code, signal) => resolve(code ?? (signal ? 128 : 2)));
  });
}

async function closeControl(control: BunControlChannel): Promise<void> {
  await control.close();
}

async function withTimeout<T>(promise: Promise<T>, ms: number, message: string): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error(message)), ms);
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}
