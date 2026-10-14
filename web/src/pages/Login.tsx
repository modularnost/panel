import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { api, call } from "../api/client";

export function Login() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const from = (useLocation().state as { from?: string } | null)?.from ?? "/";
  // The server sends a browser here with next=, from a module page it guards.
  const next = useSearchParams()[0].get("next");

  const login = useMutation({
    mutationFn: () => call(api.login({ username, password })),
    onSuccess: (me) => {
      queryClient.setQueryData(["me"], me);
      if (next && /^\/(?![/\\])/.test(next)) {
        window.location.assign(next);
        return;
      }
      navigate(from, { replace: true });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };

  return (
    <form className="login" onSubmit={submit}>
      <h1>modularnost</h1>
      <label>
        Username
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus autoComplete="username" />
      </label>
      <label>
        Password
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
        />
      </label>
      {login.isError && <p className="error">{login.error.message}</p>}
      <button disabled={login.isPending}>Sign in</button>
    </form>
  );
}
