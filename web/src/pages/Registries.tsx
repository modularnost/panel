import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { api, call } from "../api/client";
import { Button } from "../components/Button";
import { useDialogs } from "../components/Dialogs";
import { Table } from "../components/Table";
import { TextField } from "../components/TextField";

export function Registries() {
  const heading = useId();
  const formHeading = useId();
  const [host, setHost] = useState("");
  const [username, setUsername] = useState("");
  const [token, setToken] = useState("");
  const [editing, setEditing] = useState(false);
  const client = useQueryClient();
  const { confirm } = useDialogs();
  const saved = useQuery({ queryKey: ["registries"], queryFn: () => call(api.listRegistries()) });
  const refresh = () => client.invalidateQueries({ queryKey: ["registries"] });
  const save = useMutation({
    mutationFn: () => call(api.saveRegistry(host.trim().toLowerCase(), { username: username.trim(), token })),
    onSuccess: () => {
      setToken("");
      setEditing(false);
      setHost("");
      setUsername("");
      return refresh();
    },
  });
  const remove = useMutation({ mutationFn: (host: string) => call(api.deleteRegistry(host)), onSuccess: refresh });

  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate();
  }

  return (
    <section className="settings-card" aria-labelledby={heading}>
      <h2 id={heading}>Registries</h2>
      {saved.isPending && <p>Loading…</p>}
      {saved.isError && <p className="error" role="alert">{saved.error.message}</p>}
      {saved.data && !saved.data.configured && (
        <p>Configure the registry encryption key to save pull credentials.</p>
      )}
      {!!saved.data?.registries.length && (
        <Table>
          <thead><tr><th>Registry</th><th>Username</th><th>Actions</th></tr></thead>
          <tbody>
            {saved.data.registries.map((entry) => (
              <tr key={entry.host}>
                <td>{entry.host}</td>
                <td>{entry.username}</td>
                <td>
                  {saved.data.configured && (
                    <Button variant="text" onClick={() => {
                      setHost(entry.host);
                      setUsername(entry.username);
                      setToken("");
                      setEditing(true);
                      save.reset();
                    }}>Replace token</Button>
                  )}
                  <Button variant="danger" disabled={remove.isPending} onClick={async () => {
                    if (await confirm({
                      title: `Remove credentials for ${entry.host}?`,
                      message: "Deploys that use this registry will need new credentials.",
                      action: "Remove",
                      danger: true,
                    })) remove.mutate(entry.host);
                  }}>Remove</Button>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {remove.isError && <p className="error" role="alert">{remove.error.message}</p>}
      {saved.data?.configured && (
        <form className="form" method="POST" onSubmit={submit} aria-labelledby={formHeading}>
          <h3 id={formHeading}>{editing ? "Replace registry token" : "Save registry credentials"}</h3>
          <TextField label="Registry hostname" name="registry_host" value={host} onChange={setHost} spellCheck={false}
            placeholder="ghcr.io" disabled={editing} required />
          <TextField label="Username" name="username" value={username} onChange={setUsername}
            autoComplete="username" spellCheck={false} required />
          <TextField label="Pull token" name="token" type="password" value={token} onChange={setToken}
            autoComplete="new-password" required />
          {save.isError && <p className="error" role="alert">{save.error.message}</p>}
          {save.isSuccess && <p role="status">Credentials saved.</p>}
          <div className="settings-actions">
            <Button variant="tonal" disabled={save.isPending}>Save credentials</Button>
            {editing && <Button variant="text" type="button" onClick={() => {
              setEditing(false);
              setHost("");
              setUsername("");
              setToken("");
              save.reset();
            }}>Cancel</Button>}
          </div>
        </form>
      )}
    </section>
  );
}
