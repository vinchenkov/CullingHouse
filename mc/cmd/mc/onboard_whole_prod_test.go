//go:build !test_fake_routing

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func withWholeWizardFakes(t *testing.T, authReady bool) *[]string {
	t.Helper()
	originalRun := productionWholeRunSection
	originalAuth := productionWholeRuntimeAuthReady
	t.Cleanup(func() {
		productionWholeRunSection = originalRun
		productionWholeRuntimeAuthReady = originalAuth
	})
	calls := []string{}
	productionWholeRuntimeAuthReady = func(string, []string) error {
		if authReady {
			return nil
		}
		return errors.New("no canonical grants")
	}
	productionWholeRunSection = func(args []string, _ io.Reader, stdout, _ io.Writer) int {
		section := args[1]
		calls = append(calls, strings.Join(args[1:], " "))
		_ = json.NewEncoder(stdout).Encode(map[string]any{
			"ok":       true,
			"sections": []map[string]any{{"section": section, "status": "ok", "detail": "fixture"}},
		})
		return 0
	}
	return &calls
}

func wholeResult(t *testing.T, args ...string) (int, map[string]any, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := brokerWholeOnboard(append([]string{"onboard"}, args...), strings.NewReader(""), &stdout, &stderr)
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode whole result %q: %v", stdout.String(), err)
	}
	return code, result, stderr.String()
}

func TestWholeWizardStopsAtRuntimeAuthWithoutSpending(t *testing.T) {
	calls := withWholeWizardFakes(t, false)
	code, result, stderr := wholeResult(t)
	if code != 1 || result["stopped_at"] != "runtime-auth" || !strings.Contains(stderr, "no provider turn was spent") {
		t.Fatalf("code=%d result=%v stderr=%q", code, result, stderr)
	}
	if strings.Join(*calls, "|") != "preflight|home" {
		t.Fatalf("sections called after auth gate: %v", *calls)
	}
	rows := result["sections"].([]any)
	if rows[2].(map[string]any)["status"] != "needs-input" {
		t.Fatalf("runtime row=%v", rows[2])
	}
}

func TestWholeWizardPreparesButNeverImplicitlyActivates(t *testing.T) {
	calls := withWholeWizardFakes(t, true)
	code, result, _ := wholeResult(t,
		"--worksource", "primary", "--workspace-root", "/workspace",
		"--console-hour", "9", "--console-minute", "0", "--console-tz", "UTC")
	if code != 1 || result["stopped_at"] != "supervision" {
		t.Fatalf("code=%d result=%v", code, result)
	}
	want := "preflight|home|routing|container|worksource --worksource primary --workspace-root /workspace|tunables|surfaces --console-hour 9 --console-minute 0 --console-tz UTC|supervision"
	if strings.Join(*calls, "|") != want {
		t.Fatalf("calls=%v", *calls)
	}
	rows := result["sections"].([]any)
	if rows[len(rows)-1].(map[string]any)["status"] != "needs-operator" {
		t.Fatalf("supervision row=%v", rows[len(rows)-1])
	}
}

func TestWholeWizardExplicitActivationContinuesToVerify(t *testing.T) {
	calls := withWholeWizardFakes(t, true)
	code, result, stderr := wholeResult(t, "--activate")
	if code != 0 || result["ok"] != true || stderr != "" {
		t.Fatalf("code=%d result=%v stderr=%q", code, result, stderr)
	}
	if got := (*calls)[len(*calls)-2:]; strings.Join(got, "|") != "supervision --activate|verify" {
		t.Fatalf("tail calls=%v", got)
	}
}
