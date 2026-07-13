package domain

import "context"

// ---------------------------------------------------------------------------
// Review queue (spec §8, §10 step 1; Inv. 18). The queue's *selection* logic
// is the frozen mc/dispatch package; the domain owns only the occupancy read
// the packet birth checks against. The queue suite drives dispatch.Decide as
// a black-box oracle over stored state (contract §1.2).
// ---------------------------------------------------------------------------

// Occupancy is the §10 step-1 count: unarchived packets across all
// Worksources — the single global cap (Inv. 18). Landing-pending packets
// still count: the slot frees on landing success, never at approve time.
func Occupancy(ctx context.Context, q Q) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM review_packets WHERE archived = 0`).Scan(&n)
	return n, err
}
