import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, call } from "../api/client";
import { atLeast, useMe } from "../auth";
import { modulePath } from "../modules";

export function Modules() {
  const me = useMe();
  const queryClient = useQueryClient();
  const admin = atLeast(me.data?.role, "admin");

  const modules = useQuery({
    queryKey: ["modules"],
    queryFn: () => call(api.listModules()),
    refetchInterval: 3000,
  });
  const uninstall = useMutation({
    mutationFn: (name: string) => call(api.uninstallModule(name)),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["modules"] }),
  });

  // The first menu entry this role may open.
  const firstNav = (m: { contributions: { slot: string; path: string; role: string }[] }) =>
    m.contributions.find((c) => c.slot === "nav" && atLeast(me.data?.role, c.role as "viewer" | "operator" | "admin"));

  if (modules.isPending) return <p>Loading…</p>;
  if (modules.isError) return <p className="error">{modules.error.message}</p>;

  return (
    <>
      <h1>Modules</h1>
      {admin && (
        <p>
          <Link to="/install-module">Install a module</Link>
        </p>
      )}
      {uninstall.isError && <p className="error">{uninstall.error.message}</p>}
      {modules.data.length === 0 ? (
        <p className="muted">No modules.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Module</th>
              <th>Version</th>
              <th>Service</th>
              <th>Status</th>
              <th>Permissions</th>
              {admin && <th />}
            </tr>
          </thead>
          <tbody>
            {modules.data.map((m) => (
              <tr key={m.name}>
                <td>
                  {m.ok && firstNav(m) ? (
                    <Link to={modulePath(m.name, firstNav(m)!.path)}>{m.name}</Link>
                  ) : (
                    m.name
                  )}
                </td>
                <td>{m.version}</td>
                <td className="mono">{m.service}</td>
                <td>
                  {m.ok && <span className="pill ok">ok</span>}
                  {m.state === "starting" && <span className="pill">{m.error}</span>}
                  {(m.state === "failed" || m.state === "error") && (
                    <span className="pill bad">{m.error || "unavailable"}</span>
                  )}
                </td>
                <td className="muted">{m.permissions.join(", ")}</td>
                {admin && (
                  <td>
                    <button
                      className="link"
                      disabled={uninstall.isPending}
                      onClick={() =>
                        window.confirm(`Remove ${m.name}? Its service is stopped; the image and its data stay.`) &&
                        uninstall.mutate(m.name)
                      }
                    >
                      Remove
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
