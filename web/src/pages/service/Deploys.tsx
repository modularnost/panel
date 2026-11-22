import { useQuery } from "@tanstack/react-query";
import { api, call, retryableQueryError } from "../../api/client";
import { DeployTable } from "../../components/DeployTable";
import { RefreshWarning } from "../../components/RefreshWarning";
import { useService } from "./Layout";

export function ServiceDeploys() {
  const s = useService();
  const deploys = useQuery({
    queryKey: ["deploys", s.id],
    queryFn: () => call(api.listServiceDeploys(s.id, { limit: 50 })),
    refetchInterval: 5000,
  });

  if (deploys.isPending) return <p>Loading…</p>;
  if (deploys.isError && (deploys.data === undefined || !retryableQueryError(deploys.error))) {
    return <p className="error">{deploys.error.message}</p>;
  }
  return (
    <>
      <RefreshWarning error={deploys.error} />
      <DeployTable deploys={deploys.data} />
    </>
  );
}
