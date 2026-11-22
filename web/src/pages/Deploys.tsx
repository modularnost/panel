import { useQuery } from "@tanstack/react-query";
import { api, call, retryableQueryError } from "../api/client";
import { RefreshWarning } from "../components/RefreshWarning";
import { DeployTable } from "../components/DeployTable";

export function Deploys() {
  const deploys = useQuery({
    queryKey: ["deploys"],
    queryFn: () => call(api.listDeploys({ limit: 100 })),
    refetchInterval: 5000,
  });

  if (deploys.isPending) return <p>Loading…</p>;
  if (deploys.isError && (deploys.data === undefined || !retryableQueryError(deploys.error))) {
    return <p className="error">{deploys.error.message}</p>;
  }

  return (
    <>
      <RefreshWarning error={deploys.error} />
      <h1>Deploys</h1>
      <DeployTable deploys={deploys.data} />
    </>
  );
}
