import { chmod, lstat, mkdir, mkdtemp, readFile, realpath, rm, symlink, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { afterEach, describe, expect, test } from "bun:test";
import { precreateTaskSkeleton, recheckAcceptedSeal, type PathIdentity } from "./task-skeleton";

const homes: string[] = [];

afterEach(async () => {
  for (const path of homes.splice(0)) {
    await chmod(join(path, "workspace", ".mission-control", "tasks", "task-7"), 0o700).catch(() => {});
    await rm(path, { recursive: true, force: true });
  }
});

async function fixture(): Promise<{ workspace: string; tasks: string; identity: PathIdentity }> {
  const root = await realpath(await mkdtemp(join(tmpdir(), "mc-task-skeleton-")));
  homes.push(root);
  const workspace = join(root, "workspace");
  const tasks = join(workspace, ".mission-control", "tasks");
  await mkdir(tasks, { recursive: true, mode: 0o700 });
  const canonical = await realpath(tasks);
  const stat = await lstat(canonical, { bigint: true });
  return {
    workspace,
    tasks,
    identity: {
      canonical,
      device: stat.dev.toString(10),
      inode: stat.ino.toString(10),
      owner_uid: Number(stat.uid),
    },
  };
}

describe("resident task-skeleton precreate (ADR-017:437-441)", () => {
  test("creates children first, fixes the parent 0555, and returns its registered identity", async () => {
    const f = await fixture();
    const result = await precreateTaskSkeleton({
      workspace_root: f.workspace,
      task_id: 7,
      child_mode: 0o700,
      tasks_parent: f.identity,
    });
    const taskRoot = join(f.tasks, "task-7");
    expect(result.canonical).toBe(taskRoot);
    const rootStat = await lstat(taskRoot, { bigint: true });
    expect(Number(rootStat.mode & 0o777n)).toBe(0o555);
    expect(result.device).toBe(rootStat.dev.toString(10));
    expect(result.inode).toBe(rootStat.ino.toString(10));
    for (const child of ["source", "git"]) {
      const stat = await lstat(join(taskRoot, child), { bigint: true });
      expect(stat.isDirectory()).toBe(true);
      expect(Number(stat.mode & 0o777n)).toBe(0o700);
    }
  });

  test("refuses an existing task identity without touching its bytes", async () => {
    const f = await fixture();
    const taskRoot = join(f.tasks, "task-7");
    await mkdir(taskRoot, { mode: 0o700 });
    const sentinel = join(taskRoot, "operator.txt");
    await writeFile(sentinel, "keep me\n", { mode: 0o600 });
    await expect(precreateTaskSkeleton({
      workspace_root: f.workspace,
      task_id: 7,
      child_mode: 0o700,
      tasks_parent: f.identity,
    })).rejects.toThrow("already exists");
    expect(await readFile(sentinel, "utf8")).toBe("keep me\n");
  });

  test("refuses parent identity drift before creating task bytes", async () => {
    const f = await fixture();
    const drifted = { ...f.identity, inode: (BigInt(f.identity.inode) + 1n).toString(10) };
    await expect(precreateTaskSkeleton({
      workspace_root: f.workspace,
      task_id: 7,
      child_mode: 0o700,
      tasks_parent: drifted,
    })).rejects.toThrow("parent identity changed");
    await expect(lstat(join(f.tasks, "task-7"))).rejects.toThrow();
  });

  test("refuses bytes raced into a child before the empty skeleton is registered", async () => {
    const f = await fixture();
    const injected = join(f.tasks, "task-7", "source", "injected");
    const inject = async (): Promise<void> => {
      for (;;) {
        try {
          await writeFile(injected, "not empty\n", { mode: 0o600 });
          return;
        } catch (err) {
          if ((err as { code?: string }).code !== "ENOENT") throw err;
        }
      }
    };
    await expect(Promise.all([
      precreateTaskSkeleton({
        workspace_root: f.workspace,
        task_id: 7,
        child_mode: 0o700,
        tasks_parent: f.identity,
      }),
      inject(),
    ])).rejects.toThrow("child source changed or is not empty");
  });

  test("refuses noncanonical ids and every child mode except closed 0700", async () => {
    const f = await fixture();
    for (const task_id of [0, -1, 1.5]) {
      await expect(precreateTaskSkeleton({
        workspace_root: f.workspace,
        task_id,
        child_mode: 0o700,
        tasks_parent: f.identity,
      })).rejects.toThrow("task_id");
    }
    await expect(precreateTaskSkeleton({
      workspace_root: f.workspace,
      task_id: 7,
      child_mode: 0o500,
      tasks_parent: f.identity,
    })).rejects.toThrow("child_mode");
	await expect(precreateTaskSkeleton({
		workspace_root: f.workspace,
		task_id: 7,
		child_mode: 0o777,
		tasks_parent: f.identity,
	})).rejects.toThrow("child_mode");
  });
});

describe("resident accepted completion seal recheck", () => {
	// The recorded receipt identity is written by the in-container setuid
	// publisher and is namespace-local, so it is NOT comparable to a host lstat
	// (IMPLEMENTATION-NOTES 2026-07-19). This side proves host-operator custody
	// of the locally derived path; the seal's integrity is the in-image
	// manifest/pack digest verification.
	test("derives only MC_HOME/seals/<run> under host-operator custody", async () => {
		const f = await fixture();
		const seal = join(f.workspace, "seals", "run-7-worker");
		await mkdir(seal, { recursive: true, mode: 0o700 });
		const stat = await lstat(seal, { bigint: true });
		await expect(recheckAcceptedSeal(f.workspace, {
			run_id: "run-7-worker", device: stat.dev.toString(10), inode: stat.ino.toString(10), owner_uid: Number(stat.uid),
		})).resolves.toBe(seal);
	});

	test("accepts the namespace-local recorded identity it can no longer compare", async () => {
		const f = await fixture();
		const seal = join(f.workspace, "seals", "run-7-worker");
		await mkdir(seal, { recursive: true, mode: 0o700 });
		await expect(recheckAcceptedSeal(f.workspace, {
			run_id: "run-7-worker", device: "48", inode: "1234567", owner_uid: 10001,
		})).resolves.toBe(seal);
	});

	test("still refuses malformed receipts, escaping run ids, and non-directories", async () => {
		const f = await fixture();
		await mkdir(join(f.workspace, "seals"), { recursive: true, mode: 0o700 });
		for (const run_id of ["../escape", "run/7", ".hidden", ""]) {
			await expect(recheckAcceptedSeal(f.workspace, {
				run_id, device: "1", inode: "2", owner_uid: 501,
			})).rejects.toThrow("malformed");
		}
		await expect(recheckAcceptedSeal(f.workspace, {
			run_id: "run-7-worker", device: "01", inode: "2", owner_uid: 501,
		})).rejects.toThrow("malformed");
		await expect(recheckAcceptedSeal(f.workspace, {
			run_id: "run-7-worker", device: "1", inode: "2", owner_uid: -1,
		})).rejects.toThrow("malformed");
		await writeFile(join(f.workspace, "seals", "run-8-worker"), "not a directory\n", { mode: 0o600 });
		await expect(recheckAcceptedSeal(f.workspace, {
			run_id: "run-8-worker", device: "1", inode: "2", owner_uid: 501,
		})).rejects.toThrow("non-symlink directory");
	});

	test("refuses a seal root that is not the exact canonical run path", async () => {
		const f = await fixture();
		const seals = join(f.workspace, "seals");
		await mkdir(join(seals, "elsewhere"), { recursive: true, mode: 0o700 });
		await symlink(join(seals, "elsewhere"), join(seals, "run-9-worker"));
		await expect(recheckAcceptedSeal(f.workspace, {
			run_id: "run-9-worker", device: "1", inode: "2", owner_uid: 501,
		})).rejects.toThrow("exact canonical run path");
	});
});
