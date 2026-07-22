package verbs

import (
	"database/sql"
	"strings"
	"testing"
)

const hbLaunch = "aaaaaaaaaaaaaaaa"

var hbContainer = strings.Repeat("c", 64)

func hbSpine(t *testing.T) *sql.DB {
	t.Helper()
	db := dvSpine(t)
	dvExec(t, db, `
		INSERT INTO homie_sessions (id, container_name, verb_allowlist, session_path, binding)
		VALUES ('sess-1', 'mc-homie-sess-1', '[]', 'sessions/sess-1', 'claude')`)
	dvExec(t, db, `UPDATE homie_sessions
		SET current_launch_id = ?, current_launch_mode = 'fresh' WHERE id = 'sess-1'`, hbLaunch)
	return db
}

func hbBind(t *testing.T, db *sql.DB, session, launch, container string) map[string]any {
	t.Helper()
	res, err := HomieLaunchBind(db, nil, session, launch, container)
	if err != nil {
		t.Fatalf("HomieLaunchBind: %v", err)
	}
	return res.(map[string]any)
}

func TestHomieLaunchBind(t *testing.T) {
	t.Run("binds the exact docker id to the current launch", func(t *testing.T) {
		db := hbSpine(t)
		got := hbBind(t, db, "sess-1", hbLaunch, hbContainer)
		if got["fenced"] != true {
			t.Fatalf("fenced = %v, want true", got["fenced"])
		}
		if got["bound_at"] == nil {
			t.Fatal("bound_at not returned")
		}
		var cid, boundAt sql.NullString
		if err := db.QueryRow(
			`SELECT current_container_id, launch_bound_at FROM homie_sessions WHERE id='sess-1'`,
		).Scan(&cid, &boundAt); err != nil {
			t.Fatal(err)
		}
		if cid.String != hbContainer || !boundAt.Valid {
			t.Fatalf("bind did not persist: container=%q bound_at_valid=%v", cid.String, boundAt.Valid)
		}
	})

	t.Run("idempotent: same launch + same container returns the original receipt", func(t *testing.T) {
		db := hbSpine(t)
		first := hbBind(t, db, "sess-1", hbLaunch, hbContainer)
		second := hbBind(t, db, "sess-1", hbLaunch, hbContainer)
		if second["fenced"] != true || second["bound_at"] != first["bound_at"] {
			t.Fatalf("re-bind not idempotent: %v vs %v", second, first)
		}
	})

	t.Run("a different container id is fenced and rebinds nothing", func(t *testing.T) {
		db := hbSpine(t)
		hbBind(t, db, "sess-1", hbLaunch, hbContainer)
		got := hbBind(t, db, "sess-1", hbLaunch, strings.Repeat("d", 64))
		if got["fenced"] != false {
			t.Fatalf("fenced = %v, want false for a different container", got["fenced"])
		}
		var cid string
		if err := db.QueryRow(`SELECT current_container_id FROM homie_sessions WHERE id='sess-1'`).Scan(&cid); err != nil {
			t.Fatal(err)
		}
		if cid != hbContainer {
			t.Fatalf("a fenced bind changed the container to %q", cid)
		}
	})

	t.Run("a stale launch id is fenced", func(t *testing.T) {
		db := hbSpine(t)
		got := hbBind(t, db, "sess-1", "bbbbbbbbbbbbbbbb", hbContainer)
		if got["fenced"] != false {
			t.Fatalf("fenced = %v, want false for a stale launch", got["fenced"])
		}
	})

	t.Run("an idled session refuses the bind", func(t *testing.T) {
		db := hbSpine(t)
		dvExec(t, db, `UPDATE lock SET homie_idle_timeout_s = 1 WHERE id = 1`)
		dvExec(t, db, `UPDATE homie_sessions SET last_activity_at = datetime('now', '-1 hour') WHERE id = 'sess-1'`)
		got := hbBind(t, db, "sess-1", hbLaunch, hbContainer)
		if got["fenced"] != false {
			t.Fatalf("fenced = %v, want false for an idled session", got["fenced"])
		}
		var cid sql.NullString
		if err := db.QueryRow(`SELECT current_container_id FROM homie_sessions WHERE id='sess-1'`).Scan(&cid); err != nil {
			t.Fatal(err)
		}
		if cid.Valid {
			t.Fatal("an idled session must not bind a container")
		}
	})

	t.Run("unknown session errors", func(t *testing.T) {
		db := hbSpine(t)
		if _, err := HomieLaunchBind(db, nil, "sess-nope", hbLaunch, hbContainer); err == nil {
			t.Fatal("want error for unknown session")
		}
	})

	t.Run("malformed ids refuse", func(t *testing.T) {
		db := hbSpine(t)
		if _, err := HomieLaunchBind(db, nil, "sess-1", "short", hbContainer); err == nil {
			t.Fatal("want usage error for a bad launch id")
		}
		if _, err := HomieLaunchBind(db, nil, "sess-1", hbLaunch, "short"); err == nil {
			t.Fatal("want usage error for a bad container id")
		}
	})
}
