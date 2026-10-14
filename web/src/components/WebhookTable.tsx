import type { Webhook } from "../api/generated";
import { Button } from "./Button";
import { useDialogs } from "./Dialogs";
import { Table } from "./Table";

type Props = {
  hooks: Webhook[];
  busy: boolean;
  onDelete: (id: number) => void;
};

export function WebhookTable({ hooks, busy, onDelete }: Props) {
  const { confirm } = useDialogs();
  if (hooks.length === 0) return <p className="muted">No webhooks.</p>;

  return (
    <Table>
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
              <Button
                variant="text"
                disabled={busy}
                onClick={async () =>
                  (await confirm({
                    title: "Delete this webhook?",
                    message: "Callers using it will start failing.",
                    action: "Delete",
                    danger: true,
                  })) && onDelete(h.id)
                }
              >
                Delete
              </Button>
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
