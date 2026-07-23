//go:build !test_fake_routing

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"mc/deployment"
	"mc/verbs"
)

var productionWholeRunSection func(args []string, stdin io.Reader, stdout, stderr io.Writer) int

var productionWholeRuntimeAuthReady = func(home string, bindings []string) error {
	return deployment.ValidateRuntimeGrantStore(home, bindings, os.Environ())
}

func wholeSectionArgs(section string, a verbs.OnboardArgs) []string {
	args := []string{"onboard", section}
	switch section {
	case "home":
		if a.ReleaseSource != "" {
			args = append(args, "--release-source", a.ReleaseSource)
		}
		if a.HostReleaseSource != "" {
			args = append(args, "--host-release-source", a.HostReleaseSource)
		}
	case "runtime-auth":
		if a.RuntimeBindings != "" {
			args = append(args, "--runtime-bindings", a.RuntimeBindings)
		}
		if a.CodexAuthFile != "" {
			args = append(args, "--codex-auth-file", a.CodexAuthFile)
		}
		if a.ClaudeCredentialsFile != "" {
			args = append(args, "--claude-credentials-file", a.ClaudeCredentialsFile)
		}
		if a.MinimaxTokenFile != "" {
			args = append(args, "--minimax-token-file", a.MinimaxTokenFile)
		}
		if a.AcquireRuntimeAuth {
			args = append(args, "--acquire")
		}
	case "worksource":
		if a.Worksource != "" {
			args = append(args, "--worksource", a.Worksource)
		}
		if a.WorkspaceRoot != "" {
			args = append(args, "--workspace-root", a.WorkspaceRoot)
		}
	case "tunables":
		for _, item := range []struct {
			name  string
			value int
		}{
			{"timeout-minutes", a.TimeoutMinutes}, {"grace-minutes", a.GraceMinutes},
			{"heartbeat-interval-s", a.HeartbeatIntervalS}, {"spawn-grace-s", a.SpawnGraceS},
			{"hard-deadline-minutes", a.HardDeadlineMinutes},
		} {
			if item.value != 0 {
				args = append(args, "--"+item.name, fmt.Sprint(item.value))
			}
		}
	case "surfaces":
		if a.ConsoleScheduleSet {
			args = append(args, "--console-hour", fmt.Sprint(a.ConsoleHour),
				"--console-minute", fmt.Sprint(a.ConsoleMinute), "--console-tz", a.ConsoleTZ)
		}
	case "supervision":
		if a.ActivateSupervision {
			args = append(args, "--activate")
		}
	}
	return args
}

func appendWholeSection(rows []map[string]any, section string, a verbs.OnboardArgs, stdin io.Reader, stderr io.Writer) ([]map[string]any, int) {
	var out, diagnostics bytes.Buffer
	runner := productionWholeRunSection
	if runner == nil {
		runner = run
	}
	code := runner(wholeSectionArgs(section, a), stdin, &out, &diagnostics)
	if diagnostics.Len() != 0 {
		_, _ = stderr.Write(diagnostics.Bytes())
	}
	if code != 0 {
		detail := strings.TrimSpace(diagnostics.String())
		if detail == "" {
			detail = fmt.Sprintf("section exited %d", code)
		}
		return append(rows, map[string]any{"section": section, "status": "fail", "detail": detail}), code
	}
	var result struct {
		OK       bool             `json:"ok"`
		Sections []map[string]any `json:"sections"`
	}
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	dec.DisallowUnknownFields()
	var trailing any
	decodeErr := dec.Decode(&result)
	trailingErr := dec.Decode(&trailing)
	if decodeErr != nil || trailingErr != io.EOF || !result.OK || len(result.Sections) != 1 || result.Sections[0]["section"] != section {
		return append(rows, map[string]any{"section": section, "status": "fail", "detail": "section returned an invalid wizard receipt"}), 2
	}
	return append(rows, result.Sections[0]), 0
}

func writeWholeOnboardResult(stdout, stderr io.Writer, rows []map[string]any, stoppedAt, detail string, code int) int {
	result := map[string]any{"ok": code == 0, "sections": rows}
	if stoppedAt != "" {
		result["stopped_at"] = stoppedAt
		result["detail"] = detail
	}
	if err := writeJSON(stdout, result); err != nil {
		fmt.Fprintln(stderr, "mc:", err)
		return 2
	}
	if detail != "" {
		fmt.Fprintln(stderr, "mc:", detail)
	}
	return code
}

func brokerWholeOnboard(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a, err := parseOnboardArgs(args[1:])
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	if a.Section != "" {
		return writeVerbError(stdout, stderr, verbs.Usagef("whole onboarding received an unexpected section"))
	}
	if a.Smoke {
		return writeVerbError(stdout, stderr, verbs.Domainf("production full-pipeline smoke is not implemented yet"))
	}
	rows := []map[string]any{}
	for _, section := range []string{"preflight", "home"} {
		var code int
		rows, code = appendWholeSection(rows, section, a, stdin, stderr)
		if code != 0 {
			return writeWholeOnboardResult(stdout, stderr, rows, section, "onboarding stopped at the failing section", code)
		}
	}

	bindings, err := selectedRuntimeBindings(a.RuntimeBindings)
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	hasRuntimeInput := a.AcquireRuntimeAuth || a.CodexAuthFile != "" || a.ClaudeCredentialsFile != "" || a.MinimaxTokenFile != ""
	if hasRuntimeInput {
		var code int
		rows, code = appendWholeSection(rows, "runtime-auth", a, stdin, stderr)
		if code != 0 {
			return writeWholeOnboardResult(stdout, stderr, rows, "runtime-auth", "runtime authentication did not pass its live gates", code)
		}
	} else {
		home, homeErr := configuredCanonicalHome()
		if homeErr != nil || productionWholeRuntimeAuthReady(home, bindings) != nil {
			detail := "runtime authentication needs provider-owned subscription login and the MiniMax subscription key; no provider turn was spent"
			rows = append(rows, map[string]any{"section": "runtime-auth", "status": "needs-input", "detail": detail})
			return writeWholeOnboardResult(stdout, stderr, rows, "runtime-auth", detail, 1)
		}
		rows = append(rows, map[string]any{
			"section": "runtime-auth", "status": "ok",
			"detail": "canonical grant set and forbidden environment are structurally healthy; no replay token was spent",
		})
	}

	for _, section := range []string{"routing", "container", "worksource", "tunables", "surfaces", "supervision"} {
		var code int
		rows, code = appendWholeSection(rows, section, a, stdin, stderr)
		if code != 0 {
			return writeWholeOnboardResult(stdout, stderr, rows, section, "onboarding stopped at the failing section", code)
		}
	}
	if !a.ActivateSupervision {
		detail := "supervision is prepared and verified unloaded; activation and real-tick observation require the operator-present --activate gate"
		rows[len(rows)-1]["status"] = "needs-operator"
		rows[len(rows)-1]["detail"] = detail
		return writeWholeOnboardResult(stdout, stderr, rows, "supervision", detail, 1)
	}
	rows, code := appendWholeSection(rows, "verify", a, stdin, stderr)
	if code != 0 {
		return writeWholeOnboardResult(stdout, stderr, rows, "verify", "final verification failed", code)
	}
	return writeWholeOnboardResult(stdout, stderr, rows, "", "", 0)
}
