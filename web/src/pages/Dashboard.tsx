import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, call, retryableQueryError } from "../api/client";
import type { Deploy, NodeUsage, Service } from "../api/generated";
import { atLeast, useMe } from "../auth";
import { ActivityTime } from "../components/ActivityTime";
import { Chip } from "../components/Chip";
import { RefreshWarning } from "../components/RefreshWarning";
import { formatBytes, memoryUse } from "../format";

const overviewLimit = 5;

function lowCapacity(total?: number, available?: number) {
  return total !== undefined && total > 0 && available !== undefined && available / total < 0.1;
}

function nodeIssues(node: NodeUsage): string[] {
  if (node.status !== "ready") return [`Swarm status: ${node.status || "unknown"}`];
  const issues: string[] = [];
  if (node.memory_available !== undefined && lowCapacity(node.memory_total, node.memory_available)) issues.push(`${formatBytes(node.memory_available)} RAM available`);
  if (node.disk_available !== undefined && lowCapacity(node.disk_total, node.disk_available)) issues.push(`${formatBytes(node.disk_available)} free on Docker disk`);
  return issues;
}

function serviceOrder(a: Service, b: Service) {
  return Number(a.running >= a.desired) - Number(b.running >= b.desired) || a.name.localeCompare(b.name);
}

function Workloads({ services }: { services: Service[] }) {
  const groups = new Map<string, Service[]>();
  for (const service of [...services].sort(serviceOrder)) {
    const stack = service.stack || "";
    const group = groups.get(stack);
    if (group) group.push(service);
    else groups.set(stack, [service]);
  }
  const ordered = [...groups].sort(([nameA, a], [nameB, b]) =>
    Number(a[0].running >= a[0].desired) - Number(b[0].running >= b[0].desired) || nameA.localeCompare(nameB));
  if (!ordered.length) return <p>No services yet.</p>;
  return (
    <ul className="dashboard-list dashboard-workloads">
      {ordered.map(([stack, group]) => (
        <li key={stack}>
          <div className="dashboard-workload-heading">
            <h3>{stack ? <Link to={`/stacks/${encodeURIComponent(stack)}`}>{stack}</Link> : "Standalone services"}</h3>
          </div>
          <ul className="dashboard-list">
            {group.map((s) => (
              <li key={s.id} className="dashboard-service-row">
                <Link className="dashboard-service-link" to={`/services/${s.id}`}>
                  <span>{s.name}</span>
                  <Chip tone={s.running < s.desired ? "bad" : s.desired ? "ok" : ""}>
                    {s.desired || s.running ? `${s.running} / ${s.desired} replicas` : "Scaled to zero"}
                  </Chip>
                </Link>
              </li>
            ))}
          </ul>
        </li>
      ))}
    </ul>
  );
}

function NodeMetrics({ node, showDisk, linkToNodes }: { node: NodeUsage; showDisk: boolean; linkToNodes: boolean }) {
  const { cpu_percent: cpu, memory_total: memory, memory_available: available, disk_total: disk, disk_available: free } = node;
  const ramUsed = memory && available !== undefined ? memory - available : undefined;
  const diskUsed = disk && free !== undefined ? disk - free : undefined;
  const status = node.status !== "ready" ? node.status || "Unknown" : node.availability !== "active" ? node.availability : undefined;
  const label = node.name || node.id;
  return (
    <tr>
      <th scope="row">
        {linkToNodes ? <Link to="/nodes">{label}</Link> : label}
        {status && <span className="dashboard-node-status muted" data-critical={node.status !== "ready" || undefined}>{status}</span>}
      </th>
      <NodeUsageCell value={cpu} max={100} />
      <NodeUsageCell value={ramUsed} max={memory} critical={lowCapacity(memory, available)}
        detail={ramUsed !== undefined && memory ? `${memoryUse(ramUsed, memory)} used` : undefined} />
      {showDisk && (
        <NodeUsageCell value={diskUsed} max={disk} critical={lowCapacity(disk, free)}
          detail={disk && free !== undefined ? `${formatBytes(free)} free of ${formatBytes(disk)}` : undefined} />
      )}
    </tr>
  );
}

function NodeUsageCell({ value, max, detail, critical }: {
  value?: number;
  max?: number;
  detail?: string;
  critical?: boolean;
}) {
  if (value === undefined || max === undefined || max <= 0) {
    return <td title="Unavailable"><span aria-hidden="true">—</span><span className="dashboard-node-unavailable">Unavailable</span></td>;
  }
  const used = Math.max(0, Math.min(100, 100 * value / max));
  const percent = used > 0 && used < 1 ? "<1%" : `${Math.round(used)}%`;
  return <td title={detail} data-critical={critical || undefined}>{percent}</td>;
}

function DeployTarget({ deploy, services }: { deploy: Deploy; services: Service[] }) {
  const service = services.find((s) => s.name === deploy.service);
  if (service) return <Link to={`/services/${service.id}/deploys`}>{service.name}</Link>;
  if (!deploy.service && deploy.stack && services.some((s) => s.stack === deploy.stack)) {
    return <Link to={`/stacks/${encodeURIComponent(deploy.stack)}`}>{deploy.stack}</Link>;
  }
  return <>{deploy.service || deploy.stack}</>;
}

export function Dashboard() {
  const me = useMe();
  const services = useQuery({
    queryKey: ["services"],
    queryFn: () => call(api.listServices()),
    refetchInterval: ({ state: { data } }) => data?.some((s) => s.running < s.desired) ? 3000 : 10_000,
  });
  const deploys = useQuery({
    queryKey: ["deploys", "overview"],
    queryFn: () => call(api.listDeploys({ limit: overviewLimit })),
    refetchInterval: 10_000,
  });
  const nodes = useQuery({
    queryKey: ["node-usage"],
    queryFn: () => call(api.listNodeUsage()),
    refetchInterval: 15_000,
  });

  const serviceRows = services.isError && !retryableQueryError(services.error) ? undefined : services.data;
  const deployRows = deploys.isError && !retryableQueryError(deploys.error) ? undefined : deploys.data;
  const nodeRows = nodes.isError && !retryableQueryError(nodes.error) ? undefined : nodes.data;
  const affectedServices = (serviceRows ?? []).filter((s) => s.running < s.desired)
    .sort((a, b) => Number(a.running > 0) - Number(b.running > 0) || a.name.localeCompare(b.name));
  const orderedNodes = (nodeRows ?? []).map((node) => ({ node, issues: nodeIssues(node) }))
    .sort((a, b) => Number(!a.issues.length) - Number(!b.issues.length) || a.node.name.localeCompare(b.node.name));
  const affectedNodes = orderedNodes.filter(({ issues }) => issues.length > 0);
  const isAdmin = atLeast(me.data?.role, "admin");
  const activeNodes = orderedNodes.filter(({ node }) => node.status === "ready" && node.availability === "active").length;
  const showDisk = orderedNodes.some(({ node }) => node.disk_total !== undefined && node.disk_total > 0 && node.disk_available !== undefined);
  const attention = affectedServices.length + affectedNodes.length;
  const pendingDeploys = (deployRows ?? []).filter((d) => d.status === "pending");

  return (
    <div className="dashboard">
      <h1>Overview</h1>
      {(attention > 0 || pendingDeploys.length > 0) ? (
        <section className="dashboard-panel dashboard-summary" data-attention={attention > 0 || undefined} aria-labelledby="overview-attention">
          <h2 id="overview-attention">{attention > 0 ? "Needs attention" : "Pending deploys"}</h2>
          <ul className="dashboard-list">
            {affectedServices.slice(0, overviewLimit).map((s) => (
              <li key={s.id}>
                <div><Link to={`/services/${s.id}`}>{s.name}</Link><div className="dashboard-attention">{s.running} of {s.desired} replicas running</div></div>
                <Link to={`/services/${s.id}/logs`} aria-label={`Logs for ${s.name}`}>Logs</Link>
              </li>
            ))}
            {affectedNodes.map(({ node, issues }) => <li key={node.id}><strong>{node.name || node.id}</strong><span className="dashboard-attention">{issues.join(" · ")}</span></li>)}
            {pendingDeploys.map((d) => <li key={d.id}><DeployTarget deploy={d} services={serviceRows ?? []} /><Chip>Pending deploy</Chip></li>)}
          </ul>
          {affectedServices.length > overviewLimit && <p><Link to="/services">All services</Link></p>}
        </section>
      ) : services.isSuccess && !!serviceRows?.some((s) => s.desired > 0) && (
        <p className="dashboard-summary muted">All services meet their replica targets.</p>
      )}
      <div className="dashboard-panels">
        <section className="dashboard-panel" aria-labelledby="overview-workloads">
          <div className="dashboard-panel-heading">
            <h2 id="overview-workloads">Workloads</h2>
            <div className="dashboard-panel-actions">
              {isAdmin && <Link to="/stacks/new">Deploy stack</Link>}
              <Link to="/services">All services</Link>
            </div>
          </div>
          <RefreshWarning error={serviceRows ? services.error : null} />
          {services.isPending && <p role="status">Loading services…</p>}
          {!serviceRows && services.isError && <p className="error" role="alert">{services.error.message}</p>}
          {serviceRows && <Workloads services={serviceRows} />}
        </section>
        <div className="dashboard-secondary">
          <section className="dashboard-panel" aria-labelledby="overview-nodes">
            <div className="dashboard-panel-heading"><h2 id="overview-nodes">Nodes</h2>{isAdmin && <Link to="/nodes">All nodes</Link>}</div>
            <RefreshWarning error={nodeRows ? nodes.error : null} />
            {nodes.isPending && <p role="status">Loading nodes…</p>}
            {!nodeRows && nodes.isError && <p className="error" role="alert">{nodes.error.message}</p>}
            {nodeRows?.length === 0 && <p>No nodes available.</p>}
            {orderedNodes.length > 0 && (
              <>
                <p className="dashboard-node-summary muted">{activeNodes} of {orderedNodes.length} active</p>
                <table className="dashboard-nodes" aria-label="Node resource usage">
                  <thead><tr>
                    <th scope="col">Node</th>
                    <th scope="col">CPU</th>
                    <th scope="col">RAM</th>
                    {showDisk && <th scope="col">Docker disk</th>}
                  </tr></thead>
                  <tbody>{orderedNodes.map(({ node }) => <NodeMetrics key={node.id} node={node} showDisk={showDisk} linkToNodes={isAdmin} />)}</tbody>
                </table>
              </>
            )}
          </section>
          <section className="dashboard-panel" aria-labelledby="overview-deploys">
            <div className="dashboard-panel-heading"><h2 id="overview-deploys">Recent deploys</h2><Link to="/deploys">All deploys</Link></div>
            <RefreshWarning error={deployRows ? deploys.error : null} />
            {deploys.isPending && <p role="status">Loading deploys…</p>}
            {!deployRows && deploys.isError && <p className="error" role="alert">{deploys.error.message}</p>}
            {deployRows?.length === 0 && <p>No deploys yet.</p>}
            {!!deployRows?.length && <ul className="dashboard-list">
              {deployRows.map((d) => <li key={d.id}>
                <div><DeployTarget deploy={d} services={serviceRows ?? []} /><div className="muted"><ActivityTime value={d.started_at} /></div></div>
                <Chip tone={d.status === "failed" ? "bad" : d.status === "success" ? "ok" : ""}>{d.status}</Chip>
              </li>)}
            </ul>}
          </section>
        </div>
      </div>
    </div>
  );
}
