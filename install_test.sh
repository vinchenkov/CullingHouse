#!/bin/sh
# Fast, Docker-free contract tests for install.sh's Level-0 failure boundary.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/mc-install-test.XXXXXX")
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() {
  printf 'install_test.sh: %s\n' "$*" >&2
  exit 1
}

make_mise() {
  bin=$1
  mkdir -p "$bin"
  cat >"$bin/mise" <<'EOF'
#!/bin/sh
set -eu
if [ "${1:-}" = install ]; then
  exit 0
fi
if [ "${1:-}" = exec ]; then
  shift
  [ "${1:-}" != -- ] || shift
fi
if [ "${1:-}" = go ] && [ "${2:-}" = version ]; then
  exit 0
fi
if [ "${1:-}" = go ] && [ "${2:-}" = build ]; then
  out=
  while [ "$#" -gt 0 ]; do
    if [ "$1" = -o ]; then
      out=${2:?missing build output}
      break
    fi
    shift
  done
  [ -n "$out" ] || exit 90
  cat >"$out" <<'SCRIPT'
#!/bin/sh
if [ -n "${MC_INSTALL_TEST_LOG:-}" ]; then
  {
    printf 'CALL'
    for arg do
      printf '\t%s' "$arg"
    done
    printf '\n'
  } >>"$MC_INSTALL_TEST_LOG"
  exit 0
fi
exit 91
SCRIPT
  chmod 755 "$out"
  exit 0
fi
exit 92
EOF
  chmod 755 "$bin/mise"
}

run_must_fail() {
  name=$1
  expected=$2
  shift 2
  out="$TMP/$name.out"
  if "$@" >"$out" 2>&1; then
    fail "$name unexpectedly exited 0"
  fi
  grep -F "$expected" "$out" >/dev/null || {
    sed -n '1,120p' "$out" >&2
    fail "$name did not report: $expected"
  }
}

# PATH deliberately omits Homebrew, where Docker normally lives on macOS.
case_root="$TMP/missing-cli"
make_mise "$case_root/bin"
run_must_fail missing-cli "docker CLI is required" \
  env PATH="$case_root/bin:/usr/bin:/bin" HOME="$case_root/home" \
  sh "$ROOT/install.sh" --yes --bin-dir "$case_root/install-bin"

case_root="$TMP/stopped-daemon"
make_mise "$case_root/bin"
cat >"$case_root/bin/docker" <<'EOF'
#!/bin/sh
[ "${1:-}" != info ] || exit 1
exit 93
EOF
cat >"$case_root/bin/uname" <<'EOF'
#!/bin/sh
printf 'Linux\n'
EOF
chmod 755 "$case_root/bin/docker" "$case_root/bin/uname"
run_must_fail stopped-daemon "docker daemon is not responding" \
  env PATH="$case_root/bin:/usr/bin:/bin" HOME="$case_root/home" \
  sh "$ROOT/install.sh" --yes --bin-dir "$case_root/install-bin"
[ ! -e "$case_root/install-bin/mc" ] || fail "stopped-daemon wrote mc before preflight passed"

case_root="$TMP/image-build-failed"
make_mise "$case_root/bin"
cat >"$case_root/bin/docker" <<'EOF'
#!/bin/sh
case "${1:-}" in
  info) exit 0 ;;
  build) exit 1 ;;
esac
exit 94
EOF
chmod 755 "$case_root/bin/docker"
run_must_fail image-build-failed "production image build failed" \
  env PATH="$case_root/bin:/usr/bin:/bin" HOME="$case_root/home" \
  sh "$ROOT/install.sh" --yes --bin-dir "$case_root/install-bin"

case_root="$TMP/production-forwarding"
make_mise "$case_root/bin"
cat >"$case_root/bin/docker" <<'EOF'
#!/bin/sh
case "${1:-}" in
  info|build) exit 0 ;;
esac
exit 94
EOF
cat >"$case_root/bin/uname" <<'EOF'
#!/bin/sh
printf 'Linux\n'
EOF
chmod 755 "$case_root/bin/docker" "$case_root/bin/uname"
mkdir -p "$case_root/home"
forward_log="$case_root/forward.log"
env PATH="$case_root/bin:/usr/bin:/bin" HOME="$case_root/home" \
  MC_INSTALL_TEST_LOG="$forward_log" \
  sh "$ROOT/install.sh" --yes --bin-dir "$case_root/install-bin" \
    --worksource primary --workspace-root "$case_root/workspace root" \
    --console-hour 9 --console-minute 5 --console-tz America/Los_Angeles \
    --runtime-bindings chatgpt,claude,minimax --acquire-runtime-auth \
    --codex-auth-file "$case_root/codex auth.json" \
    --claude-credentials-file "$case_root/claude credentials.json" \
    --minimax-token-file "$case_root/minimax token" --restore-latest --activate \
    >/dev/null 2>&1 || fail "production-forwarding did not reach the whole wizard"
expected=$(printf 'CALL\tonboard\t--worksource\tprimary\t--workspace-root\t%s\t--console-hour\t9\t--console-minute\t5\t--console-tz\tAmerica/Los_Angeles\t--runtime-bindings\tchatgpt,claude,minimax\t--acquire\t--codex-auth-file\t%s\t--claude-credentials-file\t%s\t--minimax-token-file\t%s\t--restore-latest\t--activate' \
  "$case_root/workspace root" "$case_root/codex auth.json" \
  "$case_root/claude credentials.json" "$case_root/minimax token")
actual=$(tail -n 1 "$forward_log")
[ "$actual" = "$expected" ] || {
  printf 'expected: %s\nactual:   %s\n' "$expected" "$actual" >&2
  fail "production-forwarding changed whole-wizard arguments"
}

printf 'install.sh fail-closed preflight: ok\n'
