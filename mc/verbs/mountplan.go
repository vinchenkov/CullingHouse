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
	"sort"
	"strconv"
	"strings"
	"syscall"

	"mc/boundary"
	"mc/refusal"
)

// maxPlanMounts mirrors ADR-016's frame bound: a plan permits at most 256
// mounts (ADR-016:158-163). The write-side budgeter owns admission; this is
// the seam's own backstop, and it also keeps ItemIndex inside Detail's
// [0,255] domain.
const maxPlanMounts = 256

// mountClass names which ADR-017 D6 destination family a request belongs to.
// The container path is derived, never operator-chosen: the allowlist target
// is the stable external-root name and the class supplies its fixed prefix.
type mountClass string

const (
	classArtifact        mountClass = "artifact"
	classReference       mountClass = "reference"
	classWorkspaceLegacy mountClass = "workspace"
)

var mountClassPrefixes = map[mountClass]string{
	classArtifact:        "workspace/artifacts",
	classReference:       "workspace/references",
	classWorkspaceLegacy: "workspace",
}

// mountRequest is one requested bind: the raw source spelling, the requested
// access, whose authority the request rides on (candidate-authored
// profile/Worksource state vs deployment-shared configuration) — D4's sole
// class discriminator for the mount codes — and the D6 destination class.
//
// A TYPED request (Kind != KindNone) instead claims one ADR-017 D6 typed
// system row: it bypasses the external allowlist requirement only
// (ADR-017:349-351), is checked against the jurisdiction's authorized typed
// roots (ADR-021 D10), and lands at its fixed table Destination rather than
// a class-derived one. Class is unused on a typed request.
type mountRequest struct {
	Source          string
	Access          boundary.Access
	Authority       refusal.Authority
	Class           mountClass
	Kind            boundary.TypedKind
	Destination     string
	ContentSHA256   string
	RequireEmptyDir bool
}

// PrivateDispatchMountEntry is one authorized bind of the closed plan carrier
// (ADR-016 D6: ordered {logical_id,source,destination,kind,access} with
// captured host identity evidence). Device and inode are decimal strings so a
// JavaScript consumer can carry the plan without 2^53 truncation; the resident
// treats them as opaque and only the host recheck compares them.
//
// Fields are declared in alphabetical json-key order ON PURPOSE: the plan
// rides inside the spawn effect, whose D2 exact-replay path round-trips
// through map[string]any (alphabetical re-encoding). Any other declared order
// breaks byte-for-byte lost-response replay.
type PrivateDispatchMountEntry struct {
	Access          string `json:"access"`
	ContentSHA256   string `json:"content_sha256,omitempty"`
	Destination     string `json:"destination"`
	Device          string `json:"device"`
	Inode           string `json:"inode"`
	Kind            string `json:"kind"`
	LogicalID       string `json:"logical_id"`
	Mode            int    `json:"mode"`
	OwnerUID        int    `json:"owner_uid"`
	RequireEmptyDir bool   `json:"require_empty_dir,omitempty"`
	Source          string `json:"source"`
}

// PrivateDispatchPathIdentity is host identity evidence carried as decimal
// strings so the JavaScript resident never truncates device/inode values.
// Field order is alphabetical because the containing plan is replayed through
// canonical JSON.
type PrivateDispatchPathIdentity struct {
	Canonical string `json:"canonical"`
	Device    string `json:"device"`
	Inode     string `json:"inode"`
	OwnerUID  int    `json:"owner_uid"`
}

// PrivateDispatchTaskSetup is the plan's first-task setup instruction: the
// only knowledge the spine-blind resident may use to write /mc/setup.json
// (branch and worktree name are pure derivations of the task id and never
// travel). Mode and pins restate token-frozen spine state; the object format
// is host evidence probed at attest from the repo's administrative files.
// Field order is alphabetical because the containing plan is replayed through
// canonical JSON; the optional fields use omitempty so fresh instructions
// carry no pin keys and retry instructions no target key.
type PrivateDispatchTaskSetup struct {
	Mode                string `json:"mode"`
	ObjectFormat        string `json:"object_format"`
	PinnedBaseSHA       string `json:"pinned_base_sha,omitempty"`
	PinnedClosureDigest string `json:"pinned_closure_digest,omitempty"`
	PinnedLocalRepoUUID string `json:"pinned_local_repo_uuid,omitempty"`
	TargetRef           string `json:"target_ref,omitempty"`
}

// PrivateDispatchTaskPrecreate is ADR-016 D5/D6's first post-claim setup
// operation. Ordinary setup proves the exact task root absent beneath the
// captured parent; recovery instead carries a receipt-vouched existing root
// for host-only exact-empty. It authorizes only that skeleton operation plus
// the closure setup its Setup instruction names; it does not invent task
// mount rows before setup has inspected the resulting store.
type PrivateDispatchTaskPrecreate struct {
	ChildMode int `json:"child_mode"`
	// RecoverRoot is present only for ADR-016 D6 recovery. It is the exact
	// receipt-vouched root whose inode must be preserved while setup children
	// are scrubbed; ordinary first setup proves this path absent and omits it.
	RecoverRoot   *PrivateDispatchPathIdentity `json:"recover_root,omitempty"`
	Setup         *PrivateDispatchTaskSetup    `json:"setup"`
	TaskID        int64                        `json:"task_id"`
	TasksParent   PrivateDispatchPathIdentity  `json:"tasks_parent"`
	WorkspaceRoot string                       `json:"workspace_root"`
}

// PrivateDispatchAcceptedSealRebuild is D6's downstream setup authority. It
// contains only the task-pointed accepted receipt frozen at prepare; the
// resident derives the host seal source from MC_HOME/seals/<run_id> and must
// re-attest its identity before invoking the fixed setup executor.
type PrivateDispatchAcceptedSealRebuild struct {
	ClosureDigest     string `json:"closure_digest"`
	CompletionRequest string `json:"completion_request_id"`
	Device            string `json:"device"`
	Inode             string `json:"inode"`
	ManifestDigest    string `json:"manifest_digest"`
	ObjectFormat      string `json:"object_format"`
	OwnerUID          int    `json:"owner_uid"`
	RunID             string `json:"run_id"`
	SealedSHA         string `json:"sealed_sha"`
	TaskID            int64  `json:"task_id"`
}

// PrivateDispatchMountPlan is ADR-016 D5's bounded authorization carrier: the
// complete validated plan the attestation and the committed spawn effect
// carry, and the only mount authority the resident may consume. Entries are
// sorted by destination (the declared key of a semantically unordered set).
// The alphabetical field order is load-bearing — see the entry type.
type PrivateDispatchMountPlan struct {
	AcceptedSealRebuild *PrivateDispatchAcceptedSealRebuild `json:"accepted_seal_rebuild,omitempty"`
	Entries             []PrivateDispatchMountEntry         `json:"entries"`
	JurisdictionDigest  string                              `json:"jurisdiction_digest,omitempty"`
	TaskPrecreate       *PrivateDispatchTaskPrecreate       `json:"task_precreate,omitempty"`
	Version             int                                 `json:"version"`
}

type mountPlanInputs struct {
	AllowlistPath string
	OwnerUID      int
	Blocked       boundary.BlockPolicy
	Jurisdiction  boundary.Jurisdiction
}

// planMounts authorizes every requested source against the operator's
// allowlist, the blocked floor, and jurisdiction, then requires the derived
// absolute container destinations to be mutually collision-free (equal or
// ancestor-overlapping destinations reject, the same grammar the allowlist's
// own parse-time target set enforces). It returns exactly one of: the full
// evidence-backed plan entry set sorted by destination, a classified refusal,
// or a protocol error.
func planMounts(requests []mountRequest, in mountPlanInputs) ([]PrivateDispatchMountEntry, *refusal.Refusal, error) {
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

	entries := make([]PrivateDispatchMountEntry, 0, len(requests))
	for i, req := range requests {
		i := i
		var dest, logicalID string
		var identity boundary.SourceIdentity
		access := req.Access
		if req.Kind != boundary.KindNone {
			// The typed arm: the destination is the row's fixed table cell,
			// never derived, and an out-of-table cell is a confused planner —
			// a protocol error, not a plannable row.
			if !validTaskPlanDestination(req.Destination) {
				return nil, nil, Domainf("typed mount request %d destination %q is outside the closed D6 task table", i, req.Destination)
			}
			id, err := boundary.ResolveSource(req.Source)
			if err != nil {
				r, aerr := adaptMountError(err, req.Authority, &i)
				return nil, r, aerr
			}
			if in.Blocked.Rejects(id.RawClean, id.Canonical) {
				r, aerr := adaptMountError(&boundary.MountError{
					Code: boundary.CodeSourceBlocked,
					Msg:  "typed source has a blocked address component",
				}, req.Authority, &i)
				return nil, r, aerr
			}
			if err := in.Jurisdiction.Rejects(id, boundary.TypedClaim{Kind: req.Kind}); err != nil {
				r, aerr := adaptMountError(err, req.Authority, &i)
				return nil, r, aerr
			}
			dest, logicalID, identity = req.Destination, req.Kind.String(), id
		} else {
			prefix, ok := mountClassPrefixes[req.Class]
			if !ok {
				return nil, nil, Domainf("mount request %d carries no destination class (ADR-017 D6)", i)
			}
			auth, err := resolved.Authorize(req.Source, req.Access, in.Blocked, in.Jurisdiction)
			if err != nil {
				r, aerr := adaptMountError(err, req.Authority, &i)
				return nil, r, aerr
			}
			dest = "/" + path.Join(prefix, auth.Target, auth.Suffix)
			logicalID = string(req.Class) + ":" + path.Join(auth.Target, auth.Suffix)
			identity, access = auth.Source, auth.Access
		}
		for _, prior := range entries {
			overlap := dest == prior.Destination
			if strings.HasPrefix(dest, prior.Destination+"/") && !mountOverlapPermitted(prior.Destination, dest) {
				overlap = true
			}
			if strings.HasPrefix(prior.Destination, dest+"/") && !mountOverlapPermitted(dest, prior.Destination) {
				overlap = true
			}
			if overlap {
				r, rerr := refusalForMountError(&boundary.MountError{
					Code: boundary.CodeTargetCollision,
					Msg:  "destination " + dest + " collides with " + prior.Destination,
				}, req.Authority, &i)
				if rerr != nil {
					return nil, nil, rerr
				}
				return nil, &r, nil
			}
		}
		entry, err := planEntry(logicalID, dest, identity, access)
		if err != nil {
			return nil, nil, err
		}
		if req.ContentSHA256 != "" && entry.Kind != "file" {
			return nil, nil, Domainf("mount request %d carries file-content evidence for a non-file", i)
		}
		if req.RequireEmptyDir && entry.Kind != "dir" {
			return nil, nil, Domainf("mount request %d carries empty-directory evidence for a non-directory", i)
		}
		if req.ContentSHA256 != "" && req.RequireEmptyDir {
			return nil, nil, Domainf("mount request %d carries incoherent fixed-shape evidence", i)
		}
		entry.ContentSHA256 = req.ContentSHA256
		entry.RequireEmptyDir = req.RequireEmptyDir
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Destination < entries[j].Destination })
	return entries, nil, nil
}

// validTaskPlanDestination reports whether one destination is a cell of the
// closed ADR-017 D6 standalone-task table. It is grammar-based (the worktree
// segment embeds the task id) so the helper boundary can re-check a plan
// without knowing which task produced it.
func validTaskPlanDestination(dest string) bool {
	switch dest {
	case "/workspace", "/workspace/source", "/workspace/git",
		"/workspace/source/.git", "/workspace/source/.mission-control",
		"/workspace/git/config", "/workspace/git/hooks", "/workspace/git/info",
		"/workspace/git/objects/info", "/workspace/git/objects/pack",
		"/workspace/git/packed-refs", "/workspace/git/shallow":
		return true
	}
	rest, ok := strings.CutPrefix(dest, "/workspace/git/worktrees/")
	if !ok {
		return false
	}
	name, leaf, ok := strings.Cut(rest, "/")
	if !ok || !validTaskWorktreeName(name) {
		return false
	}
	return leaf == "commondir" || leaf == "gitdir" || leaf == "config.worktree"
}

// validTaskWorktreeName accepts exactly `mc-task-<canonical positive
// decimal>` — the pinned administrative worktree name of a task-local
// repository.
func validTaskWorktreeName(name string) bool {
	id, ok := strings.CutPrefix(name, "mc-task-")
	if !ok || id == "" || len(id) > 19 || id[0] == '0' {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// mountOverlapPermitted reports whether a strictly-nested destination pair is
// one of ADR-017 D6's named parent-before-child table edges. The task root
// admits every separately-validated row beneath it; source and git admit
// exactly their fixed cover children. "No other overlap is accepted."
func mountOverlapPermitted(parent, child string) bool {
	if !strings.HasPrefix(child, parent+"/") {
		return false
	}
	switch parent {
	case "/workspace":
		// Every child entry is itself a validated closed destination (a D6
		// task cell or a class-prefixed artifact/reference row), so the root
		// edge only needs strict nesting.
		return true
	case "/workspace/source":
		return child == "/workspace/source/.git" || child == "/workspace/source/.mission-control"
	case "/workspace/git":
		return validTaskPlanDestination(child)
	}
	return false
}

// planEntry captures ADR-016 D5's host identity evidence for one authorized
// source: the canonical resolved path plus the (device, inode, kind, owner,
// mode) identity the pre-create and pre-start rechecks compare.
func planEntry(logicalID, dest string, source boundary.SourceIdentity, access boundary.Access) (PrivateDispatchMountEntry, error) {
	st, ok := source.Info.Sys().(*syscall.Stat_t)
	if !ok {
		return PrivateDispatchMountEntry{}, Domainf("mount source %q has no native identity evidence (ADR-016 D5)", source.Canonical)
	}
	kind := "file"
	if source.IsDir {
		kind = "dir"
	}
	return PrivateDispatchMountEntry{
		LogicalID:   logicalID,
		Source:      source.Canonical,
		Destination: dest,
		Kind:        kind,
		Access:      string(access),
		Device:      strconv.FormatUint(uint64(st.Dev), 10),
		Inode:       strconv.FormatUint(st.Ino, 10),
		OwnerUID:    int(st.Uid),
		Mode:        int(source.Info.Mode().Perm()),
	}, nil
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
