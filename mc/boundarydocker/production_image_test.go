//go:build docker_boundary

package boundarydocker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// prodImage is the untagged production build (runner/image/build-prod.sh).
// Its `mc` is compiled WITHOUT `-tags test_fake_routing`, and it alone carries
// the pinned real adapter runtime.
const prodImage = "mc-prod"

func requireProdImage(t *testing.T) {
	t.Helper()
	requireDocker(t)
	out, err := exec.Command("docker", "image", "inspect", prodImage).CombinedOutput()
	if err != nil {
		t.Skipf("production image %q not built — run runner/image/build-prod.sh (%s)",
			prodImage, strings.TrimSpace(string(out)))
	}
}

// ---------------------------------------------------------------------------
// §8 — "the production image contains no fake route and has its own untagged
// build".
//
// Until this file, the ONLY image in the tree was mc-fake-e2e, built with
// `-tags test_fake_routing`. That tag is what admits the fake harness family
// into the routing registry, so the requirement was not merely untested — the
// artifact it describes did not exist.
//
// The property is proved by BEHAVIOUR rather than by inspecting build metadata:
// a production binary must be unable to resolve a `fake` route at all, because
// an unparsable route is what denies fake spawn and mount authority.
// ---------------------------------------------------------------------------

func TestProductionImageHasNoFakeRouteDockerBoundary(t *testing.T) {
	requireProdImage(t)

	home := sharedTempDir(t)
	// Every role bound to the fake family. A production binary must not be able
	// to resolve ANY of them.
	routing := "# routing\n| role | harness | binding |\n| --- | --- | --- |\n" +
		"| worker | fake | fake |\n| strategist | fake | fake |\n| editor | fake | fake |\n" +
		"| verifier | fake | fake |\n| packager | fake | fake |\n| refiner | fake | fake |\n"
	if err := os.WriteFile(filepath.Join(home, "routing.md"), []byte(routing), 0o644); err != nil {
		t.Fatal(err)
	}

	probe := func(image string) string {
		t.Helper()
		out, _ := dockerRun(t,
			"-v", home+":/mc/home:ro", "-e", "MC_HOME=/mc/home",
			image, "mc", "doctor")
		return out
	}

	// The production binary must refuse the fake BINDING itself. This is the
	// property that denies fake spawn and mount authority: an unresolvable
	// binding cannot be routed to, so no fake harness can ever be selected.
	prod := probe(prodImage)
	if !strings.Contains(prod, `unresolved binding \"fake\"`) {
		t.Fatalf("the production image did not reject the fake binding:\n%s", prod)
	}

	// The control, and it is doing real work rather than decorating: the
	// fake-route image must get PAST the binding and fail on something else
	// entirely. Without this arm, a fixture that was malformed for some
	// unrelated reason would make the assertion above pass while proving
	// nothing about fake routes at all.
	fake := probe("mc-fake-e2e")
	if strings.Contains(fake, `unresolved binding \"fake\"`) {
		t.Fatalf("the fake-route image ALSO rejected the fake binding, so the fixture "+
			"proves nothing about the production build:\n%s", fake)
	}
}

func TestProductionImageCarriesPinnedRealAdapterRuntime(t *testing.T) {
	requireProdImage(t)
	out, err := dockerRun(t, prodImage, "sh", "-c",
		"/app/node_modules/@openai/codex-linux-arm64/vendor/aarch64-unknown-linux-musl/bin/codex --version; "+
			"test -f /app/node_modules/@anthropic-ai/claude-agent-sdk/package.json; "+
			"bun -e 'const p=require(\"/app/node_modules/@anthropic-ai/claude-agent-sdk/package.json\"); console.log(p.version)'")
	if err != nil {
		t.Fatalf("inspect production adapter runtime: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0.145.0") || !strings.Contains(out, "0.3.217") {
		t.Fatalf("production adapter runtime is not pinned:\n%s", out)
	}
}

func TestProductionImageGeneralMCIsSetuidDockerBoundary(t *testing.T) {
	requireProdImage(t)
	out, err := dockerRun(t, "--user", "10002:10002", prodImage,
		"sh", "-c", "stat -c '%u:%g %a' /usr/local/libexec/mc-real")
	if err != nil {
		t.Fatalf("inspect privileged mc: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "10001:10001 6755" {
		t.Fatalf("privileged mc ownership/mode = %q, want 10001:10001 6755", strings.TrimSpace(out))
	}
}

// ---------------------------------------------------------------------------
// Row 8 — the landing container's scope INVERSION.
//
// Every other container class proves its authority by PRESENCE: `/mc/run.json`
// is mounted and pins the tier, role, and run identity it may act as. The
// landing container's argument is the exact inverse — it carries no run.json,
// and `mc __land-sealed` calls RequireHostScope (main.go:291), which passes
// only when LoadIdentity returns nil (verbs.go:187-199).
//
// So "absent therefore trusted" is the security property, and it had never been
// exercised inside the image — only against the host binary. Two directions
// matter: the landing must actually GET host scope, and it must LOSE it if a
// run.json ever appears at that path.
// ---------------------------------------------------------------------------

func TestLandingContainerHasHostScopeAndNoRunJsonDockerBoundary(t *testing.T) {
	requireProdImage(t)

	t.Run("no run.json is present in a landing envelope", func(t *testing.T) {
		out, err := dockerRun(t, "--user", "10002:10002", prodImage,
			"sh", "-c", "ls /mc 2>/dev/null; test -e /mc/run.json && echo HAS_RUN_JSON || echo NO_RUN_JSON")
		if err != nil {
			t.Fatalf("probe: %v\n%s", err, out)
		}
		if strings.Contains(out, "HAS_RUN_JSON") {
			t.Fatalf("the landing image ships a run.json; the landing class would authorize as a pipeline run:\n%s", out)
		}
	})

	t.Run("host scope is granted when run.json is absent", func(t *testing.T) {
		// No envelope file, so the verb must get PAST the scope gate and fail
		// on the envelope instead. A scope refusal here would mean the landing
		// class cannot run at all.
		out, _ := dockerRun(t, "--user", "10002:10002", prodImage,
			"mc", "__land-sealed", "/mc/landing.json")
		if strings.Contains(strings.ToLower(out), "host scope only") {
			t.Fatalf("mc __land-sealed was refused for scope inside the landing image; "+
				"the landing class cannot execute:\n%s", out)
		}
	})

	t.Run("an injected run.json revokes host scope", func(t *testing.T) {
		// The inverse direction, and the one that makes the property a fence
		// rather than an accident: if something ever mounted a run.json into a
		// landing envelope, the verb must refuse rather than act as that run.
		home := sharedTempDir(t)
		runJSON := filepath.Join(home, "run.json")
		if err := os.WriteFile(runJSON,
			[]byte(`{"run_id":"forged","tier":"pipeline","role":"worker"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := dockerRun(t, "--user", "10002:10002",
			"-v", runJSON+":/mc/run.json:ro",
			"-e", "MC_RUN_JSON=/tmp/agent-controlled-missing.json",
			prodImage, "mc", "__land-sealed", "/mc/landing.json")
		if err == nil {
			t.Fatalf("mc __land-sealed succeeded with a forged run.json mounted:\n%s", out)
		}
		if !strings.Contains(strings.ToLower(out), "host scope only") {
			t.Fatalf("a landing carrying a run.json refused for the wrong reason "+
				"(want a host-scope refusal, so the identity is what stopped it):\n%s", out)
		}
	})
}
