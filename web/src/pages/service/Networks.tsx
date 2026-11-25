import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { api, call } from "../../api/client";
import { atLeast, useMe } from "../../auth";
import { Button } from "../../components/Button";
import { Chip } from "../../components/Chip";
import { Dialog } from "../../components/Dialog";
import { Select } from "../../components/Select";
import { Table } from "../../components/Table";
import { networksKey, useNetworks } from "../Networks";
import { useService } from "./Layout";

type Draft = { version: string; add: string[]; remove: string[] };

export function ServiceNetworks() {
  const { id } = useService();
  const admin = atLeast(useMe().data?.role, "admin");
  const client = useQueryClient();
  const key = ["service-networks", id];
  const current = useQuery({ queryKey: key, queryFn: () => call(api.getServiceNetworks(id)) });
  const networks = useNetworks();
  const [selected, setSelected] = useState("");
  const [draft, setDraft] = useState<Draft>();
  const [review, setReview] = useState<{ compose?: string }>();
  const state = current.data;
  const choices = networks.data?.filter((n) => n.scope === "swarm" && !n.protected && !n.config_only &&
    (!n.stack || n.stack === state?.stack) && !state?.attachments.some((a) => a.id === n.id) && !draft?.add.includes(n.id)) ?? [];
  const net = (networkID: string) => networks.data?.find((n) => n.id === networkID);
  const nameOf = (networkID: string) => net(networkID)?.name ?? networkID;
  const canEdit = admin && (state?.source === "service" || state?.source === "compose");
  const pending = draft ? draft.add.length + draft.remove.length : 0;
  const change = useMutation({
    mutationFn: ({ draft, preview }: { draft: Draft; preview: boolean }) => call(api.changeServiceNetworks(id, { ...draft, preview })),
    onSuccess: async (result, { preview }) => {
      if (preview) {
        setReview(result);
        return;
      }
      setReview(undefined);
      setDraft(undefined);
      setSelected("");
      await Promise.all([client.invalidateQueries({ queryKey: key }), client.invalidateQueries({ queryKey: networksKey })]);
    },
  });

  function toggle(networkID: string, field: "add" | "remove") {
    if (!state) return;
    change.reset();
    setDraft((old) => {
      const next = old ?? { version: state.version, add: [], remove: [] };
      const ids = next[field];
      const updated = { ...next, [field]: ids.includes(networkID) ? ids.filter((id) => id !== networkID) : [...ids, networkID] };
      return updated.add.length + updated.remove.length ? updated : undefined;
    });
  }

  return (
    <>
      <h2>Networks</h2>
      {(current.isPending || networks.isPending) && <p>Loading…</p>}
      {(current.error || networks.error) && <p className="error" role="alert">{(current.error ?? networks.error)?.message}</p>}
      {state?.source === "external" && <p>Update the service’s <code>networks</code> in the source Compose file and redeploy <Link to={`/stacks/${encodeURIComponent(state.stack ?? "")}`}>{state.stack}</Link>. For a shared network, declare it with <code>external: true</code>.</p>}
      {state?.source === "system" && <p>The panel installer manages these connections.</p>}
      {state && networks.data && <>
        {state.attachments.length + (draft?.add.length ?? 0) > 0 ? (
          <Table scrollLabel="Service networks">
            <thead><tr><th scope="col">Network</th><th scope="col">Aliases</th>{canEdit && <th scope="col" className="service-network-actions">Actions</th>}</tr></thead>
            <tbody>
              {state.attachments.map((a) => {
                const n = net(a.id);
                const removing = draft?.remove.includes(a.id);
                return <tr key={a.id}>
                  <th scope="row">
                    {n ? <Link to={`/networks/${n.id}`}>{n.name}</Link> : <span className="mono">{a.id}</span>}
                    {removing && <> <Chip>Pending disconnection</Chip></>}
                  </th>
                  <td>{a.automatic ? "Automatic" : a.aliases.join(", ") || "—"}</td>
                  {canEdit && <td className="service-network-actions">{n && !n.protected && !a.automatic && (
                    <Button variant="text" disabled={change.isPending} onClick={() => toggle(a.id, "remove")}>
                      {removing ? "Undo" : "Disconnect…"}
                    </Button>
                  )}</td>}
                </tr>;
              })}
              {draft?.add.map((networkID) => <tr key={networkID}>
                <th scope="row">{nameOf(networkID)} <Chip>Pending connection</Chip></th>
                <td>—</td>
                <td className="service-network-actions"><Button variant="text" disabled={change.isPending} onClick={() => toggle(networkID, "add")}>Undo</Button></td>
              </tr>)}
            </tbody>
          </Table>
        ) : <p>No explicit network connections.</p>}
        {canEdit && (choices.length > 0 ? <div className="toolbar">
          <Select label="Network to connect" value={selected} onChange={setSelected} disabled={change.isPending}>
            <option value="">Select a network</option>
            {choices.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
          </Select>
          <Button variant="outlined" disabled={change.isPending || !choices.some((n) => n.id === selected)}
            onClick={() => { toggle(selected, "add"); setSelected(""); }}>
            Add to changes
          </Button>
        </div> : <p className="muted">No other networks available.</p>)}
        {canEdit && draft && <div className="banner toolbar service-network-changes">
          <span role="status">{pending === 1 ? "1 pending change" : `${pending} pending changes`}</span>
          <Button variant="text" disabled={change.isPending}
            onClick={() => { setDraft(undefined); setSelected(""); change.reset(); void current.refetch(); }}>
            Discard
          </Button>
          <Button disabled={change.isPending} onClick={() => change.mutate({ draft, preview: true })}>
            {change.isPending && !review ? "Checking…" : "Apply changes"}
          </Button>
        </div>}
      </>}
      {change.isError && !review && <p className="error" role="alert">{change.error.message}</p>}
      {change.isSuccess && !change.variables.preview && <p role="status">Network changes applied.</p>}
      {review && draft && <Dialog headline="Apply network changes?" onDismiss={() => { if (!change.isPending) setReview(undefined); }} actions={<>
        <Button variant="text" autoFocus disabled={change.isPending} onClick={() => setReview(undefined)}>Cancel</Button>
        <Button disabled={change.isPending} onClick={() => change.mutate({ draft, preview: false })}>
          {change.isPending ? "Applying…" : "Apply changes"}
        </Button>
      </>}>
        {draft.add.length > 0 && <p><strong>Connect</strong> {draft.add.map(nameOf).join(", ")}</p>}
        {draft.remove.length > 0 && <p><strong>Disconnect</strong> {draft.remove.map(nameOf).join(", ")}</p>}
        {state?.source === "compose" && <p>This redeploys the <strong>{state.stack}</strong> stack with the updated Compose file.</p>}
        <p>Swarm may replace running tasks while the service changes networks.</p>
        {review.compose && <details><summary>Updated Compose file</summary><pre>{review.compose}</pre></details>}
        {change.isError && <p className="error" role="alert">{change.error.message}</p>}
      </Dialog>}
    </>
  );
}
