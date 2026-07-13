import { afterAll, describe, expect, test } from "bun:test";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const SCRIPT = join(import.meta.dir, "mc-land");
const roots: string[] = [];

afterAll(() => {
  for (const root of roots) rmSync(root, { recursive: true, force: true });
});

function exec(argv: string[], cwd: string, env?: Record<string, string>) {
  return Bun.spawnSync(argv, {
    cwd,
    env: { ...process.env, ...env },
    stdout: "pipe",
    stderr: "pipe",
  });
}

function must(argv: string[], cwd: string): string {
  const res = exec(argv, cwd);
  if (res.exitCode !== 0) {
    throw new Error(`${argv.join(" ")} failed: ${res.stderr.toString()}`);
  }
  return res.stdout.toString().trim();
}

function repoFixture() {
  const root = mkdtempSync(join(tmpdir(), "mc-land-test-"));
  roots.push(root);
  const repo = join(root, "repo");
  const worktree = join(root, "task-worktree");
  must(["git", "init", "-b", "main", repo], root);
  must(["git", "config", "user.name", "test"], repo);
  must(["git", "config", "user.email", "test@example.invalid"], repo);
  writeFileSync(join(repo, "base.txt"), "base\n");
  must(["git", "add", "base.txt"], repo);
  must(["git", "commit", "-m", "base"], repo);
  must(["git", "worktree", "add", "-b", "mc/task-1", worktree], repo);
  writeFileSync(join(worktree, "work.txt"), "reviewed\n");
  must(["git", "add", "work.txt"], worktree);
  must(["git", "commit", "-m", "task"], worktree);
  const sha = must(["git", "rev-parse", "HEAD"], worktree);
  return { root, repo, worktree, sha };
}

describe("mc-land split boundary (§7, Inv. 25)", () => {
  test("dirty task worktree is refused before main moves", () => {
    const f = repoFixture();
    writeFileSync(join(f.worktree, "unreviewed.txt"), "must not be erased after merge\n");

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("worktree");
    expect(exec(["git", "merge-base", "--is-ancestor", f.sha, "main"], f.repo).exitCode).not.toBe(0);
  });

  test("unexpected cleanup failure after merge is surfaced but never reported as a failed landing", () => {
    const f = repoFixture();
    const bin = join(f.root, "bin");
    must(["mkdir", "-p", bin], f.root);
    const realGit = Bun.which("git");
    if (realGit === null) throw new Error("git not found");
    const wrapper = join(bin, "git");
    writeFileSync(
      wrapper,
      `#!/bin/sh\nif [ "$1 $2" = "worktree remove" ]; then echo "forced cleanup failure" >&2; exit 77; fi\nexec "${realGit}" "$@"\n`,
    );
    chmodSync(wrapper, 0o755);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
      PATH: `${bin}:${process.env.PATH ?? ""}`,
    });
    expect(res.exitCode).toBe(0);
    expect(exec(["git", "merge-base", "--is-ancestor", f.sha, "main"], f.repo).exitCode).toBe(0);
    expect(res.stderr.toString()).toContain("cleanup debt");
    expect(readFileSync(join(f.worktree, "work.txt"), "utf8")).toBe("reviewed\n");
  });
});
