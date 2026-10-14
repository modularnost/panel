import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, call } from "../api/client";
import { atLeast, useMe } from "../auth";
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

  if (services.isPending) return <p>Loading…</p>;
  if (services.isError) return <p className="error">{services.error.message}</p>;
  if (services.data.length === 0) return <p>No services.</p>;

  return (
    <>
      {newest && (
        <p className="banner">
          {newest.tag} is available. <Link to="/updates">What changed</Link>
        </p>
      )}
      <h1>Services</h1>
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
    </>
  );
}
