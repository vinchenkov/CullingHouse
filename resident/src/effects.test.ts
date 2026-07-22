// effects.test.ts — the effectors against stubbed mc/docker/fs
// (docs/phase1b-contract.md §5): spawn's folder → run.json → container
// order, run.json schema (§6), docker argv (§1 rows), the land →
// `mc land report` handoff, exact-name reap, and idle/reenter as no-ops.

import { describe, expect, test } from "bun:test";
import { applyEffect, requireAcceptedSealProducerAbsent, runSealedLanding } from "./effects";
import { fail, makeRig, ok, testConfig } from "./test-helpers";
import type { Effect, MountPlan } from "./types";

const workspaceEntry = {
  access: "rw" as const,
  destination: "/workspace/source",
  device: "16777231",
  inode: "424242",
  kind: "dir" as const,
  logical_id: "workspace:source",
  mode: 448,
  owner_uid: 501,
  source: "/host/workspace",
};

const spawnEffect: Effect = {
  action: "spawn",
  run_id: "run-42-worker",
  role: "worker",
  subject_id: 42,
  worksource: "ws-e2e",
  pool_ids: [42],
  harness: "fake",
  model_binding: "fake",
	brief: "immutable claimed-state brief",
  mount_plan: { entries: [workspaceEntry], version: 1 },
  session_path: "sessions/run-42-worker",
  heartbeat_interval_s: 1,
};

const PLAN_PATH = "/tmp/mc-home/runs/run-42-worker.mounts.json";

/** The committed precreate step, setup instruction included: the resident is
 * spine-blind, so mode/target/format arrive only inside the plan. */
const taskPrecreateStep = {
  child_mode: 0o700,
  setup: {
    mode: "fresh" as const,
    object_format: "sha1",
    target_ref: "main",
  },
  task_id: 42,
  tasks_parent: {
    canonical: "/host/workspace/.mission-control/tasks",
    device: "8",
    inode: "88",
    owner_uid: 501,
  },
  workspace_root: "/host/workspace",
};

const setupResultJson =
  '{"base_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","object_format":"sha1",' +
  '"local_repo_uuid":"0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",' +
  '"closure_digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",' +
  '"object_count":3,"fsck_clean":true}';

const SETUP_JSON_PATH = "/tmp/mc-home/runs/run-42-worker.setup.json";
const SETUP_COVER_DIR = "/tmp/mc-home/runs/run-42-worker.setup-cover";

const verifierProjection = {
  task_id: 42,
  rebuild_run_id: "run-42-rebuild",
  completion_request_id: "0011223344556677",
  object_format: "sha1" as const,
  sealed_sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  closure_digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  manifest_digest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
  seal_device: "1",
  seal_inode: "2",
  seal_owner_uid: 501,
};

/** The exact one-shot setup-container argv: ADR-017's setup mount table
 * (source RO under an empty .mission-control cover, task root RO with only
 * its source/git children RW, envelope RO) inside ADR-019's setup-class
 * resource/security envelope. */
const setupRunArgv = [
	"run", "--rm",
	"--name", "mc-setup-run-42-worker",
  "--network", "none",
  "--label", "mc-managed=true",
  "--label", "mc-tier=pipeline",
  "--label", "mc-run-id=run-42-worker",
  "--user", "10002:10002",
  "--cap-drop", "ALL",
  "--security-opt", "no-new-privileges=true",
  "--cpus", "1",
  "--memory", "1024m",
  "--pids-limit", "128",
  "-v", "/host/workspace:/repo/source:ro",
  "-v", `${SETUP_COVER_DIR}:/repo/source/.mission-control:ro`,
  "-v", "/host/workspace/.mission-control/tasks/task-42:/repo/task:ro",
  "-v", "/host/workspace/.mission-control/tasks/task-42/source:/repo/task/source",
  "-v", "/host/workspace/.mission-control/tasks/task-42/git:/repo/task/git",
  "-v", `${SETUP_JSON_PATH}:/mc/setup.json:ro`,
  "mc-fake-e2e:test",
  "mc", "__setup-first-task", "/mc/setup.json",
];

describe("idle / reenter / unknown", () => {
  test("idle does nothing but log", async () => {
    const rig = makeRig();
    await applyEffect({ action: "idle", reason: "no work" }, rig.deps);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.mc.calls).toEqual([]);
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.logs.some((l) => l.includes("idle") && l.includes("no work"))).toBe(true);
  });

  test("reenter is a no-op in the skeleton", async () => {
    const rig = makeRig();
    await applyEffect({ action: "reenter", task_id: 7 }, rig.deps);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.mc.calls).toEqual([]);
    expect(rig.fakeFs.events).toEqual([]);
  });

  test("an unknown action is logged and ignored (fail-closed)", async () => {
    const rig = makeRig();
    await applyEffect({ action: "explode" } as unknown as Effect, rig.deps);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.mc.calls).toEqual([]);
    expect(rig.logs.some((l) => l.includes("unknown action") && l.includes("explode"))).toBe(true);
  });
});

describe("spawn effect", () => {

  test("accepted-seal setup admits only confirmed absence of the exact prior Worker artifacts", async () => {
    const rig = makeRig();
    rig.docker.enqueue(fail(1, "No such container"), fail(1, "No such container"));
    await expect(requireAcceptedSealProducerAbsent("run-42-worker", rig.deps)).resolves.toBeUndefined();
    expect(rig.docker.calls).toEqual([
      ["inspect", "--type", "container", "mc-run-run-42-worker"],
      ["inspect", "--type", "container", "mc-setup-run-42-worker"],
    ]);
  });

  test("accepted-seal setup refuses a surviving or ambiguously labelled producer", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(JSON.stringify([{
      Id: "a".repeat(64), Name: "/mc-run-run-42-worker",
      Config: { Labels: { "mc-managed": "true", "mc-run-id": "run-42-worker", "mc-tier": "pipeline" } },
      State: { Status: "exited" },
    }])));
    await expect(requireAcceptedSealProducerAbsent("run-42-worker", rig.deps)).rejects.toThrow("still present");

    const ambiguous = makeRig();
    ambiguous.docker.enqueue(ok(JSON.stringify([{
      Id: "a".repeat(64), Name: "/mc-run-run-42-worker",
      Config: { Labels: { "mc-managed": "true", "mc-tier": "pipeline" } },
      State: { Status: "exited" },
    }])));
    await expect(requireAcceptedSealProducerAbsent("run-42-worker", ambiguous.deps)).rejects.toThrow("identity is ambiguous");
  });

  test("post-claim task precreate registers, then runs the setup container and records its result", async () => {
    const events: string[] = [];
    const registered = {
      canonical: "/host/workspace/.mission-control/tasks/task-42",
      device: "9",
      inode: "99",
      owner_uid: 501,
    };
    const rig = makeRig({
		recheckTaskParent: async (request) => {
			events.push(`recheck:${request.task_id}`);
		},
      precreateTaskSkeleton: async (request) => {
        events.push(`precreate:${request.task_id}`);
        return registered;
      },
		registerTaskRoot: async (runId, taskId, identity) => {
			events.push(`register:${runId}`);
			expect(taskId).toBe(42);
        expect(identity).toBe(registered);
      },
    });
    rig.docker.enqueue(ok(setupResultJson + "\n"));
    await applyEffect({
      ...spawnEffect,
      mount_plan: {
        entries: [],
        task_precreate: taskPrecreateStep,
        version: 1,
      },
    }, rig.deps);
	expect(events).toEqual(["recheck:42", "precreate:42", "register:run-42-worker"]);
    // Envelope materialized under runs/ beside the launch files, then removed
    // once the result is durably recorded; the cover dir masks other tasks.
    expect(rig.fakeFs.events).toEqual([
      "mkdir:/tmp/mc-home/runs",
      `mkdir:${SETUP_COVER_DIR}`,
      `write:${SETUP_JSON_PATH}`,
      `rm:${SETUP_JSON_PATH}`,
    ]);
    expect(rig.docker.calls).toEqual([setupRunArgv]);
    // The SetupResult travels byte-exactly from container stdout to the host
    // verb; the resident never reinterprets evidence or normalizes its
    // trailing newline.
    expect(rig.mc.calls).toEqual([[
      "task", "setup-record",
      "--run", "run-42-worker",
      "--task", "42",
      "--workspace", "/host/workspace",
      "--result", setupResultJson + "\n",
    ], [
      "task", "setup-continue", "--run", "run-42-worker",
    ]]);
    expect(rig.logs.some((line) => line.includes("first-task setup recorded"))).toBe(true);
  });

  test("receipt-backed recovery uses only the host exact-empty helper before fresh setup", async () => {
    const events: string[] = [];
    const recoverRoot = {
      canonical: "/host/workspace/.mission-control/tasks/task-42",
      device: "9",
      inode: "99",
      owner_uid: 501,
    };
    const rig = makeRig({
      recheckTaskParent: async () => { events.push("recheck"); },
      precreateTaskSkeleton: async () => {
        throw new Error("ordinary precreate must not consume a recovery plan");
      },
      recoverTaskSkeleton: async (request) => {
        events.push("recover");
        expect(request.recover_root).toEqual(recoverRoot);
        return recoverRoot;
      },
      registerTaskRoot: async () => { events.push("register"); },
    });
    rig.docker.enqueue(ok(setupResultJson + "\n"));
    await applyEffect({
      ...spawnEffect,
      mount_plan: {
        entries: [],
        task_precreate: { ...taskPrecreateStep, recover_root: recoverRoot },
        version: 1,
      },
    }, rig.deps);
    expect(events).toEqual(["recheck", "recover", "register"]);
  });

  test("the setup envelope restates exactly the committed fresh instruction", async () => {
    // The envelope is rm'd on success, so capture its bytes at the write.
    const rig = makeRig();
    let envelope = "";
    const realWrite = rig.deps.fs.writeFile;
    rig.deps.fs.writeFile = async (path, data, opts) => {
      if (path === SETUP_JSON_PATH) envelope = data;
      return realWrite(path, data, opts);
    };
    rig.docker.enqueue(ok(setupResultJson + "\n"));
    await applyEffect({
      ...spawnEffect,
      mount_plan: { entries: [], task_precreate: taskPrecreateStep, version: 1 },
    }, rig.deps);
    expect(JSON.parse(envelope)).toEqual({
      schema_version: 1,
      operation: "first-task-closure-extraction",
      run_id: "run-42-worker",
      task_id: 42,
      mode: "fresh",
      object_format: "sha1",
      target_ref: "main",
      branch: "mc/task-42",
      worktree_name: "mc-task-42",
      source_repo: "/repo/source",
      task_root: "/repo/task",
    });
  });

  test("a retry instruction produces a pinned envelope and never a target ref", async () => {
    const rig = makeRig();
    let envelope = "";
    const realWrite = rig.deps.fs.writeFile;
    rig.deps.fs.writeFile = async (path, data, opts) => {
      if (path === SETUP_JSON_PATH) envelope = data;
      return realWrite(path, data, opts);
    };
    rig.docker.enqueue(ok(setupResultJson + "\n"));
    await applyEffect({
      ...spawnEffect,
      mount_plan: {
        entries: [],
        task_precreate: {
          ...taskPrecreateStep,
          setup: {
            mode: "retry" as const,
            object_format: "sha1",
            pinned_base_sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            pinned_closure_digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
            pinned_local_repo_uuid: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
          },
        },
        version: 1,
      },
    }, rig.deps);
    expect(JSON.parse(envelope)).toEqual({
      schema_version: 1,
      operation: "first-task-closure-extraction",
      run_id: "run-42-worker",
      task_id: 42,
      mode: "retry",
      object_format: "sha1",
      pinned_base_sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      pinned_closure_digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      pinned_local_repo_uuid: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
      branch: "mc/task-42",
      worktree_name: "mc-task-42",
      source_repo: "/repo/source",
      task_root: "/repo/task",
    });
    expect(rig.docker.calls).toEqual([setupRunArgv]);
  });

  test("a failed setup container throws, keeps the envelope, and records nothing", async () => {
    const rig = makeRig();
    rig.docker.enqueue(fail(1, "fsck: missing blob"));
    await expect(applyEffect({
      ...spawnEffect,
      mount_plan: { entries: [], task_precreate: taskPrecreateStep, version: 1 },
    }, rig.deps)).rejects.toThrow("first-task setup container exited 1");
    expect(rig.mc.calls).toEqual([]);
    // The envelope stays on disk as failure evidence: no rm event.
    expect(rig.fakeFs.events).toEqual([
      "mkdir:/tmp/mc-home/runs",
      `mkdir:${SETUP_COVER_DIR}`,
      `write:${SETUP_JSON_PATH}`,
    ]);
  });

  test("a refused setup-record throws and keeps the envelope", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(setupResultJson + "\n"));
    rig.mc.enqueue(fail(1, "retry diverges from the recorded assignment"));
    await expect(applyEffect({
      ...spawnEffect,
      mount_plan: { entries: [], task_precreate: taskPrecreateStep, version: 1 },
    }, rig.deps)).rejects.toThrow("first-task setup record refused (exit 1)");
    expect(rig.fakeFs.events).toEqual([
      "mkdir:/tmp/mc-home/runs",
      `mkdir:${SETUP_COVER_DIR}`,
      `write:${SETUP_JSON_PATH}`,
    ]);
  });

  test("a malformed setup instruction is refused before any effect", async () => {
    for (const setup of [
      undefined,
      { mode: "rebase", object_format: "sha1", target_ref: "main" },
      { mode: "fresh", object_format: "sha512", target_ref: "main" },
      { mode: "fresh", object_format: "sha1", target_ref: "" },
      { mode: "fresh", object_format: "sha1", target_ref: "main", pinned_base_sha: "aa" },
      { mode: "retry", object_format: "sha1", pinned_base_sha: "aa" },
      {
        mode: "retry", object_format: "sha1", target_ref: "main",
        pinned_base_sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        pinned_closure_digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        pinned_local_repo_uuid: "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
      },
    ]) {
      let touched = false;
      const rig = makeRig({
        recheckTaskParent: async () => {
          touched = true;
        },
        precreateTaskSkeleton: async () => {
          touched = true;
          return taskPrecreateStep.tasks_parent;
        },
      });
      await applyEffect({
        ...spawnEffect,
        mount_plan: {
          entries: [],
          task_precreate: {
            ...taskPrecreateStep,
            setup: setup as never,
          },
          version: 1,
        },
      }, rig.deps);
      expect(touched).toBe(false);
      expect(rig.docker.calls).toEqual([]);
      expect(rig.mc.calls).toEqual([]);
      expect(rig.fakeFs.events).toEqual([]);
      expect(rig.logs.some((line) => line.includes("spawn refused"))).toBe(true);
    }
  });

  test("an accepted-seal setup rebuilds through its closed host handoff without verifier creation", async () => {
		let rechecked = false;
    const rig = makeRig({
			recheckAcceptedSeal: async () => {
				rechecked = true;
				return "/tmp/mc-home/seals/run-42-worker";
			},
		});
		rig.docker.enqueue(fail(1, "No such container"), fail(1, "No such container"));
		rig.docker.enqueue(ok(setupResultJson + "\n"));
    await applyEffect({
      ...spawnEffect,
			run_id: "run-42-verifier",
      role: "verifier",
      mount_plan: {
			entries: [{ ...workspaceEntry, access: "ro", destination: "/workspace", logical_id: "task-root", source: "/host/workspace/.mission-control/tasks/task-42" }], version: 1,
        accepted_seal_rebuild: {
          task_id: 42, run_id: "run-42-worker", completion_request_id: "0011223344556677",
          object_format: "sha1", sealed_sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          closure_digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
          manifest_digest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
          device: "1", inode: "2", owner_uid: 501,
        },
      },
    }, rig.deps);
    expect(rig.docker.calls).toEqual([
			["inspect", "--type", "container", "mc-run-run-42-worker"],
			["inspect", "--type", "container", "mc-setup-run-42-worker"],
			[
				"run", "--rm", "--name", "mc-setup-run-42-verifier", "--network", "none",
				"--label", "mc-managed=true", "--label", "mc-tier=pipeline", "--label", "mc-run-id=run-42-verifier",
				"--user", "10002:10002", "--cap-drop", "ALL", "--security-opt", "no-new-privileges=true",
				"--cpus", "1", "--memory", "1024m", "--pids-limit", "128",
				"-v", "/tmp/mc-home/seals/run-42-worker:/repo/seal:ro",
				"-v", "/host/workspace/.mission-control/tasks/task-42:/repo/task:ro", "-v", "/host/workspace/.mission-control/tasks/task-42/source:/repo/task/source",
				"-v", "/host/workspace/.mission-control/tasks/task-42/git:/repo/task/git", "-v", "/tmp/mc-home/runs/run-42-verifier.setup.json:/mc/setup.json:ro",
				"mc-fake-e2e:test", "mc", "__setup-accepted-seal", "/mc/setup.json",
			],
		]);
		expect(rig.mc.calls).toEqual([
			["task", "accepted-seal-record", "--run", "run-42-verifier", "--task", "42", "--workspace", "/host/workspace", "--result", setupResultJson + "\n"],
			["task", "accepted-seal-continue", "--run", "run-42-verifier"],
		]);
		expect(rig.fakeFs.events).toContain("write:/tmp/mc-home/runs/run-42-verifier.setup.json");
		expect(rig.fakeFs.events).toContain("rm:/tmp/mc-home/runs/run-42-verifier.setup.json");
		expect(rechecked).toBe(true);
		expect(rig.logs.some((line) => line.includes("accepted-seal rebuild recorded and continued"))).toBe(true);
  });

  // The real attested plan for a sealed Verifier carries the full typed task
  // table, which already names /workspace/source and both covers (see
  // mc/verbs/taskskeleton.go). The projection must REPLACE those rows, not sit
  // beside them: Docker refuses a duplicate mount point outright, and letting
  // the canonical task source through RW would hand the Verifier the very store
  // the disposable projection exists to keep it out of.
  test("a Verifier projection replaces the canonical source rows instead of duplicating them", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""), ok(""), ok(""));
    const taskRoot = "/host/workspace/.mission-control/tasks/task-42";
    await applyEffect({
      ...spawnEffect,
      run_id: "run-42-verifier",
      role: "verifier",
      mount_plan: {
        entries: [
          { ...workspaceEntry, access: "ro", destination: "/workspace", logical_id: "task-root", source: taskRoot },
          { ...workspaceEntry, access: "rw", destination: "/workspace/source", logical_id: "task:source", source: `${taskRoot}/source` },
          { ...workspaceEntry, access: "ro", destination: "/workspace/source/.git", logical_id: "task:source-git-cover", source: `${taskRoot}/source/.git` },
          { ...workspaceEntry, access: "ro", destination: "/workspace/source/.mission-control", logical_id: "task:source-mc-cover", source: `${taskRoot}/source/.mission-control` },
          { ...workspaceEntry, access: "rw", destination: "/workspace/git", logical_id: "task:git", source: `${taskRoot}/git` },
        ],
        verifier_projection: verifierProjection,
        version: 1,
      },
    }, rig.deps);

    const projectionRoot = "/tmp/mc-home/runs/projections/run-42-verifier";
    const create = rig.docker.calls[1]!;
    // Exactly one bind per destination.
    for (const dest of ["/workspace/source", "/workspace/source/.git", "/workspace/source/.mission-control"]) {
      const bound = create.filter((arg) => arg.endsWith(`:${dest}`) || arg.endsWith(`:${dest}:ro`));
      expect([dest, bound.length]).toEqual([dest, 1]);
    }
    // The canonical task source never reaches the Verifier; the projection does.
    expect(create).toContain(`${projectionRoot}:/workspace/source`);
    expect(create).not.toContain(`${taskRoot}/source:/workspace/source`);
    // Rows outside the projected source tree are untouched.
    expect(create).toContain(`${taskRoot}/git:/workspace/git`);
    expect(create).toContain(`${taskRoot}:/workspace:ro`);
  });

  test("a Verifier projection is set up before agent creation and covers canonical controls after its RW source", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""), ok(""), ok(""));
    await applyEffect({
      ...spawnEffect,
      run_id: "run-42-verifier",
      role: "verifier",
      mount_plan: {
        entries: [{ ...workspaceEntry, access: "ro", destination: "/workspace", logical_id: "task-root", source: "/host/workspace/.mission-control/tasks/task-42" }],
        verifier_projection: verifierProjection,
        version: 1,
      },
    }, rig.deps);

    const projectionRoot = "/tmp/mc-home/runs/projections/run-42-verifier";
    expect(rig.docker.calls[0]).toEqual([
      "run", "--rm", "--name", "mc-setup-run-42-verifier", "--network", "none",
      "--label", "mc-managed=true", "--label", "mc-tier=pipeline", "--label", "mc-run-id=run-42-verifier",
      "--user", "10002:10002", "--cap-drop", "ALL", "--security-opt", "no-new-privileges=true",
      "--cpus", "1", "--memory", "1024m", "--pids-limit", "128",
      "-v", "/host/workspace/.mission-control/tasks/task-42:/repo/task:ro",
      "-v", `${projectionRoot}:/repo/projection`,
      "-v", "/tmp/mc-home/runs/run-42-verifier.projection.json:/mc/setup.json:ro",
      "mc-fake-e2e:test", "mc", "__setup-verifier-projection", "/mc/setup.json",
    ]);
    expect(rig.docker.calls[1]).toContain(`${projectionRoot}:/workspace/source`);
    const create = rig.docker.calls[1];
    expect(create).toEqual(expect.arrayContaining([
      "-v", `${projectionRoot}:/workspace/source`,
      "-v", "/host/workspace/.mission-control/tasks/task-42/source/.git:/workspace/source/.git:ro",
      "-v", "/host/workspace/.mission-control/tasks/task-42/source/.mission-control:/workspace/source/.mission-control:ro",
    ]));
    expect(create.indexOf(`${projectionRoot}:/workspace/source`)).toBeLessThan(create.indexOf("/host/workspace/.mission-control/tasks/task-42/source/.git:/workspace/source/.git:ro"));
    expect(rig.fakeFs.events).toEqual(expect.arrayContaining([
      `mkdir:${projectionRoot}`,
      "rm:/tmp/mc-home/runs/run-42-verifier.projection.json",
    ]));
  });

  test("a failed Verifier projection setup retains its envelope but recursively removes its unusable source tree", async () => {
    const rig = makeRig();
    rig.docker.enqueue(fail(1, "sealed tree mismatch"));
    await applyEffect({
      ...spawnEffect,
      run_id: "run-42-verifier",
      role: "verifier",
      mount_plan: {
        entries: [{ ...workspaceEntry, access: "ro", destination: "/workspace", logical_id: "task-root", source: "/host/workspace/.mission-control/tasks/task-42" }],
        verifier_projection: verifierProjection,
        version: 1,
      },
    }, rig.deps);
    expect(rig.docker.calls).toHaveLength(1);
    expect(rig.mc.calls).toEqual([]);
    expect(rig.fakeFs.events).toEqual([
      "mkdir:/tmp/mc-home/runs/projections",
      "mkdir:/tmp/mc-home/runs/projections/run-42-verifier",
      "write:/tmp/mc-home/runs/run-42-verifier.projection.json",
      "rm:/tmp/mc-home/runs/projections/run-42-verifier",
    ]);
    expect(rig.logs.some((line) => line.includes("verifier projection setup failed"))).toBe(true);
  });

  test("an existing Verifier projection root is refused without adopting or deleting it", async () => {
    const rig = makeRig();
    const projectionRoot = "/tmp/mc-home/runs/projections/run-42-verifier";
    const mkdir = rig.deps.fs.mkdir;
    rig.deps.fs.mkdir = async (path, opts) => {
      if (path === projectionRoot) {
        expect(opts).toEqual({ exclusive: true });
        throw new Error("file already exists");
      }
      return mkdir(path, opts);
    };
    await applyEffect({
      ...spawnEffect,
      run_id: "run-42-verifier",
      role: "verifier",
      mount_plan: {
        entries: [{ ...workspaceEntry, access: "ro", destination: "/workspace", logical_id: "task-root", source: "/host/workspace/.mission-control/tasks/task-42" }],
        verifier_projection: verifierProjection,
        version: 1,
      },
    }, rig.deps);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.fakeFs.events).toEqual(["mkdir:/tmp/mc-home/runs/projections"]);
    expect(rig.logs.some((line) => line.includes("verifier projection setup failed"))).toBe(true);
  });

	test("a surviving accepted-seal producer leaves the verifier unprepared", async () => {
		let rechecked = false;
		const rig = makeRig({ recheckAcceptedSeal: async () => {
			rechecked = true;
			return "/tmp/mc-home/seals/run-42-worker";
		} });
		rig.docker.enqueue(ok(JSON.stringify([{
			Id: "a".repeat(64), Name: "/mc-run-run-42-worker",
			Config: { Labels: { "mc-managed": "true", "mc-run-id": "run-42-worker", "mc-tier": "pipeline" } },
			State: { Status: "exited" },
		}])));
		await applyEffect({
			...spawnEffect,
			role: "verifier",
			mount_plan: {
			entries: [{ ...workspaceEntry, access: "ro", destination: "/workspace", logical_id: "task-root", source: "/host/workspace/.mission-control/tasks/task-42" }], version: 1,
				accepted_seal_rebuild: {
					task_id: 42, run_id: "run-42-worker", completion_request_id: "0011223344556677",
					object_format: "sha1", sealed_sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					closure_digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					manifest_digest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
					device: "1", inode: "2", owner_uid: 501,
				},
			},
		}, rig.deps);
		expect(rig.docker.calls).toEqual([["inspect", "--type", "container", "mc-run-run-42-worker"]]);
		expect(rig.mc.calls).toEqual([]);
		expect(rig.fakeFs.events).toEqual([]);
		expect(rechecked).toBe(false);
		expect(rig.logs.some((line) => line.includes("producer is still present"))).toBe(true);
	});

  test("a malformed accepted-seal receipt is refused before the setup fence", async () => {
    const rig = makeRig();
    await applyEffect({
      ...spawnEffect,
      role: "verifier",
      mount_plan: {
        entries: [], version: 1,
        accepted_seal_rebuild: {
          task_id: 42, run_id: "run-42-worker", completion_request_id: "not-a-request",
          object_format: "sha1", sealed_sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          closure_digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
          manifest_digest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
          device: "1", inode: "2", owner_uid: 501,
        },
      },
    }, rig.deps);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.mc.calls).toEqual([]);
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.logs.some((line) => line.includes("accepted seal rebuild descriptor is malformed"))).toBe(true);
  });

  test("task precreate failure performs no later effect", async () => {
    const rig = makeRig({
      precreateTaskSkeleton: async () => {
        throw new Error("task parent identity changed after preclaim");
      },
    });
    await expect(applyEffect({
      ...spawnEffect,
      mount_plan: {
        entries: [],
        task_precreate: taskPrecreateStep,
        version: 1,
      },
    }, rig.deps)).rejects.toThrow("parent identity changed");
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.docker.calls).toEqual([]);
  });

  test("task-parent trust recheck refusal performs no precreate", async () => {
	let precreated = false;
	const rig = makeRig({
		recheckTaskParent: async () => {
			throw new Error("task parent recheck refused: ACL grants non-owner access");
		},
		precreateTaskSkeleton: async (request) => {
			precreated = true;
			return request.tasks_parent;
		},
	});
	await expect(applyEffect({
		...spawnEffect,
		mount_plan: {
			entries: [],
			task_precreate: taskPrecreateStep,
			version: 1,
		},
	}, rig.deps)).rejects.toThrow("ACL grants non-owner access");
	expect(precreated).toBe(false);
	expect(rig.fakeFs.events).toEqual([]);
	expect(rig.docker.calls).toEqual([]);
  });

  test("task-root registration failure leaves the created residue and performs no launch", async () => {
    const events: string[] = [];
    const rig = makeRig({
      precreateTaskSkeleton: async (request) => {
        events.push("precreate");
        return {
          canonical: `${request.tasks_parent.canonical}/task-${request.task_id}`,
          device: "9", inode: "99", owner_uid: request.tasks_parent.owner_uid,
        };
      },
      registerTaskRoot: async () => {
        events.push("register");
        throw new Error("registration fence failed");
      },
    });
    await expect(applyEffect({
      ...spawnEffect,
      mount_plan: {
        entries: [],
        task_precreate: taskPrecreateStep,
        version: 1,
      },
    }, rig.deps)).rejects.toThrow("registration fence failed");
    expect(events).toEqual(["precreate", "register"]);
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.docker.calls).toEqual([]);
  });

  test("forged returned task-root identity is never registered", async () => {
    let registered = false;
    const rig = makeRig({
      precreateTaskSkeleton: async () => ({
        canonical: "/host/workspace/.mission-control/tasks/task-evil",
        device: "9", inode: "99", owner_uid: 501,
      }),
      registerTaskRoot: async () => {
        registered = true;
      },
    });
    await expect(applyEffect({
      ...spawnEffect,
      mount_plan: {
        entries: [],
        task_precreate: taskPrecreateStep,
        version: 1,
      },
    }, rig.deps)).rejects.toThrow("invalid registration evidence");
    expect(registered).toBe(false);
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.docker.calls).toEqual([]);
  });

  test("a widened task child mode is refused before precreate", async () => {
    let called = false;
    const rig = makeRig({
      precreateTaskSkeleton: async (request) => {
        called = true;
        return request.tasks_parent;
      },
    });
    await applyEffect({
      ...spawnEffect,
      mount_plan: {
        entries: [],
        task_precreate: { ...taskPrecreateStep, child_mode: 0o777 },
        version: 1,
      },
    }, rig.deps);
    expect(called).toBe(false);
    expect(rig.logs.some((line) => line.includes("task precreate descriptor is malformed"))).toBe(true);
  });

  test("canonical route is refused before the fake-only skeleton can mislabel execution", async () => {
    const rig = makeRig();
    await applyEffect(
      { ...spawnEffect, harness: "codex", model_binding: "chatgpt" },
      rig.deps,
    );
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.logs.some((l) => l.includes("spawn refused") && l.includes("unsupported route"))).toBe(true);
  });

  // Design B (production completion-seal E2E): the shipped image carries only
  // the fake agent-runner, but the seal + typed task-store plan attach only on
  // a non-fake (production) route. `agentRunnerRoutes` authorizes exactly which
  // non-fake `harness/model_binding` routes that one adapter may stand in for,
  // so the real resident can launch a production Worker through it. The default
  // (unset) keeps the fake/fake-only refusal — fail-closed.
  test("a configured non-fake adapter route launches through the shipped agent-runner", async () => {
    const rig = makeRig({ config: { ...testConfig, agentRunnerRoutes: ["codex/chatgpt"] } });
    rig.docker.enqueue(ok("container-id\n"), ok(""));
    await applyEffect({ ...spawnEffect, harness: "codex", model_binding: "chatgpt" }, rig.deps);
    const create = rig.docker.calls[0]!;
    expect(create[0]).toBe("create");
    expect(create).toContain("mc-run-run-42-worker");
    // The same allowlist is handed to the in-container adapter so it, too, will
    // stand in for the route (its own fail-closed gate is otherwise fake-only).
    expect(create).toEqual(expect.arrayContaining(["-e", "MC_AGENT_RUNNER_ROUTES=codex/chatgpt"]));
    // The run.json records the real committed route, never a fake relabel.
    const runJson = JSON.parse(rig.fakeFs.writes.get("/tmp/mc-home/runs/run-42-worker.json")!);
    expect(runJson.harness).toBe("codex");
    expect(runJson.model_binding).toBe("chatgpt");
    expect(rig.logs.some((l) => l.includes("unsupported route"))).toBe(false);
  });

  test("a non-fake route absent from the adapter allowlist stays refused (fail-closed)", async () => {
    const rig = makeRig({ config: { ...testConfig, agentRunnerRoutes: ["codex/chatgpt"] } });
    await applyEffect({ ...spawnEffect, harness: "claude-sdk", model_binding: "claude" }, rig.deps);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.logs.some((l) => l.includes("spawn refused") && l.includes("unsupported route"))).toBe(true);
  });

  test("an authorized non-fake Worker still binds its private completion seal and drops to model uid", async () => {
    const states: string[] = [];
    const rig = makeRig({
      config: { ...testConfig, agentRunnerRoutes: ["codex/chatgpt"] },
      recheckCompletionSeal: async (_step, state) => { states.push(state); },
    });
    await applyEffect({
      ...spawnEffect, harness: "codex", model_binding: "chatgpt",
      mount_plan: { version: 1, entries: [workspaceEntry], completion_seal: {
        run_id: "run-42-worker", task_id: 42,
        seals_parent: { canonical: "/tmp/mc-home/seals", device: "1", inode: "2", owner_uid: 501 },
      } },
    }, rig.deps);
    const create = rig.docker.calls[0]!;
    expect(create).toContain("/tmp/mc-home/seals/run-42-worker:/mc/private/completion-seal");
    expect(create).toEqual(expect.arrayContaining(["--user", "10002:10002", "--cap-drop", "ALL"]));
    expect(create).not.toContain("no-new-privileges=true");
    expect(states).toEqual(["absent", "ready", "ready", "ready"]);
  });

  test("effects in §10 order: folder → run.json → container", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok("container-id\n"));
    // interleave docker into the same event stream
    const realDocker = rig.deps.docker;
    rig.deps.docker = (argv) => {
      rig.fakeFs.events.push("docker:" + argv[0]);
      return realDocker(argv);
    };
    await applyEffect(spawnEffect, rig.deps);
    expect(rig.fakeFs.events).toEqual([
      "mkdir:/tmp/mc-home/sessions/run-42-worker",
      "mkdir:/tmp/mc-home/runs",
      "write:/tmp/mc-home/runs/run-42-worker.json",
      "write:" + PLAN_PATH,
      "docker:create",
      "docker:start",
    ]);
    expect(rig.mc.calls).toEqual([
      ["__mount-recheck", PLAN_PATH],
      ["__mount-recheck", PLAN_PATH],
    ]);
  });

  test("run.json is materialized OUTSIDE the session folder (spec §4, Inv. 26)", async () => {
    const rig = makeRig();
    await applyEffect(spawnEffect, rig.deps);
    for (const path of rig.fakeFs.writes.keys()) {
      expect(path.startsWith("/tmp/mc-home/sessions/")).toBe(false);
    }
  });

  test("run.json matches the §6 fixed schema", async () => {
    const rig = makeRig();
    await applyEffect(spawnEffect, rig.deps);
    const written = rig.fakeFs.writes.get("/tmp/mc-home/runs/run-42-worker.json");
    expect(written).toBeDefined();
    expect(written!.endsWith("\n")).toBe(true);
    const runJson = JSON.parse(written!);
    expect(runJson).toEqual({
      run_id: "run-42-worker",
      tier: "pipeline",
      role: "worker",
      subject_id: 42,
      worksource: "ws-e2e",
      harness: "fake",
      model_binding: "fake",
      mode: "fresh",
	      brief: "immutable claimed-state brief",
      pool_ids: [42],
      heartbeat_interval_s: 1,
      harness_config: { behavior: "/mc/behaviors/worker.json" },
      mounts: { session: "/mc/session" },
    });
	    expect(runJson.brief).toBe(spawnEffect.brief);
    // The agent-visible envelope never carries host source paths; they live
    // only in the host-side plan sibling.
    expect(written!.includes("/host/workspace")).toBe(false);
    const plan = JSON.parse(rig.fakeFs.writes.get(PLAN_PATH)!);
    expect(plan).toEqual({ entries: [workspaceEntry], version: 1 });
  });

  test("subjectless spawn (strategist) writes subject_id null", async () => {
    const rig = makeRig();
    await applyEffect(
      {
        action: "spawn", run_id: "run-9-strategist", role: "strategist",
        subject_id: null, worksource: "ws-e2e", pool_ids: [],
        harness: "fake", model_binding: "fake",
	        brief: "subjectless immutable brief",
        mount_plan: { entries: [], version: 1 },
        heartbeat_interval_s: 1,
      },
      rig.deps,
    );
    const runJson = JSON.parse(
      rig.fakeFs.writes.get("/tmp/mc-home/runs/run-9-strategist.json")!,
    );
    expect(runJson.subject_id).toBeNull();
    expect(runJson.pool_ids).toEqual([]);
    expect(runJson.harness_config.behavior).toBe("/mc/behaviors/strategist.json");
  });

  // ADR-022 step 3: a real (non-fake) agent route projects credentials
  // through the injected CredentialProjector and gets an open network — the
  // egress control is deleted by requirement. The fake family stays
  // --network none as spawn-side hygiene (it is deterministic and
  // token-free), which also keeps the Phase 1 contract §1 row intact.
  describe("credentialed agent spawn (ADR-022)", () => {
    const claudeEffect: Effect = {
      ...spawnEffect,
      run_id: "run-77-worker",
      harness: "claude",
      model_binding: "claude-max",
    };
    const routedConfig = { ...testConfig, agentRunnerRoutes: ["claude/claude-max", "codex/codex-sub"] };

    test("a claude-channel spawn merges the flag env and drops --network none", async () => {
      const rig = makeRig({
        config: routedConfig,
        credentials: {
          project: () => ({
            env: {
              CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST: "1",
              CLAUDE_CODE_OAUTH_TOKEN: "host-minted-access",
            },
          }),
        },
      });
      rig.docker.enqueue(ok("container-id\n"), ok(""));
      await applyEffect(claudeEffect, rig.deps);
      const create = rig.docker.calls[0]!;
      expect(create).not.toContain("--network");
      expect(create).toContain("CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST=1");
      expect(create).toContain("CLAUDE_CODE_OAUTH_TOKEN=host-minted-access");
      expect(create.join(" ")).not.toContain("ANTHROPIC_API_KEY");
      expect(rig.docker.calls[1]).toEqual(["start", "mc-run-run-77-worker"]);
    });

    test("a codex-channel spawn binds the projected auth.json RW and points CODEX_HOME at it", async () => {
      const rig = makeRig({
        config: routedConfig,
        credentials: {
          project: () => ({
            env: { CODEX_REFRESH_TOKEN_URL_OVERRIDE: "http://host.docker.internal:7799/oauth/token" },
            authJson: '{"auth_mode":"chatgpt"}',
          }),
        },
      });
      rig.docker.enqueue(ok("container-id\n"), ok(""));
      await applyEffect(
        { ...claudeEffect, run_id: "run-78-worker", harness: "codex", model_binding: "codex-sub" },
        rig.deps,
      );
      expect(rig.fakeFs.writes.get("/tmp/mc-home/runs/run-78-worker.codex-auth.json")).toBe('{"auth_mode":"chatgpt"}');
      const create = rig.docker.calls[0]!;
      expect(create).toContain("/tmp/mc-home/runs/run-78-worker.codex-auth.json:/mc/codex/auth.json");
      expect(create).toContain("CODEX_HOME=/mc/codex");
      expect(create).toContain("CODEX_REFRESH_TOKEN_URL_OVERRIDE=http://host.docker.internal:7799/oauth/token");
      expect(create).not.toContain("--network");
    });

    test("a refused projection spawns nothing and writes nothing (D8)", async () => {
      const rig = makeRig({
        config: routedConfig,
        credentials: { project: () => ({ refused: "claude credential lapsed" }) },
      });
      await applyEffect(claudeEffect, rig.deps);
      expect(rig.docker.calls).toEqual([]);
      expect(rig.fakeFs.writes.size).toBe(0);
      expect(rig.logs.some((line) => line.includes("claude credential lapsed"))).toBe(true);
    });

    test("without a projector a routed non-fake spawn launches token-free on the open network", async () => {
      const rig = makeRig({ config: routedConfig });
      rig.docker.enqueue(ok("container-id\n"), ok(""));
      await applyEffect(claudeEffect, rig.deps);
      const create = rig.docker.calls[0]!;
      expect(create).not.toContain("--network");
      expect(create.join(" ")).not.toContain("CLAUDE_CODE");
    });

    test("the fake family keeps --network none as hygiene", async () => {
      const rig = makeRig({
        credentials: { project: () => ({ env: { CLAUDE_CODE_OAUTH_TOKEN: "never" } }) },
      });
      rig.docker.enqueue(ok("container-id\n"), ok(""));
      await applyEffect(spawnEffect, rig.deps);
      const create = rig.docker.calls[0]!;
      expect(create).toContain("--network");
      expect(create.join(" ")).not.toContain("CLAUDE_CODE");
    });
  });

  test("docker create+start argv: --rm, --network none, exact name, labels, plan binds, MC_SPINE", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok("container-id\n"), ok(""));
    await applyEffect(spawnEffect, rig.deps);
    expect(rig.docker.calls).toEqual([
      [
        "create", "--rm",
        "--network", "none",
        "--name", "mc-run-run-42-worker",
        "--label", "mc-managed=true",
		"--label", "mc-tier=pipeline",
		"--label", "mc-run-id=run-42-worker",
		"-v", "/tmp/mc-home/sessions/run-42-worker:/mc/session",
        "-v", "/tmp/mc-home/runs/run-42-worker.json:/mc/run.json:ro",
        "-v", "/host/behaviors:/mc/behaviors:ro",
        "-v", "/host/runner:/app/src:ro",
        "-v", "/host/workspace:/workspace/source",
        "-v", "mc-spine-test:/mc/spine",
        "-e", "MC_SPINE=/mc/spine/spine.db",
        "mc-fake-e2e:test",
        "bun", "/app/src/agent-runner/main.ts",
      ],
      ["start", "mc-run-run-42-worker"],
    ]);
  });

	test("a completion-seal Worker binds only its derived private root and omits NNP", async () => {
		let states: string[] = [];
		const rig = makeRig({ recheckCompletionSeal: async (_step, state) => { states.push(state); } });
		await applyEffect({ ...spawnEffect, mount_plan: { version: 1, entries: [workspaceEntry], completion_seal: {
			run_id: "run-42-worker", task_id: 42,
			seals_parent: { canonical: "/tmp/mc-home/seals", device: "1", inode: "2", owner_uid: 501 },
		} } }, rig.deps);
		const create = rig.docker.calls[0]!;
		expect(create).toContain("/tmp/mc-home/seals/run-42-worker:/mc/private/completion-seal");
		expect(create).toEqual(expect.arrayContaining(["--user", "10002:10002", "--cap-drop", "ALL"]));
		expect(create).not.toContain("no-new-privileges=true");
		expect(states).toEqual(["absent", "ready", "ready", "ready"]);
	});

  test("an RO plan entry binds with the :ro flag; an empty plan skips the rechecks", async () => {
    const rig = makeRig();
    await applyEffect(
      {
        ...spawnEffect,
        mount_plan: {
          entries: [{ ...workspaceEntry, access: "ro", destination: "/workspace/references/ref", logical_id: "reference:ref", source: "/host/reference" }],
          version: 1,
        },
      },
      rig.deps,
    );
    const createArgv = rig.docker.calls[0]!;
    expect(createArgv).toContain("/host/reference:/workspace/references/ref:ro");
    expect(rig.mc.calls).toHaveLength(2);

    const emptyRig = makeRig();
    await applyEffect(
      { ...spawnEffect, mount_plan: { entries: [], version: 1 } },
      emptyRig.deps,
    );
    expect(emptyRig.mc.calls).toEqual([]);
    expect(emptyRig.docker.calls.map((argv) => argv[0])).toEqual(["create", "start"]);
    expect(emptyRig.docker.calls[0]!.join(" ")).not.toContain("/workspace/source");
  });

  test("the task-root row binds at exactly /workspace; a sibling-prefix destination is refused", async () => {
    const rig = makeRig();
    await applyEffect(
      {
        ...spawnEffect,
        mount_plan: {
          entries: [{ ...workspaceEntry, access: "ro", destination: "/workspace", logical_id: "task-root", source: "/host/task-root" }],
          version: 1,
        },
      },
      rig.deps,
    );
    expect(rig.docker.calls[0]!).toContain("/host/task-root:/workspace:ro");

    const evilRig = makeRig();
    await applyEffect(
      {
        ...spawnEffect,
        mount_plan: {
          entries: [{ ...workspaceEntry, destination: "/workspacex" }],
          version: 1,
        },
      },
      evilRig.deps,
    );
    expect(evilRig.docker.calls).toEqual([]);
    expect(evilRig.logs.some((l) => l.includes("spawn refused") && l.includes("/workspace namespace"))).toBe(true);
  });

  test("a spawn without a mount plan is refused before any effect (fail-closed)", async () => {
    const rig = makeRig();
    const { mount_plan: _dropped, ...withoutPlan } = spawnEffect as Effect & { mount_plan: unknown };
    await applyEffect(withoutPlan as Effect, rig.deps);
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.logs.some((l) => l.includes("spawn refused") && l.includes("no explicit mount plan"))).toBe(true);
  });

  test("a plan entry unsafe for -v grammar is refused before any effect", async () => {
    const rig = makeRig();
    await applyEffect(
      {
        ...spawnEffect,
        mount_plan: { entries: [{ ...workspaceEntry, source: "/host/evil:path" }], version: 1 },
      },
      rig.deps,
    );
    expect(rig.docker.calls).toEqual([]);
    expect(rig.logs.some((l) => l.includes("spawn refused") && l.includes("colon-free"))).toBe(true);
  });

  test("recheck drift before create: no container, launch files removed", async () => {
    const rig = makeRig();
    rig.mc.enqueue(fail(1, "mount recheck: entry 0 (workspace:source): source mode grant changed"));
    await applyEffect(spawnEffect, rig.deps);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.logs.some((l) => l.includes("mount recheck refused before create (exit 1)"))).toBe(true);
    expect(rig.fakeFs.events.slice(-2)).toEqual([
      "rm:/tmp/mc-home/runs/run-42-worker.json",
      "rm:" + PLAN_PATH,
    ]);
  });

  test("recheck drift after create removes the unstarted container (ADR-016 D5)", async () => {
    const rig = makeRig();
    rig.mc.enqueue(ok("{}"), fail(1, "source is a different filesystem object"));
    rig.docker.enqueue(ok("container-id\n"), ok(""));
    await applyEffect(spawnEffect, rig.deps);
    expect(rig.docker.calls.map((argv) => argv[0])).toEqual(["create", "rm"]);
    expect(rig.docker.calls[1]).toEqual(["rm", "mc-run-run-42-worker"]);
    expect(rig.logs.some((l) => l.includes("mount recheck refused after create (exit 1)"))).toBe(true);
    expect(rig.fakeFs.events.slice(-2)).toEqual([
      "rm:/tmp/mc-home/runs/run-42-worker.json",
      "rm:" + PLAN_PATH,
    ]);
  });

  test("docker create failure removes the launch files and never starts", async () => {
    const rig = makeRig();
    rig.docker.enqueue(fail(125, "docker daemon down"));
    await applyEffect(spawnEffect, rig.deps);
    expect(rig.docker.calls.map((argv) => argv[0])).toEqual(["create"]);
    expect(rig.logs.some((l) => l.includes("docker create failed (exit 125)"))).toBe(true);
    expect(rig.fakeFs.events.slice(-2)).toEqual([
      "rm:/tmp/mc-home/runs/run-42-worker.json",
      "rm:" + PLAN_PATH,
    ]);
  });

  test("a role with no mapped behavior is refused before any effect", async () => {
    const rig = makeRig();
    await applyEffect({ ...spawnEffect, role: "gardener" } as Effect, rig.deps);
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.logs.some((l) => l.includes("spawn refused") && l.includes("gardener"))).toBe(true);
  });

  test("a run_id outside the container-name charset is refused", async () => {
    const rig = makeRig();
    await applyEffect({ ...spawnEffect, run_id: "../etc; rm -rf" } as Effect, rig.deps);
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.logs.some((l) => l.includes("spawn refused") && l.includes("charset"))).toBe(true);
  });
});

describe("land effect", () => {
  const landEffect: Effect = {
    action: "land",
    task_id: 42,
    branch: "mc/task-42",
    verified_sha: "abc123def",
    target_ref: "main",
  };

  test("runs mc-land with (branch, sha, target_ref) argv, then reports success", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""));
    rig.mc.enqueue(ok("{}"));
    await applyEffect(landEffect, rig.deps);
    expect(rig.docker.calls).toEqual([
      [
        "run", "--rm",
        "--network", "none",
        "--label", "mc-managed",
        "-v", "/host/workspace:/workspace/source",
        "mc-fake-e2e:test",
        "mc-land", "mc/task-42", "abc123def", "main",
      ],
    ]);
    expect(rig.mc.calls).toEqual([
      ["land", "report", "42", "--status", "success"],
    ]);
  });

  test("nonzero mc-land exit reports failure with a reason (nothing half-lands)", async () => {
    const rig = makeRig();
    rig.docker.enqueue(fail(7, "SHA fence mismatch"));
    rig.mc.enqueue(ok("{}"));
    await applyEffect(landEffect, rig.deps);
    expect(rig.mc.calls).toEqual([
      ["land", "report", "42", "--status", "failure", "--reason", "mc-land exited 7"],
    ]);
    expect(rig.logs.some((l) => l.includes("mc-land failed (exit 7)"))).toBe(true);
  });

  test("successful landing surfaces post-merge cleanup debt without lying to the spine", async () => {
    const rig = makeRig();
    rig.docker.enqueue({ exitCode: 0, stdout: "", stderr: "mc-land: cleanup debt: branch remains" });
    rig.mc.enqueue(ok("{}"));
    await applyEffect(landEffect, rig.deps);
    expect(rig.mc.calls).toEqual([
      ["land", "report", "42", "--status", "success"],
    ]);
    expect(rig.logs.some((l) => l.includes("cleanup debt"))).toBe(true);
  });

  test("a failed `mc land report` is logged, not thrown", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""));
    rig.mc.enqueue(fail(1, "fencing mismatch"));
    await applyEffect(landEffect, rig.deps);
    expect(rig.logs.some((l) => l.includes("mc land report failed (exit 1)"))).toBe(true);
  });
});

describe("reap effect", () => {
	test("interrupt stops the exact run container and removes its envelope", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		await applyEffect(
			{ action: "interrupt", task_id: 42, run_id: "run-42-worker", stop_container: true },
			rig.deps,
		);
		expect(rig.docker.calls).toEqual([["stop", "mc-setup-run-42-worker"], ["stop", "mc-run-run-42-worker"]]);
		expect(rig.fakeFs.events).toEqual([
			"rm:/tmp/mc-home/runs/run-42-worker.json",
			"rm:/tmp/mc-home/runs/run-42-worker.mounts.json",
			"rm:/tmp/mc-home/runs/projections/run-42-worker",
		]);
	});

  test("stops the exact-named container when stop_container is set", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""));
    await applyEffect({ action: "reap", run_id: "run-42-worker", stop_container: true }, rig.deps);
		expect(rig.docker.calls).toEqual([["stop", "mc-setup-run-42-worker"], ["stop", "mc-run-run-42-worker"]]);
  });

  test("removes the launch envelope and plan sibling with the container (§11.3)", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""));
    await applyEffect({ action: "reap", run_id: "run-42-worker", stop_container: true }, rig.deps);
    expect(rig.fakeFs.events).toEqual([
      "rm:/tmp/mc-home/runs/run-42-worker.json",
      "rm:/tmp/mc-home/runs/run-42-worker.mounts.json",
			"rm:/tmp/mc-home/runs/projections/run-42-worker",
    ]);
  });

  test("no docker call and no fs effect when stop_container is absent", async () => {
    const rig = makeRig();
    await applyEffect({ action: "reap", run_id: "run-42-worker" }, rig.deps);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.fakeFs.events).toEqual([]);
  });

  test("a run_id outside the charset is refused (never spliced into argv)", async () => {
    const rig = makeRig();
    await applyEffect({ action: "reap", run_id: "x $(reboot)", stop_container: true }, rig.deps);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.logs.some((l) => l.includes("reap refused"))).toBe(true);
  });

  test("docker stop failure (already gone) is logged, not thrown — envelope still removed", async () => {
    const rig = makeRig();
    rig.docker.enqueue(fail(1, "No such container"));
    await applyEffect({ action: "reap", run_id: "run-42-worker", stop_container: true }, rig.deps);
	expect(rig.logs.some((l) => l.includes("docker stop mc-setup-run-42-worker exited 1"))).toBe(true);
    expect(rig.fakeFs.events).toEqual([
      "rm:/tmp/mc-home/runs/run-42-worker.json",
      "rm:/tmp/mc-home/runs/run-42-worker.mounts.json",
			"rm:/tmp/mc-home/runs/projections/run-42-worker",
    ]);
  });
});

describe("config sanity", () => {
  test("test config maps every skeleton role (contract §7 ladder)", () => {
    for (const role of ["editor", "worker", "verifier", "packager", "strategist"]) {
      expect(testConfig.roleBehaviors[role]).toBeDefined();
    }
  });
});

// Hoisted to module scope: the container-class divergence guard at the bottom
// of this file needs the same fixture, and a second copy would be free to drift
// away from the one the envelope test pins.
const landingStep: NonNullable<MountPlan["landing"]> = {
	approved_run_id: "a1b2c3d4e5f60718",
	branch: "mc/task-42",
	closure_digest: "d".repeat(64),
	cover_dest: "/repo/source/.mission-control",
	landing_id: "0011223344556677",
	local_repo_uuid: "0f9c1e2a-3b4c-4d5e-8f60-112233445566",
	object_format: "sha1",
	pinned_base_sha: "c".repeat(40),
	pre_merge_sha: "b".repeat(40),
	target_ref: "main",
	task_id: 42,
	task_root: {
		canonical: "/host/workspace/.mission-control/tasks/task-42",
		device: "16777232", inode: "9001", owner_uid: 501,
	},
	verified_sha: "a".repeat(40),
	worksource_root: {
		canonical: "/host/workspace", device: "16777232", inode: "42", owner_uid: 501,
	},
};

// The SEALED landing class (ADR-017:697-702), sibling of the legacy land
// effect above. Exercised DIRECTLY rather than through applyEffect, because no
// effect arm dispatches it yet — the seam that would is the activation step
// after this one. Testing it here pins the container envelope now, so the
// wiring step changes routing alone.
describe("sealed landing", () => {

	test("runs the landing class envelope over the four-row table, then reports success", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok("")); // the confirmed-absence probe: no such container
		rig.docker.enqueue(ok(""));
		rig.mc.enqueue(ok("{}"));
		await runSealedLanding(landingStep, rig.deps);
		expect(rig.docker.calls[0]).toEqual([
			"ps", "-aq", "--filter", "name=^mc-landing-0011223344556677$",
		]);
		expect(rig.docker.calls[1]).toEqual([
			"run", "--rm",
			"--name", "mc-landing-0011223344556677",
			"--network", "none",
			"--label", "mc-managed=true",
			"--label", "mc-component=landing",
			"--label", "mc-landing-id=0011223344556677",
			"--label", "mc-task-id=42",
			"--label", "mc-approved-run-id=a1b2c3d4e5f60718",
			"--user", "10002:10002",
			"--cap-drop", "ALL",
			"--security-opt", "no-new-privileges=true",
			"--cpus", "1", "--memory", "1024m", "--pids-limit", "128",
			"-v", "/host/workspace:/repo/source",
			"-v", "/tmp/mc-home/runs/landing-0011223344556677.cover:/repo/source/.mission-control:ro",
			"-v", "/host/workspace/.mission-control/tasks/task-42:/repo/task:ro",
			"-v", "/tmp/mc-home/runs/landing-0011223344556677.json:/mc/landing.json:ro",
			"mc-fake-e2e:test", "mc", "__land-sealed", "/mc/landing.json",
		]);
		expect(rig.docker.calls).toHaveLength(2);
		expect(rig.mc.calls).toEqual([["land", "report", "42", "--status", "success"]]);
	});

	// The cover must exist BEFORE the container runs, or `/repo/source` hands
	// out the sealed task bytes RW through the `.mission-control` alias.
	test("creates the cover directory and the envelope before the container", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		rig.docker.enqueue(ok(""));
		rig.mc.enqueue(ok("{}"));
		await runSealedLanding(landingStep, rig.deps);
		const cover = rig.fakeFs.events.indexOf("mkdir:/tmp/mc-home/runs/landing-0011223344556677.cover");
		const wrote = rig.fakeFs.events.indexOf("write:/tmp/mc-home/runs/landing-0011223344556677.json");
		expect(cover).toBeGreaterThanOrEqual(0);
		expect(wrote).toBeGreaterThanOrEqual(0);
		expect(rig.fakeFs.events).toContain("rm:/tmp/mc-home/runs/landing-0011223344556677.json");
	});

	// Landing has NO run id — it opens no Run — so every per-landing host file
	// keys on the landing id. A run-keyed path here would collide with, or be
	// swept by, the run-keyed setup machinery.
	test("keys its host files on the landing id, never a run id", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		rig.docker.enqueue(ok(""));
		rig.mc.enqueue(ok("{}"));
		await runSealedLanding(landingStep, rig.deps);
		for (const event of rig.fakeFs.events) {
			expect(event).not.toContain(".setup.json");
			expect(event).not.toContain(".mounts.json");
		}
	});

	// ADR-016:576 — "runtime failure is never mislabeled as a failed reviewed
	// change". An earlier draft reported failure on ANY nonzero exit, which
	// turned a broken image or a bad mount into a durable blocked row the
	// operator had to clear by hand. Only mc's domain-rejection exit (1) is
	// evidence about the change itself.
	test("a semantic mc-land refusal (exit 1) reports failure", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		rig.docker.enqueue(fail(1, "target moved"));
		rig.mc.enqueue(ok("{}"));
		await runSealedLanding(landingStep, rig.deps);
		expect(rig.mc.calls).toEqual([[
			"land", "report", "42", "--status", "failure",
			"--reason", "mc __land-sealed refused the merge: target moved",
		]]);
		expect(rig.logs.some((l) => l.includes("refused by mc-land"))).toBe(true);
	});

	test.each([2, 125, 126, 127])(
		"an infrastructure exit (%i) reports nothing and leaves the landing pending",
		async (exitCode) => {
			const rig = makeRig();
			rig.docker.enqueue(ok(""));
			rig.docker.enqueue(fail(exitCode, "no such image"));
			await runSealedLanding(landingStep, rig.deps);
			expect(rig.mc.calls).toEqual([]);
			expect(rig.logs.some((l) => l.includes("did not run to a verdict"))).toBe(true);
		},
	);

	// The confirmed-absence gate (ADR-016:373-375). The container name is
	// stable across attempts by construction, so a crashed prior attempt leaves
	// a container this one collides with. Without this gate the collision exits
	// nonzero and — before the classification above — reported a failure this
	// attempt never actually observed.
	// Contract §3 (orphan sweep): "setup/landing/probe residue and closed
	// derived file artifacts have exact component/action liveness and cleanup".
	//
	// The cover directory is a closed derived file artifact once the landing
	// reaches a VERDICT. It used to be left behind on the grounds that
	// ADR-016:344-349 makes a later tick responsible for landing residue — but
	// that clause is about action CONTAINERS visible to a later tick, not about
	// host directories the resident itself created, and the sweep it defers to
	// was never written. So every landing leaked one directory under
	// MC_HOME/runs, permanently.
	test("a closed landing removes its cover and envelope", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		rig.docker.enqueue(ok(""));
		rig.mc.enqueue(ok("{}"));
		await runSealedLanding(landingStep, rig.deps);
		expect(rig.fakeFs.events).toContain("rm:/tmp/mc-home/runs/landing-0011223344556677.json");
		expect(rig.fakeFs.events).toContain("rm:/tmp/mc-home/runs/landing-0011223344556677.cover");
	});

	// ...but an UNCLOSED landing keeps it. An infrastructure failure leaves the
	// landing pending, and the retry reuses this exact path because the landing
	// id is stable by construction. Removing it here would be churn, and worse,
	// it would delete the cover out from under a container that a confirmed
	// absence has not yet been established for.
	test("an infrastructure failure leaves the cover for the retry", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		rig.docker.enqueue(fail(125, "no such image"));
		await runSealedLanding(landingStep, rig.deps);
		expect(rig.mc.calls).toEqual([]);
		expect(rig.fakeFs.events).not.toContain("rm:/tmp/mc-home/runs/landing-0011223344556677.cover");
	});

	// The lane discriminator. applyEffect routes on the PRESENCE of the landing
	// carrier alone, so these two tests are what keep the legacy lane from
	// being swallowed by the sealed one and vice versa.
	test("a land effect carrying a landing routes to the sealed lane", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		rig.docker.enqueue(ok(""));
		rig.mc.enqueue(ok("{}"));
		await applyEffect({
			action: "land", task_id: 42, branch: "mc/task-42",
			verified_sha: "a".repeat(40), target_ref: "main",
			landing: landingStep,
		}, rig.deps);
		expect(rig.docker.calls[1]).toContain("__land-sealed");
	});

	test("a land effect with no landing still routes to the legacy lane", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		rig.mc.enqueue(ok("{}"));
		await applyEffect({
			action: "land", task_id: 42, branch: "mc/task-42",
			verified_sha: "a".repeat(40), target_ref: "main",
		}, rig.deps);
		expect(rig.docker.calls).toHaveLength(1);
		expect(rig.docker.calls[0]).not.toContain("__land-sealed");
	});

	test("an existing landing container aborts the attempt without reporting", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok("abc123def456\n")); // the probe finds one
		await runSealedLanding(landingStep, rig.deps);
		expect(rig.docker.calls).toHaveLength(1);
		expect(rig.mc.calls).toEqual([]);
		expect(rig.logs.some((l) => l.includes("already exists"))).toBe(true);
	});

	// A refused report is logged, never thrown: the lane must not take the
	// resident down, and the landing row stays pending for a later tick.
	test("a refused land report is logged rather than thrown", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		rig.docker.enqueue(ok(""));
		rig.mc.enqueue(fail(1, "task 42 has no branch"));
		await runSealedLanding(landingStep, rig.deps);
		expect(rig.logs.some((l) => l.includes("land report refused"))).toBe(true);
	});
});

// ---------------------------------------------------------------------------
// Row 7 (setuid gate) — the class divergence guard.
//
// The setuid gate depends on `no-new-privileges` being ABSENT from the AGENT
// container, so the setuid `mc` binary can raise privilege to reach the spine.
// The setup and landing classes are the opposite: they never touch the spine,
// so they set it and are hardened by it.
//
// Landing was the newest class to add that flag, and nothing anywhere asserts
// the two classes disagree. A perfectly reasonable future refactor — hoisting
// "the hardening flags" into one shared envelope builder because three call
// sites repeat them — would silently add `no-new-privileges` to the agent
// container and kill the setuid gate, with every existing test still green.
// This is the tripwire for that refactor.
// ---------------------------------------------------------------------------

describe("container class hardening divergence", () => {
	const NNP = "no-new-privileges=true";

	test("the agent container omits no-new-privileges so the setuid gate survives", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok("container-id"));
		rig.docker.enqueue(ok(""));
		await applyEffect(spawnEffect, rig.deps);

		const create = rig.docker.calls.find((c) => c[0] === "create");
		expect(create).toBeDefined();
		expect(create).not.toContain(NNP);
	});

	test("the landing container sets no-new-privileges", async () => {
		const rig = makeRig();
		rig.docker.enqueue(ok(""));
		rig.docker.enqueue(ok(""));
		rig.mc.enqueue(ok("{}"));
		await runSealedLanding(landingStep, rig.deps);

		const run = rig.docker.calls.find((c) => c[0] === "run");
		expect(run).toBeDefined();
		expect(run).toContain(NNP);
	});
});

describe("homie tier (lease-free)", () => {
  const cid = "c".repeat(64);
  const wake: Effect = {
    action: "homie-wake",
    session: "h-abc",
    launch: "aaaaaaaaaaaaaaaa",
    mode: "fresh",
    binding: "claude",
    container_name: "mc-homie-h-abc",
  };

  test("wake: folder → run.json → create → launch-bind(id) → start", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok("")); // rm -f any stale same-named container
    rig.docker.enqueue(ok(cid + "\n")); // create prints the 64-hex id
    rig.mc.enqueue(ok(JSON.stringify({ session: "h-abc", launch: wake.launch, container_id: cid, bound_at: "t" })));
    rig.docker.enqueue(ok("")); // start
    await applyEffect(wake, rig.deps);

    // run.json is heartbeat-free and tier "homie".
    const runJson = rig.fakeFs.writes.get(`${testConfig.mcHome}/runs/h-abc.json`);
    expect(runJson).toBeDefined();
    const parsed = JSON.parse(runJson!);
    expect(parsed.tier).toBe("homie");
    expect(parsed.run_id).toBe("h-abc");
    expect(parsed.session_id).toBe("h-abc");
    expect(parsed.launch).toBe(wake.launch);
    expect(parsed.heartbeat_interval_s).toBeUndefined();
    // Republished with the bound container id before start (the runner reads it).
    expect(parsed.container_id).toBe(cid);
    // The route key rides as `binding`; the behavior comes from config.
    expect(parsed.binding).toBe("claude");
    expect(parsed.harness_config).toEqual({ behavior: "/mc/behaviors/homie.json" });

    // docker: rm -f (clear stale name), create, then start; launch-bind between.
    expect(rig.docker.calls[0]).toEqual(["rm", "-f", "mc-homie-h-abc"]);
    const create = rig.docker.calls[1];
    expect(create[0]).toBe("create");
    expect(create).toContain("mc-tier=homie");
    expect(create).toContain("--rm");
    // Operator read-scope: the workspace is bound RO, never RW.
    expect(create).toContain(`${testConfig.workspaceRoot}:/workspace/source:ro`);
    expect(rig.docker.calls[2]).toEqual(["start", "mc-homie-h-abc"]);

    // launch-bind CASes the exact docker id onto the launch, before start.
    expect(rig.mc.calls).toEqual([
      ["homie", "launch-bind", "h-abc", "--launch", wake.launch, "--container-id", cid],
    ]);
  });

  test("wake: a fenced launch abandons the container and never starts", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok("")); // rm -f stale name
    rig.docker.enqueue(ok(cid + "\n")); // create
    rig.mc.enqueue(ok(JSON.stringify({ session: "h-abc", fenced: false })));
    rig.docker.enqueue(ok("")); // rm (abandon)
    await applyEffect(wake, rig.deps);

    expect(rig.docker.calls[1][0]).toBe("create");
    expect(rig.docker.calls[2]).toEqual(["rm", "mc-homie-h-abc"]);
    expect(rig.docker.calls.some((c) => c[0] === "start")).toBe(false);
  });

  test("wake: a failed create removes only the envelope, no bind or start", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok("")); // rm -f stale name
    rig.docker.enqueue(fail(1, "boom")); // create
    await applyEffect(wake, rig.deps);

    expect(rig.docker.calls.map((c) => c[0])).toEqual(["rm", "create"]);
    expect(rig.mc.calls).toEqual([]);
    expect(rig.fakeFs.events).toContain(`rm:${testConfig.mcHome}/runs/h-abc.json`);
  });

  test("stop: a bound container is stopped by its id", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""));
    await applyEffect({ action: "homie-stop", session: "h-abc", container_id: cid, reason: "idle" }, rig.deps);
    expect(rig.docker.calls).toEqual([["stop", cid]]);
  });

  test("stop: a pre-bind idle end stops nothing", async () => {
    const rig = makeRig();
    await applyEffect({ action: "homie-stop", session: "h-abc", reason: "idle" }, rig.deps);
    expect(rig.docker.calls).toEqual([]);
  });
});
