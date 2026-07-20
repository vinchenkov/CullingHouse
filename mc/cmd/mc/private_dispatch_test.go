package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type closedPrivateTestFrame struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
}

func TestPrivateHelperSelfDeadlineTerminatesContainerSideProcess(t *testing.T) {
	if os.Getenv("MC_TEST_PRIVATE_INSTANT_EXIT") == "1" {
		os.Exit(0)
	}
	if os.Getenv("MC_TEST_PRIVATE_SELF_DEADLINE") == "1" {
		reader, writer, err := os.Pipe()
		if err != nil {
			os.Exit(99)
		}
		defer reader.Close()
		defer writer.Close()
		os.Exit(runPrivateDispatchInScope("__dispatch-prepare", time.Now().Add(20*time.Millisecond), reader, os.Stdout, os.Stderr))
	}

	startup := privateHelperTestStartup(t, "TestPrivateHelperSelfDeadlineTerminatesContainerSideProcess")
	cmd := exec.Command(os.Args[0], "-test.run=^TestPrivateHelperSelfDeadlineTerminatesContainerSideProcess$")
	cmd.Env = append(os.Environ(), "MC_TEST_PRIVATE_SELF_DEADLINE=1")
	start := time.Now()
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != privateHelperTimeoutExitCode {
		t.Fatalf("container-side helper exit = %v, want code %d", err, privateHelperTimeoutExitCode)
	}
	if elapsed := time.Since(start); elapsed > startup+250*time.Millisecond {
		t.Fatalf("container-side self-deadline took %s, startup baseline %s", elapsed, startup)
	}
}

func TestPrivateHelperAbsoluteDeadlineIncludesBrokerStartup(t *testing.T) {
	if os.Getenv("MC_TEST_PRIVATE_INSTANT_EXIT") == "1" {
		os.Exit(0)
	}
	if raw := os.Getenv("MC_TEST_PRIVATE_ABSOLUTE_DEADLINE"); raw != "" {
		millis, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			os.Exit(99)
		}
		reader, writer, err := os.Pipe()
		if err != nil {
			os.Exit(98)
		}
		defer reader.Close()
		defer writer.Close()
		os.Exit(runPrivateDispatchInScope("__dispatch-commit", time.UnixMilli(millis), reader, os.Stdout, os.Stderr))
	}

	startup := privateHelperTestStartup(t, "TestPrivateHelperAbsoluteDeadlineIncludesBrokerStartup")
	deadline := time.Now().Add(20 * time.Millisecond).UnixMilli()
	time.Sleep(40 * time.Millisecond) // stand in for Docker startup latency
	cmd := exec.Command(os.Args[0], "-test.run=^TestPrivateHelperAbsoluteDeadlineIncludesBrokerStartup$")
	cmd.Env = append(os.Environ(), "MC_TEST_PRIVATE_ABSOLUTE_DEADLINE="+strconv.FormatInt(deadline, 10))
	start := time.Now()
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != privateHelperTimeoutExitCode {
		t.Fatalf("late-starting helper exit = %v, want code %d", err, privateHelperTimeoutExitCode)
	}
	if elapsed := time.Since(start); elapsed > startup+250*time.Millisecond {
		t.Fatalf("expired absolute deadline took %s after helper startup, baseline %s", elapsed, startup)
	}
}

// privateHelperTestStartup removes Go test-binary process startup from the
// deadline assertion. The production requirement is that the in-scope helper
// observes its absolute deadline promptly once it starts; test-harness startup
// is outside that process and is highly variable on macOS under load.
func privateHelperTestStartup(t *testing.T, testName string) time.Duration {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), "MC_TEST_PRIVATE_INSTANT_EXIT=1")
	start := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("startup baseline failed: %v", err)
	}
	return time.Since(start)
}

func TestPrivateHelperNoProgressDeadline(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "docker")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexec sleep 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldProgress, oldWall := helperNoProgressTimeout, helperWallTimeout
	helperNoProgressTimeout, helperWallTimeout = 20*time.Millisecond, time.Second
	t.Cleanup(func() { helperNoProgressTimeout, helperWallTimeout = oldProgress, oldWall })

	start := time.Now()
	err := callPrivateHelper("__dispatch-prepare", closedPrivateTestFrame{Version: 1, Name: "request"}, &closedPrivateTestFrame{})
	if err == nil || !strings.Contains(err.Error(), "no I/O progress") {
		t.Fatalf("deadline error = %v, want no-progress refusal", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("no-progress refusal took %s, want bounded prompt kill", elapsed)
	}
}

func TestPrivateFrameClosedCanonicalAndBounded(t *testing.T) {
	good := closedPrivateTestFrame{Version: 1, Name: "candidate"}
	var encoded bytes.Buffer
	if err := writePrivateFrame(&encoded, good); err != nil {
		t.Fatal(err)
	}
	var decoded closedPrivateTestFrame
	if err := readPrivateFrame(bytes.NewReader(encoded.Bytes()), &decoded); err != nil {
		t.Fatalf("canonical frame rejected: %v", err)
	}
	if decoded != good {
		t.Fatalf("decoded = %+v, want %+v", decoded, good)
	}

	for name, body := range map[string][]byte{
		"unknown_field":   []byte(`{"version":1,"name":"candidate","extra":true}`),
		"duplicate_field": []byte(`{"version":1,"name":"candidate","name":"candidate"}`),
		"reordered":       []byte(`{"name":"candidate","version":1}`),
	} {
		t.Run(name, func(t *testing.T) {
			var frame bytes.Buffer
			var prefix [4]byte
			binary.BigEndian.PutUint32(prefix[:], uint32(len(body)))
			frame.Write(prefix[:])
			frame.Write(body)
			if err := readPrivateFrame(&frame, &closedPrivateTestFrame{}); err == nil {
				t.Fatal("non-canonical private frame was accepted")
			}
		})
	}

	var oversized [4]byte
	binary.BigEndian.PutUint32(oversized[:], maxPrivateFrameBytes+1)
	if err := readPrivateFrame(bytes.NewReader(oversized[:]), &closedPrivateTestFrame{}); err == nil {
		t.Fatal("oversized private frame was accepted")
	}
}

// A broker failure must carry the helper's own reason. Discarding the child's
// stderr made every private-helper failure read as the bare "private helper
// <verb> failed", which is what made the sealed E2E's intermittent
// __dispatch-prepare failures undiagnosable (ledger 2026-07-19). The exit
// error is carried too: exit 124 is the container-side absolute deadline, the
// signature of a docker exec that spent the broker allowance starting up.
func TestHelperReasonCarriesTheChildsEvidence(t *testing.T) {
	var buf boundedBuffer
	buf.limit = maxHelperStderrBytes
	buf.Write([]byte("mc: private prepare refused\n"))

	got := helperReason(&buf, errors.New("exit status 1"))
	for _, want := range []string{"exit status 1", "private prepare refused"} {
		if !strings.Contains(got, want) {
			t.Fatalf("helperReason = %q, want it to carry %q", got, want)
		}
	}

	// Nothing to report stays empty, so the success path is unchanged.
	var empty boundedBuffer
	empty.limit = maxHelperStderrBytes
	if got := helperReason(&empty, nil); got != "" {
		t.Fatalf("helperReason with no evidence = %q, want empty", got)
	}

	// The tail is bounded and says so, so a chatty helper cannot use the
	// diagnostic as a data channel.
	var big boundedBuffer
	big.limit = 64
	big.Write([]byte(strings.Repeat("x", 500)))
	got = helperReason(&big, nil)
	if !strings.Contains(got, "truncated") {
		t.Fatalf("helperReason over its bound = %q, want it marked truncated", got)
	}
	if len(got) > 200 {
		t.Fatalf("helperReason over its bound is %d bytes, want it held near its limit", len(got))
	}

	// Newlines are folded so the reason stays one log line.
	var multi boundedBuffer
	multi.limit = maxHelperStderrBytes
	multi.Write([]byte("first\nsecond\n"))
	if got := helperReason(&multi, nil); strings.Contains(got, "\n") {
		t.Fatalf("helperReason = %q, want no embedded newline", got)
	}
}
