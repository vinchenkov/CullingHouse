#!/usr/bin/env bash
# S8 — arm64 image build + Playwright smoke: machine-rerunnable probe.
# Exits nonzero on the first failed assertion. Rerun is cheap: the docker
# build layer cache makes A2 a no-op after the first run, and the image ID
# printed by A5 is the pin — agents must compare it and NOT rebuild in a loop.
set -euo pipefail
cd "$(dirname "$0")"

IMAGE=mcspike-08-base
SMOKE=mcspike-08-smoke
fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

# --- A1: native arm64 host + daemon (precondition for "no QEMU") -------------
[ "$(uname -m)" = "arm64" ] || fail "A1 host arch is $(uname -m), not arm64"
daemon_arch=$(docker info --format '{{.Architecture}}')
[ "$daemon_arch" = "aarch64" ] || fail "A1 docker daemon arch is $daemon_arch, not aarch64"
pass "A1 host=arm64 daemon=aarch64 (native, same arch => no emulation path)"

# --- A2: build natively (no --platform flag => native arch only) -------------
start=$(date +%s)
docker build -t "$IMAGE" . 2>&1 | tail -5
build_s=$(( $(date +%s) - start ))
pass "A2 build completed in ${build_s}s (cached reruns ~0s; cold ~see RESULT.md)"

# --- A3: image reports Architecture arm64, Os linux --------------------------
arch=$(docker image inspect "$IMAGE" --format '{{.Os}}/{{.Architecture}}')
[ "$arch" = "linux/arm64" ] || fail "A3 image platform is $arch, not linux/arm64"
pass "A3 image platform $arch"

# --- A4: size + digest (the pin) ---------------------------------------------
size=$(docker image inspect "$IMAGE" --format '{{.Size}}')
image_id=$(docker image inspect "$IMAGE" --format '{{.Id}}')
pass "A4 size_bytes=$size ($(( size / 1024 / 1024 )) MiB) image_id=$image_id"

# --- A5: in-container: native aarch64, pinned tool versions ------------------
docker rm -f "$SMOKE" >/dev/null 2>&1 || true
docker run --name "$SMOKE" --user agent \
  -v "$PWD/probe-browser.js:/probe/probe-browser.js:ro" \
  "$IMAGE" bash -c '
    set -euo pipefail
    [ "$(uname -m)" = "aarch64" ] || { echo "container arch $(uname -m)"; exit 1; }
    git --version | grep -q " 2\.\(4[89]\|[5-9][0-9]\)" || { echo "git too old: $(git --version)"; exit 1; }
    node --version | grep -qx "v22.17.1"
    bun --version  | grep -qx "1.3.9"
    claude --version | grep -q "2.1.207"
    codex --version  | grep -q "0.144.1"
    playwright --version | grep -q "1.61.1"
    echo "versions: $(git --version) node=$(node --version) bun=$(bun --version)"
    echo "harness: claude=$(claude --version) codex=$(codex --version)"
    # --- A6: one Chromium launch + screenshot (Playwright) ---
    node /probe/probe-browser.js
  ' || fail "A5/A6 in-container smoke failed"
pass "A5 in-container aarch64 + all pins verified"

# --- A6: pull the screenshot out and verify it is a real PNG on the host -----
docker cp "$SMOKE:/tmp/smoke.png" ./smoke.png >/dev/null
sig=$(head -c 8 ./smoke.png | xxd -p | tr -d '\n')
[ "$sig" = "89504e470d0a1a0a" ] || fail "A6 host-side screenshot signature $sig is not PNG"
pass "A6 Chromium screenshot captured ($(wc -c < ./smoke.png | tr -d ' ') bytes) -> ./smoke.png"

echo
echo "ALL PASS  image=$IMAGE  id=$image_id  size_bytes=$size  build_s=$build_s"
echo "Artifacts left in place for the restart drill: image '$IMAGE', container '$SMOKE' (stopped)."
