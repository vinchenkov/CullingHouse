// homie-runner/main.ts — the lease-free Homie conversation runner (spec §11.5,
// §15.3; ADR-016 D3). It runs inside a tier:"homie" container the resident
// woke and bound. Unlike the pipeline agent-runner it:
//   - NEVER heartbeats and holds NO lease (Homie is the concurrent, lease-free
//     tier, Inv. 1/22);
//   - is LONG-LIVED: it loops claim → run one harness turn → post the reply,
//     draining the conversation, and exits only when the conversation is quiet
//     (idle-out) — the --rm container then cleans itself up;
//   - reports `mc homie runner-started` ONCE at boot with its own launch +
//     64-hex container id (both carried in the republished run.json, §S2). A
//     fenced report means this launch was superseded: exit immediately.
//
// The loop's spine is two host verbs: `mc homie claim` (atomically take the
// oldest pending inbound turn — durable, so a fresh runner resumes a held
// turn) and `mc homie reply --to <id>` (append the reply, complete the claim,
// fan the outbox). Native-locator registration and native/rows RESUME are
// deliberately out of scope here (they need a not-yet-built homie register
// verb); this runner is the fresh claim→reply happy path.

const RUN_JSON = process.env["MC_RUN_JSON"] || "/mc/run.json";
const HARNESS_CLI = process.env["MC_HARNESS_CLI"] || "/app/src/fake-harness/cli.ts";

/** Exit after this many consecutive empty claims (idle-out). The dispatch tick
 * owns the authoritative idle-END on the spine clock (branch 6); this is the
 * runner freeing its own container once there is plainly nothing to answer. */
const DEFAULT_MAX_IDLE_POLLS = 3;
/** Delay between empty polls. */
const DEFAULT_POLL_MS = 1000;

/** The homie launch envelope (a subset of run.json). Written by the resident's
 * wake effector and republished with container_id before start (§S2). */
export interface HomieEnvelope {
  run_id: string;
  session_id: string;
  tier: "homie";
  launch: string;
  container_id: string;
  /** Historical route key "harness/model_binding" (e.g. "fake/fake"). */
  binding: string;
  mode: string;
  harness_config?: { behavior: string };
  mounts: { session: string };
}

/** One claimed inbound turn (the `message` field of `mc homie claim`). */
export interface ClaimedMessage {
  id: number;
  seq: number;
  surface: string;
  channel_ref: string | null;
  body: string;
  attachments: string[];
}

/** One harness turn's outcome. `reply` is null when the harness produced no
 * completed turn (crash/hang/failure). `nativeSessionId` is the harness's own
 * session id from its session-start event, present the first time it is seen —
 * the loop registers it once as the session's native locator (ADR-012). */
export interface TurnResult {
  reply: string | null;
  nativeSessionId?: string;
}

/** Injectable seams so the loop is testable without real subprocesses. */
export interface RunnerDeps {
  /** Run `mc <argv>` and parse its stdout as JSON (null when not JSON). */
  mcJSON: (argv: string[]) => Promise<Record<string, unknown> | null>;
  /** Run ONE harness turn for `turn`; resolves the reply + observed native id. */
  runHarnessTurn: (run: HomieEnvelope, turn: string) => Promise<TurnResult>;
  sleep: (ms: number) => Promise<void>;
  log: (msg: string) => void;
  maxIdlePolls: number;
  pollMs: number;
}

/** The trace file every harness writes into the session folder (§15.4). */
export const NATIVE_FILENAME = "native.jsonl";

/** Incremental line splitter for the harness stdout event stream. */
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

/** Scan a harness event line for a successful turn-complete's output. Returns
 * the output string, or undefined for any other line. A failed/absent
 * completion (crash/hang) simply never yields one. */
export function replyFromEvent(line: string): string | undefined {
  let ev: { event?: string; status?: string; output?: unknown };
  try {
    ev = JSON.parse(line) as typeof ev;
  } catch {
    return undefined;
  }
  if (ev.event === "turn-complete" && ev.status === "success" && typeof ev.output === "string") {
    return ev.output;
  }
  return undefined;
}

/** Scan a harness event line for the session-start's native session id. */
export function nativeIdFromEvent(line: string): string | undefined {
  let ev: { event?: string; session_id?: unknown };
  try {
    ev = JSON.parse(line) as typeof ev;
  } catch {
    return undefined;
  }
  if (ev.event === "session-start" && typeof ev.session_id === "string") {
    return ev.session_id;
  }
  return undefined;
}

/** The lease-free claim→reply loop. Returns the process exit code. */
export async function runHomieLoop(run: HomieEnvelope, deps: RunnerDeps): Promise<number> {
  // Boot fence: report runner-started once. A fenced report means a newer
  // launch superseded this one (or the session ended) — this runner is a
  // zombie; exit cleanly and let the --rm container disappear.
  const started = await deps.mcJSON([
    "homie", "runner-started", run.session_id,
    "--launch", run.launch, "--container-id", run.container_id,
  ]);
  if (started !== null && started["fenced"] === false) {
    deps.log(`runner-started fenced (launch ${run.launch} superseded); exiting`);
    return 0;
  }

  let idlePolls = 0;
  let registered = false;
  while (idlePolls < deps.maxIdlePolls) {
    const claimed = await deps.mcJSON(["homie", "claim", run.session_id]);
    const message = (claimed?.["message"] ?? null) as ClaimedMessage | null;
    if (message === null) {
      idlePolls += 1;
      await deps.sleep(deps.pollMs);
      continue;
    }
    idlePolls = 0;

    const result = await deps.runHarnessTurn(run, message.body);
    // Register the native session locator once (ADR-012, Inv. 26): the trace
    // filename is permanent and enables a later `mc homie resume`. Idempotent
    // on the same pair; a failure is non-fatal (the next turn retries).
    if (!registered && result.nativeSessionId !== undefined) {
      const reg = await deps.mcJSON([
        "run", "register-session", run.session_id,
        "--native-ref", result.nativeSessionId, "--file", NATIVE_FILENAME,
      ]);
      if (reg !== null) registered = true;
    }
    if (result.reply === null) {
      // The turn stays claimed (durable): a fresh runner will reclaim it. A
      // broken harness must not spin the loop, so exit non-zero as evidence.
      deps.log(`turn ${message.id}: harness produced no reply; leaving it claimed`);
      return 1;
    }
    const posted = await deps.mcJSON([
      "homie", "reply", run.session_id, "--to", String(message.id), "--body", result.reply,
    ]);
    if (posted === null) {
      deps.log(`turn ${message.id}: reply post failed; leaving the turn claimed`);
      return 1;
    }
  }
  deps.log(`idle after ${deps.maxIdlePolls} empty polls; exiting`);
  return 0;
}

function log(msg: string): void {
  console.error(`[homie-runner] ${msg}`);
}

/** Run `mc` and parse stdout JSON; a non-zero exit or non-JSON stdout is null
 * (logged). The verbs return a JSON object on success. */
async function mcJSON(argv: string[]): Promise<Record<string, unknown> | null> {
  const proc = Bun.spawn(["mc", ...argv], { stdin: "ignore", stdout: "pipe", stderr: "pipe" });
  const [code, out, err] = await Promise.all([
    proc.exited,
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
  ]);
  if (code !== 0) {
    log(`mc ${argv.join(" ")} exited ${code}: ${err.trim()}`);
    return null;
  }
  try {
    return JSON.parse(out) as Record<string, unknown>;
  } catch {
    return out.trim() === "" ? {} : null;
  }
}

/** Production harness turn: spawn the (fake) harness with the session's
 * behavior, feed the operator's turn on stdin, and read the turn-complete
 * output + session-start native id off the event stream. A non-fresh mode
 * asks the harness to continue the recorded native session; the fake adapter
 * ignores the flag (each invocation is independent), so cross-turn continuity
 * is a real-harness concern noted for a later slice. */
async function runHarnessTurn(run: HomieEnvelope, turn: string): Promise<TurnResult> {
  const behavior = run.harness_config?.behavior;
  if (behavior === undefined) {
    log("run.json carries no harness_config.behavior; cannot run a fake turn");
    return { reply: null };
  }
  if (run.mode !== "fresh") {
    // A real per-harness adapter would continue the recorded native session
    // (native mode) or replay conversation rows (rows mode); the fake adapter
    // is stateless per invocation, so it starts anew. Cross-turn native
    // continuity is a real-harness concern noted for a later slice.
    log(`mode ${run.mode}: fake adapter starts anew (no native continuity)`);
  }
  const proc = Bun.spawn(
    ["bun", HARNESS_CLI, "--behavior", behavior, "--session-dir", run.mounts.session],
    { stdin: new TextEncoder().encode(turn), stdout: "pipe", stderr: "inherit" },
  );
  let reply: string | undefined;
  let nativeSessionId: string | undefined;
  const split = makeLineSplitter((line) => {
    console.log(line); // mirror to docker logs
    const out = replyFromEvent(line);
    if (out !== undefined) reply = out;
    const nid = nativeIdFromEvent(line);
    if (nid !== undefined) nativeSessionId = nid;
  });
  const decoder = new TextDecoder();
  for await (const chunk of proc.stdout) {
    split(decoder.decode(chunk, { stream: true }));
  }
  const code = await proc.exited;
  if (code !== 0 || reply === undefined) {
    log(`harness turn exited ${code}${reply === undefined ? " (no turn-complete)" : ""}`);
    return { reply: null, nativeSessionId };
  }
  return { reply, nativeSessionId };
}

async function main(): Promise<number> {
  const run = (await Bun.file(RUN_JSON).json()) as HomieEnvelope;
  log(`homie run ${run.session_id} binding=${run.binding} mode=${run.mode}`);
  // This runner ships only the fake adapter. The fake/fake route always runs;
  // any other route ("harness/model_binding") runs only when the deployment
  // authorized this adapter to stand in for it via MC_AGENT_RUNNER_ROUTES —
  // symmetric with the agent-runner gate. Unset ⇒ fake-only, fail-closed.
  const routes = (process.env["MC_AGENT_RUNNER_ROUTES"] ?? "").split(",").filter((r) => r !== "");
  if (run.binding !== "fake/fake" && !routes.includes(run.binding)) {
    log(`unsupported homie route ${JSON.stringify(run.binding)}; this runner ships only the fake adapter`);
    return 2;
  }
  return runHomieLoop(run, {
    mcJSON,
    runHarnessTurn,
    sleep: (ms) => Bun.sleep(ms),
    log,
    maxIdlePolls: Number(process.env["MC_HOMIE_MAX_IDLE_POLLS"] ?? DEFAULT_MAX_IDLE_POLLS),
    pollMs: Number(process.env["MC_HOMIE_POLL_MS"] ?? DEFAULT_POLL_MS),
  });
}

if (import.meta.main) {
  process.exit(await main());
}
