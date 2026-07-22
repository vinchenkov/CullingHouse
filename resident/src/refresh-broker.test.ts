import { afterEach, describe, expect, test } from "bun:test";
import { startRefreshBroker, type RefreshBrokerHandle } from "./refresh-broker";
import { CODEX_DUMMY_REFRESH_TOKEN, type HostCredential } from "./token-service";

// ADR-022 D4: CODEX_REFRESH_TOKEN_URL_OVERRIDE points Codex's refresh path at
// this host endpoint. The broker never reads the container-supplied refresh
// token — it mints from the host-held grant behind the same seam the token
// service uses — and its response carries only the dummy refresh placeholder.

const brokers: RefreshBrokerHandle[] = [];

afterEach(() => {
  for (const broker of brokers.splice(0)) broker.stop();
});

function credential(overrides: Partial<HostCredential> = {}): HostCredential {
  return {
    accessToken: "fresh-access",
    idToken: "fresh-id",
    accountId: "account-1",
    expiresAtMs: 3_600_000,
    ...overrides,
  };
}

function broker(mint: () => Promise<HostCredential>, log: string[] = []): RefreshBrokerHandle {
  const handle = startRefreshBroker(
    { bindingId: "codex" },
    { mint: () => mint(), log: (line) => log.push(line) },
  );
  brokers.push(handle);
  return handle;
}

function refreshRequest(handle: RefreshBrokerHandle, body: unknown, init: RequestInit = {}): Promise<Response> {
  return fetch(handle.url, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
    ...init,
  });
}

const wellFormed = {
  client_id: "app_client",
  grant_type: "refresh_token",
  refresh_token: CODEX_DUMMY_REFRESH_TOKEN,
  scope: "openid profile email",
};

describe("codex refresh broker (ADR-022 D4)", () => {
  test("binds loopback only", () => {
    const handle = broker(() => Promise.resolve(credential()));
    expect(new URL(handle.url).hostname).toBe("127.0.0.1");
  });

  test("a refresh mints host-side and returns only the dummy refresh token", async () => {
    const handle = broker(() => Promise.resolve(credential()));
    const res = await refreshRequest(handle, wellFormed);
    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, string>;
    expect(body.access_token).toBe("fresh-access");
    expect(body.id_token).toBe("fresh-id");
    expect(body.refresh_token).toBe(CODEX_DUMMY_REFRESH_TOKEN);
  });

  test("the container-supplied refresh token is never used or echoed", async () => {
    const handle = broker(() => Promise.resolve(credential()));
    const res = await refreshRequest(handle, { ...wellFormed, refresh_token: "attacker-supplied-grant" });
    expect(res.status).toBe(200);
    const raw = await res.text();
    expect(raw).not.toContain("attacker-supplied-grant");
  });

  test("a failed mint is 503 with no credential material", async () => {
    const log: string[] = [];
    const handle = broker(() => Promise.reject(new Error("refresh grant sk-secret rejected upstream")), log);
    const res = await refreshRequest(handle, wellFormed);
    expect(res.status).toBe(503);
    expect(await res.text()).not.toContain("sk-secret");
    expect(log.some((line) => line.includes("codex"))).toBe(true);
  });

  test("a mint without an id_token fails closed as 503", async () => {
    const handle = broker(() => Promise.resolve(credential({ idToken: undefined })));
    const res = await refreshRequest(handle, wellFormed);
    expect(res.status).toBe(503);
  });

  test("non-POST is refused", async () => {
    const handle = broker(() => Promise.resolve(credential()));
    const res = await fetch(handle.url);
    expect(res.status).toBe(405);
  });

  test("a malformed or wrong-grant body is refused without minting", async () => {
    let mints = 0;
    const handle = broker(() => {
      mints++;
      return Promise.resolve(credential());
    });
    const wrongGrant = await refreshRequest(handle, { ...wellFormed, grant_type: "authorization_code" });
    expect(wrongGrant.status).toBe(400);
    const notJson = await fetch(handle.url, { method: "POST", body: "not json" });
    expect(notJson.status).toBe(400);
    expect(mints).toBe(0);
  });

  test("stop closes the listener", async () => {
    const handle = broker(() => Promise.resolve(credential()));
    handle.stop();
    await expect(refreshRequest(handle, wellFormed)).rejects.toThrow();
  });
});
