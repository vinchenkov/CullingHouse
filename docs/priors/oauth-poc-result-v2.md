# Credential-injection POC v2 — free-internet model

**Question this POC answers (the load-bearing unknown):** with the agent
container on the *open internet* (no network containment), can the host keep the
OAuth **refresh** token to itself, project only a short-lived **access** token to
the runtime, and have the runtime **adopt** a host-rewritten token — or does the
SDK hard-fail when it can't self-refresh? Tested live for **both** Claude Code
and Codex CLI.

**Method.** A stdlib mock provider stood in for api.anthropic.com / chatgpt.com.
Every token was **synthetic** (fake unsigned JWTs minted locally); no real
credential value was ever read or transmitted. The operator's real
`~/.codex/auth.json` was never touched (verified: mtime/last_refresh unchanged).
Scratch `CLAUDE_CONFIG_DIR` / `CODEX_HOME` only.

Installed versions under test: Claude Code **2.1.216**, Codex CLI **0.144.6**
(arm64).

---

## 0. Reframe: free internet deletes network containment

Per the operator: **"the agent can browse the internet freely — that is the
requirement."** So the v2 design drops everything the earlier recommendation
built for a sealed container: no `--network none`, no loopback→UDS forwarder, no
broker-as-egress-gateway, no endpoint allowlist. The container has a normal
network stack and reaches providers directly.

The **only** blast-radius control is therefore the credential boundary:
**access token in, refresh token out.** With free egress there is no network
backstop, so this boundary is not defense-in-depth — it is the entire security
property. A leaked access token is short-lived and non-refreshable; the refresh
grant never enters the container.

---

## 1. Result — Claude Code (subscription OAuth)

**Adoption: YES, per invocation.** Host rewrote `accessToken` TOKA→TOKB between
two `claude -p` runs; the mock saw TOKB on the second run. Each invocation
re-reads `.credentials.json`; a long-lived session re-reads on the next request
after the file mtime changes (source-confirmed `dog()` mtime poll). **200
end-to-end**, and the request carried the `oauth-2025-04-20` beta → OAuth mode
engaged.

**Hard constraints (all live-verified on 2.1.216):**

| Condition | Result |
|---|---|
| `refreshToken` = dummy non-empty **+ future `expiresAt`** + `user:inference` scope | **WORKS**, adopts host rewrites, never refreshes, file not clobbered |
| `refreshToken` = `""` (empty) | **HARD FAIL** — "OAuth session expired and could not be refreshed"; **no request made**; file NOT wiped |
| `refreshToken` absent | **HARD FAIL** (same) |
| valid token but `expiresAt` in the past | **HARD FAIL** — trusts `expiresAt`, tries self-refresh first, never sends the good token |
| `ANTHROPIC_AUTH_TOKEN` bearer (no file) | sends bearer but **no oauth beta** → unusable for subscription against the real API |

> Note: this **overturns** the source-only advice to use an empty refreshToken.
> The runtime is authoritative; empty refresh hard-fails in 2.1.216.

**Claude projection contract:**
- Write a **non-empty dummy** `refreshToken`, a **future `expiresAt`**, and
  `scopes` including `user:inference` alongside each host-minted `accessToken`.
- Keep `expiresAt` comfortably ahead of real expiry and rewrite before it lapses
  → Claude never enters its refresh path, so it never (a) writes a refreshed
  token back and clobbers the projection, nor (b) hits `invalid_grant` and wipes
  the record (a real risk *only if* a refresh is actually attempted).
- Do **not** set `ANTHROPIC_API_KEY` (disables OAuth mode). `ANTHROPIC_BASE_URL`
  redirects the messages API but **not** the refresh endpoint (hardcoded to
  platform.claude.com; override is allowlist-gated). So a Claude host-side
  refresh-*broker* via URL redirect is **not** clean — Claude relies on the host
  keeping the file fresh instead.

**Residual risk:** if the host is late and `expiresAt` lapses (or a 401 fires),
Claude attempts the dummy refresh → `invalid_grant` → compare-and-swap **wipe**
of accessToken/refreshToken/expiresAt on disk. Fail-closed (no leak), but the
agent stalls until the host rewrites. Mitigation: generous refresh lead time.

---

## 2. Result — Codex CLI (ChatGPT subscription OAuth)

**Adoption: YES.** `codex exec` builds a fresh AuthManager and reads `auth.json`
per invocation (source-confirmed). Live end-to-end proof of the **host
refresh-broker** path:

```
seq21/22  POST /backend-api/codex/responses   bearer=STALE   -> 401
seq23     POST /oauth/token (refresh)          -> serviced HOST-SIDE, returns FRESH
seq24     POST /backend-api/codex/responses   bearer=FRESH   -> 200   ✅
```

Codex tried the stale token, refreshed, **adopted the host-minted access token**,
and completed the responses call — with **only a dummy refresh token in the
container**. (`exit=1` was solely because the mock returns plain JSON, not an SSE
stream: "stream disconnected before completion". The auth path fully closed at
200; SSE fidelity is a mock gap, not an auth failure.)

**Findings (live + source):**
- Codex **proactively refreshes at startup** and will not trust an unsigned
  synthetic token's `exp`, so pure access-token-only projection couldn't be
  proven with fake tokens — a *real* freshly-minted token (≈10-day validity)
  would likely skip the refresh, but that is unproven here.
- **`CODEX_REFRESH_TOKEN_URL_OVERRIDE`** cleanly redirects the refresh endpoint →
  a **host refresh-broker is first-class for Codex.** The container's inbound
  refresh token is ignored; the host mints from the real refresh token it holds.
- Empty/bogus `refresh_token` is **safe at load** (Option<String>, no wipe);
  `auth.json` is only rewritten on a *successful* refresh or logout.
- Long-lived processes: guarded reload adopts an on-disk token near cached-token
  expiry, **gated on `account_id`** — the projection must preserve
  `tokens.account_id` and `auth_mode`.
- `CODEX_ACCESS_TOKEN` env is **Agent Identity** mode (service accounts,
  prod/staging only) — **not** the subscription path.

**Codex projection contract (two viable shapes):**
- **A. Refresh-broker (proven):** project a stale/short access token + dummy
  refresh; set `CODEX_REFRESH_TOKEN_URL_OVERRIDE` to a host endpoint that mints
  fresh access tokens from the host-held refresh token. Refresh token never in
  container. Redirect the responses call with a `[model_providers.*]` entry
  (`requires_openai_auth=true`, `base_url`) — `chatgpt_base_url` alone does not
  redirect responses in 0.144.6.
- **B. Fresh-token projection:** host re-mints a real access token and writes it
  to `auth.json`; if Codex trusts a real token's `exp` it won't refresh
  (unproven with synthetic tokens).

---

## 3. The asymmetry that matters

| | Claude Code | Codex CLI |
|---|---|---|
| Adopts host-rewritten access token | ✅ per invocation / mtime poll | ✅ per invocation / guarded reload |
| Refresh endpoint redirectable to host | ❌ hardcoded (allowlist-gated) | ✅ `CODEX_REFRESH_TOKEN_URL_OVERRIDE` |
| Host **refresh-broker** viable | ✗ — rely on host keeping file fresh | ✅ proven |
| Refresh-token-out achievable | ✅ dummy refresh + fresh `expiresAt` | ✅ dummy refresh + host broker |
| Empty refresh token | hard-fail (needs dummy) | safe (optional) |
| Wrong-`exp`/stale field | hard-fail until host rewrites | refreshes (→ host broker) |

**Both runtimes satisfy access-in/refresh-out on the open internet** — but via
different mechanisms. Claude: keep the projected file always-valid so it never
refreshes. Codex: let it refresh, but service the refresh from the host.

---

## 4. Recommended follow-ups (not yet proven)
- **`CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST`** — a Claude mode the bundle appears to
  ship purpose-built for host-managed providers (suppresses disk writes and the
  401→disk-reread). Could be a cleaner Claude path than the dummy-refresh dance;
  worth a dedicated spike.
- **VirtioFS/bind-mount mtime propagation** — Claude's long-session adoption
  relies on `mtimeMs` changing; validate on the actual container FS (AGENTS.md §9
  already flags VirtioFS watchers as poll-only).
- **SSE-fidelity mock** — to get a clean `exit=0` from `codex exec`, the mock must
  stream a `response.completed` event; only needed if a full green codex run is
  wanted as evidence.
- **Real-token codex projection (shape B)** — confirm whether a real fresh access
  token skips the startup refresh, which would remove the need for the broker.

## 5. Security posture of this POC
All tokens synthetic; no real credential value read or logged; operator
`~/.codex/auth.json` verified untouched. Two subagent investigations tripped
"credential exploration" heuristics — expected, since the task *is* mapping the
runtimes' credential state machines; it was operator-authorized and stayed
within synthetic/local bounds.
