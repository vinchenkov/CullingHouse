# ADR-024 — Homie dashboard: framework, read path, and the Console slice

## Status

Accepted (2026-07-22). Scope: the dashboard process's implementation stack,
its spine access path, and the Phase 4 S6 Console slice. The spec (§12, §13,
§15.4–15.7, §16.1, §16.3, Inv. 15/23/24) remains the authority on what the
dashboard is; this ADR records the delegated implementation choices only.
Classification (AGENTS.md §6): explicitly delegated — §16.1 fixes the stack
(TypeScript on Bun) and delegates the rest; §13/§15.4 fix the surfaces.

## Context

Phase 4 family (6) needs the Console: a browser surface that lists Homie
sessions, renders a conversation, and sends operator text into it. The spec
pins the hard constraints:

- The dashboard is a native host process, loopback by default, stateless,
  holding no authoritative state and running no agent logic (Inv. 23, §12).
- It may never open the spine: every read and write crosses the host→runtime
  boundary as a real `mc` invocation (Inv. 24, §11.5, §15.1). It may not
  branch on the runtime (Inv. 15).
- Delivery to the dashboard is pull-based (`mc homie history` + `mc outbox
  poll/ack`) — recorded operator decision, ledger phase-4 2026-07-22.
- Stack: TypeScript on Bun, tracked in repo, dependencies pinned with a
  committed lockfile (§16.1).

Undecided until now: the HTTP framework, the frontend approach, the API
shape, and how the Playwright smoke runs without Docker.

## Decision

**D1 — Zero-framework Bun package `dashboard/`.** The server is `Bun.serve`
on the pinned mise Bun, following the `resident/src/refresh-broker.ts`
precedent; no HTTP framework, no router dependency, no build step. The
frontend is vanilla ES-module JavaScript + one HTML page served as static
files from `dashboard/src/ui/`. Runtime dependency count: zero. The package
follows repo convention (`mc-dashboard`, `"type": "module"`, `src/` layout,
colocated `bun:test` files, `check.sh` = `bun test src/`), and its check.sh
joins the fast-suite chain as a sixth leg.

**D2 — The spine is reached only by spawning `mc`.** Every data access is a
child-process invocation of the real `mc` binary — one JSON object on stdout,
exit 0/1/2 — through a single injected seam (`McRunner`), which tests replace
with a scripted fake. The binary path comes from config (`mc_bin`); the
process environment passes through untouched, so the production darwin binary
self-delegates into the helper container and the `test_fake_routing` build
honors `MC_SPINE`/`MC_HELPER` — the dashboard cannot tell and never asks
(Inv. 15/24). No SQLite driver may ever appear in this package.

**D3 — The HTTP API is a thin verb mirror.** Routes map one-to-one onto `mc`
verbs; the server adds no interpretation, no cache, no durable state:

- `GET  /api/sessions`              → `mc homie list`
- `GET  /api/sessions/:id/history`  → `mc homie history <id>`
- `POST /api/sessions`              → `mc homie start --from dashboard:<ref>`
- `POST /api/sessions/:id/send`     → `mc homie send <id> --from dashboard:<ref> --body …`
- `POST /api/sessions/:id/resume`   → `mc homie resume <id> --from dashboard:<ref>`
- `POST /api/outbox/drain`          → `mc outbox poll --surface dashboard` + `mc outbox ack` each row

Verb failures pass through as `{error:{code,message}}`: caller-caused
rejections (domain, usage) are 400, genuine environment failures 500 (mc
exit 2 covers both, disambiguated by the envelope's own code); error text
never instructs a human to run an `mc` command (§17). Free operator text travels only as the body of `homie send` —
the dashboard never parses it (§15.2).

**D4 — Bind and auth are fail-closed, and the browser boundary is
enforced.** Default bind `127.0.0.1:7333` (§15.7). A non-loopback bind in
config is refused at startup unless `auth_token` is set; when `auth_token`
is set, every `/api` request must carry `Authorization: Bearer <token>` or
is rejected 401 before any `mc` spawn (the data-free static shell stays
reachable so a browser can load it and present the token, supplied once via
the URL fragment and held per-tab). Independent of the token, every request
must name this server: a Host header not matching the bind host or a
loopback spelling is 403 (DNS rebinding), any present Origin header not
matching the same set — including `null` — is 403 (cross-site request
forgery), and JSON bodies are accepted only with an `application/json`
content type (no cross-site "simple request" smuggling).

**D5 — The dashboard's channel ref is per-session and derived, not stored.**
A `(surface, channel_ref)` place binds to exactly one session
(`ensureActiveBinding`), so a single shared `dashboard:console` ref cannot
serve multiple sessions. The dashboard derives its ref statelessly: use the
session's existing `dashboard` binding from `mc homie list`'s
`active_bindings` when present; otherwise generate `dash-<8 hex>` and let
first traffic auto-bind it. `POST /api/sessions` generates the ref the same
way. Nothing is persisted server-side (Inv. 23: on death, restart, re-read).

**D6 — Pull loop with adaptive polling.** The Console polls
`GET …/history` — tight (1 s) for 30 s after an operator send, decaying to
lazy (10 s) when idle (§15.4 "adaptive polling"). Because history is the
render source, `homie_reply`/`homie_echo` outbox rows addressed to the
dashboard surface are delivery bookkeeping only: the poll loop drains and
acks them trivially via `/api/outbox/drain` (§15.4 "dashboard rows ack
trivially"). At-least-once ack is idempotent; a crash between poll and ack
re-delivers, which the dashboard ignores by construction.

**D7 — Two test lanes.** (1) Fast lane: `bun:test` unit tests against the
`McRunner` fake — API mapping, bind/auth refusal, channel-ref derivation —
Docker-free, token-free, in `check.sh`. (2) Smoke lane, not in the fast
suite: `dashboard/smoke.sh` builds the `test_fake_routing` `mc`, provisions a
scratch spine under `/private/tmp` (routing.md all-fake, `mc init`), starts
the real server, and drives a real Chromium via the Playwright **library**
inside `bun test smoke/` — load page, start session, send "ping", then the
test plays the runner with a fabricated `tier:"homie"` run.json
(`MC_RUN_JSON` claim → reply), and asserts the reply renders. Playwright is
the package's only dependency (dev, pinned, committed `bun.lock`, §16.1);
browsers install via `bunx playwright install chromium` in smoke.sh.

**D8 — S6 ships the Console only.** The page carries the five-tab shell
(§13) with Board, Review Queue, Activity, and System Health as inert
placeholders; each activates with its subsystem in later work. LaunchAgent
unit generation belongs to install/onboard (§16.1) and is not built here;
during development the server is started manually (AGENTS.md §9: never load
launchd). No trace view, no token/cost metrics, ever (§13).

## Consequences

- Zero runtime dependencies keeps the frozen-lockfile obligation (§16.1)
  trivial: the lockfile pins only Playwright, and the fast suite needs no
  install step.
- The `McRunner` seam makes every spine interaction observable in unit tests
  and keeps Inv. 24 auditable by grep: no `sqlite` import, no db path.
- Deriving the channel ref from `active_bindings` costs one `mc homie list`
  per send but keeps the process stateless; a cache would be the first
  authoritative state and is refused (Inv. 23).
- Pull-based polling bounds staleness at the lazy interval; a push channel
  (SSE/WebSocket) is a compatible later addition behind the same API and is
  deliberately not built now.
- Pinned by: `dashboard/src/*.test.ts` (API mirror, fail-closed bind/auth,
  ref derivation, trivial ack) and `dashboard/smoke/` (browser round-trip).
