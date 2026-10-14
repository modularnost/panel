import { useQuery } from "@tanstack/react-query";
import { api, call } from "../api/client";
import { formatTime, shortDigest } from "../format";

const STATUS_CLASS: Record<string, string> = { success: "ok", failed: "bad", pending: "" };

export function Deploys() {
  const deploys = useQuery({
    queryKey: ["deploys"],
    queryFn: () => call(api.listDeploys({ limit: 100 })),
    refetchInterval: 5000,
  });

  if (deploys.isPending) return <p>Loading…</p>;
  if (deploys.isError) return <p className="error">{deploys.error.message}</p>;

  return (
    <>
      <h1>Deploys</h1>
      {deploys.data.length === 0 ? (
        <p>No deploys yet.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>When</th>
              <th>Target</th>
              <th>Source</th>
              <th>By</th>
              <th>Status</th>
              <th>Image</th>
            </tr>
          </thead>
          <tbody>
            {deploys.data.map((d) => (
              <tr key={d.id}>
                <td>{formatTime(d.started_at)}</td>
                <td>
                  {d.service || d.stack}
                  {d.service && d.stack ? <span className="muted"> · {d.stack}</span> : null}
                </td>
                <td>{d.source}</td>
                <td>{d.triggered_by ?? ""}</td>
                <td>
                  <span className={`pill ${STATUS_CLASS[d.status] ?? ""}`}>{d.status}</span>
                  {d.error ? <div className="error">{d.error}</div> : null}
                </td>
                <td className="mono">
                  {d.old_digest || d.new_digest
                    ? `${shortDigest(d.old_digest)} → ${shortDigest(d.new_digest)}`
                    : ""}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
