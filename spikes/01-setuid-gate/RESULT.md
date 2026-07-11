# S1 — The setuid gate on Docker Desktop named volumes: RESULT

**Status: GREEN (partial — two survival assertions deferred to the serialized
Docker-Desktop-restart drill by instruction). Nothing failed. Fallback ADR
(privileged writer daemon behind a unix socket) NOT needed.**

Run: 2026-07-10, Docker Desktop (server 29.4.0, linux/arm64, runc), macOS
arm64 host, Go 1.24.5 via mise. Probe binary: static (CGO_ENABLED=0)
linux/arm64 Go using modernc.org/sqlite v1.53.0.

## Rerun

```sh
/Users/vinchenkov/Documents/dev/ai/homie/spikes/01-setuid-gate/check.sh
```

Machine-rerunnable, exits nonzero on any failure. It rebuilds the binary and
image, recreates the volume, and walks every assertion below. For the
**drill** (Docker Desktop restart / volume detach-reattach): restart Docker
Desktop, then `docker start mcspike-01-agent` and re-exec the gate probes
(`docker exec -u 10002 mcspike-01-agent agentprobe directopen /spine/spine.db`
must EACCES; `docker exec -u 10002 mcspike-01-agent mc read /spine/spine.db`
must succeed) — or simply rerun `check.sh`, which proves the same gate holds
on a freshly attached volume+container after the restart.

## Setup that the probe proved

- `mc` (the probe binary) baked into the image at `/usr/local/bin/mc`,
  owned by privileged uid 10001 (`mcpriv`), mode `4755` (`-rwsr-xr-x`).
- Spine `spine.db` on **named volume** `mcspike-01-spine` (ext4 inside the
  Docker Desktop VM), dir `/spine` mode `0700` and file mode `0600`, both
  owned by uid 10001.
- Agent container runs as uid 10002 (`agent`); a non-setuid copy of the same
  binary (`agentprobe`) performs the raw `open(2)` probes.

## Assertions

| # | Assertion | Result |
|---|-----------|--------|
| 1 | Probe binary is statically linked (CGO_ENABLED=0, linux/arm64) | PASS |
| 2 | (a) direct `open(2)` O_RDONLY on the DB as agent uid → kernel refuses with **EACCES** | PASS |
| 3 | (a') direct `open(2)` O_RDWR on the DB as agent uid → **EACCES** | PASS |
| 4 | (b) same read via setuid `mc` (modernc.org/sqlite SELECT) succeeds | PASS |
| 5 | (b') write via setuid `mc` (INSERT) succeeds — gate brokers writes | PASS |
| 6 | setuid escalation real: `ruid=10002 euid=10001` inside `mc` | PASS |
| 7 | Canary: docker default runtime is `runc` | PASS |
| 8 | Canary: no user-namespace remap (daemon SecurityOptions + identity `/proc/self/uid_map` `0 0 4294967295` → Enhanced Container Isolation off) | PASS |
| 9 | Canary: `no-new-privileges` NOT set on the agent container (inspect `SecurityOpt=[]` + in-container `NoNewPrivs: 0`) | PASS |
| 10 | Canary: named-volume mount is **not** `nosuid` (`/dev/vda1 /spine ext4 rw,relatime,discard`) | PASS |
| 11 | Canary: image `Architecture: arm64` (native, no QEMU) | PASS |
| 12 | Survival: **container restart** → assertions 2–6 hold | PASS |
| 13 | Survival: **image rebuild** (nonce-busted layers, fresh container, same volume) → assertions 2–6 hold | PASS |
| 14 | Survival: **Docker Desktop restart** | DEFERRED (serialized drill) |
| 15 | Survival: **volume detach/reattach** | DEFERRED (serialized drill) |

## Artifacts left in place for the drill

| Kind | Exact name |
|---|---|
| volume | `mcspike-01-spine` |
| image | `mcspike-01-gate:latest` |
| container | `mcspike-01-agent` (running, `sleep infinity`, no restart policy — `docker start` it after the Desktop restart) |

## Notes / observations

- Docker Desktop 29.4.0 named volumes are ext4 inside the VM, mounted into
  containers with plain `rw,relatime,discard` — uid ownership and the suid
  bit behave natively, exactly as Inv. 2 / §11.5 assume.
- The EACCES on the direct open is produced by the `0700` directory + `0600`
  file, both owned by the privileged uid — either alone suffices.
- WAL sidecars (`-wal`/`-shm`) are created by the setuid process as euid
  10001 and checkpointed away on close; nothing agent-readable is ever left
  on the volume.
- `docker exec` without `-u` inherits the container's agent uid, so even the
  default exec path cannot list `/spine` — observed, not just inferred.
- Files: probe source `probe/main.go`, image `Dockerfile`, check `check.sh`
  (all in this directory).

## Restart drill

Run 2026-07-10 (serialized drill agent). Docker Desktop fully quit and
restarted (hard VM stop — `mcspike-01-agent` showed `Exited (255)`).

| # | Assertion | Result | Evidence |
|---|---|---|---|
| 14 | Survival: Docker Desktop restart | **PASS** | `docker start mcspike-01-agent`, then all five gate probes re-run on the SAME container+volume: directopen → EACCES, directwrite → EACCES, `mc read` → sentinel row, `mc write` → OK, `mc ids` → `ruid=10002 euid=10001` |
| 15 | Survival: volume detach/reattach | **PASS** | `docker rm -f mcspike-01-agent`; fresh container created from `mcspike-01-gate:latest` with the same `mcspike-01-spine` volume; all five probes pass identically (suid bit, uid ownership, 0700/0600 modes all persisted on the volume across detach) |
| — | Full `check.sh` through the restarted daemon | **PASS** | `S1 CHECK: ALL PASS` (rebuild + fresh volume + all assertions incl. container-restart and image-rebuild phases) |

S1 is now fully green. Fallback ADR (privileged writer daemon) not needed.
Artifacts recreated by check.sh and left in place: `mcspike-01-spine`,
`mcspike-01-gate:latest`, `mcspike-01-agent` (running).
