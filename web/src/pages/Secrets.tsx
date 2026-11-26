import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router";
import { ApiError, api, call } from "../api/client";
import type { Secret, SecretAttachment } from "../api/generated";
import { atLeast, useMe } from "../auth";
import { ActivityTime } from "../components/ActivityTime";
import { Button } from "../components/Button";
import { Dialog } from "../components/Dialog";
import { useDialogs } from "../components/Dialogs";
import { Select } from "../components/Select";
import { Table } from "../components/Table";
import { TextField } from "../components/TextField";
import { isStandaloneService, resourceJSONBytes, resourceMaxBytes, resourceNamePattern } from "../swarmResources";

const secretQuery = { queryKey: ["secrets"], queryFn: () => call(api.listSecrets()) } as const;
const serviceQuery = { queryKey: ["services"], queryFn: () => call(api.listServices()) } as const;
const secretPath = (id: string) => "/secrets/" + encodeURIComponent(id);
const attachmentKey = (ref: SecretAttachment) => JSON.stringify([ref.service, ref.target]);

function containerPath(target: string): string {
  if (!target) return "—";
  return target.startsWith("/") ? target : "/run/secrets/" + target;
}

export function secretTarget(target: string): string | undefined {
  const name = target.replace(/^\/run\/secrets\//, "");
  return new RegExp(`^${resourceNamePattern}$`).test(name) ? name : undefined;
}

export function Secrets() {
  return atLeast(useMe().data?.role, "admin") ? <SecretManager /> : <Navigate to="/" replace />;
}

function SecretManager() {
  const { id } = useParams();
  const navigate = useNavigate();
  const client = useQueryClient();
  const secrets = useQuery(secretQuery);
  const rows = secrets.data ?? [];
  const current = rows.find((secret) => secret.id === id);
  const [creating, setCreating] = useState(false);

  return (
    <>
      {id ? <p><Link to="/secrets">Secrets</Link></p> : (
        <header className="toolbar network-heading">
          <h1>Secrets</h1>
          <Button onClick={() => setCreating(true)}>Create secret</Button>
        </header>
      )}
      {secrets.isPending && <p role="status">Loading secrets…</p>}
      {secrets.isError && <p className="error" role="alert">{secrets.error.message}</p>}
      {id ? (
        current ? <SecretDetail key={current.id} secret={current} secrets={rows} /> :
          secrets.isSuccess && <p>Secret not found.</p>
      ) : secrets.isSuccess && (
        rows.length ? (
          <Table scrollLabel="Secrets">
            <thead><tr><th scope="col">Secret</th><th scope="col">Services</th><th scope="col">Created</th></tr></thead>
            <tbody>
              {[...rows].sort((a, b) => a.name.localeCompare(b.name)).map((secret) => (
                <tr key={secret.id}>
                  <th scope="row"><Link to={secretPath(secret.id)}>{secret.name}</Link></th>
                  <td>
                    {secret.attachments.length
                      ? [...new Set(secret.attachments.map((ref) => ref.service))].sort().join(", ")
                      : <span className="muted">No active attachments</span>}
                  </td>
                  <td><ActivityTime value={secret.created_at} /></td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : <p className="muted">No secrets yet.</p>
      )}
      {creating && <CreateSecret onClose={() => setCreating(false)} onCreated={async (secretID) => {
        setCreating(false);
        await client.invalidateQueries({ queryKey: secretQuery.queryKey });
        navigate(secretPath(secretID));
      }} />}
    </>
  );
}

function CreateSecret({ onClose, onCreated }: { onClose: () => void; onCreated: (id: string) => Promise<void> }) {
  const formID = useId();
  const [name, setName] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    const form = event.currentTarget;
    const text = form.elements.namedItem("value") as HTMLInputElement;
    const upload = form.elements.namedItem("file") as HTMLInputElement;
    const file = upload.files?.[0];
    const value = new TextEncoder().encode(text.value);
    text.value = "";
    upload.value = "";
    const size = file?.size ?? value.length;
    if ((value.length > 0 && file) || size === 0 || size > resourceMaxBytes) {
      setError("Enter a value or choose a file, up to 500 KiB.");
      return;
    }
    setPending(true);
    setError("");
    try {
      const bytes = file ? new Uint8Array(await file.arrayBuffer()) : value;
      const result = await call(api.createSecret({ name: name.trim(), data: resourceJSONBytes(bytes) }));
      await onCreated(result.id);
    } catch (error) {
      setError(error instanceof ApiError && error.status === 409
        ? "A secret with this name already exists."
        : "Could not create the secret. Enter its value again to retry.");
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog headline="Create secret" onDismiss={onClose} actions={<>
      <Button type="button" variant="text" onClick={onClose}>Cancel</Button>
      <Button form={formID} disabled={pending}>{pending ? "Creating…" : "Create secret"}</Button>
    </>}>
      <form id={formID} className="form" autoComplete="off" onSubmit={create}>
        <TextField label="Name" value={name} onChange={setName} pattern={resourceNamePattern} placeholder="api-key-v2" required autoFocus />
        <label className="field" htmlFor={formID + "-value"}>
          Value
          <input id={formID + "-value"} name="value" type="password" autoComplete="new-password" />
        </label>
        <label className="field" htmlFor={formID + "-file"}>
          Or upload a file
          <input id={formID + "-file"} name="file" type="file" />
        </label>
        <p className="muted">Up to 500 KiB. You can enter the value only when creating the secret.</p>
        {error && <p className="error" role="alert">{error}</p>}
      </form>
    </Dialog>
  );
}

function SecretDetail({ secret, secrets }: { secret: Secret; secrets: Secret[] }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const { confirm } = useDialogs();
  const services = useQuery(serviceQuery);
  const servicesByName = new Map(services.data?.map((svc) => [svc.name, svc]));
  const standalone = services.data?.filter(isStandaloneService) ?? [];
  const [attaching, setAttaching] = useState(false);
  const [attachError, setAttachError] = useState("");
  const attachForm = useId();
  const [service, setService] = useState("");
  const [target, setTarget] = useState("");
  const [rotation, setRotation] = useState<Set<string>>();
  const [replacement, setReplacement] = useState("");
  const [pending, setPending] = useState(false);
  const [status, setStatus] = useState<{ text: string; error?: boolean }>();
  const labels = Object.entries(secret.labels ?? {});
  const editable = secret.attachments.filter((ref) => isStandaloneService(servicesByName.get(ref.service)) && secretTarget(ref.target));
  const selected = editable.filter((ref) => rotation?.has(attachmentKey(ref)));
  const replacements = secrets.filter((candidate) => candidate.id !== secret.id).sort((a, b) => a.name.localeCompare(b.name));

  async function attach(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = secretTarget(target);
    if (pending || !name || !isStandaloneService(servicesByName.get(service))) return;
    setAttachError("");
    await change(async () => {
      const [fresh, currentServices] = await Promise.all([secretQuery.queryFn(), serviceQuery.queryFn()]);
      const current = currentServices.find((svc) => svc.name === service);
      if (!current || current.stack) throw new Error("Choose a service outside a stack.");
      if (fresh.some((candidate) => candidate.attachments.some((ref) => ref.service === service && secretTarget(ref.target) === name))) {
        throw new Error("This service already has a secret at that target. Use rotation to replace it.");
      }
      await call(api.setServiceSecret(current.id, { target: name, resource: secret.id }));
      setAttaching(false);
      return `Attached to ${service}.`;
    }, setAttachError);
  }

  async function change(run: () => Promise<string>, onError?: (message: string) => void) {
    setPending(true);
    setStatus(undefined);
    try {
      setStatus({ text: await run() });
    } catch (error) {
      const message = error instanceof Error ? error.message : "Could not update the secret.";
      if (onError) onError(message);
      else setStatus({ text: message, error: true });
    } finally {
      await client.invalidateQueries({ queryKey: secretQuery.queryKey });
      setPending(false);
    }
  }

  async function setReferences(refs: SecretAttachment[], resource?: string) {
    const [fresh, currentServices] = await Promise.all([secretQuery.queryFn(), serviceQuery.queryFn()]);
    const byName = new Map(currentServices.map((svc) => [svc.name, svc]));
    if (resource && !fresh.some((candidate) => candidate.id === resource)) {
      throw new Error("The replacement secret is no longer available.");
    }
    const current = fresh.find((candidate) => candidate.id === secret.id);
    const failed: string[] = [];
    for (const ref of refs) {
      const name = secretTarget(ref.target);
      const service = byName.get(ref.service);
      try {
        if (!name || !service || service.stack || !current?.attachments.some((attachment) => attachmentKey(attachment) === attachmentKey(ref))) {
          throw new Error("Attachment changed.");
        }
        await call(api.setServiceSecret(service.id, { target: name, resource }));
      } catch {
        failed.push(`${ref.service} (${ref.target})`);
      }
    }
    const changed = refs.length - failed.length;
    if (failed.length) throw new Error(`Updated ${changed} of ${refs.length} attachments. Could not update ${failed.join(", ")}.`);
    return resource ? `Rotated ${changed} ${changed === 1 ? "attachment" : "attachments"}.` : "Secret detached.";
  }

  return (
    <>
      <h1>{secret.name}</h1>
      <header className="toolbar network-heading">
        <h2>Where it’s used</h2>
        <div className="actions">
          {standalone.length > 0 && <Button variant="outlined" disabled={pending} onClick={() => {
            setService("");
            setTarget(secret.name);
            setAttachError("");
            setAttaching(true);
          }}>Attach manually</Button>}
          {editable.length > 0 && <Button variant="tonal" disabled={pending || !replacements.length} onClick={() => {
            setReplacement("");
            setRotation(new Set(editable.map(attachmentKey)));
          }}>Rotate secret</Button>}
        </div>
      </header>
      <p className="muted">For services in a stack, edit the secret reference in Compose and redeploy.</p>
      {services.isError && <p className="error" role="alert">{services.error.message}</p>}
      {status && <p className={status.error ? "error" : undefined} role={status.error ? "alert" : "status"}>{status.text}</p>}
      {editable.length > 0 && !replacements.length && <p className="muted"><Link to="/secrets">Create a replacement secret</Link> before rotating.</p>}
      {secret.attachments.length ? (
        <Table scrollLabel="Secret attachments">
          <thead><tr><th scope="col">Service</th><th scope="col">Stack</th><th scope="col">Container path</th><th scope="col">Actions</th></tr></thead>
          <tbody>{secret.attachments.map((ref) => {
            const svc = servicesByName.get(ref.service);
            const path = containerPath(ref.target);
            return <tr key={attachmentKey(ref)}>
              <th scope="row">{svc ? <Link to={`/services/${svc.id}`}>{ref.service}</Link> : ref.service}</th>
              <td>{svc?.stack || (svc ? "Standalone" : "—")}</td>
              <td className="mono">{path}</td>
              <td>{svc?.stack ? (
                <Link to={`/stacks/${encodeURIComponent(svc.stack)}`} aria-label={`Open stack ${svc.stack}`}>Open stack</Link>
              ) : svc && secretTarget(ref.target) ? <Button variant="text" disabled={pending}
                aria-label={`Detach ${path} from ${ref.service}`}
                onClick={async () => {
                  if (await confirm({
                    title: `Detach from ${ref.service}?`, action: "Detach", danger: true,
                    message: `Removes ${path} from the service. Swarm may restart its tasks.`,
                  })) void change(() => setReferences([ref]));
                }}>Detach</Button> : svc ? <span className="muted">Change in service configuration</span> : "—"}</td>
            </tr>;
          })}</tbody>
        </Table>
      ) : <p className="muted">No active attachments.</p>}

      <h2>Details</h2>
      <dl className="facts">
        <dt>Created</dt><dd><ActivityTime value={secret.created_at} /></dd>
        <dt>ID</dt><dd className="mono">{secret.id}</dd>
        {labels.length > 0 && <><dt>Labels</dt><dd>{labels.map(([key, value]) => <div key={key}>{key}={value}</div>)}</dd></>}
      </dl>

      <h2>Delete secret</h2>
      <p className="muted">{secret.attachments.length > 0
        ? "Remove or replace every active attachment before deleting this secret."
        : "Check saved Compose files for references before deleting this secret."}</p>
      <Button variant="danger" disabled={pending || secret.attachments.length > 0} onClick={async () => {
        if (await confirm({
          title: `Delete ${secret.name}?`,
          message: "The value will be permanently deleted. Stacks that reference this secret may fail to redeploy.",
          action: "Delete", danger: true,
        })) {
          void change(async () => {
            await call(api.removeSecret(secret.id));
            navigate("/secrets", { replace: true });
            return "Secret deleted.";
          });
        }
      }}>Delete secret</Button>

      {attaching && <Dialog headline={`Attach ${secret.name}`} onDismiss={() => setAttaching(false)} actions={<>
        <Button type="button" variant="text" onClick={() => setAttaching(false)}>Cancel</Button>
        <Button form={attachForm} disabled={pending || !isStandaloneService(servicesByName.get(service)) || !secretTarget(target)}>
          {pending ? "Attaching…" : "Attach secret"}
        </Button>
      </>}>
        <form id={attachForm} className="form" onSubmit={attach}>
          <Select label="Standalone service" value={service} onChange={setService} disabled={pending} required>
            <option value="">Choose a service…</option>
            {standalone.map((svc) => <option key={svc.id} value={svc.name}>{svc.name}</option>)}
          </Select>
          <TextField label="Target filename" value={target} onChange={setTarget} pattern={resourceNamePattern}
            supportingText="Mounted under /run/secrets. Swarm may restart service tasks." required disabled={pending} />
          {attachError && <p className="error" role="alert">{attachError}</p>}
        </form>
      </Dialog>}

      {rotation && <Dialog headline={`Rotate ${secret.name}`} onDismiss={() => setRotation(undefined)} actions={<>
        <Button type="button" variant="text" onClick={() => setRotation(undefined)}>Cancel</Button>
        <Button type="button" disabled={!replacements.some((candidate) => candidate.id === replacement) || !selected.length || pending} onClick={() => {
          setRotation(undefined);
          void change(() => setReferences(selected, replacement));
        }}>Rotate selected</Button>
      </>}>
        <Select label="Replacement secret" value={replacement} onChange={setReplacement}>
          <option value="">Choose a secret…</option>
          {replacements.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.name}</option>)}
        </Select>
        <p>Swarm may restart the selected services. Their target filenames stay the same.</p>
        <fieldset>
          <legend>Standalone service attachments to update</legend>
          {editable.map((ref) => {
            const key = attachmentKey(ref);
            return <div key={key}>
              <label><input type="checkbox" checked={rotation.has(key)} onChange={(event) => {
                const next = new Set(rotation);
                if (event.target.checked) next.add(key);
                else next.delete(key);
                setRotation(next);
              }} /> {ref.service} · <code>{containerPath(ref.target)}</code></label>
            </div>;
          })}
        </fieldset>
      </Dialog>}
    </>
  );
}
