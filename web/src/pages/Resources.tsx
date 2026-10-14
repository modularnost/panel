import { useQuery } from "@tanstack/react-query";
import { api, call } from "../api/client";
import { formatBytes, memoryUse } from "../format";
import { Table } from "../components/Table";

export function Resources() {
  // Sampling CPU takes a second on the server, so this is polled slower than the lists.
  const stats = useQuery({
    queryKey: ["stats"],
    queryFn: () => call(api.listStats()),
    refetchInterval: 10_000,
  });

  if (stats.isPending) return <p>Loading…</p>;
  if (stats.isError) return <p className="error">{stats.error.message}</p>;

  return (
    <>
      <h1>Resources</h1>
      {stats.data.length === 0 ? (
        <p>Nothing is running.</p>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>Node</th>
              <th>Service</th>
              <th>Replica</th>
              <th>CPU</th>
              <th>Memory</th>
              <th>Network in / out</th>
              <th>Disk read / write</th>
            </tr>
          </thead>
          <tbody>
            {stats.data.map((s, i) => (
              <tr key={`${s.node}/${s.service}/${s.slot}/${i}`}>
                <td>{s.node}</td>
                {s.error ? (
                  <td colSpan={6} className="error">
                    {s.error}
                  </td>
                ) : (
                  <>
                    <td>{s.service}</td>
                    <td>{s.slot || ""}</td>
                    <td>{s.cpu.toFixed(1)}%</td>
                    <td>{memoryUse(s.mem, s.mem_limit)}</td>
                    <td>
                      {formatBytes(s.net_rx)} / {formatBytes(s.net_tx)}
                    </td>
                    <td>
                      {formatBytes(s.block_read)} / {formatBytes(s.block_write)}
                    </td>
                  </>
                )}
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}
