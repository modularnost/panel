package docker

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
)

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
