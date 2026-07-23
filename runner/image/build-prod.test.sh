#!/bin/sh
set -eu

HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/mc-build-prod-test.XXXXXX")
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() {
  printf 'build-prod.test.sh: %s\n' "$*" >&2
  exit 1
}

mkdir -p "$TMP/bin"
case "$(MC_RELEASE_BUILD_ID=not-a-commit "$HERE/build-prod.sh" 2>&1 || :)" in
  *"MC_RELEASE_BUILD_ID must be a 40..64 lowercase-hex commit"*) ;;
  *) fail "invalid release identity was not refused" ;;
esac

cat >"$TMP/bin/mise" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >"${MC_TEST_MISE_LOG:?}"
exit 73
EOF
chmod 755 "$TMP/bin/mise"

release_id=0123456789abcdef0123456789abcdef01234567
if PATH="$TMP/bin:/usr/bin:/bin" MC_TEST_MISE_LOG="$TMP/mise.log" \
    MC_RELEASE_BUILD_ID="$release_id" "$HERE/build-prod.sh" \
    >"$TMP/output" 2>&1; then
  fail "stubbed compiler unexpectedly completed"
fi

grep -F -- "-ldflags -X main.releaseBuildID=$release_id" "$TMP/mise.log" >/dev/null ||
  fail "release identity was not passed to the production helper build"

printf 'production image release identity: ok\n'
