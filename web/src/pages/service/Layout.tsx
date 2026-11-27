import { useQuery } from "@tanstack/react-query";
import { Link, matchPath, Outlet, useLocation, useOutletContext, useParams } from "react-router";
import { api, call, retryableQueryError } from "../../api/client";
import { RefreshWarning } from "../../components/RefreshWarning";
import type { Service } from "../../api/generated";
import { atLeast, useMe } from "../../auth";
import { useModules, serviceTabs } from "../../modules";
import { Chip } from "../../components/Chip";
import { Icon, type IconName } from "../../components/Icon";
import { Tab } from "../../components/Tabs";

// The tabs of a service share the one service the layout loads.
export function useService() {
  return useOutletContext<Service>();
}

export function ServiceLayout() {
  const { id = "" } = useParams();
  const { pathname } = useLocation();
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
  const more: { title: string; to: string; icon: IconName }[] = [
    { title: "Networks", to: "networks", icon: "lan" },
    { title: "Volumes", to: "volumes", icon: "hard_drive" },
  ];
  if (admin) more.push(
    { title: "Traefik", to: "traefik", icon: "alt_route" },
    { title: "Webhooks", to: "webhooks", icon: "webhook" },
  );
  for (const { module, contribution } of serviceTabs(modules.data, me.data?.role, s)) {
    more.push({ title: contribution.title, to: `module/${module}${contribution.path}`, icon: "extension" });
  }
  const active = more.find((entry) => matchPath(`/services/:id/${entry.to}`, pathname));

  return (
    <>
      <p className="muted">
        <Link to="/services">Services</Link>
      </p>
      <RefreshWarning error={service.error} />
      <h1>
        {s.name}{" "}
        <Chip tone={s.healthy ? "ok" : "bad"}>
          {s.running}/{s.desired}
        </Chip>
      </h1>
      <nav className="tabs service-tabs" aria-label="Service">
        <Tab icon="info" to="." end>
          Overview
        </Tab>
        <Tab icon="terminal" to="logs">Logs</Tab>
        <Tab icon="rocket_launch" to="deploys">Deploys</Tab>
        <details
          key={pathname}
          className="service-more"
          onBlur={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget)) e.currentTarget.open = false;
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape" && e.currentTarget.open) {
              e.stopPropagation();
              e.currentTarget.open = false;
              e.currentTarget.querySelector("summary")?.focus();
            }
          }}
        >
          <summary className={active ? "active" : undefined} aria-label={active && `More service pages, current page ${active.title}`}>
            <span>{active?.title ?? "More"}</span>
            <Icon name="chevron_right" size={16} />
          </summary>
          <div className="service-more-links">
            {more.map((entry) => <Tab key={entry.to} to={entry.to} icon={entry.icon} end>{entry.title}</Tab>)}
          </div>
        </details>
      </nav>
      <Outlet context={s} />
    </>
  );
}
