#!/bin/sh
# Mission Control front door (spec §17). Level-0 bootstrap with no setup
# logic of its own: check prerequisites, build/install mc, hand off to the
# `mc onboard` wizard, which automates every deterministic step and stops
# only where a step genuinely needs the operator. Every question is
# dual-input: interactive at the terminal, or the same answers as flags from
# a shepherding agent — either way the deployment comes out identical.
#
# The operator's vocabulary is exactly this script and their coding harness;
# section names printed below are addressed to the shepherding agent.
#
# Modes:
#   ./install.sh                 production: build mc, hand off through the
#                                already-provisioned warm helper; a missing
#                                helper is a fail-closed incomplete install
#   ./install.sh --dev ...       development tier: direct-spine build under a
#                                scratch MC_HOME (never ~/.mission-control)
#
# Flags (all optional interactively, required for non-interactive runs):
#   --dev                        development-tier build and hand-off
#   --mc-home <path>             deployment home (required with --dev)
#   --bin-dir <path>             install destination for mc (default:
#                                $HOME/.local/bin; dev default: MC_HOME/bin)
#   --worksource <id>            first Worksource id
#   --workspace-root <path>      first Worksource workspace root
#   --console-hour <0-23> --console-minute <0-59> --console-tz <IANA>
#                                Daily Console schedule
#   --runtime-bindings <csv>     selected subscription bindings
#   --acquire-runtime-auth       run isolated Codex/Claude subscription login
#   --codex-auth-file <path>     isolated provider-owned Codex source
#   --claude-credentials-file <path>
#   --minimax-token-file <path>  owner-only MiniMax subscription-key source
#   --activate                   operator-present launchd activation gate
#   --yes                       never prompt; fail instead (agent mode)

set -eu

say() { printf '%s\n' "$*"; }
fail() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

DEV=0
ASSUME_YES=0
MC_HOME_IN="${MC_HOME:-}"
BIN_DIR=""
WORKSOURCE=""
WORKSPACE_ROOT=""
CONSOLE_HOUR=""
CONSOLE_MINUTE=""
CONSOLE_TZ=""
RUNTIME_BINDINGS=""
ACQUIRE_RUNTIME_AUTH=0
CODEX_AUTH_FILE=""
CLAUDE_CREDENTIALS_FILE=""
MINIMAX_TOKEN_FILE=""
ACTIVATE=0

while [ $# -gt 0 ]; do
  case "$1" in
    --dev) DEV=1 ;;
    --yes) ASSUME_YES=1 ;;
    --mc-home) MC_HOME_IN=${2:?--mc-home needs a path}; shift ;;
    --bin-dir) BIN_DIR=${2:?--bin-dir needs a path}; shift ;;
    --worksource) WORKSOURCE=${2:?--worksource needs an id}; shift ;;
    --workspace-root) WORKSPACE_ROOT=${2:?--workspace-root needs a path}; shift ;;
    --console-hour) CONSOLE_HOUR=${2:?}; shift ;;
    --console-minute) CONSOLE_MINUTE=${2:?}; shift ;;
    --console-tz) CONSOLE_TZ=${2:?}; shift ;;
    --runtime-bindings) RUNTIME_BINDINGS=${2:?}; shift ;;
    --acquire-runtime-auth) ACQUIRE_RUNTIME_AUTH=1 ;;
    --codex-auth-file) CODEX_AUTH_FILE=${2:?}; shift ;;
    --claude-credentials-file) CLAUDE_CREDENTIALS_FILE=${2:?}; shift ;;
    --minimax-token-file) MINIMAX_TOKEN_FILE=${2:?}; shift ;;
    --activate) ACTIVATE=1 ;;
    -h|--help) sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) fail "unknown flag $1 (see --help)" ;;
  esac
  shift
done

interactive() { [ "$ASSUME_YES" -eq 0 ] && [ -t 0 ]; }

ask() {
  # ask <var-name> <prompt> [default]
  _var=$1; _prompt=$2; _default=${3:-}
  if interactive; then
    if [ -n "$_default" ]; then
      printf '%s [%s]: ' "$_prompt" "$_default"
    else
      printf '%s: ' "$_prompt"
    fi
    read -r _answer
    [ -n "$_answer" ] || _answer=$_default
  else
    _answer=$_default
  fi
  [ -n "$_answer" ] || fail "$_prompt is required (pass the matching flag for non-interactive runs)"
  eval "$_var=\$_answer"
}

# --- 1. Prerequisites -------------------------------------------------------

say "== prerequisites"
command -v git >/dev/null 2>&1 || fail "git is required and was not found on PATH"
OS=$(uname -s)
case "$OS" in
  Darwin|Linux) ;;
  *) fail "unsupported operating system $OS (expected macOS/launchd or Linux/systemd)" ;;
esac
command -v docker >/dev/null 2>&1 || \
  fail "docker CLI is required — install Docker Desktop, then re-run the front door"
if ! docker info >/dev/null 2>&1; then
  if [ "$OS" = Darwin ] && command -v open >/dev/null 2>&1; then
    say "   docker daemon is not responding — starting Docker Desktop"
    open -a Docker >/dev/null 2>&1 || true
    docker_ready=0
    attempt=0
    while [ "$attempt" -lt 60 ]; do
      if docker info >/dev/null 2>&1; then
        docker_ready=1
        break
      fi
      sleep 1
      attempt=$((attempt + 1))
    done
    [ "$docker_ready" -eq 1 ] || \
      fail "docker daemon is not responding after 60s — check Docker Desktop health, then re-run the front door"
  else
    fail "docker daemon is not responding — start the container runtime, then re-run the front door"
  fi
fi
command -v mise >/dev/null 2>&1 || fail "mise is required (https://mise.jdx.dev) — it pins the toolchain this repo builds with"
( cd "$REPO_DIR" && mise install >/dev/null 2>&1 ) || true
( cd "$REPO_DIR" && mise exec -- go version >/dev/null 2>&1 ) || fail "the pinned Go toolchain is unavailable (try: mise install)"
say "   git, toolchain, container runtime: ok"

RELEASE_BUILD_ID=$(git -C "$REPO_DIR" rev-parse --verify 'HEAD^{commit}') || \
  fail "cannot resolve the immutable release commit"
case "$RELEASE_BUILD_ID" in
  *[!0-9a-f]*|'') fail "release commit is not lowercase hexadecimal" ;;
esac
[ "${#RELEASE_BUILD_ID}" -ge 40 ] && [ "${#RELEASE_BUILD_ID}" -le 64 ] || \
  fail "release commit has an unsupported length"

# --- 2. Build / install mc --------------------------------------------------

if [ "$DEV" -eq 1 ]; then
  ask MC_HOME_IN "deployment home (MC_HOME, a scratch path outside any git tree)" "$MC_HOME_IN"
  [ -n "$BIN_DIR" ] || BIN_DIR="$MC_HOME_IN/bin"
  mkdir -p "$MC_HOME_IN" "$BIN_DIR"
  chmod 700 "$MC_HOME_IN"
  say "== build (development tier, direct spine)"
  ( cd "$REPO_DIR/mc" && mise exec -- go build -tags test_fake_routing -o "$BIN_DIR/mc" ./cmd/mc )
else
  [ -n "$BIN_DIR" ] || BIN_DIR="${MC_BIN_DIR:-$HOME/.local/bin}"
  mkdir -p "$BIN_DIR"
  say "== build (production)"
  ( cd "$REPO_DIR/mc" && mise exec -- go build \
      -ldflags "-X main.releaseBuildID=$RELEASE_BUILD_ID" \
      -o "$BIN_DIR/mc" ./cmd/mc )
  say "== build (production image, native arm64)"
  ( cd "$REPO_DIR" && MC_RELEASE_BUILD_ID="$RELEASE_BUILD_ID" ./runner/image/build-prod.sh ) || \
    fail "production image build failed — fix the reported build/runtime error, then re-run the front door"
fi
say "   installed $BIN_DIR/mc"

# --- 3. Hand off to the wizard ---------------------------------------------

if [ "$DEV" -eq 0 ]; then
  [ -z "$MC_HOME_IN" ] || export MC_HOME="$MC_HOME_IN"
  say "== hand-off: mc onboard preflight"
  "$BIN_DIR/mc" onboard preflight
  say "== hand-off: mc onboard home (through the managed helper)"
  "$BIN_DIR/mc" onboard home --release-source "$REPO_DIR/runner" \
    --host-release-source "$REPO_DIR"
  say "== hand-off: remaining mc onboard sections"
  set -- "$BIN_DIR/mc" onboard
  [ -z "$WORKSOURCE" ] || set -- "$@" --worksource "$WORKSOURCE"
  [ -z "$WORKSPACE_ROOT" ] || set -- "$@" --workspace-root "$WORKSPACE_ROOT"
  [ -z "$CONSOLE_HOUR" ] || set -- "$@" --console-hour "$CONSOLE_HOUR"
  [ -z "$CONSOLE_MINUTE" ] || set -- "$@" --console-minute "$CONSOLE_MINUTE"
  [ -z "$CONSOLE_TZ" ] || set -- "$@" --console-tz "$CONSOLE_TZ"
  [ -z "$RUNTIME_BINDINGS" ] || set -- "$@" --runtime-bindings "$RUNTIME_BINDINGS"
  [ "$ACQUIRE_RUNTIME_AUTH" -eq 0 ] || set -- "$@" --acquire
  [ -z "$CODEX_AUTH_FILE" ] || set -- "$@" --codex-auth-file "$CODEX_AUTH_FILE"
  [ -z "$CLAUDE_CREDENTIALS_FILE" ] || set -- "$@" --claude-credentials-file "$CLAUDE_CREDENTIALS_FILE"
  [ -z "$MINIMAX_TOKEN_FILE" ] || set -- "$@" --minimax-token-file "$MINIMAX_TOKEN_FILE"
  [ "$ACTIVATE" -eq 0 ] || set -- "$@" --activate
  exec "$@"
fi

ask WORKSOURCE "first Worksource id" "$WORKSOURCE"
ask WORKSPACE_ROOT "first Worksource workspace root (absolute path)" "$WORKSPACE_ROOT"
ask CONSOLE_HOUR "Daily Console hour (0-23)" "${CONSOLE_HOUR:-9}"
ask CONSOLE_MINUTE "Daily Console minute (0-59)" "${CONSOLE_MINUTE:-0}"
ask CONSOLE_TZ "Daily Console timezone (IANA)" "${CONSOLE_TZ:-$(readlink /etc/localtime 2>/dev/null | sed 's|.*zoneinfo/||' || echo UTC)}"

say "== hand-off: mc onboard (development tier)"
MC_HOME="$MC_HOME_IN" MC_SPINE="$MC_HOME_IN/spine.db" exec "$BIN_DIR/mc" onboard \
  --release-source "$REPO_DIR/runner" \
  --host-release-source "$REPO_DIR" \
  --worksource "$WORKSOURCE" --workspace-root "$WORKSPACE_ROOT" \
  --console-hour "$CONSOLE_HOUR" --console-minute "$CONSOLE_MINUTE" --console-tz "$CONSOLE_TZ"
