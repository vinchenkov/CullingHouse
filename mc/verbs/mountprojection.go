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
type PrivateDispatchTaskSetupIdentity = substrate.DispatchTaskSetupIdentity
type PrivateDispatchTaskAssignment = substrate.DispatchTaskAssignment
type PrivateDispatchAcceptedCompletionSeal = substrate.DispatchAcceptedCompletionSeal

func loadDispatchMountState(ctx context.Context, q Q, sp *dispatch.Spawn, rec dispatch.Records) (PrivateDispatchMountState, error) {
	state := PrivateDispatchMountState{
		Worksources:           []PrivateDispatchWorksource{},
		SubjectTaskSetupRoots: []PrivateDispatchTaskSetupIdentity{},
	}
	if sp.SubjectID != nil {
		for _, task := range rec.Tasks {
			if task.ID == *sp.SubjectID {
				state.SelectedWorksource = task.Worksource
				state.SubjectTaskTargetRef = task.TargetRef
				if task.InitiativeID != nil {
					initiativeID := *task.InitiativeID
					state.SubjectInitiativeID = &initiativeID
				}
				break
			}
		}
		if state.SelectedWorksource == "" {
			return state, Domainf("dispatch: selected subject %d has no Worksource projection", *sp.SubjectID)
		}
		// Freeze the durable first-task setup receipt identities for the subject
		// task under the token. The host mount attest admits the on-disk task
		// skeleton into an agent plan only when the resolved root matches one of
		// these (ADR-016 D5); an unattested skeleton is never trusted.
		roots, err := substrate.LoadSubjectTaskSetupRoots(ctx, q, *sp.SubjectID)
		if err != nil {
			return state, err
		}
		state.SubjectTaskSetupRoots = roots
		// Freeze any recorded first-task closure assignment (ADR-016 D5): the
		// spine-free attest leg authors the plan's setup instruction from the
		// frozen state alone, so a fresh run pins the frozen target ref and a
		// retry restates the recorded pins — it can neither re-read the spine
		// nor rebase.
		assignment, err := substrate.LoadSubjectTaskAssignment(ctx, q, *sp.SubjectID)
		if err != nil {
			return state, err
		}
		state.SubjectTaskAssignment = assignment
		seal, err := substrate.LoadSubjectAcceptedCompletionSeal(ctx, q, *sp.SubjectID)
		if err != nil {
			return state, err
		}
		state.SubjectAcceptedCompletionSeal = seal
		rebuild, err := substrate.LoadSubjectAcceptedSealRebuild(ctx, q, *sp.SubjectID)
		if err != nil {
			return state, err
		}
		state.SubjectAcceptedSealRebuild = rebuild
	}

	rows, err := substrate.LoadDispatchWorksourceProjection(ctx, q)
	if err != nil {
		return state, err
	}
	state.Worksources = rows
	return state, nil
}
