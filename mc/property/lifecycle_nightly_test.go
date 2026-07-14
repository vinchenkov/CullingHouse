//go:build nightly

package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"mc/domain"
)

type rawRelation int

const (
	rawMustAccept rawRelation = iota
	rawMustReject
	rawMayAccept
)

type walkApply func(context.Context, domain.Q) (string, error)

type walkOperation struct {
	Name        string
	Coverage    []string
	WantAccept  bool
	WantCode    string
	RawRelation rawRelation
	Domain      walkApply
	Raw         walkApply
}

type spineSnapshot struct {
	Worksources []string
	Tasks       []string
	Packets     []string
	Lock        string
	Runs        []string
	Activity    []string
}

type taskFact struct {
	ID           int64
	Scope        string
	Status       string
	InitiativeID sql.NullInt64
	Blocked      bool
	Archived     bool
	Decision     sql.NullString
	HasPacket    bool
	PacketLive   bool
	Saturated    bool
	Retries      int
	Correction   int
	Identity     string
}

type walkHarness struct {
	t          *testing.T
	domainDB   *sql.DB
	rawDB      *sql.DB
	seed       int64
	step       int
	history    []string
	coverage   map[string]int
	randomHits map[string]int
	inRandom   bool
	lastDomain spineSnapshot
	lastRaw    spineSnapshot
}

func TestLifecycleRandomWalkDifferential(t *testing.T) {
	for _, seed := range []int64{260713, 260731, 261307, 271306} {
		t.Run("seed-"+intString(seed), func(t *testing.T) {
			h := newWalkHarness(t, seed)
			h.runCoverageWalk()
			h.runRandomTail(160)
			h.assertCoverage()
		})
	}
}

func newWalkHarness(t *testing.T, seed int64) *walkHarness {
	t.Helper()
	h := &walkHarness{
		t: t, domainDB: openPropertySpine(t), rawDB: openPropertySpine(t),
		seed: seed, coverage: map[string]int{}, randomHits: map[string]int{},
	}
	h.lastDomain = snapshotSpine(t, h.domainDB)
	h.lastRaw = snapshotSpine(t, h.rawDB)
	if !reflect.DeepEqual(h.lastDomain, h.lastRaw) {
		t.Fatalf("seed=%d initial twin spines differ:\ndomain=%+v\nraw=%+v", seed, h.lastDomain, h.lastRaw)
	}
	h.audit("initial")
	return h
}

func accepted(name string, coverage []string, domainApply, rawApply walkApply) walkOperation {
	return walkOperation{
		Name: name, Coverage: coverage, WantAccept: true,
		RawRelation: rawMustAccept, Domain: domainApply, Raw: rawApply,
	}
}

func rejected(name, code string, relation rawRelation, domainApply, rawApply walkApply) walkOperation {
	return walkOperation{
		Name: name, Coverage: []string{
			"rejected:" + name,
			fmt.Sprintf("rejected-case:%s|%s|%s", name, code, relationName(relation)),
		},
		WantCode: code, RawRelation: relation,
		Domain: domainApply, Raw: rawApply,
	}
}

func relationName(relation rawRelation) string {
	switch relation {
	case rawMustAccept:
		return "raw-must-accept"
	case rawMustReject:
		return "raw-must-reject"
	case rawMayAccept:
		return "raw-may-accept"
	default:
		return "raw-relation-unknown"
	}
}

func (h *walkHarness) run(op walkOperation) {
	h.t.Helper()
	h.step++
	h.history = append(h.history, op.Name)
	if len(h.history) > 32 {
		h.history = append([]string(nil), h.history[len(h.history)-32:]...)
	}
	beforeDomain := snapshotSpine(h.t, h.domainDB)
	beforeRaw := snapshotSpine(h.t, h.rawDB)
	beforeFacts := loadTaskFacts(h.t, h.domainDB)

	dConn := beginWalk(h.t, h.domainDB)
	rConn := beginWalk(h.t, h.rawDB)
	dResult, dErr := op.Domain(context.Background(), dConn)
	rResult, rErr := op.Raw(context.Background(), rConn)

	fail := func(format string, args ...any) {
		_, _ = dConn.ExecContext(context.Background(), `ROLLBACK`)
		_, _ = rConn.ExecContext(context.Background(), `ROLLBACK`)
		_ = dConn.Close()
		_ = rConn.Close()
		prefix := fmt.Sprintf("seed=%d step=%d op=%s history=%v: ",
			h.seed, h.step, op.Name, h.history)
		h.t.Fatalf(prefix+format, args...)
	}

	if op.WantAccept {
		if dErr != nil || rErr != nil {
			fail("accepted operation errors: domain=%v raw=%v", dErr, rErr)
		}
		if dResult != rResult {
			fail("result differential: domain=%q raw=%q", dResult, rResult)
		}
		if _, err := dConn.ExecContext(context.Background(), `COMMIT`); err != nil {
			fail("commit domain: %v", err)
		}
		if _, err := rConn.ExecContext(context.Background(), `COMMIT`); err != nil {
			fail("commit raw: %v", err)
		}
		_ = dConn.Close()
		_ = rConn.Close()
		afterDomain := snapshotSpine(h.t, h.domainDB)
		afterRaw := snapshotSpine(h.t, h.rawDB)
		if !reflect.DeepEqual(afterDomain, afterRaw) {
			h.t.Fatalf("seed=%d step=%d op=%s twin state differential:\ndomain=%+v\nraw=%+v\nhistory=%v",
				h.seed, h.step, op.Name, afterDomain, afterRaw, h.history)
		}
		assertNoDeletedRows(h.t, h.seed, h.step, op.Name, beforeDomain, afterDomain)
		assertLegalStatusEdges(h.t, h.seed, h.step, op.Name, beforeFacts, loadTaskFacts(h.t, h.domainDB))
		h.lastDomain, h.lastRaw = afterDomain, afterRaw
	} else {
		if code := codeOf(dErr); code != op.WantCode {
			fail("domain rejection code=%q want=%q error=%v", code, op.WantCode, dErr)
		}
		switch op.RawRelation {
		case rawMustAccept:
			if rErr != nil {
				fail("raw relation must accept, got %v", rErr)
			}
		case rawMustReject:
			if rErr == nil {
				fail("raw relation must reject but accepted result %q", rResult)
			}
		case rawMayAccept:
		}
		_, _ = dConn.ExecContext(context.Background(), `ROLLBACK`)
		_, _ = rConn.ExecContext(context.Background(), `ROLLBACK`)
		_ = dConn.Close()
		_ = rConn.Close()
		if after := snapshotSpine(h.t, h.domainDB); !reflect.DeepEqual(after, beforeDomain) {
			h.t.Fatalf("seed=%d step=%d op=%s rejected domain operation was not atomic:\nbefore=%+v\nafter=%+v",
				h.seed, h.step, op.Name, beforeDomain, after)
		}
		if after := snapshotSpine(h.t, h.rawDB); !reflect.DeepEqual(after, beforeRaw) {
			h.t.Fatalf("seed=%d step=%d op=%s rejected raw operation was not rolled back:\nbefore=%+v\nafter=%+v",
				h.seed, h.step, op.Name, beforeRaw, after)
		}
	}
	for _, key := range op.Coverage {
		h.coverage[key]++
		if h.inRandom {
			h.randomHits[key]++
		}
	}
	h.audit(op.Name)
}

func beginWalk(t *testing.T, db *sql.DB) *sql.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("walk conn: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		_ = conn.Close()
		t.Fatalf("BEGIN IMMEDIATE: %v", err)
	}
	return conn
}

func (h *walkHarness) audit(op string) {
	h.t.Helper()
	for label, db := range map[string]*sql.DB{"domain": h.domainDB, "raw": h.rawDB} {
		assertSpineInvariants(h.t, db, h.seed, h.step, op, label)
	}
	if h.step%20 == 0 {
		for label, db := range map[string]*sql.DB{"domain": h.domainDB, "raw": h.rawDB} {
			var result string
			if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil || result != "ok" {
				h.t.Fatalf("seed=%d step=%d op=%s %s integrity_check=%q err=%v",
					h.seed, h.step, op, label, result, err)
			}
		}
	}
}

func (h *walkHarness) runCoverageWalk() {
	r := rand.New(rand.NewSource(h.seed))
	priority := func() *int { v := r.Intn(5) - 1; return &v }

	// Track A: ordinary PASS, packet saturation/recovery, re-render. It also
	// exercises the subject-carrying lease, live fence, heartbeat and release.
	var a int64
	h.run(birthProposalOp("A birth", domain.ProposalArgs{
		Title: "A", Description: "pass/refine track", Scope: "task",
		Priority: priority(), Origin: "user", Worksource: "ws",
	}, &a))
	h.run(promoteOp("A promote", a))
	h.run(advanceWorkedOp("A work", a))
	h.run(claimOp("A verifier claim", "a-v1", "verifier", &a, false))
	h.run(fenceOp("A live fence", "a-v1", &a))
	h.run(heartbeatOp("A heartbeat", "a-v1"))
	h.run(releaseOp("A release", "a-v1"))
	h.run(rejectedVerdictOp("A premature budget-spent rejected", domain.VerdictArgs{
		TaskID: a, RunID: "a-v1", Outcome: "budget-spent",
		EvidencePath: "evidence/a-invalid-budget", VerifiedSHA: "sha-a-invalid",
	}, domain.CodeBudgetRemaining))
	h.run(rejectedVerdictOp("A correction carrier rejected", domain.VerdictArgs{
		TaskID: a, RunID: "a-v1", Outcome: "correct",
		EvidencePath: "evidence/a-invalid-correction",
	}, domain.CodeCorrectionRequired))
	h.run(verdictOp("A pass", a, "a-v1", "pass", "evidence/a", "sha-a1", "", ""))
	h.run(packageOp("A package", a, "packets/a1.html", true))
	h.run(deepeningOp("A churn 1", a, false))
	h.run(deepeningOp("A genuine reset", a, true))
	h.run(deepeningOp("A churn after reset 1", a, false))
	h.run(deepeningOp("A churn after reset 2", a, false))
	h.run(deepeningOp("A churn after reset 3", a, false))
	h.run(rejectedDeepeningOp("A saturated reset rejected", a))
	h.run(reenterOp("A reenter", a, "deepen A"))
	h.run(advanceWorkedOp("A rework", a))
	h.run(claimOp("A verifier claim 2", "a-v2", "verifier", &a, false))
	h.run(releaseOp("A release 2", "a-v2"))
	h.run(verdictOp("A recovery pass", a, "a-v2", "pass", "evidence/a2", "sha-a2", "", "genuine"))
	h.run(packageOp("A rerender", a, "packets/a2.html", true))

	// Track B: the complete correction rally and BUDGET-SPENT arm.
	var b int64
	h.run(birthProposalOp("B birth", domain.ProposalArgs{
		Title: "B", Description: "correction track", Scope: "task",
		Priority: priority(), Origin: "autonomous", Worksource: "ws",
	}, &b))
	h.run(promoteOp("B promote", b))
	h.run(advanceWorkedOp("B work", b))
	for round := 1; round <= 3; round++ {
		runID := fmt.Sprintf("b-v%d", round)
		h.run(claimOp(fmt.Sprintf("B verifier claim %d", round), runID, "verifier", &b, false))
		h.run(releaseOp(fmt.Sprintf("B release %d", round), runID))
		h.run(verdictOp(fmt.Sprintf("B correct %d", round), b, runID, "correct",
			fmt.Sprintf("evidence/b%d", round), "", fmt.Sprintf("correction/b%d", round), ""))
		h.run(advanceWorkedOp(fmt.Sprintf("B rework %d", round), b))
	}
	h.run(claimOp("B verifier claim spent", "b-v4", "verifier", &b, false))
	h.run(releaseOp("B release spent", "b-v4"))
	h.run(verdictOp("B budget spent", b, "b-v4", "budget-spent", "evidence/b4", "sha-b", "", ""))
	h.run(packageOp("B package", b, "packets/b.html", true))

	// Track C: initiative wave atomicity, strict drain and block propagation.
	var initiative int64
	h.run(birthProposalOp("initiative birth", domain.ProposalArgs{
		Title: "initiative", Description: "wave track", Scope: "initiative",
		Priority: priority(), Origin: "user", Worksource: "ws",
	}, &initiative))
	h.run(promoteOp("initiative promote", initiative))
	h.run(emptyWaveRejectedOp("empty wave domain-only rejection", initiative))
	var children []int64
	h.run(birthWaveOp("initiative wave", initiative, []domain.WaveChild{
		{Title: "child one", Description: "one", Priority: priority()},
		{Title: "child two", Description: "two", Priority: priority()},
	}, &children))
	h.run(overlapWaveRejectedOp("overlapping wave rejected", initiative, priority()))
	h.run(strictDrainRejectedOp("initiative strict drain rejected", initiative))
	h.run(blockOp("child block", children[0], "generated operator question"))
	h.run(unblockParentRejectedOp("parent unblock rejected", initiative))
	h.run(unblockOp("child unblock", children[0]))
	h.run(cancelOp("child one cancel", children[0], "wave retired"))
	h.run(cancelOp("child two cancel", children[1], "wave retired"))
	h.run(advanceWorkedOp("initiative declare done", initiative))
	h.run(claimOp("initiative verifier claim", "i-v1", "verifier", &initiative, false))
	h.run(releaseOp("initiative verifier release", "i-v1"))
	h.run(verdictOp("initiative pass", initiative, "i-v1", "pass", "evidence/i", "sha-i", "", ""))
	h.run(packageOp("initiative package", initiative, "packets/i.html", true))

	// Track D: blocked proposal rejection, WIP-cap composite rollback, then
	// packet decision arms once a slot is released.
	var d int64
	h.run(birthProposalOp("D birth", domain.ProposalArgs{
		Title: "D", Description: "cap track", Scope: "task",
		Priority: priority(), Origin: "user", Worksource: "ws",
	}, &d))
	h.run(blockOp("D block", d, "hold proposal"))
	h.run(blockedPromoteRejectedOp("D blocked promote rejected", d))
	h.run(unblockOp("D unblock", d))
	h.run(promoteOp("D promote", d))
	h.run(nonProposedRejectRejectedOp("D non-proposed reject rejected", d))
	h.run(advanceWorkedOp("D work", d))
	h.run(claimOp("D verifier claim", "d-v1", "verifier", &d, false))
	h.run(releaseOp("D verifier release", "d-v1"))
	h.run(verdictOp("D pass", d, "d-v1", "pass", "evidence/d", "sha-d", "", ""))
	h.run(wipPackageRejectedOp("D fourth packet rejected", d, "packets/d.html"))
	h.run(cancelPacketOp("B packet cancel", b, "do not ship B"))
	h.run(packageOp("D package after slot", d, "packets/d.html", true))
	h.run(approveBranchlessOp("D approve", d))
	h.run(branchFixtureOp("A branch fixture", a, "mc/task-A"))
	h.run(approveBranchOp("A approve landing pending", a))
	h.run(landingFixtureOp("A landing completion fixture", a))
	h.run(cancelPacketOp("initiative packet cancel", initiative, "retire arc"))

	// Remaining task/operator arms and retry budget.
	var rejectedID int64
	h.run(birthProposalOp("reject birth", domain.ProposalArgs{
		Title: "reject me", Scope: "task", Priority: priority(), Origin: "autonomous", Worksource: "ws",
	}, &rejectedID))
	h.run(rejectProposalOp("proposal reject", rejectedID, "duplicate"))

	// Editor pool snapshots and inactive-Worksource intake are domain-only
	// policy surfaces, so both need explicit twin-spine witnesses.
	var poolA, poolB int64
	h.run(birthProposalOp("pool A birth", domain.ProposalArgs{
		Title: "pool A", Scope: "task", Priority: priority(), Origin: "autonomous", Worksource: "ws",
	}, &poolA))
	h.run(birthProposalOp("pool B birth", domain.ProposalArgs{
		Title: "pool B", Scope: "task", Priority: priority(), Origin: "autonomous", Worksource: "ws",
	}, &poolB))
	h.run(claimPoolOp("editor pool claim", "editor-pool", []int64{poolA, poolB}))
	h.run(releaseOp("editor pool release", "editor-pool"))
	h.run(rejectProposalOp("pool A reject", poolA, "snapshot consumed"))
	h.run(rejectProposalOp("pool B reject", poolB, "snapshot consumed"))
	h.run(inactiveWorksourceFixtureOp("inactive Worksource fixture", "ws-inactive"))
	h.run(inactiveBirthRejectedOp("inactive Worksource birth rejected", "ws-inactive", priority()))

	var infra int64
	h.run(birthProposalOp("infra birth", domain.ProposalArgs{
		Title: "infra", Scope: "task", Priority: priority(), Origin: "user", Worksource: "ws",
	}, &infra))
	h.run(blockNoReasonRejectedOp("block without reason rejected", infra))
	h.run(promoteOp("infra promote", infra))
	h.run(cancelPacketMissingRejectedOp("packet cancel without packet rejected", infra))
	h.run(chargeInfraOp("infra direct charge 1", infra, "adapter failed"))
	h.run(chargeInfraOp("infra direct charge 2", infra, "adapter failed again"))
	h.run(chargeInfraExhaustionOp("infra direct charge exhaustion", infra, "adapter failed finally"))

	// Subjectless lease path.
	h.run(claimOp("subjectless claim", "subjectless", "strategist", nil, true))
	h.run(fenceOp("subjectless fence", "subjectless", nil))
	h.run(heartbeatOp("subjectless heartbeat", "subjectless"))
	h.run(releaseOp("subjectless release", "subjectless"))
	h.run(claimOp("subjectless reap claim", "subjectless-reap", "strategist", nil, true))
	h.run(subjectlessReapOp("subjectless reap", "subjectless-reap", "spawn-watchdog"))

	// Held-lease refusals and the reap write side. The stale raw adapters use
	// the same compare token and must reject, not silently no-op.
	var reapSubject int64
	h.run(birthProposalOp("reap subject birth", domain.ProposalArgs{
		Title: "reap exhaustion", Scope: "task", Priority: priority(), Origin: "user", Worksource: "ws",
	}, &reapSubject))
	h.run(promoteOp("reap subject promote", reapSubject))
	h.run(retryFixtureOp("reap subject one retry", reapSubject, 1))
	h.run(claimOp("reap subject claim", "reap-live", "worker", &reapSubject, false))
	h.run(claimHeldRejectedOp("claim while held rejected", "loser", &reapSubject))
	h.run(staleFenceRejectedOp("stale fence rejected", "stale"))
	h.run(staleHeartbeatRejectedOp("stale heartbeat rejected", "stale"))
	h.run(staleReleaseRejectedOp("stale release rejected", "stale"))
	h.run(staleReapRejectedOp("stale reap rejected", "stale"))
	h.run(reapExhaustionOp("live reap exhaustion", "reap-live", "lease-timeout"))
	h.run(occupancyOp("final occupancy"))
}

type generatedTaskTrack struct {
	prefix           string
	id               int64
	priority         int
	phase            int
	verifierRound    int
	currentRun       string
	correctionTarget int
	corrections      int
	reenter          bool
	refinement       bool
	terminal         string
}

func (track *generatedTaskTrack) done() bool { return track.phase >= 8 }

func (track *generatedTaskTrack) holder() string {
	if track.phase == 4 {
		return track.currentRun
	}
	return ""
}

func (track *generatedTaskTrack) step(h *walkHarness) {
	prefix := "random lifecycle " + track.prefix
	switch track.phase {
	case 0:
		h.run(birthProposalOp(prefix+" birth", domain.ProposalArgs{
			Title: prefix, Description: "seeded randomized lifecycle witness", Scope: "task",
			Priority: &track.priority, Origin: "autonomous", Worksource: "ws",
		}, &track.id))
		track.phase = 1
	case 1:
		h.run(promoteOp(prefix+" promote", track.id))
		track.phase = 2
	case 2:
		h.run(advanceWorkedOp(prefix+" work", track.id))
		track.phase = 3
	case 3:
		track.verifierRound++
		track.currentRun = fmt.Sprintf("%s-v-%d", track.prefix, track.verifierRound)
		h.run(claimOp(prefix+" verifier claim", track.currentRun, "verifier", &track.id, false))
		track.phase = 4
	case 4:
		h.run(releaseOp(prefix+" verifier release", track.currentRun))
		track.phase = 5
	case 5:
		if !track.refinement && track.corrections < track.correctionTarget {
			track.corrections++
			h.run(verdictOp(prefix+" correct", track.id, track.currentRun, "correct",
				fmt.Sprintf("evidence/%s/%d", track.prefix, track.verifierRound), "",
				fmt.Sprintf("correction/%s/%d", track.prefix, track.verifierRound), ""))
			track.phase = 2
			return
		}
		outcome, deepening := "pass", ""
		if !track.refinement && track.correctionTarget == 3 {
			outcome = "budget-spent"
		}
		if track.refinement {
			deepening = "genuine"
		}
		h.run(verdictOp(prefix+" "+outcome, track.id, track.currentRun, outcome,
			fmt.Sprintf("evidence/%s/%d", track.prefix, track.verifierRound),
			"sha-"+track.prefix, "", deepening))
		track.phase = 6
	case 6:
		h.run(packageOp(prefix+" package", track.id, "packets/"+track.prefix+".html", true))
		track.phase = 7
	case 7:
		if track.reenter && !track.refinement {
			h.run(reenterOp(prefix+" reenter", track.id, "generated deepening"))
			track.refinement = true
			track.phase = 2
			return
		}
		if track.terminal == "approve" {
			h.run(approveBranchlessOp(prefix+" approve", track.id))
		} else {
			h.run(cancelPacketOp(prefix+" packet cancel", track.id, "generated terminal"))
		}
		track.phase = 8
	default:
		h.t.Fatalf("seed=%d invalid generated task phase %d", h.seed, track.phase)
	}
}

type generatedInitiativeTrack struct {
	prefix      string
	id          int64
	priority    int
	phase       int
	children    []int64
	childCursor int
	currentRun  string
	waveSize    int
	priorities  []int
}

func (track *generatedInitiativeTrack) done() bool { return track.phase >= 10 }

func (track *generatedInitiativeTrack) holder() string {
	if track.phase == 6 {
		return track.currentRun
	}
	return ""
}

func (track *generatedInitiativeTrack) step(h *walkHarness, r *rand.Rand) {
	prefix := "random lifecycle " + track.prefix
	switch track.phase {
	case 0:
		h.run(birthProposalOp(prefix+" birth", domain.ProposalArgs{
			Title: prefix, Description: "randomized wave charter", Scope: "initiative",
			Priority: &track.priority, Origin: "autonomous", Worksource: "ws",
		}, &track.id))
		track.phase = 1
	case 1:
		h.run(promoteOp(prefix+" promote", track.id))
		track.phase = 2
	case 2:
		children := make([]domain.WaveChild, track.waveSize)
		for i := range children {
			children[i] = domain.WaveChild{
				Title:       fmt.Sprintf("%s child %d", prefix, i),
				Description: "generated wave child", Priority: &track.priorities[i],
			}
		}
		h.run(birthWaveOp(prefix+" wave", track.id, children, &track.children))
		r.Shuffle(len(track.children), func(i, j int) {
			track.children[i], track.children[j] = track.children[j], track.children[i]
		})
		track.phase = 3
	case 3:
		h.run(cancelOp(prefix+" child cancel", track.children[track.childCursor], "generated wave drain"))
		track.childCursor++
		if track.childCursor == len(track.children) {
			track.phase = 4
		}
	case 4:
		h.run(advanceWorkedOp(prefix+" declare done", track.id))
		track.phase = 5
	case 5:
		track.currentRun = track.prefix + "-v"
		h.run(claimOp(prefix+" verifier claim", track.currentRun, "verifier", &track.id, false))
		track.phase = 6
	case 6:
		h.run(releaseOp(prefix+" verifier release", track.currentRun))
		track.phase = 7
	case 7:
		h.run(verdictOp(prefix+" pass", track.id, track.currentRun, "pass",
			"evidence/"+track.prefix, "sha-"+track.prefix, "", ""))
		track.phase = 8
	case 8:
		h.run(packageOp(prefix+" package", track.id, "packets/"+track.prefix+".html", true))
		track.phase = 9
	case 9:
		h.run(cancelPacketOp(prefix+" packet cancel", track.id, "generated initiative terminal"))
		track.phase = 10
	default:
		h.t.Fatalf("seed=%d invalid generated initiative phase %d", h.seed, track.phase)
	}
}

type generatedStepper struct {
	done   func() bool
	holder func() string
	step   func()
}

func (h *walkHarness) runRandomLifecycleStrata(r *rand.Rand) {
	budgetTrack := &generatedTaskTrack{
		prefix: "budget", priority: r.Intn(5) - 1,
		correctionTarget: 3, reenter: true, terminal: "approve",
	}
	passTrack := &generatedTaskTrack{
		prefix: "pass", priority: r.Intn(5) - 1,
		correctionTarget: 0, terminal: "cancel",
	}
	waveSize := 1 + r.Intn(3)
	waveTrack := &generatedInitiativeTrack{
		prefix: "wave", priority: r.Intn(5) - 1, waveSize: waveSize,
		priorities: make([]int, waveSize),
	}
	for i := range waveTrack.priorities {
		waveTrack.priorities[i] = r.Intn(5) - 1
	}
	steppers := []generatedStepper{
		{done: budgetTrack.done, holder: budgetTrack.holder, step: func() { budgetTrack.step(h) }},
		{done: passTrack.done, holder: passTrack.holder, step: func() { passTrack.step(h) }},
		{done: waveTrack.done, holder: waveTrack.holder, step: func() { waveTrack.step(h, r) }},
	}
	for {
		lockRun := currentLockRun(h.t, h.domainDB)
		var candidates []int
		for i := range steppers {
			if steppers[i].done() {
				continue
			}
			if lockRun == "" || steppers[i].holder() == lockRun {
				candidates = append(candidates, i)
			}
		}
		if len(candidates) == 0 {
			allDone := true
			for i := range steppers {
				allDone = allDone && steppers[i].done()
			}
			if allDone {
				return
			}
			h.t.Fatalf("seed=%d generated lifecycle deadlock lock=%q", h.seed, lockRun)
		}
		steppers[candidates[r.Intn(len(candidates))]].step()
	}
}

func (h *walkHarness) runRandomTail(steps int) {
	r := rand.New(rand.NewSource(h.seed ^ 0x5eed5eed))
	h.inRandom = true
	defer func() { h.inRandom = false }()
	h.runRandomLifecycleStrata(r)
	for i := 0; i < steps; i++ {
		facts := loadTaskFacts(h.t, h.domainDB)
		lockRun := currentLockRun(h.t, h.domainDB)
		if lockRun != "" {
			switch r.Intn(4) {
			case 0:
				h.run(fenceOp("random live fence", lockRun, lockSubject(h.t, h.domainDB)))
			case 1:
				h.run(heartbeatOp("random heartbeat", lockRun))
			case 2:
				h.run(releaseOp("random release", lockRun))
			case 3:
				h.run(reapOp("random reap", lockRun, "spawn-watchdog"))
			}
			continue
		}

		switch r.Intn(9) {
		case 0:
			var id int64
			scope := "task"
			if r.Intn(5) == 0 {
				scope = "initiative"
			}
			p := r.Intn(5) - 1
			h.run(birthProposalOp("random birth", domain.ProposalArgs{
				Title: fmt.Sprintf("random-%d-%d", h.seed, h.step), Scope: scope,
				Priority: &p, Origin: "autonomous", Worksource: "ws",
			}, &id))
		case 1:
			if f, ok := chooseFact(r, facts, func(f taskFact) bool {
				return f.Status == "proposed" && !f.Blocked && !f.Archived && !f.Decision.Valid
			}); ok {
				h.run(promoteOp("random promote", f.ID))
			} else {
				h.run(occupancyOp("random occupancy fallback"))
			}
		case 2:
			if f, ok := chooseFact(r, facts, func(f taskFact) bool {
				return f.Status == "proposed" && !f.Blocked && !f.Archived && !f.Decision.Valid
			}); ok {
				h.run(rejectProposalOp("random reject", f.ID, "random duplicate"))
			} else {
				h.run(occupancyOp("random occupancy fallback"))
			}
		case 3:
			if f, ok := chooseFact(r, facts, func(f taskFact) bool { return f.Blocked && !f.Archived }); ok {
				if f.Scope != "initiative" || !hasBlockedChild(f.ID, facts) {
					h.run(unblockOp("random unblock", f.ID))
				} else {
					h.run(occupancyOp("random occupancy blocked parent"))
				}
			} else if f, ok := chooseFact(r, facts, func(f taskFact) bool { return !f.Blocked && !f.Archived }); ok {
				h.run(blockOp("random block", f.ID, "random hold"))
			} else {
				h.run(occupancyOp("random occupancy fallback"))
			}
		case 4:
			if f, ok := chooseFact(r, facts, func(f taskFact) bool {
				return !f.Archived && !f.Decision.Valid && f.Status != "packaged"
			}); ok {
				h.run(cancelOp("random cancel", f.ID, "random retirement"))
			} else {
				h.run(occupancyOp("random occupancy fallback"))
			}
		case 5:
			if f, ok := chooseFact(r, facts, func(f taskFact) bool { return !f.Archived && !f.Decision.Valid }); ok {
				h.run(chargeInfraOp("random infra charge", f.ID, "random infra"))
			} else {
				h.run(occupancyOp("random occupancy fallback"))
			}
		case 6:
			if f, ok := chooseFact(r, facts, func(f taskFact) bool {
				return f.HasPacket && f.PacketLive && !f.Saturated && !f.Archived
			}); ok {
				h.run(deepeningOp("random packet churn", f.ID, false))
			} else {
				h.run(occupancyOp("random occupancy fallback"))
			}
		case 7:
			if f, ok := chooseFact(r, facts, func(f taskFact) bool {
				return f.Scope == "task" && f.Status == "seeded" &&
					!f.Blocked && !f.Archived && !f.Decision.Valid
			}); ok {
				runID := fmt.Sprintf("random-run-%d-%d", h.seed, h.step)
				h.run(claimOp("random subject claim", runID, "worker", &f.ID, false))
			} else {
				h.run(claimOp("random subjectless claim", fmt.Sprintf("random-run-%d-%d", h.seed, h.step), "strategist", nil, true))
			}
		case 8:
			h.run(occupancyOp("random occupancy"))
		}
	}
}

func (h *walkHarness) assertCoverage() {
	h.t.Helper()
	required := []string{
		"BirthProposal", "Promote", "RejectProposal", "AdvanceStage:worked",
		"AdvanceStage:packaged", "ApplyVerdict:pass", "ApplyVerdict:correct",
		"ApplyVerdict:budget-spent", "Reenter", "Block", "Unblock", "Approve",
		"Approve:branch", "Approve:branchless",
		"Cancel", "CancelPacket", "BirthWave", "Birth", "ApplyDeepening:genuine",
		"ApplyDeepening:churn", "Occupancy", "ChargeInfra", "Claim:subject",
		"ChargeInfra:exhaustion", "Claim:subjectless", "Claim:pool-snapshot",
		"Fence", "Heartbeat", "Release", "ApplyReap", "ApplyReap:subject",
		"ApplyReap:subjectless", "ApplyReap:exhaustion",
	}
	for _, key := range required {
		if h.coverage[key] == 0 {
			h.t.Errorf("seed=%d lifecycle generator never exercised %s; coverage=%v", h.seed, key, h.coverage)
		}
	}
	requiredRejected := []struct {
		name     string
		code     string
		relation rawRelation
	}{
		{"A premature budget-spent rejected", domain.CodeBudgetRemaining, rawMayAccept},
		{"A correction carrier rejected", domain.CodeCorrectionRequired, rawMayAccept},
		{"A saturated reset rejected", domain.CodeSaturated, rawMustReject},
		{"empty wave domain-only rejection", domain.CodeEmptyWave, rawMayAccept},
		{"overlapping wave rejected", domain.CodeStrictDrain, rawMayAccept},
		{"initiative strict drain rejected", domain.CodeStrictDrain, rawMustReject},
		{"parent unblock rejected", domain.CodeBlockedChild, rawMustReject},
		{"D blocked promote rejected", domain.CodeBlocked, rawMustReject},
		{"D non-proposed reject rejected", domain.CodeIllegalTransition, rawMayAccept},
		{"D fourth packet rejected", domain.CodeWIPCap, rawMustReject},
		{"inactive Worksource birth rejected", domain.CodeWorksourceInactive, rawMayAccept},
		{"block without reason rejected", domain.CodeReasonRequired, rawMustReject},
		{"packet cancel without packet rejected", domain.CodeNotFound, rawMayAccept},
		{"claim while held rejected", domain.CodeLeaseHeld, rawMustReject},
		{"stale fence rejected", domain.CodeStaleRun, rawMustReject},
		{"stale heartbeat rejected", domain.CodeStaleRun, rawMustReject},
		{"stale release rejected", domain.CodeStaleRun, rawMustReject},
		{"stale reap rejected", domain.CodeStaleRun, rawMustReject},
	}
	for _, item := range requiredRejected {
		key := fmt.Sprintf("rejected-case:%s|%s|%s", item.name, item.code, relationName(item.relation))
		if h.coverage[key] == 0 {
			h.t.Errorf("seed=%d lifecycle generator never exercised rejection %s; coverage=%v", h.seed, key, h.coverage)
		}
	}
	randomRequired := []string{
		"BirthProposal", "Promote", "AdvanceStage:worked", "ApplyVerdict:pass",
		"ApplyVerdict:correct", "ApplyVerdict:budget-spent", "AdvanceStage:packaged",
		"Birth", "Reenter", "Approve", "Cancel", "CancelPacket", "BirthWave",
	}
	for _, key := range randomRequired {
		if h.randomHits[key] == 0 {
			h.t.Errorf("seed=%d randomized lifecycle strata never exercised %s; random coverage=%v",
				h.seed, key, h.randomHits)
		}
	}
}

func chooseFact(r *rand.Rand, facts map[int64]taskFact, predicate func(taskFact) bool) (taskFact, bool) {
	var candidates []taskFact
	for _, fact := range facts {
		if predicate(fact) {
			candidates = append(candidates, fact)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	if len(candidates) == 0 {
		return taskFact{}, false
	}
	return candidates[r.Intn(len(candidates))], true
}

func hasBlockedChild(parent int64, facts map[int64]taskFact) bool {
	for _, fact := range facts {
		if fact.InitiativeID.Valid && fact.InitiativeID.Int64 == parent && fact.Blocked && !fact.Archived {
			return true
		}
	}
	return false
}

func rawNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func birthProposalOp(name string, args domain.ProposalArgs, captured *int64) walkOperation {
	return accepted(name, []string{"BirthProposal"},
		func(ctx context.Context, q domain.Q) (string, error) {
			id, err := domain.BirthProposal(ctx, q, args)
			if err == nil && captured != nil {
				*captured = id
			}
			return fmt.Sprintf("id:%d", id), err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			scope := args.Scope
			if scope == "" {
				scope = "task"
			}
			priority := 2
			if args.Priority != nil {
				priority = *args.Priority
			}
			res, err := q.ExecContext(ctx, `
				INSERT INTO tasks (title, description, scope, priority, origin, worksource, target_ref)
				VALUES (?, ?, ?, ?, ?, ?, 'main')`,
				args.Title, rawNull(args.Description), scope, priority, args.Origin, args.Worksource)
			if err != nil {
				return "", err
			}
			id, err := res.LastInsertId()
			return fmt.Sprintf("id:%d", id), err
		})
}

func promoteOp(name string, id int64) walkOperation {
	return accepted(name, []string{"Promote"},
		func(ctx context.Context, q domain.Q) (string, error) {
			if _, err := q.ExecContext(ctx, `UPDATE tasks SET stage_entered_at = '1900-01-01 00:00:00' WHERE id = ?`, id); err != nil {
				return "", err
			}
			return "promoted", domain.Promote(ctx, q, id)
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			if _, err := q.ExecContext(ctx, `UPDATE tasks SET stage_entered_at = '1900-01-01 00:00:00' WHERE id = ?`, id); err != nil {
				return "", err
			}
			_, err := q.ExecContext(ctx, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
			return "promoted", err
		})
}

func rejectProposalOp(name string, id int64, reason string) walkOperation {
	return accepted(name, []string{"RejectProposal"},
		func(ctx context.Context, q domain.Q) (string, error) {
			return "rejected", domain.RejectProposal(ctx, q, id, reason)
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			if _, err := q.ExecContext(ctx, `
				UPDATE tasks SET decision = 'rejected', decided_at = datetime('now'), archived = 1
				WHERE id = ?`, id); err != nil {
				return "", err
			}
			_, err := q.ExecContext(ctx, `
				INSERT INTO activity (actor, kind, subject, detail)
				VALUES ('editor', 'task.rejected', ?, ?)`, id, reason)
			return "rejected", err
		})
}

func advanceWorkedOp(name string, id int64) walkOperation {
	return accepted(name, []string{"AdvanceStage:worked"},
		func(ctx context.Context, q domain.Q) (string, error) {
			return "worked", domain.AdvanceStage(ctx, q, id, "worked")
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := q.ExecContext(ctx, `UPDATE tasks SET status = 'worked' WHERE id = ?`, id)
			return "worked", err
		})
}

func packageOp(name string, id int64, render string, wantAccept bool) walkOperation {
	domainApply := func(ctx context.Context, q domain.Q) (string, error) {
		if err := domain.AdvanceStage(ctx, q, id, "packaged"); err != nil {
			return "", err
		}
		if err := domain.Birth(ctx, q, id, render); err != nil {
			return "", err
		}
		return "packaged", nil
	}
	rawApply := func(ctx context.Context, q domain.Q) (string, error) {
		if _, err := q.ExecContext(ctx,
			`UPDATE tasks SET status = 'packaged', refine_notes = NULL WHERE id = ?`, id); err != nil {
			return "", err
		}
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_packets WHERE task_id = ?`, id).Scan(&n); err != nil {
			return "", err
		}
		var err error
		if n == 0 {
			_, err = q.ExecContext(ctx, `INSERT INTO review_packets (task_id, render_path) VALUES (?, ?)`, id, rawNull(render))
		} else {
			_, err = q.ExecContext(ctx, `UPDATE review_packets SET render_path = ? WHERE task_id = ?`, rawNull(render), id)
		}
		return "packaged", err
	}
	if wantAccept {
		return accepted(name, []string{"AdvanceStage:packaged", "Birth"}, domainApply, rawApply)
	}
	return rejected(name, domain.CodeWIPCap, rawMustReject, domainApply, rawApply)
}

func verdictOp(name string, taskID int64, runID, outcome, evidence, sha, correction, deepening string) walkOperation {
	coverage := []string{"ApplyVerdict:" + outcome}
	return accepted(name, coverage,
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := domain.ApplyVerdict(ctx, q, domain.VerdictArgs{
				TaskID: taskID, RunID: runID, Outcome: outcome,
				EvidencePath: evidence, VerifiedSHA: sha,
				CorrectionPath: correction, Deepening: deepening,
			})
			return fmt.Sprintf("%s|%d|%t", res.Status, res.CorrectionCount, res.ExceptionLabeled), err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			var correctionCount int
			if err := q.QueryRowContext(ctx, `SELECT correction_count FROM tasks WHERE id = ?`, taskID).Scan(&correctionCount); err != nil {
				return "", err
			}
			if deepening == "genuine" {
				if _, err := q.ExecContext(ctx, `UPDATE review_packets SET refine_streak = 0 WHERE task_id = ?`, taskID); err != nil {
					return "", err
				}
			} else if deepening == "churn" {
				if _, err := q.ExecContext(ctx, `UPDATE review_packets SET refine_streak = refine_streak + 1 WHERE task_id = ?`, taskID); err != nil {
					return "", err
				}
			}
			status, exception := "", false
			switch outcome {
			case "pass":
				_, err := q.ExecContext(ctx, `UPDATE tasks SET status = 'verified', verified_sha = ? WHERE id = ?`, sha, taskID)
				if err != nil {
					return "", err
				}
				status = "verified"
			case "correct":
				_, err := q.ExecContext(ctx, `UPDATE tasks SET status = 'seeded', correction_count = correction_count + 1 WHERE id = ?`, taskID)
				if err != nil {
					return "", err
				}
				correctionCount++
				status = "seeded"
			case "budget-spent":
				_, err := q.ExecContext(ctx, `UPDATE tasks SET status = 'verified', verified_sha = ? WHERE id = ?`, sha, taskID)
				if err != nil {
					return "", err
				}
				status, exception = "verified", true
			}
			if _, err := q.ExecContext(ctx, `
				UPDATE runs SET verdict_outcome = ?, evidence_path = ?, correction_path = ?, deepening = ?
				WHERE id = ?`, outcome, rawNull(evidence), rawNull(correction), rawNull(deepening), runID); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s|%d|%t", status, correctionCount, exception), nil
		})
}

func reenterOp(name string, id int64, notes string) walkOperation {
	return accepted(name, []string{"Reenter"},
		func(ctx context.Context, q domain.Q) (string, error) {
			return "seeded", domain.Reenter(ctx, q, id, notes)
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := q.ExecContext(ctx, `UPDATE tasks SET status = 'seeded', refine_notes = ? WHERE id = ?`, rawNull(notes), id)
			return "seeded", err
		})
}

func blockOp(name string, id int64, reason string) walkOperation {
	return accepted(name, []string{"Block"},
		func(ctx context.Context, q domain.Q) (string, error) {
			return "blocked", domain.Block(ctx, q, id, reason)
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := q.ExecContext(ctx, `UPDATE tasks SET blocked = 1, blocked_reason = ? WHERE id = ?`, reason, id)
			return "blocked", err
		})
}

func unblockOp(name string, id int64) walkOperation {
	return accepted(name, []string{"Unblock"},
		func(ctx context.Context, q domain.Q) (string, error) { return "unblocked", domain.Unblock(ctx, q, id) },
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := q.ExecContext(ctx, `UPDATE tasks SET blocked = 0 WHERE id = ?`, id)
			return "unblocked", err
		})
}

func approveOp(name string, id int64) walkOperation {
	return accepted(name, []string{"Approve"},
		func(ctx context.Context, q domain.Q) (string, error) {
			archived, err := domain.Approve(ctx, q, id)
			return fmt.Sprintf("archived:%t", archived), err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			var branch sql.NullString
			if err := q.QueryRowContext(ctx, `SELECT branch FROM tasks WHERE id = ?`, id).Scan(&branch); err != nil {
				return "", err
			}
			if _, err := q.ExecContext(ctx, `UPDATE tasks SET decision = 'approved', decided_at = datetime('now') WHERE id = ?`, id); err != nil {
				return "", err
			}
			archived := !branch.Valid || branch.String == ""
			if archived {
				if _, err := q.ExecContext(ctx, `UPDATE tasks SET archived = 1 WHERE id = ?`, id); err != nil {
					return "", err
				}
			}
			return fmt.Sprintf("archived:%t", archived), nil
		})
}

func approveBranchlessOp(name string, id int64) walkOperation {
	op := approveOp(name, id)
	op.Coverage = append(op.Coverage, "Approve:branchless")
	return op
}

func approveBranchOp(name string, id int64) walkOperation {
	op := approveOp(name, id)
	op.Coverage = append(op.Coverage, "Approve:branch")
	return op
}

func branchFixtureOp(name string, id int64, branch string) walkOperation {
	apply := func(ctx context.Context, q domain.Q) (string, error) {
		_, err := q.ExecContext(ctx, `
			UPDATE tasks SET branch = ?, verified_sha = COALESCE(verified_sha, 'fixture-sha'), target_ref = 'main'
			WHERE id = ?`, branch, id)
		return "branch:" + branch, err
	}
	return accepted(name, []string{"Fixture:branch"}, apply, apply)
}

func landingFixtureOp(name string, id int64) walkOperation {
	apply := func(ctx context.Context, q domain.Q) (string, error) {
		_, err := q.ExecContext(ctx, `UPDATE tasks SET archived = 1 WHERE id = ?`, id)
		return "landed", err
	}
	return accepted(name, []string{"Fixture:landing"}, apply, apply)
}

func cancelOp(name string, id int64, reason string) walkOperation {
	return cancelLikeOp(name, id, reason, false)
}

func cancelPacketOp(name string, id int64, reason string) walkOperation {
	return cancelLikeOp(name, id, reason, true)
}

func cancelLikeOp(name string, id int64, reason string, packet bool) walkOperation {
	coverage := []string{"Cancel"}
	if packet {
		coverage = []string{"CancelPacket"}
	}
	return accepted(name, coverage,
		func(ctx context.Context, q domain.Q) (string, error) {
			var err error
			if packet {
				err = domain.CancelPacket(ctx, q, id, reason)
			} else {
				err = domain.Cancel(ctx, q, id, reason)
			}
			return "cancelled", err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			if _, err := q.ExecContext(ctx, `
				UPDATE tasks SET decision = 'cancelled', decided_at = datetime('now'), archived = 1
				WHERE id = ?`, id); err != nil {
				return "", err
			}
			_, err := q.ExecContext(ctx, `
				INSERT INTO activity (actor, kind, subject, detail)
				VALUES ('operator', 'task.cancelled', ?, ?)`, id, reason)
			return "cancelled", err
		})
}

func birthWaveOp(name string, initiative int64, children []domain.WaveChild, captured *[]int64) walkOperation {
	return accepted(name, []string{"BirthWave"},
		func(ctx context.Context, q domain.Q) (string, error) {
			ids, err := domain.BirthWave(ctx, q, initiative, children)
			if err == nil && captured != nil {
				*captured = append([]int64(nil), ids...)
			}
			return idsString(ids), err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			var ws string
			if err := q.QueryRowContext(ctx, `SELECT worksource FROM tasks WHERE id = ?`, initiative).Scan(&ws); err != nil {
				return "", err
			}
			ids := make([]int64, 0, len(children))
			for _, child := range children {
				priority := 2
				if child.Priority != nil {
					priority = *child.Priority
				}
				res, err := q.ExecContext(ctx, `
					INSERT INTO tasks (title, description, scope, status, initiative_id,
					                   priority, origin, worksource, target_ref)
					VALUES (?, ?, 'task', 'seeded', ?, ?, 'autonomous', ?, 'main')`,
					child.Title, rawNull(child.Description), initiative, priority, ws)
				if err != nil {
					return "", err
				}
				id, err := res.LastInsertId()
				if err != nil {
					return "", err
				}
				ids = append(ids, id)
			}
			return idsString(ids), nil
		})
}

func deepeningOp(name string, id int64, genuine bool) walkOperation {
	class := "churn"
	if genuine {
		class = "genuine"
	}
	return accepted(name, []string{"ApplyDeepening:" + class},
		func(ctx context.Context, q domain.Q) (string, error) {
			return class, domain.ApplyDeepening(ctx, q, id, genuine)
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			var err error
			if genuine {
				_, err = q.ExecContext(ctx, `UPDATE review_packets SET refine_streak = 0 WHERE task_id = ?`, id)
			} else {
				_, err = q.ExecContext(ctx, `UPDATE review_packets SET refine_streak = refine_streak + 1 WHERE task_id = ?`, id)
			}
			return class, err
		})
}

func occupancyOp(name string) walkOperation {
	return accepted(name, []string{"Occupancy"},
		func(ctx context.Context, q domain.Q) (string, error) {
			n, err := domain.Occupancy(ctx, q)
			return fmt.Sprintf("occupancy:%d", n), err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			var n int
			err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_packets WHERE archived = 0`).Scan(&n)
			return fmt.Sprintf("occupancy:%d", n), err
		})
}

func chargeInfraOp(name string, id int64, reason string) walkOperation {
	return accepted(name, []string{"ChargeInfra"},
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := domain.ChargeInfra(ctx, q, id, reason)
			return fmt.Sprintf("%d|%t", res.Remaining, res.Blocked), err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := rawChargeInfra(ctx, q, id, reason)
			return fmt.Sprintf("%d|%t", res.Remaining, res.Blocked), err
		})
}

func chargeInfraExhaustionOp(name string, id int64, reason string) walkOperation {
	op := chargeInfraOp(name, id, reason)
	op.Coverage = append(op.Coverage, "ChargeInfra:exhaustion")
	return op
}

func retryFixtureOp(name string, id int64, retries int) walkOperation {
	apply := func(ctx context.Context, q domain.Q) (string, error) {
		_, err := q.ExecContext(ctx, `UPDATE tasks SET dispatch_retries = ? WHERE id = ?`, retries, id)
		return fmt.Sprintf("retries:%d", retries), err
	}
	return accepted(name, []string{"Fixture:retry-budget"}, apply, apply)
}

func rawChargeInfra(ctx context.Context, q domain.Q, id int64, reason string) (domain.ChargeResult, error) {
	var out domain.ChargeResult
	var retries, blocked int
	if err := q.QueryRowContext(ctx, `SELECT dispatch_retries, blocked FROM tasks WHERE id = ?`, id).Scan(&retries, &blocked); err != nil {
		return out, err
	}
	remaining := retries - 1
	if remaining < 0 {
		remaining = 0
	}
	if _, err := q.ExecContext(ctx, `UPDATE tasks SET dispatch_retries = ? WHERE id = ?`, remaining, id); err != nil {
		return out, err
	}
	out.Remaining = remaining
	if remaining == 0 && blocked == 0 {
		if _, err := q.ExecContext(ctx, `
			UPDATE tasks SET blocked = 1,
			blocked_reason = 'dispatch retries exhausted (' || ? || ')'
			WHERE id = ? AND blocked = 0`, reason, id); err != nil {
			return out, err
		}
		out.Blocked = true
	}
	return out, nil
}

func claimOp(name, runID, owner string, subject *int64, subjectless bool) walkOperation {
	coverage := []string{"Claim:subject"}
	if subjectless {
		coverage = []string{"Claim:subjectless"}
	}
	args := domain.ClaimArgs{
		RunID: runID, Owner: owner, SubjectID: subject,
		SessionPath: "sessions/" + runID, Binding: "fake", HardDeadlineMinutes: 240,
	}
	return claimArgsOp(name, coverage, args)
}

func claimPoolOp(name, runID string, pool []int64) walkOperation {
	return claimArgsOp(name, []string{"Claim:subjectless", "Claim:pool-snapshot"}, domain.ClaimArgs{
		RunID: runID, Owner: "editor", SessionPath: "sessions/" + runID,
		Binding: "fake", PoolSnapshot: append([]int64(nil), pool...), HardDeadlineMinutes: 240,
	})
}

func claimArgsOp(name string, coverage []string, args domain.ClaimArgs) walkOperation {
	return accepted(name, coverage,
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := domain.Claim(ctx, q, propertyNow, args)
			return pointerString(res.Worksource), err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			var ws any
			var subjectValue any
			var wsString string
			if args.SubjectID != nil {
				subjectValue = *args.SubjectID
				if err := q.QueryRowContext(ctx, `SELECT worksource FROM tasks WHERE id = ?`, *args.SubjectID).Scan(&wsString); err != nil {
					return "", err
				}
				ws = wsString
			}
			acquired := propertyNow.UTC().Format(domain.SpineTime)
			deadline := propertyNow.UTC().Add(time.Duration(args.HardDeadlineMinutes) * time.Minute).Format(domain.SpineTime)
			res, err := q.ExecContext(ctx, `
				UPDATE lock SET run_id = ?, worksource = ?, subject = ?, owner = ?,
				acquired_at = ?, last_heartbeat_at = NULL, hard_deadline_at = ?
				WHERE id = 1 AND run_id IS NULL`, args.RunID, ws, subjectValue, args.Owner, acquired, deadline)
			if err != nil {
				return "", err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return "", fmt.Errorf("raw lease held")
			}
			var pool any
			if args.PoolSnapshot != nil {
				encoded, err := json.Marshal(args.PoolSnapshot)
				if err != nil {
					return "", err
				}
				pool = string(encoded)
			}
			if _, err := q.ExecContext(ctx, `
				INSERT INTO runs (id, tier, role, worksource, subject, session_path, binding, pool_snapshot)
				VALUES (?, 'pipeline', ?, ?, ?, ?, ?, ?)`,
				args.RunID, args.Owner, ws, subjectValue, args.SessionPath, args.Binding, pool); err != nil {
				return "", err
			}
			if args.SubjectID == nil {
				return "<nil>", nil
			}
			return wsString, nil
		})
}

func fenceOp(name, runID string, wantSubject *int64) walkOperation {
	return accepted(name, []string{"Fence"},
		func(ctx context.Context, q domain.Q) (string, error) {
			subject, err := domain.Fence(ctx, q, runID)
			return intPointerString(subject), err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			var gotRun sql.NullString
			var subject sql.NullInt64
			if err := q.QueryRowContext(ctx, `SELECT run_id, subject FROM lock WHERE id = 1`).Scan(&gotRun, &subject); err != nil {
				return "", err
			}
			if !gotRun.Valid || gotRun.String != runID {
				return "", fmt.Errorf("raw stale run")
			}
			if subject.Valid {
				v := subject.Int64
				return intPointerString(&v), nil
			}
			return intPointerString(wantSubject), nil
		})
}

func heartbeatOp(name, runID string) walkOperation {
	return accepted(name, []string{"Heartbeat"},
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := domain.Heartbeat(ctx, q, runID)
			return "heartbeat", err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := q.ExecContext(ctx, `UPDATE lock SET last_heartbeat_at = datetime('now') WHERE id = 1 AND run_id = ?`, runID)
			if err != nil {
				return "", err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return "", fmt.Errorf("raw stale heartbeat")
			}
			return "heartbeat", nil
		})
}

func releaseOp(name, runID string) walkOperation {
	return accepted(name, []string{"Release"},
		func(ctx context.Context, q domain.Q) (string, error) {
			return "released", domain.Release(ctx, q, runID)
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := rawRelease(ctx, q, runID)
			return "released", rowsOne(res, err, "raw stale release")
		})
}

func rawRelease(ctx context.Context, q domain.Q, runID string) (sql.Result, error) {
	return q.ExecContext(ctx, `
		UPDATE lock SET run_id = NULL, worksource = NULL, subject = NULL,
		owner = NULL, acquired_at = NULL, last_heartbeat_at = NULL,
		hard_deadline_at = NULL WHERE id = 1 AND run_id = ?`, runID)
}

func reapOp(name, runID, reason string) walkOperation {
	return accepted(name, []string{"ApplyReap"},
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := domain.ApplyReap(ctx, q, domain.ReapArgs{RunID: runID, Reason: reason})
			return fmt.Sprintf("%t|%t", res.Charged, res.Blocked), err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := rawReap(ctx, q, runID, reason)
			return fmt.Sprintf("%t|%t", res.Charged, res.Blocked), err
		})
}

func subjectlessReapOp(name, runID, reason string) walkOperation {
	op := reapOp(name, runID, reason)
	op.Coverage = append(op.Coverage, "ApplyReap:subjectless")
	return op
}

func reapExhaustionOp(name, runID, reason string) walkOperation {
	op := reapOp(name, runID, reason)
	op.Coverage = append(op.Coverage, "ApplyReap:subject", "ApplyReap:exhaustion")
	return op
}

func rawReap(ctx context.Context, q domain.Q, runID, reason string) (domain.ReapResult, error) {
	var out domain.ReapResult
	var subject sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT subject FROM lock WHERE id = 1 AND run_id = ?`, runID).Scan(&subject); err != nil {
		return out, err
	}
	if _, err := q.ExecContext(ctx, `UPDATE runs SET ended_at = datetime('now'), outcome = 'reaped' WHERE id = ?`, runID); err != nil {
		return out, err
	}
	if subject.Valid {
		charge, err := rawChargeInfra(ctx, q, subject.Int64, reason)
		if err != nil {
			return out, err
		}
		out.Charged, out.Blocked = true, charge.Blocked
	}
	res, err := rawRelease(ctx, q, runID)
	if err != nil {
		return out, err
	}
	return out, rowsOne(res, nil, "raw stale reap release")
}

func rejectedVerdictOp(name string, args domain.VerdictArgs, code string) walkOperation {
	return rejected(name, code, rawMayAccept,
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := domain.ApplyVerdict(ctx, q, args)
			return "", err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			switch args.Outcome {
			case "correct":
				if _, err := q.ExecContext(ctx, `
					UPDATE tasks SET status = 'seeded', correction_count = correction_count + 1
					WHERE id = ?`, args.TaskID); err != nil {
					return "", err
				}
			case "budget-spent":
				if _, err := q.ExecContext(ctx, `
					UPDATE tasks SET status = 'verified', verified_sha = ? WHERE id = ?`,
					args.VerifiedSHA, args.TaskID); err != nil {
					return "", err
				}
			}
			_, err := q.ExecContext(ctx, `
				UPDATE runs SET verdict_outcome = ?, evidence_path = ?, correction_path = ?, deepening = ?
				WHERE id = ?`, args.Outcome, rawNull(args.EvidencePath), rawNull(args.CorrectionPath),
				rawNull(args.Deepening), args.RunID)
			return "raw-verdict", err
		})
}

func overlapWaveRejectedOp(name string, initiative int64, priority *int) walkOperation {
	child := domain.WaveChild{Title: "overlap child", Description: "must roll back", Priority: priority}
	return rejected(name, domain.CodeStrictDrain, rawMayAccept,
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := domain.BirthWave(ctx, q, initiative, []domain.WaveChild{child})
			return "", err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			p := 2
			if priority != nil {
				p = *priority
			}
			var ws string
			if err := q.QueryRowContext(ctx, `SELECT worksource FROM tasks WHERE id = ?`, initiative).Scan(&ws); err != nil {
				return "", err
			}
			_, err := q.ExecContext(ctx, `
				INSERT INTO tasks (title, description, scope, status, initiative_id, priority, origin, worksource, target_ref)
				VALUES (?, ?, 'task', 'seeded', ?, ?, 'autonomous', ?, 'main')`,
				child.Title, child.Description, initiative, p, ws)
			return "raw-wave", err
		})
}

func nonProposedRejectRejectedOp(name string, id int64) walkOperation {
	return rejected(name, domain.CodeIllegalTransition, rawMayAccept,
		func(ctx context.Context, q domain.Q) (string, error) {
			return "", domain.RejectProposal(ctx, q, id, "stale editor snapshot")
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			if _, err := q.ExecContext(ctx, `
				UPDATE tasks SET decision = 'rejected', decided_at = datetime('now'), archived = 1
				WHERE id = ?`, id); err != nil {
				return "", err
			}
			_, err := q.ExecContext(ctx, `
				INSERT INTO activity (actor, kind, subject, detail)
				VALUES ('editor', 'task.rejected', ?, 'stale editor snapshot')`, id)
			return "raw-rejected", err
		})
}

func inactiveWorksourceFixtureOp(name, id string) walkOperation {
	apply := func(ctx context.Context, q domain.Q) (string, error) {
		_, err := q.ExecContext(ctx, `
			INSERT INTO worksources (id, title, kind, status) VALUES (?, ?, 'personal', 'paused')`, id, id)
		return "worksource:" + id, err
	}
	return accepted(name, []string{"Fixture:inactive-worksource"}, apply, apply)
}

func inactiveBirthRejectedOp(name, worksource string, priority *int) walkOperation {
	args := domain.ProposalArgs{
		Title: "inert intake", Scope: "task", Priority: priority,
		Origin: "autonomous", Worksource: worksource,
	}
	return rejected(name, domain.CodeWorksourceInactive, rawMayAccept,
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := domain.BirthProposal(ctx, q, args)
			return "", err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			p := 2
			if priority != nil {
				p = *priority
			}
			_, err := q.ExecContext(ctx, `
				INSERT INTO tasks (title, scope, priority, origin, worksource, target_ref)
				VALUES (?, 'task', ?, 'autonomous', ?, 'main')`, args.Title, p, worksource)
			return "raw-birth", err
		})
}

func cancelPacketMissingRejectedOp(name string, id int64) walkOperation {
	return rejected(name, domain.CodeNotFound, rawMayAccept,
		func(ctx context.Context, q domain.Q) (string, error) {
			return "", domain.CancelPacket(ctx, q, id, "no packet exists")
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			if _, err := q.ExecContext(ctx, `
				UPDATE tasks SET decision = 'cancelled', decided_at = datetime('now'), archived = 1
				WHERE id = ?`, id); err != nil {
				return "", err
			}
			_, err := q.ExecContext(ctx, `
				INSERT INTO activity (actor, kind, subject, detail)
				VALUES ('operator', 'task.cancelled', ?, 'no packet exists')`, id)
			return "raw-cancelled", err
		})
}

func blockedPromoteRejectedOp(name string, id int64) walkOperation {
	return rejected(name, domain.CodeBlocked, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) { return "", domain.Promote(ctx, q, id) },
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := q.ExecContext(ctx, `UPDATE tasks SET status = 'seeded' WHERE id = ?`, id)
			return "", err
		})
}

func strictDrainRejectedOp(name string, id int64) walkOperation {
	return rejected(name, domain.CodeStrictDrain, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) {
			return "", domain.AdvanceStage(ctx, q, id, "worked")
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := q.ExecContext(ctx, `UPDATE tasks SET status = 'worked' WHERE id = ?`, id)
			return "", err
		})
}

// Empty-wave rejection is intentionally domain-only: the substrate has no
// row mutation to backstop. The raw oracle may accept its no-op, but both
// transactions still roll back and the classified asymmetry must stay inert.
func emptyWaveRejectedOp(name string, id int64) walkOperation {
	return rejected(name, domain.CodeEmptyWave, rawMayAccept,
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := domain.BirthWave(ctx, q, id, nil)
			return "", err
		},
		func(context.Context, domain.Q) (string, error) { return "raw-no-op", nil })
}

func unblockParentRejectedOp(name string, id int64) walkOperation {
	return rejected(name, domain.CodeBlockedChild, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) { return "", domain.Unblock(ctx, q, id) },
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := q.ExecContext(ctx, `UPDATE tasks SET blocked = 0 WHERE id = ?`, id)
			return "", err
		})
}

func rejectedDeepeningOp(name string, id int64) walkOperation {
	return rejected(name, domain.CodeSaturated, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) {
			return "", domain.ApplyDeepening(ctx, q, id, true)
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := q.ExecContext(ctx, `UPDATE review_packets SET refine_streak = 0 WHERE task_id = ?`, id)
			return "", err
		})
}

func wipPackageRejectedOp(name string, id int64, render string) walkOperation {
	return packageOp(name, id, render, false)
}

func blockNoReasonRejectedOp(name string, id int64) walkOperation {
	return rejected(name, domain.CodeReasonRequired, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) { return "", domain.Block(ctx, q, id, "") },
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := q.ExecContext(ctx, `UPDATE tasks SET blocked = 1, blocked_reason = NULL WHERE id = ?`, id)
			return "", err
		})
}

func claimHeldRejectedOp(name, runID string, subject *int64) walkOperation {
	args := domain.ClaimArgs{RunID: runID, Owner: "worker", SubjectID: subject, HardDeadlineMinutes: 240}
	return rejected(name, domain.CodeLeaseHeld, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := domain.Claim(ctx, q, propertyNow, args)
			return "", err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := q.ExecContext(ctx, `
				UPDATE lock SET run_id = ?, owner = 'worker', subject = ?, acquired_at = ?, hard_deadline_at = ?
				WHERE id = 1 AND run_id IS NULL`, runID, *subject,
				propertyNow.Format(domain.SpineTime), propertyNow.Add(4*time.Hour).Format(domain.SpineTime))
			return "", rowsOne(res, err, "raw lease held")
		})
}

func staleFenceRejectedOp(name, runID string) walkOperation {
	return rejected(name, domain.CodeStaleRun, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := domain.Fence(ctx, q, runID)
			return "", err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			var n int
			if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM lock WHERE id = 1 AND run_id = ?`, runID).Scan(&n); err != nil {
				return "", err
			}
			if n != 1 {
				return "", fmt.Errorf("raw stale fence")
			}
			return "", nil
		})
}

func staleHeartbeatRejectedOp(name, runID string) walkOperation {
	return rejected(name, domain.CodeStaleRun, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := domain.Heartbeat(ctx, q, runID)
			return "", err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := q.ExecContext(ctx, `UPDATE lock SET last_heartbeat_at = datetime('now') WHERE id = 1 AND run_id = ?`, runID)
			return "", rowsOne(res, err, "raw stale heartbeat")
		})
}

func staleReleaseRejectedOp(name, runID string) walkOperation {
	return rejected(name, domain.CodeStaleRun, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) { return "", domain.Release(ctx, q, runID) },
		func(ctx context.Context, q domain.Q) (string, error) {
			res, err := rawRelease(ctx, q, runID)
			return "", rowsOne(res, err, "raw stale release")
		})
}

func staleReapRejectedOp(name, runID string) walkOperation {
	return rejected(name, domain.CodeStaleRun, rawMustReject,
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := domain.ApplyReap(ctx, q, domain.ReapArgs{RunID: runID, Reason: "stale"})
			return "", err
		},
		func(ctx context.Context, q domain.Q) (string, error) {
			_, err := rawReap(ctx, q, runID, "stale")
			return "", err
		})
}

func rowsOne(result sql.Result, err error, message string) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%s", message)
	}
	return nil
}

func pointerString(v *string) string {
	if v == nil {
		return "<nil>"
	}
	return *v
}

func intPointerString(v *int64) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *v)
}

func idsString(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%d", id)
	}
	return strings.Join(parts, ",")
}

func currentLockRun(t *testing.T, db *sql.DB) string {
	t.Helper()
	var run sql.NullString
	if err := db.QueryRow(`SELECT run_id FROM lock WHERE id = 1`).Scan(&run); err != nil {
		t.Fatalf("load lock run: %v", err)
	}
	return run.String
}

func lockSubject(t *testing.T, db *sql.DB) *int64 {
	t.Helper()
	var subject sql.NullInt64
	if err := db.QueryRow(`SELECT subject FROM lock WHERE id = 1`).Scan(&subject); err != nil {
		t.Fatalf("load lock subject: %v", err)
	}
	if !subject.Valid {
		return nil
	}
	v := subject.Int64
	return &v
}

func snapshotSpine(t *testing.T, db *sql.DB) spineSnapshot {
	t.Helper()
	var out spineSnapshot
	out.Worksources = queryLines(t, db, `SELECT id, title, kind, status FROM worksources ORDER BY id`, 4)
	out.Tasks = queryTaskLines(t, db)
	out.Packets = queryPacketLines(t, db)
	out.Lock = queryLockLine(t, db)
	out.Runs = queryRunLines(t, db)
	out.Activity = queryActivityLines(t, db)
	return out
}

func queryLines(t *testing.T, db *sql.DB, query string, width int) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("snapshot query: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		values := make([]sql.NullString, width)
		args := make([]any, width)
		for i := range values {
			args[i] = &values[i]
		}
		if err := rows.Scan(args...); err != nil {
			t.Fatalf("snapshot scan: %v", err)
		}
		parts := make([]string, width)
		for i := range values {
			parts[i] = normalizedNull(values[i])
		}
		out = append(out, strings.Join(parts, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot rows: %v", err)
	}
	return out
}

func queryTaskLines(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return queryLines(t, db, `
		SELECT CAST(id AS TEXT), title, description, scope, CAST(initiative_id AS TEXT),
		       CAST(priority AS TEXT), status, CAST(stage_rank AS TEXT),
		       CAST(correction_count AS TEXT), CAST(blocked AS TEXT), blocked_reason,
		       CAST(dispatch_retries AS TEXT), decision,
		       CASE WHEN decided_at IS NULL THEN '0' ELSE '1' END,
		       CAST(archived AS TEXT), origin, worksource, branch, verified_sha,
		       target_ref, refine_notes
		FROM tasks ORDER BY id`, 21)
}

func queryPacketLines(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return queryLines(t, db, `
		SELECT CAST(task_id AS TEXT), render_path, CAST(refine_streak AS TEXT),
		       CAST(saturated AS TEXT), CAST(archived AS TEXT)
		FROM review_packets ORDER BY task_id`, 5)
}

func queryLockLine(t *testing.T, db *sql.DB) string {
	t.Helper()
	lines := queryLines(t, db, `
		SELECT CAST(id AS TEXT), run_id, worksource, CAST(subject AS TEXT), owner,
		       acquired_at,
		       CASE WHEN last_heartbeat_at IS NULL THEN '0' ELSE '1' END,
		       hard_deadline_at
		FROM lock ORDER BY id`, 8)
	if len(lines) != 1 {
		t.Fatalf("lock snapshot rows=%d", len(lines))
	}
	return lines[0]
}

func queryRunLines(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return queryLines(t, db, `
		SELECT id, tier, role, worksource, CAST(subject AS TEXT),
		       CASE WHEN ended_at IS NULL THEN '0' ELSE '1' END,
		       outcome, session_path, binding, pool_snapshot, verdict_outcome,
		       evidence_path, correction_path, deepening, output_path
		FROM runs ORDER BY id`, 15)
}

func queryActivityLines(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return queryLines(t, db, `
		SELECT CAST(id AS TEXT), actor, kind, subject, detail
		FROM activity ORDER BY id`, 5)
}

func normalizedNull(v sql.NullString) string {
	if !v.Valid {
		return "<null>"
	}
	return "v:" + v.String
}

func loadTaskFacts(t *testing.T, db *sql.DB) map[int64]taskFact {
	t.Helper()
	rows, err := db.Query(`
		SELECT t.id, t.scope, t.status, t.initiative_id, t.blocked, t.archived,
		       t.decision, t.dispatch_retries, t.correction_count,
		       EXISTS(SELECT 1 FROM review_packets p WHERE p.task_id = t.id),
		       EXISTS(SELECT 1 FROM review_packets p WHERE p.task_id = t.id AND p.archived = 0),
		       COALESCE((SELECT saturated FROM review_packets p WHERE p.task_id = t.id), 0),
		       t.title || '|' || t.scope || '|' || COALESCE(t.initiative_id, -1) ||
		       '|' || t.origin || '|' || t.worksource || '|' || t.created_at
		FROM tasks t ORDER BY t.id`)
	if err != nil {
		t.Fatalf("task facts: %v", err)
	}
	defer rows.Close()
	out := map[int64]taskFact{}
	for rows.Next() {
		var f taskFact
		var blocked, archived, hasPacket, packetLive, saturated int
		if err := rows.Scan(&f.ID, &f.Scope, &f.Status, &f.InitiativeID,
			&blocked, &archived, &f.Decision, &f.Retries, &f.Correction,
			&hasPacket, &packetLive, &saturated, &f.Identity); err != nil {
			t.Fatalf("task facts scan: %v", err)
		}
		f.Blocked, f.Archived = blocked == 1, archived == 1
		f.HasPacket, f.PacketLive, f.Saturated = hasPacket == 1, packetLive == 1, saturated == 1
		out[f.ID] = f
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("task facts rows: %v", err)
	}
	return out
}

func assertNoDeletedRows(t *testing.T, seed int64, step int, op string, before, after spineSnapshot) {
	t.Helper()
	for _, pair := range []struct {
		name       string
		beforeSize int
		afterSize  int
	}{
		{"task", len(before.Tasks), len(after.Tasks)},
		{"packet", len(before.Packets), len(after.Packets)},
		{"run", len(before.Runs), len(after.Runs)},
		{"activity", len(before.Activity), len(after.Activity)},
	} {
		if pair.afterSize < pair.beforeSize {
			t.Fatalf("seed=%d step=%d op=%s deleted %s rows: %d -> %d",
				seed, step, op, pair.name, pair.beforeSize, pair.afterSize)
		}
	}
	for i := range before.Activity {
		if before.Activity[i] != after.Activity[i] {
			t.Fatalf("seed=%d step=%d op=%s rewrote append-only activity row %d", seed, step, op, i)
		}
	}
}

func assertLegalStatusEdges(t *testing.T, seed int64, step int, op string, before, after map[int64]taskFact) {
	t.Helper()
	legal := map[string]bool{
		"proposed->seeded": true, "seeded->worked": true,
		"worked->verified": true, "worked->seeded": true,
		"verified->packaged": true, "packaged->seeded": true,
	}
	for id, old := range before {
		current, ok := after[id]
		if !ok {
			t.Fatalf("seed=%d step=%d op=%s deleted task %d", seed, step, op, id)
		}
		if old.Identity != current.Identity {
			t.Fatalf("seed=%d step=%d op=%s changed task %d identity", seed, step, op, id)
		}
		if old.Status != current.Status && !legal[old.Status+"->"+current.Status] {
			t.Fatalf("seed=%d step=%d op=%s illegal status edge task %d: %s -> %s",
				seed, step, op, id, old.Status, current.Status)
		}
	}
}

func assertSpineInvariants(t *testing.T, db *sql.DB, seed int64, step int, op, label string) {
	t.Helper()
	failCount := func(name, query string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRow(query).Scan(&got); err != nil {
			t.Fatalf("seed=%d step=%d op=%s %s invariant %s query: %v", seed, step, op, label, name, err)
		}
		if got != want {
			t.Fatalf("seed=%d step=%d op=%s %s invariant %s=%d want=%d",
				seed, step, op, label, name, got, want)
		}
	}
	failCount("foreign-keys", `SELECT COUNT(*) FROM pragma_foreign_key_check`, 0)
	failCount("stage-rank", `SELECT COUNT(*) FROM tasks WHERE stage_rank <> CASE status
		WHEN 'packaged' THEN 0 WHEN 'proposed' THEN 1 WHEN 'seeded' THEN 2
		WHEN 'worked' THEN 3 WHEN 'verified' THEN 4 END`, 0)
	failCount("stage-stamp-poison", `SELECT COUNT(*) FROM tasks WHERE stage_entered_at = '1900-01-01 00:00:00'`, 0)
	failCount("blocked-reason", `SELECT COUNT(*) FROM tasks
		WHERE (blocked = 1 AND blocked_reason IS NULL) OR (blocked = 0 AND blocked_reason IS NOT NULL)`, 0)
	failCount("decision-pair", `SELECT COUNT(*) FROM tasks WHERE (decision IS NULL) <> (decided_at IS NULL)`, 0)
	failCount("approved-packaged", `SELECT COUNT(*) FROM tasks WHERE decision = 'approved' AND status <> 'packaged'`, 0)
	failCount("archive-decision", `SELECT COUNT(*) FROM tasks WHERE archived = 1 AND decision IS NULL`, 0)
	failCount("terminal-decisions-archive", `SELECT COUNT(*) FROM tasks
		WHERE decision IN ('rejected','cancelled') AND archived = 0`, 0)
	failCount("blocked-child-propagation", `SELECT COUNT(*) FROM tasks c JOIN tasks p ON p.id = c.initiative_id
		WHERE c.blocked = 1 AND c.archived = 0 AND p.archived = 0 AND p.blocked = 0`, 0)
	failCount("propagated-parent-has-child", `SELECT COUNT(*) FROM tasks p
		WHERE p.blocked_reason LIKE 'blocked child #%'
		AND NOT EXISTS (SELECT 1 FROM tasks c WHERE c.initiative_id = p.id AND c.blocked = 1 AND c.archived = 0)`, 0)
	failCount("archived-initiative-drained", `SELECT COUNT(*) FROM tasks p JOIN tasks c ON c.initiative_id = p.id
		WHERE p.scope = 'initiative' AND p.archived = 1 AND c.archived = 0`, 0)
	failCount("packet-archive-agreement", `SELECT COUNT(*) FROM review_packets p JOIN tasks t ON t.id = p.task_id
		WHERE p.archived <> t.archived`, 0)
	failCount("wip-cap", `SELECT CASE WHEN COUNT(*) > 3 THEN 1 ELSE 0 END FROM review_packets WHERE archived = 0`, 0)
	failCount("saturation-computed", `SELECT COUNT(*) FROM review_packets
		WHERE saturated <> CASE WHEN refine_streak >= 3 THEN 1 ELSE 0 END`, 0)
	failCount("packaging-composite", `SELECT COUNT(*) FROM tasks t
		WHERE t.status = 'packaged' AND t.decision IS NULL AND t.archived = 0
		AND NOT EXISTS (SELECT 1 FROM review_packets p WHERE p.task_id = t.id AND p.archived = 0)`, 0)
	failCount("free-lock-residue", `SELECT COUNT(*) FROM lock WHERE run_id IS NULL AND
		(worksource IS NOT NULL OR subject IS NOT NULL OR owner IS NOT NULL OR acquired_at IS NOT NULL OR
		 last_heartbeat_at IS NOT NULL OR hard_deadline_at IS NOT NULL)`, 0)
	failCount("held-lock-run", `SELECT COUNT(*) FROM lock l WHERE l.run_id IS NOT NULL AND
		NOT EXISTS (SELECT 1 FROM runs r WHERE r.id = l.run_id AND r.role = l.owner AND
		 r.subject IS l.subject AND r.worksource IS l.worksource)`, 0)

	var runID, acquiredAt, deadlineAt sql.NullString
	var deadlineMinutes int
	if err := db.QueryRow(`
		SELECT run_id, acquired_at, hard_deadline_at, hard_deadline_minutes FROM lock WHERE id = 1`).
		Scan(&runID, &acquiredAt, &deadlineAt, &deadlineMinutes); err != nil {
		t.Fatalf("seed=%d step=%d op=%s %s lease arithmetic query: %v", seed, step, op, label, err)
	}
	if runID.Valid {
		acquired, acquiredErr := time.Parse(domain.SpineTime, acquiredAt.String)
		deadline, deadlineErr := time.Parse(domain.SpineTime, deadlineAt.String)
		if !acquiredAt.Valid || !deadlineAt.Valid || acquiredErr != nil || deadlineErr != nil {
			t.Fatalf("seed=%d step=%d op=%s %s invalid lease stamps acquired=%q deadline=%q errors=%v/%v",
				seed, step, op, label, acquiredAt.String, deadlineAt.String, acquiredErr, deadlineErr)
		}
		wantDeadline := acquired.Add(time.Duration(deadlineMinutes) * time.Minute)
		if !deadline.Equal(wantDeadline) {
			t.Fatalf("seed=%d step=%d op=%s %s hard deadline=%s want acquired %s + %dm = %s",
				seed, step, op, label, deadline, acquired, deadlineMinutes, wantDeadline)
		}
	}

	// Occupancy's exported read agrees with the independent count after every
	// operation, including rejected/rolled-back steps.
	var direct, aggregate int
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_packets WHERE archived = 0`).Scan(&direct); err != nil {
		t.Fatalf("occupancy direct: %v", err)
	}
	if err := inImmediate(db, func(ctx context.Context, q domain.Q) error {
		var err error
		aggregate, err = domain.Occupancy(ctx, q)
		return err
	}); err != nil || aggregate != direct {
		t.Fatalf("seed=%d step=%d op=%s %s occupancy domain=%d direct=%d err=%v",
			seed, step, op, label, aggregate, direct, err)
	}
}
