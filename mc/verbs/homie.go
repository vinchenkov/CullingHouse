package verbs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
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
	Surface    string `json:"surface"`
	ChannelRef string `json:"channel_ref"`
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

		bound, err = ensureActiveBinding(ctx, q, sessionID, from)
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

// HomieResume is the ADR-012 record-only status/binding transition: host
// scope only, ended|reaped → active, requiring the immutable native locator
// pair (the §15.4 conversation-rows fallback is a separate explicit arm,
// never an implicit downgrade). No launch effect — re-mounting the folder
// and relaunching the recorded harness is the resident's host authority.
func HomieResume(db *sql.DB, id *RunIdentity, sessionID, rawFrom string) (any, error) {
	if err := RequireHostScope(id, "mc homie resume"); err != nil {
		return nil, err
	}
	from, err := parseSurfaceRef(rawFrom)
	if err != nil {
		return nil, err
	}
	resumed := false
	err = inTx(db, func(ctx context.Context, q Q) error {
		var status string
		var nativeRef, traceFile sql.NullString
		err := q.QueryRowContext(ctx, `
			SELECT status, native_session_ref, trace_filename
			FROM homie_sessions WHERE id = ?`, sessionID,
		).Scan(&status, &nativeRef, &traceFile)
		if err == sql.ErrNoRows {
			return Domainf("unknown Homie session %q", sessionID)
		}
		if err != nil {
			return err
		}
		if status == "active" {
			// Crash-after-commit retry: idempotent iff the requested place
			// is already this session's active binding.
			var owner string
			err := q.QueryRowContext(ctx, `
				SELECT session_id FROM homie_bindings
				WHERE surface = ? AND channel_ref = ? AND active = 1`,
				from.Surface, from.ChannelRef).Scan(&owner)
			if err == nil && owner == sessionID {
				return nil
			}
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			return Domainf("Homie session %q is already active (use bind)", sessionID)
		}
		if !nativeRef.Valid || !traceFile.Valid {
			return Domainf("Homie session %q never registered its native locator pair; "+
				"native continue is impossible and the §15.4 conversation-rows fallback "+
				"is a separate explicit arm (ADR-012)", sessionID)
		}
		// Status flips first: the binding trigger requires an active session.
		if _, err := q.ExecContext(ctx, `
			UPDATE homie_sessions SET status = 'active', last_activity_at = datetime('now')
			WHERE id = ?`, sessionID); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO activity (actor, kind, subject, detail)
			VALUES ('operator', 'homie.resumed', ?, ?)`,
			sessionID, from.Surface+":"+from.ChannelRef); err != nil {
			return err
		}
		if _, err := ensureActiveBinding(ctx, q, sessionID, from); err != nil {
			return err
		}
		resumed = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id": sessionID, "status": "active",
		"surface": from.Surface, "channel_ref": from.ChannelRef,
		"resumed": resumed,
	}, nil
}

func ensureActiveBinding(ctx context.Context, q Q, sessionID string, from SurfaceRef) (bool, error) {
	var owner string
	err := q.QueryRowContext(ctx, `
		SELECT session_id FROM homie_bindings
		WHERE surface = ? AND channel_ref = ? AND active = 1`,
		from.Surface, from.ChannelRef).Scan(&owner)
	switch {
	case err == nil && owner == sessionID:
		return false, nil
	case err == nil:
		return false, Domainf("%s:%s is already bound to active Homie session %q",
			from.Surface, from.ChannelRef, owner)
	case err != sql.ErrNoRows:
		return false, err
	}
	_, err = q.ExecContext(ctx, `
		INSERT INTO homie_bindings (session_id, surface, channel_ref)
		VALUES (?, ?, ?)`, sessionID, from.Surface, from.ChannelRef)
	return true, err
}

func parseAttachments(verb, raw string) ([]string, any, error) {
	if raw == "" {
		return []string{}, nil, nil
	}
	var refs []string
	if err := json.Unmarshal([]byte(raw), &refs); err != nil || refs == nil {
		return nil, nil, Usagef("%s --attachments must be a JSON array of path strings", verb)
	}
	for _, ref := range refs {
		clean := path.Clean(ref)
		if ref == "" || path.IsAbs(ref) || clean != ref || clean == "." ||
			clean == ".." || strings.HasPrefix(clean, "../") {
			return nil, nil, Usagef("attachment references must be normalized relative file-plane paths (§15.5)")
		}
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return nil, nil, Usagef("encode attachment references: %v", err)
	}
	return refs, string(b), nil
}

type HomieSendArgs struct {
	Session     string
	From        string
	Body        string
	Attachments string
}

type homieEchoPayload struct {
	MessageID   int64      `json:"message_id"`
	Seq         int64      `json:"seq"`
	Body        string     `json:"body"`
	Attachments []string   `json:"attachments"`
	Origin      SurfaceRef `json:"origin"`
}

// HomieSend is native-surface transport into one active conversation. The
// inbound record, first-traffic binding, activity timestamp, and echo rows to
// every other binding commit together (§15.5). It is structurally host-only;
// the Homie model never owns inbound transport.
func HomieSend(db *sql.DB, id *RunIdentity, a HomieSendArgs) (any, error) {
	if err := RequireHostScope(id, "mc homie send"); err != nil {
		return nil, err
	}
	from, err := parseSurfaceRef(a.From)
	if err != nil {
		return nil, err
	}
	attachments, attachmentValue, err := parseAttachments("mc homie send", a.Attachments)
	if err != nil {
		return nil, err
	}
	if a.Body == "" && len(attachments) == 0 {
		return nil, Usagef("mc homie send requires a body or attachment (§15.5)")
	}

	var messageID, seq int64
	echoes := 0
	err = inTx(db, func(ctx context.Context, q Q) error {
		var status string
		err := q.QueryRowContext(ctx,
			`SELECT status FROM homie_sessions WHERE id = ?`, a.Session).Scan(&status)
		if err == sql.ErrNoRows {
			return Domainf("unknown Homie session %q", a.Session)
		}
		if err != nil {
			return err
		}
		if status != "active" {
			return Domainf("Homie session %q is %s; use resume before send (§15.4)", a.Session, status)
		}
		if _, err := ensureActiveBinding(ctx, q, a.Session, from); err != nil {
			return err
		}
		if err := q.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(seq), 0) + 1
			FROM conversation_messages WHERE session_id = ?`, a.Session).Scan(&seq); err != nil {
			return err
		}
		inserted, err := q.ExecContext(ctx, `
			INSERT INTO conversation_messages
				(session_id, seq, direction, surface, channel_ref, body, attachments)
			VALUES (?, ?, 'inbound', ?, ?, ?, ?)`,
			a.Session, seq, from.Surface, from.ChannelRef, a.Body, attachmentValue)
		if err != nil {
			return err
		}
		messageID, err = inserted.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE homie_sessions SET last_activity_at = datetime('now')
			WHERE id = ? AND status = 'active'`, a.Session); err != nil {
			return err
		}
		payload, err := json.Marshal(homieEchoPayload{
			MessageID: messageID, Seq: seq, Body: a.Body,
			Attachments: attachments, Origin: from,
		})
		if err != nil {
			return err
		}
		rows, err := q.QueryContext(ctx, `
			SELECT surface, channel_ref FROM homie_bindings
			WHERE session_id = ? AND active = 1
			  AND NOT (surface = ? AND channel_ref = ?)
			ORDER BY surface, channel_ref, id`,
			a.Session, from.Surface, from.ChannelRef)
		if err != nil {
			return err
		}
		var destinations []SurfaceRef
		for rows.Next() {
			var destination SurfaceRef
			if err := rows.Scan(&destination.Surface, &destination.ChannelRef); err != nil {
				rows.Close()
				return err
			}
			destinations = append(destinations, destination)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, destination := range destinations {
			if _, err := q.ExecContext(ctx, `
				INSERT INTO outbox (kind, session_id, surface, channel_ref, payload)
				VALUES ('homie_echo', ?, ?, ?, ?)`,
				a.Session, destination.Surface, destination.ChannelRef, string(payload)); err != nil {
				return err
			}
			echoes++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id": a.Session, "message_id": messageID, "seq": seq,
		"echoes": echoes,
	}, nil
}

// requireHomieRunner fences the ADR-013 transport verbs to the Homie
// runner's own-session identity (§11.5): run.json tier `homie` naming this
// exact session. Like register-session, this is the runner's private scope —
// the frozen verb_allowlist governs only the model's operator verbs and is
// deliberately never consulted here. Host and pipeline callers own no
// conversation transport.
func requireHomieRunner(id *RunIdentity, sessionID, verb string) error {
	if id == nil || id.Tier != "homie" {
		return Domainf("%s is the Homie runner's own-session transport scope (§11.5)", verb)
	}
	if id.RunID == "" || id.RunID != sessionID {
		return Domainf("a Homie runner touches only its own conversation; run.json identifies %q, target is %q (§11.5)", id.RunID, sessionID)
	}
	return nil
}

// requireActiveHomieSessionTx requires the canonical registry row to be live
// inside the caller's write transaction: transport on an ended or reaped
// session is inert (§15.4) — resume is an explicit host transition, never a
// side effect of a zombie runner's traffic.
func requireActiveHomieSessionTx(ctx context.Context, q Q, sessionID string) error {
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
		return Domainf("Homie session %q is %s; transport requires an active session (§15.4)", sessionID, status)
	}
	return nil
}

// HomieClaim atomically claims the next pending inbound turn of the caller's
// own conversation (§11.5). Pending is durable state — direction inbound,
// completed_at NULL — so a fresh runner resumes the exact turn a dead one
// held: an already-claimed pending turn is returned again with its original
// set-once claim stamp. Claiming is bookkeeping, not conversation traffic;
// it never advances last_activity_at.
func HomieClaim(db *sql.DB, id *RunIdentity, sessionID string) (any, error) {
	if err := requireHomieRunner(id, sessionID, "mc homie claim"); err != nil {
		return nil, err
	}
	var message map[string]any
	err := inTx(db, func(ctx context.Context, q Q) error {
		if err := requireActiveHomieSessionTx(ctx, q, sessionID); err != nil {
			return err
		}
		var messageID, seq int64
		var surface, body, createdAt string
		var channelRef, attachmentJSON, claimedAt sql.NullString
		err := q.QueryRowContext(ctx, `
			SELECT id, seq, surface, channel_ref, body, attachments, created_at, claimed_at
			FROM conversation_messages
			WHERE session_id = ? AND direction = 'inbound' AND completed_at IS NULL
			ORDER BY id LIMIT 1`, sessionID,
		).Scan(&messageID, &seq, &surface, &channelRef, &body, &attachmentJSON, &createdAt, &claimedAt)
		if err == sql.ErrNoRows {
			return nil // nothing pending is an ordinary poll outcome
		}
		if err != nil {
			return err
		}
		reclaimed := claimedAt.Valid
		if !reclaimed {
			if _, err := q.ExecContext(ctx, `
				UPDATE conversation_messages SET claimed_by = ?, claimed_at = datetime('now')
				WHERE id = ?`, sessionID, messageID); err != nil {
				return err
			}
			if err := q.QueryRowContext(ctx,
				`SELECT claimed_at FROM conversation_messages WHERE id = ?`, messageID,
			).Scan(&claimedAt); err != nil {
				return err
			}
		}
		attachments := []string{}
		if attachmentJSON.Valid {
			if err := json.Unmarshal([]byte(attachmentJSON.String), &attachments); err != nil {
				return fmt.Errorf("parse attachments for conversation message %d: %w", messageID, err)
			}
		}
		var channel any
		if channelRef.Valid {
			channel = channelRef.String
		}
		message = map[string]any{
			"id": messageID, "seq": seq, "surface": surface, "channel_ref": channel,
			"body": body, "attachments": attachments, "created_at": createdAt,
			"claimed_at": claimedAt.String, "reclaimed": reclaimed,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"session_id": sessionID, "message": message}, nil
}

type HomieReplyArgs struct {
	Session     string
	To          int64
	Body        string
	Attachments string
}

type homieReplyPayload struct {
	MessageID   int64    `json:"message_id"`
	Seq         int64    `json:"seq"`
	Body        string   `json:"body"`
	Attachments []string `json:"attachments"`
	ReplyTo     int64    `json:"reply_to"`
}

// HomieReply is the runner's outbound half of the turn (§11.5, §15.5): one
// transaction appends the reply row against the claimed inbound turn,
// completes the claim, advances activity time, and fans one homie_reply
// outbox row to EVERY active binding (a reply reads identically on every
// bound surface, origin included — unlike an inbound echo). Replay of a
// committed turn is idempotent for the identical logical reply and refused
// for any other: one inbound turn, one logical reply.
func HomieReply(db *sql.DB, id *RunIdentity, a HomieReplyArgs) (any, error) {
	if err := requireHomieRunner(id, a.Session, "mc homie reply"); err != nil {
		return nil, err
	}
	attachments, attachmentValue, err := parseAttachments("mc homie reply", a.Attachments)
	if err != nil {
		return nil, err
	}
	if a.Body == "" && len(attachments) == 0 {
		return nil, Usagef("mc homie reply requires a body or attachment (§15.5)")
	}
	var out map[string]any
	err = inTx(db, func(ctx context.Context, q Q) error {
		if err := requireActiveHomieSessionTx(ctx, q, a.Session); err != nil {
			return err
		}
		var turnSession, direction string
		var claimedBy, completedAt sql.NullString
		err := q.QueryRowContext(ctx, `
			SELECT session_id, direction, claimed_by, completed_at
			FROM conversation_messages WHERE id = ?`, a.To,
		).Scan(&turnSession, &direction, &claimedBy, &completedAt)
		if err == sql.ErrNoRows {
			return Domainf("unknown conversation message %d", a.To)
		}
		if err != nil {
			return err
		}
		if turnSession != a.Session {
			return Domainf("message %d belongs to another conversation (§11.5)", a.To)
		}
		if direction != "inbound" {
			return Domainf("mc homie reply answers an inbound turn; message %d is a %s row", a.To, direction)
		}
		if !claimedBy.Valid {
			return Domainf("inbound turn %d is unclaimed; the runner claims a turn before replying (§11.5)", a.To)
		}
		if completedAt.Valid {
			// Crash-after-commit retry: the turn's one logical reply exists.
			var replyID, replySeq int64
			var replyBody string
			var replyAttachments sql.NullString
			err := q.QueryRowContext(ctx, `
				SELECT id, seq, body, attachments FROM conversation_messages
				WHERE reply_to = ?`, a.To,
			).Scan(&replyID, &replySeq, &replyBody, &replyAttachments)
			if err == sql.ErrNoRows {
				return Domainf("inbound turn %d is completed but carries no reply row; refusing to guess (§11.5)", a.To)
			}
			if err != nil {
				return err
			}
			sameAttachments := (!replyAttachments.Valid && attachmentValue == nil) ||
				(replyAttachments.Valid && attachmentValue != nil && replyAttachments.String == attachmentValue.(string))
			if replyBody != a.Body || !sameAttachments {
				return Domainf("inbound turn %d already has its one logical reply (§11.5)", a.To)
			}
			out = map[string]any{
				"session_id": a.Session, "message_id": replyID, "seq": replySeq,
				"reply_to": a.To, "deliveries": 0, "replied": false,
			}
			return nil
		}

		var seq int64
		if err := q.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(seq), 0) + 1
			FROM conversation_messages WHERE session_id = ?`, a.Session).Scan(&seq); err != nil {
			return err
		}
		inserted, err := q.ExecContext(ctx, `
			INSERT INTO conversation_messages
				(session_id, seq, direction, surface, channel_ref, body, attachments, reply_to)
			VALUES (?, ?, 'reply', 'homie', NULL, ?, ?, ?)`,
			a.Session, seq, a.Body, attachmentValue, a.To)
		if err != nil {
			return err
		}
		replyID, err := inserted.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE conversation_messages SET completed_at = datetime('now')
			WHERE id = ? AND completed_at IS NULL`, a.To); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE homie_sessions SET last_activity_at = datetime('now')
			WHERE id = ? AND status = 'active'`, a.Session); err != nil {
			return err
		}
		payload, err := json.Marshal(homieReplyPayload{
			MessageID: replyID, Seq: seq, Body: a.Body,
			Attachments: attachments, ReplyTo: a.To,
		})
		if err != nil {
			return err
		}
		rows, err := q.QueryContext(ctx, `
			SELECT surface, channel_ref FROM homie_bindings
			WHERE session_id = ? AND active = 1
			ORDER BY surface, channel_ref, id`, a.Session)
		if err != nil {
			return err
		}
		var destinations []SurfaceRef
		for rows.Next() {
			var destination SurfaceRef
			if err := rows.Scan(&destination.Surface, &destination.ChannelRef); err != nil {
				rows.Close()
				return err
			}
			destinations = append(destinations, destination)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		deliveries := 0
		for _, destination := range destinations {
			if _, err := q.ExecContext(ctx, `
				INSERT INTO outbox (kind, session_id, surface, channel_ref, payload)
				VALUES ('homie_reply', ?, ?, ?, ?)`,
				a.Session, destination.Surface, destination.ChannelRef, string(payload)); err != nil {
				return err
			}
			deliveries++
		}
		out = map[string]any{
			"session_id": a.Session, "message_id": replyID, "seq": seq,
			"reply_to": a.To, "deliveries": deliveries, "replied": true,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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

// HomieHistory reads the one durable transcript in stable sequence order.
// Host may read any retained session; a Homie agent is canonical-active,
// allowlisted, and restricted to its own conversation (ADR-001 D6).
func HomieHistory(db *sql.DB, id *RunIdentity, sessionID string) (any, error) {
	if id != nil {
		if id.Tier != "homie" {
			return nil, Domainf("mc homie history is host or Homie-agent scope; run.json tier is %q (ADR-001 D6)", id.Tier)
		}
		if id.RunID != sessionID {
			return nil, Domainf("a Homie agent may read only its own session; caller is %q, target is %q", id.RunID, sessionID)
		}
		if err := requireActiveHomieVerb(context.Background(), db, id, "homie.history"); err != nil {
			return nil, classify(err)
		}
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM homie_sessions WHERE id = ?`, sessionID).Scan(&status); err == sql.ErrNoRows {
		return nil, Domainf("unknown Homie session %q", sessionID)
	} else if err != nil {
		return nil, classify(err)
	}
	rows, err := db.Query(`
		SELECT id, seq, direction, surface, channel_ref, body, attachments, created_at
		FROM conversation_messages WHERE session_id = ? ORDER BY seq, id`, sessionID)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	messages := []map[string]any{}
	for rows.Next() {
		var messageID, seq int64
		var direction, surface, body, createdAt string
		var channelRef, attachmentJSON sql.NullString
		if err := rows.Scan(
			&messageID, &seq, &direction, &surface, &channelRef,
			&body, &attachmentJSON, &createdAt,
		); err != nil {
			return nil, classify(err)
		}
		attachments := []string{}
		if attachmentJSON.Valid {
			if err := json.Unmarshal([]byte(attachmentJSON.String), &attachments); err != nil {
				return nil, classify(fmt.Errorf("parse attachments for conversation message %d: %w", messageID, err))
			}
		}
		var channel any
		if channelRef.Valid {
			channel = channelRef.String
		}
		messages = append(messages, map[string]any{
			"id": messageID, "seq": seq, "direction": direction,
			"surface": surface, "channel_ref": channel, "body": body,
			"attachments": attachments, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, classify(err)
	}
	return map[string]any{
		"session_id": sessionID, "status": status, "messages": messages,
	}, nil
}

// HomieEnd changes only durable conversation state. The substrate trigger
// deactivates bindings; the next resident sweep owns any container stop.
// Host retries are idempotent. A Homie agent may end only its own active,
// canonically allowlisted session.
func HomieEnd(db *sql.DB, id *RunIdentity, sessionID, reason string) (any, error) {
	if reason == "" {
		return nil, Usagef("mc homie end requires --reason")
	}
	if id != nil {
		if id.Tier != "homie" {
			return nil, Domainf("mc homie end is host or Homie-agent scope; run.json tier is %q (ADR-001 D6)", id.Tier)
		}
		if id.RunID != sessionID {
			return nil, Domainf("a Homie agent may end only its own session; caller is %q, target is %q", id.RunID, sessionID)
		}
		if err := RequireOperatorVerb(id, "homie.end"); err != nil {
			return nil, err
		}
	}
	ended := false
	status := ""
	err := inTx(db, func(ctx context.Context, q Q) error {
		if id != nil {
			if err := requireActiveHomieVerb(ctx, q, id, "homie.end"); err != nil {
				return err
			}
		}
		actor := "operator"
		if id != nil {
			actor = "homie"
		}
		var err error
		ended, status, err = homieEndTx(ctx, q, actor, sessionID, reason)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id": sessionID, "status": status, "ended": ended,
	}, nil
}

// homieEndTx is the end transaction's body (ADR-010): `active -> ended` plus
// one `homie.ended` activity carrying the reason, with the substrate trigger
// deactivating the bindings alongside it.
//
// It takes a Q rather than a *sql.DB so that a caller which already holds a
// transaction can end a session inside it. ADR-016 D4's dispatch seam is that
// caller: a policy-invalid Homie must be ended in the *same* transaction that
// refused it, or it stays the repeatedly selected oldest active row and starves
// pipeline work. Opening a second transaction from inside the first would
// deadlock on the spine's single writer.
//
// The caller owns authorization: HomieEnd checks caller scope and the frozen
// verb allowlist before calling in, and the dispatch seam acts as the system.
func homieEndTx(ctx context.Context, q Q, actor, sessionID, reason string) (bool, string, error) {
	if reason == "" {
		return false, "", Usagef("ending a Homie session requires a reason")
	}
	var status string
	err := q.QueryRowContext(ctx,
		`SELECT status FROM homie_sessions WHERE id = ?`, sessionID).Scan(&status)
	if err == sql.ErrNoRows {
		return false, "", Domainf("unknown Homie session %q", sessionID)
	}
	if err != nil {
		return false, "", err
	}
	if status != "active" {
		return false, status, nil // host/surface retry: preserve ended or reaped truth
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE homie_sessions SET status = 'ended' WHERE id = ? AND status = 'active'`,
		sessionID); err != nil {
		return false, "", err
	}
	if _, err := q.ExecContext(ctx, `
		INSERT INTO activity (actor, kind, subject, detail)
		VALUES (?, 'homie.ended', ?, ?)`, actor, sessionID, reason); err != nil {
		return false, "", err
	}
	return true, "ended", nil
}

// HomieLaunchBind is the resident's private pre-start bind receipt (ADR-016
// D3): after creating and inspecting a Homie container but before starting it,
// the resident CASes the exact Docker id onto the session's current launch. It
// is idempotent — the same launch plus the same container id returns the
// original receipt — and fenced: a stale launch, an ended session, or a
// different already-bound container binds nothing and returns fenced:false so
// the resident abandons the container. The non-idle guard refuses to bind a
// launch whose session has idled past homie_idle_timeout_s; that session is
// owed an end, not a container.
func HomieLaunchBind(db *sql.DB, id *RunIdentity, sessionID, launchID, containerID string) (any, error) {
	if err := RequireHostScope(id, "mc homie launch-bind"); err != nil {
		return nil, err
	}
	if !validLowercaseHex(launchID, 16) {
		return nil, Usagef("mc homie launch-bind --launch must be exactly 16 lowercase hex characters (ADR-016 D3)")
	}
	if !validLowercaseHex(containerID, 64) {
		return nil, Usagef("mc homie launch-bind --container-id must be exactly 64 lowercase hex characters")
	}
	result := map[string]any{"session": sessionID, "launch": launchID, "container_id": containerID}
	err := inTx(db, func(ctx context.Context, q Q) error {
		var status string
		var curLaunch, curContainer, boundAt, lastActivity sql.NullString
		var idleTimeout int64
		err := q.QueryRowContext(ctx, `
			SELECT s.status, s.current_launch_id, s.current_container_id,
			       s.launch_bound_at, s.last_activity_at,
			       (SELECT homie_idle_timeout_s FROM lock WHERE id = 1)
			FROM homie_sessions s WHERE s.id = ?`, sessionID).Scan(
			&status, &curLaunch, &curContainer, &boundAt, &lastActivity, &idleTimeout)
		if err == sql.ErrNoRows {
			return Domainf("unknown Homie session %q", sessionID)
		}
		if err != nil {
			return err
		}
		// A late bind from an old launch (session ended, or the current launch
		// moved on) binds nothing (ADR-016 D3: fenced:false).
		if status != "active" || !curLaunch.Valid || curLaunch.String != launchID {
			result["fenced"] = false
			return nil
		}
		// Idempotent success iff already bound to this exact container; a
		// different bound id is fenced.
		if curContainer.Valid {
			result["fenced"] = curContainer.String == containerID
			if curContainer.String == containerID {
				result["bound_at"] = boundAt.String
			}
			return nil
		}
		// The launch must bind while the session is still non-idle.
		var live int
		if err := q.QueryRowContext(ctx, `
			SELECT CASE WHEN datetime('now') < datetime(?, '+' || ? || ' seconds')
			            THEN 1 ELSE 0 END`,
			lastActivity.String, idleTimeout).Scan(&live); err != nil {
			return err
		}
		if live == 0 {
			result["fenced"] = false
			return nil
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE homie_sessions
			SET current_container_id = ?, launch_bound_at = datetime('now')
			WHERE id = ? AND status = 'active' AND current_launch_id = ?
			  AND current_container_id IS NULL`,
			containerID, sessionID, launchID); err != nil {
			return err
		}
		var newBound string
		if err := q.QueryRowContext(ctx,
			`SELECT launch_bound_at FROM homie_sessions WHERE id = ?`, sessionID).Scan(&newBound); err != nil {
			return err
		}
		result["fenced"] = true
		result["bound_at"] = newBound
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// HomieRunnerStarted is the runner's private lifecycle receipt (ADR-016 D3):
// after the native harness has successfully started or resumed, the runner
// CASes launch_started_at onto its own session's bound launch. It is fenced to
// the current launch and its bound container, idempotent (a second call
// returns the original start), and repeats the non-idle guard. It runs under
// the runner's own tier:"homie" identity, never the model's allowlist.
func HomieRunnerStarted(db *sql.DB, id *RunIdentity, sessionID, launchID, containerID string) (any, error) {
	if err := requireHomieRunner(id, sessionID, "mc homie runner-started"); err != nil {
		return nil, err
	}
	if !validLowercaseHex(launchID, 16) {
		return nil, Usagef("mc homie runner-started --launch must be exactly 16 lowercase hex characters (ADR-016 D3)")
	}
	if !validLowercaseHex(containerID, 64) {
		return nil, Usagef("mc homie runner-started --container-id must be exactly 64 lowercase hex characters")
	}
	result := map[string]any{"session": sessionID, "launch": launchID, "container_id": containerID}
	err := inTx(db, func(ctx context.Context, q Q) error {
		var status string
		var curLaunch, curContainer, startedAt, lastActivity sql.NullString
		var idleTimeout int64
		err := q.QueryRowContext(ctx, `
			SELECT s.status, s.current_launch_id, s.current_container_id,
			       s.launch_started_at, s.last_activity_at,
			       (SELECT homie_idle_timeout_s FROM lock WHERE id = 1)
			FROM homie_sessions s WHERE s.id = ?`, sessionID).Scan(
			&status, &curLaunch, &curContainer, &startedAt, &lastActivity, &idleTimeout)
		if err == sql.ErrNoRows {
			return Domainf("unknown Homie session %q", sessionID)
		}
		if err != nil {
			return err
		}
		// A start from an old launch or a container that is not the bound one
		// stamps nothing (ADR-016 D3: fenced:false).
		if status != "active" || !curLaunch.Valid || curLaunch.String != launchID ||
			!curContainer.Valid || curContainer.String != containerID {
			result["fenced"] = false
			return nil
		}
		// Idempotent: already started returns the original stamp.
		if startedAt.Valid {
			result["fenced"] = true
			result["started_at"] = startedAt.String
			return nil
		}
		var live int
		if err := q.QueryRowContext(ctx, `
			SELECT CASE WHEN datetime('now') < datetime(?, '+' || ? || ' seconds')
			            THEN 1 ELSE 0 END`,
			lastActivity.String, idleTimeout).Scan(&live); err != nil {
			return err
		}
		if live == 0 {
			result["fenced"] = false
			return nil
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE homie_sessions SET launch_started_at = datetime('now')
			WHERE id = ? AND status = 'active' AND current_launch_id = ?
			  AND current_container_id = ? AND launch_started_at IS NULL`,
			sessionID, launchID, containerID); err != nil {
			return err
		}
		var newStarted string
		if err := q.QueryRowContext(ctx,
			`SELECT launch_started_at FROM homie_sessions WHERE id = ?`, sessionID).Scan(&newStarted); err != nil {
			return err
		}
		result["fenced"] = true
		result["started_at"] = newStarted
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// HomieExit is the resident-observed exit receipt (ADR-016 D3), private host
// scope: `mc homie exit <session> --launch <id> --container-id <id> --reason`.
// It CASes the current active launch and its bound container and is
// idempotent. If the launch established anything — the runner started, or
// native locators were registered — it ends the exact session. A truly
// pre-runner-start null-locator launch instead clears the confirmed-exited
// bound pair and retains the same launch/mode as effect debt, so the selector
// re-launches a fresh container for the same generation. A late exit from an
// old launch (or an already-processed session) is fenced:false and changes
// nothing.
func HomieExit(db *sql.DB, id *RunIdentity, sessionID, launchID, containerID, reason string) (any, error) {
	if err := RequireHostScope(id, "mc homie exit"); err != nil {
		return nil, err
	}
	if !validLowercaseHex(launchID, 16) {
		return nil, Usagef("mc homie exit --launch must be exactly 16 lowercase hex characters (ADR-016 D3)")
	}
	if !validLowercaseHex(containerID, 64) {
		return nil, Usagef("mc homie exit --container-id must be exactly 64 lowercase hex characters")
	}
	if reason == "" {
		return nil, Usagef("mc homie exit requires --reason")
	}
	result := map[string]any{"session": sessionID, "launch": launchID, "container_id": containerID, "reason": reason}
	err := inTx(db, func(ctx context.Context, q Q) error {
		var status string
		var curLaunch, curContainer, startedAt, nativeRef sql.NullString
		err := q.QueryRowContext(ctx, `
			SELECT status, current_launch_id, current_container_id,
			       launch_started_at, native_session_ref
			FROM homie_sessions WHERE id = ?`, sessionID).Scan(
			&status, &curLaunch, &curContainer, &startedAt, &nativeRef)
		if err == sql.ErrNoRows {
			return Domainf("unknown Homie session %q", sessionID)
		}
		if err != nil {
			return err
		}
		// A late exit from an old launch, or an already-processed session,
		// changes nothing (ADR-016 D3: fenced:false, idempotent).
		if status != "active" || !curLaunch.Valid || curLaunch.String != launchID ||
			!curContainer.Valid || curContainer.String != containerID {
			result["fenced"] = false
			return nil
		}
		if startedAt.Valid || nativeRef.Valid {
			// The launch established something: end the exact session.
			if _, _, err := homieEndTx(ctx, q, "resident", sessionID, reason); err != nil {
				return err
			}
			result["fenced"] = true
			result["ended"] = true
			return nil
		}
		// Pre-runner-start null-locator launch: clear the confirmed-exited bound
		// pair; the launch/mode stays as effect debt for the selector to retry.
		if _, err := q.ExecContext(ctx, `
			UPDATE homie_sessions SET current_container_id = NULL, launch_bound_at = NULL
			WHERE id = ? AND status = 'active' AND current_launch_id = ? AND current_container_id = ?`,
			sessionID, launchID, containerID); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO activity (actor, kind, subject, detail)
			VALUES ('resident', 'homie.launch_cleared', ?, ?)`, sessionID, reason); err != nil {
			return err
		}
		result["fenced"] = true
		result["ended"] = false
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
