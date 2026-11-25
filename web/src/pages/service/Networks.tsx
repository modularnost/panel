import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { api, call } from "../../api/client";
import { atLeast, useMe } from "../../auth";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { Select } from "../../components/Select";
import { Table } from "../../components/Table";
import { networksKey, useNetworks } from "../Networks";
import { useService } from "./Layout";

export function ServiceNetworks() {
  const { id } = useService();
  const admin = atLeast(useMe().data?.role, "admin");
  const client = useQueryClient();
  const key = ["service-networks", id];
  const current = useQuery({ queryKey: key, queryFn: () => call(api.getServiceNetworks(id)) });
  const networks = useNetworks();
  const [selected, setSelected] = useState("");
  const [draft, setDraft] = useState<{ version: string; add: string[]; remove: string[] }>();
  const [review, setReview] = useState<{ compose?: string }>();
  const state = current.data;
  const choices = networks.data?.filter((n) => n.scope === "swarm" && !n.protected && !n.config_only &&
    (!n.stack || n.stack === state?.stack) && !state?.attachments.some((a) => a.id === n.id) && !draft?.add.includes(n.id)) ?? [];
  const net = (networkID: string) => networks.data?.find((n) => n.id === networkID);
  const canEdit = admin && (state?.source === "service" || state?.source === "compose");
  const change = useMutation({
    mutationFn: (preview: boolean) => call(api.changeServiceNetworks(id, { ...draft!, preview })),
    onSuccess: async (result, preview) => {
      if (preview) {
        setReview(result);
      } else {
        setReview(undefined);
        setDraft(undefined);
        await Promise.all([client.invalidateQueries({ queryKey: key }), client.invalidateQueries({ queryKey: networksKey })]);
      }
    },
  });
  function toggle(networkID: string, field: "add" | "remove") {
    if (!state) return;
    change.reset();
    setDraft((old) => {
      const next = old ?? { version: state.version, add: [], remove: [] };
      return { ...next, [field]: next[field].includes(networkID) ? next[field].filter((id) => id !== networkID) : [...next[field], networkID] };
    });
  }
  return (
    <>
      <h2>Networks</h2>
      {(current.isPending || networks.isPending) && <p>Loading…</p>}
      {(current.error || networks.error) && <p className="error" role="alert">{(current.error ?? networks.error)?.message}</p>}
      {state?.source === "compose" && <p>Changes deploy the updated Compose file for <Link to={`/stacks/${encodeURIComponent(state.stack ?? "")}`}>{state.stack}</Link>.</p>}
      {state?.source === "external" && <p>Update the service’s <code>networks</code> in the source Compose file and redeploy <Link to={`/stacks/${encodeURIComponent(state.stack ?? "")}`}>{state.stack}</Link>. For a shared network, declare it with <code>external: true</code>.</p>}
      {state?.source === "system" && <p>The panel installer manages these connections.</p>}
      {state && networks.data && <>
        {state.attachments.length + (draft?.add.length ?? 0) > 0 ? (
          <Table scrollLabel="Service networks">
            <thead><tr><th scope="col">Network</th><th scope="col">Aliases</th>{canEdit && <th scope="col">Change</th>}</tr></thead>
            <tbody>
              {state.attachments.map((a) => {
                const n = net(a.id);
                const removing = draft?.remove.includes(a.id);
                return <tr key={a.id}>
                  <th scope="row">{n ? <Link to={`/networks/${n.id}`}>{n.name}</Link> : <span className="mono">{a.id}</span>}</th>
                  <td>{a.automatic ? "Automatic" : a.aliases.join(", ") || "—"}</td>
                  {canEdit && <td>{n && !n.protected && !a.automatic && <Button variant="text" onClick={() => toggle(a.id, "remove")}>{removing ? "Keep connected" : "Disconnect"}</Button>}{removing && <span className="muted"> Pending disconnect</span>}</td>}
                </tr>;
              })}
              {draft?.add.map((id) => <tr key={id}><th scope="row">{net(id)?.name ?? id}</th><td>—</td><td><Button variant="text" onClick={() => toggle(id, "add")}>Undo</Button><span className="muted"> Pending connection</span></td></tr>)}
            </tbody>
          </Table>
        ) : <p>No explicit network attachments.</p>}
        {canEdit && <div className="toolbar">
          <Select label="Available network" value={selected} onChange={setSelected} disabled={!choices.length}>
            <option value="">Select a network</option>
            {choices.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
          </Select>
          <Button disabled={!choices.some((n) => n.id === selected)} onClick={() => { toggle(selected, "add"); setSelected(""); }}>Connect network</Button>
          <Button disabled={change.isPending || !draft || draft.add.length + draft.remove.length === 0} onClick={() => change.mutate(true)}>Review changes</Button>
          {draft && <Button variant="text" onClick={() => { setDraft(undefined); change.reset(); void current.refetch(); }}>Discard changes</Button>}
        </div>}
      </>}
      {change.isError && !review && <p className="error" role="alert">{change.error.message}</p>}
      {change.isSuccess && !draft && <p role="status">Network changes applied.</p>}
      {review && draft && <Dialog headline="Apply network changes?" onDismiss={() => { if (!change.isPending) setReview(undefined); }} actions={<>
        <Button variant="text" disabled={change.isPending} onClick={() => setReview(undefined)}>Cancel</Button>
        <Button disabled={change.isPending} onClick={() => change.mutate(false)}>Apply changes</Button>
      </>}>
        <p>Swarm may replace running tasks while the service changes networks.</p>
        {draft.add.length > 0 && <p>Connect {draft.add.map((id) => net(id)?.name ?? id).join(", ")}</p>}
        {draft.remove.length > 0 && <p>Disconnect {draft.remove.map((id) => net(id)?.name ?? id).join(", ")}</p>}
        {review.compose && <details><summary>Updated Compose file</summary><pre>{review.compose}</pre></details>}
        {change.isError && <p className="error" role="alert">{change.error.message}</p>}
      </Dialog>}
    </>
  );
}

