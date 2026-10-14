import { useQuery } from "@tanstack/react-query";
import { api, call } from "../api/client";

export function Services() {
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
              <td>{s.name}</td>
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
