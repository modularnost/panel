import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, call } from "../api/client";
import { Button } from "../components/Button";
import { useDialogs } from "../components/Dialogs";
import { Select } from "../components/Select";
import { Switch } from "../components/Switch";
import { TextField } from "../components/TextField";
import { type Block, formDefaults, formFields, strings } from "./document";

const str = (v: unknown): string => (typeof v === "string" ? v : "");

// useAction sends what a button or a form carries to the module and asks the
// page again: the module may have changed what it shows.
function useAction(module: string, page: string, action: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (values: Record<string, string>) => call(api.runModuleAction(module, { page, action, values })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["ui", module, page] }),
  });
}

function Result({ run }: { run: ReturnType<typeof useAction> }) {
  if (run.isError) return <p className="error">{run.error.message}</p>;
  if (run.isSuccess) return <p className="muted">{run.data.message || "Done."}</p>;
  return null;
}

// askFirst is the panel's own dialog for a block that carries a question.
function useAskFirst(block: Block) {
  const { confirm } = useDialogs();
  const question = str(block.confirm);
  return async () =>
    !question || (await confirm({ title: question, message: "", action: str(block.label) || "Confirm", danger: block.tone === "danger" }));
}

export function ButtonBlock({ block, module, page }: { block: Block; module: string; page: string }) {
  const run = useAction(module, page, str(block.action));
  const askFirst = useAskFirst(block);
  return (
    <div>
      <Button
        variant={block.tone === "danger" ? "danger" : "tonal"}
        disabled={run.isPending}
        onClick={async () => (await askFirst()) && run.mutate(strings(block.values))}
      >
        {str(block.label)}
      </Button>
      <Result run={run} />
    </div>
  );
}

export function FormBlock({ block, module, page }: { block: Block; module: string; page: string }) {
  const fields = formFields(block.fields);
  const [values, setValues] = useState(() => formDefaults(fields));
  const run = useAction(module, page, str(block.action));
  const askFirst = useAskFirst(block);
  const set = (name: string, value: string) => setValues((prev) => ({ ...prev, [name]: value }));

  return (
    <form
      className="ui-form"
      onSubmit={async (e) => {
        e.preventDefault();
        if (await askFirst()) run.mutate(values);
      }}
    >
      {fields.map((f) =>
        f.type === "switch" ? (
          <Switch key={f.name} label={f.label} checked={values[f.name] === "true"} onChange={(on) => set(f.name, String(on))} />
        ) : f.type === "select" ? (
          <Select key={f.name} label={f.label} value={values[f.name]} onChange={(v) => set(f.name, v)}>
            {f.options.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </Select>
        ) : (
          <TextField key={f.name} label={f.label} value={values[f.name]} onChange={(v) => set(f.name, v)} />
        ),
      )}
      <Button disabled={run.isPending}>{str(block.submit) || "Send"}</Button>
      <Result run={run} />
    </form>
  );
}
