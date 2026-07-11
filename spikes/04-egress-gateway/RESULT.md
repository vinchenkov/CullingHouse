# S4 — Egress gateway + CA trust per harness: RESULT

Date: 2026-07-10 · Status: **GREEN** — every assertion passed live except the
Docker-Desktop-restart interaction, which is deferred to the serialized leg.

Gateway: throwaway host-side mitmproxy (via `uvx --from mitmproxy mitmdump`),
one process, two listeners:

- `:8080` regular mode — HTTPS forward proxy **with TLS interception**, leaf
  certs signed by the MC dev CA (`~/.mc-dev-home/ca`); used by the codex and
  claude legs.
- `:8081` reverse mode → `https://api.minimax.io` — **no TLS interception of
  the client hop**; the gateway terminates plain HTTP from the container and
  re-originates real, verified TLS upstream. The MiniMax `Authorization`
  header is injected **at the gateway** (`gateway_addon.py`); the key is read
  at runtime from `OPERATOR-INPUTS.md` into the gateway process env only.

Secrets discipline: `ca.key` and the mitmproxy signing bundle live host-side
under `/tmp/mcspike-04/mitm-confdir` (0600) and are never mounted; containers
only ever see `ca.crt`. `check.sh` greps the spike dir for the live key value
on every run (clean) and asserts no probe mounts `ca.key`.

## Assertion table

| # | Assertion | Result | Evidence |
|---|---|---|---|
| 1 | Gateway starts; MITM leafs chain to the MC dev CA | PASS | check.sh: curl via proxy with `--cacert ca.crt` → 404 from api.anthropic.com root |
| 2 | minimax leg: container carries NO key, dummy bearer swapped at gateway | PASS | `out/probe1-minimax.log` — `inject` events; env scan clean in-container |
| 3 | minimax live no-op turn (buffered POST) via `ANTHROPIC_BASE_URL` shape | PASS | 200, model `MiniMax-M3`, reply "OK" |
| 4 | minimax live STREAMING turn (SSE) | PASS | gateway saw `text/event-stream`, 8 `data:` events, streamed pass-through; client counted 8 |
| 5 | codex 0.144.1 honors `CODEX_CA_CERTIFICATE` (version floor pinned) | PASS | run A: live turn "OK" with only `CODEX_CA_CERTIFICATE=/ca/ca.crt` set |
| 6 | codex `SSL_CERT_FILE` fallback honored | PASS | run B: live turn "OK" with only `SSL_CERT_FILE=/ca/ca.crt` set |
| 7 | codex streaming path through MITM | PASS | **WebSocket**, not SSE: `GET /backend-api/codex/responses` → 101; 18 ws messages ≈ 275 KB per turn counted at the gateway |
| 8 | codex CA removed → clean TLS failure, NO silent/metered fallback | PASS | run C: `invalid peer certificate: UnknownIssuer` on wss, then its own HTTPS fallback transport fails the same way; exit 1; `tls_failed_client` at gateway; no 200 crossed; no `OPENAI_API_KEY` existed to fall back to |
| 9 | claude 2.1.207 honors `NODE_EXTRA_CA_CERTS` in the REAL process env | PASS | run A: live no-op "OK" through MITM (settings-file variant NOT retested — trusted prior: reported-but-not-honored) |
| 10 | claude live streaming turn through MITM | PASS | run B: "1..5"; gateway counted 8 SSE `data:` events per `/v1/messages` flow, streamed pass-through |
| 11 | claude CA removed → clean TLS failure, no fallback | PASS | run C: `API Error: Unable to connect to API: SSL certificate verification failed…`, exit 1, `tls_failed_client`, no anthropic 200 |
| 12 | Direct non-proxy egress refused (network shape) | PASS | internal net: by-name → `Could not resolve host`; by-IP `1.1.1.1:443` → connect refused |
| 13 | Gateway-only egress usable from the internal net, incl. a live turn | PASS | proxied curl 404 + live claude turn with 8-event SSE via relay→gateway |
| 14 | Forbidden-env scan in every container (Inv. 13) | PASS | every probe guards `ANTHROPIC_API_KEY`/`CODEX_API_KEY`/`OPENAI_API_KEY` (+`CLAUDE_CODE_OAUTH_TOKEN` on claude legs) before running anything |
| 15 | Gateway/containers across a Docker Desktop restart | DEFERRED | serialized leg owns Docker restarts |

## The fail-closed network mechanism (promote to Phase 3's gateway probe)

1. `docker network create --internal mcspike-04-int` — containers on it have
   no route to host/LAN/internet; Docker's embedded DNS refuses external
   names (`curl: (6) Could not resolve host`), and direct-by-IP connects fail
   (`curl: (7)`), so both DNS-based and DNS-free exfil paths are dead.
2. A dual-homed **relay** (`alpine/socat`, no secrets, no CA key) joins both
   the internal net and bridge, forwarding `:8080` →
   `host.docker.internal:8080` (mapped via `--add-host …:host-gateway`) to the
   host-side gateway.
3. Agent containers run internal-only with `HTTPS_PROXY=http://mcspike-04-relay:8080`
   — the relay is the single reachable egress, so the proxy cannot be
   bypassed. A live claude turn ran end-to-end in this exact shape.

`network_allow` (spec §11.4 raw-TCP plane) maps to additional dual-homed
relays or per-port socat forwards on the same pattern; nothing here forecloses
it.

## ADR-005 evidence (minimax: base_url routing instead of MITM)

The recommended day-2 simplification is confirmed end-to-end:

- Container env: `ANTHROPIC_BASE_URL=http://host.docker.internal:8081/anthropic`
  (mirrors OPERATOR-INPUTS' `https://api.minimax.io/anthropic`), dummy bearer,
  **no key, no CA, no TLS interception client-side**.
- Gateway reverse listener re-originates verified TLS to api.minimax.io and
  swaps `Authorization` (and drops `x-api-key` so a dummy can't shadow).
- Both a buffered no-op POST (200, "OK") and a true streamed SSE turn
  (8 `data:` events, pass-through chunks) worked against `MiniMax-M3`.
- Inv. 16 becomes a *fact* for this binding: nothing readable in the
  container holds the credential.

## Findings (feed into Phase 3 design)

1. **codex 0.144.1 streams over WebSocket first** (`wss://chatgpt.com/backend-api/codex/responses`),
   falling back to HTTPS SSE on its own. The gateway must MITM websockets
   (mitmproxy does out of the box), and any egress-audit/event-count logic
   needs a ws path, not just SSE. Fail-closed held on *both* transports.
2. **Version floor pinned**: codex 0.144.1 + `CODEX_CA_CERTIFICATE` verified
   live (handoff open question #10). `SSL_CERT_FILE` works as the fallback
   var. CA vars were passed explicitly as real container env (`docker -e`) per
   the sandbox env-filtering prior; not re-derived.
3. **Compressed SSE hides audit**: harnesses request gzip/br, so an auditing
   gateway that wants event-level visibility must force
   `Accept-Encoding: identity` (what the spike does; `MC_FORCE_IDENTITY=1`)
   or decompress. Streaming itself works either way.
4. **Egress inventory observed** (for future `egress_policy: none|allowlist`
   provider-endpoint lists): claude → `api.anthropic.com`,
   `http-intake.logs.us5.datadoghq.com` (telemetry); codex → `chatgpt.com`,
   `ab.chatgpt.com` (otlp telemetry). Telemetry hosts will need an
   allow-or-expect-noise decision.
5. **MiniMax Anthropic-compatible endpoint auths on `Authorization: Bearer`**;
   the gateway drops `x-api-key` on that leg.
6. claude with a proxied-but-untrusted CA fails with a clear, operator-legible
   error (`SSL certificate verification failed. Check your proxy or corporate
   SSL certificates`) — good diagnostics for `mc doctor`.

## Artifacts

- `start_gateway.sh` — builds the mitm confdir from `~/.mc-dev-home/ca`
  (host-side, 0600), starts mitmdump with both listeners, reads the MiniMax
  key at runtime from OPERATOR-INPUTS.md (env only, never disk).
- `gateway_addon.py` — header injection (minimax host only), SSE pass-through
  chunk counter, websocket message counter, `tls_failed_client` audit,
  JSONL flow log at `/tmp/mcspike-04/logs/flows.jsonl`.
- `probes/probe1-minimax.sh` · `probes/probe2-codex.sh` ·
  `probes/probe3-claude.sh` · `probes/probe4-failclosed-net.sh`
- `check.sh` — non-live lane always; `--live [all|minimax|codex|claude|net]`
  for the turn-burning legs; stops the gateway unless `--keep-gateway`.
- `out/` — captured logs (token prefixes redacted to 8 chars; secret-scanned).

## Rerun

```sh
cd spikes/04-egress-gateway
./check.sh                 # no live turns; gateway+CA+fail-closed hygiene
./check.sh --live all      # full rerun (~7 tiny turns: 2 minimax, 2 codex,
                           # 2 claude, 1 claude via internal-net relay)
```

Preconditions: Docker Desktop up; `~/.mc-dev-home/ca/{ca.crt,ca.key}` and the
canonical cred copies (`cred/codex/auth.json`, `cred/claude/.credentials.json`)
materialized; `OPERATOR-INPUTS.md` present; image `mcspike-08-base:latest`
built (S8). Ports 8080/8081 free on the host.
