import { afterEach, describe, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { claudeAdapterEnv, claudeOptions, MINIMAX_BASE_URL } from "./claude";
import { CODEX_BIN, codexAdapterEnv, codexArgv, findCodexNativeFile } from "./codex";
import { NativeSessionStore } from "./session-store";

const roots: string[] = [];
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true });
});

describe("Codex production adapter", () => {
  test("fresh/resume argv are closed, noninteractive, and externally sandboxed", () => {
    expect(codexArgv("fresh")).toEqual([
      CODEX_BIN, "exec", "--json", "--dangerously-bypass-approvals-and-sandbox",
      "--skip-git-repo-check", "--ignore-user-config", "--ignore-rules", "-",
    ]);
    expect(codexArgv("native", "thread-1")).toEqual([
      CODEX_BIN, "exec", "resume", "--json", "--dangerously-bypass-approvals-and-sandbox",
      "--skip-git-repo-check", "--ignore-user-config", "--ignore-rules", "thread-1", "-",
    ]);
    expect(() => codexArgv("native")).toThrow("thread id");
  });

  test("only the projected auth file/broker channel survives env construction", () => {
    const env = codexAdapterEnv({
      PATH: "/bin", CODEX_HOME: "/mc/codex",
      CODEX_REFRESH_TOKEN_URL_OVERRIDE: "http://host.docker.internal:7/oauth/token",
      OPENAI_API_KEY: "metered", CODEX_ACCESS_TOKEN: "identity", ANTHROPIC_AUTH_TOKEN: "foreign",
    });
    expect(env.CODEX_HOME).toBe("/mc/codex");
    expect(env.CODEX_REFRESH_TOKEN_URL_OVERRIDE).toContain("host.docker.internal");
    expect(env.OPENAI_API_KEY).toBeUndefined();
    expect(env.CODEX_ACCESS_TOKEN).toBeUndefined();
    expect(env.ANTHROPIC_AUTH_TOKEN).toBeUndefined();
  });

  test("the registered locator is the exact durable rollout under the session root", async () => {
    const root = mkdtempSync(join(tmpdir(), "mc-codex-native-"));
    roots.push(root);
    mkdirSync(join(root, "2026", "07", "22"), { recursive: true });
    writeFileSync(join(root, "2026", "07", "22", "rollout-11111111-1111-4111-8111-111111111111.jsonl"), "{}\n");
    expect(await findCodexNativeFile(root, "11111111-1111-4111-8111-111111111111", 10)).toBe(
      "2026/07/22/rollout-11111111-1111-4111-8111-111111111111.jsonl",
    );
  });
});

describe("Claude-SDK production adapter", () => {
  test("Claude and MiniMax receive exactly their own credential channel", () => {
    const base = {
      PATH: "/bin", MC_SPINE: "/mc/spine/spine.db",
      CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST: "1",
      CLAUDE_CODE_OAUTH_TOKEN: "short-oauth",
      ANTHROPIC_AUTH_TOKEN: "minimax-static",
      ANTHROPIC_API_KEY: "metered",
    };
    const claude = claudeAdapterEnv("claude", base, "/tmp/config");
    expect(claude.CLAUDE_CODE_OAUTH_TOKEN).toBe("short-oauth");
    expect(claude.ANTHROPIC_AUTH_TOKEN).toBeUndefined();
    expect(claude.ANTHROPIC_API_KEY).toBeUndefined();
    const minimax = claudeAdapterEnv("minimax", base, "/tmp/config");
    expect(minimax.ANTHROPIC_AUTH_TOKEN).toBe("minimax-static");
    expect(minimax.CLAUDE_CODE_OAUTH_TOKEN).toBeUndefined();
    expect(minimax.ANTHROPIC_BASE_URL).toBe(MINIMAX_BASE_URL);
  });

  test("SDK options pin isolation, tools, native persistence, and resume", () => {
    const store = new NativeSessionStore("/mc/session");
    const options = claudeOptions("minimax", "/workspace/source", { PATH: "/bin" }, store, "native-1");
    expect(options).toMatchObject({
      cwd: "/workspace/source", model: "MiniMax-M3", resume: "native-1",
      permissionMode: "bypassPermissions", allowDangerouslySkipPermissions: true,
      settingSources: [], strictMcpConfig: true, persistSession: true,
      sessionStore: store, sessionStoreFlush: "eager", forwardSubagentText: true,
      systemPrompt: { type: "preset", preset: "claude_code" },
      tools: { type: "preset", preset: "claude_code" },
    });
  });
});
