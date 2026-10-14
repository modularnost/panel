import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { LinkButton } from "../components/Button";
import { api, call } from "../api/client";
import { atLeast, useMe } from "../auth";
import { modulePath, slot, useModules } from "../modules";
import { useUpdates } from "../updates";

export function Services() {
  const me = useMe();
  const updates = useUpdates(atLeast(me.data?.role, "admin"));
  const newest = updates.data?.releases[0];
  const services = useQuery({
    queryKey: ["services"],
    queryFn: () => call(api.listServices()),
    refetchInterval: 3000,
  });

  const modules = useModules();
  const actions = slot(modules.data, "services.action", me.data?.role);

  if (services.isPending) return <p>Loading…</p>;
  if (services.isError) return <p className="error">{services.error.message}</p>;

  return (
    <>
      {newest && (
        <p className="banner">
          {newest.tag} is available. <Link to="/updates">What changed</Link>
        </p>
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
        <table>
          <thead>
            <tr>
              <th>Service</th>
              <th>Stack</th>
              <th>Image</th>
              <th>Replicas</th>
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
                  <span className={`pill ${s.healthy ? "ok" : "bad"}`}>
                    {s.running}/{s.desired}
                  </span>{" "}
                  <span className="muted">{s.mode}</span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
