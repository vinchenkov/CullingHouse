#!/usr/bin/env bun
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { query, type Options } from "@anthropic-ai/claude-agent-sdk";
import { MAIN_NATIVE_FILE, NativeSessionStore } from "./session-store";

export const MINIMAX_BASE_URL = "https://api.minimax.io/anthropic";

const ALL_CREDENTIAL_KEYS = [
  "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
  "CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST", "CODEX_ACCESS_TOKEN", "CODEX_API_KEY",
  "CODEX_REFRESH_TOKEN_URL_OVERRIDE", "OPENAI_API_KEY",
];

export function claudeAdapterEnv(
  binding: "claude" | "minimax",
  base: Record<string, string | undefined>,
  configDir: string,
): Record<string, string> {
  const projectedOAuth = base["CLAUDE_CODE_OAUTH_TOKEN"];
  const projectedStatic = base["ANTHROPIC_AUTH_TOKEN"];
  const env: Record<string, string> = {};
  for (const [name, value] of Object.entries(base)) {
    if (value !== undefined && !ALL_CREDENTIAL_KEYS.includes(name) && !name.includes("REFRESH_TOKEN")) env[name] = value;
  }
  env.CLAUDE_CONFIG_DIR = configDir;
  env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = "1";
  env.DISABLE_AUTOUPDATER = "1";
  env.IS_SANDBOX = "1";
  if (binding === "claude") {
    if (base["CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST"] !== "1" || !projectedOAuth) {
      throw new Error("Claude binding requires the host-managed projected OAuth token");
    }
    env.CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST = "1";
    env.CLAUDE_CODE_OAUTH_TOKEN = projectedOAuth;
  } else {
    if (!projectedStatic) throw new Error("MiniMax binding requires its projected subscription token");
    env.ANTHROPIC_AUTH_TOKEN = projectedStatic;
    env.ANTHROPIC_BASE_URL = MINIMAX_BASE_URL;
  }
  return env;
}

export function claudeOptions(
  binding: "claude" | "minimax",
  workspace: string,
  env: Record<string, string>,
  store: NativeSessionStore,
  nativeRef?: string,
): Options {
  return {
    cwd: workspace,
    env,
    ...(binding === "minimax" ? { model: "MiniMax-M3" } : {}),
    ...(nativeRef ? { resume: nativeRef } : {}),
    allowDangerouslySkipPermissions: true,
    permissionMode: "bypassPermissions",
    settingSources: [],
    strictMcpConfig: true,
    systemPrompt: { type: "preset", preset: "claude_code" },
    tools: { type: "preset", preset: "claude_code" },
    sandbox: {
      enabled: true,
      failIfUnavailable: true,
      autoAllowBashIfSandboxed: true,
      allowUnsandboxedCommands: false,
      network: { allowedDomains: ["*"] },
      filesystem: { denyRead: ["/mc/session", env.CLAUDE_CONFIG_DIR!] },
      credentials: {
        files: [{ path: env.CLAUDE_CONFIG_DIR!, mode: "deny" }],
        envVars: [
          { name: "CLAUDE_CODE_OAUTH_TOKEN", mode: "deny" },
          { name: "ANTHROPIC_AUTH_TOKEN", mode: "deny" },
        ],
      },
      enableWeakerNestedSandbox: true,
    },
    persistSession: true,
    sessionStore: store,
    sessionStoreFlush: "eager",
    forwardSubagentText: true,
    stderr: (data) => process.stderr.write(data),
  };
}

function arg(name: string): string | undefined {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : undefined;
}

async function main(): Promise<number> {
  const binding = arg("--binding");
  const sessionDir = arg("--session-dir");
  const workspace = arg("--workspace");
  const nativeRef = arg("--native-ref");
  if ((binding !== "claude" && binding !== "minimax") || !sessionDir || !workspace) {
    console.error("claude adapter: invalid invocation");
    return 2;
  }
  const localRoot = await mkdtemp("/tmp/mc-claude-");
  const configDir = join(localRoot, "config");
  await mkdir(configDir, { mode: 0o700 });
  const store = new NativeSessionStore(sessionDir);
  let sessionSeen = false;
  let failed = false;
  try {
    const env = claudeAdapterEnv(binding, process.env, configDir);
    for await (const message of query({
      prompt: await Bun.stdin.text(),
      options: claudeOptions(binding, workspace, env, store, nativeRef),
    })) {
      console.log(JSON.stringify(message));
      if (!sessionSeen && message.type === "system" && message.subtype === "init") {
        console.log(JSON.stringify({
          event: "session-start", session_id: message.session_id, native_file: MAIN_NATIVE_FILE,
        }));
        sessionSeen = true;
      }
      if (message.type === "system" && message.subtype === "mirror_error") failed = true;
      if (message.type === "result" && message.is_error) failed = true;
    }
  } catch (err) {
    console.error(`claude adapter: ${err instanceof Error ? err.message : String(err)}`);
    return 1;
  } finally {
    await rm(localRoot, { recursive: true, force: true });
  }
  if (!sessionSeen) {
    console.error("claude adapter: query ended without a native session");
    return 2;
  }
  return failed ? 1 : 0;
}

if (import.meta.main) process.exit(await main());
