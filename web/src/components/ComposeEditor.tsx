import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { api, call } from "../api/client";
import { Button } from "./Button";
import { TextField } from "./TextField";
import { RegistrySelect } from "./RegistrySelect";

type Props = {
  name: string;
  initial: string;
  initialEnvironment?: string;
  initialRegistry?: string;
  placeholder?: string;
  onDeployed?: (name: string) => void;
};

// ComposeEditor deploys a compose file for a stack. Deploying prunes, so what
// is not in the file leaves the cluster; that is the one thing worth saying.
export function ComposeEditor({ name, initial, initialEnvironment = "", initialRegistry = "", placeholder, onDeployed }: Props) {
  const [compose, setCompose] = useState(initial);
  const [environment, setEnvironment] = useState(initialEnvironment);
  const [registryHost, setRegistryHost] = useState(initialRegistry);
  const queryClient = useQueryClient();

  const deploy = useMutation({
    mutationFn: () => call(api.deployStack(name, { compose, environment, registry_host: registryHost })),
    onSuccess: () => {
      for (const key of ["stacks", "stack", "services", "deploys"]) {
        queryClient.invalidateQueries({ queryKey: [key] });
      }
      onDeployed?.(name);
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    deploy.mutate();
  };

  return (
    <form onSubmit={submit} className="compose">
      <TextField
        label="Compose file"
        type="textarea"
        value={compose}
        onChange={setCompose}
        spellCheck={false}
        placeholder={placeholder}
        rows={18}
        mono
      />
      <TextField
        label="Environment variables"
        supportingText="Optional. One NAME=value per line."
        name="environment"
        type="textarea"
        value={environment}
        onChange={setEnvironment}
        spellCheck={false}
        rows={4}
        mono
      />
      <RegistrySelect value={registryHost} onChange={setRegistryHost} />
      <p className="muted">Services that are not in the file are removed.</p>
      {deploy.isError && <p className="error" role="alert">{deploy.error.message}</p>}
      {deploy.isSuccess && <p className="muted" role="status">Deployed.</p>}
      {deploy.isSuccess && deploy.data.output && <pre className="output">{deploy.data.output}</pre>}
      <Button disabled={deploy.isPending || !name || !compose.trim()}>
        {deploy.isPending ? "Deploying…" : "Deploy"}
      </Button>
    </form>
  );
}
