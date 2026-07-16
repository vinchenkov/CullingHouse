package verbs

import (
	"context"

	"mc/dispatch"
	"mc/substrate"
)

// PrivateDispatchMountState is the spine-owned, non-secret input the host
// needs to assemble ADR-021 jurisdiction. It is frozen under the preparation
// token and repeated in the private candidate frame; the helper reloads and
// compares it before commit, so profile or Worksource drift is stale.
type PrivateDispatchMountState = substrate.DispatchMountState
type PrivateDispatchWorksource = substrate.DispatchWorksource

func loadDispatchMountState(ctx context.Context, q Q, sp *dispatch.Spawn, rec dispatch.Records) (PrivateDispatchMountState, error) {
	state := PrivateDispatchMountState{Worksources: []PrivateDispatchWorksource{}}
	if sp.SubjectID != nil {
		for _, task := range rec.Tasks {
			if task.ID == *sp.SubjectID {
				state.SelectedWorksource = task.Worksource
				break
			}
		}
		if state.SelectedWorksource == "" {
			return state, Domainf("dispatch: selected subject %d has no Worksource projection", *sp.SubjectID)
		}
	}

	rows, err := substrate.LoadDispatchWorksourceProjection(ctx, q)
	if err != nil {
		return state, err
	}
	state.Worksources = rows
	return state, nil
}
