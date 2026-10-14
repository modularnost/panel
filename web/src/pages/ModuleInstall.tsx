import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Link, useNavigate } from "react-router";
import { api, call } from "../api/client";
import { envLines } from "../modules";

export function ModuleInstall() {
  const [name, setName] = useState("");
  const [image, setImage] = useState("");
  const [port, setPort] = useState("");
  const [env, setEnv] = useState("");
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const install = useMutation({
    mutationFn: () =>
      call(api.installModule({ name: name.trim(), image: image.trim(), port: port.trim(), env: envLines(env) })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["modules"] });
      navigate("/modules");
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    install.mutate();
  };

  return (
    <>
      <p className="muted">
        <Link to="/modules">Modules</Link>
      </p>
      <h1>Install a module</h1>
      <form className="form" onSubmit={submit}>
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="metrics" autoFocus required />
        </label>
        <label>
          Image
          <input
            value={image}
            onChange={(e) => setImage(e.target.value)}
            placeholder="ghcr.io/modularnost/metrics-module:0.1.0"
            required
          />
        </label>
        <label>
          Port
          <input value={port} onChange={(e) => setPort(e.target.value)} placeholder="8080" inputMode="numeric" />
        </label>
        <label>
          Environment
          <textarea value={env} onChange={(e) => setEnv(e.target.value)} rows={4} placeholder="KEY=value" />
        </label>
        {install.isError && <p className="error">{install.error.message}</p>}
        <button disabled={install.isPending}>{install.isPending ? "Installing…" : "Install"}</button>
      </form>
    </>
  );
}
