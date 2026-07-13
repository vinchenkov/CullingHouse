package verbs

import (
	"context"
	"database/sql"

	"mc/domain"
)

// InitiativeAdd files operator/Homie intent into the same contrastive pool as
// every task, with initiative scope and a mandatory checkable charter
// (spec §3/§6.1/§18). It does not create a wave or dispatch.
func InitiativeAdd(db *sql.DB, id *RunIdentity, title, worksource, charter string, priority *int) (any, error) {
	if err := RequireOperatorVerb(id, "initiative.add"); err != nil {
		return nil, err
	}
	if title == "" {
		return nil, Usagef("mc initiative add requires a title")
	}
	if worksource == "" {
		return nil, Usagef("mc initiative add requires --worksource")
	}
	if charter == "" {
		return nil, Usagef("mc initiative add requires --charter with checkable success criteria (Inv. 12)")
	}
	var initiativeID int64
	err := inTx(db, func(ctx context.Context, q Q) error {
		var err error
		initiativeID, err = domain.BirthProposal(ctx, q, domain.ProposalArgs{
			Title: title, Description: charter, Scope: "initiative", Priority: priority,
			Origin: "user", Worksource: worksource,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"initiative_id": initiativeID}, nil
}
