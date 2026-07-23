// runner.test.ts — unit tests for the agent runner's pure helpers. The
// process flow (exit-code passthrough, register-session-once, nonfatal mc
// failures) is covered subprocess-level by runner-process.test.ts; the
// Docker e2e exercises only exit-0 harness runs.
// Docker-free, token-free: part of the fast suite.

import { describe, expect, test } from "bun:test";
import { adapterInvocation, harnessEnv, makeLineSplitter, nativeLocator, type RunEnvelope } from "./main";

const envelope = (over: Partial<RunEnvelope>): RunEnvelope => ({
  run_id: "abc123",
  role: "worker",
	harness: "fake",
	model_binding: "fake",
  subject_id: 42,
  pool_ids: [],
  heartbeat_interval_s: 1,
  brief: "b",
  harness_config: { behavior: "/mc/behaviors/worker.json" },
  mounts: { session: "/mc/session" },
  ...over,
});

describe("harnessEnv (the env-interpolation channel, contract §4)", () => {
  test("exports run id, role, subject", () => {
    const env = harnessEnv(envelope({}), {});
    expect(env["MC_RUN_ID"]).toBe("abc123");
    expect(env["MC_ROLE"]).toBe("worker");
    expect(env["MC_SUBJECT_ID"]).toBe("42");
  });

  test("null subject exports as the empty string", () => {
    const env = harnessEnv(envelope({ subject_id: null, role: "strategist(propose)" }), {});
    expect(env["MC_SUBJECT_ID"]).toBe("");
    expect(env["MC_ROLE"]).toBe("strategist(propose)");
  });

  test("pool ids are comma-joined; absent pool is empty", () => {
    expect(harnessEnv(envelope({ pool_ids: [7, 8, 9] }), {})["MC_POOL_IDS"]).toBe("7,8,9");
    expect(harnessEnv(envelope({ pool_ids: undefined }), {})["MC_POOL_IDS"]).toBe("");
  });

  test("base env (MC_SPINE et al.) passes through; MC_* keys win over base", () => {
    const env = harnessEnv(envelope({}), {
      MC_SPINE: "/mc/spine/spine.db",
      PATH: "/usr/local/bin",
      MC_ROLE: "forged",
    });
    expect(env["MC_SPINE"]).toBe("/mc/spine/spine.db");
    expect(env["PATH"]).toBe("/usr/local/bin");
    expect(env["MC_ROLE"]).toBe("worker");
  });
});

describe("makeLineSplitter", () => {
  test("reassembles lines across arbitrary chunk boundaries", () => {
    const got: string[] = [];
    const feed = makeLineSplitter((l) => got.push(l));
    feed('{"event":"sess');
    feed('ion-start"}\n{"event":"turn-');
    feed('complete"}\n');
    expect(got).toEqual(['{"event":"session-start"}', '{"event":"turn-complete"}']);
  });

  test("a trailing partial line is held, never emitted", () => {
    const got: string[] = [];
    const feed = makeLineSplitter((l) => got.push(l));
    feed("a\nb");
    expect(got).toEqual(["a"]);
  });

  test("several lines in one chunk all emit, in order", () => {
    const got: string[] = [];
    const feed = makeLineSplitter((l) => got.push(l));
    feed("1\n2\n3\n");
    expect(got).toEqual(["1", "2", "3"]);
  });
});

describe("closed adapter selection", () => {
  test("the three production bindings select their real adapter", () => {
    expect(adapterInvocation(envelope({ harness: "codex", model_binding: "chatgpt", harness_config: undefined })).argv)
      .toContain("/app/src/agent-runner/adapters/codex.ts");
    expect(adapterInvocation(envelope({ harness: "claude-sdk", model_binding: "claude", harness_config: undefined })).argv)
      .toContain("/app/src/agent-runner/adapters/claude.ts");
    expect(adapterInvocation(envelope({ harness: "claude-sdk", model_binding: "minimax", harness_config: undefined })).argv)
      .toEqual(expect.arrayContaining(["--binding", "minimax"]));
  });

  test("an explicit test route selects the fake stand-in and unknown routes refuse", () => {
    const run = envelope({ harness: "codex", model_binding: "chatgpt" });
    expect(adapterInvocation(run, "codex/chatgpt").defaultNativeFile).toBe("native.jsonl");
    expect(() => adapterInvocation(envelope({ harness: "unknown", model_binding: "unknown" }))).toThrow("unsupported runtime route");
  });

  test("native resume and trace locators are fail-closed", () => {
    expect(() => adapterInvocation(envelope({
      harness: "codex", model_binding: "chatgpt", mode: "native", harness_config: undefined,
    }))).toThrow("native_session_ref");
    expect(nativeLocator("2026/07/native.jsonl")).toBe("2026/07/native.jsonl");
    expect(() => nativeLocator("../escape.jsonl")).toThrow("safe native trace locator");
  });
});
