package verbs

import (
	"context"
	"database/sql"
	"encoding/json"
	"path"
	"strings"
)

// alertDestination is resolved by trusted Mission Control code, never by the
// Strategist. Phase 2 has no config.toml layer yet (phase2-contract A-P2-5),
// so the only enabled surface is the spec's always-on dashboard. Onboarding
// replaces this resolver with the configured alert-class route in Phase 5;
// the terminal and outbox schema do not change.
type alertDestination struct {
	surface    string
	channelRef *string
}

func consoleDestinations() []alertDestination {
	return []alertDestination{{surface: "dashboard"}}
}

// ConsolePublish is Strategist(console)'s terminal (ADR-001 D4). The caller
// must own the subjectless lease. One transaction records the suppression
// event, fans the file-plane path out to every trusted destination, ends the
// Run, and releases the lease (§14, §15.5).
func ConsolePublish(db *sql.DB, id *RunIdentity, run, content string) (any, error) {
	if err := requireExactRole(id, "strategist(console)"); err != nil {
		return nil, err
	}
	if err := requireOwnRun(id, run); err != nil {
		return nil, err
	}
	clean := path.Clean(content)
	if content == "" || clean != content || clean == "outputs" ||
		strings.HasPrefix(clean, "../") || !strings.HasPrefix(clean, "outputs/") {
		return nil, Domainf("mc console publish --content must be a normalized relative path beneath outputs/ (§14)")
	}
	payload, err := json.Marshal(map[string]string{"content_path": content})
	if err != nil {
		return nil, Usagef("encode Console outbox payload: %v", err)
	}
	destinations := consoleDestinations()

	err = inTx(db, func(ctx context.Context, q Q) error {
		subject, err := fenceRun(ctx, q, run)
		if err != nil {
			return err
		}
		if subject != nil {
			return Domainf("Strategist(console) requires a subjectless lease; run %s carries task %d (ADR-001 D4)", run, *subject)
		}
		if len(destinations) == 0 {
			return Domainf("Daily Console has no configured destination (§16.3)")
		}

		if _, err := q.ExecContext(ctx, `
			INSERT INTO activity (actor, kind, subject, detail)
			VALUES ('strategist(console)', 'daily.briefing', ?, ?)`, run, content); err != nil {
			return err
		}
		for _, destination := range destinations {
			if _, err := q.ExecContext(ctx, `
				INSERT INTO outbox (kind, surface, channel_ref, payload)
				VALUES ('console', ?, ?, ?)`,
				destination.surface, destination.channelRef, string(payload)); err != nil {
				return err
			}
		}
		if err := endRun(ctx, q, run, "completed"); err != nil {
			return err
		}
		return releaseLease(ctx, q, run)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content_path": content,
		"destinations": len(destinations),
	}, nil
}
