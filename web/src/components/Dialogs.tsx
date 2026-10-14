import { createContext, useCallback, useContext, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { Button } from "./Button";
import { Dialog } from "./Dialog";
import { TextField } from "./TextField";

type Confirm = { title: string; message: string; action: string; danger?: boolean };
type Prompt = Confirm & { label: string; type?: "text" | "password"; minLength?: number };
type Request = (Confirm | Prompt) & { resolve: (value: string | null) => void };

type Dialogs = {
  confirm: (o: Confirm) => Promise<boolean>;
  prompt: (o: Prompt) => Promise<string | null>;
};

const Context = createContext<Dialogs | null>(null);

export function useDialogs() {
  const dialogs = useContext(Context);
  if (!dialogs) throw new Error("useDialogs needs DialogsProvider");
  return dialogs;
}

export function DialogsProvider({ children }: { children: ReactNode }) {
  const [request, setRequest] = useState<Request | null>(null);
  const [value, setValue] = useState("");

  const ask = useCallback(
    (o: Confirm | Prompt) =>
      new Promise<string | null>((resolve) => {
        setValue("");
        setRequest({ ...o, resolve });
      }),
    [],
  );
  const dialogs = useMemo<Dialogs>(
    () => ({ confirm: (o) => ask(o).then((v) => v !== null), prompt: ask }),
    [ask],
  );

  const finish = useCallback(
    (answer: string | null) => {
      request?.resolve(answer);
      setRequest(null);
    },
    [request],
  );
  const submit = (e: FormEvent) => {
    e.preventDefault();
    finish(value);
  };

  return (
    <Context.Provider value={dialogs}>
      {children}
      {request && (
        <Dialog
          headline={request.title}
          onDismiss={() => finish(null)}
          actions={
            <>
              <Button type="button" variant="text" onClick={() => finish(null)}>
                Cancel
              </Button>
              <Button form="dialog-form" variant={request.danger ? "danger" : "filled"}>
                {request.action}
              </Button>
            </>
          }
        >
          <form id="dialog-form" onSubmit={submit}>
            <p>{request.message}</p>
            {"label" in request && (
              <TextField
                label={request.label}
                type={request.type}
                value={value}
                onChange={setValue}
                minLength={request.minLength}
                autoComplete="new-password"
                autoFocus
                required
              />
            )}
          </form>
        </Dialog>
      )}
    </Context.Provider>
  );
}
