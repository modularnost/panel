// Package docker is the only place that touches the Docker Swarm API.
// Handlers and modules never call the SDK directly.
package docker

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const stackLabel = "com.docker.stack.namespace"

type Client struct{ api *client.Client }

func New() (*Client, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Client{api: c}, nil
}

func (c *Client) Close() error { return c.api.Close() }

// Service is the flat shape the UI needs. Live, never cached in the database.
type Service struct {
	ID      string
	Name    string
	Stack   string
	Image   string // repo:tag without the digest
	Digest  string // sha256:... when pinned
	Running uint64
	Desired uint64
	Mode    string // replicated | global
	Labels  map[string]string
}

func (s Service) Healthy() bool { return s.Desired > 0 && s.Running == s.Desired }

func splitImage(img string) (name, digest string) {
	if i := strings.Index(img, "@"); i >= 0 {
		return img[:i], img[i+1:]
	}
	return img, ""
}

func (c *Client) Services(ctx context.Context) ([]Service, error) {
	list, err := c.api.ServiceList(ctx, swarm.ServiceListOptions{Status: true})
	if err != nil {
		return nil, err
	}
	tasks, err := c.api.TaskList(ctx, swarm.TaskListOptions{Filters: filters.NewArgs(
		filters.Arg("desired-state", string(swarm.TaskStateRunning)),
	)})
	if err != nil {
		return nil, err
	}
	running := runningTasks(tasks)
	out := make([]Service, 0, len(list))
	for _, s := range list {
		img, digest := splitImage(s.Spec.TaskTemplate.ContainerSpec.Image)
		svc := Service{
			ID:     s.ID,
			Name:   s.Spec.Name,
			Stack:  s.Spec.Labels[stackLabel],
			Image:  img,
			Digest: digest,
			Labels: s.Spec.Labels,
			Mode:   "replicated",
		}
		if s.Spec.Mode.Global != nil {
			svc.Mode = "global"
		}
		if s.ServiceStatus != nil {
			svc.Running, svc.Desired = s.ServiceStatus.RunningTasks, s.ServiceStatus.DesiredTasks
		}
		svc.Running = running[s.ID]
		out = append(out, svc)
	}
	return out, nil
}

func runningTasks(tasks []swarm.Task) map[string]uint64 {
	out := make(map[string]uint64)
	for _, t := range tasks {
		if t.DesiredState == swarm.TaskStateRunning && t.Status.State == swarm.TaskStateRunning {
			out[t.ServiceID]++
		}
	}
	return out
}

func (c *Client) service(ctx context.Context, nameOrID string) (swarm.Service, error) {
	s, _, err := c.api.ServiceInspectWithRaw(ctx, nameOrID, swarm.ServiceInspectOptions{})
	return s, err
}

func (c *Client) stackServices(ctx context.Context, stack string) ([]swarm.Service, error) {
	f := filters.NewArgs(filters.Arg("label", stackLabel+"="+stack))
	return c.api.ServiceList(ctx, swarm.ServiceListOptions{Filters: f})
}

// Deploy is the outcome of a redeploy, recorded in the deploy history.
type Deploy struct {
	Service   string
	OldDigest string
	NewDigest string
	Warnings  []string // not errors, e.g. an image with no digest in the registry
}

// ForceUpdate redeploys a service, re-pulling its image from the registry.
func (c *Client) ForceUpdate(ctx context.Context, nameOrID string) (Deploy, error) {
	var d Deploy
	// Swarm bumps the service version itself while tasks converge, and then
	// ServiceUpdate fails with "update out of sequence". Re-inspect and retry.
	for attempt := range 3 {
		s, err := c.service(ctx, nameOrID)
		if err != nil {
			return d, err
		}
		img, oldDigest := splitImage(s.Spec.TaskTemplate.ContainerSpec.Image)
		d.Service, d.OldDigest = s.Spec.Name, oldDigest

		spec := s.Spec
		// Drop the digest pin, or the daemon won't go to the registry for a new image.
		spec.TaskTemplate.ContainerSpec.Image = img
		spec.TaskTemplate.ForceUpdate++

		resp, err := c.api.ServiceUpdate(ctx, s.ID, s.Version, spec, swarm.ServiceUpdateOptions{QueryRegistry: true})
		if err != nil {
			if strings.Contains(err.Error(), "out of sequence") && attempt < 2 {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			return d, err
		}
		d.Warnings = resp.Warnings
		if upd, e := c.service(ctx, s.ID); e == nil {
			_, d.NewDigest = splitImage(upd.Spec.TaskTemplate.ContainerSpec.Image)
		}
		return d, nil
	}
	return d, fmt.Errorf("%s: the service is changing too fast, try again", nameOrID)
}

// SetLabels merges labels into the service spec (an empty value deletes a key).
// This is how the Traefik form works: the panel only writes labels and Traefik
// picks them up on its own.
func (c *Client) SetLabels(ctx context.Context, nameOrID string, labels map[string]string) error {
	s, err := c.service(ctx, nameOrID)
	if err != nil {
		return err
	}
	spec := s.Spec
	if spec.Labels == nil {
		spec.Labels = map[string]string{}
	}
	for k, v := range labels {
		if v == "" {
			delete(spec.Labels, k)
		} else {
			spec.Labels[k] = v
		}
	}
	_, err = c.api.ServiceUpdate(ctx, s.ID, s.Version, spec, swarm.ServiceUpdateOptions{})
	return err
}

// RedeployStack force-updates every service in a stack.
// ponytail: no `docker stack deploy` — the panel has no compose file and is not
// going to store one. Upgrade path: a real redeploy from compose, if ever needed.
func (c *Client) RedeployStack(ctx context.Context, stack string) ([]Deploy, error) {
	list, err := c.stackServices(ctx, stack)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("stack %q not found", stack)
	}
	// One failing service must not leave the stack half-redeployed: walk them
	// all and collect the errors.
	var done []Deploy
	var errs []error
	for _, s := range list {
		d, err := c.ForceUpdate(ctx, s.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Spec.Name, err))
			continue
		}
		done = append(done, d)
	}
	return done, errors.Join(errs...)
}

// --- Logs ---

// withSlot turns "<ts> <attrs> <msg>" into "<ts> [slot] <msg>".
// For single-replica logs (an empty map) the line is left alone.
func withSlot(line string, slotByTask map[string]int) string {
	if len(slotByTask) == 0 {
		return line
	}
	ts, rest, ok := strings.Cut(line, " ")
	if !ok {
		return line
	}
	attrs, msg, ok := strings.Cut(rest, " ")
	if !ok || !strings.Contains(attrs, "com.docker.swarm.task.id=") {
		return line
	}
	slot := "?"
	for _, kv := range strings.Split(attrs, ",") {
		if id, found := strings.CutPrefix(kv, "com.docker.swarm.task.id="); found {
			if n, known := slotByTask[id]; known {
				slot = strconv.Itoa(n)
			}
		}
	}
	return ts + " [" + slot + "] " + msg
}

// Task is one replica of a service, used by the UI filter.
type Task struct {
	ID    string
	Slot  int
	State string
}

func (c *Client) tasks(ctx context.Context, serviceID string) ([]swarm.Task, error) {
	f := filters.NewArgs(filters.Arg("service", serviceID))
	return c.api.TaskList(ctx, swarm.TaskListOptions{Filters: f})
}

// Tasks returns live replicas only — those are what the log filter offers.
func (c *Client) Tasks(ctx context.Context, serviceID string) ([]Task, error) {
	list, err := c.tasks(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(list))
	for _, t := range list {
		if t.DesiredState == swarm.TaskStateShutdown {
			continue // leftovers from a previous rolling update
		}
		out = append(out, Task{ID: t.ID, Slot: t.Slot, State: string(t.Status.State)})
	}
	slices.SortFunc(out, func(a, b Task) int { return cmp.Compare(a.Slot, b.Slot) })
	return out, nil
}

// slotMap maps task id to replica slot, shut-down tasks included: the log
// buffer still holds lines from replicas retired by an earlier rolling update.
func (c *Client) slotMap(ctx context.Context, serviceID string) (map[string]int, error) {
	list, err := c.tasks(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]int, len(list))
	for _, t := range list {
		m[t.ID] = t.Slot
	}
	return m, nil
}

// Logs streams a service's logs, or one replica's when taskID is set.
// The returned channel closes when ctx is cancelled or the stream ends.
func (c *Client) Logs(ctx context.Context, serviceID, taskID string, tail int) (<-chan string, error) {
	opts := container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: true,
		Timestamps: true, Tail: strconv.Itoa(tail),
		// For whole-service logs, Details is the only way to tell which replica a
		// line came from: the daemon puts task.id into the attributes.
		Details: taskID == "",
	}
	var slotByTask map[string]int
	if taskID == "" {
		var err error
		if slotByTask, err = c.slotMap(ctx, serviceID); err != nil {
			return nil, err
		}
	}
	var (
		rc  io.ReadCloser
		err error
	)
	if taskID != "" {
		rc, err = c.api.TaskLogs(ctx, taskID, opts)
	} else {
		rc, err = c.api.ServiceLogs(ctx, serviceID, opts)
	}
	if err != nil {
		return nil, err
	}

	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		defer rc.Close()
		// The stream is multiplexed (stdout/stderr); demux it into a single pipe.
		pr, pw := io.Pipe()
		go func() {
			_, err := stdcopy.StdCopy(pw, pw, rc)
			pw.CloseWithError(err)
		}()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case lines <- withSlot(sc.Text(), slotByTask):
			case <-ctx.Done():
				return
			}
		}
	}()
	return lines, nil
}

// --- Metrics ---

// TaskStats is a point-in-time sample for one replica.
type TaskStats struct {
	Node       string  `json:"node"`
	Service    string  `json:"service"`
	Stack      string  `json:"stack"`
	Slot       int     `json:"slot"`
	CPU        float64 `json:"cpu"`         // percent of one core, same as docker stats
	Mem        uint64  `json:"mem"`         // bytes
	MemLimit   uint64  `json:"mem_limit"`   // bytes, 0 when no limit is set
	NetRX      uint64  `json:"net_rx"`      // cumulative received bytes
	NetTX      uint64  `json:"net_tx"`      // cumulative transmitted bytes
	BlockRead  uint64  `json:"block_read"`  // cumulative bytes read from block devices
	BlockWrite uint64  `json:"block_write"` // cumulative bytes written to block devices
	Err        string  `json:"err,omitempty"`
}

// Stats collects CPU and memory for the containers running on THIS node.
//
// Deliberately built on ContainerList, not on the Swarm API: a worker node
// cannot list services or tasks at all ("this node is not a swarm manager"),
// and a manager lists tasks for the whole cluster while only holding its own
// containers. Task containers carry everything needed as labels, and those are
// readable on any node.
//
// A non-empty service name narrows the sweep: one service page should not open
// a stats stream for every container in the cluster.
func (c *Client) Stats(ctx context.Context, service string) ([]TaskStats, error) {
	f := filters.NewArgs(filters.Arg("label", "com.docker.swarm.task.id"))
	if service != "" {
		f.Add("label", "com.docker.swarm.service.name="+service)
	}
	list, err := c.api.ContainerList(ctx, container.ListOptions{Filters: f})
	if err != nil {
		return nil, err
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []TaskStats
	)
	for _, ct := range list {
		wg.Add(1)
		go func(id string, labels map[string]string) {
			defer wg.Done()
			st := TaskStats{
				Service: labels["com.docker.swarm.service.name"],
				Stack:   labels["com.docker.stack.namespace"],
				Slot:    slotFromTaskName(labels["com.docker.swarm.task.name"]),
			}
			if err := c.containerStats(ctx, id, &st); err != nil {
				st.Err = err.Error()
			}
			mu.Lock()
			out = append(out, st)
			mu.Unlock()
		}(ct.ID, ct.Labels)
	}
	wg.Wait()
	SortStats(out)
	return out, nil
}

// slotFromTaskName pulls the replica number out of "web.3.abc123". Global
// services have a node id in that position instead, and get slot 0.
func slotFromTaskName(name string) int {
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return 0
	}
	slot, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return slot
}

// SortStats orders rows the way the UI shows them: by node, then service, then
// replica. Exported because the panel merges what several agents send back.
func SortStats(out []TaskStats) {
	slices.SortFunc(out, func(a, b TaskStats) int {
		return cmp.Or(
			strings.Compare(a.Node, b.Node),
			strings.Compare(a.Service, b.Service),
			cmp.Compare(a.Slot, b.Slot))
	})
}

// NodeName is the name of the node this daemon runs on.
func (c *Client) NodeName(ctx context.Context) (string, error) {
	info, err := c.api.Info(ctx)
	if err != nil {
		return "", err
	}
	return info.Name, nil
}

// containerStats reads two samples in a row: CPU percent comes from the delta,
// and a single sample is not enough for it (its precpu block is empty).
func (c *Client) containerStats(ctx context.Context, id string, st *TaskStats) error {
	resp, err := c.api.ContainerStats(ctx, id, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	var s container.StatsResponse
	for range 2 {
		if err := dec.Decode(&s); err != nil {
			return err
		}
	}
	st.Mem, st.MemLimit = s.MemoryStats.Usage, s.MemoryStats.Limit
	// Subtract the page cache from usage, the way docker stats reports it.
	if cache, ok := s.MemoryStats.Stats["inactive_file"]; ok && cache < st.Mem {
		st.Mem -= cache
	}
	addCounters(st, s)
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if cpuDelta > 0 && sysDelta > 0 {
		cores := float64(s.CPUStats.OnlineCPUs)
		if cores == 0 {
			cores = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
		}
		st.CPU = cpuDelta / sysDelta * cores * 100
	}
	return nil
}

func addCounters(st *TaskStats, s container.StatsResponse) {
	for _, n := range s.Networks {
		st.NetRX += n.RxBytes
		st.NetTX += n.TxBytes
	}
	for _, io := range s.BlkioStats.IoServiceBytesRecursive {
		switch io.Op {
		case "Read":
			st.BlockRead += io.Value
		case "Write":
			st.BlockWrite += io.Value
		}
	}
}
