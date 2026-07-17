// effects.test.ts — the effectors against stubbed mc/docker/fs
// (docs/phase1b-contract.md §5): spawn's folder → run.json → container
// order, run.json schema (§6), docker argv (§1 rows), the land →
// `mc land report` handoff, exact-name reap, and idle/reenter as no-ops.

import { describe, expect, test } from "bun:test";
import { applyEffect } from "./effects";
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

  test("post-claim task precreate registers the returned identity before setup or launch", async () => {
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
      registerTaskRoot: async (runId, identity) => {
        events.push(`register:${runId}`);
        expect(identity).toBe(registered);
      },
    });
    await applyEffect({
      ...spawnEffect,
      mount_plan: {
        entries: [],
        task_precreate: {
          child_mode: 0o700,
          task_id: 42,
          tasks_parent: {
            canonical: "/host/workspace/.mission-control/tasks",
            device: "8",
            inode: "88",
            owner_uid: 501,
          },
          workspace_root: "/host/workspace",
        },
        version: 1,
      },
    }, rig.deps);
	expect(events).toEqual(["recheck:42", "precreate:42", "register:run-42-worker"]);
    expect(rig.fakeFs.events).toEqual([]);
    expect(rig.docker.calls).toEqual([]);
    expect(rig.logs.some((line) => line.includes("task root registered") && line.includes("setup pending"))).toBe(true);
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
        task_precreate: {
          child_mode: 0o700,
          task_id: 42,
          tasks_parent: {
            canonical: "/host/workspace/.mission-control/tasks",
            device: "8",
            inode: "88",
            owner_uid: 501,
          },
          workspace_root: "/host/workspace",
        },
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
			task_precreate: {
				child_mode: 0o700, task_id: 42,
				tasks_parent: {
					canonical: "/host/workspace/.mission-control/tasks",
					device: "8", inode: "88", owner_uid: 501,
				},
				workspace_root: "/host/workspace",
			},
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
        task_precreate: {
          child_mode: 0o700, task_id: 42,
          tasks_parent: {
            canonical: "/host/workspace/.mission-control/tasks",
            device: "8", inode: "88", owner_uid: 501,
          },
          workspace_root: "/host/workspace",
        },
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
        task_precreate: {
          child_mode: 0o700, task_id: 42,
          tasks_parent: {
            canonical: "/host/workspace/.mission-control/tasks",
            device: "8", inode: "88", owner_uid: 501,
          },
          workspace_root: "/host/workspace",
        },
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
        task_precreate: {
          child_mode: 0o777, task_id: 42,
          tasks_parent: {
            canonical: "/host/workspace/.mission-control/tasks",
            device: "8", inode: "88", owner_uid: 501,
          },
          workspace_root: "/host/workspace",
        },
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
        "--label", "mc-managed",
        "--label", "mc-tier=pipeline",
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
		expect(rig.docker.calls).toEqual([["stop", "mc-run-run-42-worker"]]);
		expect(rig.fakeFs.events).toEqual([
			"rm:/tmp/mc-home/runs/run-42-worker.json",
			"rm:/tmp/mc-home/runs/run-42-worker.mounts.json",
		]);
	});

  test("stops the exact-named container when stop_container is set", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""));
    await applyEffect({ action: "reap", run_id: "run-42-worker", stop_container: true }, rig.deps);
    expect(rig.docker.calls).toEqual([["stop", "mc-run-run-42-worker"]]);
  });

  test("removes the launch envelope and plan sibling with the container (§11.3)", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""));
    await applyEffect({ action: "reap", run_id: "run-42-worker", stop_container: true }, rig.deps);
    expect(rig.fakeFs.events).toEqual([
      "rm:/tmp/mc-home/runs/run-42-worker.json",
      "rm:/tmp/mc-home/runs/run-42-worker.mounts.json",
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
    expect(rig.logs.some((l) => l.includes("docker stop exited 1"))).toBe(true);
    expect(rig.fakeFs.events).toEqual([
      "rm:/tmp/mc-home/runs/run-42-worker.json",
      "rm:/tmp/mc-home/runs/run-42-worker.mounts.json",
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
