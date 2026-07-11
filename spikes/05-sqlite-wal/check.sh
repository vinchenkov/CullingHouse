#!/usr/bin/env bash
# S5 — SQLite WAL + crash discipline on the named volume. Machine-rerunnable.
# Rebuilds the probe, recreates the spike volume+container, runs every
# assertion end-to-end. Exits nonzero on the first failure.
# Leaves mcspike-05-* artifacts IN PLACE for the Docker-restart drill.
set -euo pipefail

SPIKE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$SPIKE/../.." && pwd)"
VOL=mcspike-05-spine
RUNNER=mcspike-05-runner
IMAGE=mcspike-05-base
DB=/spine/spine.db
PIDFILE=/spine/hang.pid

pass=0
step() { echo; echo "== $* =="; }
ok()   { echo "-- PASS: $*"; pass=$((pass+1)); }

step "build probe (static linux/arm64, CGO off, modernc.org/sqlite)"
(cd "$REPO" && mise exec -- env CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -C "$SPIKE/probe" -o "$SPIKE/bin/s5probe" .)

step "docker setup: image tag, fresh volume, runner container"
docker image inspect "$IMAGE" >/dev/null 2>&1 || {
  docker pull --platform linux/arm64 alpine:3.20
  docker tag alpine:3.20 "$IMAGE"
}
docker rm -f "$RUNNER" >/dev/null 2>&1 || true
docker volume rm "$VOL" >/dev/null 2>&1 || true
docker volume create "$VOL" >/dev/null
docker run -d --name "$RUNNER" --platform linux/arm64 \
  -v "$VOL":/spine \
  -v "$SPIKE/bin":/probe:ro \
  -v "$SPIKE/bindtest":/bindtest \
  "$IMAGE" sleep infinity >/dev/null
docker exec "$RUNNER" uname -m | grep -q aarch64
ok "runner up on linux/arm64, DB volume mounted at /spine"

step "A5 guard: named volume ACCEPTED"
docker exec "$RUNNER" /probe/s5probe guard --dir /spine
ok "guard accepts the named volume"

step "A5 guard: bind mount (VirtioFS) REJECTED, fail-closed"
if docker exec "$RUNNER" /probe/s5probe guard --dir /bindtest; then
  echo "FAIL: guard accepted a bind mount"; exit 1
fi
ok "guard refuses the bind-mounted path"

step "A5 guard: integrated fail-closed — seed onto bind mount refused before open"
if docker exec "$RUNNER" /probe/s5probe seed --db /bindtest/spine.db --rows 1; then
  echo "FAIL: probe opened a DB on a bind mount"; exit 1
fi
if docker exec "$RUNNER" test -e /bindtest/spine.db; then
  echo "FAIL: DB file was created on the bind mount"; exit 1
fi
ok "DB open on bind mount refused; no file created"

step "A1 seed: schema + 10 committed baseline rows, WAL asserted, on the volume"
docker exec "$RUNNER" /probe/s5probe seed --db "$DB" --rows 10
ok "seed committed under WAL on the named volume"

step "A1 write: BEGIN IMMEDIATE + commit 5 rows"
docker exec "$RUNNER" /probe/s5probe write --db "$DB" --rows 5 --tag committed
ok "BEGIN IMMEDIATE commit path works"

step "A4 pragmacheck: per-connection PRAGMAs on 4 simultaneous pool connections"
docker exec "$RUNNER" /probe/s5probe pragmacheck --db "$DB" --conns 4
ok "modernc.org/sqlite applies DSN _pragma on every pool connection"

step "A2/A3 hang writer: BEGIN IMMEDIATE + uncommitted rows spilled to WAL"
docker exec "$RUNNER" rm -f "$PIDFILE"
docker exec -d "$RUNNER" /probe/s5probe hang --db "$DB" --rows 20 --tag uncommitted --pidfile "$PIDFILE"
for i in $(seq 1 60); do
  if docker exec "$RUNNER" sh -c "test -s $PIDFILE"; then break; fi
  [ "$i" = 60 ] && { echo "FAIL: hang writer never became ready"; exit 1; }
  sleep 0.5
done
HANGPID=$(docker exec "$RUNNER" cat "$PIDFILE" | tr -d '[:space:]')
echo "hang writer pid (container ns): $HANGPID"
ok "writer holds an open txn with uncommitted frames on disk"

step "A3 concurrent reader: consistent snapshot, not blocked by open write txn"
docker exec "$RUNNER" /probe/s5probe read --db "$DB" --expect 15 --max-ms 2000
ok "reader saw 15 committed rows quickly; uncommitted rows invisible"

step "A1 contend: busy_timeout waits ~2s then SQLITE_BUSY against the held lock"
docker exec "$RUNNER" /probe/s5probe contend --db "$DB" --busy-ms 2000 --min-ms 1500
ok "busy_timeout demonstrably applied (waited before SQLITE_BUSY)"

step "A2 kill -9 the writer mid-transaction"
docker exec "$RUNNER" kill -9 "$HANGPID"
sleep 1
if docker exec "$RUNNER" sh -c "kill -0 $HANGPID 2>/dev/null"; then
  echo "FAIL: writer still alive after kill -9"; exit 1
fi
docker exec "$RUNNER" rm -f "$PIDFILE"
ok "writer killed with SIGKILL while txn open"

step "A2 verify: reopen -> integrity_check ok, 15 committed rows, 0 uncommitted"
docker exec "$RUNNER" /probe/s5probe verify --db "$DB" --expect 15 --absent-tag uncommitted
ok "crash recovery clean: committed survive, uncommitted vanish"

echo
echo "ALL $pass CHECKS PASSED"
echo "Artifacts left for the restart drill: volume=$VOL container=$RUNNER image=$IMAGE"
echo "DEFERRED to serialized drill: Docker Desktop restart mid-write (see RESULT.md)"
