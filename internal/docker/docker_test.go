package docker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/swarm"
)

func TestStatsLimitsContainerStreams(t *testing.T) {
	const containers = 2 * maxContainerStatsStreams
	var active, peak atomic.Int32
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.40")
		case r.URL.Path == "/v1.40/containers/json":
			items := make([]map[string]string, containers)
			for i := range items {
				items[i] = map[string]string{"Id": strconv.Itoa(i)}
			}
			_ = json.NewEncoder(w).Encode(items)
		case strings.HasSuffix(r.URL.Path, "/stats"):
			current := active.Add(1)
			defer active.Add(-1)
			for highest := peak.Load(); current > highest && !peak.CompareAndSwap(highest, current); highest = peak.Load() {
			}
			time.Sleep(50 * time.Millisecond)
			_, _ = io.WriteString(w, "{}\n{}\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()
	t.Setenv("DOCKER_HOST", daemon.URL)
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	stats, err := c.Stats(t.Context(), "")
	if err != nil || len(stats) != containers {
		t.Fatalf("got %d samples, err=%v", len(stats), err)
	}
	for _, st := range stats {
		if st.Err != "" {
			t.Fatal(st.Err)
		}
	}
	if got := peak.Load(); got < 2 || got > maxContainerStatsStreams {
		t.Fatalf("peak concurrent streams = %d, want between 2 and %d", got, maxContainerStatsStreams)
	}
}

func TestServicesNegotiatesDockerAPIVersion(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_ping":
			w.Header().Set("API-Version", "1.40")
		case "/v1.40/services":
			_, _ = io.WriteString(w, `[{"ID":"web-id","Spec":{"Name":"demo_web","TaskTemplate":{"ContainerSpec":{"Image":"nginx:1.28"}}}}]`)
		case "/v1.40/tasks":
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()
	t.Setenv("DOCKER_HOST", daemon.URL)
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	services, err := c.Services(context.Background())
	if err != nil || len(services) != 1 || services[0].Name != "demo_web" || services[0].Image != "nginx:1.28" {
		t.Fatalf("services = %+v, err = %v", services, err)
	}
}

func TestPickNetwork(t *testing.T) {
	all := []string{"traefik-public", "ingress", "panel_default"}
	for _, c := range []struct {
		names               []string
		explicit, stack, to string
	}{
		{all, "", "panel", "panel_default"},
		{all, "mine", "panel", "mine"},
		{all, "", "", "panel_default"}, // sorted, ingress skipped
		{[]string{"ingress", "bridge"}, "", "", ""},
	} {
		if got := pickNetwork(c.names, c.explicit, c.stack); got != c.to {
			t.Errorf("pickNetwork(%v, %q, %q) = %q, want %q", c.names, c.explicit, c.stack, got, c.to)
		}
	}
}

func TestSplitImage(t *testing.T) {
	name, digest := splitImage("ghcr.io/me/app:v1@sha256:abc")
	if name != "ghcr.io/me/app:v1" || digest != "sha256:abc" {
		t.Fatalf("%q %q", name, digest)
	}
	if name, digest = splitImage("nginx:latest"); name != "nginx:latest" || digest != "" {
		t.Fatalf("%q %q", name, digest)
	}
}

func TestWithSlot(t *testing.T) {
	line := "2026-08-24T00:00:56Z com.docker.swarm.node.id=n1,com.docker.swarm.task.id=abc hello world"
	got := withSlot(line, map[string]int{"abc": 2})
	if want := "2026-08-24T00:00:56Z [2] hello world"; got != want {
		t.Fatalf("got %q", got)
	}
	// Unknown task: the line is kept, not dropped.
	if got := withSlot(line, map[string]int{"zzz": 1}); !strings.HasSuffix(got, "[?] hello world") {
		t.Fatalf("unknown task: %q", got)
	}
	// Single-replica logs carry no attributes and pass through untouched.
	if got := withSlot("plain line", nil); got != "plain line" {
		t.Fatalf("passthrough: %q", got)
	}
}

func TestParsePortsAndNames(t *testing.T) {
	ports, err := parsePorts([]string{"8080:80", " 53:53/udp ", ""})
	if err != nil || len(ports) != 2 {
		t.Fatalf("parse: %v %+v", err, ports)
	}
	if ports[0].PublishedPort != 8080 || ports[0].TargetPort != 80 || ports[0].Protocol != "tcp" {
		t.Fatalf("tcp port wrong: %+v", ports[0])
	}
	if ports[1].Protocol != "udp" {
		t.Fatalf("udp port wrong: %+v", ports[1])
	}
	for _, bad := range []string{"80", "0:80", "80:0", "http:80", "80:80/sctp", "99999:80"} {
		if _, err := parsePorts([]string{bad}); err == nil {
			t.Fatalf("accepted bad port %q", bad)
		}
	}
	// A name starting with a dash would be read as a CLI flag.
	for _, bad := range []string{"", "-rf", "a b", "a/b", "a;b", strings.Repeat("x", 64)} {
		if ValidName(bad) {
			t.Fatalf("accepted bad name %q", bad)
		}
	}
	for _, ok := range []string{"web", "my_app.v2", "a-b-c"} {
		if !ValidName(ok) {
			t.Fatalf("rejected good name %q", ok)
		}
	}
}

func TestSlotFromTaskName(t *testing.T) {
	if got := slotFromTaskName("web.3.abc123"); got != 3 {
		t.Fatalf("replicated task: %d", got)
	}
	// Global services carry a node id where the slot would be.
	if got := slotFromTaskName("agent.x3i2qypaetduwegupdj4o97p8.qjpn"); got != 0 {
		t.Fatalf("global task: %d", got)
	}
	if got := slotFromTaskName(""); got != 0 {
		t.Fatalf("empty: %d", got)
	}
}

func TestAddCounters(t *testing.T) {
	st := TaskStats{}
	addCounters(&st, container.StatsResponse{
		Networks: map[string]container.NetworkStats{
			"eth0": {RxBytes: 10, TxBytes: 20},
			"eth1": {RxBytes: 1, TxBytes: 2},
		},
		BlkioStats: container.BlkioStats{IoServiceBytesRecursive: []container.BlkioStatEntry{
			{Op: "Read", Value: 30}, {Op: "Write", Value: 40}, {Op: "Sync", Value: 50},
		}},
	})
	if st.NetRX != 11 || st.NetTX != 22 || st.BlockRead != 30 || st.BlockWrite != 40 {
		t.Fatalf("wrong counters: %+v", st)
	}
}

func TestDesiredReplicas(t *testing.T) {
	three := uint64(3)
	replicated := swarm.Service{ID: "web", Spec: swarm.ServiceSpec{
		Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &three}},
	}}
	global := swarm.Service{ID: "agent", Spec: swarm.ServiceSpec{
		Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}},
	}}
	tasks := []swarm.Task{
		{ServiceID: "agent", DesiredState: swarm.TaskStateRunning},
		{ServiceID: "agent", DesiredState: swarm.TaskStateRunning},
		{ServiceID: "agent", DesiredState: swarm.TaskStateShutdown},
	}
	scheduled := scheduledTasks(tasks)

	if got := desiredReplicas(replicated, scheduled); got != 3 {
		t.Errorf("replicated: want 3 from the spec, got %d", got)
	}
	if got := desiredReplicas(global, scheduled); got != 2 {
		t.Errorf("global: want 2 scheduled tasks, got %d", got)
	}
}

func TestRunningTasksExcludesShutDownReplica(t *testing.T) {
	got := runningTasks([]swarm.Task{
		{ServiceID: "web", DesiredState: swarm.TaskStateRunning, Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
		// A dead worker can leave this last reported as Running, but Swarm has
		// already asked it to stop. It must not inflate the replica count.
		{ServiceID: "web", DesiredState: swarm.TaskStateShutdown, Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
		{ServiceID: "web", DesiredState: swarm.TaskStateRunning, Status: swarm.TaskStatus{State: swarm.TaskStateFailed}},
	})
	if got["web"] != 1 {
		t.Fatalf("want 1 live task, got %d", got["web"])
	}
}

func TestImageRepo(t *testing.T) {
	for in, want := range map[string]string{
		"ghcr.io/modularnost/panel:edge":             "ghcr.io/modularnost/panel",
		"ghcr.io/modularnost/panel:0.2.0@sha256:abc": "ghcr.io/modularnost/panel",
		"ghcr.io/modularnost/panel":                  "ghcr.io/modularnost/panel",
		"localhost:5000/panel:dev":                   "localhost:5000/panel",
		"localhost:5000/panel":                       "localhost:5000/panel",
		"traefik:v3@sha256:575fa15b1350":             "traefik",
	} {
		if got := ImageRepo(in); got != want {
			t.Errorf("ImageRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImageTag(t *testing.T) {
	for in, want := range map[string]string{
		"ghcr.io/modularnost/panel:edge":             "edge",
		"ghcr.io/modularnost/panel:0.2.0@sha256:abc": "0.2.0@sha256:abc",
		"ghcr.io/modularnost/panel@sha256:abc":       "@sha256:abc",
		"ghcr.io/modularnost/panel":                  "",
		"localhost:5000/panel:dev":                   "dev",
		"nginx:1.28":                                 "1.28",
		"alpine":                                     "",
	} {
		if got := ImageTag(in); got != want {
			t.Errorf("ImageTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTargetImage(t *testing.T) {
	for _, tc := range []struct {
		current, tag, want string
	}{
		{"ghcr.io/modularnost/panel:0.1.0", "0.2.0", "ghcr.io/modularnost/panel:0.2.0"},
		{"ghcr.io/modularnost/panel:0.1.0", "0.2.0@sha256:abc", "ghcr.io/modularnost/panel:0.2.0@sha256:abc"},
		{"ghcr.io/modularnost/panel:0.1.0", "@sha256:abc", "ghcr.io/modularnost/panel@sha256:abc"},
		{"ghcr.io/modularnost/panel:0.1.0", "ghcr.io/modularnost/panel:0.1.0", "ghcr.io/modularnost/panel:0.1.0"},
		{"panel:latest", "0.2.0", "panel:0.2.0"},
	} {
		if got := targetImage(tc.current, tc.tag); got != tc.want {
			t.Errorf("targetImage(%q, %q) = %q, want %q", tc.current, tc.tag, got, tc.want)
		}
	}
}

func TestDistributionInspect(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_ping":
			w.Header().Set("API-Version", "1.40")
		case "/v1.40/distribution/nginx:1.28/json":
			_, _ = io.WriteString(w, `{"Descriptor":{"mediaType":"application/vnd.docker.distribution.manifest.v2+json","size":1234,"digest":"sha256:abcdef1234567890"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()
	t.Setenv("DOCKER_HOST", daemon.URL)
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	insp, err := c.DistributionInspect(context.Background(), "nginx:1.28", "")
	if err != nil {
		t.Fatalf("DistributionInspect err: %v", err)
	}
	if string(insp.Descriptor.Digest) != "sha256:abcdef1234567890" {
		t.Fatalf("digest = %s, want sha256:abcdef1234567890", insp.Descriptor.Digest)
	}
}

func TestLastFailure(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	task := func(ago time.Duration, state swarm.TaskState, errText string) swarm.Task {
		return swarm.Task{
			Meta:   swarm.Meta{CreatedAt: at.Add(-ago)},
			Status: swarm.TaskStatus{State: state, Err: errText},
		}
	}
	for name, c := range map[string]struct {
		tasks []swarm.Task
		want  string
	}{
		"no tasks":            {nil, ""},
		"a slow first pull":   {[]swarm.Task{task(0, swarm.TaskStatePreparing, "")}, ""},
		"the newest failed":   {[]swarm.Task{task(0, swarm.TaskStateRejected, "No such image: x:1")}, "No such image: x:1"},
		"a retry in progress": {[]swarm.Task{task(0, swarm.TaskStatePreparing, ""), task(10*time.Second, swarm.TaskStateRejected, "not found")}, "not found"},
		"the newest reason":   {[]swarm.Task{task(20*time.Second, swarm.TaskStateRejected, "old"), task(0, swarm.TaskStateFailed, "new")}, "new"},
		"running wins":        {[]swarm.Task{task(0, swarm.TaskStateRunning, ""), task(10*time.Second, swarm.TaskStateFailed, "earlier")}, ""},
		"failed, no reason":   {[]swarm.Task{task(0, swarm.TaskStateFailed, "")}, ""},
	} {
		if got := lastFailure(c.tasks); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}
