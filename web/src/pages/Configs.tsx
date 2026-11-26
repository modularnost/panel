import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router";
import { ApiError, api, call } from "../api/client";
import type { ConfigAttachment, Service } from "../api/generated";
import { atLeast, useMe } from "../auth";
import { ActivityTime } from "../components/ActivityTime";
import { Button } from "../components/Button";
import { Dialog } from "../components/Dialog";
import { useDialogs } from "../components/Dialogs";
import { Select } from "../components/Select";
import { Table } from "../components/Table";
import { TextField } from "../components/TextField";
import { configTarget, readConfigData } from "../configData";
import { isStandaloneService, resourceJSONBytes, resourceMaxBytes, resourceNamePattern } from "../swarmResources";

const configQuery = { queryKey: ["configs"], queryFn: () => call(api.listConfigs()) } as const;
const serviceQuery = { queryKey: ["services"], queryFn: () => call(api.listServices()) } as const;
const configPath = (id: string) => "/configs/" + encodeURIComponent(id);
const attachmentKey = (ref: ConfigAttachment) => JSON.stringify([ref.service, ref.target]);

export function Configs() {
  const { id } = useParams();
  if (!atLeast(useMe().data?.role, "admin")) return <Navigate to="/" replace />;
  return id ? <ConfigDetails key={id} id={id} /> : <ConfigList />;
}

function ConfigList() {
  const configs = useQuery(configQuery);
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const rows = [...(configs.data ?? [])].sort((a, b) => a.name.localeCompare(b.name));

  return (
    <>
      <header className="toolbar network-heading">
        <h1>Configs</h1>
        <Button onClick={() => setCreating(true)}>Create config</Button>
      </header>
      {configs.isPending && <p role="status">Loading configs…</p>}
      {configs.isError && <p className="error" role="alert">{configs.error.message}</p>}
      {rows.length > 0 ? (
        <Table scrollLabel="Configs">
          <thead><tr><th scope="col">Config</th><th scope="col">Services</th><th scope="col">Created</th></tr></thead>
          <tbody>{rows.map((config) => (
            <tr key={config.id}>
              <th scope="row"><Link to={configPath(config.id)}>{config.name}</Link></th>
              <td>{config.attachments.length
                ? [...new Set(config.attachments.map((ref) => ref.service))].sort().join(", ")
                : <span className="muted">No active attachments</span>}</td>
              <td><ActivityTime value={config.created_at} /></td>
            </tr>
          ))}</tbody>
        </Table>
      ) : configs.isSuccess && <p className="muted">No configs yet.</p>}
      {creating && <CreateConfig onClose={() => setCreating(false)} onCreated={(config) => navigate(configPath(config.id))} />}
    </>
  );
}

type CreatedConfig = { id: string; name: string };

function CreateConfig({ source, onClose, onCreated }: {
  source?: { name: string; text?: string };
  onClose: () => void;
  onCreated: (config: CreatedConfig) => void;
}) {
  const client = useQueryClient();
  const form = useId();
  const [name, setName] = useState("");
  const [text, setText] = useState(source?.text ?? "");
  const [file, setFile] = useState<File>();
  const bytes = file ? undefined : new TextEncoder().encode(text);
  const size = file?.size ?? bytes?.length ?? 0;
  const create = useMutation({
    mutationFn: async () => {
      const data = file ? new Uint8Array(await file.arrayBuffer()) : new TextEncoder().encode(text);
      const { id } = await call(api.createConfig({ name, data: resourceJSONBytes(data) }));
      return { id, name };
    },
    onSuccess: async (config) => {
      await client.invalidateQueries({ queryKey: configQuery.queryKey });
      onCreated(config);
      onClose();
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (name && size && size <= resourceMaxBytes && !create.isPending) create.mutate();
  }

  return (
    <Dialog headline={source ? `Create a replacement for ${source.name}` : "Create config"} onDismiss={onClose}
      actions={<>
        <Button type="button" variant="text" onClick={onClose}>Cancel</Button>
        <Button form={form} disabled={!name || !size || size > resourceMaxBytes || create.isPending}>
          {create.isPending ? "Creating…" : "Create config"}
        </Button>
      </>}>
      <form id={form} className="form" onSubmit={submit}>
        <TextField label="Name" name="name" value={name} onChange={setName} pattern={resourceNamePattern}
          placeholder={source ? "app-config-v2" : "app-config-v1"}
          supportingText={source ? "Use a new name for this version." : undefined} required autoFocus disabled={create.isPending} />
        <TextField label="Contents" name="contents" type="textarea" rows={8} value={text} onChange={setText}
          spellCheck={false} mono disabled={!!file || create.isPending} />
        <label className="field" htmlFor={form + "-file"}>
          Or upload a file
          <input id={form + "-file"} name="file" type="file" disabled={create.isPending}
            onChange={(event) => setFile(event.currentTarget.files?.[0])} />
        </label>
        <p className="muted">{file ? "The selected file will be used. " : ""}Maximum size is 500 KiB.</p>
        {size > resourceMaxBytes && <p className="error" role="alert">Choose a smaller file or shorten the contents.</p>}
        {create.isError && <p className="error" role="alert">{create.error.message}</p>}
      </form>
    </Dialog>
  );
}

function ConfigDetails({ id }: { id: string }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const { confirm } = useDialogs();
  const detail = useQuery({
    queryKey: [...configQuery.queryKey, id],
    queryFn: async () => {
      const config = await call(api.inspectConfig(id));
      return { ...config, contents: readConfigData(config.data) };
    },
  });
  const inventory = useQuery(configQuery);
  const services = useQuery(serviceQuery);
  const knownServices = new Map(services.data?.map((service) => [service.name, service]));
  const standalone = services.data?.filter(isStandaloneService) ?? [];
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedConfig>();
  const [attachOpen, setAttachOpen] = useState(false);
  const [replacement, setReplacement] = useState("");
  const [selection, setSelection] = useState<Set<string>>();
  const [status, setStatus] = useState("");
  const config = detail.data;
  const editable = config?.attachments.filter((ref) => configTarget(ref.target) && isStandaloneService(knownServices.get(ref.service))) ?? [];
  const replacements = (inventory.data ?? []).filter((row) => row.id !== id).sort((a, b) => a.name.localeCompare(b.name));
  const selected = editable.filter((ref) => selection?.has(attachmentKey(ref)));
  const change = useMutation({
    mutationFn: (action: () => Promise<string>) => action(),
    onMutate: () => setStatus(""),
    onSuccess: setStatus,
    onSettled: () => client.invalidateQueries({ queryKey: configQuery.queryKey }),
  });

  function openReplacement(next = "") {
    setReplacement(next);
    setSelection(new Set(editable.map(attachmentKey)));
  }

  async function setReferences(refs: ConfigAttachment[], resource?: string) {
    const [rows, currentServices] = await Promise.all([configQuery.queryFn(), serviceQuery.queryFn()]);
    const current = rows.find((row) => row.id === id);
    if (resource && !rows.some((row) => row.id === resource)) throw new Error("The replacement is no longer available.");
    const byName = new Map(currentServices.map((service) => [service.name, service]));
    const failed: string[] = [];
    for (const ref of refs) {
      const service = byName.get(ref.service);
      try {
        if (!service || service.stack || !current?.attachments.some((candidate) => attachmentKey(candidate) === attachmentKey(ref))) {
          throw new Error("Attachment changed.");
        }
        await call(api.setServiceConfig(service.id, { target: ref.target, resource }));
      } catch {
        failed.push(`${ref.service} (${ref.target})`);
      }
    }
    const updated = refs.length - failed.length;
    if (failed.length) throw new Error(`Updated ${updated} of ${refs.length} attachments. Could not update ${failed.join(", ")}.`);
    return resource ? `Replaced ${updated} ${updated === 1 ? "attachment" : "attachments"}.` : "Config detached.";
  }

  function download() {
    if (!config) return;
    const url = URL.createObjectURL(new Blob([config.contents.bytes], { type: "application/octet-stream" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = config.name;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }

  return (
    <>
      <p><Link to="/configs">Configs</Link></p>
      {detail.isPending && <p role="status">Loading config…</p>}
      {detail.isError && <p className="error" role="alert">
        {detail.error instanceof ApiError && detail.error.status === 404 ? "Config not found." : detail.error.message}
      </p>}
      {config && <>
        <header className="toolbar network-heading">
          <h1>{config.name}</h1>
          <Button onClick={() => setCreating(true)} disabled={change.isPending}>Create replacement</Button>
        </header>
        {created && <p role="status">Created <Link to={configPath(created.id)}>{created.name}</Link>.</p>}
        {status && <p role="status">{status}</p>}
        {change.isError && <p className="error" role="alert">{change.error.message}</p>}

        <header className="toolbar network-heading">
          <h2>Where it’s used</h2>
          <div className="actions">
            {standalone.length > 0 && <Button variant="outlined" disabled={change.isPending}
              onClick={() => setAttachOpen(true)}>Attach manually</Button>}
            {editable.length > 0 && <Button variant="tonal" disabled={!replacements.length || change.isPending}
              onClick={() => openReplacement()}>Replace on services</Button>}
          </div>
        </header>
        <p className="muted">For services in a stack, edit the config reference in Compose and redeploy.</p>
        {services.isError && <p className="error" role="alert">{services.error.message}</p>}
        {inventory.isError && <p className="error" role="alert">{inventory.error.message}</p>}
        {config.attachments.length > 0 ? (
          <Table scrollLabel="Config attachments">
            <thead><tr><th scope="col">Service</th><th scope="col">Stack</th><th scope="col">Container path</th><th scope="col">Actions</th></tr></thead>
            <tbody>{config.attachments.map((ref) => {
              const service = knownServices.get(ref.service);
              return <tr key={attachmentKey(ref)}>
                <th scope="row">{service ? <Link to={`/services/${service.id}`}>{ref.service}</Link> : ref.service}</th>
                <td>{service?.stack || (service ? "Standalone" : "—")}</td>
                <td><code>{ref.target || "—"}</code></td>
                <td>{service?.stack
                  ? <Link to={`/stacks/${encodeURIComponent(service.stack)}`} aria-label={`Open stack ${service.stack}`}>Open stack</Link>
                  : service && configTarget(ref.target)
                    ? <Button variant="text" disabled={change.isPending} aria-label={`Detach ${ref.target} from ${ref.service}`} onClick={async () => {
                      if (await confirm({
                        title: `Detach from ${ref.service}?`,
                        message: `Removes ${ref.target} from the service. Swarm may restart its tasks.`,
                        action: "Detach",
                        danger: true,
                      })) change.mutate(() => setReferences([ref]));
                    }}>Detach</Button>
                    : "—"}</td>
              </tr>;
            })}</tbody>
          </Table>
        ) : <p className="muted">No active attachments.</p>}

        <header className="toolbar network-heading">
          <h2>Contents</h2>
          <Button variant="outlined" onClick={download}>Download config</Button>
        </header>
        {config.contents.text === undefined
          ? <p className="muted">Binary config ({config.contents.bytes.length} bytes).</p>
          : <pre className="config-content" role="region" aria-label="Config contents" tabIndex={0}>{config.contents.text}</pre>}
        <h2>Details</h2>
        <dl className="facts">
          <dt>Created</dt><dd><ActivityTime value={config.created_at} /></dd>
          <dt>ID</dt><dd className="mono">{config.id}</dd>
          {Object.keys(config.labels ?? {}).length > 0 && <>
            <dt>Labels</dt><dd>{Object.entries(config.labels ?? {}).map(([key, value]) => <div key={key}>{key}={value}</div>)}</dd>
          </>}
        </dl>
        <h2>Delete config</h2>
        <p className="muted">{config.attachments.length
          ? "Remove or replace every active attachment before deleting this config."
          : "Check saved Compose files for references before deleting this config."}</p>
        <Button variant="danger" disabled={config.attachments.length > 0 || change.isPending} onClick={async () => {
          if (await confirm({
            title: `Delete ${config.name}?`,
            message: "The config will be permanently deleted. Stacks that reference it may fail to redeploy.",
            action: "Delete",
            danger: true,
          })) change.mutate(async () => {
            await call(api.removeConfig(id));
            navigate("/configs", { replace: true });
            return "Config deleted.";
          });
        }}>Delete config</Button>

        {creating && <CreateConfig source={{ name: config.name, text: config.contents.text }}
          onClose={() => setCreating(false)} onCreated={(next) => {
            setCreated(next);
            if (editable.length) openReplacement(next.id);
          }} />}
        {attachOpen && <AttachConfig id={id} services={standalone} onClose={() => setAttachOpen(false)} onAttached={setStatus} />}
        {selection && <Dialog headline={`Replace ${config.name} on services`} onDismiss={() => setSelection(undefined)} actions={<>
          <Button type="button" variant="text" onClick={() => setSelection(undefined)}>Cancel</Button>
          <Button type="button" disabled={!replacements.some((row) => row.id === replacement) || !selected.length || change.isPending}
            onClick={() => {
              setSelection(undefined);
              change.mutate(() => setReferences(selected, replacement));
            }}>Replace selected</Button>
        </>}>
          <Select label="Replacement config" value={replacement} onChange={setReplacement}>
            <option value="">Choose a config…</option>
            {replacements.map((row) => <option key={row.id} value={row.id}>{row.name}</option>)}
          </Select>
          <p>Swarm may restart the selected services. Their container paths stay the same.</p>
          <fieldset>
            <legend>Standalone service attachments to update</legend>
            {editable.map((ref) => {
              const key = attachmentKey(ref);
              return <div key={key}><label>
                <input type="checkbox" checked={selection.has(key)} onChange={(event) => {
                  const next = new Set(selection);
                  if (event.currentTarget.checked) next.add(key);
                  else next.delete(key);
                  setSelection(next);
                }} /> {ref.service} · <code>{ref.target}</code>
              </label></div>;
            })}
          </fieldset>
        </Dialog>}
      </>}
    </>
  );
}

function AttachConfig({ id, services, onClose, onAttached }: {
  id: string;
  services: Service[];
  onClose: () => void;
  onAttached: (status: string) => void;
}) {
  const client = useQueryClient();
  const form = useId();
  const [serviceID, setServiceID] = useState("");
  const [target, setTarget] = useState("");
  const attach = useMutation({
    mutationFn: async () => {
      const [rows, currentServices] = await Promise.all([configQuery.queryFn(), serviceQuery.queryFn()]);
      const service = currentServices.find((row) => row.id === serviceID);
      if (!service || service.stack) throw new Error("Choose a service outside a stack.");
      if (rows.some((row) => row.attachments.some((ref) => ref.service === service.name && ref.target === target))) {
        throw new Error("This service already has a config at that path. Use replacement to change it.");
      }
      await call(api.setServiceConfig(service.id, { target, resource: id }));
      return `Attached to ${service.name}.`;
    },
    onSuccess: async (status) => {
      await client.invalidateQueries({ queryKey: configQuery.queryKey });
      onAttached(status);
      onClose();
    },
  });
  const canAttach = services.some((service) => service.id === serviceID) && configTarget(target) && !attach.isPending;

  return (
    <Dialog headline="Attach config" onDismiss={onClose} actions={<>
      <Button type="button" variant="text" onClick={onClose}>Cancel</Button>
      <Button form={form} disabled={!canAttach}>{attach.isPending ? "Attaching…" : "Attach config"}</Button>
    </>}>
      <form id={form} className="form" onSubmit={(event) => {
        event.preventDefault();
        if (canAttach) attach.mutate();
      }}>
        <Select label="Standalone service" value={serviceID} onChange={setServiceID} required disabled={attach.isPending}>
          <option value="">Choose a service…</option>
          {services.map((service) => <option key={service.id} value={service.id}>{service.name}</option>)}
        </Select>
        <TextField label="Container path" name="target" value={target} onChange={setTarget} placeholder="/etc/app/app.conf"
          supportingText="Swarm may restart service tasks."
          error={target && !configTarget(target) ? "Enter an absolute file path like /etc/app/app.conf." : undefined}
          required mono disabled={attach.isPending} />
        {attach.isError && <p className="error" role="alert">{attach.error.message}</p>}
      </form>
    </Dialog>
  );
}
