#!/usr/bin/env bash
# S3 probe 1 — directory-vs-file bind mount staleness under atomic replace.
# Uses a DUMMY file only; no credentials involved. Safe to rerun anytime.
#
# Demonstrates: a refresh that atomically replaces a credential file
# (write-tmp + rename, the pattern both codex and claude use) swaps the
# inode. Classic bind-mount theory says a file-level mount pins the old
# inode; empirically on Docker Desktop VirtioFS the HOST-side replace
# propagates anyway (path-based share), but the IN-CONTAINER replace —
# the direction a real refresh uses — fails outright: rename(2) onto a
# mount point returns EBUSY. Either way, only a directory mount works.
set -euo pipefail

SCRATCH="${HOME}/.mc-dev-home/spike03/inode-demo"
IMG="mcspike-08-base:latest"
CNAME="mcspike-03-inode"

mkdir -p "$SCRATCH"
echo '{"token":"v1-original"}' > "$SCRATCH/dummy.json"

docker rm -f "$CNAME" >/dev/null 2>&1 || true
docker run -d --name "$CNAME" \
  -v "$SCRATCH":/mnt/dirmount \
  -v "$SCRATCH/dummy.json":/mnt/filemount.json \
  "$IMG" sleep 600 >/dev/null

read_both() {
  echo "  dir-mount : $(docker exec "$CNAME" cat /mnt/dirmount/dummy.json)"
  echo "  file-mount: $(docker exec "$CNAME" cat /mnt/filemount.json)"
}

echo "== before replace =="
read_both

# Atomic replace on the host — same inode-swap shape as a real token refresh.
echo '{"token":"v2-rotated"}' > "$SCRATCH/dummy.json.tmp"
mv "$SCRATCH/dummy.json.tmp" "$SCRATCH/dummy.json"
sleep 2   # let VirtioFS settle

echo "== after atomic replace on host =="
read_both

DIRV=$(docker exec "$CNAME" cat /mnt/dirmount/dummy.json)
FILEV=$(docker exec "$CNAME" cat /mnt/filemount.json)

# Also test the reverse direction: container writes (atomic replace inside
# container through the DIRECTORY mount) must land host-side.
docker exec "$CNAME" bash -c 'echo "{\"token\":\"v3-container-rotated\"}" > /mnt/dirmount/dummy.json.tmp && mv /mnt/dirmount/dummy.json.tmp /mnt/dirmount/dummy.json'
sleep 2
HOSTV=$(cat "$SCRATCH/dummy.json")
echo "== after container-side atomic replace (dir mount) =="
echo "  host sees : $HOSTV"

# The decisive leg: in-container atomic replace THROUGH THE FILE MOUNT —
# the exact write a harness token refresh performs. rename(2) onto a
# mount point must fail EBUSY, i.e. a file-level mount cannot accept a
# refresh at all.
echo "== container-side atomic replace onto the FILE mount =="
FRC=0
docker exec "$CNAME" bash -c 'echo "{\"token\":\"v4-via-filemount\"}" > /tmp/n.json && mv /tmp/n.json /mnt/filemount.json' 2>&1 || FRC=$?
echo "  rename onto file mount exit code: $FRC (expect nonzero: EBUSY)"

docker rm -f "$CNAME" >/dev/null

PASS=1
[[ $FRC -ne 0 ]] && echo "CONFIRMED: refresh-shaped write onto a file mount fails (EBUSY)" \
  || { echo "FAIL: rename onto file mount unexpectedly succeeded"; PASS=0; }
[[ "$DIRV" == *v2-rotated* ]] || { echo "FAIL: dir mount went stale"; PASS=0; }
[[ "$FILEV" == *v1-original* ]] && echo "CONFIRMED: file mount pinned the old inode (stale, as predicted)" \
  || echo "NOTE: file mount did NOT go stale on this backend (value: $FILEV) — record it"
[[ "$HOSTV" == *v3-container-rotated* ]] || { echo "FAIL: container-side rotation did not land host-side"; PASS=0; }

[[ $PASS == 1 ]] && echo "PROBE1 PASS: directory mount stays fresh both directions" || { echo "PROBE1 FAIL"; exit 1; }
