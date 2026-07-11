"""Mission Control S4 egress-gateway mitmproxy addon.

Two responsibilities, both toggled by env so the same addon serves every leg:

1. Credential injection (minimax reverse-proxy leg, NO TLS interception of
   the client hop). When MC_INJECT_AUTH=1 the addon replaces the inbound
   Authorization header with the real MiniMax bearer key, which is read ONCE
   from the environment (MC_MINIMAX_KEY) at startup. The container therefore
   never carries the key; it sends a dummy bearer that the gateway swaps at
   the edge. The secret is never logged (first 8 chars max, per spike
   secrets discipline).

2. Flow audit + streaming evidence (all legs). Every response is recorded to
   MC_LOG_PATH as one JSON line: host, path, method, status, content-type,
   whether the body is an SSE stream (text/event-stream) and, for SSE, the
   number of `data:` events observed. SSE bodies are counted by a real
   pass-through stream wrapper installed in `responseheaders` — the body is
   forwarded chunk-by-chunk as it arrives (never buffered), so this is
   machine-checkable evidence that each leg exercised a STREAMING path, not
   just a single buffered POST.

3. TLS handshake audit (fail-closed evidence). tls_failed_client events are
   logged, so "CA removed -> clean TLS failure at the client hop, no request
   ever leaves" is observable in the same flow log.

No secret value is ever written to MC_LOG_PATH or to stdout/stderr.
"""

import json
import os
import time

from mitmproxy import ctx, http

MC_LOG_PATH = os.environ.get("MC_LOG_PATH", "/tmp/mcspike-04/logs/flows.jsonl")
MC_INJECT_AUTH = os.environ.get("MC_INJECT_AUTH", "") == "1"
MC_INJECT_HEADER = os.environ.get("MC_INJECT_HEADER", "Authorization")
MC_INJECT_SCHEME = os.environ.get("MC_INJECT_SCHEME", "Bearer ")
# Spike-only: force identity encoding so streamed SSE chunks are countable
# (harnesses request gzip/br; compressed chunks hide the `data:` markers from
# the pass-through counter). A production gateway would not need this — it is
# purely for sharp audit evidence in the spike.
MC_FORCE_IDENTITY = os.environ.get("MC_FORCE_IDENTITY", "1") == "1"
# Secret pulled from env once; kept only in memory, never logged in full.
_SECRET = os.environ.get("MC_MINIMAX_KEY", "")


def _redact(tok: str) -> str:
    if not tok:
        return ""
    return tok[:8] + "...REDACTED"


def _log(rec: dict) -> None:
    rec["ts"] = time.time()
    try:
        with open(MC_LOG_PATH, "a") as fh:
            fh.write(json.dumps(rec) + "\n")
    except Exception as exc:  # never crash the proxy on a log failure
        ctx.log.warn(f"mc-gateway: log write failed: {exc}")


def load(loader):
    ctx.log.info(
        f"mc-gateway addon loaded: inject_auth={MC_INJECT_AUTH} "
        f"header={MC_INJECT_HEADER} key_present={bool(_SECRET)} "
        f"key_prefix={_redact(_SECRET)} log={MC_LOG_PATH}"
    )


def request(flow: http.HTTPFlow) -> None:
    if MC_FORCE_IDENTITY:
        flow.request.headers["accept-encoding"] = "identity"
    # Injection is scoped to the minimax reverse-proxy leg only, so it never
    # clobbers the OAuth Authorization that codex/claude carry through the MITM
    # forward proxy on the same mitmdump process.
    if MC_INJECT_AUTH and _SECRET and "minimax" in flow.request.host:
        # Replace whatever dummy bearer the container sent with the real key.
        flow.request.headers[MC_INJECT_HEADER] = MC_INJECT_SCHEME + _SECRET
        # MiniMax's Anthropic-compatible endpoint authenticates on Authorization;
        # drop x-api-key so it can't shadow / leak a dummy.
        if "x-api-key" in flow.request.headers:
            del flow.request.headers["x-api-key"]
        _log(
            {
                "event": "inject",
                "host": flow.request.host,
                "path": flow.request.path.split("?")[0],
                "method": flow.request.method,
                "injected_header": MC_INJECT_HEADER,
                "key_prefix": _redact(_SECRET),
            }
        )


def responseheaders(flow: http.HTTPFlow) -> None:
    """Install a pass-through chunk counter on SSE responses.

    Setting flow.response.stream makes mitmproxy forward the body to the
    client as it arrives from the server (true streaming, no buffering).
    The wrapper counts `data:` event markers and bytes as they fly past.
    """
    resp = flow.response
    if resp is None:
        return
    ctype = resp.headers.get("content-type", "")
    if "text/event-stream" in ctype:
        flow.metadata["mc_sse"] = True
        flow.metadata["mc_sse_events"] = 0
        flow.metadata["mc_sse_bytes"] = 0

        def counter(chunk: bytes) -> bytes:
            if chunk:
                flow.metadata["mc_sse_events"] += chunk.count(b"data:")
                flow.metadata["mc_sse_bytes"] += len(chunk)
            return chunk

        resp.stream = counter


def response(flow: http.HTTPFlow) -> None:
    # Fires after the full body has been read/streamed.
    resp = flow.response
    ctype = resp.headers.get("content-type", "") if resp else ""
    is_sse = bool(flow.metadata.get("mc_sse")) or "text/event-stream" in ctype
    _log(
        {
            "event": "response",
            "host": flow.request.host,
            "path": flow.request.path.split("?")[0],
            "method": flow.request.method,
            "status": resp.status_code if resp else None,
            "content_type": ctype,
            "sse": is_sse,
            "sse_events": flow.metadata.get("mc_sse_events"),
            "sse_bytes": flow.metadata.get("mc_sse_bytes"),
            "streamed": bool(flow.metadata.get("mc_sse")),
        }
    )


def websocket_message(flow: http.HTTPFlow) -> None:
    # codex 0.144.1's primary streaming transport is a WebSocket
    # (wss://chatgpt.com/backend-api/codex/responses). Count messages as they
    # pass through the MITM'd tunnel — the websocket analogue of SSE events.
    ws = flow.websocket
    flow.metadata["mc_ws_msgs"] = flow.metadata.get("mc_ws_msgs", 0) + 1
    if ws and ws.messages:
        flow.metadata["mc_ws_bytes"] = flow.metadata.get("mc_ws_bytes", 0) + len(
            ws.messages[-1].content
        )


def websocket_end(flow: http.HTTPFlow) -> None:
    _log(
        {
            "event": "websocket_end",
            "host": flow.request.host,
            "path": flow.request.path.split("?")[0],
            "ws_messages": flow.metadata.get("mc_ws_msgs", 0),
            "ws_bytes": flow.metadata.get("mc_ws_bytes", 0),
        }
    )


def error(flow: http.HTTPFlow) -> None:
    _log(
        {
            "event": "error",
            "host": flow.request.host if flow.request else None,
            "path": flow.request.path.split("?")[0] if flow.request else None,
            "error": str(flow.error) if flow.error else None,
        }
    )


def tls_failed_client(data) -> None:
    # Client refused our leaf cert (e.g. CA not trusted in the container):
    # the fail-closed evidence — the request dies at the handshake, nothing
    # is forwarded upstream.
    try:
        sni = data.context.client.sni
    except Exception:
        sni = None
    _log({"event": "tls_failed_client", "sni": sni})
