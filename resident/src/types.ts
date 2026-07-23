// types.ts — the resident's dependency surface, pinned by
// docs/phase1b-contract.md §5: the loop is `startTickLoop(deps)` where
// deps carries {intervalMs, setTimer, clearTimer, runMc, docker, log}.
// `fs` and `config` are the two additions the spawn effector needs
// (folder + run.json writes, §10 effect order); both are injectable so
// `bun test` never touches the real filesystem or Docker.

import type { AcceptedSealIdentity, CompletionSealRequest, PathIdentity, TaskSkeletonRequest } from "./task-skeleton";

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
  /** Command run inside the Homie container (the homie runner). Falls back to
   * agentCmd when unset so the fake e2e image can share one entrypoint. */
  homieCmd?: string[];
  /** Command run inside the land container (the baked `mc-land` script). */
  landCmd: string[];
  /** Host dir of behavior fixtures, mounted RO at /mc/behaviors. */
  behaviorsDir: string;
  /** Host dir of runner source, mounted RO at /app/src (§11.2: never baked). */
  runnerSrcDir: string;
  /** Host workspace root, mounted at /workspace/source (§6.2 canonical path). */
  workspaceRoot: string;
  /** Closed production Worksource catalog for Homie's operator-wide read
   * scope. The legacy single root remains the /workspace/source alias. */
  workspaceRoots?: Array<{ id: string; root: string }>;
  /** Docker named volume holding the spine (Inv. 24). */
  spineVolume: string;
  /** In-container spine db file path; volume mounts at its dirname; exported
   * as MC_SPINE for the runner's env interpolation (contract §4). */
  spineDbPath: string;
  /** role → in-container behavior file path (contract §6 / deviation D-4:
   * role→behavior mapping lives in the resident's e2e config). */
  roleBehaviors: Record<string, string>;
  /** Explicit test/development `harness/model_binding` routes that use the
   * fake adapter as a stand-in. Production leaves this empty: the runner's
   * closed Codex/Claude/MiniMax catalog selects real adapters intrinsically. */
  agentRunnerRoutes?: string[];
}

/** The ADR-022 credential projection seam. The projector owns the binding
 * catalog's channel knowledge and the token-service state; the effector stays
 * format-blind. `null` applies only to fake/fake and proceeds token-free;
 * `refused` means a required
 * credential is unavailable and the spawn must not happen (D8: stall, never
 * launch uncredentialed). */
export interface CredentialProjection {
  /** Env entries merged into the agent container (flag channel, overrides). */
  env: Record<string, string>;
  /** A projected Codex auth.json body, bound RW at CODEX_HOME/auth.json. */
  authJson?: string;
}

export interface CredentialProjector {
  project(
    harness: string,
    modelBinding: string,
  ): CredentialProjection | { refused: string } | null;
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
  /** Durable liveness receipt after one dispatch result was parsed and its
   * effect completed. Activation waits for this release-bound proof. */
  tickComplete?: () => Promise<void>;
  /** Serialized resident chore run before dispatch. A failure skips dispatch
   * for this tick so required durability work retries before any new write. */
  beforeTick?: () => Promise<void>;
  fs: {
    /** mkdir -p semantics by default; exclusive is for a newly derived,
     * run-keyed disposable root that must not adopt existing bytes. */
    mkdir(path: string, opts?: { exclusive?: boolean }): Promise<void>;
    /** opts.mode applies on creation (the plan sibling must be 0600: the
     * recheck's trust seam refuses a group/other-readable plan file). */
    writeFile(path: string, data: string, opts?: { mode?: number }): Promise<void>;
    /** rm -f semantics (missing file is not an error). `recursive` is reserved
     * for a resident-derived disposable directory under MC_HOME/runs. */
    rm(path: string, opts?: { recursive?: boolean }): Promise<void>;
  };
	/** Exclusive post-claim task-root materializer (ADR-016 D5). */
	precreateTaskSkeleton(request: TaskSkeletonRequest): Promise<PathIdentity>;
	/** Descriptor-relative exact-empty of an existing receipt-vouched root. */
	recoverTaskSkeleton(request: TaskSkeletonRequest): Promise<PathIdentity>;
	/** Repeats parent identity, owner-only mode, and native ACL trust. */
	recheckTaskParent(request: TaskSkeletonRequest): Promise<void>;
	/** Repeats completion-seal parent trust plus expected absent/ready root shape. */
	recheckCompletionSeal(step: CompletionSealRequest, state: "absent" | "ready"): Promise<void>;
	/** Creates the exact run-keyed completion staging root after an absent recheck. */
	precreateCompletionSeal(mcHome: string, step: CompletionSealRequest): Promise<PathIdentity>;
	/** Repeats the exact accepted seal root identity before trusted setup binds it. */
	recheckAcceptedSeal(seal: AcceptedSealIdentity): Promise<string>;
	/** Durably registers the exact returned identity before any setup consumer runs. */
	registerTaskRoot(runId: string, taskId: number, identity: PathIdentity): Promise<void>;
  /** ADR-022 credential projection for real agent routes. An absent
   * projector refuses every non-fake launch. */
  credentials?: CredentialProjector;
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
	completion_seal?: CompletionSealRequest;
	accepted_seal_rebuild?: {
		closure_digest: string;
		completion_request_id: string;
		device: string;
		inode: string;
		manifest_digest: string;
		object_format: "sha1" | "sha256";
		owner_uid: number;
		run_id: string;
		sealed_sha: string;
		seal_device: string;
		seal_inode: string;
		seal_owner_uid: number;
		task_id: number;
	};
	verifier_projection?: {
		closure_digest: string;
		completion_request_id: string;
		manifest_digest: string;
		object_format: "sha1" | "sha256";
		rebuild_run_id: string;
		seal_device: string;
		seal_inode: string;
		seal_owner_uid: number;
		sealed_sha: string;
		task_id: number;
	};
	/** The sealed landing instruction (ADR-017:699-702), mirroring Go's
	 * `PrivateDispatchLanding`. It is a SIBLING of `entries`, not a member of
	 * it, and a landing plan carries no entries at all: the landing table lives
	 * on the `/repo` plane, while `invalidMountPlanReason` admits only
	 * `/workspace` destinations. So the four landing rows are composed by the
	 * resident from this instruction rather than authorized as binds.
	 *
	 * The two host-backed anchors arrive as identities; the cover and the
	 * envelope are generated per run and have none. `cover_dest` is an
	 * obligation, not decoration — without the generated empty RO cover the
	 * sealed task bytes are reachable through the RW `/repo/source` alias,
	 * which is the single thing ADR-017:700 exists to prevent. */
	landing?: {
		/** The accepted Worker seal's run, for ADR-016:846's label. Carried for
		 * discovery, never matched against — and deliberately NOT surfaced as
		 * `mc-run-id`, which everywhere else means "the run this container IS". */
		approved_run_id: string;
		branch: string;
		closure_digest: string;
		cover_dest: string;
		landing_id: string;
		local_repo_uuid: string;
		object_format: "sha1" | "sha256";
		pinned_base_sha: string;
		pre_merge_sha: string;
		target_ref: string;
		task_id: number;
		task_root: PathIdentity;
		verified_sha: string;
		worksource_root: PathIdentity;
	};
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
      /** The SEALED lane's carrier. Present only on a sealed landing, absent on
       * the legacy branch-carrying one — its presence IS the lane
       * discriminator, which is why it is optional rather than nullable and why
       * the five keys above must stay identical between the two producers. */
      landing?: NonNullable<MountPlan["landing"]>;
    }
  | { action: "reap"; run_id: string; stop_container?: boolean }
	| { action: "interrupt"; task_id: number; run_id: string; stop_container: true }
  | { action: "reenter"; task_id: number }
  // The lease-free Homie tier (Inv. 1/22, §15.3). A wake carries no mount plan
  // and no heartbeat interval: the resident binds a fixed operator-read-scope
  // set and the runner never heartbeats. run_id == session (the "h-" id).
  | {
      action: "homie-wake";
      session: string;
      /** 16-hex launch generation the dispatch tick persisted (ADR-016 D3). */
      launch: string;
      mode: "fresh" | "native" | "rows";
      /** Harness the session speaks (e.g. "claude"); drives the runner. */
      binding: string;
      /** Container name the session is pinned to (mc-homie-<session>). */
      container_name: string;
    }
  | { action: "homie-stop"; session: string; container_id?: string; reason?: string };
