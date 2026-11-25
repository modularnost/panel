import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useOutletContext, useParams } from "react-router";
import { api, call, retryableQueryError } from "../../api/client";
import { RefreshWarning } from "../../components/RefreshWarning";
import type { Service } from "../../api/generated";
import { atLeast, useMe } from "../../auth";
import { useModules, serviceTabs } from "../../modules";
import { Chip } from "../../components/Chip";
import { Tab, Tabs } from "../../components/Tabs";

// The tabs of a service share the one service the layout loads.
export function useService() {
  return useOutletContext<Service>();
}

export function ServiceLayout() {
  const { id = "" } = useParams();
  const me = useMe();
  const admin = atLeast(me.data?.role, "admin");
  const service = useQuery({
    queryKey: ["service", id],
    queryFn: () => call(api.getService(id)),
    refetchInterval: ({ state: { data } }) => data && data.running < data.desired ? 3000 : 10_000,
  });

  const modules = useModules();

  if (service.isPending) return <p>Loading…</p>;
  if (service.isError && (service.data === undefined || !retryableQueryError(service.error))) {
    return <p className="error">{service.error.message}</p>;
  }
  const s = service.data;

  return (
    <>
      <p className="muted">
        <Link to="/">Services</Link>
      </p>
      <RefreshWarning error={service.error} />
      <h1>
        {s.name}{" "}
        <Chip tone={s.healthy ? "ok" : "bad"}>
          {s.running}/{s.desired}
        </Chip>
      </h1>
      <Tabs label="Service">
        <Tab icon="info" to="." end>
          Overview
        </Tab>
        <Tab icon="terminal" to="logs">Logs</Tab>
        <Tab icon="rocket_launch" to="deploys">Deploys</Tab>
        <Tab icon="lan" to="networks">Networks</Tab>
        {admin && <Tab icon="alt_route" to="traefik">Traefik</Tab>}
        {admin && <Tab icon="webhook" to="webhooks">Webhooks</Tab>}
        {serviceTabs(modules.data, me.data?.role, s).map(({ module, contribution: c }) => (
          <Tab key={`${module}${c.path}`} icon="extension" to={`module/${module}${c.path}`}>
            {c.title}
          </Tab>
        ))}
      </Tabs>
      <Outlet context={s} />
    </>
  );
}
