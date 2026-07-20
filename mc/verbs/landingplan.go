package verbs

// The landing effect class: ADR-017:697-702's closed four-row mount table.
//
// Landing is NOT the agent table and not the setup table. ADR-017:686-687
// makes that separation load-bearing rather than cosmetic, and the sharpest
// edge is access: setup's `/repo/source` is RO (:691) while landing's is RW
// (:699) — "the only grant in the system that gets a real Worksource
// repository RW, intentionally including its primary checkout". The sealed
// task bytes stay reachable only through the RO `/repo/task` row, never
// through that RW alias, which is what the nested `.mission-control` cover
// buys (:700).
//
// This slice ships the GRAMMAR only, per the sealed-landing build order in
// PROGRESS.md: the rows and the destination predicate exist so `mountplan.go`
// stops classifying every `/repo/...` cell as a confused-planner protocol
// error. No producer resolves a landing kind to an authorized typed root yet
// (boundary/typedkind.go's four landing kinds have no jurisdiction entry), so
// every landing request still DENIES — fail-closed, and deliberately inert
// until the lane is turned on as a whole. A half-built lane is the failure
// mode this order exists to avoid.

import "mc/boundary"

// landingPlanRow is one host-bind row of the closed landing table: the typed
// kind it claims, its fixed container destination, its access, and its shape.
// Unlike taskPlanRow there is no task-root-relative path: landing's rows are
// anchored to two different host roots (the real Worksource and the sealed
// task root), so the producer resolves each one separately.
type landingPlanRow struct {
	Kind           boundary.TypedKind
	Dest           string
	Access         boundary.Access
	IsDir          bool
	MustBeEmptyDir bool
	// ResidentMaterialized marks a row whose host source does not exist until
	// the resident writes it, so dispatch never plans it as a bind entry — the
	// same division `/mc/setup.json` already has, where the plan carries a
	// setup INSTRUCTION and the resident writes the file and binds it. The row
	// stays in this table because the table is ADR-017's, not the planner's.
	ResidentMaterialized bool
}

// landingPlanRows returns the complete landing mount table (ADR-017:699-702).
// The order is the ADR's; planMounts re-sorts entries by destination.
func landingPlanRows() []landingPlanRow {
	return []landingPlanRow{
		{Kind: boundary.KindLandingWorksource, Dest: "/repo/source", Access: boundary.AccessRW, IsDir: true},
		{Kind: boundary.KindLandingMissionControlCover, Dest: "/repo/source/.mission-control", Access: boundary.AccessRO, IsDir: true, MustBeEmptyDir: true},
		{Kind: boundary.KindLandingTaskRoot, Dest: "/repo/task", Access: boundary.AccessRO, IsDir: true},
		{Kind: boundary.KindLandingEnvelope, Dest: "/mc/landing.json", Access: boundary.AccessRO, ResidentMaterialized: true},
	}
}
