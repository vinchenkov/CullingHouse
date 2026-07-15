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

	// Task-local skeleton (ADR-017:636-650; host formulas :713-714).
	KindTaskRoot   // :636  <workspace_root>/.mission-control/tasks/task-<task-id>
	KindTaskSource // :637  <task-root>/source
	KindTaskGit    // :640  <task-root>/git
	KindSealedPack // :645  <task-git>/objects/pack
	KindInertCover // :638-:650, :654, :656-:658, :692, :700 — generated RO covers

	// Projections and spine-registered roots (:637, :653, :655, :659, :660).
	KindCommittedProjection // :637, :653  clean pinned committed-tree projection
	KindExecutionProjection // :637        execution-scoped sealed-tree materialization
	KindRegisteredRoot      // :637, :653  registered non-repository root
	KindOperatorWorksource  // :655        Homie's registered live Worksource root
	KindOperatorArtifact    // :659        Homie's registered artifact source
	KindTraceProjection     // :660        ADR-017 Decision 7's hardlink projection root

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
	KindSetupWorksource   // :691  RO
	KindSetupTaskRoot     // :693
	KindSetupTaskSource   // :694
	KindSetupTaskGit      // :695
	KindSetupSeal         // :696
	KindSetupProjection   // :697
	KindSetupEnvelope     // :698
	KindLandingWorksource // :699  RW
	KindLandingTaskRoot   // :701
	KindLandingEnvelope   // :702

	// KindNotABind names a destination that is NOT a host bind: an image-rootfs
	// directory (:636's non-standalone arm, :665 "never a bind", :667) or the
	// runtime-local named volume (:679, whose boundary is setuid mc ownership).
	// These have no host inode, so a jurisdiction question about them is
	// meaningless and any answer would be a lie. Reaching Rejects with this kind
	// means the planner is confused: it denies, loudly, rather than permitting.
	KindNotABind

	kindMax
)

// kindNames is indexed by TypedKind. A kind missing here yields a numeric
// fallback rather than a panic; the orphan guard is what keeps it honest.
var kindNames = [...]string{
	KindNone:                  "none",
	KindTaskRoot:              "task-root",
	KindTaskSource:            "task-source",
	KindTaskGit:               "task-git",
	KindSealedPack:            "sealed-pack",
	KindInertCover:            "inert-cover",
	KindCommittedProjection:   "committed-projection",
	KindExecutionProjection:   "execution-projection",
	KindRegisteredRoot:        "registered-root",
	KindOperatorWorksource:    "operator-worksource",
	KindOperatorArtifact:      "operator-artifact",
	KindTraceProjection:       "trace-projection",
	KindOwnSession:            "own-session",
	KindOwnState:              "own-state",
	KindPackageCache:          "package-cache",
	KindRuntimeControl:        "runtime-control",
	KindRunOutput:             "run-output",
	KindCompletionSeal:        "completion-seal",
	KindAttachmentIn:          "attachment-in",
	KindAttachmentOut:         "attachment-out",
	KindWorkflowCapture:       "workflow-capture",
	KindCorrectionOutput:      "correction-output",
	KindRecordInputOutput:     "record-input-output",
	KindRecordInputCorrection: "record-input-correction",
	KindRecordInputRevision:   "record-input-revision",
	KindRecordInputContext:    "record-input-context",
	KindEnvelope:              "envelope",
	KindSandbox:               "sandbox",
	KindNetworkProjection:     "network-projection",
	KindGatewayCA:             "gateway-ca",
	KindResolverProjection:    "resolver-projection",
	KindRunnerSource:          "runner-source",
	KindSetupWorksource:       "setup-worksource",
	KindSetupTaskRoot:         "setup-task-root",
	KindSetupTaskSource:       "setup-task-source",
	KindSetupTaskGit:          "setup-task-git",
	KindSetupSeal:             "setup-seal",
	KindSetupProjection:       "setup-projection",
	KindSetupEnvelope:         "setup-envelope",
	KindLandingWorksource:     "landing-worksource",
	KindLandingTaskRoot:       "landing-task-root",
	KindLandingEnvelope:       "landing-envelope",
	KindNotABind:              "not-a-bind",
}

func (k TypedKind) String() string {
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
