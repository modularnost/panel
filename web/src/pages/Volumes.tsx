import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { api, call } from "../api/client";
import type { Node, Service, Volume } from "../api/generated";
import { atLeast, useMe } from "../auth";
import { ActivityTime } from "../components/ActivityTime";
import { Button } from "../components/Button";
import { Chip } from "../components/Chip";
import { Dialog } from "../components/Dialog";
import { useDialogs } from "../components/Dialogs";
import { Select } from "../components/Select";
import { Switch } from "../components/Switch";
import { Table } from "../components/Table";
import { useTableSort } from "../components/TableSort";
import { TextField } from "../components/TextField";

export function volumePath(node: string, name: string) {
  return `/volumes/${encodeURIComponent(node)}/${encodeURIComponent(name)}`;
}

export function deleteBlockReason(volume: Pick<Volume, "services">): string | undefined {
  if (volume.services.length === 0) return undefined;
  return `In use by ${volume.services.join(", ")}`;
}

export function filterVolumes(volumes: Volume[], nodeFilter: string, search: string, nodeNames: Map<string, string>): Volume[] {
  const q = search.trim().toLowerCase();
  return volumes.filter((v) => {
    if (nodeFilter && v.node !== nodeFilter) return false;
    if (!q) return true;
    const host = nodeNames.get(v.node) ?? v.node;
    return [v.name, v.driver, host, ...v.services].some((s) => s.toLowerCase().includes(q));
  });
}

function healthyNodes(nodes: Node[]): Node[] {
  return nodes.filter((n) => n.status === "ready" && n.availability === "active");
}

type VolumesViewProps = {
  volumes: Volume[];
  nodes: Node[];
  services: Service[];
  unavailableNodes: string[];
  admin: boolean;
  loading: boolean;
  error?: string;
  search: string;
  onSearch: (v: string) => void;
  nodeFilter: string;
  onNodeFilter: (v: string) => void;
  createOpen: boolean;
  onOpenCreate: () => void;
  onCloseCreate: () => void;
  createNode: string;
  onCreateNode: (v: string) => void;
  createName: string;
  onCreateName: (v: string) => void;
  creating: boolean;
  createError?: string;
  createdName?: string;
  onCreate: (e: FormEvent) => void;
  deletingKey?: string;
  deleteError?: string;
  onDelete: (v: Volume) => void;
};

export function VolumesView(props: VolumesViewProps) {
  const formId = useId();
  const nodeNames = new Map(props.nodes.map((n) => [n.id, n.hostname]));
  const serviceIDs = new Map(props.services.map((s) => [s.name, s.id]));
  const sort = useTableSort<Volume>({
    name: (v) => v.name,
    driver: (v) => v.driver,
    node: (v) => nodeNames.get(v.node) ?? v.node,
    consumers: (v) => v.services.length,
  });
  const rows = sort.rows(filterVolumes(props.volumes, props.nodeFilter, props.search, nodeNames));
  const createChoices = healthyNodes(props.nodes);

  return (
    <>
      <header className="toolbar network-heading">
        <h1>Volumes</h1>
        {props.admin && (
          <Button onClick={props.onOpenCreate} disabled={createChoices.length === 0}>
            Create volume
          </Button>
        )}
      </header>
      <div className="toolbar">
        <TextField label="Search volumes" value={props.search} onChange={props.onSearch} />
        <Select label="Node" value={props.nodeFilter} onChange={props.onNodeFilter}>
          <option value="">All nodes</option>
          {props.nodes.map((n) => (
            <option key={n.id} value={n.id}>
              {n.hostname}
            </option>
          ))}
        </Select>
      </div>
      {props.loading && <p>Loading…</p>}
      {props.error && <p className="error" role="alert">{props.error}</p>}
      {props.unavailableNodes.length > 0 && (
        <p role="status">Local volumes unavailable on {props.unavailableNodes.join(", ")}.</p>
      )}
      {props.createdName && <p role="status">Created volume {props.createdName}.</p>}
      {props.deleteError && <p className="error" role="alert">{props.deleteError}</p>}
      {!props.loading && (rows.length > 0 ? (
        <Table scrollLabel="Volumes">
          <thead>
            <tr>
              {sort.header("name", "Volume")}
              {sort.header("driver", "Driver")}
              {sort.header("node", "Node")}
              {sort.header("consumers", "Consumers")}
              {props.admin && <th scope="col">Actions</th>}
            </tr>
          </thead>
          <tbody>
            {rows.map((v) => {
              const host = nodeNames.get(v.node) ?? v.node;
              const blocked = deleteBlockReason(v);
              const rowKey = `${v.node}/${v.name}`;
              return (
                <tr key={rowKey}>
                  <th scope="row">
                    <Link to={volumePath(v.node, v.name)}>{v.name}</Link>
                  </th>
                  <td>{v.driver}</td>
                  <td>{host}</td>
                  <td>
                    {v.services.length === 0 ? (
                      <span className="muted">None</span>
                    ) : (
                      v.services.map((svcName, i) => {
                        const svcID = serviceIDs.get(svcName);
                        return (
                          <span key={svcName}>
                            {i > 0 && ", "}
                            {svcID ? <Link to={`/services/${svcID}`}>{svcName}</Link> : svcName}
                          </span>
                        );
                      })
                    )}
                  </td>
                  {props.admin && (
                    <td>
                      <div className="table-actions">
                        <Button
                          variant="text"
                          disabled={!!blocked || props.deletingKey === rowKey}
                          onClick={() => props.onDelete(v)}
                        >
                          {props.deletingKey === rowKey ? "Deleting…" : "Delete"}
                        </Button>
                        {blocked && <span className="muted">{blocked}</span>}
                      </div>
                    </td>
                  )}
                </tr>
              );
            })}
          </tbody>
        </Table>
      ) : (
        <p className="muted">{props.volumes.length === 0 ? "No local volumes." : "No volumes match these filters."}</p>
      ))}

      {props.admin && props.createOpen && (
        <Dialog
          headline="Create volume"
          onDismiss={() => {
            if (!props.creating) props.onCloseCreate();
          }}
          actions={
            <>
              <Button type="button" variant="text" disabled={props.creating} onClick={props.onCloseCreate}>
                Cancel
              </Button>
              <Button form={formId} disabled={props.creating || !props.createNode || !props.createName.trim()}>
                {props.creating ? "Creating…" : "Create"}
              </Button>
            </>
          }
        >
          <form id={formId} className="form" onSubmit={props.onCreate}>
            <Select label="Node" value={props.createNode} onChange={props.onCreateNode} disabled={props.creating} required>
              {createChoices.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.hostname}
                </option>
              ))}
            </Select>
            <TextField
              label="Name"
              value={props.createName}
              onChange={props.onCreateName}
              pattern="^[a-zA-Z0-9][a-zA-Z0-9_.-]*$"
              supportingText="Local volume on the selected node"
              placeholder="app-data"
              disabled={props.creating}
              mono
              autoFocus
              required
            />
            {props.createError && <p className="error" role="alert">{props.createError}</p>}
          </form>
        </Dialog>
      )}
    </>
  );
}

export function Volumes() {
  const admin = atLeast(useMe().data?.role, "admin");
  const client = useQueryClient();
  const { confirm } = useDialogs();
  const [search, setSearch] = useState("");
  const [nodeFilter, setNodeFilter] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [createNode, setCreateNode] = useState("");
  const [createName, setCreateName] = useState("");
  const [createdName, setCreatedName] = useState<string>();

  const nodes = useQuery({ queryKey: ["nodes"], queryFn: () => call(api.listNodes()) });
  const services = useQuery({ queryKey: ["services"], queryFn: () => call(api.listServices()) });
  const nodeList = nodes.data ?? [];
  const createChoices = healthyNodes(nodeList);

  const volumeQueries = useQueries({
    queries: nodeList.map((n) => ({
      queryKey: ["volumes", n.id],
      queryFn: () => call(api.listNodeVolumes(n.id)),
      enabled: n.status === "ready",
    })),
  });

  const volumes: Volume[] = [];
  const unavailableNodes: string[] = [];
  nodeList.forEach((n, i) => {
    const q = volumeQueries[i];
    if (n.status !== "ready" || q?.isError) {
      unavailableNodes.push(n.hostname);
    } else if (q?.data) {
      volumes.push(...q.data);
    }
  });

  const selectedCreateNode = createChoices.some((n) => n.id === createNode)
    ? createNode
    : createChoices.find((n) => n.id === nodeFilter)?.id ?? createChoices[0]?.id ?? "";

  const create = useMutation({
    mutationFn: () => call(api.createNodeVolume(selectedCreateNode, createName.trim())),
    onSuccess: async (v) => {
      setCreateOpen(false);
      setCreatedName(v.name);
      setCreateName("");
      await client.invalidateQueries({ queryKey: ["volumes", v.node] });
    },
  });

  const remove = useMutation({
    mutationFn: (v: Volume) => call(api.removeNodeVolume(v.node, v.name)),
    onSuccess: async (_data, v) => {
      await client.invalidateQueries({ queryKey: ["volumes", v.node] });
    },
  });

  const nodeNames = new Map(nodeList.map((n) => [n.id, n.hostname]));

  return (
    <VolumesView
      volumes={volumes}
      nodes={nodeList}
      services={services.data ?? []}
      unavailableNodes={unavailableNodes}
      admin={admin}
      loading={nodes.isPending || (nodeList.length > 0 && volumeQueries.some((q, i) => nodeList[i].status === "ready" && q.isPending))}
      error={nodes.isError ? nodes.error.message : undefined}
      search={search}
      onSearch={setSearch}
      nodeFilter={nodeFilter}
      onNodeFilter={setNodeFilter}
      createOpen={createOpen}
      onOpenCreate={() => {
        create.reset();
        setCreatedName(undefined);
        setCreateName("");
        setCreateNode(selectedCreateNode);
        setCreateOpen(true);
      }}
      onCloseCreate={() => {
        setCreateOpen(false);
        create.reset();
      }}
      createNode={selectedCreateNode}
      onCreateNode={setCreateNode}
      createName={createName}
      onCreateName={setCreateName}
      creating={create.isPending}
      createError={create.isError ? create.error.message : undefined}
      createdName={createdName}
      onCreate={(e) => {
        e.preventDefault();
        create.mutate();
      }}
      deletingKey={remove.isPending && remove.variables ? `${remove.variables.node}/${remove.variables.name}` : undefined}
      deleteError={remove.isError ? remove.error.message : undefined}
      onDelete={async (v) => {
        const host = nodeNames.get(v.node) ?? v.node;
        if (await confirm({
          title: `Delete ${v.name}?`,
          message: `Removes local volume ${v.name} from ${host}. Stored data is deleted.`,
          action: "Delete",
          danger: true,
        })) {
          remove.mutate(v);
        }
      }}
    />
  );
}

type VolumeDetailViewProps = {
  volume?: Volume;
  nodeHostname: string;
  services: Service[];
  admin: boolean;
  loading: boolean;
  error?: string;
  serviceID: string;
  onServiceID: (v: string) => void;
  target: string;
  onTarget: (v: string) => void;
  readOnly: boolean;
  onReadOnly: (v: boolean) => void;
  attaching: boolean;
  attachError?: string;
  attached?: string;
  onAttach: (e: FormEvent) => void;
  deleting: boolean;
  deleteError?: string;
  onDelete: () => void;
};

export function VolumeDetailView(props: VolumeDetailViewProps) {
  const v = props.volume;
  const serviceIDs = new Map(props.services.map((s) => [s.name, s.id]));
  const attachable = props.services.filter((s) => s.mode !== "global" && s.mode !== "global-job");
  const blocked = v ? deleteBlockReason(v) : undefined;
  const labels = Object.entries(v?.labels ?? {});

  return (
    <>
      <p><Link to="/volumes">Volumes</Link></p>
      {props.loading && <p>Loading…</p>}
      {props.error && <p className="error" role="alert">{props.error}</p>}
      {v && (
        <>
          <h1>{v.name}</h1>
          <dl className="facts">
            <dt>Driver</dt>
            <dd>{v.driver}</dd>
            <dt>Node</dt>
            <dd>{props.nodeHostname}</dd>
            <dt>Created</dt>
            <dd>{v.created_at ? <ActivityTime value={v.created_at} /> : "—"}</dd>
            {labels.length > 0 && (
              <>
                <dt>Labels</dt>
                <dd>{labels.map(([k, val]) => <Chip key={k}>{k}={val}</Chip>)}</dd>
              </>
            )}
          </dl>

          <h2>Consumers</h2>
          {v.services.length > 0 ? (
            <Table scrollLabel="Consumers">
              <thead>
                <tr>
                  <th scope="col">Service</th>
                </tr>
              </thead>
              <tbody>
                {v.services.map((name) => {
                  const id = serviceIDs.get(name);
                  return (
                    <tr key={name}>
                      <th scope="row">{id ? <Link to={`/services/${id}`}>{name}</Link> : name}</th>
                    </tr>
                  );
                })}
              </tbody>
            </Table>
          ) : (
            <p className="muted">No service consumers.</p>
          )}

          {props.admin && (
            <>
              <h2>Attach to service</h2>
              <p className="muted">
                Mounts this volume on a standalone service and pins it to {props.nodeHostname}. Swarm may restart tasks when mounts or placement change.
              </p>
              <form className="form" onSubmit={props.onAttach}>
                <Select label="Service" value={props.serviceID} onChange={props.onServiceID} required>
                  <option value="">Choose a service…</option>
                  {attachable.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </Select>
                <TextField
                  label="Container path"
                  value={props.target}
                  onChange={props.onTarget}
                  supportingText="Absolute path inside the container (not /)"
                  placeholder="/data"
                  mono
                  required
                />
                <Switch label="Read-only" checked={props.readOnly} onChange={props.onReadOnly} />
                {props.attachError && <p className="error" role="alert">{props.attachError}</p>}
                {props.attached && <p role="status">Attached to {props.attached}.</p>}
                <Button variant="tonal" disabled={props.attaching || !props.serviceID || !props.target.trim()}>
                  {props.attaching ? "Attaching…" : "Attach volume"}
                </Button>
              </form>

              <h2>Delete volume</h2>
              {blocked && <p className="muted">{blocked}. Remove or update those services before deleting.</p>}
              <Button variant="danger" disabled={!!blocked || props.deleting} onClick={props.onDelete}>
                {props.deleting ? "Deleting…" : "Delete volume"}
              </Button>
              {props.deleteError && <p className="error" role="alert">{props.deleteError}</p>}
            </>
          )}
        </>
      )}
    </>
  );
}

export function VolumeDetail() {
  const { node = "", name = "" } = useParams();
  const admin = atLeast(useMe().data?.role, "admin");
  const client = useQueryClient();
  const navigate = useNavigate();
  const { confirm } = useDialogs();
  const [serviceID, setServiceID] = useState("");
  const [target, setTarget] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [attached, setAttached] = useState<string>();

  const nodes = useQuery({ queryKey: ["nodes"], queryFn: () => call(api.listNodes()) });
  const services = useQuery({ queryKey: ["services"], queryFn: () => call(api.listServices()) });
  const detail = useQuery({
    queryKey: ["volumes", node, name],
    queryFn: () => call(api.getNodeVolume(node, name)),
  });

  const nodeHostname = nodes.data?.find((n) => n.id === node)?.hostname ?? node;

  const attach = useMutation({
    mutationFn: () => call(api.attachServiceVolume(serviceID, { node, name, target: target.trim(), read_only: readOnly || undefined })),
    onSuccess: async () => {
      const svcName = services.data?.find((s) => s.id === serviceID)?.name ?? serviceID;
      setAttached(svcName);
      setTarget("");
      await Promise.all([
        client.invalidateQueries({ queryKey: ["volumes", node] }),
        client.invalidateQueries({ queryKey: ["volumes", node, name] }),
      ]);
    },
  });

  const remove = useMutation({
    mutationFn: () => call(api.removeNodeVolume(node, name)),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["volumes", node] });
      navigate("/volumes", { replace: true });
    },
  });

  return (
    <VolumeDetailView
      volume={detail.data}
      nodeHostname={nodeHostname}
      services={services.data ?? []}
      admin={admin}
      loading={detail.isPending}
      error={detail.isError ? detail.error.message : undefined}
      serviceID={serviceID}
      onServiceID={(v) => {
        setServiceID(v);
        setAttached(undefined);
      }}
      target={target}
      onTarget={setTarget}
      readOnly={readOnly}
      onReadOnly={setReadOnly}
      attaching={attach.isPending}
      attachError={attach.isError ? attach.error.message : undefined}
      attached={attached}
      onAttach={(e) => {
        e.preventDefault();
        setAttached(undefined);
        attach.mutate();
      }}
      deleting={remove.isPending}
      deleteError={remove.isError ? remove.error.message : undefined}
      onDelete={async () => {
        if (await confirm({
          title: `Delete ${name}?`,
          message: `Removes local volume ${name} from ${nodeHostname}. Stored data is deleted.`,
          action: "Delete",
          danger: true,
        })) {
          remove.mutate();
        }
      }}
    />
  );
}
