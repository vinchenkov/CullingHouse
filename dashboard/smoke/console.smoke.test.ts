// console.smoke.test.ts — the one Playwright dashboard smoke (ADR-024 D7):
// real server, real Chromium, real `mc` (test_fake_routing build) against a
// scratch spine — Docker-free. The browser starts a session and sends
// "ping"; the test then plays the runner with a fabricated tier:"homie"
// run.json (claim → reply, the sanctioned fast-lane stand-in for the woken
// container) and asserts the reply renders in the Console.
//
// Run via dashboard/smoke.sh, which builds mc and exports MC_SMOKE_BIN.

import { afterAll, beforeAll, expect, test } from "bun:test";
import { chmodSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium, type Browser, type Page } from "playwright";

import { makeGenRef } from "../src/api";
import { makeMcRunner } from "../src/mc";
import { startDashboardServer, type DashboardServerHandle } from "../src/server";

const MC_BIN = process.env["MC_SMOKE_BIN"] ?? "";

const ROUTING = `# Mission Control routing

| role | harness | binding |
| --- | --- | --- |
| strategist | fake | fake |
| editor | fake | fake |
| worker | fake | fake |
| verifier | fake | fake |
| packager | fake | fake |
| refiner | fake | fake |
| homie | fake | fake |
`;

let scratch: string;
let server: DashboardServerHandle;
let browser: Browser;
let page: Page;
const browserLog: string[] = [];

async function mc(argv: string[], extraEnv: Record<string, string> = {}) {
  const proc = Bun.spawn([MC_BIN, ...argv], {
    env: { ...process.env, ...extraEnv },
    stdin: "ignore",
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr, code] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ]);
  if (code !== 0) {
    throw new Error(`mc ${argv.join(" ")} exited ${code}: ${stderr}`);
  }
  return JSON.parse(stdout) as Record<string, unknown>;
}

beforeAll(async () => {
  if (MC_BIN === "") {
    throw new Error("MC_SMOKE_BIN is unset — run this lane via dashboard/smoke.sh");
  }
  scratch = mkdtempSync("/private/tmp/mc-dash-smoke-");
  const home = join(scratch, "home");
  mkdirSync(home);
  chmodSync(home, 0o700);
  mkdirSync(join(scratch, "ws"));
  const spine = join(home, "spine.db");

  const init = await mc([
    "init", "--spine", spine,
    "--worksource", "ws-smoke",
    "--workspace-root", join(scratch, "ws"),
  ]);
  writeFileSync(join(home, "routing.md"), ROUTING, { mode: 0o600 });
  writeFileSync(join(home, "deployment.uuid"), `${init["deployment_uuid"]}\n`, { mode: 0o600 });

  // The dashboard's mc children inherit this environment (ADR-024 D2).
  process.env["MC_SPINE"] = spine;
  process.env["MC_HOME"] = home;

  server = startDashboardServer(
    { bind: "127.0.0.1:0" },
    { mc: makeMcRunner(MC_BIN), genRef: makeGenRef(), log: () => {} },
  );
  browser = await chromium.launch();
  page = await browser.newPage();
  page.on("console", (msg) => browserLog.push(`console.${msg.type()}: ${msg.text()}`));
  page.on("pageerror", (err) => browserLog.push(`pageerror: ${err.message}`));
  page.on("requestfailed", (req) => browserLog.push(`requestfailed: ${req.url()} ${req.failure()?.errorText}`));
}, 60000);

afterAll(async () => {
  await browser?.close();
  server?.stop();
  if (scratch) {
    rmSync(scratch, { recursive: true, force: true });
  }
});

test("start a session, send ping, see the runner's reply render", async () => {
  await page.goto(server.url);
  await page.locator('[data-testid="new-session"]').click();

  const head = page.locator('[data-testid="conversation-head"]');
  try {
    await head.filter({ hasText: /h-/ }).waitFor({ timeout: 10000 });
  } catch {
    const errorBox = await page.locator('[data-testid="error"]').textContent();
    throw new Error(
      `no session appeared (error box: ${JSON.stringify(errorBox)}; browser log: ${browserLog.join(" | ")})`,
    );
  }
  const sessionID = (await head.textContent())?.trim() ?? "";
  expect(sessionID).toMatch(/^h-[0-9a-f]+$/);

  await page.locator('[data-testid="send-body"]').fill("ping");
  await page.locator('[data-testid="send"]').click();
  await page
    .locator('[data-testid="message"][data-direction="inbound"]', { hasText: "ping" })
    .waitFor({ timeout: 10000 });

  // Play the woken runner: claim the turn, post the reply.
  const runJSON = join(scratch, "run.json");
  writeFileSync(
    runJSON,
    JSON.stringify({ run_id: sessionID, tier: "homie", role: "homie" }),
  );
  let claimed: Record<string, unknown> | null = null;
  for (let i = 0; i < 20 && claimed === null; i++) {
    const res = await mc(["homie", "claim", sessionID], { MC_RUN_JSON: runJSON });
    claimed = (res["message"] as Record<string, unknown> | null) ?? null;
    if (claimed === null) {
      await Bun.sleep(250);
    }
  }
  if (claimed === null) {
    throw new Error("the sent turn never became claimable");
  }
  expect(claimed["body"]).toBe("ping");
  await mc(
    ["homie", "reply", sessionID, "--to", String(claimed["id"]), "--body", "homie ack"],
    { MC_RUN_JSON: runJSON },
  );

  // The Console's tight polling window renders the reply without a reload.
  await page
    .locator('[data-testid="message"][data-direction="reply"]', { hasText: "homie ack" })
    .waitFor({ timeout: 15000 });

  // The session row is listed, and the dashboard outbox drains to empty.
  await page
    .locator(`[data-testid="session-row"][data-session="${sessionID}"]`)
    .waitFor({ timeout: 10000 });
  const drained = await fetch(`${server.url}api/outbox/drain`, { method: "POST" });
  expect(((await drained.json()) as { polled: number }).polled).toBe(0);
}, 120000);
