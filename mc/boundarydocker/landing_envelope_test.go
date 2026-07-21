//go:build docker_boundary

// Package boundarydocker is the Phase 3 boundary suite: the acceptance rows of
// docs/phase3-contract.md §3 whose required evidence is a KERNEL or RUNTIME
// fact — `docker inspect` output, in-container probes, real filesystem
// semantics — and which therefore cannot be satisfied by a host-side fixture
// test no matter how carefully it is written.
//
// It is gated by the `docker_boundary` build tag, so the ordinary fast lane
// never compiles it (contract §1, §8).
//
// The distinction this package exists to enforce: an argv assertion proves a
// bound was REQUESTED. Only an inspect or an in-container probe proves it was
// APPLIED. Several landing-lane properties had argv-level tests and no evidence
// at all that the kernel agreed.
package boundarydocker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// probeImage is a minimal image used for pure kernel/filesystem probes. These
// rows are about what the RUNTIME does with a mount table and a uid, which is
// image-independent; the rows that are about the production image's own
// behaviour belong in their own file and must not borrow this one.
const probeImage = "alpine:3.22"

func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}
	if out, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Skipf("docker not available: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

// dockerRun runs one throwaway probe container and returns its combined output.
func dockerRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	full := append([]string{"run", "--rm", "--network", "none"}, args...)
	out, err := exec.Command("docker", full...).CombinedOutput()
	return string(out), err
}

// sharedTempDir is t.TempDir's replacement for fixtures that will be used as
// Docker BIND MOUNTPOINTS.
//
// t.TempDir lives under `/var/folders`, and a directory there that has served
// as a nested bind mountpoint acquires a `com.apple.provenance` xattr from
// Docker Desktop which makes it resist `os.RemoveAll` with EPERM — so the test
// body passes and then the harness fails the test during teardown. Under
// `/private/tmp` the same directory removes cleanly. Verified both ways rather
// than assumed.
//
// This is a fixture concern only, and deliberately NOT worked around by
// ignoring the cleanup error: in production the cover lives at
// `MC_HOME/runs/landing-<id>.cover`, which MC owns, and the covered path is
// inside the operator's repository, which MC never deletes.
func sharedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/private/tmp", "mc-boundary-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command("rm", "-rf", dir).Run() })
	return dir
}

// operatorRepo builds a host directory shaped like a real operator Worksource:
// owned by the invoking (operator) uid, mode 0755, and NOT world-writable —
// which is the whole point, because the landing container runs as 10002 and
// nothing in the design chowns anything.
func operatorRepo(t *testing.T) string {
	t.Helper()
	root := sharedTempDir(t)
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("operator\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// ---------------------------------------------------------------------------
// Row 14 — the final-uid VirtioFS canary (ADR-019:183-188).
//
// ADR-017:76-86 explicitly REFUSES to assert anything about how the VirtioFS
// share presents host ownership, and defers the entire question to this canary.
// ADR-019:85 then puts setup and landing on one row at uid 10002:10002, and
// `effects.ts` chose that over an operator-uid exception on corpus grounds
// alone.
//
// Landing is the ONLY real-repository RW grant in the system. If the final uid
// cannot write the operator's bind, the lane is non-functional and every other
// landing acceptance row is testing a dead path — which is why this canary is
// first in the file and cheapest to run.
// ---------------------------------------------------------------------------

func TestFinalUidCanWriteTheVirtioFSWorksourceDockerBoundary(t *testing.T) {
	requireDocker(t)
	repo := operatorRepo(t)

	out, err := dockerRun(t,
		"--user", "10002:10002",
		"-v", repo+":/repo/source",
		probeImage, "sh", "-c",
		"id -u && touch /repo/source/CANARY && echo WROTE")
	if err != nil {
		t.Fatalf("uid 10002 could not write the operator-owned VirtioFS bind: %v\n%s\n"+
			"Landing is the only real-repository RW grant in the system; a red here means "+
			"the sealed landing lane cannot function on this host at all (ADR-019:183-188).", err, out)
	}
	if !strings.Contains(out, "10002") || !strings.Contains(out, "WROTE") {
		t.Fatalf("canary output did not confirm uid and write: %q", out)
	}

	// The file must actually exist on the host — an overlay that swallowed the
	// write would look identical from inside the container.
	canary := filepath.Join(repo, "CANARY")
	fi, err := os.Stat(canary)
	if err != nil {
		t.Fatalf("the container reported a successful write but the host has no file: %v", err)
	}

	// And it must land OWNED BY THE OPERATOR, not by 10002. Nothing in the
	// design chowns a host inode, so this is what makes a landing's merge
	// produce operator-owned bytes. If this ever flips to 10002, the operator
	// ends up with files in their own repository that they do not own, and the
	// no-permission-widening posture (ADR-017:76-86) stops being sufficient.
	if err := assertOwnedByInvoker(t, fi); err != nil {
		t.Fatalf("host ownership of a landing-written file: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Row 10 — the nested `.mission-control` cover (ADR-017:700).
//
// `/repo/source` is bound RW so a landing can merge into the operator's real
// repository. The sealed task stores live UNDER it at `.mission-control`, and
// the only thing keeping them out of that writable alias is an RO bind of an
// empty directory shadowing that subpath.
//
// This was asserted by the ADR and verified by nothing. `fenceLandingCover`
// (mc/verbs/landsealedrun.go:65-84) checks only that the path is an EMPTY
// DIRECTORY — which passes identically if the bind silently did nothing and the
// real `.mission-control` happened to be empty. The fence is not evidence of
// shadowing; this probe is.
// ---------------------------------------------------------------------------

func TestLandingNestedCoverActuallyShadowsDockerBoundary(t *testing.T) {
	requireDocker(t)
	repo := operatorRepo(t)

	// A sealed task store with a sentinel, exactly where a real one lives.
	store := filepath.Join(repo, ".mission-control", "tasks", "task-7")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(repo, ".mission-control", "SENTINEL")
	if err := os.WriteFile(sentinel, []byte("sealed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cover := filepath.Join(sharedTempDir(t), "cover")
	if err := os.MkdirAll(cover, 0o755); err != nil {
		t.Fatal(err)
	}

	out, _ := dockerRun(t,
		"--user", "10002:10002",
		"-v", repo+":/repo/source",
		"-v", cover+":/repo/source/.mission-control:ro",
		probeImage, "sh", "-c",
		`if [ -e /repo/source/.mission-control/SENTINEL ]; then echo LEAKED; else echo SHADOWED; fi; `+
			`if touch /repo/source/.mission-control/x 2>/dev/null; then echo COVER_WRITABLE; else echo COVER_RO; fi`)

	if strings.Contains(out, "LEAKED") {
		t.Fatalf("the nested cover did not shadow: the landing container can see the sealed task store "+
			"through the RW /repo/source alias, which is exactly what ADR-017:700 exists to prevent.\n%s", out)
	}
	if !strings.Contains(out, "SHADOWED") {
		t.Fatalf("cover probe was inconclusive: %q", out)
	}
	if !strings.Contains(out, "COVER_RO") {
		t.Fatalf("the cover bind is WRITABLE; a landing could write into the sealed store's path.\n%s", out)
	}

	// The host sentinel must be untouched — a cover that shadowed reads but
	// passed writes through would be worse than no cover at all.
	body, err := os.ReadFile(sentinel)
	if err != nil || string(body) != "sealed\n" {
		t.Fatalf("host sentinel was disturbed by the covered run: %q %v", string(body), err)
	}
}

// ---------------------------------------------------------------------------
// Row 14 / row 11 — the landing container's realized envelope.
//
// The resident's own tests assert the docker ARGV. Argv proves a bound was
// requested; only inspect proves it was applied, and the two can differ
// (an unsupported flag, a daemon-level override, a silently-dropped cap).
// ---------------------------------------------------------------------------

func TestLandingEnvelopeIsAppliedNotJustRequestedDockerBoundary(t *testing.T) {
	requireDocker(t)
	repo := operatorRepo(t)
	name := "mc-landing-boundaryprobe01"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	// The ADR-019:30 landing row, as the resident builds it.
	create := exec.Command("docker", "create",
		"--name", name,
		"--network", "none",
		"--user", "10002:10002",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges=true",
		"--cpus", "1", "--memory", "1024m", "--pids-limit", "128",
		"-v", repo+":/repo/source",
		probeImage, "true")
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create landing-shaped container: %v\n%s", err, out)
	}

	for _, tc := range []struct {
		what   string
		format string
		want   string
	}{
		{"network mode", "{{.HostConfig.NetworkMode}}", "none"},
		{"user", "{{.Config.User}}", "10002:10002"},
		{"memory bound", "{{.HostConfig.Memory}}", "1073741824"},
		{"pids limit", "{{.HostConfig.PidsLimit}}", "128"},
		{"privileged", "{{.HostConfig.Privileged}}", "false"},
		{"cap drop", "{{.HostConfig.CapDrop}}", "[ALL]"},
	} {
		out, err := exec.Command("docker", "inspect", "-f", tc.format, name).CombinedOutput()
		if err != nil {
			t.Fatalf("inspect %s: %v\n%s", tc.what, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != tc.want {
			t.Fatalf("landing %s = %q, want %q — requested in argv but not applied by the runtime",
				tc.what, got, tc.want)
		}
	}

	// CPU is expressed as NanoCpus rather than the requested "1".
	out, err := exec.Command("docker", "inspect", "-f", "{{.HostConfig.NanoCpus}}", name).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect cpus: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "1000000000" {
		t.Fatalf("landing cpu bound = %q, want 1000000000 (1 cpu)", got)
	}

	// No Docker socket may be bound into a landing under any circumstance.
	out, err = exec.Command("docker", "inspect", "-f", "{{range .Mounts}}{{.Source}} {{end}}", name).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect mounts: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "docker.sock") {
		t.Fatalf("a landing container has the Docker socket bound: %s", out)
	}

	// no-new-privileges must be present on the LANDING class specifically. The
	// setuid gate (row 7) requires its own class to be free of it, so the two
	// classes must never share one envelope builder.
	out, err = exec.Command("docker", "inspect", "-f", "{{.HostConfig.SecurityOpt}}", name).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect security opts: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "no-new-privileges") {
		t.Fatalf("landing security opts = %q, want no-new-privileges", strings.TrimSpace(string(out)))
	}
}

// The `none` network mode's negative probe: a landing must not be able to reach
// the network at all. This is row 11's landing-side obligation, and it is the
// only egress mode the landing class ever uses.
func TestLandingHasNoNetworkDockerBoundary(t *testing.T) {
	requireDocker(t)
	out, err := dockerRun(t,
		"--user", "10002:10002",
		probeImage, "sh", "-c",
		"ip -o addr show 2>/dev/null | grep -v ' lo ' | wc -l")
	if err != nil {
		t.Fatalf("network probe: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(out); got != "0" {
		t.Fatalf("a --network none landing has %s non-loopback interfaces:\n%s", got, out)
	}
}
