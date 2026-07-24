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

/** The immutable host-object portion of a completion receipt.  The resident
 * repeats this check immediately before a trusted setup container receives
 * the seal bind; the setup executor repeats it again inside that container. */
export interface AcceptedSealIdentity {
	device: string;
	inode: string;
	owner_uid: number;
	run_id: string;
}

/** Producer-side, absent-derived completion-root authority. */
export interface CompletionSealRequest {
	run_id: string;
	task_id: number;
	seals_parent: PathIdentity;
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
  /** Existing receipt-vouched root to exact-empty before fresh setup. */
  recover_root?: PathIdentity;
}

/** The ADR-025 D3 initiative shared-store precreate request. Unlike a task it has
 * TWO proven parents on separate host bases (D1): the store parent
 * `.mission-control/initiatives` and the worktree parent `.mc-worktrees`. */
export interface InitiativeSkeletonRequest {
  workspace_root: string;
  initiative_id: number;
  child_mode: number;
  setup: TaskSetupInstruction;
  store_parent: PathIdentity;
  worktree_parent: PathIdentity;
  recover_store?: PathIdentity;
  recover_worktree?: PathIdentity;
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

/** Re-attest the exact run-keyed completion seal before it can be bound into
 * accepted-seal setup.  `MC_HOME/seals/<run>` is derived locally rather than
 * supplied by dispatch, so a hostile effect cannot choose another host path.
 *
 * The receipt's device/inode/owner are deliberately NOT compared here. They are
 * recorded by the in-container setuid publisher and are namespace-local (uid
 * 10001, container device numbers), so a host lstat can never equal them on
 * Docker Desktop; comparing made every non-fake rebuild refuse. What survives is
 * custody, not identity: the path is derived, must resolve to itself, and must
 * be a non-symlink directory owned by the host operator this resident runs as.
 * Seal integrity remains the in-image immutable manifest/pack digest
 * verification (`verifyAcceptedSealIdentity` + the manifest digest bound in
 * `completion_seals`). Logged deviation, IMPLEMENTATION-NOTES 2026-07-19. */
export async function recheckAcceptedSeal(
	mcHome: string,
	seal: AcceptedSealIdentity,
): Promise<string> {
	if (!isAbsolute(mcHome) || normalize(mcHome) !== mcHome ||
		!(/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/).test(seal.run_id) ||
		!validDecimal(seal.device) || !validDecimal(seal.inode) ||
		!Number.isSafeInteger(seal.owner_uid) || seal.owner_uid < 0) {
		throw new Error("accepted seal receipt identity is malformed");
	}
	const root = join(mcHome, "seals", seal.run_id);
	if (await realpath(root) !== root) {
		throw new Error("accepted seal root is not its exact canonical run path");
	}
	const stat = await lstat(root, { bigint: true });
	if (!stat.isDirectory() || stat.isSymbolicLink()) {
		throw new Error("accepted seal root is not a non-symlink directory");
	}
	const operatorUID = typeof process.getuid === "function" ? process.getuid() : -1;
	if (operatorUID < 0 || Number(stat.uid) !== operatorUID) {
		throw new Error("accepted seal root is not owned by the host operator");
	}
	return root;
}

// Creates only the run-keyed child after the host helper has rechecked its
// parent and absence. The child is derived here, never accepted as an effect
// path, and is fixed back to private mode before it can be bound.
export async function precreateCompletionSeal(
	mcHome: string,
	step: CompletionSealRequest,
): Promise<PathIdentity> {
	if (!isAbsolute(mcHome) || normalize(mcHome) !== mcHome ||
		!(/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/).test(step.run_id) ||
		!Number.isSafeInteger(step.task_id) || step.task_id < 1 ||
		step.seals_parent.canonical !== join(mcHome, "seals") ||
		!validDecimal(step.seals_parent.device) || !validDecimal(step.seals_parent.inode) ||
		!Number.isSafeInteger(step.seals_parent.owner_uid) || step.seals_parent.owner_uid < 0) {
		throw new Error("completion seal precreate descriptor is malformed");
	}
	const root = join(step.seals_parent.canonical, step.run_id);
	await mkdir(root, { mode: 0o700 });
	await chmod(root, 0o700);
	const identity = await registeredIdentity(root);
	if (identity.owner_uid !== step.seals_parent.owner_uid) {
		throw new Error("completion seal root owner differs from its attested parent");
	}
	return identity;
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
	if (request.recover_root !== undefined) {
		throw new Error("recovery skeleton must be exact-emptied by the host helper");
	}
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

/** Prove one operator-owned, mode-0700 initiative precreate parent. */
async function proveInitiativeParent(expected: string, parent: PathIdentity, operatorUID: number): Promise<void> {
  if (!validDecimal(parent.device) || !validDecimal(parent.inode) ||
      !Number.isSafeInteger(parent.owner_uid) || parent.owner_uid < 0) {
    throw new Error("initiative precreate parent identity is malformed");
  }
  if (parent.canonical !== expected || await realpath(expected) !== expected) {
    throw new Error(`initiative precreate parent is not the exact canonical ${expected} path`);
  }
  const stat = await lstat(expected, { bigint: true });
  if (!stat.isDirectory() || stat.isSymbolicLink() || !sameIdentity(stat, parent) ||
      Number(stat.mode & 0o777n) !== 0o700) {
    throw new Error("initiative precreate parent identity changed after preclaim");
  }
  if (parent.owner_uid !== operatorUID) {
    throw new Error("initiative precreate parent is not owned by the host operator");
  }
}

/** Create the sanitized store root `initiative-<id>/{git,source}`, fixed 0555 —
 * the same discipline as the task root. */
async function createInitiativeStoreRoot(root: string, childMode: number, operatorUID: number): Promise<PathIdentity> {
  await requireAbsent(root);
  await mkdir(root, { mode: 0o700 });
  const created = await registeredIdentity(root);
  if (created.owner_uid !== operatorUID) {
    throw new Error("created initiative store root is not owned by the host operator");
  }
  for (const child of ["source", "git"]) {
    if (!sameIdentity(await lstat(root, { bigint: true }), created)) {
      throw new Error("created initiative store root identity changed during precreate");
    }
    const path = join(root, child);
    await mkdir(path, { mode: childMode });
    await chmod(path, childMode);
    const stat = await lstat(path, { bigint: true });
    if (!stat.isDirectory() || stat.isSymbolicLink() || Number(stat.uid) !== operatorUID ||
        Number(stat.mode & 0o777n) !== childMode) {
      throw new Error(`created initiative store child ${child} does not match its canary-proved shape`);
    }
  }
  const entries = (await readdir(root)).sort();
  if (entries.length !== 2 || entries[0] !== "git" || entries[1] !== "source" ||
      !sameIdentity(await lstat(root, { bigint: true }), created)) {
    throw new Error("created initiative store root gained unexpected bytes or changed identity");
  }
  await chmod(root, 0o555);
  const finalStat = await lstat(root, { bigint: true });
  if (!sameIdentity(finalStat, created) || Number(finalStat.mode & 0o777n) !== 0o555) {
    throw new Error("created initiative store root did not retain its registered mode-0555 identity");
  }
  return created;
}

/** Create the shared worktree root `initiative-<id>`: an empty operator-owned
 * mode-0700 directory the setup container populates (ADR-025 D1/D3). */
async function createInitiativeWorktreeRoot(root: string, operatorUID: number): Promise<PathIdentity> {
  await requireAbsent(root);
  await mkdir(root, { mode: 0o700 });
  await chmod(root, 0o700);
  const created = await registeredIdentity(root);
  const stat = await lstat(root, { bigint: true });
  if (!stat.isDirectory() || stat.isSymbolicLink() || created.owner_uid !== operatorUID ||
      Number(stat.mode & 0o777n) !== 0o700 || (await readdir(root)).length !== 0) {
    throw new Error("created shared worktree root is not the empty operator-owned mode-0700 directory");
  }
  return created;
}

/**
 * Exclusively precreate the ADR-025 D3 initiative skeleton: the sanitized store
 * root `initiative-<id>/{git,source}` (fixed 0555) under the store parent, and
 * the shared worktree root `initiative-<id>` (empty, 0700; the container
 * populates it) under the worktree parent — on SEPARATE host bases (D1). Every
 * path op is followed by an identity/shape check; residue is left in place on a
 * partial failure (cleanup without the registered identity could erase operator
 * bytes). Recovery over residue is the host helper's job, not this primitive's.
 */
export async function precreateInitiativeSkeleton(
  request: InitiativeSkeletonRequest,
): Promise<{ store: PathIdentity; worktree: PathIdentity }> {
  if (request.recover_store !== undefined || request.recover_worktree !== undefined) {
    throw new Error("recovery initiative skeleton must be exact-emptied by the host helper");
  }
  if (!Number.isSafeInteger(request.initiative_id) || request.initiative_id < 1) {
    throw new Error("initiative_id must be a canonical positive safe integer");
  }
  if (request.child_mode !== 0o700) {
    throw new Error("child_mode must be the closed mode 0700 supplied to the final-uid canary");
  }
  if (!isAbsolute(request.workspace_root) || normalize(request.workspace_root) !== request.workspace_root) {
    throw new Error("workspace_root must be an absolute normalized canonical path");
  }
  const operatorUID = typeof process.getuid === "function" ? process.getuid() : -1;
  if (operatorUID < 0) {
    throw new Error("resident cannot determine the host operator uid");
  }

  const storeParentPath = join(request.workspace_root, ".mission-control", "initiatives");
  const worktreeParentPath = join(request.workspace_root, ".mc-worktrees");
  await proveInitiativeParent(storeParentPath, request.store_parent, operatorUID);
  await proveInitiativeParent(worktreeParentPath, request.worktree_parent, operatorUID);

  const store = await createInitiativeStoreRoot(join(storeParentPath, `initiative-${request.initiative_id}`), request.child_mode, operatorUID);
  const worktree = await createInitiativeWorktreeRoot(join(worktreeParentPath, `initiative-${request.initiative_id}`), operatorUID);
  return { store, worktree };
}
