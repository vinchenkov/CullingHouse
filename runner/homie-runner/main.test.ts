// main.test.ts — unit tests for the homie runner's pure helpers and the
// lease-free claim→reply loop, driven through injected mc/harness seams.
// Docker-free, token-free: part of the fast suite.

import { describe, expect, test } from "bun:test";
import {
  makeLineSplitter,
  nativeIdFromEvent,
  replyFromEvent,
  runHomieLoop,
  type HomieEnvelope,
  type RunnerDeps,
} from "./main";

const envelope = (over: Partial<HomieEnvelope> = {}): HomieEnvelope => ({
  run_id: "h-abc",
  session_id: "h-abc",
  tier: "homie",
  launch: "aaaaaaaaaaaaaaaa",
  container_id: "c".repeat(64),
  binding: "fake/fake",
  mode: "fresh",
  harness_config: { behavior: "/mc/behaviors/homie.json" },
  mounts: { session: "/mc/session" },
  ...over,
});

/** A scripted mc: answers from a per-argv[1..2] queue, records every call. */
class FakeMc {
  calls: string[][] = [];
  private answers: (Record<string, unknown> | null)[] = [];
  enqueue(...a: (Record<string, unknown> | null)[]): void {
    this.answers.push(...a);
  }
  mcJSON = async (argv: string[]): Promise<Record<string, unknown> | null> => {
    this.calls.push(argv);
    return this.answers.length > 0 ? this.answers.shift()! : {};
  };
}

function deps(over: Partial<RunnerDeps> = {}): RunnerDeps {
  const logs: string[] = [];
  return {
    mcJSON: async () => ({}),
    runHarnessTurn: async () => ({ reply: "ok" }),
    sleep: async () => {},
    log: (m) => logs.push(m),
    maxIdlePolls: 2,
    pollMs: 0,
    ...over,
  };
}

describe("replyFromEvent", () => {
  test("extracts a successful turn-complete output", () => {
    expect(replyFromEvent(JSON.stringify({ event: "turn-complete", status: "success", output: "hi there" }))).toBe("hi there");
  });
  test("ignores non-completion and failed events", () => {
    expect(replyFromEvent(JSON.stringify({ event: "session-start" }))).toBeUndefined();
    expect(replyFromEvent(JSON.stringify({ event: "turn-complete", status: "error", output: "x" }))).toBeUndefined();
  });
  test("ignores non-JSON lines", () => {
    expect(replyFromEvent("not json")).toBeUndefined();
  });
});

describe("nativeIdFromEvent", () => {
  test("extracts the session-start native id", () => {
    expect(nativeIdFromEvent(JSON.stringify({ event: "session-start", session_id: "abc" }))).toBe("abc");
  });
  test("ignores other events and non-JSON", () => {
    expect(nativeIdFromEvent(JSON.stringify({ event: "turn-complete" }))).toBeUndefined();
    expect(nativeIdFromEvent("x")).toBeUndefined();
  });
});

describe("makeLineSplitter", () => {
  test("reassembles across chunk boundaries and holds a partial tail", () => {
    const lines: string[] = [];
    const split = makeLineSplitter((l) => lines.push(l));
    split("a");
    split("bc\nde");
    expect(lines).toEqual(["abc"]);
    split("f\n");
    expect(lines).toEqual(["abc", "def"]);
  });
});

describe("runHomieLoop", () => {
  test("boot: reports runner-started with launch + container id", async () => {
    const mc = new FakeMc();
    mc.enqueue({}, { message: null }, { message: null }); // started, then two empty polls
    await runHomieLoop(envelope(), deps({ mcJSON: mc.mcJSON }));
    expect(mc.calls[0]).toEqual([
      "homie", "runner-started", "h-abc", "--launch", "aaaaaaaaaaaaaaaa", "--container-id", "c".repeat(64),
    ]);
  });

  test("a fenced runner-started exits 0 without claiming", async () => {
    const mc = new FakeMc();
    mc.enqueue({ fenced: false });
    const code = await runHomieLoop(envelope(), deps({ mcJSON: mc.mcJSON }));
    expect(code).toBe(0);
    expect(mc.calls.map((c) => c[1])).toEqual(["runner-started"]); // never claimed
  });

  test("claims a turn, runs the harness, posts the reply, then idles out", async () => {
    const mc = new FakeMc();
    mc.enqueue(
      {}, // runner-started
      { message: { id: 7, seq: 1, surface: "dashboard", channel_ref: "c1", body: "ping", attachments: [] } },
      {}, // reply ack
      { message: null }, // idle poll 1
      { message: null }, // idle poll 2 -> exit
    );
    const turns: string[] = [];
    const code = await runHomieLoop(
      envelope(),
      deps({ mcJSON: mc.mcJSON, runHarnessTurn: async (_r, t) => { turns.push(t); return { reply: "pong" }; } }),
    );
    expect(code).toBe(0);
    expect(turns).toEqual(["ping"]);
    const reply = mc.calls.find((c) => c[1] === "reply");
    expect(reply).toEqual(["homie", "reply", "h-abc", "--to", "7", "--body", "pong"]);
  });

  test("registers the native locator once on the first observed session-start", async () => {
    const mc = new FakeMc();
    mc.enqueue(
      {}, // runner-started
      { message: { id: 1, seq: 1, surface: "dashboard", channel_ref: null, body: "a", attachments: [] } },
      {}, // register-session ack
      {}, // reply ack
      { message: { id: 2, seq: 2, surface: "dashboard", channel_ref: null, body: "b", attachments: [] } },
      {}, // reply ack (no second register)
      { message: null }, { message: null }, // idle out
    );
    const code = await runHomieLoop(
      envelope(),
      deps({ mcJSON: mc.mcJSON, runHarnessTurn: async () => ({ reply: "r", nativeSessionId: "native-xyz" }) }),
    );
    expect(code).toBe(0);
    const regs = mc.calls.filter((c) => c[0] === "run" && c[1] === "register-session");
    expect(regs).toEqual([
      ["run", "register-session", "h-abc", "--native-ref", "native-xyz", "--file", "native.jsonl"],
    ]);
  });

  test("a harness that yields no reply leaves the turn claimed and exits 1", async () => {
    const mc = new FakeMc();
    mc.enqueue({}, { message: { id: 9, seq: 1, surface: "dashboard", channel_ref: null, body: "x", attachments: [] } });
    const code = await runHomieLoop(
      envelope(),
      deps({ mcJSON: mc.mcJSON, runHarnessTurn: async () => ({ reply: null }) }),
    );
    expect(code).toBe(1);
    expect(mc.calls.some((c) => c[1] === "reply")).toBe(false);
  });

  test("idle-out fires after exactly maxIdlePolls empty claims", async () => {
    const mc = new FakeMc();
    mc.enqueue({}, { message: null }, { message: null }, { message: null });
    let slept = 0;
    const code = await runHomieLoop(
      envelope(),
      deps({ mcJSON: mc.mcJSON, sleep: async () => { slept++; }, maxIdlePolls: 3 }),
    );
    expect(code).toBe(0);
    expect(mc.calls.filter((c) => c[1] === "claim").length).toBe(3);
    expect(slept).toBe(3);
  });
});
