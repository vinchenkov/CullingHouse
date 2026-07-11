# S8 — arm64 image build + Playwright smoke: RESULT

Status: **GREEN** (all assertions PASS; one item DEFERRED to the serialized restart drill).
Probe run: 2026-07-10, Docker Desktop (arm64 host, runc), daemon arch aarch64.

## Assertions

| # | Assertion | Result | Evidence |
|---|---|---|---|
| A1 | Native build path: host arm64, daemon aarch64, same arch (no emulation possible) | PASS | `uname -m` = arm64; `docker info` Architecture = aarch64; no `--platform` flag anywhere in Dockerfile or build command |
| A2 | Image builds natively; build time measured | PASS | Cold build **103 s**; cached rerun **~1 s** |
| A3 | `docker image inspect` reports Architecture arm64 / Os linux | PASS | `linux/arm64` |
| A4 | Size + digest recorded (the pin — agents must never rebuild in a loop) | PASS | 2 782 721 660 bytes (**2653 MiB**); image ID below |
| A5 | In-container: `uname -m` = aarch64; all pins verified (git ≥ 2.48, node, bun, claude, codex, playwright) | PASS | git 2.54.0, node v22.17.1, bun 1.3.9, claude 2.1.207, codex-cli 0.144.1, playwright 1.61.1 |
| A6 | One Chromium launch + screenshot from inside the container (Playwright), as non-root `agent` user | PASS | Chromium 149.0.7827.0; valid 10 090-byte PNG (`smoke.png`, verified visually and by signature) |
| A7 | Image survives Docker Desktop restart (digest unchanged, no re-pull/rebuild) | DEFERRED | Serialized restart drill; check: `docker image inspect mcspike-08-base --format '{{.Id}}'` still equals the pin below, then rerun `./check.sh` |

## The pin

```
image:      mcspike-08-base:latest
image ID:   sha256:ca525f7f18a9ea2dfdbafc5359d05a1a9a4fd1fc1e11bda193c05167354978a9
size:       2782721660 bytes (2653 MiB)
cold build: 103 s   cached rebuild: ~1 s
```

The image ID (config digest) is the pin for a local-only image; a registry
`RepoDigest` exists only after a push, which onboarding will do if a registry
enters the picture. **Agents: compare against this ID; if it matches, do not
rebuild.**

Version pins live as exact-version `ARG`s in the Dockerfile (spec §11.2:
artifacts, not prose): CLAUDE_CODE_VERSION=2.1.207, CODEX_VERSION=0.144.1,
NODE_VERSION=22.17.1, BUN_VERSION=1.3.9, PLAYWRIGHT_VERSION=1.61.1, base
ubuntu:24.04 (+ git-core PPA for git ≥ 2.48; noble ships 2.43).

## Docker artifacts left in place for the restart drill

- image `mcspike-08-base:latest` (labels: `mc-spike=08`, `mc-image-role=base-candidate`)
- container `mcspike-08-smoke` (stopped; holds `/tmp/smoke.png`)
- no volumes

## Rerun

```
/Users/vinchenkov/Documents/dev/ai/homie/spikes/08-arm64-image/check.sh
```

Exits nonzero on any failed assertion. Cached rerun completes in a few seconds;
it rebuilds only if the layer cache was purged (in which case expect ~2 min and
a NEW image ID — record it here if that happens deliberately).

## Notes / findings

- **No QEMU**: emulation on Docker Desktop only engages when the requested
  platform differs from the daemon's. The Dockerfile and check.sh never pass
  `--platform`, and image arch == daemon arch == host arch; the in-container
  `uname -m` = aarch64 confirms native execution.
- **Chromium as non-root needs `--no-sandbox`**: Docker's default seccomp
  profile blocks the unprivileged user namespaces Chromium's own sandbox
  wants; the container is the sandbox in the MC design. Recorded so Phase 3
  wires the Verifier's Playwright launch args accordingly.
- Playwright layer is last and separate (spec §11.2), so harness-CLI pin bumps
  rebuild in seconds without re-downloading the ~150 MB browser payload. The
  browser lives at `PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers` (world-readable)
  so any uid can use it.
- The real base image additionally bakes the setuid `mc` binary; that is S1's
  probe and `mc` does not exist yet — out of scope here (blast radius:
  Dockerfile-only).
- Size (2.65 GiB) matches the handoff's expectation that the browser layer adds
  ~1–2 GB: `docker history` puts the Playwright layer at 1.38 GB; everything
  below it (base + git + node + bun + harness CLIs) totals ~1.4 GB.

## Files

- `Dockerfile` — the representative base image (spec §11.2 shape)
- `probe-browser.js` — in-container Chromium launch + screenshot probe
- `check.sh` — machine-rerunnable end-to-end check (this table's source of truth)
- `smoke.png` — the captured screenshot

## Restart drill

Run 2026-07-10 (serialized drill agent). Docker Desktop fully quit and
restarted (hard VM stop).

| Assertion | Result | Evidence |
|---|---|---|
| A7: image survives Docker Desktop restart — digest unchanged, no rebuild/re-pull | **PASS** | `docker image inspect mcspike-08-base:latest` after the restart: `id=sha256:ca525f7f18a9ea2dfdbafc5359d05a1a9a4fd1fc1e11bda193c05167354978a9`, `arch=arm64` — byte-identical to the pre-restart id recorded at Stage A; inspect answered from the local store (no pull traffic, no build). `mcspike-08-smoke` (stopped, holds /tmp/smoke.png) also survived |
