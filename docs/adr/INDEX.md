# ADR INDEX — navigational only

**This index is not normative. It never states a rule.** It tells you which
ADR answers which question and which line to jump to. The ADR body is the
authority; these ADRs encode fail-closed security (mount authorization,
network guard, jurisdiction), so implementing against a summary instead of
the text is how a fence goes missing.

**How to use it (AGENTS.md §1.4):** when `NEXT:` names an ADR, read its row
here, jump to the named Decision by line, and read outward from there. Do not
read a whole large ADR to find one Decision. Do not read ADRs `NEXT:` did not
name.

Line numbers drift as ADRs are amended. They are a starting offset, not a
citation — if the heading is not there, grep the heading text.

## Live ADRs

| # | Answers the question | Size |
|---|---|---|
| 001 | Which CLI verbs exist for pipeline roles beyond `mc complete`, and which caller scope may invoke each? (Open Question 1 resolved by 020) | 11 KB |
| 007 | What Markdown grammar does `routing.md` have, and how is the fake binding family kept out of production? | 2 KB |
| 008 | Who assembles an agent's opening brief, when does it freeze, and what per-role state does the carrier hold? | 3 KB |
| 009 | What contract governs the `mc homie start\|bind\|list` verbs the spec names but leaves unpinned? | 4 KB |
| 010 | How are `mc homie send/history/end` specified where the spec leaves grammar and transaction shape open? | 3 KB |
| 011 | How does the native delivery surface read and acknowledge outbox rows per surface, and who may do it? | 2 KB |
| 012 | How does the Homie runner register its session's native locators, and what does `mc homie resume` do? | 4 KB |
| 013 | How does a Homie runner take an inbound turn and submit its reply through `mc`? (resolves a slice 010 deferred) | 5 KB |
| 014 | How do `doctor`, `backup`, and `reset` behave at the Phase-2 (pre-Docker) tier? | 4 KB |
| 015 | What does `mc onboard` dispatch, provision, and stage at the Phase-2 tier? | 7 KB |
| 016 | How does one dispatch command span the Darwin/Linux boundary to plan, validate, and commit an action? | 62 KB |
| 017 | Which host paths may be mounted, under whose consent, and where do they land in the container? | 85 KB |
| 018 | ~~How is agent egress confined and mediated per launch~~ **SUPERSEDED by 022** — do not implement | 33 KB |
| 019 | How are container resource envelopes and per-class security postures configured and enforced? | 11 KB |
| 020 | How does the Editor's holistic wave review get a durable state, a dispatch step, and a terminal? | 49 KB |
| 021 | Which mount sources does jurisdiction reject, and how does that check get built, placed, and reported? | 71 KB |
| 022 | Free-internet credential projection: what replaces the egress gateway, and how does each runtime get a host-managed token? | 7 KB |
| 023 | How does an initiative get its one shared branch, how do children commit without landing individually, and how does the arc merge to main? (extends 017 D6's parked initiative path) | 6 KB |
| 024 | What stack, spine read path, and API shape does the dashboard use, and how does the S6 Console slice get built and smoke-tested? | 7 KB |
| 025 | How do production real-harness initiatives get their cut, per-child mount rows, arc verification, and landing import? (the follow-on ADR 017 D6 and 023 D6 required) | 13 KB |

**Relations.** 023 extends 017 D6's Parked initiative/child mount path; it
resolves the branch grammar, the branchless child terminal, and the arc
landing lane for the fake-harness control loop, and keeps the production
real-harness shared-worktree mount rows owed for Phase 5. **025 is that owed
follow-on**: it extends 017 D6's closed table in place (worktree-name grammar
`mc-task-<id> | mc-initiative-<id>`), discharges 023's deferred promotion-time
cut, and amends 023 D5's production sentence and 017:425-429's
identical-topology wording for initiative rows. 021 resolves gaps 017 left open — **017 remains the authority
and is not superseded**; read both. 020 resolves 001's Open Question 1 and
`docs/phase2-contract.md` A-P2-7. 013 resolves a slice 010 deferred. **022
supersedes 018 whole** (operator's free-internet requirement): the egress
gateway/proxy is deleted as a network control and replaced by a resident-hosted
credential token service. 022 also amends spec §11.4 and Inv. 16/23 — the
amendment text in the spec is the authority; 022 is the design record.

## Dead stubs — do not read

**002, 003, 004, 005, 006** are unwritten placeholders: "written only if
triggered (red spike / delegated design due)". Every spike S1–S5 went green,
no fallback was needed, and no fallback ADR was signed. Their Context,
Decision, and Consequences headings are empty. They are reserved slots, not
decisions. Nothing points at them.

## Decision maps — the five large ADRs

Jump to the line. Topics are subjects, not rules.

### 016 — cross-boundary prepare/attest/commit dispatch (62 KB)
| L | Decision | Governs |
|---|---|---|
| 25 | D1 — one external command, private same-binary composition | how the external command composes private prepare/attest/commit |
| 105 | D2 — complete, replay-safe tokens and bounded schemas | token/identity allocation and handshake frame schemas |
| 275 | D3 — exact one-action ordering | registry launch fencing and single-action selection |
| 542 | D4 — exact refusal consequences and stable codes | refusal classification and the stable code taxonomy |
| 593 | D5 — path evidence, preclaim proof, and honest residuals | host path/trust evidence and preclaim proof obligations |
| 698 | D6 — immutable effects and post-commit failures | commit-time effects, plan contents, post-commit failure |
| 817 | D7 — names, labels, and exact liveness | naming, labeling, identity/liveness grammar |

### 017 — mount authorization and deterministic container paths (85 KB)
| L | Decision | Governs |
|---|---|---|
| 10 | **Amendment 2026-07-14** (view discipline, privileged-tree gate) | in-place correction of ownership/view claims made below — read before D3/D5 |
| 135 | D1 — strict allowlist grammar | format and parsing of the operator-owned allowlist |
| 204 | D2 — shipped blocked floor | the extend-only floor of never-mountable path components |
| 237 | D3 — canonical, identity-based containment | canonicalization and identity-checking of a requested source |
| 289 | D4 — bilateral access and typed system mounts | bilateral request/allow resolution; the typed system-mount set |
| 364 | D5 — protected set and jurisdiction | the non-subtractable protected set; cross-Worksource jurisdiction |
| 611 | D6 — deterministic destinations | collision-free derivation of container-side destinations |
| 834 | D7 — Inv. 26 historical trace access | container access to run session dirs and historical paths |
| 912 | D8 — per-session attachment file plane | the per-session two-direction attachment tree and its mounts |

### 018 — per-launch network guard and egress gateway (33 KB)
| L | Decision | Governs |
|---|---|---|
| 26 | D1 — one guarded namespace per pipeline run or Homie | guard topology, bootstrap sequence, per-uid egress |
| 96 | D2 — DNS and canonical raw-rule storage | DNS resolution and raw allow-rule storage forms |
| 136 | D3 — exact destination floor | the non-removable forbidden-destination floor; address normalization |
| 162 | D4 — the native HTTP gateway is parsed, loopback-only | gateway binding, HTTP modes, interception, tunnel posture |
| 208 | D5 — exact credential injection scope | credential ownership and injection scoping |
| 233 | D6 — one-use resident control, launch credentials | resident/broker control channel, attestation, registration |
| 374 | D7 — audit ownership | who writes egress audit evidence, and where |
| 392 | D8 — mandatory preclaim proof and failure classes | preclaim probe topology, cleanup, failure classification |
| 505 | D9 — guard loss and exact reconciliation | behavior and reconciliation ordering when the guard dies |

### 020 — initiative wave plan review (49 KB)
| L | Decision | Governs |
|---|---|---|
| 145 | D1 — the carrier: `tasks.plan_reviewed` | the schema carrier for wave-child review state |
| 222 | D2 — dispatch: the gate and the new arm | dispatch visibility, the child gate, the role map split |
| 313 | D3 — the role: `editor(plan-review)` | the reviewing role, its mode mechanism, its frozen directive |
| 350 | D4 — the brief gains a wave | what the review's claim-transaction brief carries and suppresses |
| 434 | D5 — the terminal: `mc editor plan-review` | the terminal verb, its provenance/fencing checks, its arms |
| 578 | D6 — the wave is uniform in `plan_reviewed` | why one wave never splits across review states |
| 594 | D7 — crash recovery: one row on §10's table | what the next tick sees after a reaped review run |

### 021 — mount jurisdiction (71 KB)
| L | Decision | Governs |
|---|---|---|
| 189 | D1 — the seam: an injected `Jurisdiction` value | shape and injection of the jurisdiction seam and its inputs |
| 347 | D2 — non-subtractability | how the protected set is constructed; why it cannot be subtracted |
| 370 | D3 — `MC_HOME` is protected whole | scope of MC_HOME protection; status of the class enumeration |
| 423 | D4 — bidirectional by identity; `broad_root` | directionality of identity containment; HOME's broad root |
| 546 | D5 — HOME is injected, not ambient | how HOME reaches the boundary package and is validated |
| 613 | D6 — where step 5 sits | placement of the check within the authorization order |
| 645 | D7 — `cross_worksource` vs `denied_root` | which rejection code covers which members, and precedence |
| 678 | D8 — existence: absent members are members | protected members that do not yet exist on disk |
| 752 | D9 — the verdict is never cached on source identity | caching posture for the jurisdiction verdict |
| 771 | D10 — typed grants are confined per class | the second predicate governing typed system mounts by kind |
| 993 | D11 — `ResolveJurisdiction` re-runs | caching posture for the resolved jurisdiction input |
