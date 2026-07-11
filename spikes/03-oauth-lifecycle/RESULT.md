# S3 — OAuth lifecycle in bind-mounted control dirs — RESULT

Date: 2026-07-10 · Image: `mcspike-08-base:latest` (claude-code 2.1.207, codex 0.144.1) ·
Canonical dirs: `~/.mc-dev-home/cred/codex` (CODEX_HOME), `~/.mc-dev-home/cred/claude`
(CLAUDE_CONFIG_DIR). `~/.claude` / `~/.codex` were never mounted or modified.

**Verdict: GREEN — the `materialized` posture (spec §11.4) works as designed for both
OAuth bindings. No fallback ADR trigger fired.**

## Assertion table

| # | Assertion | Result | Evidence |
|---|-----------|--------|----------|
| 1a | Directory bind mount stays fresh through atomic-replace rotation, both directions (host→container, container→host) | PASS | `out/probe1.log` |
| 1b | A file-level bind mount cannot accept a refresh: in-container `rename(2)` onto the mount point fails EBUSY | PASS | `out/probe1.log` |
| 1c | (Recorded, not asserted) On Docker Desktop VirtioFS a *host-side* atomic replace does propagate through a file mount (path-based share, contra classic bind semantics) — irrelevant in practice because the refresh direction (1b) fails outright | NOTE | `out/probe1.log` |
| 2a | Codex: forced in-container refresh rotates tokens **into the canonical host dir** (refresh_token rotated, `last_refresh` + access exp advanced, same-dir atomic write) | PASS | `out/probe2.log` snaps `aged+expired` vs `post-run1` |
| 2b | Codex: a second container run starts cleanly from the rotated tokens, no further rotation | PASS | `out/probe2.log` snap `post-run2` identical to `post-run1` |
| 2c | Codex refresh trigger identified: access-token expiry. Aging `last_refresh` alone does NOT trigger a refresh in 0.144.1 | PASS (finding) | comment + snaps in `probes/probe2-codex.sh` |
| 3a | Claude: forced in-container refresh (rewind `expiresAt` in `.credentials.json`) rotates the credential into the canonical host dir via atomic replace (new inode observed host-side) | PASS | `out/probe3.log` snaps |
| 3b | Claude refresh trigger identified: proactive local `expiresAt` check before the turn; no token tampering needed | PASS (finding) | `probes/probe3-claude.sh` |
| 3c | Claude: second run starts cleanly from rotated credential | PASS | `out/probe3.log` snap `post-run2` |
| 4a | Concurrent-refresh race (two containers, one shared auth dir — scratch copy): **no corruption** — auth.json valid JSON after, atomic-rename writes give last-writer-wins | PASS | `out/probe4.log` |
| 4b | Race: both racing turns **succeeded** (exit 0, both replied) — no 401, no failed run | PASS | `out/probe4-racerA.log`, `out/probe4-racerB.log` |
| 5 | `claude setup-token` evaluated from CLI help (not minted) | DONE | `out/probe5.log`, evaluation below |
| 6 | Docker Desktop restart mid-refresh | DEFERRED | serialized leg (must not restart Docker in this run) |
| 7 | Forbidden-env guard: no ANTHROPIC_API_KEY / CODEX_API_KEY / OPENAI_API_KEY in any probe container (Inv. 13) | PASS | guard block executed at top of every containerized run |
| 8 | Host login state intact after all probes | PASS | `codex login status` → "Logged in using ChatGPT"; host `claude auth status` → "Claude Max account" (Keychain untouched) |

## Findings

1. **Mount the directory, never the file — confirmed, with a sharper reason than the
   spec's.** Classic staleness did not even get a chance to manifest: on VirtioFS an
   in-container atomic replace *onto a file mount point* fails immediately with EBUSY,
   i.e. a file-level mount cannot accept a token refresh at all. Directory mounts work
   in both directions.

2. **Codex refresh semantics (0.144.1):** trigger is token expiry (server-side
   rejection of the expired/invalid access token), not `last_refresh` age. On refresh
   codex rotates the refresh token and atomically rewrites `auth.json` in CODEX_HOME.
   **Do not use token prefixes to detect rotation** — in probe 4 the rotated
   refresh token kept the same first 8 chars (`rt.1.AAC`) while the full value changed
   (verified by full-value sha256). Hash the value.

3. **Claude refresh semantics (2.1.207):** the CLI checks `.credentials.json`'s
   `expiresAt` locally and proactively refreshes with `refreshToken` before the turn.
   Refresh writes by atomic replace (inode changes) into `CLAUDE_CONFIG_DIR`; the
   rotated file lands host-side through the directory mount and the next run starts
   from it. `refreshTokenExpiresAt` advanced on refresh too (~12 days out) — the
   canonical dir stays healthy as long as it refreshes at least that often.

4. **Race outcome — ADR-004 "serialize refreshes" fallback NOT triggered.** Two
   containers sharing one auth dir, both starting on an expired access token, both
   completed their turns and the file was never torn. Ambiguity to be honest about:
   we cannot distinguish "server tolerated near-simultaneous reuse of the same
   refresh token" from "container startup jitter accidentally serialized them and
   the loser picked up the winner's fresh token from the shared file". Either way the
   observable behavior matches the spec's accepted v1 caveat (Homie + pipeline run
   sharing a binding). Serializing refreshes remains cheap prophylaxis but is not
   evidence-required.

5. **Canonical-codex refresh-token caveat (operator note, ledger-worthy).** Probe 4's
   racers refreshed using the refresh token *shared* with the canonical copy. The
   canonical dir was never written (its access token is valid until
   2026-07-21T04:27Z), but if OpenAI rotation is strictly single-use its refresh
   token may now be consumed. The newest rotation is preserved host-side at
   `~/.mc-dev-home/spike03/race-codex/auth.json` (outside the repo). If a future
   canonical refresh fails with an auth error: install that file into
   `~/.mc-dev-home/cred/codex/auth.json` (atomic replace) or re-login. A cheap
   auth-health probe (`codex login status` against the dir, or one tiny turn) before
   relying on refresh is exactly the `mc doctor` shape the spec prescribes.

6. **`claude setup-token` evaluation (not minted — interactive).** Help output:
   "Set up a long-lived authentication token (requires Claude subscription)". Fit as
   the S3 mitigation: it would convert the `claude` binding from OAuth-lifecycle
   (`materialized`) to a **static-token** binding — the token is a bearer the CLI
   accepts via `CLAUDE_CODE_OAUTH_TOKEN`, so it could ride the gateway's
   header-injection rail like `minimax`, making Inv. 16 a *fact* for claude
   (credential never enters the container). Costs: interactive mint (operator-present
   step at onboarding), long-lived static secret with no self-rotation (expiry ~1 yr
   class, revoke-and-remint lifecycle), subscription-scoped, and feature parity with
   full OAuth is unverified. Since probes 2–4 show the materialized posture working
   cleanly, setup-token stays a **fallback candidate for the S3 ADR slot, not the
   shipped path**. Note `claude auth status --json|--text` exists and reads local
   credential state — a free per-binding auth-health probe for `mc doctor`.

7. **Keychain quirk handled by construction** (prior `docs/priors/claude-macos-keychain-quirk.md`,
   not re-derived): the container credential home is its own canonical dir; the
   operator's host Keychain login was observed intact after all probes (assertion 8).

## Rerun instructions

```sh
spikes/03-oauth-lifecycle/check.sh                # free legs only (probe 1 + 5 + guards)
spikes/03-oauth-lifecycle/check.sh --live-codex   # + probe 2 (2 tiny codex turns, canonical dir)
spikes/03-oauth-lifecycle/check.sh --live-claude  # + probe 3 (2 tiny claude turns, canonical dir)
spikes/03-oauth-lifecycle/check.sh --race         # + probe 4 (2 tiny codex turns, scratch copy;
                                                  #   consumes the shared refresh token — see finding 5)
spikes/03-oauth-lifecycle/check.sh --all-live
```

Every live probe stashes a timestamped backup of the credential file under
`~/.mc-dev-home/spike03/` before touching anything. No secret values are ever
printed (8-char prefixes max); logs under `out/` are grep-guarded by check.sh.

## Artifacts

- `probes/probe1-inode.sh` … `probes/probe5-setup-token.sh` — the five probes
- `check.sh` — machine-rerunnable check (flags gate the live legs)
- `out/probe1.log` … `out/probe5.log` (+ per-run logs) — captured evidence
