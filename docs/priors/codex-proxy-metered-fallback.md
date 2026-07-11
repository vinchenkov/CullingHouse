# Prior: codex under a proxy can silently fall back to metered billing (RECONSTRUCTED)

Observed with a LiteLLM-style proxy in front of `codex exec`: the proxy
setup forced a **metered fallback** — the run billed a per-token API key
instead of the ChatGPT-plan subscription.

Consequences already encoded in the spec:
- Inv. 13 assumes subscription billing but adds the practical guard: keep
  metered API keys **out of harness/tool envs** entirely.
- The forbidden-env scan (`CODEX_API_KEY`, `ANTHROPIC_API_KEY`) is a
  never-cut test (handoff Part 3).
- Phase 5 must run `codex exec` through the deployed proxy **without** a
  metered fallback available and confirm the subscription path is used.
