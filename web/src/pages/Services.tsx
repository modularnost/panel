import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { LinkButton } from "../components/Button";
import { Banner } from "../components/Banner";
import { api, call } from "../api/client";
import { atLeast, useMe } from "../auth";
import { modulePath, slot, useModules } from "../modules";
import { useUpdates } from "../updates";
import { Chip } from "../components/Chip";
import { Table } from "../components/Table";
import { useTableSort } from "../components/TableSort";
import type { Service } from "../api/generated";

export function Services() {
  const me = useMe();
  const sort = useTableSort<Service>({
    name: (s) => s.name, stack: (s) => s.stack, image: (s) => s.image, replicas: (s) => s.running,
  });
  const updates = useUpdates(atLeast(me.data?.role, "admin"));
  const newest = updates.data?.releases[0];
  const services = useQuery({
    queryKey: ["services"],
    queryFn: () => call(api.listServices()),
    select: sort.rows,
    refetchInterval: 3000,
  });

  const modules = useModules();
  const actions = slot(modules.data, "services.action", me.data?.role);

  if (services.isPending) return <p>Loading…</p>;
  if (services.isError) return <p className="error">{services.error.message}</p>;

  return (
    <>
      {newest && (
        <Banner>
          {newest.tag} is available. <Link to="/settings">What changed</Link>
        </Banner>
      )}
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
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}
