// mc.test.ts — the real seam spawns the binary, parses one JSON object from
// stdout, and reports exit code + stderr faithfully (ADR-024 D2).

import { describe, expect, test } from "bun:test";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { makeMcRunner } from "./mc";

function stubMc(script: string): string {
  const dir = mkdtempSync(join(tmpdir(), "mc-stub-"));
  const path = join(dir, "mc");
  writeFileSync(path, `#!/bin/sh\n${script}\n`, { mode: 0o755 });
  return path;
}

describe("makeMcRunner", () => {
  test("success: parses the JSON object and passes argv through", async () => {
    const bin = stubMc(`echo "{\\"argv\\":\\"$*\\"}"`);
    const res = await makeMcRunner(bin).run(["homie", "list"]);
    expect(res.code).toBe(0);
    expect(res.json).toEqual({ argv: "homie list" });
  });

  test("domain rejection: exit 1 with the error envelope and stderr", async () => {
    const bin = stubMc(`echo '{"error":{"code":"domain-rejection","message":"no"}}'; echo 'mc: no' >&2; exit 1`);
    const res = await makeMcRunner(bin).run(["homie", "history", "h-x"]);
    expect(res.code).toBe(1);
    expect((res.json?.["error"] as Record<string, unknown>)["code"]).toBe("domain-rejection");
    expect(res.stderr).toContain("mc: no");
  });

  test("non-JSON stdout yields json: null, never a throw", async () => {
    const bin = stubMc(`echo 'not json'; exit 2`);
    const res = await makeMcRunner(bin).run(["doctor"]);
    expect(res.code).toBe(2);
    expect(res.json).toBeNull();
  });
});
