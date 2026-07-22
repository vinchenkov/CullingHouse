// main.test.ts — config parsing fails closed on anything malformed.

import { describe, expect, test } from "bun:test";

import { parseConfig } from "./main";

describe("parseConfig", () => {
  test("accepts the minimal and the full shape", () => {
    expect(parseConfig('{"mc_bin":"/x/mc"}')).toEqual({
      mc_bin: "/x/mc",
      bind: undefined,
      auth_token: undefined,
    });
    expect(parseConfig('{"mc_bin":"/x/mc","bind":"127.0.0.1:0","auth_token":"t"}')).toEqual({
      mc_bin: "/x/mc",
      bind: "127.0.0.1:0",
      auth_token: "t",
    });
  });

  test("refuses non-objects, a missing mc_bin, and mistyped fields", () => {
    expect(() => parseConfig("nope")).toThrow("JSON object");
    expect(() => parseConfig("[1]")).toThrow("JSON object");
    expect(() => parseConfig("{}")).toThrow("mc_bin");
    expect(() => parseConfig('{"mc_bin":""}')).toThrow("mc_bin");
    expect(() => parseConfig('{"mc_bin":"/x","bind":9}')).toThrow("bind");
    expect(() => parseConfig('{"mc_bin":"/x","auth_token":9}')).toThrow("auth_token");
  });
});
