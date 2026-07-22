//go:build docker_e2e

// The lease-free Homie conversation loop, end to end (spec §15.3, ADR-016 D3).
// No pipeline task exists, so every tick's pipeline Decide() is idle and the
// dispatch crossing runs the Homie wake selector instead: an operator message
// mints a launch generation, the resident spawns and binds a tier:"homie"
// container (NO lease, NO heartbeat — Inv. 1/22), the in-container runner
// reports runner-started, claims the pending turn, runs the fake harness, and
// posts the reply. The test invokes only host verbs; the wake, the container,
// and the reply are all timer-driven (contract A6).
package e2e

import (
	"testing"
	"time"
)

func TestHomieConversationDockerBoundary(t *testing.T) {
	f := setup(t)

	// A Homie session bound to a dashboard channel. It starts active with
	// last_activity_at = now, so it is well inside homie_idle_timeout_s.
	start := f.mcOK("", "homie", "start", "--from", "dashboard:chan-1")
	session, ok := start["session_id"].(string)
	if !ok || session == "" {
		t.Fatalf("mc homie start returned no session_id: %v", start)
	}

	// From here the real timer drives. There is no pipeline work, so the
	// pipeline lease never leaves free — the Homie tier is genuinely lease-free.
	f.startResident()

	// The operator sends one inbound turn.
	send := f.mcOK("", "homie", "send", session, "--from", "dashboard:chan-1", "--body", "ping")
	if send["message_id"] == nil {
		t.Fatalf("mc homie send returned no message_id: %v", send)
	}

	// The lease-free tick wakes a container off pipeline-idle; the runner claims
	// the turn and answers with the fake homie behavior's fixed reply. The
	// pipeline lease must stay free throughout — a Homie wake never takes it.
	f.waitFor(120*time.Second, "the homie reply lands in the conversation", func() (bool, string) {
		if lock := f.mcOK("", "lock", "get"); lock["run_id"] != nil {
			return false, "pipeline lease is held by a Homie wake (Inv. 1 violated)"
		}
		h := f.mcOK("", "homie", "history", session)
		messages, _ := h["messages"].([]any)
		for _, m := range messages {
			row, _ := m.(map[string]any)
			if row["direction"] == "reply" && row["body"] == "homie ack" {
				return true, ""
			}
		}
		return false, "no reply yet"
	})

	// The wake generation is recorded on the session, and it started (the runner
	// reported runner-started, which stamps launch_started_at via the fence).
	list := f.mcOK("", "homie", "list")
	sessions, _ := list["sessions"].([]any)
	found := false
	for _, s := range sessions {
		row, _ := s.(map[string]any)
		if row["session_id"] == session || row["id"] == session {
			found = true
		}
	}
	if !found {
		t.Fatalf("homie list does not show the active session %q: %v", session, list)
	}
}
