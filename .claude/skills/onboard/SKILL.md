---
name: onboard
description: Set up or repair this Mission Control deployment. Runs install.sh (the Level-0 bootstrap) and shepherds the mc onboard wizard through, asking the operator only for genuinely operator-owned inputs. Use when the operator asks to install, set up, onboard, or repair Mission Control, or when any mc doctor check fails.
---

# /onboard — install and shepherd Mission Control setup

You are the shepherding agent (spec §17). The operator's vocabulary is two
actions: run `install.sh`, and talk to you. You speak `mc`; the operator
never does — never instruct them to run an `mc` command, and never paste one
into operator-facing text.

## Procedure

1. Collect the operator-owned inputs conversationally before running
   anything: the deployment home (during development this is the scratch
   path recorded in `OPERATOR-INPUTS.md`, never `~/.mission-control`), the
   first Worksource id and workspace root, and the Daily Console time and
   timezone. Everything else is deterministic — do not ask about it.
2. Run the front door non-interactively from the repo root:

       ./install.sh --dev --yes --mc-home <home> \
         --worksource <id> --workspace-root <path> \
         --console-hour <h> --console-minute <m> --console-tz <tz>

   (Production deployments omit `--dev`; if the script reports the hand-off
   DEFERRED, the warm helper is not provisioned yet — that is the container
   section's Phase 5 work, not an error you can shell around.)
3. The wizard prints one JSON object: sections with status `done` (changed
   state), `ok` (already healthy, skipped), or `deferred` (a later-phase
   probe, named honestly). Report the outcome to the operator in plain
   language; `deferred` is expected during development, not a failure.
4. On any failure, run `mc doctor` (with the same `MC_HOME`/`MC_SPINE`
   environment install.sh used). Every failed check names the onboarding
   section that repairs it. Fix the named input with the operator if one is
   needed, then re-run only that section:

       MC_HOME=<home> MC_SPINE=<home>/spine.db <home>/bin/mc onboard <section> [flags]

   Re-running is safe: onboarding is idempotent and sectioned; healthy
   sections skip themselves.
5. Never load launchd during development (the supervision section defers
   itself; leave it deferred). Never commit, log, or copy values from
   `OPERATOR-INPUTS.md` into tracked files.

## Failure vocabulary

- "spine lost — restore from backup": the deployment identity mirror exists
  but the spine is gone; offer the operator a restore from the newest
  snapshot rather than re-initializing. No onboarding path may ever drop or
  re-init a spine containing data.
- A refused `MC_HOME` inside a git working tree is the §16.1 fence: pick a
  path outside any repository (or one git-ignored in every containing
  worktree).
