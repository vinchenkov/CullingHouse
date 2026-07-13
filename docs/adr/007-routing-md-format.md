# ADR-007 — Strict routing.md table and test-only fake registry

- Status: Accepted
- Date: 2026-07-12
- Deciders: implementation agent, under spec §9.1/§16 delegated routing format

## Context

The spec makes `MC_HOME/routing.md` authoritative and requires `mc dispatch`
to refuse unresolved bindings and producer/judge correlation, but does not
define the Markdown grammar. Phase 2 also needs the deterministic fake family
without making it a production fallback.

## Decision

`routing.md` is exactly one Markdown pipe table with columns `role`, `harness`,
and `binding`. It contains exactly one row for strategist, editor, worker,
verifier, packager, refiner, and homie; Strategist modes share the strategist
row. Blank lines and `#` heading/comment lines are allowed. Prose, extra
tables/columns, malformed rows, duplicates, missing/unknown roles, unresolved
bindings, harness/binding mismatches, and same-family Strategist↔Editor or
Worker↔Verifier pairs are rejected.

Phase 2 injects the shipped binding registry (`chatgpt→codex`,
`claude→claude-sdk`, `minimax→claude-sdk`). The later config.toml layer
replaces that registry without changing the parser. The fake binding/family is
compiled only under `test_fake_routing`; only that build waives decorrelation
when both sides are fake. Production binaries cannot resolve fake and never
fall back to it.

Dispatch reads only `$MC_HOME/routing.md` (or the spec default home when
MC_HOME is unset). A Spawn resolves its base role before lease claim. The Run
stores the immutable `harness/binding` locator, while the effect and run.json
carry separate `harness` and `model_binding` fields. Missing routing is an
environment error with the `mc onboard routing` repair hint; invalid content
is a domain/config rejection. Non-Spawn reconciliation does not need routing.

## Consequences

- The pure dispatch package stays runtime-blind.
- `mc doctor` must reuse this parser/registry when it lands.
- Custom binding definitions remain deferred to config.toml/onboarding; the
  parser already accepts an injected registry, so no grammar migration is
  needed.
- The fake E2E image and CLI test binary must opt into the build tag and name
  fake explicitly in their routing files.
