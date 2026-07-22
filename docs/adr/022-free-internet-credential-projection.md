# ADR-022 — Free-internet credential projection: a host token service replaces the egress gateway

## Status
Accepted — 2026-07-21. **Supersedes ADR-018** (per-launch network guard and
resident-owned egress gateway). Amends spec §11.4 and reinterprets Inv. 16 and
Inv. 23; the amendment text lives in the spec at §11.4 (2026-07-21) and is the
authority — this ADR is the design record behind it.

## Context

The operator set a hard requirement that inverts the §11.4 security model:
**agent containers browse the internet freely.** There is no `--network none`,
no loopback→proxy forwarder, no egress allowlist, no per-launch network guard.
The container has an ordinary network stack and reaches providers directly.

Deleting the egress proxy deletes the only network-plane blast-radius control.
The single remaining control is therefore the **credential boundary**, and with
free egress it is not defense-in-depth — it is the entire security property: a
credential that leaks out of the container must be one that cannot do lasting
damage.

The load-bearing unknown was whether the runtimes would *accept* a host-managed
credential — adopt an access token the host rewrites, without ever holding the
refresh grant — or hard-fail when they cannot self-refresh. This was settled
live (HTTP 200, both SDKs) in `scratchpad/oauth-poc/POC-RESULT-v2.md`:
Claude Code **2.1.216** and Codex CLI **0.144.6** each adopt a host-rewritten
access token with only a dummy refresh token in the container. All POC tokens
were synthetic; no real credential was read or transmitted.

The operator has approved the resulting invariant modifications and the loss of
HTTP egress auditability. Spec §11.4's proxy/gateway model, and the whole of
ADR-018 D1–D9, are superseded by the decisions below.

## Decision

**D1 — Free internet; the egress proxy is deleted as a network control.**
Agent containers get an ordinary network stack. `egress_policy`
(`none`|`allowlist`|`open+audit`), the HTTP egress audit log, and the
`network_allow` raw-TCP plane are removed from the enforced boundary — there is
nothing in the request path to enforce or audit them. "Open" no longer means
"observed"; this is an accepted, operator-signed loss.

**D2 — The one property: access-token-in, refresh-token-out.** A host-side
**token service**, hosted by the resident (§10, Inv. 23), owns the real refresh
grants captured at onboarding. It projects only a short-lived **access** token
into each container; the **refresh** grant never enters the container. A leaked
access token is short-lived and non-refreshable from inside. This is enforced
per binding by one of two channels.

**D3 — Claude channel is host-managed via `CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST`
(amended 2026-07-21, S9).** The S9 spike (`spikes/09-credential-projection/RESULT.md`)
resolved the residual below in favor of the flag: the token service sets
`CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST=1` and delivers the host-minted access
token via `CLAUDE_CODE_OAUTH_TOKEN` (or the well-known file /
`CLAUDE_CODE_HOST_CREDS_FILE` for in-session 401 rotation). Under the flag
(verified static on 2.1.217) Claude bypasses the credential store entirely
(`if(RE())return null` precedes the `.credentials.json`/keychain read), treats
the token as `refreshToken:null, expiresAt:null` so it never self-refreshes, and
the `invalid_grant` compare-and-swap wipe is structurally unreachable — strictly
more fail-closed than the file-rewrite. `ANTHROPIC_API_KEY` must still be unset.
A refresh-ahead loop rewrites the token source before expiry; it no longer risks
a clobber or wipe. The `/v1/messages` call goes **direct** to the real provider.

The **fallback**, if the flag proves unusable in-container, is the original PUSH
design: write `<configDir>/.credentials.json` with a fresh `accessToken`, a
**non-empty dummy** `refreshToken`, a **future `expiresAt`**, and `scopes`
including `user:inference`, refreshing ahead so Claude never enters its own
refresh path. Live-verified constraints on 2.1.216 (the fallback's contract):
- Empty or absent `refreshToken` **hard-fails** ("OAuth session expired and
  could not be refreshed"), no request made — a non-empty dummy is required.
  This overturns the source-only reading that an empty refresh is safer.
- `ANTHROPIC_API_KEY` must be **unset** (it disables OAuth mode).
- `ANTHROPIC_BASE_URL` redirects the messages API but **not** the refresh
  endpoint (hardcoded to platform.claude.com, override allowlist-gated) — which
  is *why* Claude must be kept from refreshing rather than brokered.
The `/v1/messages` call goes **direct** to the real provider over open internet.

**D4 — Codex channel is PULL (broker the refresh).** The token service projects
`auth.json` = `{access_token, id_token, dummy refresh_token, account_id,
auth_mode:"chatgpt"}` and sets **`CODEX_REFRESH_TOKEN_URL_OVERRIDE`** to a host
endpoint that mints a fresh access token from the real refresh grant. Codex
proactively refreshes at startup and near cached-token expiry; the guarded
reload is **gated on `account_id`**, so the projection must preserve it. Live
proof: stale bearer → 401 → host-serviced `/oauth/token` → fresh bearer → 200 on
the responses call. The `/backend-api/codex/responses` call goes **direct** to
the real provider in production — the POC's `model_providers`/`base_url`
responses-redirect was a mock artifact and is **not** used in production.
`CODEX_ACCESS_TOKEN` is Agent-Identity mode (prod/staging service accounts), not
the subscription path, and is not used.

**D5 — Static-token bindings (e.g. MiniMax) do not get refresh-out; ruling for
v1 is materialize-with-advisory.** A static API key is entirely a long-lived
grant — there is no access/refresh split to keep half on the host, and
file-projection cannot keep a raw key out of a container that must use it.
Keeping a static key out requires an in-path injector (a proxy), the very thing
D1 deletes. For v1 the key is **materialized into the container env** as a
**declared per-binding downgrade**, bounded exactly as the spec already bounds
materialized OAuth: a MiniMax subscription key carries no metered spend and is
revocable, so a leak's blast radius is "uses the subscription until rotated."
The `mc worksource add` connect-time advisory (§11.3) applies: scoped, expiring,
revocable credentials. **Hardening path (not built):** a narrow
credential-injector shim that sits only in front of that provider's domain and
adds the auth header — not an egress control, just a header-injector — restoring
refresh-out-equivalent for static keys. Adopt it the day a metered or
high-value static key appears; until then it is deliberately deferred.

**D6 — Different backends are orthogonal and supported.** Each binding declares
its own `base_url` — Claude via `ANTHROPIC_BASE_URL`, Codex via its
`model_providers` entry, a static binding via its own endpoint. The secret model
(D2–D5) is independent of which backend a binding targets.

**D7 — Forbidden-env still ships and now covers OAuth refresh material.** The
boundary env builder rejects each shipped forbidden key before claim, proves
wildcard enumeration via `SENTINEL_API_KEY`, and guarantees the final container
env contains no provider refresh token and no provider API key for OAuth
bindings — the projected file is the *only* credential material for those. A
materialized static key (D5) is the single declared exception, present only in
its own binding's plane.

**D8 — Fail-closed.** If the token service is down or the projected access token
lapses, the runtime's own path fails without leaking: Claude attempts the dummy
refresh → `invalid_grant` → compare-and-swap **wipe** of the on-disk record (no
leak; the agent stalls until the host rewrites); Codex's host refresh is
unavailable → 401. No credential escapes; the run stalls rather than proceeds.
Generous refresh lead time is the mitigation.

## Consequences

- **Deleted:** the entire ADR-018 apparatus — guarded network namespace, CA and
  header-injection proxy, DNS/raw-rule enforcement, gateway HTTP modes, egress
  audit ownership, guard-loss reconciliation. This is a large simplification.
- **Lost (operator-accepted):** HTTP egress auditability. There is no per-host
  log; "open" is unobserved.
- **Inv. 16 reinterpreted** to "the refresh / long-lived grant never enters the
  container." For OAuth this is *stronger* than the prior materialized posture
  (which bind-mounted the refresh token in). A short-lived access token now lives
  in-container by design. Static keys (D5) are the declared exception.
- **Inv. 23 reinterpreted:** the resident hosts a **token service**, not an
  egress proxy. It still effects every spawn and holds the file plane and the
  container control socket.
- **Reversibility:** dropping the proxy removes the only in-path point that could
  inject a static key or audit egress; re-introducing either is ADR-018
  territory and a fresh operator decision.
- **Pinned by** `docs/phase3-contract.md` §3 — the *Credential projection* and
  *Forbidden env* rows. The *Gateway + three egress modes* row is removed.
- **Residual risks / spikes:**
  - `CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST` — **RESOLVED 2026-07-21 (S9).** The
    mode exists in 2.1.217 and is now the primary D3 mechanism: it bypasses the
    credential store, makes the token non-refreshable in-container, and renders
    the `invalid_grant` wipe unreachable. Evidence:
    `spikes/09-credential-projection/RESULT.md` Q1. Still to confirm live: the
    exact in-container delivery channel (env vs file-descriptor vs well-known
    file) and host-creds-file 401 rotation under a non-desktop entrypoint —
    Docker acceptance lane, not a blocker.
  - Real-token Codex — **RESOLVED 2026-07-21 (S9).** A real signed JWT with
    ~10-day `exp` deterministically skips the startup refresh (exp parsed
    without signature check; refresh only within 5 min of expiry). The D4 broker
    becomes optional on the startup happy-path but stays **mandatory** for
    reactive-401 / long-session / malformed-projection fallback. Evidence:
    `spikes/09-credential-projection/RESULT.md` Q2.
  - VirtioFS/bind-mount `mtime` propagation — still open. Claude's long-session
    token-source rewrite relies on `mtimeMs` changing; validate on the container
    FS (AGENTS.md §9 flags VirtioFS watchers as poll-only). Docker acceptance
    lane.
