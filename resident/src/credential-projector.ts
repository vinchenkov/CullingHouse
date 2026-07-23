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

export const PRODUCTION_RUNTIME_BINDINGS = {
  chatgpt: {
    harness: "codex",
    channel: "codex",
    token_url: "https://auth.openai.com/oauth/token",
    client_id: "app_EMoamEEZ73f0CkXaXp7hrann",
  },
  claude: {
    harness: "claude-sdk",
    channel: "claude",
    token_url: "https://platform.claude.com/v1/oauth/token",
    client_id: "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
  },
  minimax: {
    harness: "claude-sdk",
    channel: "static",
    env_name: "ANTHROPIC_AUTH_TOKEN",
  },
} as const;

/** One OAuth binding's on-disk refresh grant. */
export interface RefreshGrant {
  binding: string;
  channel: "claude" | "codex";
  token_url: string;
  client_id: string;
  refresh_token: string;
  account_id?: string;
  scope?: string;
}

/** MiniMax's operator-imported, provider-specific static grant. */
export interface StaticGrant {
  binding: string;
  channel: "static";
  env_name: "ANTHROPIC_AUTH_TOKEN";
  secret: string;
}

export type RuntimeGrant = RefreshGrant | StaticGrant;

function assertExactKeys(raw: Record<string, unknown>, expected: string[], binding: string): void {
  const actual = Object.keys(raw).sort();
  const wanted = [...expected].sort();
  if (actual.length !== wanted.length || actual.some((key, i) => key !== wanted[i])) {
    throw new Error(`runtime grant ${binding}: fields must be exactly ${wanted.join(", ")}`);
  }
}

/** Validates one grant file's parsed JSON, fail-closed: a malformed grant is
 * a configuration error the resident refuses to start over, never a silently
 * skipped binding. */
export function parseRefreshGrant(value: unknown): RuntimeGrant {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("runtime grant must be a JSON object");
  }
  const raw = value as Record<string, unknown>;
  if (typeof raw.binding !== "string" || raw.binding === "") {
    throw new Error("runtime grant needs a non-empty binding id");
  }
  const spec = PRODUCTION_RUNTIME_BINDINGS[raw.binding as keyof typeof PRODUCTION_RUNTIME_BINDINGS];
  if (spec === undefined) {
    throw new Error(`runtime grant ${raw.binding}: unknown production binding`);
  }
  if (raw.channel !== spec.channel) {
    throw new Error(`runtime grant ${raw.binding}: channel must be ${spec.channel}`);
  }

  if (spec.channel === "static") {
    assertExactKeys(raw, ["binding", "channel", "env_name", "secret"], raw.binding);
    if (raw.env_name !== spec.env_name) {
      throw new Error(`runtime grant ${raw.binding}: env_name must be ${spec.env_name}`);
    }
    if (typeof raw.secret !== "string" || raw.secret === "") {
      throw new Error(`runtime grant ${raw.binding}: secret is required`);
    }
    return {
      binding: raw.binding,
      channel: "static",
      env_name: spec.env_name,
      secret: raw.secret,
    };
  }

  const optional = spec.channel === "codex" ? ["scope"] : [];
  const required = spec.channel === "codex"
    ? ["binding", "channel", "token_url", "client_id", "refresh_token", "account_id"]
    : ["binding", "channel", "token_url", "client_id", "refresh_token", "scope"];
  assertExactKeys(raw, [...required, ...optional.filter((key) => key in raw)], raw.binding);
  if (raw.token_url !== spec.token_url) {
    throw new Error(`runtime grant ${raw.binding}: token_url does not match the production catalog`);
  }
  if (raw.client_id !== spec.client_id) {
    throw new Error(`runtime grant ${raw.binding}: client_id does not match the production catalog`);
  }
  if (typeof raw.refresh_token !== "string" || raw.refresh_token === "") {
    throw new Error(`runtime grant ${raw.binding}: refresh_token is required`);
  }
  const grant: RefreshGrant = {
    binding: raw.binding,
    channel: spec.channel,
    token_url: spec.token_url,
    client_id: spec.client_id,
    refresh_token: raw.refresh_token,
  };
  if (spec.channel === "codex") {
    if (typeof raw.account_id !== "string" || raw.account_id === "") {
      throw new Error(`runtime grant ${raw.binding}: account_id is required`);
    }
    grant.account_id = raw.account_id;
  }
  if (raw.scope !== undefined) {
    if (typeof raw.scope !== "string" || raw.scope === "") {
      throw new Error(`runtime grant ${raw.binding}: scope must be a non-empty string`);
    }
    grant.scope = raw.scope;
  }
  if (spec.channel === "claude" && !grant.scope?.split(/\s+/).includes("user:inference")) {
    throw new Error(`runtime grant ${raw.binding}: scope must include user:inference`);
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
  grants: RuntimeGrant[],
  deps: CredentialProjectorDeps,
): CredentialProjectorHandle {
  const byBinding = new Map<string, RuntimeGrant>();
  for (const candidate of grants) {
    const grant = parseRefreshGrant(candidate);
    if (byBinding.has(grant.binding)) {
      throw new Error(`duplicate refresh grant for binding ${grant.binding}`);
    }
    byBinding.set(grant.binding, grant);
  }

  async function mint(bindingId: string): Promise<HostCredential> {
    const grant = byBinding.get(bindingId);
    if (!grant) throw new Error(`no refresh grant for binding ${bindingId}`);
    if (grant.channel === "static") throw new Error(`binding ${bindingId} has no refresh channel`);
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
    { bindings: [...byBinding.values()].filter((grant) => grant.channel !== "static").map((grant) => grant.binding) },
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
    project(harness, modelBinding): CredentialProjection | { refused: string } | null {
      if (harness === "fake" && modelBinding === "fake") return null;
      const spec = PRODUCTION_RUNTIME_BINDINGS[modelBinding as keyof typeof PRODUCTION_RUNTIME_BINDINGS];
      if (spec === undefined) return { refused: `unknown production binding ${modelBinding}` };
      if (harness !== spec.harness) {
        return { refused: `binding ${modelBinding} requires harness ${spec.harness}` };
      }
      const grant = byBinding.get(modelBinding);
      if (!grant) return { refused: `runtime grant missing for production binding ${modelBinding}` };
      if (grant.channel === "static") {
        return { env: { [grant.env_name]: grant.secret } };
      }
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
