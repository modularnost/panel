import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, call } from "../api/client";
import { atLeast, useMe } from "../auth";
import { ComposeEditor } from "../components/ComposeEditor";
import { SAMPLE_COMPOSE } from "../stacks";

export function Stack() {
  const { name = "" } = useParams();
  const me = useMe();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const admin = atLeast(me.data?.role, "admin");

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
  const redeploy = useMutation({ mutationFn: () => call(api.redeployStack(name)), onSuccess: refresh });
  const remove = useMutation({
    mutationFn: () => call(api.deleteStack(name)),
    onSuccess: () => {
      refresh();
      navigate("/stacks", { replace: true });
    },
  });

  if (stack.isPending) return <p>Loading…</p>;
  if (stack.isError) return <p className="error">{stack.error.message}</p>;
  const s = stack.data;

  const confirmRemove = () => {
    if (window.confirm(`Remove stack ${s.name}? All ${s.service_count} of its services are stopped and removed.`)) {
      remove.mutate();
    }
  };

  return (
    <>
      <p className="muted">
        <Link to="/stacks">Stacks</Link>
      </p>
      <h1>
        {s.name}{" "}
        <span className={`pill ${s.healthy ? "ok" : "bad"}`}>
          {s.running}/{s.desired}
        </span>
      </h1>

      <div className="actions">
        {atLeast(me.data?.role, "operator") && (
          <button onClick={() => redeploy.mutate()} disabled={redeploy.isPending}>
            {redeploy.isPending ? "Redeploying…" : "Redeploy"}
          </button>
        )}
        {admin && (
          <button className="danger" onClick={confirmRemove} disabled={remove.isPending}>
            Remove
          </button>
        )}
      </div>
      {redeploy.isError && <p className="error">{redeploy.error.message}</p>}
      {remove.isError && <p className="error">{remove.error.message}</p>}
      {redeploy.isSuccess && <p className="muted">Redeployed.</p>}

      <h2>Services</h2>
      {s.services.length === 0 ? (
        <p className="muted">No services are running.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Service</th>
              <th>Image</th>
              <th>Replicas</th>
            </tr>
          </thead>
          <tbody>
            {s.services.map((svc) => (
              <tr key={svc.id}>
                <td>
                  <Link to={`/services/${svc.id}`}>{svc.name}</Link>
                </td>
                <td className="mono">{svc.image}</td>
                <td>
                  <span className={`pill ${svc.healthy ? "ok" : "bad"}`}>
                    {svc.running}/{svc.desired}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {admin && (
        <>
          <h2>Compose file</h2>
          {!s.managed && (
            <p className="muted">
              This stack was not deployed from the panel, so there is no file to show. Paste one to manage it here.
            </p>
          )}
          {/* key: a new file arriving from outside must not keep the old text */}
          <ComposeEditor key={s.updated_at ?? "none"} name={s.name} initial={s.compose ?? SAMPLE_COMPOSE} />
        </>
      )}
    </>
  );
}
