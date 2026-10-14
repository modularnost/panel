import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call } from "../api/client";
import { WebhookTable } from "../components/WebhookTable";
import { Select } from "../components/Select";

export function Webhooks() {
  const queryClient = useQueryClient();
  const [kind, setKind] = useState<"service" | "stack">("service");
  const [target, setTarget] = useState("");

  const hooks = useQuery({
    queryKey: ["webhooks"],
    queryFn: () => call(api.listWebhooks()),
  });
  const services = useQuery({ queryKey: ["services"], queryFn: () => call(api.listServices()) });
  const stacks = useQuery({ queryKey: ["stacks"], queryFn: () => call(api.listStacks()) });

  const reload = () => queryClient.invalidateQueries({ queryKey: ["webhooks"] });
  const create = useMutation({
    mutationFn: () =>
      call(
        api.createWebhook(
          kind === "service" ? { action: "force_update", service: target } : { action: "stack_redeploy", stack: target },
        ),
      ),
    onSuccess: () => {
      setTarget("");
      reload();
    },
  });
  const remove = useMutation({ mutationFn: (id: number) => call(api.deleteWebhook(id)), onSuccess: reload });

  const options = kind === "service" ? (services.data ?? []).map((s) => s.name) : (stacks.data ?? []).map((s) => s.name);

  if (hooks.isPending) return <p>Loading…</p>;
  if (hooks.isError) return <p className="error">{hooks.error.message}</p>;

  return (
    <>
      <h1>Webhooks</h1>
      <WebhookTable hooks={hooks.data} busy={remove.isPending} onDelete={(id) => remove.mutate(id)} />
      {remove.isError && <p className="error">{remove.error.message}</p>}

      <h2>New webhook</h2>
      <div className="toolbar">
        <label>
          For
          <Select
            value={kind}
            onChange={(v) => {
              setKind(v as "service" | "stack");
              setTarget("");
            }}
          >
            <option value="service">a service</option>
            <option value="stack">a stack</option>
          </Select>
        </label>
        <label>
          Name
          <Select value={target} onChange={(v) => setTarget(v)}>
            <option value="">Choose…</option>
            {options.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </Select>
        </label>
        <button disabled={!target || create.isPending} onClick={() => create.mutate()}>
          Create
        </button>
      </div>
      {create.isError && <p className="error">{create.error.message}</p>}
    </>
  );
}
