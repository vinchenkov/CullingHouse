# S2 — Warm-helper `docker exec` fidelity: RESULT

Spike per `specs/implementation-handoff.md` Part 2 (S2) against spec §11.5
(the warm helper / self-delegation crossing). Host: macOS arm64, Docker
Desktop, server 29.4.0, runtime `runc`. Helper stand-in: alpine `sleep
infinity` container; every probe drives real non-TTY `docker exec` through
it, exactly the shape of mc's darwin self-delegation.

## Verdict

**GREEN (partial: two assertions deferred by design).** Every runnable
assertion passed. No fallback ADR (Engine-API framed streams / long-lived
framed RPC) is needed for correctness of the exec channel itself. One
protocol obligation surfaced (P5, below): **cancellation must be built by
mc, not inherited from the exec channel** — and, critically, a
signal-interrupted `docker exec` CLI exits `0`, so mc must never let the
CLI's exit status stand in for "the remote command completed."

## Assertion table

| # | Assertion | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Exit codes 0, 1 propagate exactly | PASS | `probes/p1_exit_codes.sh` |
| 1 | 126 (found, not executable) propagates — both via in-container shell and direct exec spawn failure | PASS | docker CLI maps the OCI "permission denied" spawn failure to 126 itself |
| 1 | 127 (not found) propagates — both shapes | PASS | OCI "executable file not found" → CLI exit 127 |
| 1 | 137 (SIGKILL self), 143 (SIGTERM self) propagate | PASS | 128+sig arrives intact |
| 1 | 137 from an *external* SIGKILL of the exec'd process | PASS | second exec kills; waiting sh's 137 crosses the channel |
| 2 | 8-bit-clean 50 MB stdin→stdout round-trip (sha256 + byte count) | PASS | `probes/p2_binary_roundtrip.sh`; both directions and in-container hash agree |
| 3 | Interleaved stdout/stderr demux, 20 000 tightly alternating line pairs: counts, no cross-bleed, no torn lines, per-stream ordering | PASS | `probes/p3_demux.sh` |
| 4 | Long-lived stream held ~8 min (~9.5k sequenced lines at 50 ms cadence): no drop, no gap, no corruption, DONE marker received | PASS | `probes/p4_long_stream.sh` (ran concurrently with P5/P6 load on the same helper) |
| 4b | Full 30-min hold | **DEFERRED** | to the Phase 3 promoted suite (harness 10-min-per-call time cap); rerun via `STREAM_SECS=1800 ./check.sh` |
| 5 | SIGINT/SIGTERM semantics characterized | PASS | `probes/p5_signals.sh`; findings below |
| 6 | N=5 concurrent execs: per-lane rc, header/trailer, 5 MB payload sha256, zero cross-lane bleed | PASS | `probes/p6_concurrent.sh` |
| 7 | Helper `docker kill`ed → wrapper detects, lazily recreates (new container id), next invocation succeeds; also full `docker rm -f` case; exit-code fidelity intact afterwards | PASS | `probes/p7_kill_recreate.sh` |
| 8 | Docker Desktop restart → detect-and-rewarm | **DEFERRED** | to the serialized restart drill (must not restart Docker here) |

## P5 findings — what the helper protocol must therefore do

Observed on Docker CLI/server 29.4.0, non-TTY `docker exec -i`:

1. **Signals are not proxied.** SIGINT/SIGTERM to the `docker exec` client
   never reach the in-container process; it keeps running (verified alive
   after client death, both signals).
2. **The interrupted client exits `0`, immediately.** Not 130/143 — zero,
   with output truncated mid-stream. A cancelled invocation is therefore
   *indistinguishable from a short successful one* by exit status.

Consequences encoded for mc's self-delegation (spec §11.5):

- mc-on-darwin must trap INT/TERM **itself** (it is the exec client) and
  perform cancellation as an **explicit in-container kill** of the exec'd
  PID/process group — a second exec (demonstrated working in the probe) or
  the Engine API's exec-kill equivalent. Docker provides no exec-cancel API;
  the PID must be tracked by the protocol (e.g. the remote command writes
  `$$`/setsid PGID to a tmpfs path, or mc wraps the remote argv).
- mc must never treat the exec client's exit code as authoritative when a
  signal arrived: on trapped-signal it should report the conventional
  128+sig upward after effecting the remote kill, not the CLI's 0.
- Since mc will use the Engine exec API in-process (per spec, "the container
  runtime's exec API", not a shelled-out CLI), both behaviors are naturally
  under mc's control; this probe pins down why the naive CLI inheritance is
  not acceptable.

## Docker artifacts left in place for the restart drill

- container: `mcspike-02-helper` (labels `mc-managed=true`, `mc-tier=helper`, running `sleep infinity`)
- image: `mcspike-02-helper-img:latest` (tag of `alpine:3.22`)
- volumes: none

## Rerun

```sh
cd /Users/vinchenkov/Documents/dev/ai/homie/spikes/02-exec-fidelity
./check.sh                    # full: ~2 min + 8-min stream hold
STREAM_SECS=30 ./check.sh     # quick smoke
STREAM_SECS=1800 ./check.sh   # the deferred 30-min hold (Phase 3 shape)
```

`check.sh` provisions the image/helper if absent, runs P1→P7 (P7 last — it
kills and recreates the helper), leaves the helper warm, and exits nonzero
on any failure. Probes are individually runnable from `probes/`.

## Drill instructions (deferred assertion 8)

After the serialized Docker Desktop restart: run
`./probes/p7_kill_recreate.sh` (the wrapper's lazy-recreate path is the same
detect-and-rewarm code), then `STREAM_SECS=30 ./check.sh` for a full-fidelity
pass through the restarted daemon. Expected: all PASS, helper recreated as a
new container id.

## Restart drill

Run 2026-07-10 (serialized drill agent). Docker Desktop fully quit and
restarted; the helper died with the VM (`mcspike-02-helper` → `Exited
(255)`) — exactly the dead-helper state the wrapper must detect.

| # | Assertion | Result | Evidence |
|---|---|---|---|
| 8 | Docker Desktop restart → detect-and-rewarm | **PASS** | With the helper in `Exited (255)`, `probes/p7_kill_recreate.sh` ran clean: wrapper detected the dead helper, lazily recreated it as a NEW container id, next exec succeeded, exit-code fidelity intact (rc=7 case). Then `STREAM_SECS=30 ./check.sh` full pass through the restarted daemon: P1–P7 all PASS (demux, 50 MB round-trip, 30 s stream hold, signals, concurrency) |

Helper left warm (new container id, same name `mcspike-02-helper`). The only
remaining deferred item is the 30-min stream hold (Phase 3 promoted suite).
