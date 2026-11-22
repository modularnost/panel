import { useQuery } from "@tanstack/react-query";
import { Fragment, useState } from "react";
import { Link } from "react-router";
import { api, call, retryableQueryError } from "../api/client";
import { RefreshWarning } from "../components/RefreshWarning";
import { atLeast, useMe } from "../auth";
import { Chip } from "../components/Chip";
import { Icon } from "../components/Icon";
import { Button } from "../components/Button";
import { Table } from "../components/Table";
import { sortRows, useTableSort } from "../components/TableSort";
import { ActivityTime } from "../components/ActivityTime";
import type { Stack } from "../api/generated";

export function Stacks() {
  const me = useMe();
  const [expanded, setExpanded] = useState("");
  const sort = useTableSort<Stack>({
    name: (s) => s.name, services: (s) => s.service_count, replicas: (s) => s.running,
    deploy: (s) => s.last_deploy_at ? Date.parse(s.last_deploy_at) : undefined,
    updated: (s) => s.swarm_updated_at ? Date.parse(s.swarm_updated_at) : undefined,
  });
  const stacks = useQuery({
    queryKey: ["stacks"],
    queryFn: () => call(api.listStacks()),
    select: sort.rows,
    refetchInterval: 3000,
  });

  if (stacks.isPending) return <p>Loading…</p>;
  if (stacks.isError && (stacks.data === undefined || !retryableQueryError(stacks.error))) {
    return <p className="error">{stacks.error.message}</p>;
  }

  return (
    <>
      <RefreshWarning error={stacks.error} />
      <h1>Stacks</h1>
      {atLeast(me.data?.role, "admin") && (
        <p>
          <Link to="/stacks/new">New stack</Link>
        </p>
      )}
      {stacks.data.length === 0 ? (
        <p className="muted">No stacks.</p>
      ) : (
        <Table className="stacks-table">
          <thead>
            <tr>
              {sort.header("name", "Stack")}
              {sort.header("services", "Services")}
              {sort.header("replicas", "Running replicas")}
              <th>Compose file</th>
              {sort.header("deploy", "Last deploy")}
              {sort.header("updated", "Latest service update")}
            </tr>
          </thead>
          <tbody>
            {stacks.data.map((s) => (
              <Fragment key={s.name}>
              <tr>
                <td>
                  <div className="stack-name">
                    <Button type="button" variant="icon" aria-expanded={expanded === s.name}
                      aria-label={`Services in ${s.name}`}
                      onClick={() => setExpanded(expanded === s.name ? "" : s.name)}>
                      <Icon name="chevron_right" size={18} />
                    </Button>
                    <Link to={`/stacks/${s.name}`}>{s.name}</Link>
                  </div>
                </td>
                <td>{s.service_count}</td>
                <td>
                  <Chip tone={s.healthy ? "ok" : "bad"}>
                    {s.running}/{s.desired}
                  </Chip>
                </td>
                <td className="muted">{s.managed ? "kept here" : "not kept"}</td>
                <td><ActivityTime value={s.last_deploy_at} /></td>
                <td><ActivityTime value={s.swarm_updated_at} /></td>
              </tr>
              {expanded === s.name && (
                <tr><td colSpan={6}><ExpandedStack name={s.name} /></td></tr>
              )}
              </Fragment>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}

function ExpandedStack({ name }: { name: string }) {
  const stack = useQuery({
    queryKey: ["stack", name],
    queryFn: () => call(api.getStack(name)),
    refetchInterval: 3000,
  });
  if (stack.isPending) return <p role="status">Loading services…</p>;
  if (stack.isError && (stack.data === undefined || !retryableQueryError(stack.error))) {
    return <p className="error" role="alert">{stack.error.message}</p>;
  }
  if (!stack.data.services.length) {
    return <><RefreshWarning error={stack.error} /><p>No services.</p></>;
  }

  return (
    <>
    <RefreshWarning error={stack.error} />
    <ul className="stack-services" role="list" aria-label={`Services in ${name}`}>
      {sortRows(stack.data.services, (service) => service.name, false).map((service) => (
        <li key={service.id}>
          <div>
            <Link to={`/services/${service.id}`}>{service.name}</Link>
            <div className="muted mono">{service.image}</div>
          </div>
          <span aria-label={`Running replicas: ${service.running} of ${service.desired}`} role="group">
            <Chip tone={service.healthy ? "ok" : "bad"}>{service.running}/{service.desired}</Chip>
          </span>
        </li>
      ))}
    </ul>
    </>
  );
}
