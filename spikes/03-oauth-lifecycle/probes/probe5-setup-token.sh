#!/usr/bin/env bash
# S3 probe 5 — evaluate `claude setup-token` as the S3 mitigation candidate.
# DOCS/HELP ONLY: does NOT mint a token (interactive), mounts no credentials,
# burns no turns. Safe to rerun anytime.
set -euo pipefail

IMG="mcspike-08-base:latest"
OUT="$(cd "$(dirname "$0")/.." && pwd)/out"
mkdir -p "$OUT"

docker run --rm "$IMG" bash -c '
  echo "== claude --version =="
  claude --version
  echo
  echo "== claude setup-token --help =="
  claude setup-token --help 2>&1 || true
  echo
  echo "== claude --help (token/auth-relevant lines) =="
  claude --help 2>&1 | grep -iE "token|auth|login|credential" || true
  echo
  echo "== env vars the CLI documents for auth (help + error-string scan) =="
  strings "$(readlink -f "$(command -v claude)")" 2>/dev/null \
    | grep -oE "CLAUDE_CODE_OAUTH_TOKEN|ANTHROPIC_API_KEY|CLAUDE_CONFIG_DIR|apiKeyHelper" \
    | sort | uniq -c | sort -rn | head -10 || true
' | tee "$OUT/probe5.log"
echo "PROBE5 done — evaluation prose lives in RESULT.md."
