import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, call } from "../api/client";
import { atLeast, useMe } from "../auth";
import { modulePath } from "../modules";
import { Button } from "../components/Button";
import { Chip } from "../components/Chip";
import { useDialogs } from "../components/Dialogs";
import { Table } from "../components/Table";

export function Modules() {
  const me = useMe();
  const queryClient = useQueryClient();
  const admin = atLeast(me.data?.role, "admin");
  const { confirm } = useDialogs();

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
        <Table>
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
                  {m.ok && <Chip tone="ok">ok</Chip>}
                  {m.state === "starting" && <Chip>{m.error}</Chip>}
                  {(m.state === "failed" || m.state === "error") && (
                    <Chip tone="bad">{m.error || "unavailable"}</Chip>
                  )}
                </td>
                <td className="muted">{m.permissions.join(", ")}</td>
                {admin && (
                  <td>
                    <Button
                      variant="text"
                      disabled={uninstall.isPending}
                      onClick={async () =>
                        (await confirm({
                          title: `Remove ${m.name}?`,
                          message: "Its service is stopped; the image and its data stay.",
                          action: "Remove",
                          danger: true,
                        })) && uninstall.mutate(m.name)
                      }
                    >
                      Remove
                    </Button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}
