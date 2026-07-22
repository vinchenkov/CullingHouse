// api.ts — the dashboard's HTTP API: a thin one-to-one mirror of `mc` verbs
// (ADR-024 D3). The server adds no interpretation, no cache, and no durable
// state; free operator text travels only as the body of `homie send`
// (spec §15.2). Verb failures pass through as the mc error envelope — 400
// for a domain rejection, 500 for an environment failure — and error text
// never instructs a human to run an mc command (§17).
//
// The dashboard's channel ref is per-session and derived, never stored
// (ADR-024 D5): a (surface, channel_ref) place binds to exactly one session,
// so each session gets the dashboard binding it already has, or a fresh
// `dash-<8 hex>` that first traffic auto-binds.

import type { McResult, McRunner } from "./mc";

export interface ApiDeps {
  mc: McRunner;
  /** Generates a fresh dashboard channel ref (`dash-<8 hex>`). */
  genRef: () => string;
  log: (line: string) => void;
}

export function makeGenRef(): () => string {
  return () => {
    const bytes = new Uint8Array(4);
    crypto.getRandomValues(bytes);
    return `dash-${Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")}`;
  };
}

/** Maps one finished mc invocation onto the HTTP response (ADR-024 D3). */
function verbResponse(res: McResult): Response {
  if (res.code === 0) {
    return Response.json(res.json ?? {}, { status: 200 });
  }
  const envelope =
    res.json !== null && typeof res.json["error"] === "object"
      ? res.json
      : {
          error: {
            code: res.code === 1 ? "domain-rejection" : "environment",
            message: res.stderr.trim() || "the spine did not answer",
          },
        };
  return Response.json(envelope, { status: res.code === 1 ? 400 : 500 });
}

function usageError(message: string): Response {
  return Response.json({ error: { code: "usage", message } }, { status: 400 });
}

/** The session's existing dashboard channel ref from `mc homie list`, or a
 * fresh generated one. Returns a Response when the list itself fails. */
async function dashboardRef(sessionID: string, deps: ApiDeps): Promise<string | Response> {
  const list = await deps.mc.run(["homie", "list"]);
  if (list.code !== 0) {
    return verbResponse(list);
  }
  const sessions = (list.json?.["sessions"] as Record<string, unknown>[] | undefined) ?? [];
  const row = sessions.find((s) => s["id"] === sessionID);
  const bindings = (row?.["active_bindings"] as Record<string, unknown>[] | undefined) ?? [];
  const dash = bindings.find((b) => b["surface"] === "dashboard");
  const ref = dash?.["channel_ref"];
  return typeof ref === "string" && ref !== "" ? ref : deps.genRef();
}

async function readBody(req: Request): Promise<Record<string, unknown> | Response> {
  const raw = await req.text();
  if (raw.trim() === "") {
    return {};
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    // fall through to the usage error
  }
  return usageError("request body must be a JSON object");
}

/** Drains the dashboard's delivery cursor: poll once, ack every row
 * (ADR-024 D6 — history is the render source, so dashboard rows ack
 * trivially; at-least-once replay is idempotent). */
async function drainOutbox(deps: ApiDeps): Promise<Response> {
  const poll = await deps.mc.run(["outbox", "poll", "--surface", "dashboard", "--limit", "100"]);
  if (poll.code !== 0) {
    return verbResponse(poll);
  }
  const rows = (poll.json?.["rows"] as Record<string, unknown>[] | undefined) ?? [];
  let drained = 0;
  for (const row of rows) {
    const id = row["id"];
    if (typeof id !== "number") {
      continue;
    }
    const ack = await deps.mc.run(["outbox", "ack", String(id), "--surface", "dashboard"]);
    if (ack.code !== 0) {
      deps.log(`outbox drain: ack ${id} failed: ${ack.stderr.trim()}`);
      continue;
    }
    drained++;
  }
  return Response.json({ polled: rows.length, drained }, { status: 200 });
}

/** Routes one request. Returns null for paths outside /api/ (static UI). */
export async function handleApi(req: Request, url: URL, deps: ApiDeps): Promise<Response | null> {
  const path = url.pathname;
  if (!path.startsWith("/api/")) {
    return null;
  }

  if (path === "/api/sessions" && req.method === "GET") {
    return verbResponse(await deps.mc.run(["homie", "list"]));
  }

  if (path === "/api/sessions" && req.method === "POST") {
    const ref = deps.genRef();
    return verbResponse(await deps.mc.run(["homie", "start", "--from", `dashboard:${ref}`]));
  }

  const session = path.match(/^\/api\/sessions\/([^/]+)\/(history|send|resume)$/);
  if (session) {
    const [, sessionID, verb] = session;
    if (verb === "history" && req.method === "GET") {
      return verbResponse(await deps.mc.run(["homie", "history", sessionID]));
    }
    if (verb === "send" && req.method === "POST") {
      const body = await readBody(req);
      if (body instanceof Response) {
        return body;
      }
      const text = body["body"];
      if (typeof text !== "string" || text.trim() === "") {
        return usageError("send requires a non-empty string field \"body\"");
      }
      const ref = await dashboardRef(sessionID, deps);
      if (ref instanceof Response) {
        return ref;
      }
      return verbResponse(
        await deps.mc.run(["homie", "send", sessionID, "--from", `dashboard:${ref}`, "--body", text]),
      );
    }
    if (verb === "resume" && req.method === "POST") {
      const ref = await dashboardRef(sessionID, deps);
      if (ref instanceof Response) {
        return ref;
      }
      return verbResponse(
        await deps.mc.run(["homie", "resume", sessionID, "--from", `dashboard:${ref}`]),
      );
    }
  }

  if (path === "/api/outbox/drain" && req.method === "POST") {
    return drainOutbox(deps);
  }

  return Response.json(
    { error: { code: "usage", message: `no such endpoint: ${req.method} ${path}` } },
    { status: 404 },
  );
}
