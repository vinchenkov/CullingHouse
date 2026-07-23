import { describe, expect, test } from "bun:test";
import { loadRefreshGrants, type RefreshGrantStoreDeps } from "./main";

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
