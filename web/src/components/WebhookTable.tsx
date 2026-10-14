import type { Webhook } from "../api/generated";

type Props = {
  hooks: Webhook[];
  busy: boolean;
  onDelete: (id: number) => void;
};

export function WebhookTable({ hooks, busy, onDelete }: Props) {
  if (hooks.length === 0) return <p className="muted">No webhooks.</p>;

  return (
    <table>
      <thead>
        <tr>
          <th>Redeploys</th>
          <th>Call</th>
          <th>Last used</th>
          <th />
        </tr>
      </thead>
      <tbody>
        {hooks.map((h) => (
          <tr key={h.id}>
            <td>{h.action === "force_update" ? `service ${h.service}` : `stack ${h.stack}`}</td>
            <td className="mono">POST {h.path}</td>
            <td>{h.last_triggered_at ? new Date(h.last_triggered_at).toLocaleString() : "never"}</td>
            <td>
              <button
                className="link"
                disabled={busy}
                onClick={() =>
                  window.confirm("Delete this webhook? Callers using it will start failing.") && onDelete(h.id)
                }
              >
                Delete
              </button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
