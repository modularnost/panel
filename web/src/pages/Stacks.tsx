import { useQuery } from "@tanstack/react-query";
import { Fragment, useState } from "react";
import { Link } from "react-router";
import { api, call } from "../api/client";
import { atLeast, useMe } from "../auth";
import { Chip } from "../components/Chip";
import { Button } from "../components/Button";
import { Table } from "../components/Table";
import { useTableSort } from "../components/TableSort";
import type { Stack } from "../api/generated";

export function Stacks() {
  const me = useMe();
  const [expanded, setExpanded] = useState("");
  const sort = useTableSort<Stack>({
    name: (s) => s.name, services: (s) => s.service_count, replicas: (s) => s.running,
  });
  const stacks = useQuery({
    queryKey: ["stacks"],
    queryFn: () => call(api.listStacks()),
    select: sort.rows,
    refetchInterval: 3000,
  });

  if (stacks.isPending) return <p>Loading…</p>;
  if (stacks.isError) return <p className="error">{stacks.error.message}</p>;

  return (
    <>
      <h1>Stacks</h1>
      {atLeast(me.data?.role, "admin") && (
        <p>
          <Link to="/stacks/new">New stack</Link>
        </p>
      )}
      {stacks.data.length === 0 ? (
        <p className="muted">No stacks.</p>
      ) : (
        <Table>
          <thead>
            <tr>
              {sort.header("name", "Stack")}
              {sort.header("services", "Services")}
              {sort.header("replicas", "Running replicas")}
              <th>Compose file</th>
            </tr>
          </thead>
          <tbody>
            {stacks.data.map((s) => (
              <Fragment key={s.name}>
              <tr>
                <td>
                  <Button type="button" variant="text" aria-expanded={expanded === s.name}
                    aria-label={`Services in ${s.name}`}
                    onClick={() => setExpanded(expanded === s.name ? "" : s.name)}>
                    <span aria-hidden="true">{expanded === s.name ? "▾" : "▸"}</span>
                  </Button>{" "}
                  <Link to={`/stacks/${s.name}`}>{s.name}</Link>
                </td>
                <td>{s.service_count}</td>
                <td>
                  <Chip tone={s.healthy ? "ok" : "bad"}>
                    {s.running}/{s.desired}
                  </Chip>
                </td>
                <td className="muted">{s.managed ? "kept here" : "not kept"}</td>
              </tr>
              {expanded === s.name && (
                <tr><td colSpan={4}><ExpandedStack name={s.name} /></td></tr>
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
  if (stack.isError) return <p className="error" role="alert">{stack.error.message}</p>;
  if (!stack.data.services.length) return <p>No services.</p>;

  return (
    <Table aria-label={`Services in ${name}`}>
      <thead><tr><th scope="col">Service</th><th scope="col">Image</th><th scope="col">Replicas</th></tr></thead>
      <tbody>
        {stack.data.services.map((service) => (
          <tr key={service.id}>
            <td><Link to={`/services/${service.id}`}>{service.name}</Link></td>
            <td className="mono">{service.image}</td>
            <td><Chip tone={service.healthy ? "ok" : "bad"}>{service.running}/{service.desired}</Chip></td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
