# Prior: claude-sdk sharp edges (RECONSTRUCTED)

Two observed failure modes in the Claude Agent SDK, to be regression-tested
in Phase 5 (handoff Part 3, Phase 5 "real-harness sharp-edge regressions"):

1. **`options.env` is a replacement, not a merge.** Passing `env` replaces
   the process environment the harness sees; anything not explicitly
   included (e.g. CA vars, PATH entries) is gone.
2. **`settingSources: []` breaks subagent spawn.** With setting sources
   emptied, spawning subagents fails — the subagent-spawn path depends on
   settings that no longer resolve.

Related: the runtime caps subagent fan-out at **depth 1** — children cannot
spawn further (this is load-bearing for spec Inv. 14, which the spec treats
as enforced "by construction").
