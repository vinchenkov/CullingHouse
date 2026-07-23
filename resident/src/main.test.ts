import { describe, expect, test } from "bun:test";
import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createBackupChore, loadRefreshGrants, publishTickReceipt, type RefreshGrantStoreDeps } from "./main";

function errno(code: string): Error & { code: string } {
  return Object.assign(new Error(`store failed: ${code}`), { code });
}

describe("refresh-grant store startup boundary", () => {
  test("an absent store means there are no configured credential bindings", async () => {
    const deps: RefreshGrantStoreDeps = {
      readdir: async () => { throw errno("ENOENT"); },
      readJSON: async () => { throw new Error("must not read a missing store"); },
    };
    expect(await loadRefreshGrants("/mc/home", deps)).toEqual([]);
  });

  test("a grant filename must exactly match its catalog binding", async () => {
    const deps: RefreshGrantStoreDeps = {
      readdir: async () => ["renamed.json"],
      readJSON: async () => ({
        binding: "minimax",
        channel: "static",
        env_name: "ANTHROPIC_AUTH_TOKEN",
        secret: "test-secret",
      }),
    };
    expect(loadRefreshGrants("/mc/home", deps)).rejects.toThrow("filename renamed.json does not match binding minimax");
  });

  test("permission and I/O errors refuse startup instead of launching token-free", async () => {
    for (const code of ["EACCES", "EPERM", "EIO"]) {
      const deps: RefreshGrantStoreDeps = {
        readdir: async () => { throw errno(code); },
        readJSON: async () => { throw new Error("must not read an unavailable store"); },
      };
      expect(loadRefreshGrants("/mc/home", deps)).rejects.toThrow(code);
    }
  });
});

test("resident tick receipt is atomic, owner-only, and release-bound", async () => {
  const home = await mkdtemp(join(tmpdir(), "mc-resident-receipt-"));
  const when = new Date("2026-07-22T12:34:56.000Z");
  await publishTickReceipt(home, "a".repeat(40), 1, when);
  const path = join(home, "health", "resident-tick.json");
  expect(JSON.parse(await readFile(path, "utf8"))).toEqual({
    version: 1,
    release_build_id: "a".repeat(40),
    config_schema_version: 1,
    completed_at: when.toISOString(),
  });
  expect((await stat(path)).mode & 0o777).toBe(0o600);
  await rm(home, { recursive: true });
});

describe("resident backup chore", () => {
  test("snapshots at startup and only when the serialized interval is due", async () => {
    let clock = 0;
    const calls: string[][] = [];
    const chore = createBackupChore(async (argv) => {
      calls.push(argv);
      return { exitCode: 0, stdout: JSON.stringify({ snapshot: `/mc/backups/${calls.length}.db`, bytes: 4096 }), stderr: "" };
    }, 1000, () => clock);
    await chore.startup();
    clock = 999;
    await chore.beforeTick();
    expect(calls).toEqual([["backup"]]);
    clock = 1000;
    await chore.beforeTick();
    expect(calls).toEqual([["backup"], ["backup"]]);
  });

  test("failed and malformed backups do not advance the due clock", async () => {
    let clock = 0;
    let attempt = 0;
    const chore = createBackupChore(async () => {
      attempt += 1;
      if (attempt === 1) return { exitCode: 0, stdout: JSON.stringify({ snapshot: "/mc/backups/ok.db", bytes: 1 }), stderr: "" };
      if (attempt === 2) return { exitCode: 1, stdout: "{}", stderr: "snapshot refused" };
      if (attempt === 3) return { exitCode: 0, stdout: "{}", stderr: "" };
      return { exitCode: 0, stdout: JSON.stringify({ snapshot: "/mc/backups/retry.db", bytes: 2 }), stderr: "" };
    }, 1000, () => clock);
    await chore.startup();
    clock = 1000;
    await expect(chore.beforeTick()).rejects.toThrow("snapshot refused");
    await expect(chore.beforeTick()).rejects.toThrow("invalid receipt");
    await chore.beforeTick();
    expect(attempt).toBe(4);
  });
});
