import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { api, call } from "../api/client";
import type { NetworkEndpoint } from "../api/generated";
import { atLeast, useMe } from "../auth";
import { Button, LinkButton } from "../components/Button";
import { Chip } from "../components/Chip";
import { useDialogs } from "../components/Dialogs";
import { Select } from "../components/Select";
import { Switch } from "../components/Switch";
import { Table } from "../components/Table";
import { useTableSort } from "../components/TableSort";
import { TextField } from "../components/TextField";

export const networksKey = ["networks"];

export function useNetworks() {
  return useQuery({ queryKey: networksKey, queryFn: () => call(api.listNetworks()) });
}

export function Networks() {
  const networks = useNetworks();
  const admin = atLeast(useMe().data?.role, "admin");
  const [search, setSearch] = useState("");
  const [driver, setDriver] = useState("");
  const [node, setNode] = useState("");
  const nodes = useNetworkNodes();
  const inventory = [
    ...(networks.data ?? []).map((network) => ({ ...network, nodeID: "", location: "Swarm" })),
    ...(nodes.data ?? []).flatMap((node) => node.networks.map((network) => ({ ...network, nodeID: node.id, location: node.name }))),
  ];
  const sort = useTableSort<(typeof inventory)[number]>({
    name: (n) => n.name,
    driver: (n) => n.driver,
    location: (n) => n.location,
    subnets: (n) => n.subnets[0]?.subnet,
    services: (n) => n.services.length,
  });
  const rows = sort.rows(inventory.filter((n) =>
    (!node || node === (n.nodeID || "swarm")) && (!driver || n.driver === driver) &&
    [n.name, n.stack, n.location].some((value) => value?.toLowerCase().includes(search.trim().toLowerCase()))));
  return (
    <>
      <header className="toolbar network-heading">
        <h1>Networks</h1>
        {admin && <LinkButton to="/networks/new">Create network</LinkButton>}
      </header>
      <div className="toolbar">
        <TextField label="Search networks" value={search} onChange={setSearch} />
        <Select label="Driver" value={driver} onChange={setDriver}>
          <option value="">All drivers</option>
          {[...new Set(inventory.map((n) => n.driver))].sort().map((d) => <option key={d}>{d}</option>)}
        </Select>
        <Select label="Location" value={node} onChange={setNode}>
          <option value="">All locations</option>
          <option value="swarm">Swarm</option>
          {nodes.data?.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
        </Select>
      </div>
      {(networks.isPending || nodes.isPending) && <p>Loading…</p>}
      {nodes.isError && <p className="error" role="alert">Could not load the node network inventory. {nodes.error.message}</p>}
      {nodes.data?.some((n) => !n.available) && <p role="status">Local networks unavailable on {nodes.data.filter((n) => !n.available).map((n) => n.name).join(", ")}.</p>}
      {networks.isError && <p className="error" role="alert">{networks.error.message}</p>}
      {(networks.data || nodes.data) && (rows.length ? (
        <Table className="network-inventory" scrollLabel="Networks">
          <thead><tr>
            {sort.header("name", "Network")}
            {sort.header("driver", "Driver")}
            {sort.header("location", "Location")}
            {sort.header("subnets", "Subnets")}
            {sort.header("services", "Services")}
          </tr></thead>
          <tbody>{rows.map((n) => (
            <tr key={n.nodeID + n.id}>
              <th scope="row"><Link to={networkPath(n.id, n.nodeID)}>{n.name}</Link></th>
              <td>{n.driver}</td>
              <td>{n.location}{n.protected && <> <Chip>System</Chip></>}{n.stack && !n.protected && <> <Chip>{n.stack}</Chip></>}</td>
              <td className="mono">{n.subnets.map((s) => s.subnet).join(", ") || "—"}</td>
              <td>{n.services.length}</td>
            </tr>
          ))}</tbody>
        </Table>
      ) : <p>No networks match these filters.</p>)}
    </>
  );
}

export const networkNodesKey = ["network-nodes"];

function useNetworkNodes() {
  return useQuery({ queryKey: networkNodesKey, queryFn: () => call(api.listNodeNetworks()) });
}

function networkPath(id: string, nodeID = "") {
  return nodeID ? `/networks/node/${nodeID}/${id}` : `/networks/${id}`;
}

export function NetworkDetail() {
  const { id = "", node = "" } = useParams();
  const nodes = useNetworkNodes();
  const nodeName = nodes.data?.find((n) => n.id === node)?.name ?? node;
  const admin = atLeast(useMe().data?.role, "admin");
  const client = useQueryClient();
  const navigate = useNavigate();
  const { confirm } = useDialogs();
  const detail = useQuery({ queryKey: [...networksKey, node, id], queryFn: () => call(node ? api.getNodeNetwork(node, id) : api.getNetwork(id)) });
  const remove = useMutation({
    mutationFn: () => call(node ? api.removeNodeNetwork(node, id) : api.removeNetwork(id)),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: node ? networkNodesKey : networksKey });
      navigate("/networks");
    },
  });
  const n = detail.data;
  const endpoints: NetworkEndpoint[] = n?.endpoints ?? n?.containers.map((name) => ({ id: name, name })) ?? [];
  return (
    <>
      <p><Link to="/networks">Networks</Link></p>
      {detail.isPending && <p>Loading…</p>}
      {detail.isError && <p className="error" role="alert">{detail.error.message}</p>}
      {n && <>
        <h1>{n.name}</h1>
        <p>{n.driver} · {nodeName || "Swarm"}{n.protected && <> <Chip>System</Chip></>}</p>
        {n.stack && <p>Stack <Link to={`/stacks/${encodeURIComponent(n.stack)}`}>{n.stack}</Link></p>}
        <dl className="facts">
          <dt>Network ID</dt><dd className="mono">{n.id}</dd>
          <dt>Subnets</dt><dd>{n.subnets.map((s) => <div key={s.subnet}>{s.subnet}{s.gateway && ` · Gateway ${s.gateway}`}</div>)}{!n.subnets.length && "—"}</dd>
          <dt>Internal</dt><dd>{n.internal ? "Yes" : "No"}</dd>
          <dt>Attachable</dt><dd>{n.attachable ? "Yes" : "No"}</dd>
          <dt>Encrypted</dt><dd>{n.encrypted ? "Yes" : "No"}</dd>
        </dl>
        <h2>Services</h2>
        {n.services.length > 0 ? (
          <Table scrollLabel="Services">
            <thead><tr><th scope="col">Service</th><th scope="col">Stack</th></tr></thead>
            <tbody>
              {n.services.map((service) => (
                <tr key={service.id}>
                  <th scope="row"><Link to={`/services/${service.id}/networks`}>{service.name}</Link></th>
                  <td>{service.stack ? <Link to={`/stacks/${encodeURIComponent(service.stack)}`}>{service.stack}</Link> : "—"}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : <p>No service attachments.</p>}
        {endpoints.length > 0 && <>
          <h2>Containers</h2>
          <Table scrollLabel="Containers">
            <thead><tr>
              <th scope="col">Container</th>
              <th scope="col">IPv4</th>
              <th scope="col">IPv6</th>
              <th scope="col">MAC address</th>
            </tr></thead>
            <tbody>
              {endpoints.map((endpoint) => (
                <tr key={endpoint.id}>
                  <th scope="row">{endpoint.name}</th>
                  <td className="mono">{endpoint.ipv4_address || "—"}</td>
                  <td className="mono">{endpoint.ipv6_address || "—"}</td>
                  <td className="mono">{endpoint.mac_address || "—"}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        </>}
        {admin && !n.protected && !n.stack && <>
          {(n.services.length > 0 || n.containers.length > 0) && <p>Disconnect the attached workloads before removing this network.</p>}
          <Button variant="danger" disabled={remove.isPending || n.services.length > 0 || n.containers.length > 0} onClick={async () => {
            if (await confirm({ title: `Remove ${n.name}?`, message: node ? `This removes the network from ${nodeName}.` : "This removes the network from Swarm.", action: "Remove network", danger: true })) remove.mutate();
          }}>Remove network</Button>
        </>}
        {remove.isError && <p className="error" role="alert">{remove.error.message}</p>}
      </>}
    </>
  );
}

export function NetworkNew() {
  const admin = atLeast(useMe().data?.role, "admin");
  const navigate = useNavigate();
  const client = useQueryClient();
  const nodes = useNetworkNodes();
  const [node, setNode] = useState("");
  const [driver, setDriver] = useState<"bridge" | "macvlan" | "ipvlan">("bridge");
  const [parent, setParent] = useState("");
  const [name, setName] = useState("");
  const [subnet, setSubnet] = useState("");
  const [gateway, setGateway] = useState("");
  const [internal, setInternal] = useState(false);
  const [attachable, setAttachable] = useState(false);
  const [encrypted, setEncrypted] = useState(false);
  const save = useMutation({
    mutationFn: () => {
      const settings = { name: name.trim(), subnet: subnet.trim() || undefined, gateway: gateway.trim() || undefined, internal };
      return call(node ? api.createNodeNetwork(node, { ...settings, driver, parent: driver === "bridge" ? undefined : parent.trim() || undefined })
        : api.createNetwork({ ...settings, attachable, encrypted }));
    },
    onSuccess: async (created) => {
      await client.invalidateQueries({ queryKey: node ? networkNodesKey : networksKey });
      navigate(networkPath(created.id, node));
    },
  });
  if (!admin) return <p>Only admins can create networks.</p>;
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate();
  }
  return (
    <>
      <p><Link to="/networks">Networks</Link></p>
      <h1>Create network</h1>
      <form className="form" onSubmit={submit}>
        <Select label="Location" value={node} onChange={setNode}>
          <option value="">Swarm (overlay)</option>
          {nodes.data?.filter((n) => n.available).map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
        </Select>
        {node && <Select label="Driver" value={driver} onChange={(value) => {
          if (value === "bridge" || value === "macvlan" || value === "ipvlan") setDriver(value);
        }}>
          <option value="bridge">bridge</option>
          <option value="macvlan">macvlan</option>
          <option value="ipvlan">ipvlan</option>
        </Select>}
        {node && driver !== "bridge" && <TextField label="Parent interface" value={parent} onChange={setParent} placeholder="eth0" />}
        <TextField label="Name" value={name} onChange={setName} required autoFocus />
        <TextField label="Subnet" value={subnet} onChange={setSubnet} placeholder="10.42.0.0/24" />
        <TextField label="Gateway" value={gateway} onChange={setGateway} placeholder="10.42.0.1" />
        <Switch label="Internal network" checked={internal} onChange={setInternal} />
        {!node && <>
          <Switch label="Allow standalone containers" checked={attachable} onChange={setAttachable} />
          <Switch label="Encrypt overlay traffic" checked={encrypted} onChange={setEncrypted} />
        </>}
        {save.isError && <p className="error" role="alert">{save.error.message}</p>}
        <Button disabled={save.isPending || !name.trim() || (!!node && !nodes.data?.some((n) => n.id === node && n.available))}>Create network</Button>
      </form>
    </>
  );
}
