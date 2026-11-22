import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, call, retryableQueryError } from "../api/client";
import { RefreshWarning } from "../components/RefreshWarning";
import { atLeast, useMe } from "../auth";
import { ComposeEditor } from "../components/ComposeEditor";
import { Button } from "../components/Button";
import { useDialogs } from "../components/Dialogs";
import { Chip } from "../components/Chip";
import { Table } from "../components/Table";
import { useTableSort } from "../components/TableSort";
import { ActivityTime } from "../components/ActivityTime";
import type { Service } from "../api/generated";

export function Stack() {
  const { name = "" } = useParams();
  const me = useMe();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const admin = atLeast(me.data?.role, "admin");
  const serviceSort = useTableSort<Service>({
    name: (s) => s.name, image: (s) => s.image, replicas: (s) => s.running,
    deploy: (s) => s.last_deploy_at ? Date.parse(s.last_deploy_at) : undefined,
    updated: (s) => s.swarm_updated_at ? Date.parse(s.swarm_updated_at) : undefined,
  });

  const stack = useQuery({
    queryKey: ["stack", name],
    queryFn: () => call(api.getStack(name)),
    refetchInterval: 3000,
  });

  const refresh = () => {
    for (const key of ["stacks", "stack", "services", "deploys"]) {
      queryClient.invalidateQueries({ queryKey: [key] });
    }
  };
  const { confirm } = useDialogs();
  const redeploy = useMutation({ mutationFn: () => call(api.redeployStack(name)), onSuccess: refresh });
  const remove = useMutation({
    mutationFn: () => call(api.deleteStack(name)),
    onSuccess: () => {
      refresh();
      navigate("/stacks", { replace: true });
    },
  });

  if (stack.isPending) return <p>Loading…</p>;
  if (stack.isError && (stack.data === undefined || !retryableQueryError(stack.error))) {
    return <p className="error">{stack.error.message}</p>;
  }
  const s = stack.data;

  const confirmRemove = async () => {
    const ok = await confirm({
      title: `Remove stack ${s.name}?`,
      message: `All ${s.service_count} of its services are stopped and removed.`,
      action: "Remove",
      danger: true,
    });
    if (ok) remove.mutate();
  };

  return (
    <>
      <p className="muted">
        <Link to="/stacks">Stacks</Link>
      </p>
      <RefreshWarning error={stack.error} />
      <h1>
        {s.name}{" "}
        <Chip tone={s.healthy ? "ok" : "bad"}>
          {s.running}/{s.desired}
        </Chip>
      </h1>
      <dl className="facts">
        <dt>Last deploy</dt><dd><ActivityTime value={s.last_deploy_at} /></dd>
        <dt>Latest service update</dt><dd><ActivityTime value={s.swarm_updated_at} /></dd>
      </dl>

      <div className="actions">
        {atLeast(me.data?.role, "operator") && (
          <Button onClick={() => redeploy.mutate()} disabled={redeploy.isPending}>
            {redeploy.isPending ? "Redeploying…" : "Redeploy"}
          </Button>
        )}
        {admin && !s.self && (
          <Button variant="danger" onClick={confirmRemove} disabled={remove.isPending}>
            Remove
          </Button>
        )}
      </div>
      {redeploy.isError && <p className="error">{redeploy.error.message}</p>}
      {remove.isError && <p className="error">{remove.error.message}</p>}
      {redeploy.isSuccess && <p className="muted">Redeployed.</p>}

      <h2>Services</h2>
      {s.services.length === 0 ? (
        <p className="muted">No services are running.</p>
      ) : (
        <Table>
          <thead>
            <tr>
              {serviceSort.header("name", "Service")}
              {serviceSort.header("image", "Image")}
              {serviceSort.header("replicas", "Running replicas")}
              {serviceSort.header("deploy", "Last deploy")}
              {serviceSort.header("updated", "Swarm updated")}
            </tr>
          </thead>
          <tbody>
            {serviceSort.rows(s.services).map((svc) => (
              <tr key={svc.id}>
                <td>
                  <Link to={`/services/${svc.id}`}>{svc.name}</Link>
                </td>
                <td className="mono">{svc.image}</td>
                <td>
                  <Chip tone={svc.healthy ? "ok" : "bad"}>
                    {svc.running}/{svc.desired}
                  </Chip>
                </td>
                <td><ActivityTime value={svc.last_deploy_at} /></td>
                <td><ActivityTime value={svc.swarm_updated_at} /></td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}

      {admin && s.self && (
        <p className="muted">This is the panel's own stack. It is managed by the installer (install.sh).</p>
      )}
      {admin && !s.self && (
        <>
          <h2>Compose file</h2>
          {s.managed ? (
            /* key: a new file arriving from outside must not keep the old text */
            <ComposeEditor key={s.updated_at ?? "none"} name={s.name} initial={s.compose ?? ""}
              initialRegistry={s.registry_host} />
          ) : (
            <details>
              <summary>Manage with a compose file…</summary>
              <p className="muted">
                This stack was not deployed from the panel, so there is no file to show. Deploying one removes every
                service of the stack that is not in it.
              </p>
              <ComposeEditor name={s.name} initial="" placeholder="Paste the compose file for this stack" />
            </details>
          )}
        </>
      )}
    </>
  );
}
