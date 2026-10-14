import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { api, call } from "../api/client";
import { atLeast, useMe } from "../auth";
import { formatBytes, memoryUse, shortDigest } from "../format";

export function Service() {
  const { id = "" } = useParams();
  const me = useMe();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const service = useQuery({
    queryKey: ["service", id],
    queryFn: () => call(api.getService(id)),
    refetchInterval: 3000,
  });
  const name = service.data?.name;
  const stats = useQuery({
    queryKey: ["stats", name],
    queryFn: () => call(api.listStats({ service: name })),
    enabled: !!name,
    refetchInterval: 10_000,
  });

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ["service", id] });
    queryClient.invalidateQueries({ queryKey: ["services"] });
    queryClient.invalidateQueries({ queryKey: ["deploys"] });
  };
  const redeploy = useMutation({
    mutationFn: () => call(api.redeployService(id)),
    onSuccess: refresh,
  });
  const remove = useMutation({
    mutationFn: () => call(api.deleteService(id)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["services"] });
      navigate("/", { replace: true });
    },
  });

  if (service.isPending) return <p>Loading…</p>;
  if (service.isError) return <p className="error">{service.error.message}</p>;
  const s = service.data;

  const confirmRemove = () => {
    if (window.confirm(`Remove ${s.name}? Its replicas are stopped and the service is gone.`)) {
      remove.mutate();
    }
  };

  return (
    <>
      <p className="muted">
        <Link to="/">Services</Link>
      </p>
      <h1>
        {s.name}{" "}
        <span className={`pill ${s.healthy ? "ok" : "bad"}`}>
          {s.running}/{s.desired}
        </span>
      </h1>

      <dl className="facts">
        <dt>Stack</dt>
        <dd>{s.stack || "none"}</dd>
        <dt>Image</dt>
        <dd className="mono">{s.image}</dd>
        <dt>Digest</dt>
        <dd className="mono">{shortDigest(s.digest) || "not pinned"}</dd>
        <dt>Mode</dt>
        <dd>{s.mode}</dd>
      </dl>

      <div className="actions">
        {atLeast(me.data?.role, "operator") && (
          <button onClick={() => redeploy.mutate()} disabled={redeploy.isPending}>
            {redeploy.isPending ? "Redeploying…" : "Redeploy"}
          </button>
        )}
        {atLeast(me.data?.role, "admin") && (
          <button className="danger" onClick={confirmRemove} disabled={remove.isPending}>
            Remove
          </button>
        )}
      </div>
      {redeploy.isError && <p className="error">{redeploy.error.message}</p>}
      {remove.isError && <p className="error">{remove.error.message}</p>}
      {redeploy.isSuccess && (
        <p className="muted">
          Redeployed
          {redeploy.data.new_digest ? `: ${shortDigest(redeploy.data.old_digest)} → ${shortDigest(redeploy.data.new_digest)}` : ""}
          {redeploy.data.warnings.map((w) => (
            <span key={w} className="error">
              {" "}
              {w}
            </span>
          ))}
        </p>
      )}

      <h2>Replicas</h2>
      {stats.isError && <p className="error">{stats.error.message}</p>}
      {stats.data && stats.data.length === 0 && <p className="muted">No running replicas.</p>}
      {stats.data && stats.data.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>Node</th>
              <th>Replica</th>
              <th>CPU</th>
              <th>Memory</th>
              <th>Network in / out</th>
            </tr>
          </thead>
          <tbody>
            {stats.data.map((r, i) => (
              <tr key={`${r.node}/${r.slot}/${i}`}>
                <td>{r.node}</td>
                {r.error ? (
                  <td colSpan={4} className="error">
                    {r.error}
                  </td>
                ) : (
                  <>
                    <td>{r.slot || ""}</td>
                    <td>{r.cpu.toFixed(1)}%</td>
                    <td>{memoryUse(r.mem, r.mem_limit)}</td>
                    <td>
                      {formatBytes(r.net_rx)} / {formatBytes(r.net_tx)}
                    </td>
                  </>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
