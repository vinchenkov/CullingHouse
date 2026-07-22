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
#                                warm helper (deferred until the container
#                                section provisions it — Phase 5)
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
    -h|--help) sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
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
command -v mise >/dev/null 2>&1 || fail "mise is required (https://mise.jdx.dev) — it pins the toolchain this repo builds with"
( cd "$REPO_DIR" && mise install >/dev/null 2>&1 ) || true
( cd "$REPO_DIR" && mise exec -- go version >/dev/null 2>&1 ) || fail "the pinned Go toolchain is unavailable (try: mise install)"
if ! command -v docker >/dev/null 2>&1; then
  say "   docker CLI not found — install Docker Desktop before the container section runs"
elif ! docker info >/dev/null 2>&1; then
  say "   docker daemon is not responding — start Docker Desktop before the container section runs"
else
  say "   git, toolchain, container runtime: ok"
fi

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
  ( cd "$REPO_DIR/mc" && mise exec -- go build -o "$BIN_DIR/mc" ./cmd/mc )
fi
say "   installed $BIN_DIR/mc"

# --- 3. Hand off to the wizard ---------------------------------------------

if [ "$DEV" -eq 0 ]; then
  if ! docker inspect mc-helper >/dev/null 2>&1; then
    say "== hand-off: DEFERRED"
    say "   the production wizard runs through the warm helper container,"
    say "   which the container onboarding section provisions (Phase 5)."
    say "   Shepherding agent: provision it, then re-run this script, or run"
    say "   the wizard sections directly once the helper answers."
    exit 0
  fi
  say "== hand-off: mc onboard (through the warm helper)"
  exec "$BIN_DIR/mc" onboard
fi

ask WORKSOURCE "first Worksource id" "$WORKSOURCE"
ask WORKSPACE_ROOT "first Worksource workspace root (absolute path)" "$WORKSPACE_ROOT"
ask CONSOLE_HOUR "Daily Console hour (0-23)" "${CONSOLE_HOUR:-9}"
ask CONSOLE_MINUTE "Daily Console minute (0-59)" "${CONSOLE_MINUTE:-0}"
ask CONSOLE_TZ "Daily Console timezone (IANA)" "${CONSOLE_TZ:-$(readlink /etc/localtime 2>/dev/null | sed 's|.*zoneinfo/||' || echo UTC)}"

say "== hand-off: mc onboard (development tier)"
MC_HOME="$MC_HOME_IN" MC_SPINE="$MC_HOME_IN/spine.db" exec "$BIN_DIR/mc" onboard \
  --worksource "$WORKSOURCE" --workspace-root "$WORKSPACE_ROOT" \
  --console-hour "$CONSOLE_HOUR" --console-minute "$CONSOLE_MINUTE" --console-tz "$CONSOLE_TZ"
