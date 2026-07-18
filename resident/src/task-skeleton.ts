// ADR-017 D5's resident-owned first standalone-task effect. The caller is the
// post-claim effector and supplies the parent identity captured by preclaim;
// this primitive creates no sibling authority, invokes no Git, and never
// repairs or removes an unexpected existing path.

import { chmod, lstat, mkdir, readdir, realpath } from "node:fs/promises";
import { isAbsolute, join, normalize } from "node:path";

export interface PathIdentity {
  canonical: string;
  device: string;
  inode: string;
  owner_uid: number;
}

/** The plan's first-task setup instruction (ADR-016 D5): everything the
 * spine-blind resident may know when it writes /mc/setup.json. Fresh mode
 * carries the frozen target ref and no pins; retry mode carries the recorded
 * assignment pins and no target. */
export interface TaskSetupInstruction {
  mode: "fresh" | "retry";
  object_format: string;
  pinned_base_sha?: string;
  pinned_closure_digest?: string;
  pinned_local_repo_uuid?: string;
  target_ref?: string;
}

export interface TaskSkeletonRequest {
  workspace_root: string;
  task_id: number;
  /** Final host mode selected by the mandatory final-uid canary. */
  child_mode: number;
  /** The committed setup instruction the envelope restates. */
  setup: TaskSetupInstruction;
  /** Exact `.mission-control/tasks` identity attested before claim. */
  tasks_parent: PathIdentity;
}

function decimal(value: bigint): string {
  return value.toString(10);
}

function validDecimal(value: string): boolean {
  return /^(0|[1-9][0-9]*)$/.test(value);
}

function sameIdentity(
  stat: Awaited<ReturnType<typeof lstat>>,
  expected: PathIdentity,
): boolean {
  return decimal(BigInt(stat.dev)) === expected.device &&
    decimal(BigInt(stat.ino)) === expected.inode &&
    Number(stat.uid) === expected.owner_uid;
}

async function registeredIdentity(path: string): Promise<PathIdentity> {
  const stat = await lstat(path, { bigint: true });
  return {
    canonical: path,
    device: decimal(stat.dev),
    inode: decimal(stat.ino),
    owner_uid: Number(stat.uid),
  };
}

async function requireAbsent(path: string): Promise<void> {
  try {
    await lstat(path);
  } catch (err) {
    if ((err as { code?: string }).code === "ENOENT") return;
    throw err;
  }
  throw new Error(`task skeleton ${path} already exists; retry requires its separately registered identity`);
}

/**
 * Exclusively precreate one empty `task-<id>/{source,git}` skeleton.
 *
 * The root begins private while its two children are created, then is fixed
 * to 0555. Every path operation is followed by an identity/shape check. On a
 * partial failure residue is deliberately left in place: cleanup without the
 * registered identity could erase bytes created by a racing operator.
 */
export async function precreateTaskSkeleton(request: TaskSkeletonRequest): Promise<PathIdentity> {
  if (!Number.isSafeInteger(request.task_id) || request.task_id < 1) {
    throw new Error("task_id must be a canonical positive safe integer");
  }
	if (request.child_mode !== 0o700) {
		throw new Error("child_mode must be the closed mode 0700 supplied to the final-uid canary");
  }
  if (!isAbsolute(request.workspace_root) || normalize(request.workspace_root) !== request.workspace_root) {
    throw new Error("workspace_root must be an absolute normalized canonical path");
  }
  if (!validDecimal(request.tasks_parent.device) || !validDecimal(request.tasks_parent.inode) ||
      !Number.isSafeInteger(request.tasks_parent.owner_uid) || request.tasks_parent.owner_uid < 0) {
    throw new Error("tasks_parent identity is malformed");
  }

  const tasksParent = join(request.workspace_root, ".mission-control", "tasks");
  if (request.tasks_parent.canonical !== tasksParent || await realpath(tasksParent) !== tasksParent) {
    throw new Error("task parent is not the exact canonical .mission-control/tasks path");
  }
  const parentStat = await lstat(tasksParent, { bigint: true });
	if (!parentStat.isDirectory() || parentStat.isSymbolicLink() || !sameIdentity(parentStat, request.tasks_parent) ||
		Number(parentStat.mode & 0o777n) !== 0o700) {
    throw new Error("task parent identity changed after preclaim");
  }
  const operatorUID = typeof process.getuid === "function" ? process.getuid() : -1;
  if (operatorUID < 0 || request.tasks_parent.owner_uid !== operatorUID) {
    throw new Error("task parent is not owned by the host operator");
  }

  const root = join(tasksParent, `task-${request.task_id}`);
  await requireAbsent(root);
  await mkdir(root, { mode: 0o700 });
  const created = await registeredIdentity(root);
  if (created.owner_uid !== operatorUID) {
    throw new Error("created task root is not owned by the host operator");
  }

  const children = new Map<string, PathIdentity>();
  for (const child of ["source", "git"]) {
    if (!sameIdentity(await lstat(root, { bigint: true }), created)) {
      throw new Error("created task root identity changed during precreate");
    }
    const path = join(root, child);
    await mkdir(path, { mode: request.child_mode });
    await chmod(path, request.child_mode);
    const stat = await lstat(path, { bigint: true });
    if (!stat.isDirectory() || stat.isSymbolicLink() || Number(stat.uid) !== operatorUID ||
        Number(stat.mode & 0o777n) !== request.child_mode) {
      throw new Error(`created task child ${child} does not match its canary-proved shape`);
    }
    children.set(child, {
      canonical: path,
      device: decimal(stat.dev),
      inode: decimal(stat.ino),
      owner_uid: Number(stat.uid),
    });
  }

  const entries = (await readdir(root)).sort();
  if (entries.length !== 2 || entries[0] !== "git" || entries[1] !== "source" ||
      !sameIdentity(await lstat(root, { bigint: true }), created)) {
    throw new Error("created task root gained unexpected bytes or changed identity");
  }
  for (const child of ["source", "git"]) {
    const path = join(root, child);
    const stat = await lstat(path, { bigint: true });
    const identity = children.get(child)!;
    if (!stat.isDirectory() || stat.isSymbolicLink() || !sameIdentity(stat, identity) ||
        Number(stat.mode & 0o777n) !== request.child_mode || (await readdir(path)).length !== 0) {
      throw new Error(`created task child ${child} changed or is not empty`);
    }
  }
  await chmod(root, 0o555);
  const final = await lstat(root, { bigint: true });
  if (!sameIdentity(final, created) || !final.isDirectory() || final.isSymbolicLink() ||
      Number(final.mode & 0o777n) !== 0o555) {
    throw new Error("created task root did not retain its registered mode-0555 identity");
  }
  return created;
}
