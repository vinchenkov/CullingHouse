# S9 — ADR-022 credential-projection spikes (pre-implementation)

Two unknowns gate the ADR-022 token-service design (`docs/adr/022-…md`,
Residual risks). Each may simplify a projection writer; neither blocks the
D3/D4 contracts already proven live in `docs/priors/oauth-poc-result-v2.md`.

Installed under test: Claude Code **2.1.217** (POC was 2.1.216 — drift noted),
Codex CLI **0.144.6** (unchanged).

## Q1 — `CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST`

Does the installed bundle implement this mode, and does it (a) suppress
credential disk writes and the `invalid_grant` compare-and-swap wipe,
(b) suppress the 401→disk-reread/self-refresh path, (c) change required
`.credentials.json` fields? If yes to (a)+(b), the D3 writer drops the
dummy-refresh/future-`expiresAt` dance and the refresh-ahead loop loses its
fail-closed wipe hazard.

Method: static inspection of the installed bundle, then a live mock-provider
run (synthetic tokens only, scratch `CLAUDE_CONFIG_DIR`, real credential
stores untouched — same harness discipline as the POC).

## Q2 — real-token Codex startup refresh

Does a *real* freshly-minted access token (signed JWT, ~10-day exp) make
Codex skip its proactive startup refresh? Source evidence wanted: what gates
the refresh — JWT `exp` parse (signature-checked?), `last_refresh` age, or
unconditional. Prior S3 finding (0.144.1): access-token expiry triggers the
refresh; aging `last_refresh` alone does not. POC v2 (0.144.6) saw a startup
refresh with *synthetic* tokens — reconcile the two.

If a fresh real token skips the refresh, the D4 broker becomes optional
(still wanted as the recovery path); if not, the broker stays mandatory.

Method: Codex is open-source Rust (`openai/codex`); read the auth/token
module at 0.144.6. Live confirmation would need a real token and is NOT run
without operator involvement — source verdict + POC evidence suffice for the
writer design.

## Out of scope

VirtioFS `mtime` propagation for long Claude sessions (ADR-022 residual #3)
belongs to the Docker acceptance lane, not this host-side spike.

Result: `RESULT.md`.
