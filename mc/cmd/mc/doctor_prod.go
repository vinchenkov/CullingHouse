//go:build !test_fake_routing

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"mc/substrate"
	"mc/verbs"
)

const privateDoctorOutputLimit = 64 * 1024

type boundedDoctorBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedDoctorBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - b.Len()
	if remaining < len(p) {
		b.overflow = true
		if remaining < 0 {
			remaining = 0
		}
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return original, nil
}

func brokerDoctor(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return writeVerbError(stdout, stderr, verbs.Usagef("usage: mc doctor"))
	}
	result, err := productionDoctorResult()
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	if err := writeJSON(stdout, result); err != nil {
		fmt.Fprintln(stderr, "mc:", err)
		return 2
	}
	return 0
}

func productionDoctorResult() (any, error) {
	host, err := verbs.DoctorHostFacts()
	if err != nil {
		return nil, err
	}
	req := verbs.DoctorRuntimeRequest{
		ProtocolVersion: 1, ReleaseBuildID: releaseBuildID,
		ControlVersion: gatewayControlVersion, SpineSchemaVersion: substrate.CurrentSchemaVersion,
		ConfigSchemaVersion: configSchemaVersion,
	}
	frame, err := json.Marshal(req)
	if err != nil {
		return nil, verbs.Usagef("encode private doctor-runtime frame: %v", err)
	}
	helper, err := ensureRuntimeHelper()
	if err != nil {
		return verbs.ComposeDoctor(host, verbs.DoctorRuntimeUnavailable(req, "warm helper unavailable: "+err.Error()))
	}
	privateOut := boundedDoctorBuffer{limit: privateDoctorOutputLimit}
	privateErr := boundedDoctorBuffer{limit: privateDoctorOutputLimit}
	cmd := exec.Command("docker", "exec", "-i", helper, "mc", "__doctor-runtime")
	cmd.Stdin = bytes.NewReader(frame)
	cmd.Stdout = &privateOut
	cmd.Stderr = &privateErr
	if err := cmd.Run(); err != nil {
		return verbs.ComposeDoctor(host, verbs.DoctorRuntimeUnavailable(req, "helper capability crossing failed: "+err.Error()))
	}
	if privateOut.overflow || privateErr.overflow {
		return verbs.ComposeDoctor(host, verbs.DoctorRuntimeUnavailable(req, "helper capability report exceeded its bound"))
	}
	var runtimeReport verbs.DoctorRuntimeReport
	dec := json.NewDecoder(bytes.NewReader(privateOut.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&runtimeReport); err != nil {
		return verbs.ComposeDoctor(host, verbs.DoctorRuntimeUnavailable(req, "helper returned an invalid capability report"))
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return verbs.ComposeDoctor(host, verbs.DoctorRuntimeUnavailable(req, "helper returned an invalid capability report"))
	}
	if runtimeReport.ProtocolVersion != req.ProtocolVersion || runtimeReport.ReleaseBuildID != req.ReleaseBuildID ||
		runtimeReport.ControlVersion != req.ControlVersion || runtimeReport.SpineSchemaVersion != req.SpineSchemaVersion ||
		runtimeReport.ConfigSchemaVersion != req.ConfigSchemaVersion {
		return verbs.ComposeDoctor(host, verbs.DoctorRuntimeUnavailable(req, "helper returned mismatched build/schema identity"))
	}
	result, err := verbs.ComposeDoctor(host, runtimeReport)
	if err != nil {
		return verbs.ComposeDoctor(host, verbs.DoctorRuntimeUnavailable(req, "helper returned an invalid capability report"))
	}
	return result, nil
}
