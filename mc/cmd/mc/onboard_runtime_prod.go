//go:build !test_fake_routing

package main

import (
	"fmt"
	"io"
	"strings"

	"mc/verbs"
)

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
