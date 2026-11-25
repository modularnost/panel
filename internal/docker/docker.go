// Package docker is the only place that touches the Docker Swarm API.
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

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const stackLabel = "com.docker.stack.namespace"

type Client struct{ api *client.Client }

func New() (*Client, error) {
	c, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return &Client{api: c}, nil
}

func (c *Client) Close() error { return c.api.Close() }

// Service is the flat shape the UI needs. Live, never cached in the database.
type Service struct {
	ID        string
	Name      string
	Stack     string
	Image     string // repo:tag without the digest
	Digest    string // sha256:... when pinned
	Running   uint64
	Desired   uint64
	Mode      string // replicated | global
	Labels    map[string]string
	Ports     []PublishedPort
	UpdatedAt time.Time
}

// PublishedPort is a port a service opens on the nodes.
type PublishedPort struct {
	Port  int
	Proto string
}

func (s Service) Healthy() bool { return s.Desired > 0 && s.Running == s.Desired }

func splitImage(img string) (name, digest string) {
	if i := strings.Index(img, "@"); i >= 0 {
		return img[:i], img[i+1:]
	}
	return img, ""
}

func (c *Client) Services(ctx context.Context) ([]Service, error) {
	return c.listServices(ctx, nil)
}

func (c *Client) ServicesWithLabel(ctx context.Context, key, value string) ([]Service, error) {
	return c.listServices(ctx, make(client.Filters).Add("label", key+"="+value))
}

func (c *Client) listServices(ctx context.Context, filters client.Filters) ([]Service, error) {
	list, err := c.api.ServiceList(ctx, client.ServiceListOptions{Filters: filters})
	if err != nil {
		return nil, err
	}
	out := make([]Service, 0, len(list.Items))
	if len(list.Items) == 0 {
		return out, nil
	}
	taskFilters := make(client.Filters).Add("desired-state", string(swarm.TaskStateRunning))
	if len(filters) > 0 {
		for _, svc := range list.Items {
			taskFilters.Add("service", svc.ID)
		}
	}
	tasks, err := c.api.TaskList(ctx, client.TaskListOptions{Filters: taskFilters})
	if len(filters) > 0 && errdefs.IsNotFound(err) {
		// A removed service can invalidate the whole filter between the two reads.
		delete(taskFilters, "service")
		tasks, err = c.api.TaskList(ctx, client.TaskListOptions{Filters: taskFilters})
	}
	if err != nil {
		return nil, err
	}
	running, scheduled := runningTasks(tasks.Items), scheduledTasks(tasks.Items)
	for _, s := range list.Items {
		out = append(out, serviceView(s, running, scheduled))
	}
	return out, nil
}

// Service reads only the service and its currently scheduled tasks.
func (c *Client) Service(ctx context.Context, id string) (Service, bool, error) {
	if !nameRe.MatchString(id) {
		return Service{}, false, nil
	}
	s, err := c.service(ctx, id)
	if errdefs.IsNotFound(err) {
		return Service{}, false, nil
	}
	if err != nil {
		return Service{}, false, err
	}
	if s.ID != id && s.Spec.Name != id {
		return Service{}, false, nil
	}
	tasks, err := c.api.TaskList(ctx, client.TaskListOptions{
		Filters: make(client.Filters).Add("service", s.ID).Add("desired-state", string(swarm.TaskStateRunning)),
	})
	if err != nil {
		return Service{}, false, err
	}
	return serviceView(s, runningTasks(tasks.Items), scheduledTasks(tasks.Items)), true, nil
}

func serviceView(s swarm.Service, running, scheduled map[string]uint64) Service {
	img, digest := splitImage(s.Spec.TaskTemplate.ContainerSpec.Image)
	svc := Service{
		ID:        s.ID,
		Name:      s.Spec.Name,
		Stack:     s.Spec.Labels[stackLabel],
		Image:     img,
		Digest:    digest,
		Labels:    s.Spec.Labels,
		UpdatedAt: s.UpdatedAt,
		Mode:      "replicated",
		Running:   running[s.ID],
		Desired:   desiredReplicas(s, scheduled),
	}
	if s.Spec.Mode.Global != nil {
		svc.Mode = "global"
	}
	for _, p := range s.Endpoint.Ports {
		if p.PublishedPort > 0 {
			svc.Ports = append(svc.Ports, PublishedPort{int(p.PublishedPort), string(p.Protocol)})
		}
	}
	return svc
}

// desiredReplicas reads the count from the spec for a replicated service. A
// global one wants a task per eligible node, which is what Swarm has scheduled;
// the status block Docker offers for this came back as 0 for global services.
func desiredReplicas(s swarm.Service, scheduled map[string]uint64) uint64 {
	if r := s.Spec.Mode.Replicated; r != nil && r.Replicas != nil {
		return *r.Replicas
	}
	return scheduled[s.ID]
}

func scheduledTasks(tasks []swarm.Task) map[string]uint64 {
	out := make(map[string]uint64)
	for _, t := range tasks {
		if t.DesiredState == swarm.TaskStateRunning {
			out[t.ServiceID]++
		}
	}
	return out
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
	res, err := c.api.ServiceInspect(ctx, nameOrID, client.ServiceInspectOptions{})
	return res.Service, err
}

func (c *Client) stackServices(ctx context.Context, stack string) ([]swarm.Service, error) {
	f := make(client.Filters).Add("label", stackLabel+"="+stack)
	res, err := c.api.ServiceList(ctx, client.ServiceListOptions{Filters: f})
	return res.Items, err
}

// Deploy is the outcome of a redeploy, recorded in the deploy history.
type Deploy struct {
	Service   string
	OldDigest string
	NewDigest string
	Warnings  []string // not errors, e.g. an image with no digest in the registry
}

// ForceUpdate redeploys a service, re-pulling its image from the registry.
func (c *Client) ForceUpdate(ctx context.Context, nameOrID string, auth RegistryAuth) (Deploy, error) {
	return c.updateSpec(ctx, nameOrID, auth, func(spec *swarm.ServiceSpec, img string) {
		// Drop the digest pin, or the daemon won't go to the registry for a new image.
		spec.TaskTemplate.ContainerSpec.Image = img
		spec.TaskTemplate.ForceUpdate++
	})
}

// UpdateImage moves a service to another tag of the same repository. The
// rollout stops the old task first and rolls back when the new one fails: for
// the panel the old and the new process must not share the SQLite file.
func (c *Client) UpdateImage(ctx context.Context, nameOrID, tag string) (Deploy, error) {
	return c.moveToTag(ctx, nameOrID, tag, swarm.UpdateOrderStopFirst, nil)
}

// RollOut is UpdateImage for a service that may run two versions at once: the
// new task starts before the old one stops, so a healthy service stays up.
func (c *Client) RollOut(ctx context.Context, nameOrID, tag string, auth RegistryAuth) (Deploy, error) {
	return c.moveToTag(ctx, nameOrID, tag, swarm.UpdateOrderStartFirst, auth)
}

// Rollback moves a service back to a previously running image or tag.
func (c *Client) Rollback(ctx context.Context, nameOrID, image string) (Deploy, error) {
	return c.moveToTag(ctx, nameOrID, image, swarm.UpdateOrderStopFirst, nil)
}

func (c *Client) moveToTag(ctx context.Context, nameOrID, tag string, order swarm.UpdateOrder, auth RegistryAuth) (Deploy, error) {
	return c.updateSpec(ctx, nameOrID, auth, func(spec *swarm.ServiceSpec, img string) {
		spec.TaskTemplate.ContainerSpec.Image = targetImage(img, tag)
		spec.UpdateConfig = &swarm.UpdateConfig{
			Parallelism:   1,
			Order:         order,
			FailureAction: swarm.UpdateFailureActionRollback,
			Monitor:       30 * time.Second,
		}
	})
}

func targetImage(current, tag string) string {
	if strings.Contains(tag, "/") {
		return tag
	}
	repo := ImageRepo(current)
	if strings.HasPrefix(tag, "@") {
		return repo + tag
	}
	return repo + ":" + tag
}

// ImageRepo drops the tag and the digest: ghcr.io/x/panel:edge@sha256:.. -> ghcr.io/x/panel.
func ImageRepo(img string) string {
	name, _ := splitImage(img)
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name = name[:i]
	}
	return name
}

// ImageTag extracts the tag and digest from an image reference.
// "ghcr.io/x/panel:0.1.0@sha256:..." -> "0.1.0@sha256:..."
// "ghcr.io/x/panel:0.1.0" -> "0.1.0"
// "ghcr.io/x/panel@sha256:..." -> "@sha256:..."
func ImageTag(img string) string {
	name, digest := splitImage(img)
	tag := ""
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		tag = name[i+1:]
	}
	if digest != "" {
		if tag != "" {
			return tag + "@" + digest
		}
		return "@" + digest
	}
	return tag
}

// DistributionInspect queries the registry for the given image reference and returns
// its manifest descriptor (including the content-addressable digest).
func (c *Client) DistributionInspect(ctx context.Context, image, auth string) (registry.DistributionInspect, error) {
	res, err := c.api.DistributionInspect(ctx, image, client.DistributionInspectOptions{
		EncodedRegistryAuth: auth,
	})
	return res.DistributionInspect, err
}

// updateSpec applies change to the service's spec; img is the image without
// its digest pin.
type RegistryAuth func(image string, labels map[string]string) (string, error)

func (c *Client) updateSpec(ctx context.Context, nameOrID string, auth RegistryAuth, change func(spec *swarm.ServiceSpec, img string)) (Deploy, error) {
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
		change(&spec, img)

		options := client.ServiceUpdateOptions{Version: s.Version, Spec: spec, QueryRegistry: true}
		if auth != nil {
			options.EncodedRegistryAuth, err = auth(spec.TaskTemplate.ContainerSpec.Image, spec.Labels)
			if err != nil {
				return d, err
			}
		}
		resp, err := c.api.ServiceUpdate(ctx, s.ID, options)
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

// SetLabels merges labels into the service spec; an empty value deletes the key.
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
	_, err = c.api.ServiceUpdate(ctx, s.ID, client.ServiceUpdateOptions{Version: s.Version, Spec: spec})
	return err
}

// RedeployStack force-updates every service in a stack.
func (c *Client) RedeployStack(ctx context.Context, stack string, auth RegistryAuth) ([]Deploy, error) {
	list, err := c.stackServices(ctx, stack)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("stack %q not found", stack)
	}
	// Keep going on failure so the stack is not left half-redeployed.
	var done []Deploy
	var errs []error
	for _, s := range list {
		d, err := c.ForceUpdate(ctx, s.ID, auth)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Spec.Name, err))
			continue
		}
		done = append(done, d)
	}
	return done, errors.Join(errs...)
}

// withSlot turns "<ts> <attrs> <msg>" into "<ts> [slot] <msg>"; an empty map leaves the line alone.
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

// LastFailure is why the service's recent tasks were rejected or failed, such
// as "No such image" or "no matching manifest", and empty when none were.
func (c *Client) LastFailure(ctx context.Context, serviceID string) (string, error) {
	list, err := c.tasks(ctx, serviceID)
	if err != nil {
		return "", err
	}
	return lastFailure(list), nil
}

// lastFailure looks through the newest few tasks, not only the newest one: a
// service that cannot start is retried, and the retry in progress has no
// error yet while the one before it has.
func lastFailure(tasks []swarm.Task) string {
	recent := slices.Clone(tasks)
	slices.SortFunc(recent, func(a, b swarm.Task) int { return b.CreatedAt.Compare(a.CreatedAt) })
	for _, t := range recent[:min(len(recent), 5)] {
		switch t.Status.State {
		case swarm.TaskStateRunning:
			return ""
		case swarm.TaskStateRejected, swarm.TaskStateFailed:
			if t.Status.Err != "" {
				return t.Status.Err
			}
		}
	}
	return ""
}

func (c *Client) tasks(ctx context.Context, serviceID string) ([]swarm.Task, error) {
	f := make(client.Filters).Add("service", serviceID)
	res, err := c.api.TaskList(ctx, client.TaskListOptions{Filters: f})
	return res.Items, err
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
	opts := client.ServiceLogsOptions{
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
		rc, err = c.api.TaskLogs(ctx, taskID, client.TaskLogsOptions(opts))
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
		defer pr.Close()
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

const maxContainerStatsStreams = 16

var containerStatsSlots = make(chan struct{}, maxContainerStatsStreams)

// Stats samples the containers on THIS node. It uses ContainerList, not the
// Swarm API: workers cannot list services or tasks, and task containers carry
// what is needed as labels. A service name narrows the sweep.
func (c *Client) Stats(ctx context.Context, service string) ([]TaskStats, error) {
	f := make(client.Filters).Add("label", "com.docker.swarm.task.id")
	if service != "" {
		f.Add("label", "com.docker.swarm.service.name="+service)
	}
	list, err := c.api.ContainerList(ctx, client.ContainerListOptions{Filters: f})
	if err != nil {
		return nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil
	}
	out := make([]TaskStats, len(list.Items))
	var wg sync.WaitGroup
	for i, ct := range list.Items {
		st := &out[i]
		st.Service = ct.Labels["com.docker.swarm.service.name"]
		st.Stack = ct.Labels["com.docker.stack.namespace"]
		st.Slot = slotFromTaskName(ct.Labels["com.docker.swarm.task.name"])
		select {
		case containerStatsSlots <- struct{}{}:
		case <-ctx.Done():
			st.Err = ctx.Err().Error()
			continue
		}
		wg.Go(func() {
			defer func() { <-containerStatsSlots }()
			if err := c.containerStats(ctx, ct.ID, st); err != nil {
				st.Err = err.Error()
			}
		})
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

// SortStats orders by node, service, replica. Exported: the panel merges agents' answers.
func SortStats(out []TaskStats) {
	slices.SortFunc(out, func(a, b TaskStats) int {
		return cmp.Or(
			strings.Compare(a.Node, b.Node),
			strings.Compare(a.Service, b.Service),
			cmp.Compare(a.Slot, b.Slot))
	})
}

func (c *Client) NodeName(ctx context.Context) (string, error) {
	info, err := c.api.Info(ctx, client.InfoOptions{})
	if err != nil {
		return "", err
	}
	return info.Info.Name, nil
}

// containerStats reads two samples in a row: CPU percent comes from the delta,
// and a single sample is not enough for it (its precpu block is empty).
func (c *Client) containerStats(ctx context.Context, id string, st *TaskStats) error {
	resp, err := c.api.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: true})
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
