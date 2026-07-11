# S7 — launchd + sleep + clock drill: RESULT

Status: **partial** (green on everything runnable in an unattended session;
the 30-min macOS sleep leg and the Resource Saver pause/resume proof are
operator-only, DEFERRED with exact instructions below). Nothing failed.

Run: 2026-07-10, macOS arm64 (Darwin 25.5.0), Docker Desktop 4.70.0
(server 29.4.0), launchd LaunchAgent label `mcspike-07` — the sanctioned
spike exception to "no launchd during dev"; **unloaded and deleted after the
run, verified gone** (`launchctl print gui/501/mcspike-07` fails, no plist
in `~/Library/LaunchAgents`, no mcspike job in `launchctl list`).

## Rerun

```sh
/Users/vinchenkov/Documents/dev/ai/homie/spikes/07-launchd-clock/check.sh
# opt-in destructive leg (quits + restarts Docker Desktop, ~10 min):
S7_DRILL_DOCKER_QUIT=1 ./check.sh
```

`check.sh` loads the real LaunchAgent, verifies every assertion below that
is safe unattended, then **always** boots the agent out and removes the
plist (trap on EXIT, even on failure). Exits nonzero on any failure.

## Assertions

| # | Assertion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Real LaunchAgent loads; ticks fire (RunAtLoad + StartInterval=20) | PASS | `tick-drill.log`: ticks at 20 s cadence, distinct pids per tick |
| 2 | Minimal PATH, no user shell env | PASS | every tick logs `PATH=/usr/bin:/bin:/usr/sbin:/sbin`; docker CLI is NOT on that PATH and was resolved by absolute path `/usr/local/bin/docker` |
| 3 | Correct Docker socket/context resolution from the launchd context | PASS | each tick resolves `context=desktop-linux endpoint=unix:///Users/vinchenkov/.docker/run/docker.sock` via `docker context inspect` (pure client-side; works with the daemon down) |
| 4 | Docker not up → backoff-and-retry, no crash-loop | PASS | with Docker quit, ticks logged `docker DOWN attempt=1..4` with 1s/2s/4s/8s backoff then `backoff exhausted … yielding to next StartInterval tick`; two full cycles observed; agent re-bootstrapped **while Docker was down** and behaved identically (the "not yet up at load" case); `launchctl print` showed `last exit code = 0` throughout — launchd never saw a crash, no throttling/thrash |
| 5 | Docker started again → recovery observed | PASS | first tick after the daemon returned: `docker OK server=29.4.0 attempt=1` + a clock sample (see `tick-drill.log` 02:46:56Z) |
| 6 | VM clock vs host clock agree | PASS | per-tick sample: container `date +%s` vs host, bounded by the round trip; every bound within **[-1,+2] s** (1-s resolution method), including immediately after two hard Docker Desktop restarts — no drift observed |
| 7 | 30-min macOS **sleep** mid-lease → wake → immediate tick, clocks still agree | **DEFERRED (operator-only)** | instructions below |
| 8 | Resource Saver pause/resume tick survival | **DEFERRED (operator decision)** | Resource Saver is currently **enabled** (see snapshot); spec wants it disabled or proven — instructions below |
| 9 | Agent unloaded + plist deleted afterwards, verified | PASS | bootout OK; `launchctl print` fails; plist absent |

## Findings (feed these into the product design)

1. **TCC blocks launchd from ~/Documents.** The agent's first load exited
   126: `/bin/sh: …/spikes/07-launchd-clock/tick.sh: Operation not
   permitted` — macOS TCC denies launchd-spawned processes access to
   Documents/Desktop/Downloads. Runtime payload was moved to
   `~/Library/Caches/mcspike-07/` and worked immediately. **Rule: everything
   mc runs under launchd (binary, scripts, logs, DB paths it touches on the
   host) must live outside TCC-protected folders** (e.g. `~/Library/...`),
   or onboarding must walk the operator through a Full Disk Access grant.
2. **`docker info --format` exit code is NOT a liveness signal.** During
   daemon start/stop windows the CLI (29.4.0) exits **0** while printing
   `request returned 500 Internal Server Error …` to stdout; each such call
   can also stall ~30 s. mc must judge liveness on response shape (or use
   the Engine API `/_ping`), never on the CLI exit code — the probe was
   fixed to validate the output looks like a version string.
3. **Docker Desktop 4.70.0's quit path can wedge.** After `osascript quit`,
   `com.docker.backend` kept dialing the dead VM (`still dialing
   192.168.65.7:2376`) for 10+ minutes, holding the socket half-alive
   (stalled 500s). Relaunching while wedged triggers a GUI "lingering
   processes" error dialog that blocks startup until a human clicks.
   **Rule for mc's watchdog: after requesting quit, wait for
   `com.docker.backend` to actually exit before relaunching; escalate to
   SIGTERM/SIGKILL of the backend after a deadline** (the drill needed this
   once; after a clean kill, relaunch reached a stable daemon in ~10 s).
4. Clock agreement held across two hard VM restarts (skew bound ≤2 s at 1-s
   resolution). The risky case remains macOS **sleep** (assertion 7).

## Deferred: operator instructions

**30-min sleep drill (assertion 7):**
1. `cd spikes/07-launchd-clock && ./check.sh` — but interrupt it after step
   "3. ticks fire" (Ctrl-C is safe: the EXIT trap unloads the agent), or
   simpler: run `cp tick.sh ~/Library/Caches/mcspike-07/ && cp
   mcspike-07.plist ~/Library/LaunchAgents/ && launchctl bootstrap gui/501
   ~/Library/LaunchAgents/mcspike-07.plist`.
2. Close the lid / Apple menu → Sleep for **≥30 min** (on AC power, default
   powernap settings).
3. Wake, then within a minute check `~/Library/Caches/mcspike-07/tick.log`:
   expect a tick within ~20 s of wake (launchd coalesces missed
   StartInterval fires into one immediate run), `docker OK`, and a
   `clock … skew_bound=[a,b]` line with a,b within ±2 s. A large bound =
   the known VM-clock-drift-after-sleep failure mode → mc must resync
   (restart the VM or `hwclock -s` equivalent) before trusting lease math.
4. **Mandatory cleanup:** `launchctl bootout gui/501/mcspike-07 && rm
   ~/Library/LaunchAgents/mcspike-07.plist`.

**Resource Saver (assertion 8):** currently **enabled**. Either disable it
(Docker Desktop → Settings → Resources → Resource Saver) and record that in
`OPERATOR-INPUTS.md`, or repeat the sleep drill with all containers idle for
>5 min (AutoPauseTimeoutSeconds=300) and confirm ticks ride through the
pause/resume. Note the drill environment never idled long enough to pause
(the 20-s ticks themselves keep the VM busy).

## Docker Desktop settings snapshot (for OPERATOR-INPUTS.md)

Read from `~/Library/Group Containers/group.com.docker/settings-store.json`
on 2026-07-10 (values recorded here for the operator to transcribe; this
file is not the operator-inputs file):

| Setting | Value | Spec expectation (handoff Part 1 §4) |
|---|---|---|
| Docker Desktop version | 4.70.0 (server 29.4.0) | pin against auto-update |
| `AutoStart` ("start at sign-in") | **false** | spec wants **on** — operator action |
| `UseResourceSaver` | **true** | spec wants disabled **or** S7 pause/resume proof — operator action |
| `AutoPauseTimeoutSeconds` | 300 | (Resource Saver idle window) |
| `EnhancedContainerIsolation` | false | must be off — OK |
| `Cpus` | 14 | ≥4 — OK |
| `MemoryMiB` | 8092 | ≥8 GB — OK (borderline: 8092 MiB) |
| `SwapMiB` | 1024 | — |
| `DiskSizeMiB` | 122880 | — |
| `UseVirtualizationFrameworkVirtioFS` | true | "VirtioFS backend noted" — noted |
| `FilesharingDirectories` | /Users, /Volumes, /private, /tmp, /var/folders | — |
| user-namespace remap | none (verified in S1 canaries) | must be off — OK |

## Files / artifacts

- `tick.sh` — the tick probe (source of truth; a runtime copy goes to
  `~/Library/Caches/mcspike-07/` because of finding 1)
- `mcspike-07.plist` — the LaunchAgent definition (kept here as a template;
  **not installed** — verified removed from `~/Library/LaunchAgents`)
- `check.sh` — machine-rerunnable end-to-end check (always cleans up)
- `tick-drill.log` — full log of the live drill (ticks → Docker quit →
  backoff → re-bootstrap while down → recovery → clock samples)
- `tick.log` — log of the final `check.sh` verification run
- Docker artifacts: **none created by S7** (clock samples used the existing
  `mcspike-05-base` image). Launchd artifacts: **none remain**.
- `~/Library/Caches/mcspike-07/` still holds the inert runtime copy of
  `tick.sh` + logs (nothing references it; removal was permission-denied in
  the drill session — safe to `rm -rf` anytime; `check.sh` recreates it).
