import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call } from "../../api/client";
import { useService } from "./Layout";

export function Webhooks() {
  const s = useService();
  const queryClient = useQueryClient();
  const [action, setAction] = useState<"force_update" | "stack_redeploy">("force_update");

  const hooks = useQuery({
    queryKey: ["webhooks", s.id],
    queryFn: () => call(api.listServiceWebhooks(s.id)),
  });
  const reload = () => queryClient.invalidateQueries({ queryKey: ["webhooks", s.id] });

  const create = useMutation({
    mutationFn: () =>
      call(
        api.createWebhook(
          action === "force_update"
            ? { action, service: s.name }
            : { action, stack: s.stack },
        ),
      ),
    onSuccess: reload,
  });
  const remove = useMutation({
    mutationFn: (id: number) => call(api.deleteWebhook(id)),
    onSuccess: reload,
  });

  if (hooks.isPending) return <p>Loading…</p>;
  if (hooks.isError) return <p className="error">{hooks.error.message}</p>;

  return (
    <>
      {hooks.data.length === 0 ? (
        <p className="muted">No webhooks.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Runs</th>
              <th>Call</th>
              <th>Last used</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {hooks.data.map((h) => (
              <tr key={h.id}>
                <td>{h.action === "force_update" ? `Redeploy ${h.service}` : `Redeploy stack ${h.stack}`}</td>
                <td className="mono">POST {h.path}</td>
                <td>{h.last_triggered_at ? new Date(h.last_triggered_at).toLocaleString() : "never"}</td>
                <td>
                  <button
                    className="link"
                    disabled={remove.isPending}
                    onClick={() => window.confirm("Delete this webhook? Callers using it will start failing.") && remove.mutate(h.id)}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {remove.isError && <p className="error">{remove.error.message}</p>}

      <div className="toolbar">
        <label>
          New webhook
          <select value={action} onChange={(e) => setAction(e.target.value as typeof action)}>
            <option value="force_update">Redeploy this service</option>
            {s.stack && <option value="stack_redeploy">Redeploy its stack ({s.stack})</option>}
          </select>
        </label>
        <button onClick={() => create.mutate()} disabled={create.isPending}>
          Create
        </button>
      </div>
      {create.isError && <p className="error">{create.error.message}</p>}
    </>
  );
}
