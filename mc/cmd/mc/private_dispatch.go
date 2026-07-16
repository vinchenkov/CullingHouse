package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"mc/substrate"
	"mc/verbs"
)

const maxPrivateFrameBytes = 1 << 20

var (
	privateHelperSelfTimeout = 4 * time.Second
	helperWallTimeout        = 120 * time.Second
	helperNoProgressTimeout  = 5 * time.Second
)

const privateHelperTimeoutExitCode = 124

func runPrivateDispatch(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if !privateHelperScopeOK() {
		fmt.Fprintln(stderr, "mc: private dispatch verb requires the helper's fixed spine scope")
		return 1
	}
	if len(args) != 3 || args[1] != "--deadline-unix-ms" {
		fmt.Fprintln(stderr, "mc: private dispatch verb requires its broker deadline")
		return 2
	}
	deadlineMillis, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		fmt.Fprintln(stderr, "mc: private dispatch broker deadline is invalid")
		return 2
	}
	deadline := time.UnixMilli(deadlineMillis)
	now := time.Now()
	if !deadline.After(now) {
		os.Exit(privateHelperTimeoutExitCode)
	}
	if deadline.Sub(now) > privateHelperSelfTimeout {
		fmt.Fprintln(stderr, "mc: private dispatch broker deadline exceeds its allowance")
		return 2
	}
	return runPrivateDispatchInScope(args[0], deadline, stdin, stdout, stderr)
}

func runPrivateDispatchInScope(verb string, deadlineAt time.Time, stdin io.Reader, stdout, stderr io.Writer) int {
	// This timer lives in the container-side mc process, not the Darwin-side
	// docker CLI. It fires before the broker's no-progress deadline, so the
	// broker cannot report a timeout while a detached helper still holds the
	// flock or remains capable of committing.
	remaining := time.Until(deadlineAt)
	if remaining <= 0 {
		os.Exit(privateHelperTimeoutExitCode)
	}
	deadline := time.AfterFunc(remaining, func() {
		os.Exit(privateHelperTimeoutExitCode)
	})
	defer deadline.Stop()

	switch verb {
	case "__dispatch-prepare":
		var request verbs.PrivateDispatchPrepareRequest
		if err := readPrivateFrame(stdin, &request); err != nil {
			fmt.Fprintln(stderr, "mc: invalid private prepare frame")
			return 1
		}
		if err := validatePrivateReleaseIdentity(request.ReleaseBuildID, request.ControlVersion,
			request.SpineSchemaVersion, request.ConfigSchemaVersion); err != nil {
			fmt.Fprintln(stderr, "mc:", err)
			return 1
		}
		out, err := withSpine(func(db *sql.DB) (any, error) {
			return verbs.DispatchPreparePrivateDB(db, request)
		})
		if err != nil {
			fmt.Fprintln(stderr, "mc: private prepare refused")
			return 1
		}
		if err := writePrivateFrame(stdout, out); err != nil {
			fmt.Fprintln(stderr, "mc: write private prepare frame:", err)
			return 2
		}
		return 0
	case "__dispatch-commit":
		var request verbs.PrivateDispatchCommitRequest
		if err := readPrivateFrame(stdin, &request); err != nil {
			fmt.Fprintln(stderr, "mc: invalid private commit frame")
			return 1
		}
		if err := validatePrivateReleaseIdentity(request.ReleaseBuildID, request.ControlVersion,
			request.SpineSchemaVersion, request.ConfigSchemaVersion); err != nil {
			fmt.Fprintln(stderr, "mc:", err)
			return 1
		}
		out, err := withSpine(func(db *sql.DB) (any, error) {
			return verbs.DispatchCommitPrivateDB(db, request)
		})
		if err != nil {
			fmt.Fprintln(stderr, "mc: private commit refused")
			return 1
		}
		if err := writePrivateFrame(stdout, out); err != nil {
			fmt.Fprintln(stderr, "mc: write private commit frame:", err)
			return 2
		}
		return 0
	}
	return 2
}

func validatePrivateReleaseIdentity(build string, control, schema, config int) error {
	if !validReleaseBuildID(build) || build != releaseBuildID || control != gatewayControlVersion ||
		schema != substrate.CurrentSchemaVersion || config != configSchemaVersion {
		return fmt.Errorf("private dispatch release identity mismatch")
	}
	return nil
}

func brokerPrepareCommit(deploymentUUID string, stdout, stderr io.Writer) int {
	request, err := verbs.NewPrivateDispatchPrepareRequest(
		releaseBuildID, deploymentUUID, gatewayControlVersion, configSchemaVersion)
	if err != nil {
		return writeBrokerRefusal(stdout, stderr, "preflight.frame_invalid", err)
	}
	var prepared verbs.PrivateDispatchPrepareResponse
	if err := callPrivateHelper("__dispatch-prepare", request, &prepared); err != nil {
		return writeBrokerRefusal(stdout, stderr, "health.helper_unavailable", err)
	}
	if prepared.ReleaseBuildID != releaseBuildID || prepared.ControlVersion != gatewayControlVersion ||
		prepared.SpineSchemaVersion != substrate.CurrentSchemaVersion || prepared.ConfigSchemaVersion != configSchemaVersion ||
		prepared.DeploymentUUID != deploymentUUID || prepared.DispatchRequestID != request.DispatchRequestID {
		return writeBrokerRefusal(stdout, stderr, "preflight.frame_invalid", fmt.Errorf("private prepare response identity mismatch"))
	}
	if prepared.Kind == "final" {
		if prepared.Candidate != nil || prepared.Final == nil || len(*prepared.Final) == 0 {
			return writeBrokerRefusal(stdout, stderr, "preflight.frame_invalid", fmt.Errorf("malformed private final response"))
		}
		if err := writeOrdinaryResult(stdout, *prepared.Final); err != nil {
			fmt.Fprintln(stderr, "mc: write final dispatch result:", err)
			return 2
		}
		return 0
	}
	if prepared.Kind != "candidate" || prepared.Candidate == nil || prepared.Final != nil {
		return writeBrokerRefusal(stdout, stderr, "preflight.frame_invalid", fmt.Errorf("malformed private candidate response"))
	}
	home, err := verbs.ResolveDispatchHome()
	if err != nil {
		return writeBrokerRefusal(stdout, stderr, "preflight.frame_invalid", err)
	}
	commit, err := verbs.DispatchAttestPrivate(home, prepared)
	if err != nil {
		return writeBrokerRefusal(stdout, stderr, "preflight.frame_invalid", err)
	}
	commit = verbs.DispatchRecheckPrivate(home, prepared, commit)
	var result verbs.PrivateDispatchResult
	if err := callPrivateHelper("__dispatch-commit", commit, &result); err != nil {
		return writeBrokerRefusal(stdout, stderr, "health.helper_unavailable", err)
	}
	if result.Version != 1 || result.ReleaseBuildID != releaseBuildID ||
		result.ControlVersion != gatewayControlVersion || result.SpineSchemaVersion != substrate.CurrentSchemaVersion ||
		result.ConfigSchemaVersion != configSchemaVersion || result.DeploymentUUID != deploymentUUID ||
		result.DispatchRequestID != request.DispatchRequestID || len(result.Result) == 0 {
		return writeBrokerRefusal(stdout, stderr, "preflight.frame_invalid", fmt.Errorf("private commit response identity mismatch"))
	}
	if err := writeOrdinaryResult(stdout, result.Result); err != nil {
		fmt.Fprintln(stderr, "mc: write final dispatch result:", err)
		return 2
	}
	return 0
}

func writeOrdinaryResult(w io.Writer, result []byte) error {
	if len(result) == 0 || len(result) > 64*1024 || result[0] != '{' || !json.Valid(result) {
		return fmt.Errorf("helper returned an invalid final result")
	}
	return writeAll(w, append(append([]byte{}, result...), '\n'))
}

func callPrivateHelper(verb string, request, response any) error {
	var input bytes.Buffer
	if err := writePrivateFrame(&input, request); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), helperWallTimeout)
	defer cancel()
	// Pass an absolute deadline fixed before Docker startup. Even if docker
	// exec spends most of the broker allowance starting the container-side
	// process, that process inherits only the remaining time (or exits 124
	// immediately when the deadline has already passed).
	helperDeadline := time.Now().Add(privateHelperSelfTimeout).UnixMilli()
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", helperContainerName(), "mc", verb,
		"--deadline-unix-ms", strconv.FormatInt(helperDeadline, 10))
	cmd.Stdin = &input
	var output boundedBuffer
	output.limit = maxPrivateFrameBytes + 4
	output.progress = make(chan struct{}, 1)
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("private helper %s failed to start", verb)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	noProgress := time.NewTimer(helperNoProgressTimeout)
	defer noProgress.Stop()
	for {
		select {
		case err := <-waited:
			if err != nil {
				return fmt.Errorf("private helper %s failed", verb)
			}
			goto complete
		case <-output.progress:
			if !noProgress.Stop() {
				select {
				case <-noProgress.C:
				default:
				}
			}
			noProgress.Reset(helperNoProgressTimeout)
		case <-noProgress.C:
			_ = cmd.Process.Kill()
			<-waited
			return fmt.Errorf("private helper %s made no I/O progress for %s", verb, helperNoProgressTimeout)
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-waited
			return fmt.Errorf("private helper %s exceeded wall allowance", verb)
		}
	}

complete:
	if output.overflow {
		return fmt.Errorf("private helper %s response exceeded 1 MiB", verb)
	}
	return readPrivateFrame(bytes.NewReader(output.Bytes()), response)
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
	progress chan struct{}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > 0 && b.progress != nil {
		select {
		case b.progress <- struct{}{}:
		default:
		}
	}
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.overflow = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.overflow = true
		_, _ = b.Buffer.Write(p[:remaining])
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func writePrivateFrame(w io.Writer, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(body) == 0 || len(body) > maxPrivateFrameBytes {
		return fmt.Errorf("private frame length %d is outside 1..1048576", len(body))
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(body)))
	return writeAll(w, append(prefix[:], body...))
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func readPrivateFrame(r io.Reader, value any) error {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n == 0 || n > maxPrivateFrameBytes {
		return fmt.Errorf("private frame length %d is outside 1..1048576", n)
	}
	body := make([]byte, int(n))
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	var trailing [1]byte
	if n, err := r.Read(trailing[:]); n != 0 || (err != nil && err != io.EOF) {
		return fmt.Errorf("private frame has trailing bytes")
	}
	if err := json.Unmarshal(body, value); err != nil {
		return err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if !bytes.Equal(body, canonical) {
		return fmt.Errorf("private frame is not the closed canonical encoding")
	}
	return nil
}
