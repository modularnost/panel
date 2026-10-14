import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, call } from "../api/client";
import { atLeast, useMe } from "../auth";
import { Chip } from "../components/Chip";
import { Table } from "../components/Table";

export function Stacks() {
  const me = useMe();
  const stacks = useQuery({
    queryKey: ["stacks"],
    queryFn: () => call(api.listStacks()),
    refetchInterval: 3000,
  });

  if (stacks.isPending) return <p>Loading…</p>;
  if (stacks.isError) return <p className="error">{stacks.error.message}</p>;

  return (
    <>
      <h1>Stacks</h1>
      {atLeast(me.data?.role, "admin") && (
        <p>
          <Link to="/stacks/new">New stack</Link>
        </p>
      )}
      {stacks.data.length === 0 ? (
        <p className="muted">No stacks.</p>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>Stack</th>
              <th>Services</th>
              <th>Replicas</th>
              <th>Compose file</th>
            </tr>
          </thead>
          <tbody>
            {stacks.data.map((s) => (
              <tr key={s.name}>
                <td>
                  <Link to={`/stacks/${s.name}`}>{s.name}</Link>
                </td>
                <td>{s.service_count}</td>
                <td>
                  <Chip tone={s.healthy ? "ok" : "bad"}>
                    {s.running}/{s.desired}
                  </Chip>
                </td>
                <td className="muted">{s.managed ? "kept here" : "not kept"}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}
