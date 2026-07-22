# S9 — credential-projection spikes — RESULT

Date: 2026-07-21 · Method: static source inspection only (no live provider
calls, no real credentials read). Claude Code **2.1.217** bundle
(`/opt/homebrew/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`,
minified JS); Codex CLI **0.144.6** source at git tag `rust-v0.144.6`
(commit `1e66aaa`, matches the installed binary exactly).

**Verdict: GREEN on both unknowns.** Q1 simplifies the D3 writer materially;
Q2 makes the D4 broker optional on the startup happy-path but keeps it as the
mandatory fallback. Neither triggers a fallback ADR; ADR-022's core
access-in/refresh-out property stands. Both call for an ADR-022 amendment (see
IMPLEMENTATION-NOTES.md 2026-07-21).

---

## Q1 — `CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST` EXISTS and replaces the D3 dance

The flag is real in 2.1.217 (41× `PROVIDER_MANAGED`, 13× `managedByHost` in
the bundle; undocumented in `--help`, no changelog). Predicate
`RE(){return Z.CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST}`, parsed as bool
(accepts `1/true/yes/on`).

**Core effect — the credential store is bypassed, not reinterpreted.** Token
resolution (`ys()`/`C2()`) under the flag becomes, in order:

1. `CLAUDE_CODE_OAUTH_TOKEN` env → used as `accessToken` with
   **`refreshToken:null, expiresAt:null`** → no expiry-driven refresh can ever
   fire.
2. token from `CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR` or the well-known file
   `/home/claude/.claude/remote/.oauth_token`, same shape; scopes default
   `["user:inference"]` (override `CLAUDE_CODE_OAUTH_SCOPES`).
3. otherwise `null` — the `Fc().read()?.claudeAiOauth` branch (`.credentials.json`
   / keychain) is **unreachable**: `if(RE())return null` precedes it.

Alternatively the host supplies a plain bearer via `ANTHROPIC_AUTH_TOKEN` (or
the var named by `CLAUDE_CODE_HOST_AUTH_ENV_VAR`).

**Why this is safer than the POC's file-rewrite (D3 as written).** The
`invalid_grant` compare-and-swap **wipe** (`yji()` → `Fc().mutate(... refreshToken:"",
accessToken:"", expiresAt:0)`) is structurally unreachable: `yji()` returns
`"no_refresh_token"` unless `C2()` yields a `refreshToken`, and under the flag
`C2()` never sources one. No refresh HTTP call is made → no `invalid_grant` →
no wipe. The dead-refresh detector `fnt()` short-circuits `if(RE())return!1`.
The 401 disk-reread is gated off: `if(!RE()&&(...))` guards the
`readAsync()?.claudeAiOauth` recovery. So the projected file need contain
**nothing**; the future-`expiresAt` + non-empty-dummy-refresh + `user:inference`
constraints the POC discovered are all obviated.

**Rotation channels (both flag-gated).**
- `CLAUDE_CODE_HOST_CREDS_FILE`: a `0600`, uid-owned, ≤64 KB JSON
  `{env:{<allowlisted vars>}, pid, procStart, expiresAt|null}` loaded at
  startup and **re-read on 401** via a registered host-auth callback. Re-read
  rejects an endpoint change; `pid` must be alive with `procStart` matching
  within 2 s. Allowlisted env keys include `ANTHROPIC_AUTH_TOKEN`,
  `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_*_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS`.
- Desktop/SDK entrypoints only (`claude-desktop`/`-3p`/`local-agent`): a
  `host_auth_token_refresh` control request gated by
  `CLAUDE_CODE_SDK_HAS_HOST_AUTH_REFRESH` — **not** our entrypoint, so not our
  path.

**Also under the flag:** host routing wins over settings; `apiKeyHelper` /
`awsAuthRefresh` / `gcpAuthRefresh` / keychain lookups disabled;
`forceLoginMethod`/`forceLoginOrgUUID` policy pins bypassed; provider/auth env
vars stripped from child processes; on 401 with no rotation channel it throws
`api_request_host_managed_auth_fail` — **fail-fast, no refresh, no disk mutation.**

**Fail-closed check:** unset/absent token → resolves `null` → the run stalls
with no leak; a stale bearer 401s and, without a host-creds callback, fails
fast. No credential escapes. Matches ADR-022 D8 posture and is cleaner than
the wipe path.

**Design consequence for D3.** Prefer the flag. Projection writer sets
`CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST=1` and delivers the host-minted access
token via `CLAUDE_CODE_OAUTH_TOKEN` (or the well-known file / creds file for
in-session rotation), instead of rewriting `.credentials.json`. The
refresh-ahead loop still rewrites the token source before expiry, but it no
longer risks clobbering or a wipe, and it does not depend on a lapsed-`expiresAt`
self-refresh being avoided. Keep `ANTHROPIC_API_KEY` **unset** (unchanged).

**Residual (must still confirm live before wiring):** the exact in-container
delivery — env vs `CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR` vs the well-known
file — and that the host-creds-file 401 rotation callback behaves under our
`local-agent`-shaped (non-desktop) entrypoint. Static reading is unambiguous on
the resolution order; a mock run confirms the delivery channel. This is a
container-side check for the Docker acceptance lane, not a host spike blocker.

## Q2 — a real fresh Codex token skips startup refresh; broker stays as fallback

Gate: `AuthManager::should_refresh_proactively`
(`codex-rs/login/src/auth/manager.rs:2506`), for `CodexAuth::Chatgpt` only:

1. Parse the access-token JWT `exp` via `parse_jwt_expiration`
   (`token_data.rs`) — base64-decode of the **payload only, no signature
   verification**; requires a well-formed 3-segment `header.payload.sig` (all
   non-empty) with an integer `exp`. Refresh iff `exp <= now + 5min`
   (`CHATGPT_ACCESS_TOKEN_REFRESH_WINDOW_MINUTES`).
2. If `exp` absent / JWT malformed → fall back to `last_refresh`: refresh iff
   `last_refresh < now - 8 days` (`TOKEN_REFRESH_INTERVAL`).
3. `last_refresh` is `None` → **false** (no refresh).

**Prediction (high confidence): a real freshly-minted access token (signed JWT,
~10-day `exp`) SKIPS the startup refresh.** `exp` parses ~10 days out → well
outside the 5-minute window → `false`, and the 8-day `last_refresh` fallback is
never consulted. Signature validity is irrelevant to the gate — Codex never
verifies it; it only needs the signature *segment* present. The POC's synthetic
token forced a refresh because it was `exp`-less/malformed and fell through to a
stale/absent `last_refresh`, **not** because Codex checks signatures. This
reconciles the S3 (0.144.1: access-expiry triggers) and POC-v2 (0.144.6:
synthetic startup refresh) findings.

**Broker (`CODEX_REFRESH_TOKEN_URL_OVERRIDE`, `manager.rs:191`,
`refresh_token_endpoint()` at `:1455`) stays REQUIRED for:**
1. reactive 401 mid-session → `refresh_token_from_authority_impl` →
   `request_chatgpt_token_refresh` → the override endpoint (the exact path the
   live POC exercised);
2. long sessions that cross into the final 5 minutes before `exp`;
3. defense against a malformed/`exp`-less projection falling to the
   `last_refresh` path.

The guarded reload is `account_id`-gated
(`reload_if_account_id_matches`, `:2109`) and `auth.json` is rewritten only on a
**successful** refresh (`persist_tokens`, `:1306`), so the projection must
preserve `tokens.account_id` and `auth_mode:"chatgpt"` (ADR-022 D4, unchanged).

**Design consequence for D4.** Keep the broker — it is not eliminable. The
happy path may skip it when projection supplies a valid, signed, well-formed
fresh JWT per invocation, but the broker is the mandatory 401/long-session
fallback and preserves the fail-closed posture. Do not treat "fresh-token
projection" as a reason to drop the override.

## Out of scope (unchanged)

VirtioFS/bind `mtime` propagation for the Claude refresh-ahead rewrite
(ADR-022 residual #3) → Docker acceptance lane. Live delivery-channel
confirmation for Q1 → same lane. No launchd, no Docker restart in this spike.
