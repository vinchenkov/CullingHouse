#!/usr/bin/env bun
// main.ts — the skeleton pipeline agent runner (docs/phase1b-contract.md §4,
// spec §11.5 "the pipeline runner": one turn, one harness, exit with it).
//
// Flow, exactly the contract's:
//   1. read /mc/run.json (fixed path, RO mount — §11.5 launch envelope);
//   2. start the fake harness through the adapter invocation
//      (`bun /app/src/fake-harness/cli.ts --behavior … --session-dir …`),
//      supplying the ONE top-level turn (run.json.brief, unchanged) on stdin;
//   3. export the env-interpolation channel — the fake family's "brief
//      comprehension" (contract §4): MC_RUN_ID, MC_ROLE, MC_SUBJECT_ID
//      (empty when null), MC_POOL_IDS (comma-separated), MC_SPINE (inherited
//      from the container env the resident set);
//   4. read the harness stdout event stream ONLY to detect the session
//      handle and completion (spec §9): on `session-start`, register the
//      native session locators (`mc run register-session … --file
//      native.jsonl` — registered, never assumed, contract §4 flag 1) and
//      begin heartbeating (`mc heartbeat <run_id>`) every
//      heartbeat_interval_s — the heartbeat is the RUNNER's, never the
//      harness's (§10);
//   5. wait for harness exit and exit with its code — the runner never
//      converts an ordinary harness exit into success (§11.5).
//
// It does NOT: poll for work, retry, interpret output, call `mc complete`,
// or touch the spine otherwise (§11.5 "It does not" list). A failed
// heartbeat or registration is logged and never fatal — the lease fencing
// in the spine is the authority on staleness (§10).

// Fixed in-container paths; the env overrides exist ONLY for the test tier
// (runner-process.test.ts runs main() as a real subprocess against a temp
// envelope + a stub mc) — the agent container never sets either.
const RUN_JSON = process.env["MC_RUN_JSON"] || "/mc/run.json";
const HARNESS_CLI = process.env["MC_HARNESS_CLI"] || "/app/src/fake-harness/cli.ts";
const NATIVE_FILENAME = "native.jsonl"; // cli.ts's fixed name; registered via --file

export interface RunEnvelope {
  run_id: string;
  role: string;
  subject_id: number | null;
  pool_ids?: number[];
  heartbeat_interval_s: number;
  brief: string;
  harness_config: { behavior: string };
  mounts: { session: string };
}

/** The env-interpolation channel (contract §4): how a scripted behavior
 * "comprehends its brief". MC_SPINE rides in via `base` (container env). */
export function harnessEnv(
  run: RunEnvelope,
  base: Record<string, string | undefined>,
): Record<string, string | undefined> {
  return {
    ...base,
    MC_RUN_ID: run.run_id,
    MC_ROLE: run.role,
    MC_SUBJECT_ID: run.subject_id == null ? "" : String(run.subject_id),
    MC_POOL_IDS: (run.pool_ids ?? []).join(","),
  };
}

/** Incremental line splitter for the stdout event stream: chunks arrive at
 * arbitrary boundaries; onLine fires once per complete line. */
export function makeLineSplitter(onLine: (line: string) => void): (chunk: string) => void {
  let buf = "";
  return (chunk: string) => {
    buf += chunk;
    for (let i = buf.indexOf("\n"); i >= 0; i = buf.indexOf("\n")) {
      onLine(buf.slice(0, i));
      buf = buf.slice(i + 1);
    }
  };
}

function log(msg: string): void {
  console.error(`[agent-runner] ${msg}`);
}

async function mc(argv: string[]): Promise<number> {
  const proc = Bun.spawn(["mc", ...argv], {
    stdin: "ignore",
    stdout: "ignore",
    stderr: "pipe",
  });
  const [code, stderr] = await Promise.all([
    proc.exited,
    new Response(proc.stderr).text(),
  ]);
  if (code !== 0) log(`mc ${argv.join(" ")} exited ${code}: ${stderr.trim()}`);
  return code;
}

async function main(): Promise<number> {
  const run = (await Bun.file(RUN_JSON).json()) as RunEnvelope;
  log(`run ${run.run_id} role=${run.role} subject=${run.subject_id ?? "none"}`);

  const proc = Bun.spawn(
    [
      "bun",
      HARNESS_CLI,
      "--behavior",
      run.harness_config.behavior,
      "--session-dir",
      run.mounts.session,
    ],
    {
      stdin: new TextEncoder().encode(run.brief),
      stdout: "pipe",
      stderr: "inherit",
      env: harnessEnv(run, process.env),
    },
  );

  let heartbeatTimer: ReturnType<typeof setInterval> | undefined;
  let sessionSeen = false;

  const onEvent = (line: string) => {
    // Mirror the event stream to our own stdout (docker logs observability);
    // the runner acts only on session-start (spec §9).
    console.log(line);
    if (sessionSeen) return;
    let ev: { event?: string; session_id?: string };
    try {
      ev = JSON.parse(line) as typeof ev;
    } catch {
      return; // not an event line; ignore
    }
    if (ev.event !== "session-start" || typeof ev.session_id !== "string") return;
    sessionSeen = true;
    // Register the native session locators (ADR-001 D5, §15.4) …
    void mc([
      "run",
      "register-session",
      run.run_id,
      "--native-ref",
      ev.session_id,
      "--file",
      NATIVE_FILENAME,
    ]);
    // … and start heartbeating: only after the adapter established the
    // session (§10), immediately and then every interval. Failures are
    // logged, never fatal (a released/stale lease fences them, §10).
    void mc(["heartbeat", run.run_id]);
    heartbeatTimer = setInterval(() => {
      void mc(["heartbeat", run.run_id]);
    }, run.heartbeat_interval_s * 1000);
  };

  const split = makeLineSplitter(onEvent);
  const decoder = new TextDecoder();
  for await (const chunk of proc.stdout) {
    split(decoder.decode(chunk, { stream: true }));
  }

  const code = await proc.exited;
  if (heartbeatTimer !== undefined) clearInterval(heartbeatTimer);
  log(`harness exited ${code}${sessionSeen ? "" : " (no session-start seen)"}`);
  return code;
}

if (import.meta.main) {
  process.exit(await main());
}
