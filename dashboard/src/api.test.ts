// api.test.ts — the API is a thin verb mirror (ADR-024 D3), the channel ref
// is derived not stored (D5), and the outbox drains trivially (D6).

import { describe, expect, test } from "bun:test";

import { handleApi, makeGenRef } from "./api";
import { FakeMc, apiReq, deps, domain, environment, ok } from "./test-helpers";

const listWithSession = (id: string, bindings: Record<string, unknown>[]) =>
  ok({
    sessions: [
      { id, status: "active", active_bindings: bindings },
      { id: "h-other", status: "active", active_bindings: [{ surface: "dashboard", channel_ref: "dash-other" }] },
    ],
  });

describe("verb mirror", () => {
  test("GET /api/sessions passes mc homie list through", async () => {
    const mc = new FakeMc().on(["homie", "list"], ok({ sessions: [{ id: "h-1" }] }));
    const res = await handleApi(...apiReq("GET", "/api/sessions"), deps({ mc }));
    expect(res?.status).toBe(200);
    expect(await res?.json()).toEqual({ sessions: [{ id: "h-1" }] });
    expect(mc.calls).toEqual([["homie", "list"]]);
  });

  test("GET /api/sessions/:id/history passes through", async () => {
    const mc = new FakeMc().on(
      ["homie", "history", "h-1"],
      ok({ session_id: "h-1", status: "active", messages: [] }),
    );
    const res = await handleApi(...apiReq("GET", "/api/sessions/h-1/history"), deps({ mc }));
    expect(res?.status).toBe(200);
    expect(mc.calls).toEqual([["homie", "history", "h-1"]]);
  });

  test("POST /api/sessions starts with a generated dashboard ref", async () => {
    const mc = new FakeMc().on(["homie", "start"], ok({ session_id: "h-new", status: "active" }));
    const res = await handleApi(...apiReq("POST", "/api/sessions"), deps({ mc }));
    expect(res?.status).toBe(200);
    expect(mc.calls).toEqual([["homie", "start", "--from", "dashboard:dash-feedf00d"]]);
  });

  test("a domain rejection surfaces as 400 with the mc envelope", async () => {
    const mc = new FakeMc().on(["homie", "history"], domain('unknown Homie session "h-x"'));
    const res = await handleApi(...apiReq("GET", "/api/sessions/h-x/history"), deps({ mc }));
    expect(res?.status).toBe(400);
    const body = (await res?.json()) as { error: { code: string; message: string } };
    expect(body.error.code).toBe("domain-rejection");
  });

  test("an environment failure surfaces as 500 without leaking argv advice", async () => {
    const mc = new FakeMc().on(["homie", "list"], environment("spine unreachable"));
    const res = await handleApi(...apiReq("GET", "/api/sessions"), deps({ mc }));
    expect(res?.status).toBe(500);
    const body = (await res?.json()) as { error: { code: string; message: string } };
    expect(body.error.code).toBe("environment");
    expect(body.error.message).not.toContain("mc ");
  });

  test("unknown api endpoints are 404, non-api paths fall through as null", async () => {
    const res = await handleApi(...apiReq("GET", "/api/nope"), deps());
    expect(res?.status).toBe(404);
    expect(await handleApi(...apiReq("GET", "/"), deps())).toBeNull();
    expect(await handleApi(...apiReq("GET", "/app.js"), deps())).toBeNull();
  });
});

describe("channel ref derivation (D5)", () => {
  test("send reuses the session's existing dashboard binding", async () => {
    const mc = new FakeMc()
      .on(["homie", "list"], listWithSession("h-1", [
        { surface: "cli", channel_ref: "term" },
        { surface: "dashboard", channel_ref: "dash-11223344", bound_at: "t" },
      ]))
      .on(["homie", "send"], ok({ session_id: "h-1", message_id: 7, seq: 3, echoes: 1 }));
    const res = await handleApi(
      ...apiReq("POST", "/api/sessions/h-1/send", { body: "hello" }),
      deps({ mc }),
    );
    expect(res?.status).toBe(200);
    expect(mc.calls[1]).toEqual([
      "homie", "send", "h-1", "--from", "dashboard:dash-11223344", "--body", "hello",
    ]);
  });

  test("send generates a fresh ref when the session has no dashboard binding", async () => {
    const mc = new FakeMc()
      .on(["homie", "list"], listWithSession("h-1", [{ surface: "cli", channel_ref: "term" }]))
      .on(["homie", "send"], ok({ session_id: "h-1", message_id: 7, seq: 1, echoes: 0 }));
    await handleApi(...apiReq("POST", "/api/sessions/h-1/send", { body: "hello" }), deps({ mc }));
    expect(mc.calls[1]?.[4]).toBe("dashboard:dash-feedf00d");
  });

  test("resume derives the ref the same way", async () => {
    const mc = new FakeMc()
      .on(["homie", "list"], ok({ sessions: [] }))
      .on(["homie", "resume"], ok({ session_id: "h-1", status: "active", resumed: true }));
    await handleApi(...apiReq("POST", "/api/sessions/h-1/resume"), deps({ mc }));
    expect(mc.calls[1]).toEqual(["homie", "resume", "h-1", "--from", "dashboard:dash-feedf00d"]);
  });

  test("send without a body is a usage 400 and spawns nothing", async () => {
    const mc = new FakeMc();
    const res = await handleApi(...apiReq("POST", "/api/sessions/h-1/send", {}), deps({ mc }));
    expect(res?.status).toBe(400);
    expect(mc.calls).toEqual([]);
  });

  test("makeGenRef mints dash-<8 hex>", () => {
    const ref = makeGenRef()();
    expect(ref).toMatch(/^dash-[0-9a-f]{8}$/);
  });
});

describe("outbox drain (D6)", () => {
  test("drains every polled row and reports the count", async () => {
    const mc = new FakeMc()
      .on(["outbox", "poll"], ok({
        surface: "dashboard",
        rows: [
          { id: 4, kind: "homie_reply", payload: {} },
          { id: 9, kind: "homie_echo", payload: {} },
        ],
      }))
      .on(["outbox", "ack"], ok({ acked: true }));
    const res = await handleApi(...apiReq("POST", "/api/outbox/drain"), deps({ mc }));
    expect(await res?.json()).toEqual({ polled: 2, drained: 2 });
    expect(mc.calls).toEqual([
      ["outbox", "poll", "--surface", "dashboard", "--limit", "100"],
      ["outbox", "ack", "4", "--surface", "dashboard"],
      ["outbox", "ack", "9", "--surface", "dashboard"],
    ]);
  });

  test("a failed ack is logged and skipped, not fatal (at-least-once)", async () => {
    const lines: string[] = [];
    const mc = new FakeMc()
      .on(["outbox", "poll"], ok({ surface: "dashboard", rows: [{ id: 4 }, { id: 9 }] }))
      .on(["outbox", "ack", "4"], domain("outbox row 4 belongs to surface \"cli\""))
      .on(["outbox", "ack", "9"], ok({ acked: true }));
    const res = await handleApi(
      ...apiReq("POST", "/api/outbox/drain"),
      deps({ mc, log: (l) => lines.push(l) }),
    );
    expect(await res?.json()).toEqual({ polled: 2, drained: 1 });
    expect(lines).toHaveLength(1);
  });
});
