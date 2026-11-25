import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { LinkButton } from "../components/Button";
import { api, call, retryableQueryError } from "../api/client";
import { RefreshWarning } from "../components/RefreshWarning";
import { useMe } from "../auth";
import { modulePath, slot, useModules } from "../modules";
import { Chip } from "../components/Chip";
import { Table } from "../components/Table";
import { useTableSort } from "../components/TableSort";
import { ActivityTime } from "../components/ActivityTime";
import type { Service } from "../api/generated";

export function Services() {
  const me = useMe();
  const sort = useTableSort<Service>({
    name: (s) => s.name, stack: (s) => s.stack, image: (s) => s.image, replicas: (s) => s.running,
    deploy: (s) => s.last_deploy_at ? Date.parse(s.last_deploy_at) : undefined,
    updated: (s) => s.swarm_updated_at ? Date.parse(s.swarm_updated_at) : undefined,
  });
  const services = useQuery({
    queryKey: ["services"],
    queryFn: () => call(api.listServices()),
    select: sort.rows,
    refetchInterval: ({ state: { data } }) => data?.some((row) => row.running < row.desired) ? 3000 : 10_000,
  });

  const modules = useModules();
  const actions = slot(modules.data, "services.action", me.data?.role);

  if (services.isPending) return <p>Loading…</p>;
  if (services.isError && (services.data === undefined || !retryableQueryError(services.error))) {
    return <p className="error">{services.error.message}</p>;
  }

  return (
    <>
      <RefreshWarning error={services.error} />
      <h1>Services</h1>
      {actions.length > 0 && (
        <p className="actions">
          {actions.map(({ module, contribution: c }) => (
            <LinkButton key={`${module}${c.path}`} variant="tonal" to={modulePath(module, c.path)}>
              {c.title}
            </LinkButton>
          ))}
        </p>
      )}
      {services.data.length === 0 && <p>No services.</p>}
      {services.data.length > 0 && (
        <Table>
          <thead>
            <tr>
              {sort.header("name", "Service")}
              {sort.header("stack", "Stack")}
              {sort.header("image", "Image")}
              {sort.header("replicas", "Running replicas")}
              {sort.header("deploy", "Last deploy")}
              {sort.header("updated", "Swarm updated")}
            </tr>
          </thead>
          <tbody>
            {services.data.map((s) => (
              <tr key={s.id}>
                <td>
                  <Link to={`/services/${s.id}`}>{s.name}</Link>
                </td>
                <td>{s.stack ?? ""}</td>
                <td className="mono">{s.image}</td>
                <td>
                  <Chip tone={s.healthy ? "ok" : "bad"}>
                    {s.running}/{s.desired}
                  </Chip>{" "}
                  <span className="muted">{s.mode}</span>
                </td>
                <td><ActivityTime value={s.last_deploy_at} /></td>
                <td><ActivityTime value={s.swarm_updated_at} /></td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}
