//go:build !test_fake_routing

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"

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
	return writeOnboardSection(stdout, stderr, a.Section, status, detail)
}
