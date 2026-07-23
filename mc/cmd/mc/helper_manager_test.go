//go:build !test_fake_routing

package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"mc/deployment"
)

type dockerStep struct {
	want []string
	out  string
	err  error
}

type scriptedDocker struct {
	t     *testing.T
	steps []dockerStep
	calls int
}

func (s *scriptedDocker) CombinedOutput(args ...string) ([]byte, error) {
	s.t.Helper()
	if s.calls >= len(s.steps) {
		s.t.Fatalf("unexpected docker call: %#v", args)
	}
	step := s.steps[s.calls]
	s.calls++
	if !reflect.DeepEqual(args, step.want) {
		s.t.Fatalf("docker call %d:\n got %#v\nwant %#v", s.calls, args, step.want)
	}
	return []byte(step.out), step.err
}

func inspectBytes(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHelperCreateArgsPinLeastPrivilegeEnvelope(t *testing.T) {
	names, err := deployment.RuntimeNames("/Users/alice/.mission-control")
	if err != nil {
		t.Fatal(err)
	}
	m := helperManager{names: names}
	imageID := "sha256:" + strings.Repeat("a", 64)
	want := []string{
		"container", "create", "--rm", "--name", names.Helper,
		"--label", "mc-managed=true", "--label", "mc-component=helper",
		"--network", "none", "--user", "10002:10002", "--cap-drop", "ALL",
		"--privileged=false", "--ipc", "private", "--cpus", "0.5",
		"--memory", "512m", "--memory-swap", "512m", "--pids-limit", "128",
		"--oom-kill-disable=false", "--mount", "type=volume,src=" + names.Volume + ",dst=/mc/spine",
		imageID, "/bin/sleep", "infinity",
	}
	if got := m.createArgs(imageID); !reflect.DeepEqual(got, want) {
		t.Fatalf("create argv:\n got %#v\nwant %#v", got, want)
	}
	joined := strings.Join(want, " ")
	for _, forbidden := range []string{"MC_HOME", "MC_SPINE", "MC_RUN_JSON", "mc-tier", "docker.sock", "--cap-add", "no-new-privileges", "type=bind"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("helper argv carries forbidden %q: %s", forbidden, joined)
		}
	}
}

func exactHelperFixture(names deployment.Names, imageID string) helperContainerInspect {
	var c helperContainerInspect
	c.Name = "/" + names.Helper
	c.Image = imageID
	c.Config.User = "10002:10002"
	c.Config.Labels = map[string]string{"mc-managed": "true", "mc-component": "helper"}
	c.Config.Cmd = []string{"/bin/sleep", "infinity"}
	c.HostConfig.NetworkMode = "none"
	c.HostConfig.IpcMode = "private"
	c.HostConfig.CapDrop = []string{"ALL"}
	c.HostConfig.NanoCPUs = 500000000
	c.HostConfig.Memory = 536870912
	c.HostConfig.MemorySwap = 536870912
	c.HostConfig.PidsLimit = 128
	c.Mounts = append(c.Mounts, struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	}{Type: "volume", Name: names.Volume, Destination: "/mc/spine", RW: true})
	return c
}

func TestHelperExactRejectsAuthorityWidening(t *testing.T) {
	names, _ := deployment.RuntimeNames("/Users/alice/.mission-control")
	m := helperManager{names: names}
	imageID := "sha256:" + strings.Repeat("a", 64)
	base := exactHelperFixture(names, imageID)
	base.Config.Labels["org.opencontainers.image.version"] = "24.04"
	if !m.helperExact(base, imageID) {
		t.Fatal("exact helper fixture with inherited image metadata was rejected")
	}

	cases := map[string]func(*helperContainerInspect){
		"root user":        func(c *helperContainerInspect) { c.Config.User = "0:0" },
		"tier label":       func(c *helperContainerInspect) { c.Config.Labels["mc-tier"] = "pipeline" },
		"host network":     func(c *helperContainerInspect) { c.HostConfig.NetworkMode = "host" },
		"privileged":       func(c *helperContainerInspect) { c.HostConfig.Privileged = true },
		"cap add":          func(c *helperContainerInspect) { c.HostConfig.CapAdd = []string{"CHOWN"} },
		"NNP":              func(c *helperContainerInspect) { c.HostConfig.SecurityOpt = []string{"no-new-privileges:true"} },
		"host bind":        func(c *helperContainerInspect) { c.HostConfig.Binds = []string{"/tmp:/tmp"} },
		"extra mount":      func(c *helperContainerInspect) { c.Mounts = append(c.Mounts, c.Mounts[0]) },
		"wrong image":      func(c *helperContainerInspect) { c.Image = "sha256:" + strings.Repeat("b", 64) },
		"unbounded memory": func(c *helperContainerInspect) { c.HostConfig.Memory = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := exactHelperFixture(names, imageID)
			mutate(&c)
			if m.helperExact(c, imageID) {
				t.Fatalf("widened helper was accepted: %#v", c)
			}
		})
	}
}

func TestHelperEnsureCreatesAndStartsOnlyExactEnvelope(t *testing.T) {
	names, _ := deployment.RuntimeNames("/Users/alice/.mission-control")
	imageID := "sha256:" + strings.Repeat("a", 64)
	image := helperImageInspect{ID: imageID, OS: "linux", Architecture: "arm64"}
	volume := helperVolumeInspect{Name: names.Volume, Driver: "local"}
	created := exactHelperFixture(names, imageID)
	running := exactHelperFixture(names, imageID)
	running.State.Running = true
	m := helperManager{imageRef: productionImageRef, names: names}
	runner := &scriptedDocker{t: t, steps: []dockerStep{
		{want: []string{"image", "inspect", "--format", "{{json .}}", productionImageRef}, out: inspectBytes(t, image)},
		{want: []string{"volume", "inspect", "--format", "{{json .}}", names.Volume}, out: "Error: No such volume", err: errors.New("exit 1")},
		{want: []string{"volume", "create", names.Volume}, out: names.Volume},
		{want: []string{"volume", "inspect", "--format", "{{json .}}", names.Volume}, out: inspectBytes(t, volume)},
		{want: []string{"container", "inspect", "--format", "{{json .}}", names.Helper}, out: "Error: No such container", err: errors.New("exit 1")},
		{want: m.createArgs(imageID), out: "container-id"},
		{want: []string{"container", "inspect", "--format", "{{json .}}", names.Helper}, out: inspectBytes(t, created)},
		{want: []string{"container", "start", names.Helper}, out: names.Helper},
		{want: []string{"container", "inspect", "--format", "{{json .}}", names.Helper}, out: inspectBytes(t, running)},
	}}
	m.docker = runner
	got, err := m.ensure()
	if err != nil || got != names.Helper {
		t.Fatalf("ensure helper = %q, %v", got, err)
	}
	if runner.calls != len(runner.steps) {
		t.Fatalf("docker calls = %d, want %d", runner.calls, len(runner.steps))
	}
}

func TestHelperEnsureMissingOrWrongImageMutatesNothing(t *testing.T) {
	names, _ := deployment.RuntimeNames("/Users/alice/.mission-control")
	for name, image := range map[string]helperImageInspect{
		"wrong architecture": {ID: "sha256:" + strings.Repeat("a", 64), OS: "linux", Architecture: "amd64"},
		"malformed id":       {ID: "latest", OS: "linux", Architecture: "arm64"},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &scriptedDocker{t: t, steps: []dockerStep{{
				want: []string{"image", "inspect", "--format", "{{json .}}", productionImageRef}, out: inspectBytes(t, image),
			}}}
			m := helperManager{docker: runner, imageRef: productionImageRef, names: names}
			if _, err := m.ensure(); err == nil {
				t.Fatal("invalid image was accepted")
			}
			if runner.calls != 1 {
				t.Fatalf("invalid image caused %d Docker calls", runner.calls)
			}
		})
	}
}

func TestHelperEnsurePreservesUnmanagedNameCollision(t *testing.T) {
	names, _ := deployment.RuntimeNames("/Users/alice/.mission-control")
	imageID := "sha256:" + strings.Repeat("a", 64)
	image := helperImageInspect{ID: imageID, OS: "linux", Architecture: "arm64"}
	volume := helperVolumeInspect{Name: names.Volume, Driver: "local"}
	foreign := exactHelperFixture(names, imageID)
	foreign.Config.Labels = map[string]string{"somebody": "else"}
	runner := &scriptedDocker{t: t, steps: []dockerStep{
		{want: []string{"image", "inspect", "--format", "{{json .}}", productionImageRef}, out: inspectBytes(t, image)},
		{want: []string{"volume", "inspect", "--format", "{{json .}}", names.Volume}, out: inspectBytes(t, volume)},
		{want: []string{"container", "inspect", "--format", "{{json .}}", names.Helper}, out: inspectBytes(t, foreign)},
	}}
	m := helperManager{docker: runner, imageRef: productionImageRef, names: names}
	if _, err := m.ensure(); err == nil || !strings.Contains(err.Error(), "unmanaged") {
		t.Fatalf("collision error = %v", err)
	}
	if runner.calls != len(runner.steps) {
		t.Fatalf("collision caused a mutation: calls=%d", runner.calls)
	}
}
