//go:build !test_fake_routing

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"mc/verbs"
)

func brokerOnboardHome(stdout, stderr io.Writer) int {
	req, err := verbs.PrepareOnboardHome()
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	frame, err := json.Marshal(req)
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("encode private onboard-spine frame: %v", err))
	}
	helper, err := ensureRuntimeHelper()
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("ensure warm helper: %v", err))
	}
	var privateOut, privateErr bytes.Buffer
	cmd := exec.Command("docker", "exec", "-i", helper, "mc", "__onboard-spine")
	cmd.Stdin = bytes.NewReader(frame)
	cmd.Stdout = &privateOut
	cmd.Stderr = &privateErr
	if err := cmd.Run(); err != nil {
		if privateErr.Len() > 0 {
			_, _ = stderr.Write(privateErr.Bytes())
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			_, _ = stdout.Write(privateOut.Bytes())
			return exit.ExitCode()
		}
		return writeVerbError(stdout, stderr, verbs.Usagef("private onboard-spine crossing failed: %v", err))
	}
	var result verbs.OnboardSpineResult
	dec := json.NewDecoder(bytes.NewReader(privateOut.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("decode private onboard-spine response: %v", err))
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return writeVerbError(stdout, stderr, verbs.Usagef("private onboard-spine response carries trailing data"))
	}
	status, detail, err := verbs.FinalizeOnboardHome(result)
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	return writeOnboardSection(stdout, stderr, "home", status, detail)
}

func writeOnboardSection(stdout, stderr io.Writer, section, status, detail string) int {
	if err := writeJSON(stdout, map[string]any{
		"ok":       true,
		"sections": []map[string]any{{"section": section, "status": status, "detail": detail}},
	}); err != nil {
		fmt.Fprintln(stderr, "mc:", err)
		return 2
	}
	return 0
}
