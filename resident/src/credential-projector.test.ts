import { afterEach, describe, expect, test } from "bun:test";
import {
  startCredentialProjector,
	PRODUCTION_RUNTIME_BINDINGS,
  type CredentialProjectorHandle,
  type RuntimeGrant,
  type RefreshGrant,
} from "./credential-projector";
import { CODEX_DUMMY_REFRESH_TOKEN } from "./token-service";

// The composition ADR-022 step 3 wires into main.ts: refresh grants in, a
// CredentialProjector out. The real refresh grant lives only in this module's
// grant map and the on-disk store; no projection artifact may carry it.

const handles: CredentialProjectorHandle[] = [];

test("MiniMax production traffic is pinned to its Anthropic-compatible endpoint", () => {
	expect(PRODUCTION_RUNTIME_BINDINGS.minimax.base_url).toBe("https://api.minimax.io/anthropic");
});

afterEach(async () => {
  for (const handle of handles.splice(0)) handle.stop();
});

function claudeGrant(overrides: Partial<RefreshGrant> = {}): RefreshGrant {
  return {
    binding: "claude",
    channel: "claude",
    token_url: "https://platform.claude.com/v1/oauth/token",
    client_id: "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
    refresh_token: "real-refresh-grant-claude",
    scope: "user:inference",
    ...overrides,
  };
}

function codexGrant(overrides: Partial<RefreshGrant> = {}): RefreshGrant {
  return {
    binding: "chatgpt",
    channel: "codex",
    token_url: "https://auth.openai.com/oauth/token",
    client_id: "app_EMoamEEZ73f0CkXaXp7hrann",
    refresh_token: "real-refresh-grant-codex",
    account_id: "account-9",
    ...overrides,
  };
}

function minimaxGrant(overrides: Partial<RuntimeGrant> = {}): RuntimeGrant {
  return {
    binding: "minimax",
    channel: "static",
    env_name: "ANTHROPIC_AUTH_TOKEN",
    secret: "minimax-static-secret",
    ...overrides,
  } as RuntimeGrant;
}

interface ProviderCall {
  url: string;
  body: Record<string, unknown>;
}

function fakeProvider(responses: Array<Record<string, unknown> | number>) {
  const calls: ProviderCall[] = [];
  const queue = [...responses];
  const persisted: RefreshGrant[] = [];
  const logs: string[] = [];
  const deps = {
    now: () => 1_000_000,
    setTimer: (fn: () => void, ms: number) => setTimeout(fn, ms),
    clearTimer: (timer: unknown) => clearTimeout(timer as ReturnType<typeof setTimeout>),
    log: (line: string) => {
      logs.push(line);
    },
    fetchToken: async (url: string, body: Record<string, unknown>) => {
      calls.push({ url, body });
      const next = queue.shift();
      if (next === undefined || typeof next === "number") {
        return { ok: false as const, status: (next as number) ?? 500 };
      }
      return { ok: true as const, status: 200, body: next };
    },
    persistGrant: async (grant: RefreshGrant) => {
      persisted.push({ ...grant });
    },
  };
  return { deps, calls, persisted, logs };
}

const freshClaude = { access_token: "claude-access-1", expires_in: 3600 };
const freshCodex = { access_token: "codex-access-1", id_token: "codex-id-1", expires_in: 3600 };

async function start(grants: RuntimeGrant[], provider: ReturnType<typeof fakeProvider>) {
  const handle = startCredentialProjector(grants, provider.deps);
  handles.push(handle);
  await handle.ready;
  return handle;
}

describe("credential projector composition (ADR-022 step 3)", () => {
  test("duplicate binding grants refuse instead of silently choosing one", () => {
    const provider = fakeProvider([]);
    expect(() => startCredentialProjector([
      claudeGrant(),
      claudeGrant({ refresh_token: "different-owner" }),
    ], provider.deps)).toThrow("duplicate refresh grant for binding claude");
  });

  test("a claude binding projects the flag env from a minted credential", async () => {
    const provider = fakeProvider([freshClaude]);
    const handle = await start([claudeGrant()], provider);
    const projection = handle.projector.project("claude-sdk", "claude");
    expect(projection).toEqual({
      env: {
        CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST: "1",
        CLAUDE_CODE_OAUTH_TOKEN: "claude-access-1",
      },
    });
    expect(provider.calls[0]?.body.grant_type).toBe("refresh_token");
    expect(provider.calls[0]?.body.refresh_token).toBe("real-refresh-grant-claude");
  });

  test("a codex binding projects auth.json, the broker override, and only dummy refresh material", async () => {
    const provider = fakeProvider([
      freshCodex,
      { access_token: "codex-access-2", id_token: "codex-id-2", expires_in: 3600 },
    ]);
    const handle = await start([codexGrant()], provider);
    const projection = handle.projector.project("codex", "chatgpt");
    if (projection === null || "refused" in projection) {
      throw new Error(`unexpected projection ${JSON.stringify(projection)}`);
    }
    const override = projection.env.CODEX_REFRESH_TOKEN_URL_OVERRIDE!;
    expect(override).toMatch(/^http:\/\/host\.docker\.internal:\d+\/oauth\/token$/);
    const auth = JSON.parse(projection.authJson!);
    expect(auth.tokens.account_id).toBe("account-9");
    expect(auth.tokens.refresh_token).toBe(CODEX_DUMMY_REFRESH_TOKEN);
    expect(JSON.stringify(projection)).not.toContain("real-refresh-grant-codex");

    // The broker end to end from the container's side of the seam: a refresh
    // against the override mints from the HOST grant and answers with only
    // the dummy placeholder, never real refresh material.
    const res = await fetch(override.replace("host.docker.internal", "127.0.0.1"), {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ grant_type: "refresh_token", refresh_token: CODEX_DUMMY_REFRESH_TOKEN, client_id: "x" }),
    });
    expect(res.status).toBe(200);
    const minted = (await res.json()) as Record<string, string>;
    expect(minted.access_token).toBe("codex-access-2");
    expect(minted.refresh_token).toBe(CODEX_DUMMY_REFRESH_TOKEN);
    expect(JSON.stringify(minted)).not.toContain("real-refresh-grant-codex");
  });

  test("rotation: a provider-returned refresh token is persisted and used next", async () => {
    const provider = fakeProvider([
      { ...freshClaude, refresh_token: "rotated-grant" },
      { access_token: "claude-access-2", expires_in: 3600 },
    ]);
    const handle = await start([claudeGrant()], provider);
    expect(provider.persisted).toHaveLength(1);
    expect(provider.persisted[0]?.refresh_token).toBe("rotated-grant");
    const again = await handle.mint("claude");
    expect(again.accessToken).toBe("claude-access-2");
    expect(provider.calls[1]?.body.refresh_token).toBe("rotated-grant");
  });

  test("a failed mint yields a refusal, never a token-free launch (D8)", async () => {
    const provider = fakeProvider([401]);
    const handle = await start([claudeGrant()], provider);
    const projection = handle.projector.project("claude-sdk", "claude");
    expect(projection).not.toBeNull();
    expect(projection && "refused" in projection).toBe(true);
    expect(JSON.stringify(projection)).not.toContain("real-refresh-grant-claude");
  });

  test("the MiniMax static binding projects only its declared key", async () => {
    const provider = fakeProvider([]);
    const handle = await start([minimaxGrant()], provider);
    expect(handle.projector.project("claude-sdk", "minimax")).toEqual({
      env: { ANTHROPIC_AUTH_TOKEN: "minimax-static-secret" },
    });
    expect(provider.calls).toEqual([]);
  });

  test("missing and unknown non-fake bindings refuse; fake remains token-free", async () => {
    const provider = fakeProvider([freshClaude]);
    const handle = await start([claudeGrant()], provider);
    expect(handle.projector.project("fake", "fake")).toBeNull();
    expect(handle.projector.project("codex", "chatgpt")).toEqual({
      refused: "runtime grant missing for production binding chatgpt",
    });
    expect(handle.projector.project("claude-sdk", "unknown")).toEqual({
      refused: "unknown production binding unknown",
    });
  });
});

describe("runtime grant parsing", () => {
  test("the closed OAuth/static union round-trips", async () => {
    const { parseRefreshGrant } = await import("./credential-projector");
    expect(parseRefreshGrant(codexGrant())).toEqual(codexGrant());
    expect(parseRefreshGrant(claudeGrant())).toEqual(claudeGrant());
    expect(parseRefreshGrant(minimaxGrant())).toEqual(minimaxGrant());
  });

  test("catalog drift, cross-binding channels, extra fields, and malformed grants refuse", async () => {
    const { parseRefreshGrant } = await import("./credential-projector");
    for (const bad of [
      null,
      {},
      { ...claudeGrant(), channel: "gateway" },
      { ...claudeGrant(), token_url: "ftp://x" },
      { ...claudeGrant(), token_url: "https://attacker.example/token" },
      { ...claudeGrant(), client_id: "attacker-client" },
      { ...claudeGrant(), scope: "openid" },
      { ...claudeGrant(), refresh_token: "" },
      { ...codexGrant(), account_id: 7 },
      { ...codexGrant(), binding: "minimax" },
      { ...minimaxGrant(), env_name: "ANTHROPIC_API_KEY" },
      { ...minimaxGrant(), secret: "" },
      { ...minimaxGrant(), token_url: "https://attacker.example/token" },
      { ...codexGrant(), surprise: true },
    ]) {
      expect(() => parseRefreshGrant(bad)).toThrow();
    }
  });
});
