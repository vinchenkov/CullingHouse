// credential-projector.ts — the ADR-022 step-3 composition main.ts wires into
// the spawn seam: refresh grants in, a CredentialProjector out.
//
// The real refresh grants live in this module's grant map and the on-disk
// store (MC_HOME/refresh-grants, deny-mounted host-side); every outward
// artifact — env, auth.json, broker response — carries at most a short-lived
// access token and the dummy refresh placeholder. Provider refresh tokens can
// be single-use: a rotated grant is adopted in memory and persisted
// immediately, and a failed persist is loud, because losing a rotation
// strands the binding at the next restart.

import { startRefreshBroker, type RefreshBrokerHandle } from "./refresh-broker";
import {
  claudeProjectionEnv,
  codexAuthJson,
  startTokenService,
  type HostCredential,
  type TokenServiceHandle,
} from "./token-service";
import type { CredentialProjection, CredentialProjector } from "./types";

/** One binding's on-disk refresh grant (MC_HOME/refresh-grants/<binding>.json). */
export interface RefreshGrant {
  binding: string;
  channel: "claude" | "codex";
  /** The provider token endpoint the mint POSTs to. Configuration, not a
   * fake seam: a synthetic acceptance lane points it at a local authority. */
  token_url: string;
  client_id: string;
  refresh_token: string;
  account_id?: string;
  scope?: string;
}

/** Validates one grant file's parsed JSON, fail-closed: a malformed grant is
 * a configuration error the resident refuses to start over, never a silently
 * skipped binding. */
export function parseRefreshGrant(value: unknown): RefreshGrant {
  const raw = value as Partial<RefreshGrant> | null;
  if (raw === null || typeof raw !== "object") {
    throw new Error("refresh grant must be a JSON object");
  }
  if (typeof raw.binding !== "string" || raw.binding === "") {
    throw new Error("refresh grant needs a non-empty binding id");
  }
  if (raw.channel !== "claude" && raw.channel !== "codex") {
    throw new Error(`refresh grant ${raw.binding}: channel must be claude or codex`);
  }
  if (typeof raw.token_url !== "string" || !/^https?:\/\//.test(raw.token_url)) {
    throw new Error(`refresh grant ${raw.binding}: token_url must be an http(s) URL`);
  }
  if (typeof raw.client_id !== "string" || raw.client_id === "") {
    throw new Error(`refresh grant ${raw.binding}: client_id is required`);
  }
  if (typeof raw.refresh_token !== "string" || raw.refresh_token === "") {
    throw new Error(`refresh grant ${raw.binding}: refresh_token is required`);
  }
  const grant: RefreshGrant = {
    binding: raw.binding,
    channel: raw.channel,
    token_url: raw.token_url,
    client_id: raw.client_id,
    refresh_token: raw.refresh_token,
  };
  if (raw.account_id !== undefined) {
    if (typeof raw.account_id !== "string") throw new Error(`refresh grant ${raw.binding}: account_id must be a string`);
    grant.account_id = raw.account_id;
  }
  if (raw.scope !== undefined) {
    if (typeof raw.scope !== "string") throw new Error(`refresh grant ${raw.binding}: scope must be a string`);
    grant.scope = raw.scope;
  }
  return grant;
}

export interface TokenEndpointResult {
  ok: boolean;
  status: number;
  body?: Record<string, unknown>;
}

export interface CredentialProjectorDeps {
  now(): number;
  /** setTimeout-shaped one-shot timer. */
  setTimer(fn: () => void, ms: number): unknown;
  clearTimer(timer: unknown): void;
  log(line: string): void;
  /** POSTs the refresh exchange to the provider token endpoint. */
  fetchToken(url: string, body: Record<string, unknown>): Promise<TokenEndpointResult>;
  /** Durably rewrites one binding's grant after a rotation. */
  persistGrant(grant: RefreshGrant): Promise<void>;
}

export interface CredentialProjectorHandle {
  projector: CredentialProjector;
  /** Exposed for composition tests and the broker; mints a fresh credential. */
  mint(bindingId: string): Promise<HostCredential>;
  ready: Promise<void>;
  stop(): void;
}

const DEFAULT_EXPIRES_IN_S = 3600;

export function startCredentialProjector(
  grants: RefreshGrant[],
  deps: CredentialProjectorDeps,
): CredentialProjectorHandle {
  const byBinding = new Map<string, RefreshGrant>();
  for (const grant of grants) {
    if (byBinding.has(grant.binding)) {
      throw new Error(`duplicate refresh grant for binding ${grant.binding}`);
    }
    byBinding.set(grant.binding, grant);
  }

  async function mint(bindingId: string): Promise<HostCredential> {
    const grant = byBinding.get(bindingId);
    if (!grant) throw new Error(`no refresh grant for binding ${bindingId}`);
    const request: Record<string, unknown> = {
      grant_type: "refresh_token",
      client_id: grant.client_id,
      refresh_token: grant.refresh_token,
    };
    if (grant.scope !== undefined) request.scope = grant.scope;
    const result = await deps.fetchToken(grant.token_url, request);
    if (!result.ok || result.body === undefined) {
      throw new Error(`token endpoint refused (${result.status}) for ${bindingId}`);
    }
    const accessToken = result.body.access_token;
    if (typeof accessToken !== "string" || accessToken === "") {
      throw new Error(`token endpoint returned no access token for ${bindingId}`);
    }
    const rotated = result.body.refresh_token;
    if (typeof rotated === "string" && rotated !== "" && rotated !== grant.refresh_token) {
      // Adopt in memory FIRST: the old grant may already be dead server-side.
      const updated = { ...grant, refresh_token: rotated };
      byBinding.set(bindingId, updated);
      try {
        await deps.persistGrant(updated);
      } catch (err) {
        deps.log(
          `credential projector: PERSIST FAILED for rotated grant of ${bindingId}; ` +
            `the binding will strand on restart: ${err instanceof Error ? err.message : String(err)}`,
        );
      }
    }
    const expiresIn = typeof result.body.expires_in === "number" && result.body.expires_in > 0
      ? result.body.expires_in
      : DEFAULT_EXPIRES_IN_S;
    return {
      accessToken,
      idToken: typeof result.body.id_token === "string" ? result.body.id_token : undefined,
      accountId: grant.account_id,
      expiresAtMs: deps.now() + expiresIn * 1000,
    };
  }

  const service: TokenServiceHandle = startTokenService(
    { bindings: [...byBinding.keys()] },
    {
      now: deps.now,
      setTimer: deps.setTimer,
      clearTimer: deps.clearTimer,
      log: deps.log,
      mint,
    },
  );

  const brokers = new Map<string, RefreshBrokerHandle>();
  for (const grant of byBinding.values()) {
    if (grant.channel === "codex") {
      brokers.set(grant.binding, startRefreshBroker(
        { bindingId: grant.binding },
        { mint, log: deps.log },
      ));
    }
  }

  function containerBrokerUrl(bindingId: string): string | null {
    const broker = brokers.get(bindingId);
    if (!broker) return null;
    return broker.url.replace("127.0.0.1", "host.docker.internal");
  }

  const projector: CredentialProjector = {
    project(_harness, modelBinding): CredentialProjection | { refused: string } | null {
      const grant = byBinding.get(modelBinding);
      if (!grant) return null;
      const credential = service.credential(modelBinding);
      if (credential === null) {
        return { refused: `credential unavailable or lapsed for binding ${modelBinding}` };
      }
      try {
        if (grant.channel === "claude") {
          return { env: claudeProjectionEnv(credential) };
        }
        const override = containerBrokerUrl(modelBinding);
        if (override === null) {
          return { refused: `no refresh broker for codex binding ${modelBinding}` };
        }
        return {
          env: { CODEX_REFRESH_TOKEN_URL_OVERRIDE: override },
          authJson: codexAuthJson(credential, deps.now()),
        };
      } catch (err) {
        return { refused: err instanceof Error ? err.message : String(err) };
      }
    },
  };

  return {
    projector,
    mint,
    ready: service.ready,
    stop() {
      service.stop();
      for (const broker of brokers.values()) broker.stop();
    },
  };
}
