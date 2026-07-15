package boundary

import (
	"os"
	"strings"
	"testing"
)

// ADR-021 D10a: TypedKind's domain is DERIVED from ADR-017 Decision 6's
// destination table (:634-702), not hand-copied from Decision 5's illustrative
// :396-401 list. A hand-copied list has drifted twice — it killed the c120c5c
// draft outright, and it survived the first rework as a nine-item permit list
// that left ~13 grants with no kind and therefore unplannable, behind a green
// suite. This file is the structural fix: it reads the real table out of the
// real ADR, so a row added to ADR-017 fails here instead of failing in
// production.
//
// The guard runs in BOTH directions. One-directional coverage is exactly how
// the nine-item list survived a full adversarial review.

const adr017Path = "../../docs/adr/017-mount-authorization.md"

// disposition is what ADR-021 D10a says a destination row is. Every row is
// exactly one of these, and none may be left implicit: a row that reaches
// KindNone by default rather than by decision is the drift this file exists to
// catch.
type disposition struct {
	kinds    []TypedKind // typed system source(s); one row may have several arms
	ordinary bool        // ADR-017 marks it "allowlisted": union predicate, zero claim
}

func typed(kinds ...TypedKind) disposition { return disposition{kinds: kinds} }

var ordinary = disposition{ordinary: true}

// adr017Rows maps every destination in ADR-017:634-702 to its disposition.
//
// The KEY is the destination cell verbatim, including its backticks and any
// "setup "/"landing " class prefix — ADR-017:686-687 keeps setup and landing as
// separate tables that "never inherit the agent table", and the collision is
// real: setup `/repo/source` is RO (:691) while landing `/repo/source` is RW
// (:699), "intentionally including its primary checkout". Keying on the bare
// path would fuse the least- and most-privileged grants in the system.
var adr017Rows = map[string]disposition{
	// Task-local skeleton (:636-650)
	"`/workspace`":                                              typed(KindTaskRoot, KindNotABind), // bind arm, else image-rootfs
	"`/workspace/source`":                                       typed(KindTaskSource, KindCommittedProjection, KindExecutionProjection, KindRegisteredRoot),
	"`/workspace/source/.git`":                                  typed(KindInertCover),
	"`/workspace/source/.mission-control`":                      typed(KindInertCover),
	"`/workspace/git`":                                          typed(KindTaskGit),
	"`/workspace/git/config`":                                   typed(KindInertCover),
	"`/workspace/git/hooks`":                                    typed(KindInertCover),
	"`/workspace/git/info`":                                     typed(KindInertCover),
	"`/workspace/git/objects/info`":                             typed(KindInertCover),
	"`/workspace/git/objects/pack`":                             typed(KindSealedPack),
	"`/workspace/git/packed-refs`":                              typed(KindInertCover),
	"`/workspace/git/shallow`":                                  typed(KindInertCover),
	"`/workspace/git/worktrees/<mc-task-name>/commondir`":       typed(KindInertCover),
	"`/workspace/git/worktrees/<mc-task-name>/gitdir`":          typed(KindInertCover),
	"`/workspace/git/worktrees/<mc-task-name>/config.worktree`": typed(KindInertCover),

	// Allowlisted ordinary sources: the union predicate, zero claim (:651-652).
	"`/workspace/artifacts/<artifact-target>/<suffix>`":   ordinary,
	"`/workspace/references/<reference-target>/<suffix>`": ordinary,

	// Projections and spine-registered roots (:653-660)
	"`/workspace/seeding/<workspace-target>/source`":                                                 typed(KindCommittedProjection, KindRegisteredRoot),
	"`/workspace/seeding/<workspace-target>/source/.git`":                                            typed(KindInertCover),
	"`/workspace/operator/worksources/<workspace-target>/source`":                                    typed(KindOperatorWorksource),
	"`/workspace/operator/worksources/<workspace-target>/source/.git`":                               typed(KindInertCover),
	"`/workspace/operator/worksources/<workspace-target>/source/<registered-control-relative-path>`": typed(KindInertCover),
	"`/workspace/operator/worksources/<workspace-target>/source/.mission-control`":                   typed(KindInertCover),
	"`/workspace/operator/worksources/<workspace-target>/artifacts/<artifact-target>/<suffix>`":      typed(KindOperatorArtifact),
	"`/workspace/operator/traces`":                                                                   typed(KindTraceProjection),

	// Resident-materialized projections (:661-662, :676-678). ADR-017:354-362's
	// closed source-kind list names envelope, sandbox, resolver/network
	// projection and CA certificate explicitly: these ARE binds and DO get kinds.
	"`/mc/run.json`":            typed(KindEnvelope),
	"`/mc/sandbox.json`":        typed(KindSandbox),
	"`/mc/network/policy.json`": typed(KindNetworkProjection),
	"`/mc/gateway/ca.crt`":      typed(KindGatewayCA), // the CERTIFICATE, never the private-key root
	"`/etc/resolv.conf`":        typed(KindResolverProjection),

	// MC_HOME typed own-source grants (:663-684)
	"`/mc/session`":                 typed(KindOwnSession),
	"`/mc/attachments/in`":          typed(KindAttachmentIn),
	"`/mc/private`":                 typed(KindNotABind), // ":665 never a bind"
	"`/mc/private/completion-seal`": typed(KindCompletionSeal),
	"`/mc/private/attachments`":     typed(KindNotABind), // image-rootfs structural dir
	"`/mc/private/attachments/out`": typed(KindAttachmentOut),
	"`/mc/records/output`":          typed(KindRunOutput),
	"`/mc/records/inputs/outputs/run-<producer-run-id>/item-<n>`": typed(KindRecordInputOutput),
	"`/mc/records/inputs/corrections/correction-<n>`":             typed(KindRecordInputCorrection),
	"`/mc/records/inputs/revision`":                               typed(KindRecordInputRevision),
	"`/mc/records/inputs/context`":                                typed(KindRecordInputContext),
	"`/mc/records/correction`":                                    typed(KindCorrectionOutput),
	"`/mc/workflow/plan.js`":                                      typed(KindWorkflowCapture),
	"`/mc/spine`":                                                 typed(KindNotABind), // named volume, no host path
	"`/app/src`":                                                  typed(KindRunnerSource),
	"`/home/agent`":                                               typed(KindOwnState),
	"`/home/agent/.cache/pnpm`, `/home/agent/.cache/uv`, `/home/agent/.cache/pip`, `/home/agent/.cache/cargo`": typed(KindPackageCache),
	"`/home/agent/.codex`":  typed(KindRuntimeControl),
	"`/home/agent/.claude`": typed(KindRuntimeControl),

	// Setup and landing: separate tables by ADR-017:686-687 (:691-702).
	"setup `/repo/source`":                    typed(KindSetupWorksource),
	"setup `/repo/source/.mission-control`":   typed(KindInertCover),
	"setup `/repo/task`":                      typed(KindSetupTaskRoot),
	"setup `/repo/task/source`":               typed(KindSetupTaskSource),
	"setup `/repo/task/git`":                  typed(KindSetupTaskGit),
	"setup `/repo/seal`":                      typed(KindSetupSeal),
	"setup `/repo/projection`":                typed(KindSetupProjection),
	"setup `/mc/setup.json`":                  typed(KindSetupEnvelope),
	"landing `/repo/source`":                  typed(KindLandingWorksource),
	"landing `/repo/source/.mission-control`": typed(KindInertCover),
	"landing `/repo/task`":                    typed(KindLandingTaskRoot),
	"landing `/mc/landing.json`":              typed(KindLandingEnvelope),
}

// parseADR017Destinations reads the two destination tables out of the real ADR
// rather than trusting a transcription. It returns the first cell of every row,
// verbatim, keyed by the header it fell under so setup/landing keep their class
// prefix.
func parseADR017Destinations(t *testing.T) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(adr017Path)
	if err != nil {
		t.Fatalf("read %s: %v", adr017Path, err)
	}
	lines := strings.Split(string(raw), "\n")

	found := map[string]int{}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		// A destination table is a header row naming the destination column,
		// followed by the |---| separator.
		if !strings.HasPrefix(line, "| Destination |") && !strings.HasPrefix(line, "| Class/destination |") {
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|---") {
			t.Fatalf("%s:%d: table header not followed by a separator", adr017Path, i+1)
		}
		for j := i + 2; j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|"); j++ {
			cells := strings.Split(strings.Trim(strings.TrimSpace(lines[j]), "|"), "|")
			dest := strings.TrimSpace(cells[0])
			if dest == "" {
				t.Fatalf("%s:%d: empty destination cell", adr017Path, j+1)
			}
			found[dest] = j + 1
		}
	}
	if len(found) == 0 {
		t.Fatalf("%s: parsed no destination rows — the table shape changed and this guard went blind", adr017Path)
	}
	return found
}

// ADR-021 D10a, direction 1: every host-bind row of ADR-017:634-702 maps to a
// kind. A row with no kind is a grant that cannot plan.
func TestTypedKindCoversEveryADR017Row(t *testing.T) {
	found := parseADR017Destinations(t)

	// The parse itself is load-bearing: if the table shape drifts and we silently
	// read three rows, every assertion below passes vacuously.
	if len(found) != len(adr017Rows) {
		t.Errorf("parsed %d destination rows from %s but the table has %d entries",
			len(found), adr017Path, len(adr017Rows))
	}

	for dest, line := range found {
		d, ok := adr017Rows[dest]
		if !ok {
			t.Errorf("%s:%d: destination %s has NO disposition — every host-bind row needs a TypedKind (ADR-021 D10a)",
				adr017Path, line, dest)
			continue
		}
		if !d.ordinary && len(d.kinds) == 0 {
			t.Errorf("%s:%d: destination %s reached KindNone by default, not by decision", adr017Path, line, dest)
		}
	}
}

// ADR-021 D10a, direction 2: every kind is claimed by some row. An orphaned kind
// is a kind nothing can legitimately hold — and, more usefully, it catches an
// enum member added without a row to justify it.
func TestNoOrphanTypedKind(t *testing.T) {
	claimed := map[TypedKind]bool{}
	for _, d := range adr017Rows {
		for _, k := range d.kinds {
			claimed[k] = true
		}
	}
	for k := KindNone + 1; k < kindMax; k++ {
		if !claimed[k] {
			t.Errorf("TypedKind %v appears in no ADR-017:634-702 row — it is an orphan (ADR-021 D10a)", k)
		}
	}
}

// The table's own keys must be real rows. A typo'd key would otherwise satisfy
// direction 2 while covering nothing.
func TestNoPhantomTableRows(t *testing.T) {
	found := parseADR017Destinations(t)
	for dest := range adr017Rows {
		if _, ok := found[dest]; !ok {
			t.Errorf("adr017Rows has %s, which is in no ADR-017:634-702 row — phantom entry", dest)
		}
	}
}

// KindNone is the zero value and means "not a claim" (ADR-021 D1). It must never
// be reachable as a typed disposition.
func TestKindNoneIsNeverATypedDisposition(t *testing.T) {
	for dest, d := range adr017Rows {
		for _, k := range d.kinds {
			if k == KindNone {
				t.Errorf("%s maps to KindNone as a typed kind; KindNone means no claim at all", dest)
			}
		}
	}
}
