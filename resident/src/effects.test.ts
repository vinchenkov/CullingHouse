// effects.test.ts — the effectors against stubbed mc/docker/fs
// (docs/phase1b-contract.md §5): spawn's folder → run.json → container
// order, run.json schema (§6), docker argv (§1 rows), the land →
// `mc land report` handoff, exact-name reap, and idle/reenter as no-ops.

import { describe, expect, test } from "bun:test";
import { applyEffect, requireAcceptedSealProducerAbsent } from "./effects";
import { fail, makeRig, ok, testConfig } from "./test-helpers";
import type { Effect } from "./types";

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
			["task", "accepted-seal-record", "--run", "run-42-verifier", "--workspace", "/host/workspace", "--result", setupResultJson + "\n"],
			["task", "accepted-seal-continue", "--run", "run-42-verifier"],
		]);
		expect(rig.fakeFs.events).toContain("write:/tmp/mc-home/runs/run-42-verifier.setup.json");
		expect(rig.fakeFs.events).toContain("rm:/tmp/mc-home/runs/run-42-verifier.setup.json");
		expect(rechecked).toBe(true);
		expect(rig.logs.some((line) => line.includes("accepted-seal rebuild recorded and continued"))).toBe(true);
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
