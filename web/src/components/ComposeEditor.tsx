import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { api, call } from "../api/client";

type Props = {
  name: string;
  initial: string;
  placeholder?: string;
  onDeployed?: (name: string) => void;
};

// ComposeEditor deploys a compose file for a stack. Deploying prunes, so what
// is not in the file leaves the cluster; that is the one thing worth saying.
export function ComposeEditor({ name, initial, placeholder, onDeployed }: Props) {
  const [compose, setCompose] = useState(initial);
  const queryClient = useQueryClient();

  const deploy = useMutation({
    mutationFn: () => call(api.deployStack(name, { compose })),
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
      <textarea
        value={compose}
        onChange={(e) => setCompose(e.target.value)}
        spellCheck={false}
        placeholder={placeholder}
        rows={18}
        aria-label="Compose file"
      />
      <p className="muted">Services that are not in the file are removed.</p>
      {deploy.isError && <p className="error">{deploy.error.message}</p>}
      {deploy.isSuccess && deploy.data.output && <pre className="output">{deploy.data.output}</pre>}
      <button disabled={deploy.isPending || !name || !compose.trim()}>
        {deploy.isPending ? "Deploying…" : "Deploy"}
      </button>
    </form>
  );
}
