import { useQuery } from "@tanstack/react-query";
import { api, call } from "../../api/client";
import { DeployTable } from "../../components/DeployTable";
import { useService } from "./Layout";

export function ServiceDeploys() {
  const s = useService();
  const deploys = useQuery({
    queryKey: ["deploys", s.id],
    queryFn: () => call(api.listServiceDeploys(s.id, { limit: 50 })),
    refetchInterval: 5000,
  });

  if (deploys.isPending) return <p>Loading…</p>;
  if (deploys.isError) return <p className="error">{deploys.error.message}</p>;
  return <DeployTable deploys={deploys.data} />;
}
