import { useQuery } from "@tanstack/react-query";
import { api, call } from "../api/client";
import { DeployTable } from "../components/DeployTable";

export function Deploys() {
  const deploys = useQuery({
    queryKey: ["deploys"],
    queryFn: () => call(api.listDeploys({ limit: 100 })),
    refetchInterval: 5000,
  });

  if (deploys.isPending) return <p>Loading…</p>;
  if (deploys.isError) return <p className="error">{deploys.error.message}</p>;

  return (
    <>
      <h1>Deploys</h1>
      <DeployTable deploys={deploys.data} />
    </>
  );
}
