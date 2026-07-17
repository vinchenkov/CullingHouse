// tick-loop.test.ts — the loop's contract (docs/phase1b-contract.md §5):
// timer-driven, one tick at a time, `mc dispatch` and nothing else,
// failures logged and survived, clean shutdown. All time is fake.

import { describe, expect, test } from "bun:test";
import { startTickLoop } from "./tick-loop";
import { deferred, effectJson, fail, makeRig, ok } from "./test-helpers";

const idle = () => effectJson({ action: "idle", reason: "board empty" });

describe("timer wiring", () => {
  test("registers a repeating timer at intervalMs, no tick before it fires", () => {
    const rig = makeRig({ intervalMs: 1234 });
    startTickLoop(rig.deps);
    expect(rig.timer.intervalMs).toBe(1234);
    expect(rig.timer.fn).not.toBeNull();
    expect(rig.mc.calls).toEqual([]); // ticks come from the timer only
  });

  test("each firing runs exactly one `mc dispatch` with argv ['dispatch']", async () => {
    const rig = makeRig();
    rig.mc.enqueue(idle(), idle(), idle());
    startTickLoop(rig.deps);
    await rig.timer.fire();
    await rig.timer.fire();
    await rig.timer.fire();
    expect(rig.mc.calls).toEqual([["dispatch"], ["dispatch"], ["dispatch"]]);
  });
});

describe("one tick at a time", () => {
  test("a firing that lands mid-tick is skipped, never queued", async () => {
    const rig = makeRig();
    const gate = deferred<ReturnType<typeof ok>>();
    rig.mc.enqueue(gate.promise, idle());
    startTickLoop(rig.deps);

    const first = rig.timer.fire(); // tick 1 blocks on mc dispatch
    rig.timer.fire(); // fires mid-tick → skipped
    rig.timer.fire(); // and again → skipped, not queued
    expect(rig.mc.calls.length).toBe(1);
    expect(rig.logs.filter((l) => l.includes("tick skipped"))).toHaveLength(2);

    gate.resolve(idle());
    await first;

    await rig.timer.fire(); // next firing ticks normally
    expect(rig.mc.calls.length).toBe(2);
  });
});

describe("fail-closed tick failures", () => {
  test("mc dispatch nonzero exit is logged and the loop continues", async () => {
    const rig = makeRig();
    rig.mc.enqueue(fail(2, "spine unreachable"), idle());
    startTickLoop(rig.deps);
    await rig.timer.fire();
    expect(rig.logs.some((l) => l.includes("mc dispatch failed (exit 2)") && l.includes("spine unreachable"))).toBe(true);
    await rig.timer.fire();
    expect(rig.mc.calls.length).toBe(2); // still alive
  });

  test("unparseable effect JSON is logged and the loop continues", async () => {
    const rig = makeRig();
    rig.mc.enqueue(ok("not json at all\n"), idle());
    startTickLoop(rig.deps);
    await rig.timer.fire();
    expect(rig.logs.some((l) => l.includes("unparseable effect JSON"))).toBe(true);
    await rig.timer.fire();
    expect(rig.mc.calls.length).toBe(2);
  });

  test("a rejected exec (mc binary unspawnable) is logged, loop survives", async () => {
    const rig = makeRig();
    let first = true;
    rig.deps.runMc = async (argv) => {
      rig.mc.calls.push(argv);
      if (first) {
        first = false;
        throw new Error("ENOENT: mc not found");
      }
      return idle();
    };
    startTickLoop(rig.deps);
    await rig.timer.fire();
    expect(rig.logs.some((l) => l.includes("tick failed") && l.includes("ENOENT"))).toBe(true);
    await rig.timer.fire();
    expect(rig.mc.calls.length).toBe(2);
  });

  test("a failed effect never crashes the loop (docker create fails on spawn)", async () => {
    const rig = makeRig();
    rig.mc.enqueue(
	      effectJson({
	        action: "spawn", run_id: "r1", role: "worker", subject_id: 1,
	        worksource: "ws-e2e", pool_ids: [1], harness: "fake",
	        model_binding: "fake", brief: "immutable brief",
	        mount_plan: { entries: [], version: 1 }, heartbeat_interval_s: 1,
	      }),
      idle(),
    );
    rig.docker.enqueue(fail(125, "docker daemon down"));
    startTickLoop(rig.deps);
    await rig.timer.fire();
    expect(rig.logs.some((l) => l.includes("docker create failed (exit 125)"))).toBe(true);
    await rig.timer.fire();
    expect(rig.mc.calls.length).toBe(2);
  });
});

describe("clean shutdown", () => {
  test("stop() clears the timer and later firings do nothing", async () => {
    const rig = makeRig();
    const handle = startTickLoop(rig.deps);
    await handle.stop();
    expect(rig.timer.cleared).toBe(true);
    await rig.timer.fire(); // even if a stale firing arrives: no tick
    expect(rig.mc.calls).toEqual([]);
  });

  test("stop() during an in-flight tick waits for that tick to finish", async () => {
    const rig = makeRig();
    const gate = deferred<ReturnType<typeof ok>>();
    rig.mc.enqueue(gate.promise);
    const handle = startTickLoop(rig.deps);
    const inFlight = rig.timer.fire();

    let stopResolved = false;
    const stopP = handle.stop().then(() => {
      stopResolved = true;
    });
    await Promise.resolve(); // give stop a chance to (wrongly) resolve early
    await Promise.resolve();
    expect(stopResolved).toBe(false);

    gate.resolve(idle());
    await inFlight;
    await stopP;
    expect(stopResolved).toBe(true);
  });

  test("stop() is idempotent", async () => {
    const rig = makeRig();
    const handle = startTickLoop(rig.deps);
    await handle.stop();
    await handle.stop();
    expect(rig.timer.cleared).toBe(true);
  });
});
