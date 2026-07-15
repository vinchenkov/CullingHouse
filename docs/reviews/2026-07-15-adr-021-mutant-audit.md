# ADR-021 jurisdiction planted-mutant audit — 2026-07-15

## Scope and method

This is ADR-021 TDD step 8. Each named mutant was applied one at a time to a
throwaway copy under `/private/tmp`, then its narrow witness was run uncached
with Go 1.24.5 on macOS/arm64. No mutant touched the working tree and no mutant
was committed.

The audit asks whether the asserted property has an exercised discriminator,
not whether a test has a promising name. It found two surviving mutations;
both witnesses were added before the final run:

1. A separator-aware canonical-prefix gate survived the symlink fixture because
   `ResolveSource` canonicalized the alias to the protected spelling. A regular-
   file hardlink now supplies two unrelated canonical strings for one inode.
2. D8 split the ancestor rule into present-root, below-anchor suffix, and
   above-canonical-anchor arms. The first two were covered; removing the third
   survived. The absent denied-path fixture now probes a source strictly above
   its canonical anchor.

## Named ADR-021 mutants

| Mutant applied | Exercised witness | Observed failure |
|---|---|---|
| Replace identity with a separator-aware canonical-prefix gate | `TestUnionMatchesHardlinkIdentityAcrossUnrelatedSpellings` | hardlink alias was permitted (`code = ""`, wanted `mount.denied_root`) |
| Remove the present-root ancestor arm | `TestUnionIsBidirectional/ancestor`; `TestAncestorOfAProtectedRootTheFloorCannotSee` | parent and `~/Library` were both permitted |
| Remove the absent root's above-canonical-anchor arm | `TestAbsentDeniedPathRejectsCanonicalParentAndKeepsSiblingDecidable` | source above the stored anchor was permitted |
| Replace computed `broad_root` with literal `HOME`, `/Users`, `/` | `TestBroadRootIsComputedNotHardcoded` | `/private/var` was permitted and unrelated `/Users` was rejected |
| Compute `broad_root` from `filepath.Dir` only | `TestBroadRootRejectsFirmlinkAliasRoutes` | `/System/Volumes/Data`, `/System/Volumes`, and `/System` were all permitted |
| Move jurisdiction after `ResolveAccess` | `TestAuthorizeOrdering/jurisdiction_beats_ResolveAccess` | returned `mount.rw_not_permitted`, masking required `mount.denied_root` |
| Let an own-control exemption return success and subtract later `denied_paths` | `TestOwnControlAncestorExemptionIsExactAndAssociated/denied_paths_remains_additive` | explicitly denied own workspace was permitted |
| Ignore `claim.Kind` and accept a match under any typed registry entry | `TestClaimKindIsNotInert` | every other real typed kind permitted the own-session-only source |
| Add `perm & 0o077 == 0` to HOME validation | `TestValidateHomeAcceptsTheRealOperatorHome`; `TestValidateHomeAcceptsEveryOrdinaryHomeMode` | real 0750 HOME plus 0750/0755 fixtures were rejected |
| Route HOME through `TrustHomeDir` | `TestValidateHomeAcceptsTheRealOperatorHome` | real HOME was rejected because mode 0750 grants group bits |

The deny-all/permit-all controls are part of these fixtures: the union tests
pair denial with unrelated permits, the ordering test proves ordinary RW-on-RO
still returns `mount.rw_not_permitted`, the typed test first proves the correct
kind permits, and the HOME tests begin with real/ordinary permit cases.

## D8/D9/D11 supplementary mutation coverage

Step 7 introduced additional live paths after ADR-021's original named list.
Their direct discriminators are:

| Plausible regression | Witness |
|---|---|
| Skip nil-Info union members or one constructor collection | `TestEveryAbsentUnionCollectionGetsCanonicalPair` |
| Reverse or reconstruct the original suffix from the canonical target | `TestResolveAbsentProtectedIDRetainsOriginalSuffixAcrossAliasDepth` |
| Canonicalize the full missing declaration instead of the nearest existing original prefix | same alias-depth test (exact one-call argument asserted) |
| Peel ENOTDIR/EACCES/EIO as if ENOENT | `TestResolveAbsentProtectedIDPeelsOnlyENOENT`; public dangling/middle-file construction test |
| Remove shorter-source or longer-source suffix overlap | public absent denied-path parent/exact/descendant matrix |
| Treat an identity-walk error as unrelated | `TestRemainderBelowIdentityLookupErrorIsAmbiguity` |
| Use string/component prefixes | `TestAbsentSuffixOverlapIsComponentAndVolumeAware`; public near-prefix permit |
| Immediately deny an unknown-mode case variant even when a later component proves non-overlap | `later_mismatch_resolves_an_earlier_unknown_case_variant` |
| Cache an ordinary verdict or resolved raw deny input | `TestJurisdictionReconstructionTracksAbsentAliasRetarget` |
| Cache a typed verdict or pre-resolved typed registry | `TestTypedRootsReconstructionTracksSelectorRetarget` |

## Verdict

Every mutant named by ADR-021 now dies under an exercised witness. The two gaps
found by this audit are closed. The jurisdiction slice satisfies ADR-021's
eight-step TDD acceptance order; production construction/recheck call sites
remain later Phase-3 wiring because none exists yet.
