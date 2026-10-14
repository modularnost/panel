import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { api, call } from "../api/client";
import type { User } from "../api/generated";
import { Select } from "../components/Select";
import { Button } from "../components/Button";
import { Banner } from "../components/Banner";

type Role = "admin" | "operator" | "viewer";
const ROLES: Role[] = ["viewer", "operator", "admin"];

export function Users() {
  const queryClient = useQueryClient();
  const reload = (key: string) => queryClient.invalidateQueries({ queryKey: [key] });

  const users = useQuery({ queryKey: ["users"], queryFn: () => call(api.listUsers()) });
  const tokens = useQuery({ queryKey: ["tokens"], queryFn: () => call(api.listTokens()) });

  const setRole = useMutation({
    mutationFn: (v: { id: number; role: Role }) => call(api.updateUser(v.id, { role: v.role })),
    onSuccess: () => reload("users"),
  });
  const setPassword = useMutation({
    mutationFn: (v: { id: number; password: string }) => call(api.updateUser(v.id, { password: v.password })),
  });
  const removeUser = useMutation({
    mutationFn: (id: number) => call(api.deleteUser(id)),
    onSuccess: () => {
      reload("users");
      reload("tokens");
    },
  });
  const removeToken = useMutation({ mutationFn: (id: number) => call(api.deleteToken(id)), onSuccess: () => reload("tokens") });

  // The value of a new token is in the response to its creation and nowhere else.
  const [created, setCreated] = useState<{ name: string; token: string } | null>(null);

  const [newUser, setNewUser] = useState({ username: "", password: "", role: "viewer" as Role });
  const createUser = useMutation({
    mutationFn: () => call(api.createUser(newUser)),
    onSuccess: () => {
      setNewUser({ username: "", password: "", role: "viewer" });
      reload("users");
    },
  });

  const [newToken, setNewToken] = useState<{ user_id: number; name: string; role: Role | ""; days: string }>({
    user_id: 0,
    name: "",
    role: "",
    days: "",
  });
  const createToken = useMutation({
    mutationFn: () =>
      call(
        api.createToken({
          user_id: newToken.user_id,
          name: newToken.name.trim(),
          role: newToken.role || undefined,
          days: newToken.days ? Number(newToken.days) : undefined,
        }),
      ),
    onSuccess: (t) => {
      setCreated({ name: t.name, token: t.token });
      setNewToken({ user_id: 0, name: "", role: "", days: "" });
      reload("tokens");
    },
  });

  const changePassword = (u: User) => {
    const password = window.prompt(`New password for ${u.username} (8 characters or more). Their sessions end.`);
    if (password) setPassword.mutate({ id: u.id, password });
  };

  if (users.isPending || tokens.isPending) return <p>Loading…</p>;
  if (users.isError) return <p className="error">{users.error.message}</p>;
  if (tokens.isError) return <p className="error">{tokens.error.message}</p>;

  const submitUser = (e: FormEvent) => {
    e.preventDefault();
    createUser.mutate();
  };
  const submitToken = (e: FormEvent) => {
    e.preventDefault();
    createToken.mutate();
  };

  return (
    <>
      <h1>Users</h1>
      <table>
        <thead>
          <tr>
            <th>User</th>
            <th>Role</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {users.data.map((u) => (
            <tr key={u.id}>
              <td>{u.username}</td>
              <td>
                <Select
                  value={u.role}
                  disabled={setRole.isPending}
                  onChange={(v) => setRole.mutate({ id: u.id, role: v as Role })}
                  aria-label={`Role of ${u.username}`}
                >
                  {ROLES.map((r) => (
                    <option key={r} value={r}>
                      {r}
                    </option>
                  ))}
                </Select>
              </td>
              <td>
                <Button variant="text" onClick={() => changePassword(u)}>
                  Change password
                </Button>
                <Button
                  variant="text"
                  disabled={removeUser.isPending}
                  onClick={() =>
                    window.confirm(`Delete ${u.username}? Their sessions and tokens go with them.`) &&
                    removeUser.mutate(u.id)
                  }
                >
                  Delete
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {setRole.isError && <p className="error">{setRole.error.message}</p>}
      {setPassword.isError && <p className="error">{setPassword.error.message}</p>}
      {setPassword.isSuccess && <p className="muted">Password changed.</p>}
      {removeUser.isError && <p className="error">{removeUser.error.message}</p>}

      <h2>New user</h2>
      <form className="toolbar" onSubmit={submitUser}>
        <label>
          Username
          <input
            value={newUser.username}
            onChange={(e) => setNewUser({ ...newUser, username: e.target.value })}
            autoComplete="off"
            required
          />
        </label>
        <label>
          Password
          <input
            type="password"
            value={newUser.password}
            onChange={(e) => setNewUser({ ...newUser, password: e.target.value })}
            autoComplete="new-password"
            minLength={8}
            required
          />
        </label>
        <label>
          Role
          <Select value={newUser.role} onChange={(v) => setNewUser({ ...newUser, role: v as Role })}>
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </Select>
        </label>
        <Button disabled={createUser.isPending}>Create</Button>
      </form>
      {createUser.isError && <p className="error">{createUser.error.message}</p>}

      <h2>API tokens</h2>
      {created && (
        <Banner>
          <p>
            Token <strong>{created.name}</strong> — copy it now, it is not shown again:
          </p>
          <pre className="output">{created.token}</pre>
          <Button variant="text" onClick={() => setCreated(null)}>
            Done
          </Button>
        </Banner>
      )}
      {tokens.data.length === 0 ? (
        <p className="muted">No tokens.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>User</th>
              <th>Role</th>
              <th>Expires</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {tokens.data.map((t) => (
              <tr key={t.id}>
                <td>{t.name}</td>
                <td>{t.username}</td>
                <td>{t.role || <span className="muted">same as user</span>}</td>
                <td>{t.expires_at ? new Date(t.expires_at).toLocaleDateString() : "never"}</td>
                <td>
                  <Button
                    variant="text"
                    disabled={removeToken.isPending}
                    onClick={() =>
                      window.confirm(`Revoke ${t.name}? Whatever uses it stops working.`) && removeToken.mutate(t.id)
                    }
                  >
                    Revoke
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {removeToken.isError && <p className="error">{removeToken.error.message}</p>}

      <h2>New token</h2>
      <form className="toolbar" onSubmit={submitToken}>
        <label>
          User
          <Select
            value={newToken.user_id}
            onChange={(v) => setNewToken({ ...newToken, user_id: Number(v) })}
            required
          >
            <option value={0}>Choose…</option>
            {users.data.map((u) => (
              <option key={u.id} value={u.id}>
                {u.username}
              </option>
            ))}
          </Select>
        </label>
        <label>
          Name
          <input value={newToken.name} onChange={(e) => setNewToken({ ...newToken, name: e.target.value })} required />
        </label>
        <label>
          Role
          <Select value={newToken.role} onChange={(v) => setNewToken({ ...newToken, role: v as Role | "" })}>
            <option value="">same as user</option>
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {r} or less
              </option>
            ))}
          </Select>
        </label>
        <label>
          Days
          <input
            value={newToken.days}
            onChange={(e) => setNewToken({ ...newToken, days: e.target.value })}
            inputMode="numeric"
            placeholder="never expires"
          />
        </label>
        <Button disabled={createToken.isPending || newToken.user_id === 0}>Create</Button>
      </form>
      {createToken.isError && <p className="error">{createToken.error.message}</p>}
    </>
  );
}
