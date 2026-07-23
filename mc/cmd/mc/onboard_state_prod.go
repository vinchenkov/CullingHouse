//go:build !test_fake_routing

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mc/deployment"
	"mc/verbs"
)

const privateOnboardStateOutputLimit = 1 << 20

func brokerOnboardState(args []string, stdout, stderr io.Writer) int {
	a, err := parseOnboardArgs(args[1:])
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	req, err := verbs.PrepareOnboardState(a, releaseBuildID, gatewayControlVersion, configSchemaVersion)
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	frame, err := json.Marshal(req)
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("encode private onboard-state frame: %v", err))
	}
	helper, err := ensureRuntimeHelper()
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("ensure warm helper: %v", err))
	}
	if err := verbs.RecheckOnboardStateHost(req); err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	privateOut := boundedDoctorBuffer{limit: privateOnboardStateOutputLimit}
	privateErr := boundedDoctorBuffer{limit: privateDoctorOutputLimit}
	cmd := exec.Command("docker", "exec", "-i", helper, "mc", "__onboard-state")
	cmd.Stdin = bytes.NewReader(frame)
	cmd.Stdout = &privateOut
	cmd.Stderr = &privateErr
	if err := cmd.Run(); err != nil {
		if privateErr.Len() > 0 {
			_, _ = stderr.Write(privateErr.Bytes())
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && !privateOut.overflow {
			_, _ = stdout.Write(privateOut.Bytes())
			return exit.ExitCode()
		}
		return writeVerbError(stdout, stderr, verbs.Usagef("private onboard-state crossing failed: %v", err))
	}
	if privateOut.overflow || privateErr.overflow {
		return writeVerbError(stdout, stderr, verbs.Usagef("private onboard-state response exceeded its bound"))
	}
	var result verbs.OnboardStateResult
	dec := json.NewDecoder(bytes.NewReader(privateOut.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("decode private onboard-state response: %v", err))
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return writeVerbError(stdout, stderr, verbs.Usagef("private onboard-state response carries trailing data"))
	}
	status, detail, err := verbs.FinalizeOnboardState(req, result)
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	if a.Section == "routing" {
		status, detail, err = verbs.OnboardRoutingHost()
		if err != nil {
			return writeVerbError(stdout, stderr, err)
		}
	}
	if a.Section == "supervision" {
		home, err := configuredCanonicalHome()
		if err != nil {
			return writeVerbError(stdout, stderr, verbs.Usagef("resolve supervision MC_HOME: %v", err))
		}
		mcPath, err := os.Executable()
		if err != nil {
			return writeVerbError(stdout, stderr, verbs.Usagef("resolve installed mc binary: %v", err))
		}
		bunPath, err := exec.LookPath("bun")
		if err != nil {
			return writeVerbError(stdout, stderr, verbs.Usagef("resolve bun for native supervision: %v", err))
		}
		dockerPath, err := exec.LookPath("docker")
		if err != nil {
			return writeVerbError(stdout, stderr, verbs.Usagef("resolve docker for native supervision: %v", err))
		}
		for target, value := range map[string]*string{"mc": &mcPath, "bun": &bunPath, "docker": &dockerPath} {
			if !filepath.IsAbs(*value) {
				absolute, absErr := filepath.Abs(*value)
				if absErr != nil {
					return writeVerbError(stdout, stderr, verbs.Usagef("resolve %s executable path: %v", target, absErr))
				}
				*value = absolute
			}
		}
		names, err := deployment.RuntimeNames(home)
		if err != nil {
			return writeVerbError(stdout, stderr, verbs.Usagef("derive supervision runtime names: %v", err))
		}
		roots := make([]deployment.WorkspaceRoot, len(result.WorkspaceRoots))
		for i, root := range result.WorkspaceRoots {
			roots[i] = deployment.WorkspaceRoot{ID: root.ID, Root: root.Root}
		}
		prepareStatus, prepared, err := deployment.PrepareSupervision(home, deployment.SupervisionSpec{
			ReleaseBuildID: releaseBuildID, MCPath: mcPath, BunPath: bunPath,
			DockerPath: dockerPath, Image: productionImageRef, SpineVolume: names.Volume,
			WorkspaceRoots: roots,
		}, launchdLabelLoaded)
		if err != nil {
			return writeVerbError(stdout, stderr, verbs.Domainf("prepare native supervision: %v", err))
		}
		status = prepareStatus
		detail = fmt.Sprintf("resident/dashboard configs and per-user LaunchAgents %s at %s; %s and %s verified unloaded",
			prepareStatus, prepared.Root, prepared.ResidentLabel, prepared.DashboardLabel)
	}
	return writeOnboardSection(stdout, stderr, a.Section, status, detail)
}

func launchdLabelLoaded(label string) (bool, error) {
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	out, err := exec.Command("launchctl", "print", target).CombinedOutput()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		text := strings.ToLower(string(out))
		if strings.Contains(text, "could not find service") || strings.Contains(text, "service not found") {
			return false, nil
		}
	}
	return false, fmt.Errorf("launchctl print %s: %v: %s", target, err, strings.TrimSpace(string(out)))
}
