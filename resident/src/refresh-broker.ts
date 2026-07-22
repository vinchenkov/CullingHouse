// refresh-broker.ts — the host endpoint behind CODEX_REFRESH_TOKEN_URL_OVERRIDE
// (ADR-022 D4). Codex's reactive-401/long-session refresh path POSTs its
// /oauth/token-shaped request here; the broker mints a fresh access token from
// the host-held refresh grant (behind the same injected seam the token service
// uses) and answers with only the dummy refresh placeholder. The
// container-supplied refresh token is never read, used, or echoed.
//
// The listener binds loopback only; containers reach it through the
// resident's spawn wiring (step 3), never from the open network.

import { CODEX_DUMMY_REFRESH_TOKEN, type HostCredential } from "./token-service";

export interface RefreshBrokerDeps {
  mint(bindingId: string): Promise<HostCredential>;
  log(line: string): void;
}

export interface RefreshBrokerOpts {
  bindingId: string;
  /** Listening port; defaults to an ephemeral one. */
  port?: number;
}

export interface RefreshBrokerHandle {
  /** The override URL: http://127.0.0.1:<port>/oauth/token */
  url: string;
  stop(): void;
}

export function startRefreshBroker(opts: RefreshBrokerOpts, deps: RefreshBrokerDeps): RefreshBrokerHandle {
  const server = Bun.serve({
    hostname: "127.0.0.1",
    port: opts.port ?? 0,
    async fetch(req) {
      if (req.method !== "POST") {
        return new Response("method not allowed", { status: 405 });
      }
      let grantType: unknown;
      try {
        const body = (await req.json()) as Record<string, unknown>;
        grantType = body.grant_type;
      } catch {
        return new Response("invalid request body", { status: 400 });
      }
      if (grantType !== "refresh_token") {
        return new Response("unsupported grant_type", { status: 400 });
      }
      let minted: HostCredential;
      try {
        minted = await deps.mint(opts.bindingId);
      } catch (err) {
        deps.log(
          `refresh broker: mint failed for ${opts.bindingId}: ${err instanceof Error ? err.message : String(err)}`,
        );
        return new Response("token service unavailable", { status: 503 });
      }
      if (!minted.accessToken || !minted.idToken) {
        deps.log(`refresh broker: incomplete mint for ${opts.bindingId}; refusing`);
        return new Response("token service unavailable", { status: 503 });
      }
      return Response.json({
        access_token: minted.accessToken,
        id_token: minted.idToken,
        refresh_token: CODEX_DUMMY_REFRESH_TOKEN,
      });
    },
  });
  return {
    url: `http://127.0.0.1:${server.port}/oauth/token`,
    stop() {
      server.stop(true);
    },
  };
}
