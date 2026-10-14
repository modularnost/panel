import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useEffect, useState } from "react";
import { api, call } from "../../api/client";
import { useService } from "./Layout";
import { Button } from "../../components/Button";
import { TextField } from "../../components/TextField";

export function Traefik() {
  const s = useService();
  const queryClient = useQueryClient();
  const current = useQuery({
    queryKey: ["traefik", s.id],
    queryFn: () => call(api.getServiceTraefik(s.id)),
  });

  const [host, setHost] = useState("");
  const [port, setPort] = useState("");
  const [entrypoint, setEntrypoint] = useState("");
  const [resolver, setResolver] = useState("");

  useEffect(() => {
    if (!current.data) return;
    setHost(current.data.host);
    setPort(current.data.port ? String(current.data.port) : "");
    setEntrypoint(current.data.entrypoint);
    setResolver(current.data.cert_resolver);
  }, [current.data]);

  const save = useMutation({
    mutationFn: () =>
      call(
        api.setServiceTraefik(s.id, {
          host: host.trim(),
          port: port ? Number(port) : 0,
          entrypoint: entrypoint.trim(),
          cert_resolver: resolver.trim(),
        }),
      ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["traefik", s.id] });
      queryClient.invalidateQueries({ queryKey: ["service", s.id] });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };

  if (current.isPending) return <p>Loading…</p>;
  if (current.isError) return <p className="error">{current.error.message}</p>;

  return (
    <form className="form" onSubmit={submit}>
      <TextField label="Host" value={host} onChange={setHost} placeholder="app.example.com" />
      <TextField label="Container port" value={port} onChange={setPort} inputMode="numeric" pattern="[0-9]*" placeholder="8080" />
      <TextField label="Entrypoint" value={entrypoint} onChange={setEntrypoint} placeholder="websecure" />
      <TextField label="Certificate resolver" value={resolver} onChange={setResolver} placeholder="empty: plain HTTP" />
      {!host && !port && <p className="muted">With no host and no port the service is taken off Traefik.</p>}
      {save.isError && <p className="error">{save.error.message}</p>}
      {save.isSuccess && <p className="muted">Saved. Traefik picks it up on its own.</p>}
      <Button disabled={save.isPending}>{save.isPending ? "Saving…" : "Save"}</Button>
    </form>
  );
}
