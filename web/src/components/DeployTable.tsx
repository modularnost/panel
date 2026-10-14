import type { Deploy } from "../api/generated";
import { formatTime, shortDigest } from "../format";

const STATUS_CLASS: Record<string, string> = { success: "ok", failed: "bad", pending: "" };

export function DeployTable({ deploys }: { deploys: Deploy[] }) {
  if (deploys.length === 0) return <p>No deploys yet.</p>;

  return (
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
        {deploys.map((d) => (
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
              {d.old_digest || d.new_digest ? `${shortDigest(d.old_digest)} → ${shortDigest(d.new_digest)}` : ""}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
