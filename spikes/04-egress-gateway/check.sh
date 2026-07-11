#!/usr/bin/env bash
# S4 machine-rerunnable check.
#
# Usage:
#   ./check.sh              # non-live lane: gateway up, MITM chain trust,
#                           # fail-closed TLS at the gateway, CA hygiene,
#                           # secret-scan of the spike dir
#   ./check.sh --live       # + all four live probes (burns ~7 tiny
#                           # subscription/minimax turns)
#   ./check.sh --live minimax|codex|claude|net   # one live leg only
#   ./check.sh --keep-gateway   # leave mitmdump running afterwards
#
# Requires: Docker Desktop running, uvx, ~/.mc-dev-home/{ca,cred} materialized,
# OPERATOR-INPUTS.md readable (MINIMAX_KEY is read at runtime by the gateway
# process only; it never lands in any file under the repo).
set -uo pipefail

SPIKE_DIR="$(cd "$(dirname "$0")" && pwd)"
OUT="$SPIKE_DIR/out"; mkdir -p "$OUT"
FLOWS=/tmp/mcspike-04/logs/flows.jsonl
OPINPUTS="${OPINPUTS:-/Users/vinchenkov/Documents/dev/ai/homie/OPERATOR-INPUTS.md}"
CA_DIR="$HOME/.mc-dev-home/ca"

LIVE=""; KEEP=0
while [ $# -gt 0 ]; do
  case "$1" in
    --live) LIVE="${2:-all}"; [ "${2:-}" ] && [[ "$2" != --* ]] && shift ;;
    --keep-gateway) KEEP=1 ;;
    *) echo "unknown arg: $1"; exit 2 ;;
  esac; shift
done

FAIL=0
say()  { echo "[check] $*"; }
pass() { echo "PASS  $*"; }
fail() { echo "FAIL  $*"; FAIL=1; }

# ---------- stage 0: gateway + CA hygiene (no live turns) -------------------
say "starting gateway"
if bash "$SPIKE_DIR/start_gateway.sh" >/dev/null 2>&1; then
  pass "gateway starts (fwd :8080 regular, rev :8081 -> api.minimax.io)"
else
  fail "gateway failed to start"; exit 1
fi

# CA fingerprints: confdir bundle must be the MC dev CA
FP1=$(openssl x509 -in "$CA_DIR/ca.crt" -noout -fingerprint -sha256 2>/dev/null)
FP2=$(openssl x509 -in /tmp/mcspike-04/mitm-confdir/mitmproxy-ca-cert.pem -noout -fingerprint -sha256 2>/dev/null)
[ -n "$FP1" ] && [ "$FP1" = "$FP2" ] \
  && pass "gateway signs with the MC dev CA (fingerprint match)" \
  || fail "confdir CA does not match $CA_DIR/ca.crt"

# ca.key hygiene: host-side only — never referenced as a mount by any probe
if grep -n 'ca\.key' "$SPIKE_DIR"/probes/*.sh | grep -v '^ *#' | grep -q 'docker'; then
  fail "a probe mounts ca.key into a container"
else
  pass "ca.key never mounted into any container (probes mount ca.crt only)"
fi

# MITM chain trust + fail-closed at the client hop, host-side
SNAP=$(wc -l < "$FLOWS" | tr -d ' ')
CODE=$(curl -sS -o /dev/null -w "%{http_code}" --proxy http://127.0.0.1:8080 \
        --cacert "$CA_DIR/ca.crt" --max-time 20 https://api.anthropic.com/ 2>/dev/null)
[ "$CODE" = "404" ] && pass "MITM leaf chains to MC dev CA (client with CA: status 404)" \
                    || fail "MITM chain trust check (status=$CODE)"
if curl -sS -o /dev/null --proxy http://127.0.0.1:8080 --max-time 20 https://api.anthropic.com/ 2>/dev/null; then
  fail "client WITHOUT the CA got through the MITM"
else
  pass "client without the CA fails TLS cleanly"
fi
tail -n +$((SNAP+1)) "$FLOWS" | grep -q '"tls_failed_client"' \
  && pass "gateway logged tls_failed_client for the untrusted client" \
  || fail "no tls_failed_client event in the flow log"

# Secret scan: the MiniMax key must not appear anywhere under the spike dir
KEY="$(grep -E '^MINIMAX_KEY=' "$OPINPUTS" | head -1 | cut -d= -f2-)"
if [ -n "$KEY" ]; then
  if grep -rqF "$KEY" "$SPIKE_DIR"; then
    fail "SECRET LEAK: MiniMax key found under the spike dir"
  else
    pass "secret scan clean (MiniMax key absent from spike dir)"
  fi
else
  fail "could not read MINIMAX_KEY from OPERATOR-INPUTS.md for the scan"
fi

# Forbidden-env guard present in every probe that starts a container (Inv. 13)
MISSING=0
for p in "$SPIKE_DIR"/probes/*.sh; do
  grep -q 'docker run' "$p" || continue
  grep -q 'FORBIDDEN ENV PRESENT' "$p" || { echo "  missing guard: $p"; MISSING=1; }
done
[ $MISSING -eq 0 ] && pass "forbidden-env guard present in every container-starting probe" \
                   || fail "a container-starting probe lacks the forbidden-env guard"

# ---------- stage 1: live legs ----------------------------------------------
run_probe() { # name script
  say "live leg: $1"
  if bash "$2" > "$OUT/last-$1.stdout" 2>&1; then
    pass "probe $1 (see out/last-$1.stdout)"
  else
    fail "probe $1 (see out/last-$1.stdout)"
  fi
}
if [ -n "$LIVE" ]; then
  case "$LIVE" in all|minimax|codex|claude|net) ;; *) echo "unknown --live leg: $LIVE"; exit 2 ;; esac
  [ "$LIVE" = all ] || [ "$LIVE" = minimax ] && run_probe minimax "$SPIKE_DIR/probes/probe1-minimax.sh"
  [ "$LIVE" = all ] || [ "$LIVE" = codex ]   && run_probe codex   "$SPIKE_DIR/probes/probe2-codex.sh"
  [ "$LIVE" = all ] || [ "$LIVE" = claude ]  && run_probe claude  "$SPIKE_DIR/probes/probe3-claude.sh"
  [ "$LIVE" = all ] || [ "$LIVE" = net ]     && run_probe net     "$SPIKE_DIR/probes/probe4-failclosed-net.sh"
else
  say "live legs skipped (pass --live [all|minimax|codex|claude|net])"
fi

# ---------- teardown ---------------------------------------------------------
if [ $KEEP -eq 0 ]; then
  pkill -f "mitmdump.*mcspike-04" 2>/dev/null
  say "gateway stopped (use --keep-gateway to leave it running)"
fi

echo
[ $FAIL -eq 0 ] && echo "CHECK GREEN" || echo "CHECK RED"
exit $FAIL
