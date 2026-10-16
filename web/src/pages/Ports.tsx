import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call } from "../api/client";
import { Button } from "../components/Button";
import { Chip } from "../components/Chip";
import { useDialogs } from "../components/Dialogs";
import { Select } from "../components/Select";
import { Table } from "../components/Table";
import { TextField } from "../components/TextField";

type Proto = "tcp" | "udp";

export function Ports() {
  const queryClient = useQueryClient();
  const { confirm } = useDialogs();
  const ports = useQuery({
    queryKey: ["firewall-ports"],
    queryFn: () => call(api.listFirewallPorts()),
    refetchInterval: 10_000,
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["firewall-ports"] });

  const [port, setPort] = useState("");
  const [proto, setProto] = useState<Proto>("tcp");
  const [note, setNote] = useState("");
  const open = useMutation({
    mutationFn: (v: { port: number; proto: Proto; note?: string }) => call(api.allowFirewallPort(v)),
    onSuccess: () => {
      setPort("");
      setNote("");
      refresh();
    },
  });
  const close = useMutation({
    mutationFn: (v: { port: number; proto: Proto }) => call(api.closeFirewallPort(v)),
    onSuccess: refresh,
  });

  if (ports.isPending) return <p>Loading…</p>;
  if (ports.isError) return <p className="error">{ports.error.message}</p>;
  const { allowed, published } = ports.data;

  return (
    <>
      <h1>Ports</h1>
      <p className="muted">
        From the internet only SSH and the ports below answer. A service that publishes a port stays closed until the port is
        opened here; the swarm's own ports are for the cluster's nodes only.
      </p>

      <h2>Services that publish a port</h2>
      {published.length === 0 ? (
        <p className="muted">No service publishes a port.</p>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>Port</th>
              <th>Service</th>
              <th>From the internet</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {published.map((p) => (
              <tr key={`${p.service}-${p.port}-${p.proto}`}>
                <td className="mono">
                  {p.port}/{p.proto}
                </td>
                <td>{p.service}</td>
                <td>
                  <Chip tone={p.open ? "ok" : ""}>{p.open ? "open" : "closed"}</Chip>
                </td>
                <td>
                  {!p.open && (
                    <Button
                      variant="tonal"
                      disabled={open.isPending}
                      onClick={() => open.mutate({ port: p.port, proto: p.proto as Proto, note: p.service })}
                    >
                      Open
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}

      <h2>Open to everyone</h2>
      <Table>
        <thead>
          <tr>
            <th>Port</th>
            <th>Note</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {allowed.map((p) => (
            <tr key={`${p.port}-${p.proto}`}>
              <td className="mono">
                {p.port}/{p.proto}
              </td>
              <td className="muted">{p.note}</td>
              <td>
                {!p.fixed && (
                  <Button
                    variant="text"
                    disabled={close.isPending}
                    onClick={async () =>
                      (await confirm({
                        title: `Close ${p.port}/${p.proto}?`,
                        message: "Connections from the internet to it stop being accepted.",
                        action: "Close",
                      })) && close.mutate({ port: p.port, proto: p.proto as Proto })
                    }
                  >
                    Close
                  </Button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
      {close.isError && <p className="error">{close.error.message}</p>}

      <h2>Open a port</h2>
      <form
        className="toolbar"
        onSubmit={(e) => (e.preventDefault(), open.mutate({ port: Number(port), proto, note: note.trim() || undefined }))}
      >
        <TextField label="Port" value={port} onChange={setPort} placeholder="5432" required />
        <Select label="Protocol" value={proto} onChange={(v) => setProto(v as Proto)}>
          <option value="tcp">tcp</option>
          <option value="udp">udp</option>
        </Select>
        <TextField label="Note" value={note} onChange={setNote} placeholder="optional" />
        <Button variant="tonal" disabled={open.isPending}>
          Open
        </Button>
      </form>
      {open.isError && <p className="error">{open.error.message}</p>}
    </>
  );
}
