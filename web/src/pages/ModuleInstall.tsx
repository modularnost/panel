import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Link, useNavigate } from "react-router";
import { api, call } from "../api/client";
import { envLines } from "../modules";
import { Button } from "../components/Button";
import { TextField } from "../components/TextField";

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
        <TextField label="Name" value={name} onChange={setName} placeholder="metrics" autoFocus required />
        <TextField label="Image" value={image} onChange={setImage} placeholder="ghcr.io/modularnost/metrics-module:0.1.0" required />
        <TextField label="Port" value={port} onChange={setPort} placeholder="8080" inputMode="numeric" />
        <TextField label="Environment" type="textarea" value={env} onChange={setEnv} rows={4} placeholder="KEY=value" mono />
        {install.isError && <p className="error">{install.error.message}</p>}
        <Button disabled={install.isPending}>{install.isPending ? "Installing…" : "Install"}</Button>
      </form>
    </>
  );
}
