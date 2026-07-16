package verbs

// ADR-016 D1 attest — the mount-plan validator. mc/boundary deliberately
// ships no plan orchestrator: this file owns the composition (trust → parse →
// resolve → jurisdiction-checked per-source authorization → cross-request
// destination uniqueness) and the adapter from *boundary.MountError to a
// typed refusal.Refusal.
//
// Two rules carry the fail-closed posture across the seam:
//
//   - Any rejection aborts the WHOLE plan. No mount is dropped, no access is
//     downgraded (phase3-contract row 169; identity.go:14-15).
//   - Authority is the caller's knowledge, never derived from the error: which
//     source was candidate-authored vs deployment-shared is a fact about the
//     request, and mc/refusal deliberately errors on a guessed one.
//
// No production candidate carries mount requests yet — deriving them from
// Worksource/Profile state is the planner slice behind this one — so
// dispatchAttest skips the call for an empty set and these functions are
// driven by their tests until that slice lands.

import (
	"errors"
	"os"
	"path"
	"strings"

	"mc/boundary"
	"mc/refusal"
)

// maxPlanMounts mirrors ADR-016's frame bound: a plan permits at most 256
// mounts (ADR-016:158-163). The write-side budgeter owns admission; this is
// the seam's own backstop, and it also keeps ItemIndex inside Detail's
// [0,255] domain.
const maxPlanMounts = 256

// mountRequest is one requested bind: the raw source spelling, the requested
// access, and whose authority the request rides on (candidate-authored
// profile/Worksource state vs deployment-shared configuration) — D4's sole
// class discriminator for the mount codes.
type mountRequest struct {
	Source    string
	Access    boundary.Access
	Authority refusal.Authority
}

type mountPlanInputs struct {
	AllowlistPath string
	OwnerUID      int
	Blocked       boundary.BlockPolicy
	Jurisdiction  boundary.Jurisdiction
}

// planMounts authorizes every requested source against the operator's
// allowlist, the blocked floor, and jurisdiction, then requires the derived
// container destinations to be mutually collision-free (equal or
// ancestor-overlapping destinations reject, the same grammar the allowlist's
// own parse-time target set enforces). It returns exactly one of: the full
// ordered authorization set, a classified refusal, or a protocol error.
func planMounts(requests []mountRequest, in mountPlanInputs) ([]boundary.Authorization, *refusal.Refusal, error) {
	if len(requests) == 0 {
		return nil, nil, nil
	}
	if len(requests) > maxPlanMounts {
		return nil, nil, Domainf("a mount plan permits at most %d mounts, got %d (ADR-016 D2)", maxPlanMounts, len(requests))
	}

	// The allowlist stages are request-independent: their two codes are D4's
	// authority-irrelevant carve-out (health whatever the attester says), so
	// no request authority or index attaches.
	if err := boundary.TrustPolicyFile(in.AllowlistPath, in.OwnerUID); err != nil {
		r, aerr := adaptMountError(err, "", nil)
		return nil, r, aerr
	}
	data, err := os.ReadFile(in.AllowlistPath)
	if err != nil {
		// Trusted an instant ago but unreadable now: the file cannot be the
		// one that was vouched for. Same consequence as untrusted.
		r, rerr := refusalForMountError(&boundary.MountError{
			Code: boundary.CodeAllowlistUntrusted, Msg: err.Error()}, "", nil)
		if rerr != nil {
			return nil, nil, rerr
		}
		return nil, &r, nil
	}
	allowlist, err := boundary.ParseMountAllowlist(data)
	if err != nil {
		r, aerr := adaptMountError(err, "", nil)
		return nil, r, aerr
	}
	// ResolveAllowlist fails with authority-DECIDES codes (source_alias for
	// overlapping roots; source_missing/wrong_kind/symlink_escape for a bad
	// root path). The allowlist and its roots are deployment-authored, so
	// the deployment carries the authority and the class lands on health —
	// an empty authority here would be unclassifiable and wedge dispatch on
	// a recoverable operator misconfig (review finding, 2026-07-16).
	resolved, err := boundary.ResolveAllowlist(allowlist)
	if err != nil {
		r, aerr := adaptMountError(err, refusal.AuthorityDeployment, nil)
		return nil, r, aerr
	}

	auths := make([]boundary.Authorization, 0, len(requests))
	destinations := make([]string, 0, len(requests))
	for i, req := range requests {
		i := i
		auth, err := resolved.Authorize(req.Source, req.Access, in.Blocked, in.Jurisdiction)
		if err != nil {
			r, aerr := adaptMountError(err, req.Authority, &i)
			return nil, r, aerr
		}
		dest := path.Join(auth.Target, auth.Suffix)
		for _, prior := range destinations {
			if dest == prior || strings.HasPrefix(dest, prior+"/") || strings.HasPrefix(prior, dest+"/") {
				r, rerr := refusalForMountError(&boundary.MountError{
					Code: boundary.CodeTargetCollision,
					Msg:  "destination " + dest + " collides with " + prior,
				}, req.Authority, &i)
				if rerr != nil {
					return nil, nil, rerr
				}
				return nil, &r, nil
			}
		}
		destinations = append(destinations, dest)
		auths = append(auths, auth)
	}
	return auths, nil, nil
}

// adaptMountError unwraps a boundary rejection into (nil refusal, error) or
// (refusal, nil error) for planMounts' return shape. A non-MountError failure
// is a protocol error, never guessed into a code.
func adaptMountError(err error, authority refusal.Authority, itemIndex *int) (*refusal.Refusal, error) {
	var me *boundary.MountError
	if !errors.As(err, &me) {
		return nil, err
	}
	r, rerr := refusalForMountError(me, authority, itemIndex)
	if rerr != nil {
		return nil, rerr
	}
	return &r, nil
}

// refusalForMountError shapes one coded boundary rejection into the closed
// refusal enums. The pairing table is append-only alongside the boundary code
// set; an undeclared code refuses to shape (fail-closed, the same posture as
// boundary/codes_test.go's anti-drift guard). Msg — hostile by D4's
// definition, it carries raw host paths — rides only in Message, which
// DetailFor drops.
func refusalForMountError(me *boundary.MountError, authority refusal.Authority, itemIndex *int) (refusal.Refusal, error) {
	shape, ok := mountErrorShapes[me.Code]
	if !ok {
		return refusal.Refusal{}, Domainf("boundary code %q has no refusal shape — a new code needs its D4 row before production (ADR-016 D4)", me.Code)
	}
	return refusal.Refusal{
		Code:      me.Code,
		Authority: authority,
		Field:     shape.field,
		Summary:   shape.summary,
		ItemIndex: itemIndex,
		Message:   me.Msg,
	}, nil
}

var mountErrorShapes = map[string]struct {
	field   refusal.Field
	summary refusal.Summary
}{
	boundary.CodeAllowlistUntrusted: {refusal.FieldAllowlist, refusal.SummaryUntrusted},
	boundary.CodeAllowlistInvalid:   {refusal.FieldAllowlist, refusal.SummaryUnparsable},
	boundary.CodeSourceMissing:      {refusal.FieldMountSource, refusal.SummaryMissing},
	boundary.CodeSourceWrongKind:    {refusal.FieldMountSource, refusal.SummaryWrongKind},
	boundary.CodeSourceBlocked:      {refusal.FieldMountSource, refusal.SummaryBlockedFloor},
	boundary.CodeSymlinkEscape:      {refusal.FieldMountSource, refusal.SummaryEscapesRoot},
	boundary.CodeNotAllowlisted:     {refusal.FieldMountSource, refusal.SummaryNotAllowlisted},
	boundary.CodeDeniedRoot:         {refusal.FieldMountSource, refusal.SummaryOutsideJurisdiction},
	boundary.CodeCrossWorksource:    {refusal.FieldMountSource, refusal.SummaryOutsideJurisdiction},
	boundary.CodeRWNotPermitted:     {refusal.FieldMountSource, refusal.SummaryForbidden},
	boundary.CodeTargetInvalid:      {refusal.FieldMountTarget, refusal.SummaryUnparsable},
	boundary.CodeSourceAlias:        {refusal.FieldMountSource, refusal.SummaryCollision},
	boundary.CodeTargetCollision:    {refusal.FieldMountTarget, refusal.SummaryCollision},
	boundary.CodeIdentityChanged:    {refusal.FieldMountSource, refusal.SummaryIdentityChanged},
	boundary.CodeRuntimeUnappliable: {refusal.FieldRuntime, refusal.SummaryUnappliable},
	boundary.CodeGateUnhealthy:      {refusal.FieldGateway, refusal.SummaryUnavailable},
}
