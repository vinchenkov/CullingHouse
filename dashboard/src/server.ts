// server.ts — Bun.serve wiring for the dashboard (ADR-024 D1/D4). Binds
// loopback 127.0.0.1:7333 by default; a non-loopback bind is refused at
// startup unless an auth token is configured, and a configured token is
// demanded on every /api request before any mc spawn (spec §15.7,
// fail-closed; the static shell carries no data and stays open so a browser
// can load it and then present the token). Every request must also pass the
// browser boundary: a Host header naming this server (defeats DNS
// rebinding) and, when an Origin header is present, that same origin
// (defeats cross-site request forgery from pages the operator visits).
// Static UI files are served from src/ui/; everything under /api/ is the
// verb mirror in api.ts.

import { handleApi, type ApiDeps } from "./api";

export const DEFAULT_BIND = "127.0.0.1:7333";

const LOOPBACK_HOSTS = new Set(["127.0.0.1", "::1", "localhost"]);

const STATIC_FILES: Record<string, { path: string; type: string }> = {
  "/": { path: "ui/index.html", type: "text/html; charset=utf-8" },
  "/app.js": { path: "ui/app.js", type: "text/javascript; charset=utf-8" },
  "/style.css": { path: "ui/style.css", type: "text/css; charset=utf-8" },
};

export interface DashboardServerOpts {
  /** host:port; defaults to 127.0.0.1:7333. Port 0 = ephemeral (tests). */
  bind?: string;
  authToken?: string;
}

export interface DashboardServerHandle {
  url: string;
  port: number;
  stop(): void;
}

export function parseBind(bind: string): { hostname: string; port: number } {
  const at = bind.lastIndexOf(":");
  const rawPort = at === -1 ? "" : bind.slice(at + 1);
  const port = Number(rawPort);
  if (at === -1 || rawPort === "" || !Number.isInteger(port) || port < 0 || port > 65535) {
    throw new Error(`dashboard bind must be host:port, got ${JSON.stringify(bind)}`);
  }
  const hostname = bind.slice(0, at).replace(/^\[|\]$/g, "");
  if (hostname === "") {
    throw new Error(`dashboard bind must name a host, got ${JSON.stringify(bind)}`);
  }
  return { hostname, port };
}

/** The hostname a browser may name in Host/Origin for this server: the bind
 * host plus the loopback spellings (a loopback bind is reachable as any of
 * them). Anything else is a rebound DNS name or a foreign origin. */
function hostAllowed(rawHost: string | null, bindHost: string): boolean {
  if (rawHost === null) {
    return false;
  }
  const host = rawHost.replace(/:\d+$/, "").replace(/^\[|\]$/g, "").toLowerCase();
  return host === bindHost.toLowerCase() || LOOPBACK_HOSTS.has(host);
}

export function startDashboardServer(
  opts: DashboardServerOpts,
  deps: ApiDeps,
): DashboardServerHandle {
  const { hostname, port } = parseBind(opts.bind ?? DEFAULT_BIND);
  if (!LOOPBACK_HOSTS.has(hostname) && !opts.authToken) {
    throw new Error(
      `refusing non-loopback bind ${hostname}: set an auth token to serve beyond loopback (fail-closed)`,
    );
  }
  const server = Bun.serve({
    hostname,
    port,
    async fetch(req) {
      if (!hostAllowed(req.headers.get("host"), hostname)) {
        return Response.json(
          { error: { code: "forbidden", message: "request names a foreign host" } },
          { status: 403 },
        );
      }
      const origin = req.headers.get("origin");
      if (origin !== null) {
        let originHost: string | null = null;
        try {
          originHost = new URL(origin).hostname;
        } catch {
          originHost = null;
        }
        if (originHost === null || !hostAllowed(originHost, hostname)) {
          return Response.json(
            { error: { code: "forbidden", message: "cross-origin requests are refused" } },
            { status: 403 },
          );
        }
      }
      const url = new URL(req.url);
      if (opts.authToken !== undefined && url.pathname.startsWith("/api/")) {
        if (req.headers.get("authorization") !== `Bearer ${opts.authToken}`) {
          return Response.json(
            { error: { code: "unauthorized", message: "missing or wrong bearer token" } },
            { status: 401 },
          );
        }
      }
      const api = await handleApi(req, url, deps);
      if (api !== null) {
        return api;
      }
      const entry = STATIC_FILES[url.pathname];
      if (entry === undefined || req.method !== "GET") {
        return new Response("not found", { status: 404 });
      }
      const file = Bun.file(`${import.meta.dir}/${entry.path}`);
      if (!(await file.exists())) {
        return new Response("not found", { status: 404 });
      }
      return new Response(file, { headers: { "content-type": entry.type } });
    },
  });
  return {
    url: `http://${hostname}:${server.port}/`,
    port: server.port as number,
    stop() {
      server.stop(true);
    },
  };
}
