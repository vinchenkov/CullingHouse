// token-service.ts — the resident-hosted credential projection (ADR-022).
//
// The one property is D2: access-token-in, refresh-token-out. The real
// refresh grants live behind the injected `mint` seam and are never held by
// this module, so no projection built here can carry one. The service keeps
// one short-lived credential per binding, rewritten ahead of expiry with a
// generous lead (D8's mitigation); a lapsed or unavailable credential is
// served as null so the caller stalls rather than proceeds.
//
// Must NOT ever: hold or serialize a refresh grant, serve a lapsed token, or
// set ANTHROPIC_API_KEY (it disables Claude's OAuth mode).

/** One binding's short-lived, host-minted credential. No refresh field exists. */
export interface HostCredential {
  accessToken: string;
  idToken?: string;
  accountId?: string;
  expiresAtMs: number;
}

export interface TokenServiceDeps {
  now(): number;
  setTimer(fn: () => void, ms: number): unknown;
  clearTimer(timer: unknown): void;
  log(line: string): void;
  /** Exchanges the host-held refresh grant for a fresh credential. */
  mint(bindingId: string): Promise<HostCredential>;
}

export interface TokenServiceOpts {
  bindings: string[];
  /** Refresh-ahead lead before expiry. Generous by design (ADR-022 D8). */
  leadMs?: number;
  /** Delay before retrying a failed mint. */
  retryMs?: number;
}

export interface TokenServiceHandle {
  /** The binding's live credential, or null when lapsed/unavailable (D8). */
  credential(bindingId: string): HostCredential | null;
  stop(): void;
  /** Resolves once every binding's initial mint has been attempted. */
  ready: Promise<void>;
}

const DEFAULT_LEAD_MS = 10 * 60_000;
const DEFAULT_RETRY_MS = 30_000;

export function startTokenService(opts: TokenServiceOpts, deps: TokenServiceDeps): TokenServiceHandle {
  const leadMs = opts.leadMs ?? DEFAULT_LEAD_MS;
  const retryMs = opts.retryMs ?? DEFAULT_RETRY_MS;
  const credentials = new Map<string, HostCredential>();
  const timers = new Map<string, unknown>();
  let stopped = false;

  async function refresh(bindingId: string): Promise<void> {
    if (stopped) return;
    try {
      const minted = await deps.mint(bindingId);
      credentials.set(bindingId, minted);
      schedule(bindingId, Math.max(minted.expiresAtMs - leadMs - deps.now(), 0));
    } catch (err) {
      deps.log(
        `token service: mint failed for ${bindingId}: ${err instanceof Error ? err.message : String(err)}`,
      );
      schedule(bindingId, retryMs);
    }
  }

  function schedule(bindingId: string, delayMs: number): void {
    if (stopped) return;
    timers.set(
      bindingId,
      deps.setTimer(() => {
        void refresh(bindingId);
      }, delayMs),
    );
  }

  const ready = (async () => {
    await Promise.all(opts.bindings.map((bindingId) => refresh(bindingId)));
  })();

  return {
    credential(bindingId) {
      const held = credentials.get(bindingId);
      if (!held) return null;
      if (held.expiresAtMs <= deps.now()) return null;
      return held;
    },
    stop() {
      stopped = true;
      for (const timer of timers.values()) deps.clearTimer(timer);
      timers.clear();
    },
    ready,
  };
}

/**
 * The Claude channel (ADR-022 D3, amended per S9 Q1): the host-managed flag
 * bypasses the credential store entirely, the access token arrives via env,
 * and no self-refresh can ever fire. ANTHROPIC_API_KEY stays unset.
 */
export function claudeProjectionEnv(credential: HostCredential): Record<string, string> {
  if (!credential.accessToken) {
    throw new Error("claude projection: empty access token; refusing to project a stall");
  }
  return {
    CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST: "1",
    CLAUDE_CODE_OAUTH_TOKEN: credential.accessToken,
  };
}

/**
 * The dummy occupying auth.json's refresh slot. Codex treats a bogus refresh
 * token as safe at load (Option<String>), and the broker never reads it — a
 * refresh mints from the host-held grant instead (ADR-022 D4).
 */
export const CODEX_DUMMY_REFRESH_TOKEN = "mc-projected-dummy-refresh";

/**
 * The Codex channel (ADR-022 D4): a projected auth.json whose account_id and
 * auth_mode survive verbatim — the guarded reload is account_id-gated and a
 * projection missing either is malformed, not merely degraded.
 */
export function codexAuthJson(credential: HostCredential, nowMs: number): string {
  if (!credential.accessToken) {
    throw new Error("codex projection: empty access token; refusing to project a stall");
  }
  if (!credential.accountId) {
    throw new Error("codex projection: missing account_id; the guarded reload would never adopt a rewrite");
  }
  if (!credential.idToken) {
    throw new Error("codex projection: missing id_token");
  }
  return JSON.stringify(
    {
      auth_mode: "chatgpt",
      tokens: {
        access_token: credential.accessToken,
        id_token: credential.idToken,
        refresh_token: CODEX_DUMMY_REFRESH_TOKEN,
        account_id: credential.accountId,
      },
      last_refresh: new Date(nowMs).toISOString(),
    },
    null,
    2,
  );
}
