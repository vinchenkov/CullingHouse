#!/bin/sh
# S1 — setuid gate on Docker Desktop named volumes: machine-rerunnable check.
# Re-runs the whole probe end-to-end; exits nonzero on any failure.
# Leaves artifacts in place for the later Docker-Desktop-restart drill:
#   volume    mcspike-01-spine
#   image     mcspike-01-gate:latest
#   container mcspike-01-agent
#
# Requires: docker (Docker Desktop running, arm64), go via mise.
set -u

SPIKE_DIR="$(cd "$(dirname "$0")" && pwd)"
IMAGE=mcspike-01-gate:latest
VOLUME=mcspike-01-spine
CONTAINER=mcspike-01-agent
DB=/spine/spine.db
MCPRIV_UID=10001
AGENT_UID=10002

FAILURES=0
note()  { printf '\n== %s\n' "$*"; }
pass()  { printf 'PASS: %s\n' "$*"; }
fail()  { printf 'FAIL: %s\n' "$*"; FAILURES=$((FAILURES + 1)); }
assert() { # assert <description> <command...>
  desc="$1"; shift
  if "$@"; then pass "$desc"; else fail "$desc"; fi
}

note "0. build static probe binary (CGO_ENABLED=0, linux/arm64 — setuid demands static)"
( cd "$SPIKE_DIR/probe" \
  && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
     mise exec -- go build -trimpath -ldflags='-s -w' -o mcprobe . ) \
  || { fail "go build"; echo "cannot continue without the binary"; exit 1; }
if file "$SPIKE_DIR/probe/mcprobe" | grep -q 'statically linked'; then
  pass "binary is statically linked"
else
  fail "binary is NOT statically linked"
fi

note "1. clean previous run (container only; volume + image are recreated fresh)"
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker volume rm "$VOLUME" >/dev/null 2>&1 || true

note "2. build image (nonce A) and create the named volume"
docker build -q --build-arg BUILD_NONCE="$(date +%s)-A" -t "$IMAGE" "$SPIKE_DIR" >/dev/null \
  || { fail "docker build"; exit 1; }
docker volume create "$VOLUME" >/dev/null || { fail "volume create"; exit 1; }

note "3. init the spine on the named volume, owned by the privileged uid (10001), dir 0700"
docker run --rm -v "$VOLUME":/spine "$IMAGE" sh -c "
  chown $MCPRIV_UID:$MCPRIV_UID /spine && chmod 0700 /spine &&
  su mcpriv -c '/usr/local/bin/mc init $DB' &&
  chmod 0600 /spine/spine.db* 2>/dev/null; ls -ln /spine
" || { fail "spine init"; exit 1; }

note "4. start the agent container (agent uid, NO no-new-privileges, default runtime)"
docker run -d --name "$CONTAINER" -u "$AGENT_UID" \
  -v "$VOLUME":/spine "$IMAGE" >/dev/null || { fail "agent container start"; exit 1; }

run_gate_assertions() { # $1 = phase label
  phase="$1"
  assert "[$phase] (a) direct open(2) O_RDONLY as agent uid -> EACCES" \
    docker exec -u "$AGENT_UID" "$CONTAINER" agentprobe directopen "$DB"
  assert "[$phase] (a') direct open(2) O_RDWR as agent uid -> EACCES" \
    docker exec -u "$AGENT_UID" "$CONTAINER" agentprobe directwrite "$DB"
  assert "[$phase] (b) same read through setuid mc succeeds (euid=$MCPRIV_UID)" \
    docker exec -u "$AGENT_UID" "$CONTAINER" mc read "$DB"
  assert "[$phase] (b') write through setuid mc succeeds" \
    docker exec -u "$AGENT_UID" "$CONTAINER" mc write "$DB"
  # euid must actually escalate: ruid=agent, euid=mcpriv
  if docker exec -u "$AGENT_UID" "$CONTAINER" mc ids \
       | grep -q "ruid=$AGENT_UID euid=$MCPRIV_UID"; then
    pass "[$phase] setuid escalation observed (ruid=$AGENT_UID euid=$MCPRIV_UID)"
  else
    fail "[$phase] setuid escalation NOT observed"
  fi
}

note "5. gate assertions — fresh container"
run_gate_assertions "fresh"

note "6. permanent canaries"
if [ "$(docker info --format '{{.DefaultRuntime}}')" = "runc" ]; then
  pass "canary: docker default runtime is runc"
else
  fail "canary: default runtime is $(docker info --format '{{.DefaultRuntime}}'), not runc"
fi
if docker info --format '{{.SecurityOptions}}' | grep -q 'userns'; then
  fail "canary: user-namespace remap is ON"
else
  pass "canary: no user-namespace remap in daemon SecurityOptions"
fi
if docker exec "$CONTAINER" cat /proc/self/uid_map | grep -Eq '^\s*0\s+0\s+4294967295$'; then
  pass "canary: identity uid_map in container (no userns remap / no ECI)"
else
  fail "canary: uid_map is remapped: $(docker exec "$CONTAINER" cat /proc/self/uid_map)"
fi
if [ "$(docker inspect -f '{{.HostConfig.SecurityOpt}}' "$CONTAINER")" = "[]" ] \
   || ! docker inspect -f '{{.HostConfig.SecurityOpt}}' "$CONTAINER" | grep -q 'no-new-privileges'; then
  pass "canary: no-new-privileges NOT set on the agent container (inspect)"
else
  fail "canary: no-new-privileges IS set on the agent container"
fi
if docker exec "$CONTAINER" grep -q '^NoNewPrivs:.*0$' /proc/self/status; then
  pass "canary: NoNewPrivs=0 inside the container (/proc/self/status)"
else
  fail "canary: NoNewPrivs != 0 inside the container"
fi
MOUNT_LINE="$(docker exec "$CONTAINER" sh -c "grep ' /spine ' /proc/mounts")"
if [ -n "$MOUNT_LINE" ] && ! echo "$MOUNT_LINE" | awk '{print $4}' | grep -q 'nosuid'; then
  pass "canary: spine volume mount is not nosuid ($MOUNT_LINE)"
else
  fail "canary: spine volume mount missing or nosuid: $MOUNT_LINE"
fi
if [ "$(docker inspect -f '{{.Architecture}}' "$IMAGE")" = "arm64" ]; then
  pass "canary: image architecture is arm64"
else
  fail "canary: image architecture is $(docker inspect -f '{{.Architecture}}' "$IMAGE")"
fi

note "7. survival: container restart"
docker restart "$CONTAINER" >/dev/null || fail "docker restart"
run_gate_assertions "container-restart"

note "8. survival: image rebuild (new nonce -> genuinely new layers), fresh container, same volume"
docker rm -f "$CONTAINER" >/dev/null
docker build -q --build-arg BUILD_NONCE="$(date +%s)-B" -t "$IMAGE" "$SPIKE_DIR" >/dev/null \
  || fail "image rebuild"
docker run -d --name "$CONTAINER" -u "$AGENT_UID" \
  -v "$VOLUME":/spine "$IMAGE" >/dev/null || fail "agent container restart after rebuild"
run_gate_assertions "image-rebuild"

note "9. deferred to the serialized drill: Docker Desktop restart; volume detach/reattach"
echo "DEFERRED (drill): after a Docker Desktop restart and a volume detach/reattach,"
echo "  rerun: docker start $CONTAINER && re-exec steps 5-6, or simply rerun this script"
echo "  WITHOUT step 1's cleanup (the artifacts below are left in place for that)."

note "artifacts left in place"
echo "  volume:    $VOLUME"
echo "  image:     $IMAGE"
echo "  container: $CONTAINER (running)"

if [ "$FAILURES" -eq 0 ]; then
  printf '\nS1 CHECK: ALL PASS\n'
  exit 0
else
  printf '\nS1 CHECK: %d FAILURE(S)\n' "$FAILURES"
  exit 1
fi
