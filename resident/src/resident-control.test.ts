import { afterEach, describe, expect, test } from "bun:test";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { execMcVia, type ResidentControlIdentity } from "./resident-control";

const homes: string[] = [];

afterEach(async () => {
  await Promise.all(homes.splice(0).map((path) => rm(path, { recursive: true, force: true })));
});

async function identity(): Promise<ResidentControlIdentity> {
  const mcHome = await mkdtemp(join(tmpdir(), "mc-resident-control-"));
  homes.push(mcHome);
  await writeFile(join(mcHome, "deployment.uuid"), "deployment-test-1\n", { mode: 0o600 });
  return { mcHome, releaseBuildId: "development", configSchemaVersion: 1 };
}

function helloChild(overrides: Record<string, unknown> = {}, waitForAck = true): string {
  const hello = {
    op: "hello",
    seq: 1,
    release_build_id: "development",
    gateway_control_version: 1,
    spine_schema_version: 10,
    config_schema_version: 1,
    deployment_uuid: "deployment-test-1",
    ...overrides,
  };
  const encoded = Buffer.from(JSON.stringify(hello)).toString("base64");
  return `
    require "base64"
    require "json"
    control = IO.new(3, "r+")
    hello = Base64.decode64(${JSON.stringify(encoded)})
    control.write([hello.bytesize].pack("N") + hello)
    exit(0) unless ${waitForAck}
    ack_length = control.read(4).unpack1("N")
    ack = JSON.parse(control.read(ack_length))
    STDOUT.write(JSON.generate({action: "idle", ack: ack.fetch("op")}) + "\\n")
  `;
}

describe("resident one-use dispatch control", () => {
  test("passes only dispatch an inherited full-duplex fd 3 and completes hello/hello_ack", async () => {
    const exec = execMcVia(["/usr/bin/ruby", "-e", helloChild()], await identity());
    for (let tick = 0; tick < 2; tick++) {
      const result = await exec(["dispatch"]);
      expect(result.exitCode).toBe(0);
      expect(JSON.parse(result.stdout)).toEqual({ action: "idle", ack: "hello_ack" });
    }
  });

  test("rejects every identity mismatch before accepting child output", async () => {
    const cases: [string, Record<string, unknown>][] = [
      ["release build", { release_build_id: "other" }],
      ["control protocol", { gateway_control_version: 2 }],
      ["spine schema", { spine_schema_version: 3 }],
      ["config schema", { config_schema_version: 2 }],
      ["deployment", { deployment_uuid: "foreign" }],
    ];
    await Promise.all(cases.map(async ([name, changed]) => {
      const exec = execMcVia(["/usr/bin/ruby", "-e", helloChild(changed, false)], await identity());
      try {
        await exec(["dispatch"]);
        throw new Error(`${name}: mismatch was accepted`);
      } catch (err) {
        expect(`${name}: ${String(err)}`).toContain(`${name}: Error: resident control hello identity mismatch`);
      }
    }));
  });

  test("ordinary mc verbs retain plain stdio and receive no fd 3", async () => {
    const child = `
      const fs = require("node:fs");
      let fd3 = false; try { fd3 = fs.fstatSync(3).isSocket(); } catch {}
      process.stdout.write(JSON.stringify({plain:true, fd3}) + "\\n");
      process.stderr.write("ordinary-stderr\\n");
      process.exitCode = 7;
    `;
    const exec = execMcVia([process.execPath, "-e", child], await identity());
    const result = await exec(["land", "report"]);
    expect(result.exitCode).toBe(7);
    expect(JSON.parse(result.stdout)).toEqual({ plain: true, fd3: false });
    expect(result.stderr).toBe("ordinary-stderr\n");
  });
});
