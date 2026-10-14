import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call } from "../api/client";
import { Select } from "../components/Select";
import { Button } from "../components/Button";
import { useDialogs } from "../components/Dialogs";
import { Chip } from "../components/Chip";
import { Table } from "../components/Table";

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

  const { confirm } = useDialogs();
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
              <td className="muted">
                {n.engine} {n.os}
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
      {setAvailability.isError && <p className="error">{setAvailability.error.message}</p>}

      <h2>Add a server</h2>
      <div className="toolbar">
        <label>
          Joins as
          <Select value={role} onChange={(v) => setRole(v as Role)}>
            <option value="worker">worker</option>
            <option value="manager">manager</option>
          </Select>
        </label>
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
