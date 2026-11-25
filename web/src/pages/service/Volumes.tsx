import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { Link } from "react-router";
import { api, call } from "../../api/client";
import type { Node, ServiceVolume, Volume } from "../../api/generated";
import { atLeast, useMe } from "../../auth";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { Select } from "../../components/Select";
import { Switch } from "../../components/Switch";
import { Table } from "../../components/Table";
import { TextField } from "../../components/TextField";
import { volumePath } from "../Volumes";
import { useService } from "./Layout";

type ServiceVolumesViewProps = {
  volumes: ServiceVolume[];
  nodes: Node[];
  nodeVolumes: Volume[];
  canAttach: boolean;
  loading: boolean;
  error?: string;
  attachOpen: boolean;
  onOpenAttach: () => void;
  onCloseAttach: () => void;
  nodeID: string;
  onNodeID: (v: string) => void;
  volumeName: string;
  onVolumeName: (v: string) => void;
  target: string;
  onTarget: (v: string) => void;
  readOnly: boolean;
  onReadOnly: (v: boolean) => void;
  nodeVolumesLoading: boolean;
  attaching: boolean;
  attachError?: string;
  attached?: string;
  onAttach: (e: FormEvent) => void;
};

export function ServiceVolumesView(props: ServiceVolumesViewProps) {
  const formId = useId();
  const nodeNames = new Map(props.nodes.map((n) => [n.id, n.hostname]));
  const healthy = props.nodes.filter((n) => n.status === "ready" && n.availability === "active");
  const pinnedNode = props.volumes.find((v) => v.node)?.node ?? "";

  return (
    <>
      <header className="toolbar network-heading">
        <h2>Volumes</h2>
        {props.canAttach && (
          <Button variant="tonal" onClick={props.onOpenAttach} disabled={healthy.length === 0}>
            Attach volume
          </Button>
        )}
      </header>
      {props.loading && <p>Loading…</p>}
      {props.error && <p className="error" role="alert">{props.error}</p>}
      {props.attached && <p role="status">Attached {props.attached}.</p>}
      {!props.loading && (props.volumes.length > 0 ? (
        <Table scrollLabel="Service volumes">
          <thead>
            <tr>
              <th scope="col">Volume</th>
              <th scope="col">Node</th>
              <th scope="col">Container path</th>
              <th scope="col">Access</th>
            </tr>
          </thead>
          <tbody>
            {props.volumes.map((v) => (
              <tr key={`${v.name}:${v.target}`}>
                <th scope="row">
                  {v.node ? <Link to={volumePath(v.node, v.name)}>{v.name}</Link> : v.name}
                </th>
                <td>{v.node ? (nodeNames.get(v.node) ?? v.node) : <span className="muted">Any</span>}</td>
                <td className="mono">{v.target}</td>
                <td>{v.read_only ? "Read-only" : "Read-write"}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : (
        <p className="muted">No mounted volumes.</p>
      ))}

      {props.canAttach && props.attachOpen && (
        <Dialog
          headline="Attach volume"
          onDismiss={() => {
            if (!props.attaching) props.onCloseAttach();
          }}
          actions={
            <>
              <Button type="button" variant="text" disabled={props.attaching} onClick={props.onCloseAttach}>
                Cancel
              </Button>
              <Button form={formId} disabled={props.attaching || !props.nodeID || !props.volumeName || !props.target.trim()}>
                {props.attaching ? "Attaching…" : "Attach"}
              </Button>
            </>
          }
        >
          <p className="muted">
            Pins this service to the volume&apos;s node. Swarm may restart tasks when mounts or placement change.
          </p>
          <form id={formId} className="form" onSubmit={props.onAttach}>
            <Select label="Node" value={props.nodeID} onChange={props.onNodeID} disabled={props.attaching || !!pinnedNode} required>
              {healthy.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.hostname}
                </option>
              ))}
            </Select>
            <Select label="Volume" value={props.volumeName} onChange={props.onVolumeName} disabled={props.attaching || props.nodeVolumesLoading || props.nodeVolumes.length === 0} required>
              {props.nodeVolumes.length === 0 ? (
                <option value="">{props.nodeVolumesLoading ? "Loading…" : "No local volumes on this node"}</option>
              ) : (
                props.nodeVolumes.map((v) => (
                  <option key={v.name} value={v.name}>
                    {v.name}
                  </option>
                ))
              )}
            </Select>
            <TextField
              label="Container path"
              value={props.target}
              onChange={props.onTarget}
              supportingText="Absolute path inside the container (not /)"
              placeholder="/data"
              disabled={props.attaching}
              mono
              autoFocus
              required
            />
            <Switch label="Read-only" checked={props.readOnly} onChange={props.onReadOnly} />
            {props.attachError && <p className="error" role="alert">{props.attachError}</p>}
          </form>
        </Dialog>
      )}
    </>
  );
}

export function ServiceVolumes() {
  const s = useService();
  const admin = atLeast(useMe().data?.role, "admin");
  const canAttach = admin && s.mode !== "global" && s.mode !== "global-job";
  const client = useQueryClient();

  const [attachOpen, setAttachOpen] = useState(false);
  const [nodeID, setNodeID] = useState("");
  const [volumeName, setVolumeName] = useState("");
  const [target, setTarget] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [attached, setAttached] = useState<string>();

  const nodes = useQuery({ queryKey: ["nodes"], queryFn: () => call(api.listNodes()) });
  const mounts = useQuery({ queryKey: ["service-volumes", s.id], queryFn: () => call(api.listServiceVolumes(s.id)) });

  const healthy = (nodes.data ?? []).filter((n) => n.status === "ready" && n.availability === "active");
  const pinnedNode = mounts.data?.find((v) => v.node)?.node ?? "";
  const selectedNode = pinnedNode || (healthy.some((n) => n.id === nodeID) ? nodeID : healthy[0]?.id ?? "");

  const nodeVolumes = useQuery({
    queryKey: ["volumes", selectedNode],
    queryFn: () => call(api.listNodeVolumes(selectedNode)),
    enabled: attachOpen && !!selectedNode,
  });
  const choices = nodeVolumes.data ?? [];
  const selectedVolume = choices.some((v) => v.name === volumeName) ? volumeName : choices[0]?.name ?? "";

  const attach = useMutation({
    mutationFn: () => call(api.attachServiceVolume(s.id, { node: selectedNode, name: selectedVolume, target: target.trim(), read_only: readOnly || undefined })),
    onSuccess: async () => {
      const done = `${selectedVolume} at ${target.trim()}`;
      setAttachOpen(false);
      setAttached(done);
      setTarget("");
      setReadOnly(false);
      await Promise.all([
        client.invalidateQueries({ queryKey: ["service-volumes", s.id] }),
        client.invalidateQueries({ queryKey: ["volumes", selectedNode] }),
      ]);
    },
  });

  return (
    <ServiceVolumesView
      volumes={mounts.data ?? []}
      nodes={nodes.data ?? []}
      nodeVolumes={choices}
      canAttach={canAttach}
      loading={mounts.isPending}
      error={mounts.isError ? mounts.error.message : undefined}
      attachOpen={attachOpen}
      onOpenAttach={() => {
        attach.reset();
        setAttached(undefined);
        setTarget("");
        setReadOnly(false);
        setAttachOpen(true);
      }}
      onCloseAttach={() => {
        setAttachOpen(false);
        attach.reset();
      }}
      nodeID={selectedNode}
      onNodeID={(v) => {
        setNodeID(v);
        setVolumeName("");
      }}
      volumeName={selectedVolume}
      onVolumeName={setVolumeName}
      target={target}
      onTarget={setTarget}
      readOnly={readOnly}
      onReadOnly={setReadOnly}
      nodeVolumesLoading={nodeVolumes.isPending}
      attaching={attach.isPending}
      attachError={attach.isError ? attach.error.message : undefined}
      attached={attached}
      onAttach={(e) => {
        e.preventDefault();
        attach.mutate();
      }}
    />
  );
}
