// tick-loop.ts — the skeleton resident tick loop, docs/phase1b-contract.md §5.
//
// A plain in-process interval timer. A tick = invoke `mc dispatch` (the one
// caller of Decide() is the verb, never this process — spec §10, Inv. 2/3)
// and effect the returned JSON in order. One tick at a time: a firing that
// lands mid-tick is skipped, never queued. Any failure inside a tick is
// logged and the loop continues (fail-closed; the spine's lease machinery
// is the recovery path).
//
// Must NOT ever (spec §15.1/§10/Inv. 2): compute dispatch decisions, parse
// natural language, open the spine, convert harness exit into state.

import { applyEffect } from "./effects";
import type { Effect, TickDeps } from "./types";

export interface TickHandle {
  /** Clears the timer; resolves once any in-flight tick has finished. */
  stop(): Promise<void>;
}

export function startTickLoop(deps: TickDeps): TickHandle {
  let inFlight: Promise<void> | null = null;
  let stopped = false;

  const timer = deps.setTimer(() => fire(), deps.intervalMs);

  function fire(): Promise<void> | void {
    if (stopped) return;
    if (inFlight) {
      deps.log("tick skipped: previous tick still in flight");
      return;
    }
    const p = tick(deps)
      .catch((err) => {
        deps.log(`tick failed: ${err instanceof Error ? err.message : String(err)}`);
      })
      .finally(() => {
        inFlight = null;
      });
    inFlight = p;
    return p;
  }

  return {
    async stop() {
      if (!stopped) {
        stopped = true;
        deps.clearTimer(timer);
      }
      if (inFlight) await inFlight;
    },
  };
}

/** One tick: `mc dispatch`, then effect the result. Exported for tests. */
export async function tick(deps: TickDeps): Promise<void> {
  if (deps.beforeTick !== undefined) await deps.beforeTick();
  const res = await deps.runMc(["dispatch"]);
  if (res.exitCode !== 0) {
    deps.log(`mc dispatch failed (exit ${res.exitCode}): ${res.stderr.trim()}`);
    return;
  }
  let effect: Effect;
  try {
    effect = JSON.parse(res.stdout) as Effect;
  } catch {
    deps.log(`mc dispatch: unparseable effect JSON: ${res.stdout.trim().slice(0, 200)}`);
    return;
  }
  await applyEffect(effect, deps);
  await deps.tickComplete?.();
}
