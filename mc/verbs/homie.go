package verbs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Homie agent verbs are the maximum structural surface that may be frozen
// into a session (§15.3, ADR-001 D6). Reads outside the Homie session-state
// family remain governed by their own read surface and need no allowlist row.
var maximumHomieAgentVerbs = []string{
	"homie.end",
	"homie.history",
	"homie.list",
	"initiative.add",
	"packet.decide",
	"task.add",
	"task.block",
	"task.interrupt",
	"task.unblock",
	"worksource.add",
	"worksource.archive",
	"worksource.pause",
}

type SurfaceRef struct {
	Surface    string
	ChannelRef string
}

func parseSurfaceRef(raw string) (SurfaceRef, error) {
	surface, channel, ok := strings.Cut(raw, ":")
	if !ok {
		return SurfaceRef{}, Usagef("Homie surface reference must be surface:channel_ref (§15.4)")
	}
	switch surface {
	case "discord", "dashboard", "cli":
	default:
		return SurfaceRef{}, Usagef("Homie surface must be discord|dashboard|cli (§15.4)")
	}
	if channel == "" {
		return SurfaceRef{}, Usagef("Homie surface reference requires a non-empty channel_ref (§15.4)")
	}
	return SurfaceRef{Surface: surface, ChannelRef: channel}, nil
}

func canonicalHomieAllowlist(requested *[]string) ([]string, string, error) {
	allow := maximumHomieAgentVerbs
	if requested != nil {
		allow = *requested
	}
	maximum := make(map[string]struct{}, len(maximumHomieAgentVerbs))
	for _, verb := range maximumHomieAgentVerbs {
		maximum[verb] = struct{}{}
	}
	seen := make(map[string]struct{}, len(allow))
	canonical := make([]string, 0, len(allow))
	for _, verb := range allow {
		verb = strings.TrimSpace(verb)
		if verb == "" {
			return nil, "", Usagef("Homie --allow entries must be non-empty; use --allow none for a read-only session")
		}
		if _, ok := maximum[verb]; !ok {
			return nil, "", Usagef("%q is not a Homie-agent verb (ADR-001 D6)", verb)
		}
		if _, duplicate := seen[verb]; duplicate {
			continue
		}
		seen[verb] = struct{}{}
		canonical = append(canonical, verb)
	}
	sort.Strings(canonical)
	b, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", Usagef("encode Homie allowlist: %v", err)
	}
	return canonical, string(b), nil
}

type HomieStartArgs struct {
	From      string
	Allowlist *[]string // nil = the maximum default; non-nil empty = read-only
}

// HomieStart records one lease-free conversational session and its initial
// surface binding. It deliberately returns no spawn action: a later inbound
// commit makes the session eligible and only the resident tick may launch it
// (§15.5, Inv. 2–3).
func HomieStart(db *sql.DB, id *RunIdentity, a HomieStartArgs) (any, error) {
	if err := RequireHostScope(id, "mc homie start"); err != nil {
		return nil, err
	}
	from, err := parseSurfaceRef(a.From)
	if err != nil {
		return nil, err
	}
	_, allowJSON, err := canonicalHomieAllowlist(a.Allowlist)
	if err != nil {
		return nil, err
	}
	route, err := resolveRoleRoute("homie")
	if err != nil {
		return nil, err
	}
	sessionID, err := newRunID()
	if err != nil {
		return nil, err
	}
	sessionID = "h-" + sessionID // disjoint from pipeline run ids and trace paths
	sessionPath := "sessions/" + sessionID
	containerName := "mc-homie-" + sessionID
	binding := route.HistoricalBinding()

	err = inTx(db, func(ctx context.Context, q Q) error {
		var owner string
		err := q.QueryRowContext(ctx, `
			SELECT session_id FROM homie_bindings
			WHERE surface = ? AND channel_ref = ? AND active = 1`,
			from.Surface, from.ChannelRef).Scan(&owner)
		if err == nil {
			return Domainf("%s:%s is already bound to active Homie session %q",
				from.Surface, from.ChannelRef, owner)
		}
		if err != sql.ErrNoRows {
			return err
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO homie_sessions
				(id, container_name, verb_allowlist, session_path, binding)
			VALUES (?, ?, ?, ?, ?)`,
			sessionID, containerName, allowJSON, sessionPath, binding); err != nil {
			return err
		}
		_, err = q.ExecContext(ctx, `
			INSERT INTO homie_bindings (session_id, surface, channel_ref)
			VALUES (?, ?, ?)`, sessionID, from.Surface, from.ChannelRef)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id":   sessionID,
		"status":       "active",
		"session_path": sessionPath,
		"binding":      binding,
		"from":         a.From,
	}, nil
}

// HomieBind appends one binding event to an active session. Same-session
// retries are idempotent; an occupied concrete place is never transferred
// implicitly to another session.
func HomieBind(db *sql.DB, id *RunIdentity, sessionID, rawFrom string) (any, error) {
	if err := RequireHostScope(id, "mc homie bind"); err != nil {
		return nil, err
	}
	from, err := parseSurfaceRef(rawFrom)
	if err != nil {
		return nil, err
	}
	bound := true
	err = inTx(db, func(ctx context.Context, q Q) error {
		var status string
		err := q.QueryRowContext(ctx,
			`SELECT status FROM homie_sessions WHERE id = ?`, sessionID).Scan(&status)
		if err == sql.ErrNoRows {
			return Domainf("unknown Homie session %q", sessionID)
		}
		if err != nil {
			return err
		}
		if status != "active" {
			return Domainf("binding requires an active Homie session; %q is %s (use resume)", sessionID, status)
		}

		var owner string
		err = q.QueryRowContext(ctx, `
			SELECT session_id FROM homie_bindings
			WHERE surface = ? AND channel_ref = ? AND active = 1`,
			from.Surface, from.ChannelRef).Scan(&owner)
		switch {
		case err == nil && owner == sessionID:
			bound = false
			return nil
		case err == nil:
			return Domainf("%s:%s is already bound to active Homie session %q",
				from.Surface, from.ChannelRef, owner)
		case err != sql.ErrNoRows:
			return err
		}
		_, err = q.ExecContext(ctx, `
			INSERT INTO homie_bindings (session_id, surface, channel_ref)
			VALUES (?, ?, ?)`, sessionID, from.Surface, from.ChannelRef)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id": sessionID,
		"surface":    from.Surface, "channel_ref": from.ChannelRef,
		"bound": bound,
	}, nil
}

func sortedUniqueStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	uniq := out[:0]
	for _, value := range out {
		if len(uniq) == 0 || uniq[len(uniq)-1] != value {
			uniq = append(uniq, value)
		}
	}
	return uniq
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// requireActiveHomieVerb fences Homie-agent state access to the canonical
// active registry row and its persisted frozen allowlist. Mutation callers
// invoke this helper inside their write transaction so an ended zombie can
// never race a preflight check (§15.3, §18 deny rule 2).
func requireActiveHomieVerb(ctx context.Context, q Q, id *RunIdentity, verb string) error {
	if id == nil || id.Tier != "homie" || id.RunID == "" {
		return Domainf("%s requires an active Homie session identity", verb)
	}
	var status, persistedJSON string
	err := q.QueryRowContext(ctx, `
		SELECT status, verb_allowlist FROM homie_sessions WHERE id = ?`, id.RunID,
	).Scan(&status, &persistedJSON)
	if err == sql.ErrNoRows {
		return Domainf("unknown Homie session %q", id.RunID)
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return Domainf("Homie session %q is not active (status %s)", id.RunID, status)
	}
	var persisted []string
	if err := json.Unmarshal([]byte(persistedJSON), &persisted); err != nil {
		return fmt.Errorf("parse frozen allowlist for Homie session %s: %w", id.RunID, err)
	}
	persisted = sortedUniqueStrings(persisted)
	envelope := sortedUniqueStrings(id.VerbAllowlist)
	if !equalStrings(persisted, envelope) {
		return Domainf("Homie session %q envelope allowlist does not match its frozen registry value", id.RunID)
	}
	for _, allowed := range persisted {
		if allowed == verb {
			return nil
		}
	}
	return Domainf("Homie session is not allowed operator verb %s (§15.3)", verb)
}

func requireOperatorVerbTx(ctx context.Context, q Q, id *RunIdentity, verb string) error {
	if id == nil { // host/operator provenance
		return nil
	}
	return requireActiveHomieVerb(ctx, q, id, verb)
}

type homieListRow struct {
	ID               string
	Status           string
	CreatedAt        string
	LastActivityAt   string
	ContainerName    string
	VerbAllowlist    []string
	SessionPath      string
	Binding          string
	NativeSessionRef any
	TraceFilename    any
	ActiveBindings   []map[string]any
}

func (r homieListRow) jsonMap() map[string]any {
	return map[string]any{
		"id": r.ID, "status": r.Status,
		"created_at": r.CreatedAt, "last_activity_at": r.LastActivityAt,
		"container_name": r.ContainerName, "verb_allowlist": r.VerbAllowlist,
		"session_path": r.SessionPath, "binding": r.Binding,
		"native_session_ref": r.NativeSessionRef, "trace_filename": r.TraceFilename,
		"active_bindings": r.ActiveBindings,
	}
}

// HomieList is host-all (including ended/reaped resumable rows) or one
// allowlisted Homie agent's own active session (ADR-001 D6).
func HomieList(db *sql.DB, id *RunIdentity) (any, error) {
	where := ""
	args := []any{}
	if id != nil {
		if id.Tier != "homie" {
			return nil, Domainf("mc homie list is host or Homie-agent scope; run.json tier is %q (ADR-001 D6)", id.Tier)
		}
		if err := requireActiveHomieVerb(context.Background(), db, id, "homie.list"); err != nil {
			return nil, classify(err)
		}
		where = " WHERE id = ?"
		args = append(args, id.RunID)
	}
	rows, err := db.Query(`
		SELECT id, status, created_at, last_activity_at, container_name,
		       verb_allowlist, session_path, binding,
		       native_session_ref, trace_filename
		FROM homie_sessions`+where+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, classify(err)
	}
	var sessions []homieListRow
	for rows.Next() {
		var row homieListRow
		var allowJSON string
		var nativeRef, traceFile sql.NullString
		if err := rows.Scan(
			&row.ID, &row.Status, &row.CreatedAt, &row.LastActivityAt,
			&row.ContainerName, &allowJSON, &row.SessionPath, &row.Binding,
			&nativeRef, &traceFile,
		); err != nil {
			rows.Close()
			return nil, classify(err)
		}
		if err := json.Unmarshal([]byte(allowJSON), &row.VerbAllowlist); err != nil {
			rows.Close()
			return nil, classify(fmt.Errorf("parse Homie session %s allowlist: %w", row.ID, err))
		}
		if nativeRef.Valid {
			row.NativeSessionRef = nativeRef.String
		}
		if traceFile.Valid {
			row.TraceFilename = traceFile.String
		}
		sessions = append(sessions, row)
	}
	if err := rows.Close(); err != nil {
		return nil, classify(err)
	}
	if err := rows.Err(); err != nil {
		return nil, classify(err)
	}

	out := make([]map[string]any, 0, len(sessions))
	for i := range sessions {
		bindingRows, err := db.Query(`
			SELECT surface, channel_ref, bound_at FROM homie_bindings
			WHERE session_id = ? AND active = 1
			ORDER BY surface, channel_ref, id`, sessions[i].ID)
		if err != nil {
			return nil, classify(err)
		}
		bindings, err := rowsToMaps(bindingRows)
		bindingRows.Close()
		if err != nil {
			return nil, classify(err)
		}
		sessions[i].ActiveBindings = bindings
		out = append(out, sessions[i].jsonMap())
	}
	return map[string]any{"sessions": out}, nil
}
