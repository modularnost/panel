import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, call, retryableQueryError } from "../api/client";
import { ActivityTime } from "../components/ActivityTime";
import { Chip } from "../components/Chip";
import { Icon } from "../components/Icon";
import { RefreshWarning } from "../components/RefreshWarning";

const overviewLimit = 5;

export function Dashboard() {
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

  const serviceRows = services.isError && !retryableQueryError(services.error) ? undefined : services.data;
  const deployRows = deploys.isError && !retryableQueryError(deploys.error) ? undefined : deploys.data;
  const underReplicated = serviceRows?.filter((s) => s.running < s.desired);
  const desired = serviceRows?.reduce((total, s) => total + s.desired, 0);
  const running = serviceRows?.reduce((total, s) => total + s.running, 0);

  return (
    <div className="dashboard">
      <h1>Overview</h1>
      <RefreshWarning error={serviceRows ? services.error : null} />
      <div className="dashboard-totals">
        <Link className="dashboard-total" to="/services">
          <span><Icon name="dns" size={20} />Services</span>
          <strong>{serviceRows?.length ?? "—"}</strong>
        </Link>
        <Link className="dashboard-total" to="/resources">
          <span><Icon name="monitoring" size={20} />Running replicas</span>
          <strong>{running ?? "—"}{desired !== undefined && <small> / {desired}</small>}</strong>
        </Link>
        <a className="dashboard-total" href="#attention">
          <span><Icon name="info" size={20} />Services below target</span>
          <strong className={underReplicated?.length ? "dashboard-problem" : undefined}>{underReplicated?.length ?? "—"}</strong>
        </a>
      </div>
      <div className="dashboard-panels">
        <section className="dashboard-panel" aria-labelledby="attention">
          <h2 id="attention">Needs attention</h2>
          {services.isPending && <p role="status">Loading services…</p>}
          {!serviceRows && services.isError && <p className="error">{services.error.message}</p>}
          {underReplicated?.length === 0 && <p>{serviceRows?.length ? "All services meet their replica targets." : "No services yet."}</p>}
          {underReplicated && underReplicated.length > 0 && (
            <>
              <ul className="dashboard-list">
                {underReplicated.slice(0, overviewLimit).map((s) => (
                  <li key={s.id}>
                    <div><Link to={`/services/${s.id}`}>{s.name}</Link>{s.stack && <div className="muted">{s.stack}</div>}</div>
                    <Chip tone="bad">{s.running}/{s.desired} replicas</Chip>
                  </li>
                ))}
              </ul>
              {underReplicated.length > overviewLimit && <p><Link to="/services">View all services</Link></p>}
            </>
          )}
        </section>
        <section className="dashboard-panel" aria-labelledby="recent-deploys">
          <div className="dashboard-panel-heading">
            <h2 id="recent-deploys">Recent deploys</h2>
            <Link to="/deploys">View all</Link>
          </div>
          <RefreshWarning error={deployRows ? deploys.error : null} />
          {deploys.isPending && <p role="status">Loading deploys…</p>}
          {!deployRows && deploys.isError && <p className="error">{deploys.error.message}</p>}
          {deployRows?.length === 0 && <p>No deploys yet.</p>}
          {deployRows && deployRows.length > 0 && (
            <ul className="dashboard-list">
              {deployRows.map((d) => (
                <li key={d.id}>
                  <div><div>{d.service || d.stack}</div><div className="muted"><ActivityTime value={d.started_at} /></div></div>
                  <Chip tone={d.status === "failed" ? "bad" : d.status === "success" ? "ok" : ""}>{d.status}</Chip>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </div>
  );
}
