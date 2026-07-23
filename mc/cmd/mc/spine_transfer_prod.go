//go:build !test_fake_routing

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"mc/deployment"
	"mc/verbs"
)

func brokerBackup(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return writeVerbError(stdout, stderr, verbs.Usagef("usage: mc backup"))
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	if err := verbs.RequireHostScope(id, "mc backup"); err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	req, home, err := verbs.PrepareSpineTransfer(releaseBuildID, gatewayControlVersion, configSchemaVersion)
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	frame, err := json.Marshal(req)
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("encode private backup frame: %v", err))
	}
	helper, err := ensureRuntimeHelper()
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("ensure warm helper: %v", err))
	}
	cmd := exec.Command("docker", "exec", "-i", helper, "mc", "__backup-spine")
	cmd.Stdin = bytes.NewReader(frame)
	privateErr := boundedDoctorBuffer{limit: privateDoctorOutputLimit}
	cmd.Stderr = &privateErr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("open private backup stream: %v", err))
	}
	if err := cmd.Start(); err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("start private backup crossing: %v", err))
	}
	snapshot, size, receiveErr := verbs.PublishSpineBackup(home, req, pipe, time.Now())
	if receiveErr != nil {
		_, _ = io.Copy(io.Discard, pipe)
	}
	waitErr := cmd.Wait()
	if privateErr.Len() > 0 {
		_, _ = stderr.Write(privateErr.Bytes())
	}
	if privateErr.overflow {
		return writeVerbError(stdout, stderr, verbs.Usagef("private backup diagnostics exceeded their bound"))
	}
	if waitErr != nil {
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			return writeVerbError(stdout, stderr, verbs.Domainf("private backup refused with exit %d", exit.ExitCode()))
		}
		return writeVerbError(stdout, stderr, verbs.Usagef("private backup crossing failed: %v", waitErr))
	}
	if receiveErr != nil {
		return writeVerbError(stdout, stderr, receiveErr)
	}
	if err := writeJSON(stdout, map[string]any{"snapshot": snapshot, "bytes": size}); err != nil {
		fmt.Fprintln(stderr, "mc:", err)
		return 2
	}
	return 0
}

func restoreLatestSpine() (string, int64, error) {
	id, err := verbs.LoadIdentity()
	if err != nil {
		return "", 0, err
	}
	if err := verbs.RequireHostScope(id, "mc onboard home --restore-latest"); err != nil {
		return "", 0, err
	}
	req, home, err := verbs.PrepareSpineTransfer(releaseBuildID, gatewayControlVersion, configSchemaVersion)
	if err != nil {
		return "", 0, err
	}
	if err := requireSupervisionUnloaded(home, "restore"); err != nil {
		return "", 0, err
	}
	file, header, path, err := verbs.OpenLatestSpineBackup(home, req)
	if err != nil {
		return "", 0, err
	}
	framed, err := verbs.FrameSpineBackup(file, header)
	if err != nil {
		return "", 0, verbs.Usagef("frame newest spine backup: %v", err)
	}
	defer framed.Close()
	helper, err := ensureRuntimeHelper()
	if err != nil {
		return "", 0, verbs.Usagef("ensure warm helper: %v", err)
	}
	privateOut := boundedDoctorBuffer{limit: privateOnboardStateOutputLimit}
	privateErr := boundedDoctorBuffer{limit: privateDoctorOutputLimit}
	cmd := exec.Command("docker", "exec", "-i", helper, "mc", "__restore-spine")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = framed, &privateOut, &privateErr
	if err := cmd.Run(); err != nil {
		if privateErr.Len() > 0 {
			return "", 0, verbs.Domainf("private restore refused: %s", bytes.TrimSpace(privateErr.Bytes()))
		}
		return "", 0, verbs.Usagef("private restore crossing failed: %v", err)
	}
	if privateOut.overflow || privateErr.overflow {
		return "", 0, verbs.Usagef("private restore response exceeded its bound")
	}
	var result verbs.SpineBackupHeader
	dec := json.NewDecoder(bytes.NewReader(privateOut.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil || result != header {
		return "", 0, verbs.Domainf("private restore response identity mismatch")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return "", 0, verbs.Domainf("private restore response carries trailing data")
	}
	return path, header.Bytes, nil
}

func requireSupervisionUnloaded(home, operation string) error {
	names, err := deployment.RuntimeNames(home)
	if err != nil {
		return verbs.Usagef("derive %s supervision labels: %v", operation, err)
	}
	for _, label := range []string{"com.mission-control." + names.Suffix + ".resident", "com.mission-control." + names.Suffix + ".dashboard"} {
		loaded, err := launchdLabelLoaded(label)
		if err != nil {
			return verbs.Usagef("inspect supervision before %s: %v", operation, err)
		}
		if loaded {
			return verbs.Domainf("%s requires %s to be unloaded", operation, label)
		}
	}
	return nil
}

func brokerReset(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("mc reset")
	confirm := fs.Bool("confirm", false, "confirm the destructive volume reset")
	if err := parse(fs, args[1:]); err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	if !*confirm {
		return writeVerbError(stdout, stderr, verbs.Domainf("mc reset is destructive: it snapshots the spine and removes the derived runtime volume; re-run with --confirm (§16.4)"))
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	if err := verbs.RequireHostScope(id, "mc reset"); err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	req, home, err := verbs.PrepareSpineTransfer(releaseBuildID, gatewayControlVersion, configSchemaVersion)
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	if err := requireSupervisionUnloaded(home, "reset"); err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	manager, err := productionHelperManager(execDockerRunner{})
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("derive reset runtime: %v", err))
	}
	volume, volumeExists, err := inspectJSON[helperVolumeInspect](manager.docker, "volume", manager.names.Volume)
	if err != nil {
		return writeVerbError(stdout, stderr, verbs.Usagef("inspect reset volume: %v", err))
	}
	if !volumeExists {
		if _, helperExists, err := inspectJSON[helperContainerInspect](manager.docker, "container", manager.names.Helper); err != nil || helperExists {
			return writeVerbError(stdout, stderr, verbs.Domainf("reset replay found an unexpected helper without its volume"))
		}
		file, header, snapshot, err := verbs.OpenLatestSpineBackup(home, req)
		if err != nil {
			return writeVerbError(stdout, stderr, err)
		}
		file.Close()
		if err := writeJSON(stdout, map[string]any{"reset": true, "status": "already-reset", "volume": manager.names.Volume, "snapshot": snapshot, "bytes": header.Bytes}); err != nil {
			fmt.Fprintln(stderr, "mc:", err)
			return 2
		}
		return 0
	}
	if volume.Name != manager.names.Volume || volume.Driver != "local" || len(volume.Options) != 0 {
		return writeVerbError(stdout, stderr, verbs.Domainf("reset refuses a non-exact derived spine volume"))
	}
	var backupOut, backupErr bytes.Buffer
	if code := brokerBackup([]string{"backup"}, &backupOut, &backupErr); code != 0 {
		_, _ = stdout.Write(backupOut.Bytes())
		_, _ = stderr.Write(backupErr.Bytes())
		return code
	}
	var receipt struct {
		Snapshot string `json:"snapshot"`
		Bytes    int64  `json:"bytes"`
	}
	dec := json.NewDecoder(bytes.NewReader(backupOut.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&receipt); err != nil || receipt.Snapshot == "" || receipt.Bytes < 1 {
		return writeVerbError(stdout, stderr, verbs.Domainf("backup receipt was invalid; reset did not start"))
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return writeVerbError(stdout, stderr, verbs.Domainf("backup receipt carried trailing data; reset did not start"))
	}
	if err := manager.destroySpineVolume(); err != nil {
		return writeVerbError(stdout, stderr, verbs.Domainf("reset stopped after durable backup %q: %v", receipt.Snapshot, err))
	}
	if err := writeJSON(stdout, map[string]any{"reset": true, "status": "done", "volume": manager.names.Volume, "snapshot": receipt.Snapshot, "bytes": receipt.Bytes}); err != nil {
		fmt.Fprintln(stderr, "mc:", err)
		return 2
	}
	return 0
}
