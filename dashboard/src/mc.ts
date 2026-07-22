// mc.ts — the single seam through which the dashboard reaches the spine
// (ADR-024 D2). Every read and write is a spawned invocation of the real
// `mc` binary — one JSON object on stdout, exit 0/1/2 — with the process
// environment passed through untouched, so the production darwin binary
// self-delegates into the warm helper container and the test_fake_routing
// build honors MC_SPINE/MC_HELPER (Inv. 15/24). No other file in this
// package may reach the spine, and no SQLite driver may ever appear here.

export interface McResult {
  code: number;
  /** Parsed stdout when it is a single JSON object; null otherwise. */
  json: Record<string, unknown> | null;
  stderr: string;
}

/** Injectable seam: tests substitute a scripted fake (ADR-024 D7). */
export interface McRunner {
  run(argv: string[]): Promise<McResult>;
}

export function makeMcRunner(mcBin: string): McRunner {
  return {
    async run(argv: string[]): Promise<McResult> {
      const proc = Bun.spawn([mcBin, ...argv], {
        stdin: "ignore",
        stdout: "pipe",
        stderr: "pipe",
      });
      const [stdout, stderr, code] = await Promise.all([
        new Response(proc.stdout).text(),
        new Response(proc.stderr).text(),
        proc.exited,
      ]);
      let json: Record<string, unknown> | null = null;
      try {
        const parsed: unknown = JSON.parse(stdout);
        if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
          json = parsed as Record<string, unknown>;
        }
      } catch {
        json = null;
      }
      return { code, json, stderr };
    },
  };
}
