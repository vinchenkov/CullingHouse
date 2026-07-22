// main.ts — dashboard entry (ADR-024). Wiring only: reads --config <path>
// (JSON: { "mc_bin": "...", "bind"?: "host:port", "auth_token"?: "..." }),
// builds the real mc seam, and serves. Stateless by design (Inv. 23): on
// death the init system restarts it, it re-reads, and the browser
// reconnects. LaunchAgent generation belongs to install/onboard, not here.

import { makeGenRef } from "./api";
import { makeMcRunner } from "./mc";
import { DEFAULT_BIND, startDashboardServer } from "./server";

export interface DashboardConfig {
  mc_bin: string;
  bind?: string;
  auth_token?: string;
}

export function parseConfig(raw: string): DashboardConfig {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("dashboard config must be a JSON object");
  }
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("dashboard config must be a JSON object");
  }
  const cfg = parsed as Record<string, unknown>;
  if (typeof cfg["mc_bin"] !== "string" || cfg["mc_bin"] === "") {
    throw new Error('dashboard config requires "mc_bin"');
  }
  if (cfg["bind"] !== undefined && typeof cfg["bind"] !== "string") {
    throw new Error('dashboard config "bind" must be a string');
  }
  if (cfg["auth_token"] !== undefined && typeof cfg["auth_token"] !== "string") {
    throw new Error('dashboard config "auth_token" must be a string');
  }
  return {
    mc_bin: cfg["mc_bin"],
    bind: cfg["bind"] as string | undefined,
    auth_token: cfg["auth_token"] as string | undefined,
  };
}

if (import.meta.main) {
  const flag = process.argv.indexOf("--config");
  const configPath = flag === -1 ? undefined : process.argv[flag + 1];
  if (configPath === undefined) {
    console.error("usage: bun src/main.ts --config <path>");
    process.exit(2);
  }
  const cfg = parseConfig(await Bun.file(configPath).text());
  const server = startDashboardServer(
    { bind: cfg.bind ?? DEFAULT_BIND, authToken: cfg.auth_token },
    { mc: makeMcRunner(cfg.mc_bin), genRef: makeGenRef(), log: (l) => console.error(l) },
  );
  console.error(`dashboard serving on ${server.url}`);
}
