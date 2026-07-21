# Phase 3 decision memo: egress gateway and forbidden-env

## 1. What each feature is

**Egress gateway (three modes).** The thing that lets a container talk to the outside world at all, under policy. `none` allows only the selected provider's endpoints; `allowlist` adds named domains; `open+audit` allows public destinations and logs them. It also injects the MiniMax bearer token so no credential ever sits inside a container. Without it, every container in the tree runs with networking fully off — all six launch sites pass `--network none`, and the Docker tests actively assert that. **No agent can reach any model provider. The system has never carried real traffic.**

**Forbidden-env builder.** The guard from Inv. 13: prove no metered-fallback key (`CODEX_API_KEY`, `ANTHROPIC_API_KEY`) can reach a harness or tool environment, and fail `mc doctor` if one appears in a profile. Without it, nothing prevents a silent metered bill — though today nothing *causes* one either (see below).

## 2. What already exists

**Gateway: nothing that runs.** Everything that *decides* is built — the policy columns, the deny-by-default onboarding refusal, the mount classification for a CA certificate, the refusal codes. Everything that *executes* is absent: no HTTP server, no TLS, no CA, no credential store, no Docker network creation, no guard container. The resident is 5,776 lines of zero-dependency Bun that has never opened a socket. Spike 4 proved the mechanism works end-to-end (green, 14/15), but with throwaway Python and a network shape ADR-018 explicitly rejects.

**Forbidden-env: also nothing** — but the surrounding machinery is finished. Both policy columns exist since v1 (no migration). Both refusal codes are classified, routed, and consequence-tested; they simply have no producer. The container env is already effectively empty, so the safety property holds *by accident*, untested.

## 3. Size

Anchors: small ≈ 1 sitting; medium ≈ a new closed carrier across an existing seam; large ≈ a new container class with its own lifecycle.

**Gateway: very large.** Fifteen work units. Two of them (the MITM HTTP/2+WebSocket terminator, and the `mc-netguard` nftables container) each individually exceed the "large" anchor. It is comparable to several phases of work, not several sittings.

**Forbidden-env: medium, ~2–3 sittings.** Roughly one-tenth the gateway. The pure scanner is one sitting and copies an existing in-tree pattern. Cost concentrates in extending the frozen plan carrier and changing the runner's tested env contract.

## 4. What deferring costs

**Without the gateway:** Phase 4's six E2E control-loop families can still run — they exercise scheduling, refusal routing, sealing, and orphan recovery, none of which need egress. Phase 5 is **impossible**: real-subscription acceptance against real model APIs requires network. Worse, the topology work later *inverts* currently-green assertions (`NetworkMode == "none"`, "no non-loopback interfaces"), reopening landing-envelope, orphan-sweep, and launch-fencing tests.

**Without forbidden-env:** Phase 4 is unaffected — exposure is genuinely nil today. Phase 5 is where it bites: real keys, real billing, and no fence. Also, the gateway must deliver a CA env var into containers, so it will build the carrier+resident env plumbing anyway — deferring means reopening the frozen carrier twice instead of once.

## 5. Recommendation

**Split. Build forbidden-env's cheap core now (scanner + profile read + doctor finding — one sitting, no carrier, no resident, no runner). Formally defer the gateway to its own phase, and spend the remaining time resolving one paper question: which process and language hosts the gateway.**

Reasoning: the one-sitting slice fully discharges what the spec itself asks (`mc doctor` fails on a forbidden key) at near-zero risk, and buys a regression fence before the gateway adds its first `-e` flag. The gateway is not a missing row on a working system; it is a subsystem, and rushing it produces a shape ADR-018 already rejected.

**Strongest argument against:** deferring means Phase 3 — the *boundary conformance* phase — ships a boundary that has never carried traffic, and the acceptance rows that would have caught that are precisely the deferred ones. That is a real integrity cost, not a scheduling one.

## 6. Unknowns needing prototype

1. **Gateway host process/language** — blocks honest sizing of five units; Bun cannot hold locked memory. Paper decision, currently nobody's.
2. **nftables in Docker Desktop's arm64 VM** — the design anticipates the platform may refuse.
3. **Is the tool env plane realizable?** Tools run in-process under the harness; there may be no boundary to strip at. Half-sitting spike settles it.

- Decide: build now / defer / split.
- If split: approve the one-sitting forbidden-env core.
- Assign the gateway host-language question to someone; it gates every other estimate.