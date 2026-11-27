# Size the panel and agent

Measure the panel on the manager and the agent on each worker you intend to
size. On the node running the containers, get their local IDs with `docker ps`
and pass them to `docker stats --no-stream`. Repeat at idle and during a
deployment, log streaming, and a Resources refresh with the expected number of
containers. Include a panel database backup when sizing the manager. Docker's
Linux CLI subtracts inactive file cache from its reported memory usage. See the
[Docker stats reference](https://docs.docker.com/reference/cli/docker/container/stats/).

Choose `deploy.resources.limits` from the observed load peaks with room for
startup, health checks, and subprocesses such as the Docker CLI. If you set
`GOMEMLIMIT`, leave room below the container memory limit for memory outside the
Go runtime. Repeat the same workload after tuning and compare container
restarts, CPU usage, and response times. See the
[Go garbage collection guide](https://go.dev/doc/gc-guide#Suggested_uses).

## Local idle baseline

### Single-node Swarm

Panel revision `df948f1` was built with Go 1.26.11 for Linux/amd64 and run
on Docker Desktop (Docker Engine 28.3.2). The Swarm had one manager, one
panel task, one global agent task, and three application tasks. The panel had
a fresh database, no installed modules, no connected browser, and its
release check disabled. Both roles used the image's 15-second healthcheck.
Eight `docker stats --no-stream` snapshots over 58 seconds showed:

| Container | Reported memory | Reported CPU |
| --- | ---: | ---: |
| Panel | 7.92–8.47 MiB | 0.00–0.52% |
| Agent | 3.91–3.95 MiB | 0.00% |

A per-role Unix-socket proxy counted Docker API requests before forwarding
them to the real daemon. The panel negotiated the API version and listed
services at startup, then listed services four times in the 58-second idle
window, at 15-second intervals. The agent was given its node name and made
no Docker API calls in that window.

Five direct `/healthcheck` invocations per role peaked at 5.63–5.88 MiB
process RSS, as reported by BusyBox `time -v`. Docker recorded 68–83 ms
per scheduled panel probe and 73–85 ms per agent probe, including exec
overhead. These short-lived processes share the container's limit. Allow
for a probe to overlap a workload peak when setting memory limits and
leave room for it below the limit when setting `GOMEMLIMIT`.

Use these idle values as a baseline for comparison with the workload
measurements above. In particular, sample during Resources refreshes and
deployments before choosing a CPU limit or tuning the Go runtime.

### Docker API fixture

Panel revision `1260ffc` was built with Go 1.26.1 for Linux/amd64 and run on
Docker Desktop. An isolated Docker API fixture returned empty service, task,
and container lists. The panel used a fresh database, and no browser was
connected. Both roles kept a 15-second healthcheck. Eight `docker stats
--no-stream` samples after startup gave the following ranges.

| Container | Reported memory | Reported CPU |
| --- | ---: | ---: |
| Panel | 12.75–12.93 MiB | 0.00% |
| Agent | 3.40–3.48 MiB | 0.00% |

`0.00%` is the CLI's displayed precision. One timed invocation per mode also
measured the standalone `/healthcheck` at 5.1–5.4 MiB peak process RSS and
`/panel healthcheck` at 11.3–11.6 MiB. These are isolated idle measurements.
Use the load measurements above when choosing limits for a real Swarm.
