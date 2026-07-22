import { describe, expect, test } from "bun:test";
import {
  CODEX_DUMMY_REFRESH_TOKEN,
  claudeProjectionEnv,
  codexAuthJson,
  startTokenService,
  type HostCredential,
  type TokenServiceDeps,
} from "./token-service";

// ADR-022 D2: access-token-in, refresh-token-out. The service holds only
// short-lived credentials minted by an injected host-side minter; the real
// refresh grant lives behind that seam and can never reach a projection.

function credential(overrides: Partial<HostCredential> = {}): HostCredential {
  return {
    accessToken: "access-token-1",
    idToken: "id-token-1",
    accountId: "account-1",
    expiresAtMs: 3_600_000,
    ...overrides,
  };
}

describe("claude flag-based projection (ADR-022 D3, S9 Q1)", () => {
  test("projects exactly the host-managed flag and the access token", () => {
    const env = claudeProjectionEnv(credential());
    expect(env).toEqual({
      CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST: "1",
      CLAUDE_CODE_OAUTH_TOKEN: "access-token-1",
    });
  });

  test("never sets ANTHROPIC_API_KEY, which would disable OAuth mode", () => {
    expect(Object.keys(claudeProjectionEnv(credential()))).not.toContain("ANTHROPIC_API_KEY");
  });

  test("refuses to project an empty access token rather than stall later", () => {
    expect(() => claudeProjectionEnv(credential({ accessToken: "" }))).toThrow();
  });
});

describe("codex auth.json projection (ADR-022 D4)", () => {
  test("preserves account_id and auth_mode, with only a dummy refresh token", () => {
    const parsed = JSON.parse(codexAuthJson(credential(), 1_000_000));
    expect(parsed.auth_mode).toBe("chatgpt");
    expect(parsed.tokens.account_id).toBe("account-1");
    expect(parsed.tokens.access_token).toBe("access-token-1");
    expect(parsed.tokens.id_token).toBe("id-token-1");
    expect(parsed.tokens.refresh_token).toBe(CODEX_DUMMY_REFRESH_TOKEN);
    expect(parsed.tokens.refresh_token.length).toBeGreaterThan(0);
    expect(parsed.last_refresh).toBe(new Date(1_000_000).toISOString());
  });

  test("refuses a credential without the account_id gating guarded reload", () => {
    expect(() => codexAuthJson(credential({ accountId: undefined }), 0)).toThrow();
  });

  test("refuses a credential without an id_token", () => {
    expect(() => codexAuthJson(credential({ idToken: undefined }), 0)).toThrow();
  });
});

interface FakeTimer {
  id: number;
  fn: () => void;
  ms: number;
}

function fakeService(minted: HostCredential[]) {
  const clock = { now: 0 };
  const timers: FakeTimer[] = [];
  const cleared: number[] = [];
  const logs: string[] = [];
  const mints: string[] = [];
  const queue = [...minted];
  let nextTimer = 1;
  const deps: TokenServiceDeps = {
    now: () => clock.now,
    setTimer: (fn, ms) => {
      const timer = { id: nextTimer++, fn, ms };
      timers.push(timer);
      return timer.id;
    },
    clearTimer: (id) => {
      cleared.push(id as number);
    },
    log: (line) => {
      logs.push(line);
    },
    mint: (bindingId) => {
      mints.push(bindingId);
      const next = queue.shift();
      if (!next) return Promise.reject(new Error("mint unavailable"));
      return Promise.resolve(next);
    },
  };
  return { clock, timers, cleared, logs, mints, deps };
}

async function firePending(timers: FakeTimer[], fired: number): Promise<FakeTimer> {
  const timer = timers[fired];
  if (!timer) throw new Error(`no timer at index ${fired}`);
  timer.fn();
  await Bun.sleep(0); // let the mint promise settle
  return timer;
}

describe("refresh-ahead token service (ADR-022 D3/D8)", () => {
  test("mints each binding up front and serves the credential", async () => {
    const fake = fakeService([credential({ accessToken: "claude-a" }), credential({ accessToken: "codex-a" })]);
    const service = startTokenService({ bindings: ["claude", "codex"], leadMs: 600_000 }, fake.deps);
    await service.ready;
    expect(fake.mints).toEqual(["claude", "codex"]);
    expect(service.credential("claude")?.accessToken).toBe("claude-a");
    expect(service.credential("codex")?.accessToken).toBe("codex-a");
    expect(service.credential("unknown")).toBeNull();
    service.stop();
  });

  test("rewrites the token source ahead of expiry with the configured lead", async () => {
    const fake = fakeService([
      credential({ accessToken: "a1", expiresAtMs: 3_600_000 }),
      credential({ accessToken: "a2", expiresAtMs: 7_200_000 }),
    ]);
    const service = startTokenService({ bindings: ["claude"], leadMs: 600_000 }, fake.deps);
    await service.ready;
    expect(fake.timers[0]?.ms).toBe(3_000_000); // expiresAt - lead - now
    fake.clock.now = 3_000_000;
    await firePending(fake.timers, 0);
    expect(service.credential("claude")?.accessToken).toBe("a2");
    expect(fake.timers[1]?.ms).toBe(7_200_000 - 600_000 - 3_000_000);
    service.stop();
  });

  test("a failed mint keeps the unexpired credential and retries", async () => {
    const fake = fakeService([credential({ accessToken: "a1", expiresAtMs: 3_600_000 })]);
    const service = startTokenService({ bindings: ["claude"], leadMs: 600_000, retryMs: 30_000 }, fake.deps);
    await service.ready;
    fake.clock.now = 3_000_000;
    await firePending(fake.timers, 0); // queue is empty: mint rejects
    expect(fake.logs.some((line) => line.includes("claude"))).toBe(true);
    expect(service.credential("claude")?.accessToken).toBe("a1"); // still valid until expiry
    expect(fake.timers[1]?.ms).toBe(30_000); // retry, not lead-based
    service.stop();
  });

  test("a lapsed credential is never served: the run stalls instead (D8)", async () => {
    const fake = fakeService([credential({ accessToken: "a1", expiresAtMs: 3_600_000 })]);
    const service = startTokenService({ bindings: ["claude"], leadMs: 600_000 }, fake.deps);
    await service.ready;
    fake.clock.now = 3_600_001;
    expect(service.credential("claude")).toBeNull();
    service.stop();
  });

  test("stop clears every pending timer", async () => {
    const fake = fakeService([credential(), credential()]);
    const service = startTokenService({ bindings: ["claude", "codex"], leadMs: 600_000 }, fake.deps);
    await service.ready;
    service.stop();
    const pending = fake.timers.map((timer) => timer.id);
    for (const id of pending) {
      expect(fake.cleared).toContain(id);
    }
  });

  test("the projections built from a served credential carry no refresh material", async () => {
    // Structural D2 proof at the module boundary: the service type has no
    // refresh-grant field to leak, so the writers cannot embed one.
    const fake = fakeService([credential({ accessToken: "short-lived" })]);
    const service = startTokenService({ bindings: ["codex"], leadMs: 600_000 }, fake.deps);
    await service.ready;
    const served = service.credential("codex");
    expect(served).not.toBeNull();
    const authJson = codexAuthJson(served!, 0);
    expect(authJson).not.toContain("refresh-grant");
    expect(JSON.parse(authJson).tokens.refresh_token).toBe(CODEX_DUMMY_REFRESH_TOKEN);
    expect(Object.keys(served!)).toEqual(
      expect.arrayContaining(["accessToken", "expiresAtMs"]),
    );
    expect(Object.keys(served!)).not.toContain("refreshToken");
    service.stop();
  });
});
