import { afterAll, describe, expect, test } from "bun:test";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
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

function repoFixture(spacedPaths = false) {
  const root = mkdtempSync(join(tmpdir(), "mc-land-test-"));
  roots.push(root);
  const repo = join(root, spacedPaths ? "repo with spaces" : "repo");
  const worktree = join(root, spacedPaths ? "task worktree with spaces" : "task-worktree");
  must(["git", "init", "-b", "main", repo], root);
  must(["git", "config", "user.name", "test"], repo);
  must(["git", "config", "user.email", "test@example.invalid"], repo);
  writeFileSync(join(repo, "base.txt"), "base\n");
  must(["git", "add", "base.txt"], repo);
  must(["git", "commit", "-m", "base"], repo);
  const baseSHA = must(["git", "rev-parse", "main"], repo);
  must(["git", "worktree", "add", "-b", "mc/task-1", worktree], repo);
  writeFileSync(join(worktree, "work.txt"), "reviewed\n");
  must(["git", "add", "work.txt"], worktree);
  must(["git", "commit", "-m", "task"], worktree);
  const sha = must(["git", "rev-parse", "HEAD"], worktree);
  return { root, repo, worktree, baseSHA, sha };
}

function customDriverFixture() {
  const root = mkdtempSync(join(tmpdir(), "mc-land-driver-test-"));
  roots.push(root);
  const repo = join(root, "repo");
  const worktree = join(root, "task-worktree");
  const driver = join(root, "merge-driver");
  const driverResult = join(root, "driver-result");
  must(["git", "init", "-b", "main", repo], root);
  must(["git", "config", "user.name", "test"], repo);
  must(["git", "config", "user.email", "test@example.invalid"], repo);
  writeFileSync(driver, `#!/bin/sh\ncat "${driverResult}" > "$2"\n`);
  chmodSync(driver, 0o755);
  must(["git", "config", "merge.unstable.name", "mutable fixture driver"], repo);
  must(["git", "config", "merge.unstable.driver", `${driver} %O %A %B`], repo);
  writeFileSync(join(repo, ".gitattributes"), "conflict.txt merge=unstable\n");
  writeFileSync(join(repo, "conflict.txt"), "base\n");
  must(["git", "add", ".gitattributes", "conflict.txt"], repo);
  must(["git", "commit", "-m", "base"], repo);
  const baseSHA = must(["git", "rev-parse", "main"], repo);
  must(["git", "worktree", "add", "-b", "mc/task-1", worktree], repo);
  writeFileSync(join(worktree, "conflict.txt"), "task side\n");
  must(["git", "add", "conflict.txt"], worktree);
  must(["git", "commit", "-m", "task side"], worktree);
  const sha = must(["git", "rev-parse", "HEAD"], worktree);
  writeFileSync(join(repo, "conflict.txt"), "main side\n");
  must(["git", "add", "conflict.txt"], repo);
  must(["git", "commit", "-m", "main side"], repo);
  writeFileSync(driverResult, "first-driver-result\n");
  return { root, repo, worktree, driver, driverResult, baseSHA, sha };
}

describe("mc-land split boundary (§7, Inv. 25)", () => {
  test("fresh landing handles a worktree path containing spaces", () => {
    const f = repoFixture(true);
    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).toBe(0);
    expect(must(["git", "rev-parse", "main^2"], f.repo)).toBe(f.sha);
    expect(existsSync(f.worktree)).toBe(false);
  });

  test("an initiative shared branch is an authorized namespace (ADR-023)", () => {
    const root = mkdtempSync(join(tmpdir(), "mc-land-init-test-"));
    roots.push(root);
    const repo = join(root, "repo");
    const worktree = join(root, "initiative-worktree");
    must(["git", "init", "-b", "main", repo], root);
    must(["git", "config", "user.name", "test"], repo);
    must(["git", "config", "user.email", "test@example.invalid"], repo);
    writeFileSync(join(repo, "base.txt"), "base\n");
    must(["git", "add", "base.txt"], repo);
    must(["git", "commit", "-m", "base"], repo);
    must(["git", "worktree", "add", "-b", "mc/initiative-7", worktree], repo);
    writeFileSync(join(worktree, "child.txt"), "child work\n");
    must(["git", "add", "child.txt"], worktree);
    must(["git", "commit", "-m", "child"], worktree);
    const sha = must(["git", "rev-parse", "HEAD"], worktree);
    const res = exec(["sh", SCRIPT, "mc/initiative-7", sha, "main"], repo, {
      MC_LAND_WORKSPACE: repo,
    });
    expect(res.exitCode).toBe(0);
    expect(must(["git", "rev-parse", "main^2"], repo)).toBe(sha);
    expect(existsSync(worktree)).toBe(false);
  });

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

  test("ignored untracked worktree bytes are preserved before merge", () => {
    const f = repoFixture();
    writeFileSync(join(f.repo, ".git", "info", "exclude"), ".env\n");
    writeFileSync(join(f.worktree, ".env"), "operator secret\n");

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("dirty");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
    expect(readFileSync(join(f.worktree, ".env"), "utf8")).toBe("operator secret\n");
  });

  for (const indexFlag of ["--assume-unchanged", "--skip-worktree"]) {
    test(`task worktree carrying ${indexFlag} is refused before merge`, () => {
      const f = repoFixture();
      must(["git", "update-index", indexFlag, "work.txt"], f.worktree);
      writeFileSync(join(f.worktree, "work.txt"), "operator bytes hidden from ordinary diff\n");

      const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
        MC_LAND_WORKSPACE: f.repo,
      });
      expect(res.exitCode).not.toBe(0);
      expect(res.stderr.toString()).toContain("index visibility flags");
      expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
      expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
      expect(readFileSync(join(f.worktree, "work.txt"), "utf8"))
        .toBe("operator bytes hidden from ordinary diff\n");
    });
  }

  test("repo stat-cache settings cannot hide timestamp-restored task-worktree bytes", () => {
    const f = repoFixture();
    const work = join(f.worktree, "work.txt");
    const timestampReference = join(f.root, "timestamp-reference");
    must(["touch", "-t", "202001010000", work], f.root);
    must(["git", "update-index", "--refresh"], f.worktree);
    must(["cp", "-p", work, timestampReference], f.root);
    must(["git", "config", "core.trustctime", "false"], f.repo);
    must(["git", "config", "core.checkStat", "minimal"], f.repo);
    writeFileSync(work, "operator\n"); // same byte length as "reviewed\n"
    must(["touch", "-r", timestampReference, work], f.root);
    expect(must(["git", "status", "--porcelain"], f.worktree)).toBe("");

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("dirty");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
    expect(readFileSync(work, "utf8")).toBe("operator\n");
  });

  test("repo stat-cache settings cannot hide overlapping primary-checkout bytes", () => {
    const f = repoFixture();
    writeFileSync(join(f.worktree, "base.txt"), "task\n");
    must(["git", "add", "base.txt"], f.worktree);
    must(["git", "commit", "-m", "task changes shared path"], f.worktree);
    const reviewedSHA = must(["git", "rev-parse", "HEAD"], f.worktree);

    const primaryPath = join(f.repo, "base.txt");
    const timestampReference = join(f.root, "primary-timestamp-reference");
    must(["touch", "-t", "202001010000", primaryPath], f.root);
    must(["git", "update-index", "--refresh"], f.repo);
    must(["cp", "-p", primaryPath, timestampReference], f.root);
    must(["git", "config", "core.trustctime", "false"], f.repo);
    must(["git", "config", "core.checkStat", "minimal"], f.repo);
    writeFileSync(primaryPath, "oper\n"); // same length as "base\n"
    must(["touch", "-r", timestampReference, primaryPath], f.root);
    expect(must(["git", "status", "--porcelain"], f.repo)).toBe("");

    const res = exec(["sh", SCRIPT, "mc/task-1", reviewedSHA, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("primary checkout");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(readFileSync(primaryPath, "utf8")).toBe("oper\n");
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(reviewedSHA);
    expect(existsSync(f.worktree)).toBe(true);
  });

  for (const indexFlag of ["--assume-unchanged", "--skip-worktree"]) {
    test(`primary checkout carrying ${indexFlag} is refused before merge`, () => {
      const f = repoFixture();
      writeFileSync(join(f.worktree, "base.txt"), "reviewed shared change\n");
      must(["git", "add", "base.txt"], f.worktree);
      must(["git", "commit", "-m", "task changes shared path"], f.worktree);
      const reviewedSHA = must(["git", "rev-parse", "HEAD"], f.worktree);
      must(["git", "update-index", indexFlag, "base.txt"], f.repo);
      writeFileSync(join(f.repo, "base.txt"), "operator primary bytes\n");

      const res = exec(["sh", SCRIPT, "mc/task-1", reviewedSHA, "main"], f.repo, {
        MC_LAND_WORKSPACE: f.repo,
      });
      expect(res.exitCode).not.toBe(0);
      expect(res.stderr.toString()).toContain("primary checkout index visibility flags");
      expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
      expect(readFileSync(join(f.repo, "base.txt"), "utf8")).toBe("operator primary bytes\n");
      expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(reviewedSHA);
      expect(existsSync(f.worktree)).toBe(true);
    });
  }

  test("ignored untracked primary bytes colliding with a reviewed path are preserved", () => {
    const f = repoFixture();
    writeFileSync(join(f.repo, ".gitignore"), "secret.env\n");
    must(["git", "add", ".gitignore"], f.repo);
    must(["git", "commit", "-m", "ignore local secret"], f.repo);
    const targetSHA = must(["git", "rev-parse", "main"], f.repo);
    must(["git", "rebase", "main"], f.worktree);
    writeFileSync(join(f.worktree, "secret.env"), "reviewed value\n");
    must(["git", "add", "-f", "secret.env"], f.worktree);
    must(["git", "commit", "-m", "add reviewed env template"], f.worktree);
    const reviewedSHA = must(["git", "rev-parse", "HEAD"], f.worktree);
    writeFileSync(join(f.repo, "secret.env"), "operator secret\n");
    expect(must(["git", "status", "--porcelain"], f.repo)).toBe("");

    const res = exec(["sh", SCRIPT, "mc/task-1", reviewedSHA, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("untracked primary path collision");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(targetSHA);
    expect(readFileSync(join(f.repo, "secret.env"), "utf8")).toBe("operator secret\n");
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(reviewedSHA);
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("unrelated tracked and ignored primary bytes survive a successful merge", () => {
    const f = repoFixture();
    writeFileSync(join(f.repo, "base.txt"), "unrelated operator edit\n");
    writeFileSync(join(f.repo, ".git", "info", "exclude"), ".env\n");
    writeFileSync(join(f.repo, ".env"), "unrelated operator secret\n");

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).toBe(0);
    expect(must(["git", "rev-parse", "main^2"], f.repo)).toBe(f.sha);
    expect(must(["git", "show", "main:base.txt"], f.repo)).toBe("base");
    expect(readFileSync(join(f.repo, "base.txt"), "utf8")).toBe("unrelated operator edit\n");
    expect(readFileSync(join(f.repo, ".env"), "utf8")).toBe("unrelated operator secret\n");
    expect(existsSync(f.worktree)).toBe(false);
  });

  test("an operator merge in progress is refused without aborting it", () => {
    const f = repoFixture();
    const operatorWorktree = join(f.root, "operator-worktree");
    must(["git", "worktree", "add", "-b", "operator-merge", operatorWorktree, f.baseSHA], f.repo);
    writeFileSync(join(operatorWorktree, "base.txt"), "operator branch\n");
    must(["git", "add", "base.txt"], operatorWorktree);
    must(["git", "commit", "-m", "operator branch change"], operatorWorktree);
    writeFileSync(join(f.repo, "base.txt"), "operator main\n");
    must(["git", "add", "base.txt"], f.repo);
    must(["git", "commit", "-m", "operator main change"], f.repo);
    const targetSHA = must(["git", "rev-parse", "main"], f.repo);
    const conflict = exec(["git", "merge", "operator-merge"], f.repo);
    expect(conflict.exitCode).not.toBe(0);
    const mergeHead = must(["git", "rev-parse", "MERGE_HEAD"], f.repo);
    const conflictBytes = readFileSync(join(f.repo, "base.txt"), "utf8");

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("merge already in progress");
    expect(must(["git", "rev-parse", "MERGE_HEAD"], f.repo)).toBe(mergeHead);
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(targetSHA);
    expect(readFileSync(join(f.repo, "base.txt"), "utf8")).toBe(conflictBytes);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("directory rename inference cannot relocate a reviewed path onto ignored operator bytes", () => {
    const root = mkdtempSync(join(tmpdir(), "mc-land-directory-rename-test-"));
    roots.push(root);
    const repo = join(root, "repo");
    const worktree = join(root, "task-worktree");
    must(["git", "init", "-b", "main", repo], root);
    must(["git", "config", "user.name", "test"], repo);
    must(["git", "config", "user.email", "test@example.invalid"], repo);
    mkdirSync(join(repo, "old"));
    writeFileSync(join(repo, ".gitignore"), "secret.env\n");
    writeFileSync(join(repo, "old", "a.txt"), "base\n");
    must(["git", "add", ".gitignore", "old/a.txt"], repo);
    must(["git", "commit", "-m", "base"], repo);
    must(["git", "worktree", "add", "-b", "mc/task-1", worktree], repo);
    writeFileSync(join(worktree, "old", "secret.env"), "reviewed value\n");
    must(["git", "add", "-f", "old/secret.env"], worktree);
    must(["git", "commit", "-m", "task adds reviewed path"], worktree);
    const reviewedSHA = must(["git", "rev-parse", "HEAD"], worktree);
    mkdirSync(join(repo, "new"));
    must(["git", "mv", "old/a.txt", "new/a.txt"], repo);
    must(["git", "commit", "-m", "operator renames directory"], repo);
    const targetSHA = must(["git", "rev-parse", "main"], repo);
    writeFileSync(join(repo, "new", "secret.env"), "operator secret\n");

    const res = exec(["sh", SCRIPT, "mc/task-1", reviewedSHA, "main"], repo, {
      MC_LAND_WORKSPACE: repo,
    });
    if (res.exitCode !== 0) {
      throw new Error(`directory-rename landing failed: ${res.stderr.toString()}`);
    }
    expect(res.exitCode).toBe(0);
    expect(must(["git", "rev-parse", "main^1"], repo)).toBe(targetSHA);
    expect(must(["git", "rev-parse", "main^2"], repo)).toBe(reviewedSHA);
    expect(must(["git", "show", "main:old/secret.env"], repo)).toBe("reviewed value");
    expect(exec(["git", "show", "main:new/secret.env"], repo).exitCode).not.toBe(0);
    expect(readFileSync(join(repo, "new", "secret.env"), "utf8")).toBe("operator secret\n");
    expect(existsSync(worktree)).toBe(false);
  });

  test("file rename inference cannot remap a reviewed edit over hidden operator bytes", () => {
    const root = mkdtempSync(join(tmpdir(), "mc-land-file-rename-test-"));
    roots.push(root);
    const repo = join(root, "repo");
    const worktree = join(root, "task-worktree");
    must(["git", "init", "-b", "main", repo], root);
    must(["git", "config", "user.name", "test"], repo);
    must(["git", "config", "user.email", "test@example.invalid"], repo);
    writeFileSync(join(repo, "old.txt"), "base\n");
    must(["git", "add", "old.txt"], repo);
    must(["git", "commit", "-m", "base"], repo);
    must(["git", "worktree", "add", "-b", "mc/task-1", worktree], repo);
    writeFileSync(join(worktree, "old.txt"), "task\n");
    must(["git", "add", "old.txt"], worktree);
    must(["git", "commit", "-m", "task edits old path"], worktree);
    const reviewedSHA = must(["git", "rev-parse", "HEAD"], worktree);
    must(["git", "mv", "old.txt", "new.txt"], repo);
    must(["git", "commit", "-m", "operator renames file"], repo);
    const targetSHA = must(["git", "rev-parse", "main"], repo);
    const renamed = join(repo, "new.txt");
    const timestampReference = join(root, "rename-timestamp-reference");
    must(["touch", "-t", "202001010000", renamed], root);
    must(["git", "update-index", "--refresh"], repo);
    must(["cp", "-p", renamed, timestampReference], root);
    must(["git", "config", "core.trustctime", "false"], repo);
    must(["git", "config", "core.checkStat", "minimal"], repo);
    writeFileSync(renamed, "oper\n");
    must(["touch", "-r", timestampReference, renamed], root);
    expect(must(["git", "status", "--porcelain"], repo)).toBe("");

    const res = exec(["sh", SCRIPT, "mc/task-1", reviewedSHA, "main"], repo, {
      MC_LAND_WORKSPACE: repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(must(["git", "rev-parse", "main"], repo)).toBe(targetSHA);
    expect(readFileSync(renamed, "utf8")).toBe("oper\n");
    expect(must(["git", "rev-parse", "mc/task-1"], repo)).toBe(reviewedSHA);
    expect(existsSync(worktree)).toBe(true);
  });

  test("owned conflict abort restores merge paths without erasing unrelated hidden bytes", () => {
    const root = mkdtempSync(join(tmpdir(), "mc-land-owned-abort-test-"));
    roots.push(root);
    const repo = join(root, "repo");
    const worktree = join(root, "task-worktree");
    must(["git", "init", "-b", "main", repo], root);
    must(["git", "config", "user.name", "test"], repo);
    must(["git", "config", "user.email", "test@example.invalid"], repo);
    writeFileSync(join(repo, "conflict.txt"), "base conflict\n");
    writeFileSync(join(repo, "unrelated.txt"), "base\n");
    must(["git", "add", "conflict.txt", "unrelated.txt"], repo);
    must(["git", "commit", "-m", "base"], repo);
    must(["git", "worktree", "add", "-b", "mc/task-1", worktree], repo);
    writeFileSync(join(worktree, "conflict.txt"), "task conflict\n");
    must(["git", "add", "conflict.txt"], worktree);
    must(["git", "commit", "-m", "task conflict"], worktree);
    const reviewedSHA = must(["git", "rev-parse", "HEAD"], worktree);
    writeFileSync(join(repo, "conflict.txt"), "main conflict\n");
    must(["git", "add", "conflict.txt"], repo);
    must(["git", "commit", "-m", "main conflict"], repo);
    const targetSHA = must(["git", "rev-parse", "main"], repo);

    const unrelated = join(repo, "unrelated.txt");
    const timestampReference = join(root, "abort-timestamp-reference");
    must(["touch", "-t", "202001010000", unrelated], root);
    must(["git", "update-index", "--refresh"], repo);
    must(["cp", "-p", unrelated, timestampReference], root);
    must(["git", "config", "core.trustctime", "false"], repo);
    must(["git", "config", "core.checkStat", "minimal"], repo);
    writeFileSync(unrelated, "oper\n");
    must(["touch", "-r", timestampReference, unrelated], root);
    expect(must(["git", "status", "--porcelain"], repo)).toBe("");

    const res = exec(["sh", SCRIPT, "mc/task-1", reviewedSHA, "main"], repo, {
      MC_LAND_WORKSPACE: repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(exec(["git", "rev-parse", "--verify", "MERGE_HEAD"], repo).exitCode).not.toBe(0);
    expect(must(["git", "rev-parse", "main"], repo)).toBe(targetSHA);
    expect(readFileSync(join(repo, "conflict.txt"), "utf8")).toBe("main conflict\n");
    expect(readFileSync(unrelated, "utf8")).toBe("oper\n");
    expect(must(["git", "rev-parse", "mc/task-1"], repo)).toBe(reviewedSHA);
    expect(existsSync(worktree)).toBe(true);
  });

  test("failure handling does not abort an operator merge won after preflight", () => {
    const f = repoFixture();
    const operatorWorktree = join(f.root, "operator-race-worktree");
    must(["git", "worktree", "add", "-b", "operator-race", operatorWorktree, f.baseSHA], f.repo);
    writeFileSync(join(operatorWorktree, "base.txt"), "operator branch\n");
    must(["git", "add", "base.txt"], operatorWorktree);
    must(["git", "commit", "-m", "operator race branch"], operatorWorktree);
    const operatorMergeSHA = must(["git", "rev-parse", "operator-race"], f.repo);
    writeFileSync(join(f.repo, "base.txt"), "operator main\n");
    must(["git", "add", "base.txt"], f.repo);
    must(["git", "commit", "-m", "operator race main"], f.repo);
    const targetSHA = must(["git", "rev-parse", "main"], f.repo);

    const realGit = Bun.which("git");
    if (realGit === null) throw new Error("git not found");
    const bin = join(f.root, "operator-race-bin");
    const marker = join(f.root, "operator-race-triggered");
    must(["mkdir", "-p", bin], f.root);
    const wrapper = join(bin, "git");
    writeFileSync(
      wrapper,
      "#!/bin/sh\n" +
        "for arg do\n" +
        "  if [ \"$arg\" = merge ] && [ ! -e \"$MC_OPERATOR_RACE_MARKER\" ]; then\n" +
        "    : > \"$MC_OPERATOR_RACE_MARKER\"\n" +
        `    env -u GIT_DIR -u GIT_COMMON_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE ${realGit} ` +
        `-C \"$MC_OPERATOR_RACE_REPO\" merge operator-race >/dev/null 2>&1 || true\n` +
        "    printf 'operator resolution in progress\\n' > base.txt\n" +
        "    break\n" +
        "  fi\n" +
        "done\n" +
        `exec ${realGit} "$@"\n`,
    );
    chmodSync(wrapper, 0o755);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
      MC_OPERATOR_RACE_MARKER: marker,
      MC_OPERATOR_RACE_REPO: f.repo,
      PATH: `${bin}:${process.env.PATH ?? ""}`,
    });
    expect(res.exitCode).not.toBe(0);
    expect(must(["git", "rev-parse", "MERGE_HEAD"], f.repo)).toBe(operatorMergeSHA);
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(targetSHA);
    expect(readFileSync(join(f.repo, "base.txt"), "utf8"))
      .toBe("operator resolution in progress\n");
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
    expect(existsSync(f.worktree)).toBe(true);
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
      "#!/bin/sh\n" +
        "previous=\n" +
        "for arg do\n" +
        "  if [ \"$previous $arg\" = \"worktree remove\" ]; then echo \"forced cleanup failure\" >&2; exit 77; fi\n" +
        "  previous=$arg\n" +
        "done\n" +
        `exec "${realGit}" "$@"\n`,
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

    // If operator state appears in the residue before recovery, success
    // truth still wins but cleanup must preserve the now-ambiguous worktree.
    const landedTip = must(["git", "rev-parse", "main"], f.repo);
    writeFileSync(join(f.repo, ".git", "info", "exclude"), ".env\n");
    writeFileSync(join(f.worktree, ".env"), "do not delete\n");
    const retry = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(retry.exitCode).toBe(0);
    expect(retry.stderr.toString()).toContain("now dirty; preserving it");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(landedTip);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
    expect(readFileSync(join(f.worktree, ".env"), "utf8")).toBe("do not delete\n");
  });

  test("late task core.worktree redirect cannot hide bytes from cleanup", () => {
    const f = repoFixture();
    const redirected = join(f.root, "late-clean-redirect");
    mkdirSync(redirected);
    writeFileSync(join(redirected, "base.txt"), "base\n");
    writeFileSync(join(redirected, "work.txt"), "reviewed\n");
    writeFileSync(join(f.repo, ".git", "info", "exclude"), ".env\n");
    must(["git", "config", "extensions.worktreeConfig", "true"], f.repo);

    const realGit = Bun.which("git");
    if (realGit === null) throw new Error("git not found");
    const bin = join(f.root, "late-cleanup-config-bin");
    const marker = join(f.root, "late-cleanup-config-injected");
    must(["mkdir", "-p", bin], f.root);
    const wrapper = join(bin, "git");
    writeFileSync(
      wrapper,
      "#!/bin/sh\n" +
        "previous=\n" +
        "for arg do\n" +
        "  if [ \"$previous $arg\" = \"worktree remove\" ] && " +
        "[ ! -e \"$MC_LATE_CLEANUP_MARKER\" ]; then\n" +
        "    : > \"$MC_LATE_CLEANUP_MARKER\"\n" +
        "    printf 'operator secret\\n' > \"$MC_LATE_TASK_WORKTREE/.env\"\n" +
        `    env -u GIT_DIR -u GIT_COMMON_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE ${realGit} ` +
        `-C \"$MC_LATE_TASK_WORKTREE\" config --worktree core.worktree ` +
        `\"$MC_LATE_REDIRECT\"\n` +
        "    break\n" +
        "  fi\n" +
        "  previous=$arg\n" +
        "done\n" +
        `exec ${realGit} \"$@\"\n`,
    );
    chmodSync(wrapper, 0o755);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
      MC_LATE_CLEANUP_MARKER: marker,
      MC_LATE_TASK_WORKTREE: f.worktree,
      MC_LATE_REDIRECT: redirected,
      PATH: `${bin}:${process.env.PATH ?? ""}`,
    });
    expect(res.exitCode).toBe(0);
    expect(existsSync(marker)).toBe(true);
    expect(must(["git", "rev-parse", "main^2"], f.repo)).toBe(f.sha);
    expect(res.stderr.toString()).toContain("cleanup debt");
    expect(existsSync(f.worktree)).toBe(true);
    expect(readFileSync(join(f.worktree, ".env"), "utf8")).toBe("operator secret\n");
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
  });

  test("report-loss retry recognizes the exact merge receipt after cleanup", () => {
    const f = repoFixture();
    must(["git", "config", "merge.log", "true"], f.repo);

    const first = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(first.exitCode).toBe(0);
    const landedTip = must(["git", "rev-parse", "main"], f.repo);
    expect(must(["git", "show", "-s", "--format=%B", "main"], f.repo)).toBe(
      `Land mc/task-1 (${f.sha}) into main\n\n` +
        `MC-Landing-Receipt: v1\n` +
        `MC-Branch: mc/task-1\n` +
        `MC-Verified-SHA: ${f.sha}\n` +
        "MC-Target: main",
    );
    expect(must(["git", "rev-parse", "main^2"], f.repo)).toBe(f.sha);
    expect(must(["git", "rev-list", "--count", "--merges", `${f.baseSHA}..main`], f.repo)).toBe("1");
    expect(existsSync(f.worktree)).toBe(false);
    expect(exec(["git", "show-ref", "--verify", "refs/heads/mc/task-1"], f.repo).exitCode).not.toBe(0);

    // The Git effect committed, but imagine the resident died before its
    // separate `mc land report`. The same durable land effect must be safe
    // to execute again even though successful cleanup removed its inputs.
    const retry = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(retry.exitCode).toBe(0);
    expect(retry.stderr.toString()).toContain("cleanup/report debt");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(landedTip);
    expect(must(["git", "rev-list", "--count", "--merges", `${f.baseSHA}..main`], f.repo)).toBe("1");
  });

  test("receipt replay ignores executable merge config added after success", () => {
    const f = repoFixture();

    const first = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(first.exitCode).toBe(0);
    const landedTip = must(["git", "rev-parse", "main"], f.repo);

    // Recovery must inspect only the immutable merge receipt. Configuration
    // that would be unsafe during a fresh content operation appears later,
    // after cleanup removed every input the report retry no longer needs.
    must(["git", "config", "merge.later.driver", "false"], f.repo);
    const retry = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(retry.exitCode).toBe(0);
    expect(retry.stderr.toString()).toContain("cleanup/report debt");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(landedTip);
  });

  test("fresh landing refuses an executable custom merge driver before it can mutate secrets", () => {
    const f = customDriverFixture();
    const targetSHA = must(["git", "rev-parse", "main"], f.repo);
    const secret = join(f.repo, "operator.secret");
    writeFileSync(join(f.repo, ".git", "info", "exclude"), "operator.secret\n");
    writeFileSync(secret, "operator secret\n");
    writeFileSync(
      f.driver,
      `#!/bin/sh\nrm -f "${secret}"\ncat "${f.driverResult}" > "$2"\n`,
    );

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("executable Git merge driver");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(targetSHA);
    expect(readFileSync(secret, "utf8")).toBe("operator secret\n");
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("fresh landing refuses an executable clean filter before preflight can mutate secrets", () => {
    const f = repoFixture();
    writeFileSync(join(f.worktree, ".gitattributes"), "work.txt filter=evil\n");
    must(["git", "add", ".gitattributes"], f.worktree);
    must(["git", "commit", "-m", "task declares filter"], f.worktree);
    const reviewedSHA = must(["git", "rev-parse", "HEAD"], f.worktree);
    const secret = join(f.repo, "operator.secret");
    const filter = join(f.root, "evil-clean-filter");
    writeFileSync(join(f.repo, ".git", "info", "exclude"), "operator.secret\n");
    writeFileSync(secret, "operator secret\n");
    writeFileSync(filter, `#!/bin/sh\nrm -f "${secret}"\ncat\n`);
    chmodSync(filter, 0o755);
    must(["git", "config", "filter.evil.clean", filter], f.repo);

    const res = exec(["sh", SCRIPT, "mc/task-1", reviewedSHA, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("content filter");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(readFileSync(secret, "utf8")).toBe("operator secret\n");
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(reviewedSHA);
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("task-worktree-only clean filter is refused before it can mutate operator bytes", () => {
    const f = repoFixture();
    writeFileSync(join(f.worktree, ".gitattributes"), "work.txt filter=task-evil\n");
    must(["git", "add", ".gitattributes"], f.worktree);
    must(["git", "commit", "-m", "task declares task-local filter"], f.worktree);
    const reviewedSHA = must(["git", "rev-parse", "HEAD"], f.worktree);
    const marker = join(f.root, "task-filter-ran");
    const filter = join(f.root, "task-only-clean-filter");
    writeFileSync(filter, `#!/bin/sh\nprintf ran > "${marker}"\ncat\n`);
    chmodSync(filter, 0o755);
    must(["git", "config", "extensions.worktreeConfig", "true"], f.repo);
    must(["git", "config", "--worktree", "filter.task-evil.clean", filter], f.worktree);

    const res = exec(["sh", SCRIPT, "mc/task-1", reviewedSHA, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("task worktree");
    expect(res.stderr.toString()).toContain("content filter");
    expect(existsSync(marker)).toBe(false);
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(reviewedSHA);
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("executable merge config inserted after preflight cannot run during merge", () => {
    const f = customDriverFixture();
    must(["git", "config", "--unset", "merge.unstable.driver"], f.repo);
    const targetSHA = must(["git", "rev-parse", "main"], f.repo);
    const secret = join(f.repo, "operator.secret");
    const driverRan = join(f.root, "late-driver-ran");
    const lateDriver = join(f.root, "late-merge-driver");
    writeFileSync(join(f.repo, ".git", "info", "exclude"), "operator.secret\n");
    writeFileSync(secret, "operator secret\n");
    writeFileSync(
      lateDriver,
      `#!/bin/sh\nrm -f "${secret}"\nprintf ran > "${driverRan}"\n` +
        `cat "${f.driverResult}" > "$2"\n`,
    );
    chmodSync(lateDriver, 0o755);
    const realGit = Bun.which("git");
    if (realGit === null) throw new Error("git not found");
    const bin = join(f.root, "late-config-bin");
    must(["mkdir", "-p", bin], f.root);
    const wrapper = join(bin, "git");
    writeFileSync(
      wrapper,
      "#!/bin/sh\n" +
        "for arg do\n" +
        "  if [ \"$arg\" = merge ] && [ ! -e \"$MC_LATE_CONFIG_MARKER\" ]; then\n" +
        "    : > \"$MC_LATE_CONFIG_MARKER\"\n" +
        `    env -u GIT_DIR -u GIT_COMMON_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE ${realGit} ` +
        `-C \"$MC_LATE_CONFIG_REPO\" config merge.unstable.driver ` +
        `\"${lateDriver} %O %A %B\"\n` +
        "    break\n" +
        "  fi\n" +
        "done\n" +
        `exec ${realGit} \"$@\"\n`,
    );
    chmodSync(wrapper, 0o755);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
      MC_LATE_CONFIG_MARKER: join(f.root, "late-config-inserted"),
      MC_LATE_CONFIG_REPO: f.repo,
      PATH: `${bin}:${process.env.PATH ?? ""}`,
    });
    expect(existsSync(join(f.root, "late-config-inserted"))).toBe(true);
    expect(existsSync(driverRan)).toBe(false);
    expect(readFileSync(secret, "utf8")).toBe("operator secret\n");
    if (res.exitCode === 0) {
      expect(must(["git", "rev-parse", "main^2"], f.repo)).toBe(f.sha);
      expect(existsSync(f.worktree)).toBe(false);
    } else {
      expect(must(["git", "rev-parse", "main"], f.repo)).toBe(targetSHA);
      expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
      expect(existsSync(f.worktree)).toBe(true);
    }
  });

  test("target branch mergeOptions cannot replace the reviewed tree with ours", () => {
    const f = repoFixture();
    must(["git", "config", "branch.main.mergeOptions", "-s ours"], f.repo);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).toBe(0);
    expect(must(["git", "rev-parse", "main^2"], f.repo)).toBe(f.sha);
    expect(must(["git", "show", "main:work.txt"], f.repo)).toBe("reviewed");
  });

  test("target branch containing equals still clears its exact mergeOptions key", () => {
    const f = repoFixture();
    must(["git", "branch", "-m", "main", "main=evil"], f.repo);
    must(["git", "config", "branch.main=evil.mergeOptions", "-s ours"], f.repo);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main=evil"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).toBe(0);
    expect(must(["git", "rev-parse", "main=evil^2"], f.repo)).toBe(f.sha);
    expect(must(["git", "show", "main=evil:work.txt"], f.repo)).toBe("reviewed");
  });

  test("merge autostash cannot move main around operator dirty bytes", () => {
    const f = repoFixture();
    writeFileSync(join(f.worktree, "base.txt"), "reviewed base change\n");
    must(["git", "add", "base.txt"], f.worktree);
    must(["git", "commit", "-m", "reviewed base change"], f.worktree);
    const reviewedSHA = must(["git", "rev-parse", "HEAD"], f.worktree);
    writeFileSync(join(f.repo, "base.txt"), "operator dirty change\n");
    must(["git", "config", "merge.autoStash", "true"], f.repo);

    const res = exec(["sh", SCRIPT, "mc/task-1", reviewedSHA, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(readFileSync(join(f.repo, "base.txt"), "utf8")).toBe("operator dirty change\n");
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(reviewedSHA);
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("landing receipt bypasses Worksource commit hooks and hostile identity env", () => {
    const f = repoFixture();
    const hook = join(f.repo, ".git", "hooks", "commit-msg");
    writeFileSync(hook, "#!/bin/sh\nprintf '\\nhook-mutated\\n' >> \"$1\"\n");
    chmodSync(hook, 0o755);
    const hostile = {
      MC_LAND_WORKSPACE: f.repo,
      GIT_AUTHOR_NAME: "untrusted author",
      GIT_AUTHOR_EMAIL: "author@untrusted.invalid",
      GIT_COMMITTER_NAME: "untrusted committer",
      GIT_COMMITTER_EMAIL: "committer@untrusted.invalid",
    };

    const first = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, hostile);
    expect(first.exitCode).toBe(0);
    expect(must(["git", "show", "-s", "--format=%B", "main"], f.repo)).not.toContain("hook-mutated");
    expect(must(["git", "show", "-s", "--format=%an <%ae>", "main"], f.repo))
      .toBe("mission control <mc@mission-control.invalid>");
    expect(must(["git", "show", "-s", "--format=%cn <%ce>", "main"], f.repo))
      .toBe("mission control <mc@mission-control.invalid>");

    const retry = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, hostile);
    expect(retry.exitCode).toBe(0);
    expect(retry.stderr.toString()).toContain("cleanup/report debt");
  });

  test("cleanup ref deletion cannot execute a Worksource reference hook", () => {
    const f = repoFixture();
    const secret = join(f.repo, "operator.secret");
    const hook = join(f.repo, ".git", "hooks", "reference-transaction");
    writeFileSync(join(f.repo, ".git", "info", "exclude"), "operator.secret\n");
    writeFileSync(secret, "operator secret\n");
    writeFileSync(hook, `#!/bin/sh\nrm -f "${secret}"\nexit 0\n`);
    chmodSync(hook, 0o755);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).toBe(0);
    expect(readFileSync(secret, "utf8")).toBe("operator secret\n");
    expect(must(["git", "rev-parse", "main^2"], f.repo)).toBe(f.sha);
    expect(exec(["git", "show-ref", "--verify", "refs/heads/mc/task-1"], f.repo).exitCode)
      .not.toBe(0);
  });

  test("configured core.worktree is refused before Git can target another checkout", () => {
    const f = repoFixture();
    const redirected = join(f.root, "redirected-primary");
    mkdirSync(redirected);
    const secret = join(redirected, "operator.secret");
    writeFileSync(secret, "redirected operator secret\n");
    must(["git", "config", "core.worktree", redirected], f.repo);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("core.worktree");
    expect(readFileSync(secret, "utf8")).toBe("redirected operator secret\n");
    expect(must(["git", "--git-dir", join(f.repo, ".git"), "rev-parse", "main"], f.root))
      .toBe(f.baseSHA);
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("task-worktree core.worktree redirect preserves its real ignored bytes", () => {
    const f = repoFixture();
    const redirected = join(f.root, "redirected-task");
    mkdirSync(redirected);
    writeFileSync(join(f.repo, ".git", "info", "exclude"), ".env\n");
    const secret = join(f.worktree, ".env");
    writeFileSync(secret, "real task-worktree operator secret\n");
    must(["git", "config", "extensions.worktreeConfig", "true"], f.repo);
    must(["git", "config", "--worktree", "core.worktree", redirected], f.worktree);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("task worktree");
    expect(res.stderr.toString()).toContain("core.worktree");
    expect(readFileSync(secret, "utf8")).toBe("real task-worktree operator secret\n");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("mere target ancestry is not accepted as a landing receipt", () => {
    const f = repoFixture();
    must(["git", "worktree", "remove", f.worktree], f.repo);
    must(["git", "merge", "--ff-only", f.sha], f.repo);
    must(["git", "branch", "-d", "mc/task-1"], f.repo);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("missing branch");
    expect(must(["git", "rev-list", "--count", "--merges", `${f.baseSHA}..main`], f.repo)).toBe("0");
  });

  test("fresh landing refuses an already-ancestor SHA instead of deleting its inputs", () => {
    const f = repoFixture();
    must(["git", "merge", "--ff-only", f.sha], f.repo);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("already contains");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.sha);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
    expect(existsSync(f.worktree)).toBe(true);
    expect(must(["git", "rev-list", "--count", "--merges", `${f.baseSHA}..main`], f.repo)).toBe("0");
  });

  test("matching parent topology with the wrong merge tree is not a receipt", () => {
    const f = repoFixture();
    const baseTree = must(["git", "rev-parse", `${f.baseSHA}^{tree}`], f.repo);
    const forged = must([
      "git", "commit-tree", baseTree,
      "-p", f.baseSHA,
      "-p", f.sha,
      "-m", "forged merge omits reviewed bytes",
    ], f.repo);
    must(["git", "update-ref", "refs/heads/main", forged, f.baseSHA], f.repo);
    must(["git", "worktree", "remove", f.worktree], f.repo);
    must(["git", "branch", "-D", "mc/task-1"], f.repo);
    expect(exec(["git", "show", "main:work.txt"], f.repo).exitCode).not.toBe(0);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("missing branch");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(forged);
  });

  test("replacement refs cannot substitute bytes behind the verified SHA", () => {
    const f = repoFixture();
    const maliciousWorktree = join(f.root, "malicious-worktree");
    must(["git", "worktree", "add", "-b", "malicious", maliciousWorktree, f.baseSHA], f.repo);
    writeFileSync(join(maliciousWorktree, "malicious.txt"), "replacement bytes\n");
    must(["git", "add", "malicious.txt"], maliciousWorktree);
    must(["git", "commit", "-m", "malicious replacement"], maliciousWorktree);
    const maliciousSHA = must(["git", "rev-parse", "HEAD"], maliciousWorktree);
    must(["git", "worktree", "remove", maliciousWorktree], f.repo);
    must(["git", "replace", f.sha, maliciousSHA], f.repo);

    // Materialize the replacement view in the task worktree. A landing that
    // honors refs/replace would see this as clean and merge the wrong tree.
    must(["git", "reset", "--hard", f.sha], f.worktree);
    expect(existsSync(join(f.worktree, "malicious.txt"))).toBe(true);
    expect(existsSync(join(f.worktree, "work.txt"))).toBe(false);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("dirty");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(exec(["git", "show", "main:malicious.txt"], f.repo).exitCode).not.toBe(0);
    expect(exec(["git", "show-ref", "--verify", `refs/replace/${f.sha}`], f.repo).exitCode).toBe(0);
  });

  test("receipt recovery preserves a recreated branch that moved", () => {
    const f = repoFixture();
    const first = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(first.exitCode).toBe(0);
    const landedTip = must(["git", "rev-parse", "main"], f.repo);
    must(["git", "branch", "mc/task-1", f.baseSHA], f.repo);

    const retry = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(retry.exitCode).toBe(0);
    expect(retry.stderr.toString()).toContain("now points at");
    expect(retry.stderr.toString()).toContain("preserving it");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(landedTip);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.baseSHA);
  });

  test("cleanup compare-and-delete preserves a ref moved after the SHA fence", () => {
    const f = repoFixture();
    const realGit = Bun.which("git");
    if (realGit === null) throw new Error("git not found");
    const bin = join(f.root, "race-bin");
    must(["mkdir", "-p", bin], f.root);
    const wrapper = join(bin, "git");
    writeFileSync(
      wrapper,
      "#!/bin/sh\n" +
        "case \" $* \" in\n" +
        "*\" update-ref --no-deref -d refs/heads/mc/task-1 \"*)\n" +
        `  ${realGit} update-ref refs/heads/mc/task-1 ${f.baseSHA} ${f.sha}\n` +
        "  ;;\n" +
        "esac\n" +
        `exec ${realGit} \"$@\"\n`,
    );
    chmodSync(wrapper, 0o755);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
      PATH: `${bin}:${process.env.PATH ?? ""}`,
    });
    expect(res.exitCode).toBe(0);
    expect(res.stderr.toString()).toContain("could not delete branch");
    expect(must(["git", "rev-parse", "main^2"], f.repo)).toBe(f.sha);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.baseSHA);
    expect(existsSync(f.worktree)).toBe(false);
  });

  test("fresh landing refuses a symbolic task ref without touching its operator target", () => {
    const f = repoFixture();
    must(["git", "branch", "operator", f.sha], f.repo);
    must(["git", "symbolic-ref", "refs/heads/mc/task-1", "refs/heads/operator"], f.repo);

    const res = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("symbolic ref");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(must(["git", "rev-parse", "operator"], f.repo)).toBe(f.sha);
    expect(must(["git", "symbolic-ref", "refs/heads/mc/task-1"], f.repo)).toBe("refs/heads/operator");
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("fresh landing refuses a branch outside the Mission Control namespace", () => {
    const f = repoFixture();
    must(["git", "branch", "operator-feature", f.sha], f.repo);

    const res = exec(["sh", SCRIPT, "operator-feature", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(res.exitCode).not.toBe(0);
    expect(res.stderr.toString()).toContain("branch namespace");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(f.baseSHA);
    expect(must(["git", "rev-parse", "operator-feature"], f.repo)).toBe(f.sha);
    expect(must(["git", "rev-parse", "mc/task-1"], f.repo)).toBe(f.sha);
    expect(existsSync(f.worktree)).toBe(true);
  });

  test("receipt recovery preserves a symbolic task ref and its operator target", () => {
    const f = repoFixture();
    const first = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(first.exitCode).toBe(0);
    const landedTip = must(["git", "rev-parse", "main"], f.repo);
    must(["git", "branch", "operator", f.sha], f.repo);
    must(["git", "symbolic-ref", "refs/heads/mc/task-1", "refs/heads/operator"], f.repo);

    const retry = exec(["sh", SCRIPT, "mc/task-1", f.sha, "main"], f.repo, {
      MC_LAND_WORKSPACE: f.repo,
    });
    expect(retry.exitCode).toBe(0);
    expect(retry.stderr.toString()).toContain("symbolic ref");
    expect(retry.stderr.toString()).toContain("preserving it");
    expect(must(["git", "rev-parse", "main"], f.repo)).toBe(landedTip);
    expect(must(["git", "rev-parse", "operator"], f.repo)).toBe(f.sha);
    expect(must(["git", "symbolic-ref", "refs/heads/mc/task-1"], f.repo)).toBe("refs/heads/operator");
  });
});
