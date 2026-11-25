import { useMutation } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { api, call } from "../api/client";
import { Button } from "../components/Button";
import { TextField } from "../components/TextField";

type Field = "endpoint" | "build_network" | "pull_network" | "pull_entrypoint" | "worker" | "username" | "token";

export function BuilderSetup({ version, revision, docs, permissions, onInstalled }: {
  version: string;
  revision: string;
  docs?: string;
  permissions: string[];
  onInstalled: () => void;
}) {
  const secretFileID = useId();
  const registryTitle = useId();
  const installTitle = useId();
  const [inputs, setInputs] = useState<Record<Field, string>>({
    endpoint: "", build_network: "builder-build", pull_network: "builder-pull",
    pull_entrypoint: "", worker: "", username: "builder-pull", token: "",
  });
  const [secretFile, setSecretFile] = useState<File | null>(null);
  const { endpoint, build_network, pull_network, pull_entrypoint, worker, username, token } = inputs;
  const plan = {
    version, revision, endpoint: endpoint.trim(), build_network: build_network.trim(),
    pull_network: pull_network.trim(), pull_entrypoint: pull_entrypoint.trim() || undefined,
  };
    const provision = useMutation({
    mutationFn: async () => {
      let secrets: Record<string, string> | undefined;
      if (secretFile) {
        if (secretFile.size > 512 * 1024) throw new Error("Choose a Swarm secrets JSON file under 512 KB.");
        const data: unknown = await secretFile.text().then(JSON.parse).catch(() => null);
        if (!data || typeof data !== "object" || Array.isArray(data) || Object.values(data).some((value) => typeof value !== "string")) {
          throw new Error("The Swarm secrets JSON file must map secret names to their contents.");
        }
        secrets = data as Record<string, string>;
      }
      return call(api.provisionModuleRegistry("builder", { ...plan, secrets }));
    },
  });
  const install = useMutation({
    mutationFn: async () => call(api.installManagedBuilder("builder", {
      ...plan, worker: worker.trim(), username: username.trim(),
      token,
    })),
    onSuccess: () => {
      setInputs((current) => ({ ...current, token: "" }));
      onInstalled();
    },
  });
  const busy = provision.isPending || install.isPending;
  function field(name: Field, label: string, required = true) {
    return <TextField label={label} name={name} value={inputs[name]} required={required}
      disabled={busy} spellCheck={false} type={name === "token" ? "password" : "text"}
      autoComplete={name === "token" ? "current-password" : undefined}
      onChange={(value) => {
        if (provision.isSuccess && ["endpoint", "build_network", "pull_network", "pull_entrypoint"].includes(name)) provision.reset();
        setInputs((current) => ({ ...current, [name]: value }));
      }} />;
  }
  const submit = (e: FormEvent) => {
    e.preventDefault();
    install.mutate();
  };
  return (
    <section>
      <p>Install Builder {version} with {permissions.join(", ") || "no extra permissions"}.</p>
      <p><a href={docs ?? "https://github.com/modularnost/panel/blob/main/docs/catalog.md#install-builder-from-the-catalog"}>Builder installation prerequisites</a></p>
      <form className="form" method="post" action="/api/v1/module-catalog/builder/registry" aria-labelledby={registryTitle} onSubmit={(e) => {
        e.preventDefault();
        provision.mutate();
      }}>
        <h3 id={registryTitle}>Prepare the registry</h3>
        {field("endpoint", "Registry hostname and optional port")}
        {field("build_network", "Isolated build overlay")}
        {field("pull_network", "Private pull overlay")}
        {field("pull_entrypoint", "Traefik TCP entry point (optional)", false)}
        <label htmlFor={secretFileID}>Swarm secrets JSON file (optional)</label>
        <input id={secretFileID} name="secrets" type="file" accept=".json,application/json"
          disabled={busy} onChange={(event) => setSecretFile(event.target.files?.[0] ?? null)} />
        <Button disabled={busy}>{provision.isPending ? "Preparing…" : "Prepare registry"}</Button>
        {provision.isError && <p className="error" role="alert">{provision.error.message}</p>}
        {provision.isSuccess && <p role="status">Registry provisioned.</p>}
      </form>
      <form className="form" method="post" action="/api/v1/module-catalog/builder/builder" aria-labelledby={installTitle} onSubmit={submit}>
        <h3 id={installTitle}>Verify and install Builder</h3>
        {field("worker", "Swarm worker ID")}
        {field("username", "Registry username")}
        {field("token", "Registry pull token")}
        <Button disabled={busy}>{install.isPending ? "Installing Builder…" : "Verify and install"}</Button>
        {install.isError && <p className="error" role="alert">{install.error.message}</p>}
      </form>
    </section>
  );
}
