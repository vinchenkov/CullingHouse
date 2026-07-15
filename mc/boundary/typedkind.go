package boundary

// TypedKind names what a typed system mount source IS. It is DERIVED from
// ADR-017 Decision 6's destination table (:634-702) — including the setup and
// landing tables at :686-702, which ADR-017:380-381 sweeps in ("except each
// exact typed own-source grant in Decisions 4, 6, and 8") and which :686-687
// keeps strictly separate from the agent table.
//
// ADR-017:396-401's four items ("own session, derived own state, selected
// runtime-control directory, or generated projection") are ILLUSTRATIVE, not the
// domain. Reading them as the domain is what killed the c120c5c draft; a
// nine-item list copied from them is what the pre-TDD probe caught one rework
// later, in the same document. typedkind_internal_test.go parses the real table
// out of the real ADR so that a third recurrence fails a test rather than a
// container launch.
//
// The domain is closed. A kind with no authorized root in Jurisdiction.TypedRoots
// denies (ADR-021 D10, fail-closed).
type TypedKind uint8

const (
	// KindNone is the zero value and means NO CLAIM: an ordinary or
	// profile-requested mount, which takes the union predicate (ADR-021 D10).
	// Authorize always passes this.
	KindNone TypedKind = iota

	// Agent task/workspace rows (ADR-017:636-650). Cover kinds are deliberately
	// destination-specific: sharing one semantic "inert cover" kind across rows
	// made roots for seventeen agent/setup/landing destinations interchangeable.
	KindTaskRoot
	KindTaskSource
	KindWorkspaceSourceGitCover
	KindWorkspaceSourceMissionControlCover
	KindTaskGit
	KindTaskGitConfigCover
	KindTaskGitHooksCover
	KindTaskGitInfoCover
	KindTaskGitObjectsInfoCover
	KindSealedPack
	KindTaskGitPackedRefsCover
	KindTaskGitShallowCover
	KindTaskGitWorktreeCommondirCover
	KindTaskGitWorktreeGitdirCover
	KindTaskGitWorktreeConfigCover

	// The several source arms of /workspace/source share that destination row,
	// but no kind crosses into the seeding row.
	KindWorkspaceCommittedProjection
	KindExecutionProjection
	KindWorkspaceRegisteredRoot

	// Seeding rows (:653-654).
	KindSeedingCommittedProjection
	KindSeedingRegisteredRoot
	KindSeedingSourceGitCover

	// Operator/Homie workspace rows (:655-660).
	KindOperatorWorksource
	KindOperatorSourceGitCover
	KindOperatorRegisteredControlCover
	KindOperatorMissionControlCover
	KindOperatorArtifact
	KindTraceProjection

	// MC_HOME typed own-source grants (:663-:684).
	KindOwnSession       // :663  MC_HOME/sessions/<run-or-session-id>/  NOTE: run-OR-session
	KindOwnState         // :681  MC_HOME/state/worksources/<scope-id>/home
	KindPackageCache     // :682  the four exact derived package-cache roots
	KindRuntimeControl   // :683, :684  the selected canonical control directory
	KindRunOutput        // :669  MC_HOME/outputs/<run-id>/
	KindCompletionSeal   // :666  MC_HOME/seals/<run-id>/
	KindAttachmentIn     // :664  MC_HOME/attachments/<session-id>/in/published
	KindAttachmentOut    // :668  MC_HOME/attachments/<session-id>/out
	KindWorkflowCapture  // :675  MC_HOME/workflows/<run-id>-plan.js — a FILE, not a directory
	KindCorrectionOutput // :674  the exact NEXT corrections/mc-<task-id>-corrections<n>, RW

	// Prior record inputs (:670-:673). Distinct from KindCorrectionOutput even
	// though :671 and :674 share a formula string: collapsing them would
	// authorize a Verifier to overwrite a prior correction. The formula is the
	// same; the resolved root is not.
	KindRecordInputOutput     // :670  one exact completed output/evidence file or directory
	KindRecordInputCorrection // :671  the brief-referenced correction <n>, RO
	KindRecordInputRevision   // :672  revisions/<task-id>-OP-REVISION.md
	KindRecordInputContext    // :673  context/<task-id>-STEER.md

	// Resident-materialized non-secret projections (:661, :662, :676-:678).
	// ADR-017:354-362's closed source-kind list names these explicitly.
	KindEnvelope           // :661  /mc/run.json
	KindSandbox            // :662  /mc/sandbox.json
	KindNetworkProjection  // :676  ADR-018 policy
	KindGatewayCA          // :677  the CA CERTIFICATE — never the private-key root
	KindResolverProjection // :678  /etc/resolv.conf

	KindRunnerSource // :680  /app/src, the release runner tree

	// Setup and landing effect classes (:691-:702). ADR-017:686-687: "their own
	// complete mount tables; they never inherit the agent table". The separation
	// is load-bearing, not cosmetic: setup /repo/source is RO (:691) while
	// landing /repo/source is RW (:699) — the only grant in the system that gets
	// a real Worksource repository RW, "intentionally including its primary
	// checkout".
	KindSetupWorksource
	KindSetupMissionControlCover
	KindSetupTaskRoot
	KindSetupTaskSource
	KindSetupTaskGit
	KindSetupSeal
	KindSetupProjection
	KindSetupEnvelope
	KindLandingWorksource
	KindLandingMissionControlCover
	KindLandingTaskRoot
	KindLandingEnvelope

	kindMax
)

// KindNotABind is a confused-planner DENY SENTINEL, not an authorized kind.
// Image-rootfs rows and the named volume carry no host inode and therefore no
// entry in TypedRoots. Keeping the sentinel outside [KindNone+1, kindMax)
// preserves D10a's exact host-bind domain while still letting Rejects fail
// loudly if a caller tries to ask a meaningless jurisdiction question.
const KindNotABind TypedKind = ^TypedKind(0)

// kindNames is indexed by TypedKind. A kind missing here yields a numeric
// fallback rather than a panic; the orphan guard is what keeps it honest.
var kindNames = [...]string{
	KindNone:                               "none",
	KindTaskRoot:                           "task-root",
	KindTaskSource:                         "task-source",
	KindWorkspaceSourceGitCover:            "workspace-source-git-cover",
	KindWorkspaceSourceMissionControlCover: "workspace-source-mission-control-cover",
	KindTaskGit:                            "task-git",
	KindTaskGitConfigCover:                 "task-git-config-cover",
	KindTaskGitHooksCover:                  "task-git-hooks-cover",
	KindTaskGitInfoCover:                   "task-git-info-cover",
	KindTaskGitObjectsInfoCover:            "task-git-objects-info-cover",
	KindSealedPack:                         "sealed-pack",
	KindTaskGitPackedRefsCover:             "task-git-packed-refs-cover",
	KindTaskGitShallowCover:                "task-git-shallow-cover",
	KindTaskGitWorktreeCommondirCover:      "task-git-worktree-commondir-cover",
	KindTaskGitWorktreeGitdirCover:         "task-git-worktree-gitdir-cover",
	KindTaskGitWorktreeConfigCover:         "task-git-worktree-config-cover",
	KindWorkspaceCommittedProjection:       "workspace-committed-projection",
	KindExecutionProjection:                "execution-projection",
	KindWorkspaceRegisteredRoot:            "workspace-registered-root",
	KindSeedingCommittedProjection:         "seeding-committed-projection",
	KindSeedingRegisteredRoot:              "seeding-registered-root",
	KindSeedingSourceGitCover:              "seeding-source-git-cover",
	KindOperatorWorksource:                 "operator-worksource",
	KindOperatorSourceGitCover:             "operator-source-git-cover",
	KindOperatorRegisteredControlCover:     "operator-registered-control-cover",
	KindOperatorMissionControlCover:        "operator-mission-control-cover",
	KindOperatorArtifact:                   "operator-artifact",
	KindTraceProjection:                    "trace-projection",
	KindOwnSession:                         "own-session",
	KindOwnState:                           "own-state",
	KindPackageCache:                       "package-cache",
	KindRuntimeControl:                     "runtime-control",
	KindRunOutput:                          "run-output",
	KindCompletionSeal:                     "completion-seal",
	KindAttachmentIn:                       "attachment-in",
	KindAttachmentOut:                      "attachment-out",
	KindWorkflowCapture:                    "workflow-capture",
	KindCorrectionOutput:                   "correction-output",
	KindRecordInputOutput:                  "record-input-output",
	KindRecordInputCorrection:              "record-input-correction",
	KindRecordInputRevision:                "record-input-revision",
	KindRecordInputContext:                 "record-input-context",
	KindEnvelope:                           "envelope",
	KindSandbox:                            "sandbox",
	KindNetworkProjection:                  "network-projection",
	KindGatewayCA:                          "gateway-ca",
	KindResolverProjection:                 "resolver-projection",
	KindRunnerSource:                       "runner-source",
	KindSetupWorksource:                    "setup-worksource",
	KindSetupMissionControlCover:           "setup-mission-control-cover",
	KindSetupTaskRoot:                      "setup-task-root",
	KindSetupTaskSource:                    "setup-task-source",
	KindSetupTaskGit:                       "setup-task-git",
	KindSetupSeal:                          "setup-seal",
	KindSetupProjection:                    "setup-projection",
	KindSetupEnvelope:                      "setup-envelope",
	KindLandingWorksource:                  "landing-worksource",
	KindLandingMissionControlCover:         "landing-mission-control-cover",
	KindLandingTaskRoot:                    "landing-task-root",
	KindLandingEnvelope:                    "landing-envelope",
}

func (k TypedKind) String() string {
	if k == KindNotABind {
		return "not-a-bind"
	}
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "typed-kind(" + itoa(uint8(k)) + ")"
}

func itoa(v uint8) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = '0' + v%10
		v /= 10
	}
	return string(buf[i:])
}
