// effects.test.ts — the effectors against stubbed mc/docker/fs
// (docs/phase1b-contract.md §5): spawn's folder → run.json → container
// order, run.json schema (§6), docker argv (§1 rows), the land →
// `mc land report` handoff, exact-name reap, and idle/reenter as no-ops.

import { describe, expect, test } from "bun:test";
import { applyEffect } from "./effects";
import { fail, makeRig, ok, testConfig } from "./test-helpers";
import type { Effect } from "./types";

const spawnEffect: Effect = {
  action: "spawn",
  run_id: "run-42-worker",
  role: "worker",
  subject_id: 42,
  worksource: "ws-e2e",
  pool_ids: [42],
  harness: "fake",
  model_binding: "fake",
  session_path: "sessions/run-42-worker",
  heartbeat_interval_s: 1,
};

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
      "docker:run",
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
      brief: runJson.brief, // free text; asserted separately below
      pool_ids: [42],
      heartbeat_interval_s: 1,
      harness_config: { behavior: "/mc/behaviors/worker.json" },
      mounts: { session: "/mc/session", workspace: "/workspace/source" },
    });
    expect(typeof runJson.brief).toBe("string");
    expect(runJson.brief.length).toBeGreaterThan(0);
  });

  test("subjectless spawn (strategist) writes subject_id null", async () => {
    const rig = makeRig();
    await applyEffect(
      {
        action: "spawn", run_id: "run-9-strategist", role: "strategist",
        subject_id: null, worksource: "ws-e2e", pool_ids: [],
        harness: "fake", model_binding: "fake",
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

  test("docker run argv: detached, --rm, --network none, exact name, labels, mounts, MC_SPINE", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok("container-id\n"));
    await applyEffect(spawnEffect, rig.deps);
    expect(rig.docker.calls).toEqual([
      [
        "run", "-d", "--rm",
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
  test("stops the exact-named container when stop_container is set", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""));
    await applyEffect({ action: "reap", run_id: "run-42-worker", stop_container: true }, rig.deps);
    expect(rig.docker.calls).toEqual([["stop", "mc-run-run-42-worker"]]);
  });

  test("removes the launch envelope with the container (§11.3)", async () => {
    const rig = makeRig();
    rig.docker.enqueue(ok(""));
    await applyEffect({ action: "reap", run_id: "run-42-worker", stop_container: true }, rig.deps);
    expect(rig.fakeFs.events).toEqual(["rm:/tmp/mc-home/runs/run-42-worker.json"]);
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
    expect(rig.fakeFs.events).toEqual(["rm:/tmp/mc-home/runs/run-42-worker.json"]);
  });
});

describe("config sanity", () => {
  test("test config maps every skeleton role (contract §7 ladder)", () => {
    for (const role of ["editor", "worker", "verifier", "packager", "strategist"]) {
      expect(testConfig.roleBehaviors[role]).toBeDefined();
    }
  });
});
