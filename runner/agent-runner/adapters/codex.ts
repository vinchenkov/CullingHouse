#!/usr/bin/env bun
import { readdir } from "node:fs/promises";
import { join, relative } from "node:path";

const FORBIDDEN = new Set([
  "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
  "CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST", "CODEX_ACCESS_TOKEN", "CODEX_API_KEY",
  "OPENAI_API_KEY",
]);
export const CODEX_BIN = process.env["MC_CODEX_BIN"] ||
  "/app/node_modules/@openai/codex-linux-arm64/vendor/aarch64-unknown-linux-musl/bin/codex";

export function codexAdapterEnv(base: Record<string, string | undefined>): Record<string, string> {
  const env: Record<string, string> = {};
  for (const [name, value] of Object.entries(base)) {
    if (value !== undefined && !FORBIDDEN.has(name) && !name.includes("REFRESH_TOKEN")) env[name] = value;
  }
  const home = base["CODEX_HOME"];
  const broker = base["CODEX_REFRESH_TOKEN_URL_OVERRIDE"];
  if (!home || !broker) throw new Error("Codex adapter requires projected CODEX_HOME and refresh broker");
  env.CODEX_HOME = home;
  env.CODEX_REFRESH_TOKEN_URL_OVERRIDE = broker;
  return env;
}

export function codexArgv(mode: "fresh" | "native", nativeRef?: string): string[] {
  const fixed = [
    "--json", "--ask-for-approval", "never", "--strict-config",
    "--config", 'default_permissions="mc"',
    "--config", 'permissions.mc.extends=":workspace"',
    "--config", 'permissions.mc.filesystem={"/mc/codex"="deny"}',
    "--config", "permissions.mc.network.enabled=true",
    "--config", 'permissions.mc.network.mode="full"',
    "--config", 'permissions.mc.network.domains={"*"="allow"}',
    "--skip-git-repo-check", "--ignore-user-config", "--ignore-rules",
  ];
  if (mode === "native") {
    if (!nativeRef) throw new Error("Codex native resume requires a thread id");
    return [CODEX_BIN, "exec", "resume", ...fixed, nativeRef, "-"];
  }
  return [CODEX_BIN, "exec", ...fixed, "-"];
}

async function walk(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...await walk(path));
    else if (entry.isFile() && entry.name.endsWith(".jsonl")) out.push(path);
  }
  return out;
}

export async function findCodexNativeFile(sessionDir: string, threadId: string, timeoutMs = 5_000): Promise<string> {
  if (!/^[0-9a-f-]{32,64}$/i.test(threadId)) throw new Error("Codex emitted an invalid thread id");
  const deadline = Date.now() + timeoutMs;
  while (true) {
    let files: string[] = [];
    try {
      files = await walk(sessionDir);
    } catch (err) {
      if (!(err !== null && typeof err === "object" && "code" in err && err.code === "ENOENT")) throw err;
    }
    const matches = files.filter((path) => path.includes(threadId));
    if (matches.length === 1) {
      const locator = relative(sessionDir, matches[0]!);
      if (locator.startsWith("..") || locator.startsWith("/")) throw new Error("Codex native trace escaped the session root");
      return locator;
    }
    if (matches.length > 1) throw new Error("Codex produced ambiguous native traces for one thread");
    if (Date.now() >= deadline) throw new Error("Codex native trace did not appear in the durable session root");
    await Bun.sleep(25);
  }
}

function arg(name: string): string | undefined {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : undefined;
}

async function main(): Promise<number> {
  const sessionDir = arg("--session-dir");
  const workspace = arg("--workspace");
  const mode = (arg("--mode") ?? "fresh") as "fresh" | "native";
  const nativeRef = arg("--native-ref");
  if (!sessionDir || !workspace || (mode !== "fresh" && mode !== "native")) {
    console.error("codex adapter: invalid invocation");
    return 2;
  }
  let env: Record<string, string>;
  let argv: string[];
  try {
    env = codexAdapterEnv(process.env);
    argv = codexArgv(mode, nativeRef);
  } catch (err) {
    console.error(`codex adapter: ${err instanceof Error ? err.message : String(err)}`);
    return 2;
  }
  const proc = Bun.spawn(argv, {
    cwd: workspace,
    env,
    stdin: new TextEncoder().encode(await Bun.stdin.text()),
    stdout: "pipe",
    stderr: "inherit",
  });
  const decoder = new TextDecoder();
  let buffer = "";
  let sessionSeen = false;
  for await (const chunk of proc.stdout) {
    buffer += decoder.decode(chunk, { stream: true });
    for (let newline = buffer.indexOf("\n"); newline >= 0; newline = buffer.indexOf("\n")) {
      const line = buffer.slice(0, newline);
      buffer = buffer.slice(newline + 1);
      console.log(line);
      if (sessionSeen) continue;
      try {
        const event = JSON.parse(line) as { type?: string; thread_id?: string };
        if (event.type === "thread.started" && typeof event.thread_id === "string") {
          const file = await findCodexNativeFile(sessionDir, event.thread_id);
          console.log(JSON.stringify({ event: "session-start", session_id: event.thread_id, native_file: file }));
          sessionSeen = true;
        }
      } catch (err) {
        if (err instanceof SyntaxError) continue;
        console.error(`codex adapter: ${err instanceof Error ? err.message : String(err)}`);
        proc.kill();
        await proc.exited;
        return 2;
      }
    }
  }
  const code = await proc.exited;
  if (!sessionSeen && code === 0) {
    console.error("codex adapter: successful CLI exit without a durable native session");
    return 2;
  }
  return code;
}

if (import.meta.main) process.exit(await main());
