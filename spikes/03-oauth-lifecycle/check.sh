#!/usr/bin/env bash
# S3 — machine-rerunnable check.
#
# Default run is FREE (no live turns, no credential mounts):
#   probe 1 (dir-vs-file mount inode demo, dummy file) + probe 5 (setup-token
#   help evaluation) + static guards.
#
# Live legs are opt-in flags (each burns 1-2 tiny subscription turns and
# touches the CANONICAL credential copies under ~/.mc-dev-home/cred/ —
# probe 4 touches only a scratch copy but consumes the shared refresh token
# server-side if rotation is single-use):
#   --live-codex    probe 2: codex refresh round-trip (canonical dir)
#   --live-claude   probe 3: claude refresh round-trip (canonical dir)
#   --race          probe 4: concurrent-refresh race (scratch copy)
#   --all-live      all of the above
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
IMG="mcspike-08-base:latest"
FAIL=0
LIVE_CODEX=0; LIVE_CLAUDE=0; RACE=0
for a in "$@"; do case "$a" in
  --live-codex) LIVE_CODEX=1;;
  --live-claude) LIVE_CLAUDE=1;;
  --race) RACE=1;;
  --all-live) LIVE_CODEX=1; LIVE_CLAUDE=1; RACE=1;;
  *) echo "unknown flag: $a"; exit 2;;
esac; done

say() { printf '\n=== %s ===\n' "$*"; }
check() { # name cmd...
  local name="$1"; shift
  if "$@"; then echo "PASS: $name"; else echo "FAIL: $name"; FAIL=1; fi
}

say "S0: base image present with both harnesses"
check "image has codex + claude" \
  docker run --rm "$IMG" bash -c 'codex --version >/dev/null && claude --version >/dev/null'

say "S1: no secret material inside the spike dir (repo hygiene)"
if grep -rInE 'sk-ant-[a-zA-Z0-9_-]{20}|rt\.1\.[A-Za-z0-9_-]{20}|eyJ[A-Za-z0-9_-]{40}' "$HERE" >/dev/null 2>&1; then
  echo "FAIL: token-shaped string found under spike dir"; FAIL=1
else
  echo "PASS: no token-shaped strings under spike dir"
fi

say "Probe 1 (free): directory-vs-file mount under atomic replace"
if "$HERE/probes/probe1-inode.sh" > /tmp/mcspike03-p1.log 2>&1; then
  echo "PASS: probe1 (log: /tmp/mcspike03-p1.log)"
else
  echo "FAIL: probe1"; tail -20 /tmp/mcspike03-p1.log; FAIL=1
fi

say "Probe 5 (free): setup-token evaluation surface still present"
check "claude setup-token help advertises long-lived token" \
  bash -c "docker run --rm $IMG claude setup-token --help 2>&1 | grep -qi 'long-lived authentication token'"
check "claude auth status subcommand exists (mc doctor auth-health candidate)" \
  bash -c "docker run --rm $IMG claude auth status --help 2>&1 | grep -qi 'authentication status'"

if [[ $LIVE_CODEX == 1 ]]; then
  say "Probe 2 (LIVE): codex refresh round-trip through canonical dir"
  if "$HERE/probes/probe2-codex.sh" > /tmp/mcspike03-p2.log 2>&1; then
    # refresh happened iff the post-run1 snap differs from the aged+expired snap
    if grep -q '^\[post-run1\]' /tmp/mcspike03-p2.log; then
      A=$(grep '^\[aged+expired\]' /tmp/mcspike03-p2.log | grep -o 'sha256=[0-9a-f]*')
      B=$(grep '^\[post-run1\]'    /tmp/mcspike03-p2.log | grep -o 'sha256=[0-9a-f]*')
      [[ -n "$A" && -n "$B" && "$A" != "$B" ]] && echo "PASS: probe2 (rotation landed host-side)" \
        || { echo "FAIL: probe2 ran but no rotation observed"; FAIL=1; }
    else echo "FAIL: probe2 incomplete"; FAIL=1; fi
  else echo "FAIL: probe2"; tail -20 /tmp/mcspike03-p2.log; FAIL=1; fi
fi

if [[ $LIVE_CLAUDE == 1 ]]; then
  say "Probe 3 (LIVE): claude refresh round-trip through canonical dir"
  if "$HERE/probes/probe3-claude.sh" > /tmp/mcspike03-p3.log 2>&1; then
    A=$(grep '^\[expired\]'   /tmp/mcspike03-p3.log | grep -o 'sha256=[0-9a-f]*')
    B=$(grep '^\[post-run1\]' /tmp/mcspike03-p3.log | grep -o 'sha256=[0-9a-f]*')
    [[ -n "$A" && -n "$B" && "$A" != "$B" ]] && echo "PASS: probe3 (rotation landed host-side)" \
      || { echo "FAIL: probe3 ran but no rotation observed"; FAIL=1; }
  else echo "FAIL: probe3"; tail -20 /tmp/mcspike03-p3.log; FAIL=1; fi
fi

if [[ $RACE == 1 ]]; then
  say "Probe 4 (LIVE): concurrent-refresh race on a scratch copy"
  if "$HERE/probes/probe4-race.sh" > /tmp/mcspike03-p4.log 2>&1; then
    grep -q 'VALID JSON, not torn' /tmp/mcspike03-p4.log \
      && echo "PASS: probe4 (no corruption)" \
      || { echo "FAIL: probe4 corruption or incomplete"; FAIL=1; }
  else echo "FAIL: probe4"; tail -20 /tmp/mcspike03-p4.log; FAIL=1; fi
fi

say "result"
[[ $FAIL == 0 ]] && echo "S3 CHECK: GREEN" || echo "S3 CHECK: RED"
exit $FAIL
