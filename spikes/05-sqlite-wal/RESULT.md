# S5 — SQLite WAL + crash discipline on the named volume: RESULT

Status: **partial** (green on everything runnable today; the Docker Desktop
restart-mid-write assertion is DEFERRED to the serialized drill by design).
Fallback ADR (`journal_mode=DELETE` + exclusive locking): **not needed**.

Probe: Go + `modernc.org/sqlite` v1.53.0 (pure Go, `CGO_ENABLED=0`), compiled
as a static linux/arm64 binary on the host and executed **inside** a
linux/arm64 container with the DB on a runtime-local named volume (Inv. 24).

## Rerun

```sh
/Users/vinchenkov/Documents/dev/ai/homie/spikes/05-sqlite-wal/check.sh
```

Rebuilds the probe, recreates the volume + runner from scratch, runs every
assertion, exits nonzero on any failure. Takes ~30s. Requires Docker Desktop
running and mise (Go).

## Assertions

| # | Assertion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | WAL + `busy_timeout` + `BEGIN IMMEDIATE` work on the named volume | PASS | `journal_mode=wal` read back on the volume (`ext4` on `/dev/vda1` inside the VM); `BEGIN IMMEDIATE`+COMMIT path exercised; second writer against a held write lock waited **2.06s** (busy_timeout=2000) then got `SQLITE_BUSY (5)` — the timeout demonstrably applied, not an instant failure |
| 2 | `kill -9` writer mid-transaction → reopen → `integrity_check` ok, committed rows survive, uncommitted vanish | PASS | Hang writer held an open `BEGIN IMMEDIATE` txn with 20 uncommitted 64KB-blob rows **spilled to the WAL file on disk** (probe forces `cache_size(-16)` and asserts WAL growth, so the crash test is not vacuous); SIGKILL'd in-container; reopen: `integrity_check=ok`, 15 committed rows present, 0 `uncommitted` rows |
| 3 | Concurrent reader during write sees consistent data | PASS | While the writer txn was open with uncommitted frames in the WAL, an independent reader connection returned the committed snapshot (15 rows, none of the uncommitted 20) in **1.7ms** — unblocked |
| 4 | modernc.org/sqlite applies per-connection PRAGMAs on EVERY connection path | PASS | Pool with `SetMaxOpenConns(4)`, all 4 conns held simultaneously (forcing 4 distinct driver connections); each read back `busy_timeout=5000`, `journal_mode=wal`, `foreign_keys=1` from the DSN `_pragma=` params. `busy_timeout` and `foreign_keys` are per-connection state, so this proves per-conn application, not a persistent DB-file property |
| 5 | Fail-closed guard: DB on a bind-mounted (VirtioFS) path is REJECTED | PASS | Guard parses `/proc/self/mountinfo`, finds the longest-prefix mount for the DB dir, and **allowlists** only block-device-backed local fs (ext4/ext3/xfs/btrfs with a `/dev/` source). Named volume → accepted (`ext4` `/dev/vda1`). Bind mount → refused with exit 1 and no DB file created; guard runs before `sql.Open` in every subcommand |
| 6 | Docker Desktop restart mid-write | **DEFERRED** | Serialized restart drill; artifacts + instructions below |

## Finding worth keeping (feeds the permanent guard)

On this Docker Desktop (29.4.0, VirtioFS enabled) the bind mount surfaces in
mountinfo as **`fstype=fakeowner source=/run/host_mark/Users`** — Docker
Desktop's FUSE ownership shim — *not* as `virtiofs`. A denylist keyed on
"virtiofs" would have **accepted** it. The allowlist (block-device-backed
local fs only) is the correct fail-closed shape and must be what the product
guard implements. The container rootfs (`overlay`) is also correctly refused.

## Toolchain note

`modernc.org/sqlite` v1.53.0 declares `go >= 1.25.0`; mise's pinned go1.24.5
auto-fetched the go1.25.12 toolchain to build (Go toolchain-switching, no
config change). Product code must either bump the pinned Go to ≥1.25 or pin
`modernc.org/sqlite` to the last go1.24-compatible release — decide at Phase 1.

## Artifacts left in place for the restart drill

| Kind | Exact name | Contents |
|------|------------|----------|
| named volume | `mcspike-05-spine` | `spine.db` with 15 committed rows (10 `seed` + 5 `committed`), WAL mode, post-crash-recovery state |
| container | `mcspike-05-runner` | running `sleep infinity`; mounts volume at `/spine`, probe at `/probe` (ro), bindtest at `/bindtest` |
| image | `mcspike-05-base` | tag of `alpine:3.20` (arm64) |
| probe binary | `/Users/vinchenkov/Documents/dev/ai/homie/spikes/05-sqlite-wal/bin/s5probe` | static linux/arm64; also reachable in-container as `/probe/s5probe` |

Do **not** `docker volume rm mcspike-05-spine` before the drill.

## Restart-drill instructions (assertion 6)

1. Start a mid-write hang (uncommitted frames on disk in the WAL):
   ```sh
   docker exec mcspike-05-runner rm -f /spine/hang.pid
   docker exec -d mcspike-05-runner /probe/s5probe hang \
     --db /spine/spine.db --rows 20 --tag uncommitted --pidfile /spine/hang.pid
   # wait until: docker exec mcspike-05-runner test -s /spine/hang.pid
   ```
2. Restart Docker Desktop (the drill's serialized step).
3. After the daemon is back:
   ```sh
   docker start mcspike-05-runner   # container will not auto-restart
   docker exec mcspike-05-runner /probe/s5probe verify \
     --db /spine/spine.db --expect 15 --absent-tag uncommitted
   docker exec mcspike-05-runner rm -f /spine/hang.pid
   ```
   Pass = `integrity_check=ok`, 15 rows, 0 uncommitted — identical criteria to
   the kill -9 case. (If the drill runs `check.sh` beforehand, the volume is
   recreated and the expected count is again 15.)

## Files

- `probe/main.go` — throwaway probe, all subcommands (guard/seed/write/pragmacheck/hang/read/contend/verify)
- `check.sh` — machine-rerunnable end-to-end check (12 steps)
- `bin/s5probe` — built static linux/arm64 probe binary
- `bindtest/` — empty host dir bind-mounted into the runner as the rejection target

## Restart drill

Run 2026-07-10 (serialized drill agent). Assertion 6 executed exactly per the
instructions above: hang writer started (`/probe/s5probe hang --rows 20 --tag
uncommitted`), pidfile confirmed, WAL grown to ~1.3 MB with uncommitted
frames on disk; Docker Desktop then fully quit (the VM died with the txn
open — container later shows `Exited (255)`), restarted, `docker start
mcspike-05-runner`, then verify.

| Assertion | Result | Evidence |
|---|---|---|
| 6. Docker Desktop restart mid-write → reopen clean | **PASS** | `guard: accepted /spine (ext4 /dev/vda1)`; `integrity_check=ok`; `journal_mode=wal`; `rows=15 rows(tag=uncommitted)=0` — committed rows intact, all 20 uncommitted rows gone |

Notes: after the VM restart the WAL/-shm sidecars were recovered and
checkpointed away on first open. The 4-byte `hang.pid` written just before
the restart came back 0 bytes (unsynced file data lost with the VM — fine
for a pidfile, and a useful reminder that non-SQLite files on the volume get
no durability without fsync). S5 is now fully green; fallback ADR not needed.
