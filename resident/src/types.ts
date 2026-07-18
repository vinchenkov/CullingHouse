// types.ts — the resident's dependency surface, pinned by
// docs/phase1b-contract.md §5: the loop is `startTickLoop(deps)` where
// deps carries {intervalMs, setTimer, clearTimer, runMc, docker, log}.
// `fs` and `config` are the two additions the spawn effector needs
// (folder + run.json writes, §10 effect order); both are injectable so
// `bun test` never touches the real filesystem or Docker.

import type { PathIdentity, TaskSkeletonRequest } from "./task-skeleton";

/** Result of running an external command (mc or docker). */
export interface ExecResult {
  exitCode: number;
  stdout: string;
  stderr: string;
}

/** Runs a command with the given argv (binary path already bound). */
export type Exec = (argv: string[]) => Promise<ExecResult>;

/**
 * Static wiring the effectors need. Everything here is decided by whoever
 * spawns the resident (the e2e's test config, contract §6/A5); the loop
 * itself computes nothing from it (spec §15.1: the resident is "the
 * mechanical hand of a dispatch decision already committed in the spine").
 */
export interface ResidentConfig {
  /** Host MC_HOME; sessions live at `<mcHome>/sessions/<run_id>` (§16.1). */
  mcHome: string;
  /** The baked e2e image (contract §1, `mc-fake-e2e`). */
  image: string;
  /** Command run inside the agent container (the skeleton agent runner). */
  agentCmd: string[];
  /** Command run inside the land container (the baked `mc-land` script). */
  landCmd: string[];
  /** Host dir of behavior fixtures, mounted RO at /mc/behaviors. */
  behaviorsDir: string;
  /** Host dir of runner source, mounted RO at /app/src (§11.2: never baked). */
  runnerSrcDir: string;
  /** Host workspace root, mounted at /workspace/source (§6.2 canonical path). */
  workspaceRoot: string;
  /** Docker named volume holding the spine (Inv. 24). */
  spineVolume: string;
  /** In-container spine db file path; volume mounts at its dirname; exported
   * as MC_SPINE for the runner's env interpolation (contract §4). */
  spineDbPath: string;
  /** role → in-container behavior file path (contract §6 / deviation D-4:
   * role→behavior mapping lives in the resident's e2e config). */
  roleBehaviors: Record<string, string>;
}

/** The injectable dependency bundle for `startTickLoop` (contract §5). */
export interface TickDeps {
  intervalMs: number;
  /** setInterval-shaped: fn fires every ms until cleared. */
  setTimer: (fn: () => unknown, ms: number) => unknown;
  clearTimer: (handle: unknown) => void;
  /** Invokes the host mc binary (self-delegating, contract §1). */
  runMc: Exec;
  /** Invokes the docker CLI. */
  docker: Exec;
  log: (msg: string) => void;
  fs: {
    /** mkdir -p semantics. */
    mkdir(path: string): Promise<void>;
    /** opts.mode applies on creation (the plan sibling must be 0600: the
     * recheck's trust seam refuses a group/other-readable plan file). */
    writeFile(path: string, data: string, opts?: { mode?: number }): Promise<void>;
    /** rm -f semantics (missing file is not an error). */
    rm(path: string): Promise<void>;
  };
	/** Exclusive post-claim task-root materializer (ADR-016 D5). */
	precreateTaskSkeleton(request: TaskSkeletonRequest): Promise<PathIdentity>;
	/** Descriptor-relative exact-empty of an existing receipt-vouched root. */
	recoverTaskSkeleton(request: TaskSkeletonRequest): Promise<PathIdentity>;
	/** Repeats parent identity, owner-only mode, and native ACL trust. */
	recheckTaskParent(request: TaskSkeletonRequest): Promise<void>;
	/** Durably registers the exact returned identity before any setup consumer runs. */
	registerTaskRoot(runId: string, taskId: number, identity: PathIdentity): Promise<void>;
  config: ResidentConfig;
}

/** One authorized bind of the committed mount plan — mc's closed carrier
 * (ADR-016 D5/D6). The resident consumes only source/destination/access;
 * device/inode/kind/mode/owner are host evidence it carries opaquely to the
 * `mc __mount-recheck` legs. Keys are alphabetical by contract. */
export interface MountPlanEntry {
  access: "ro" | "rw";
  destination: string;
  device: string;
  inode: string;
  kind: "dir" | "file";
  logical_id: string;
  mode: number;
  owner_uid: number;
  source: string;
}

/** The committed spawn effect's validated mount plan; explicit, never absent. */
export interface MountPlan {
  entries: MountPlanEntry[];
	task_precreate?: TaskSkeletonRequest;
  version: number;
}

/** `mc dispatch` effect JSON, mirroring dispatch.Action (contract §2). */
export type Effect =
  | { action: "idle"; reason?: string }
  | {
      action: "spawn";
      run_id: string;
      role: string;
      subject_id: number | null;
      worksource: string | null;
      pool_ids: number[];
      harness: string;
      model_binding: string;
	      /** Immutable opening input rendered inside the dispatch transaction. */
	      brief: string;
      /** The only mount authority the resident may effect (ADR-016 D5). */
      mount_plan: MountPlan;
      session_path?: string;
      heartbeat_interval_s: number;
    }
  | {
      action: "land";
      task_id: number;
      branch: string;
      verified_sha: string;
      target_ref: string;
    }
  | { action: "reap"; run_id: string; stop_container?: boolean }
	| { action: "interrupt"; task_id: number; run_id: string; stop_container: true }
  | { action: "reenter"; task_id: number };
