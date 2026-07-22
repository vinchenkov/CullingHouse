// server.test.ts — bind/auth are fail-closed (ADR-024 D4) and the server
// serves the static Console shell plus the API mirror (D1/D3).

import { describe, expect, test } from "bun:test";

import { parseBind, startDashboardServer } from "./server";
import { FakeMc, deps, ok } from "./test-helpers";

const ephemeral = { bind: "127.0.0.1:0" };

describe("bind grammar", () => {
  test("parses host:port and bracketed IPv6", () => {
    expect(parseBind("127.0.0.1:7333")).toEqual({ hostname: "127.0.0.1", port: 7333 });
    expect(parseBind("[::1]:7333")).toEqual({ hostname: "::1", port: 7333 });
  });

  test("refuses a portless or hostless bind", () => {
    expect(() => parseBind("127.0.0.1")).toThrow("host:port");
    expect(() => parseBind(":7333")).toThrow("host");
    expect(() => parseBind("127.0.0.1:notaport")).toThrow("host:port");
  });
});

describe("fail-closed bind/auth (D4)", () => {
  test("a non-loopback bind without a token is refused at startup", () => {
    expect(() => startDashboardServer({ bind: "0.0.0.0:0" }, deps())).toThrow("non-loopback");
  });

  test("a configured token gates every request before any mc spawn", async () => {
    const mc = new FakeMc().on(["homie", "list"], ok({ sessions: [] }));
    const server = startDashboardServer({ ...ephemeral, authToken: "s3cret" }, deps({ mc }));
    try {
      const bare = await fetch(`${server.url}api/sessions`);
      expect(bare.status).toBe(401);
      expect(mc.calls).toEqual([]);
      const authed = await fetch(`${server.url}api/sessions`, {
        headers: { authorization: "Bearer s3cret" },
      });
      expect(authed.status).toBe(200);
      expect(mc.calls).toEqual([["homie", "list"]]);
    } finally {
      server.stop();
    }
  });

  test("loopback without a token serves openly (the sanctioned default)", async () => {
    const mc = new FakeMc().on(["homie", "list"], ok({ sessions: [] }));
    const server = startDashboardServer(ephemeral, deps({ mc }));
    try {
      const res = await fetch(`${server.url}api/sessions`);
      expect(res.status).toBe(200);
    } finally {
      server.stop();
    }
  });
});

describe("static shell (D1/D8)", () => {
  test("serves the Console page at / and the app module", async () => {
    const server = startDashboardServer(ephemeral, deps());
    try {
      const page = await fetch(server.url);
      expect(page.status).toBe(200);
      expect(page.headers.get("content-type")).toContain("text/html");
      const html = await page.text();
      expect(html).toContain("Console");
      const js = await fetch(`${server.url}app.js`);
      expect(js.status).toBe(200);
    } finally {
      server.stop();
    }
  });

  test("anything else is 404, never a directory listing", async () => {
    const server = startDashboardServer(ephemeral, deps());
    try {
      expect((await fetch(`${server.url}ui/`)).status).toBe(404);
      expect((await fetch(`${server.url}..%2fserver.ts`)).status).toBe(404);
    } finally {
      server.stop();
    }
  });
});
