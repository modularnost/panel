import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call } from "../api/client";
import { Select } from "../components/Select";
import { Button } from "../components/Button";
import { useDialogs } from "../components/Dialogs";
import { Chip } from "../components/Chip";
import { Table } from "../components/Table";
import { TextField } from "../components/TextField";

type Role = "worker" | "manager";

export function Nodes() {
  const queryClient = useQueryClient();
  const [role, setRole] = useState<Role>("worker");

  const nodes = useQuery({
    queryKey: ["nodes"],
    queryFn: () => call(api.listNodes()),
    refetchInterval: 5000,
  });
  const join = useQuery({
    queryKey: ["join", role],
    queryFn: () => call(api.getJoin({ role })),
  });

  const [address, setAddress] = useState("");
  const peers = useQuery({
    queryKey: ["firewall-peers"],
    queryFn: () => call(api.listFirewallPeers()),
    refetchInterval: 10_000,
  });
  const allow = useMutation({
    mutationFn: () => call(api.allowFirewallPeer({ address: address.trim() })),
    onSuccess: () => {
      setAddress("");
      queryClient.invalidateQueries({ queryKey: ["firewall-peers"] });
    },
  });
  const revoke = useMutation({
    mutationFn: (a: string) => call(api.revokeFirewallPeer({ address: a })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["firewall-peers"] }),
  });

  const { confirm } = useDialogs();
  const [declined, setDeclined] = useState(0);
  const setAvailability = useMutation({
    mutationFn: (v: { id: string; availability: "active" | "pause" | "drain" }) =>
      call(api.setNodeAvailability(v.id, { availability: v.availability })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["nodes"] }),
  });
  const rotate = useMutation({
    mutationFn: () => call(api.rotateJoinToken({ role })),
    onSuccess: (fresh) => queryClient.setQueryData(["join", role], fresh),
  });

  const change = async (id: string, hostname: string, availability: "active" | "pause" | "drain") => {
    const ok =
      availability !== "drain" ||
      (await confirm({
        title: `Drain ${hostname}?`,
        message: "Its replicas move to other nodes.",
        action: "Drain",
      }));
    if (ok) setAvailability.mutate({ id, availability });
    // The select already shows the refused choice and its value prop did not change.
    else setDeclined((n) => n + 1);
  };

  if (nodes.isPending) return <p>Loading…</p>;
  if (nodes.isError) return <p className="error">{nodes.error.message}</p>;

  return (
    <>
      <h1>Nodes</h1>
      <Table>
        <thead>
          <tr>
            <th>Node</th>
            <th>Role</th>
            <th>Status</th>
            <th>Availability</th>
            <th>Address</th>
            <th>Firewall</th>
            <th>Engine</th>
          </tr>
        </thead>
        <tbody>
          {nodes.data.map((n) => (
            <tr key={n.id}>
              <td>{n.hostname}</td>
              <td>
                {n.role}
                {n.leader ? <span className="muted"> · leader</span> : null}
              </td>
              <td>
                <Chip tone={n.status === "ready" ? "ok" : "bad"}>{n.status}</Chip>
              </td>
              <td>
                <Select
                  key={declined}
                  value={n.availability}
                  disabled={setAvailability.isPending}
                  onChange={(v) => change(n.id, n.hostname, v as "active" | "pause" | "drain")}
                  aria-label={`Availability of ${n.hostname}`}
                >
                  <option value="active">active</option>
                  <option value="pause">pause</option>
                  <option value="drain">drain</option>
                </Select>
              </td>
              <td className="mono">{n.address ?? ""}</td>
              <td>
                <Chip tone={n.firewall === "applied" ? "ok" : n.firewall === "failed" ? "bad" : ""}>{n.firewall}</Chip>
              </td>
              <td className="muted">
                {n.engine} {n.os}
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
      {setAvailability.isError && <p className="error">{setAvailability.error.message}</p>}

      <h2>Add a server</h2>
      <p className="muted">The swarm's ports are closed to everyone but the nodes: allow the new server's address first.</p>
      <form className="toolbar" onSubmit={(e) => (e.preventDefault(), allow.mutate())}>
        <TextField label="Address of the server" value={address} onChange={setAddress} placeholder="203.0.113.7" required />
        <Button variant="tonal" disabled={allow.isPending}>
          Allow
        </Button>
      </form>
      {allow.isError && <p className="error">{allow.error.message}</p>}
      {peers.data && peers.data.length > 0 && (
        <p>
          Waiting to join:{" "}
          {peers.data.map((p) => (
            <span key={p.address}>
              <span className="mono">{p.address}</span>{" "}
              <Button variant="text" disabled={revoke.isPending} onClick={() => revoke.mutate(p.address)}>
                Revoke
              </Button>{" "}
            </span>
          ))}
        </p>
      )}
      <div className="toolbar">
        <Select label="Joins as" value={role} onChange={(v) => setRole(v as Role)}>
            <option value="worker">worker</option>
            <option value="manager">manager</option>
          </Select>
        <Button
          variant="tonal"
          disabled={rotate.isPending}
          onClick={async () =>
            (await confirm({
              title: "Rotate the token?",
              message: "A command copied earlier stops working for new servers.",
              action: "Rotate",
            })) && rotate.mutate()
          }
        >
          Rotate token
        </Button>
      </div>
      {join.isError && <p className="error">{join.error.message}</p>}
      {rotate.isError && <p className="error">{rotate.error.message}</p>}
      {join.data &&
        (join.data.command ? (
          <pre className="output">{join.data.command}</pre>
        ) : (
          <p className="muted">No join command is available.</p>
        ))}
    </>
  );
}
