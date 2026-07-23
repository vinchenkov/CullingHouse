//go:build !test_fake_routing

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"mc/deployment"
	"mc/verbs"
)

// Tests replace this seam. Production constructs the real verifier only after
// canonical MC_HOME is known, because installed runner assets are home-owned.
var productionRuntimeAuthVerifier deployment.RuntimeAuthVerifier

func selectedRuntimeBindings(raw string) ([]string, error) {
	if raw == "" {
		return []string{"chatgpt", "claude", "minimax"}, nil
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if parts[i] == "" {
			return nil, verbs.Usagef("mc onboard runtime-auth --runtime-bindings contains an empty binding")
		}
	}
	return parts, nil
}

func brokerOnboardRuntimeAuth(args []string, stdout, stderr io.Writer) int {
	a, err := parseOnboardArgs(args[1:])
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	if a.Section != "runtime-auth" {
		return writeVerbError(stdout, stderr, verbs.Usagef("usage: mc onboard runtime-auth [source flags]"))
	}
	bindings, err := selectedRuntimeBindings(a.RuntimeBindings)
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	home, err := configuredCanonicalHome()
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("resolve MC_HOME: %v", err))
	}
	verifier := productionRuntimeAuthVerifier
	if verifier == nil {
		verifier = deployment.NewAdapterNoopVerifier(home)
	}
	status, err := deployment.ImportRuntimeAuth(home, deployment.RuntimeAuthSources{
		Bindings: bindings, CodexAuthFile: a.CodexAuthFile,
		ClaudeCredentialsFile: a.ClaudeCredentialsFile,
		MinimaxTokenFile:      a.MinimaxTokenFile, Environment: os.Environ(),
	}, verifier)
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Domainf("runtime-auth import refused: %v", err))
	}
	return writeOnboardSection(stdout, stderr, "runtime-auth", status,
		fmt.Sprintf("%d binding grants passed forbidden-env and live no-op gates and were atomically published", len(bindings)))
}

func brokerOnboardContainer(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "onboard" || args[1] != "container" {
		return writeVerbError(stdout, stderr, verbs.Usagef("usage: mc onboard container"))
	}
	result, err := productionDoctorResult()
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	findings := result.(map[string]any)["findings"].([]verbs.DoctorFinding)
	for _, finding := range findings {
		if (finding.Check == "spine" || finding.Check == "container-runtime") && finding.Status != "ok" {
			return writeVerbError(stdout, stderr, verbs.Domainf("container onboarding failed %s: %s", finding.Check, finding.Detail))
		}
	}
	return writeOnboardSection(stdout, stderr, "container", "ok",
		"exact native image/helper envelope and kernel capability probe are healthy")
}

func brokerOnboardVerify(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "onboard" || args[1] != "verify" {
		return writeVerbError(stdout, stderr, verbs.Usagef("usage: mc onboard verify"))
	}
	result, err := productionDoctorResult()
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	findings := result.(map[string]any)["findings"].([]verbs.DoctorFinding)
	okCount, deferredCount := 0, 0
	failing := []string{}
	for _, finding := range findings {
		switch finding.Status {
		case "ok":
			okCount++
		case "deferred":
			deferredCount++
		case "fail":
			failing = append(failing, fmt.Sprintf("%s (run: mc onboard %s)", finding.Check, finding.OnboardSection))
		}
	}
	if len(failing) != 0 {
		return writeVerbError(stdout, stderr, verbs.Domainf("mc doctor reports failing checks: %s", strings.Join(failing, "; ")))
	}
	return writeOnboardSection(stdout, stderr, "verify", "ok",
		fmt.Sprintf("mc doctor: %d checks ok, %d deferred to Phase 5", okCount, deferredCount))
}
