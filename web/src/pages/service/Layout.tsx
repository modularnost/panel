import { useQuery } from "@tanstack/react-query";
import { Link, NavLink, Outlet, useOutletContext, useParams } from "react-router";
import { api, call } from "../../api/client";
import type { Service } from "../../api/generated";
import { atLeast, useMe } from "../../auth";

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
    refetchInterval: 3000,
  });

  if (service.isPending) return <p>Loading…</p>;
  if (service.isError) return <p className="error">{service.error.message}</p>;
  const s = service.data;

  return (
    <>
      <p className="muted">
        <Link to="/">Services</Link>
      </p>
      <h1>
        {s.name}{" "}
        <span className={`pill ${s.healthy ? "ok" : "bad"}`}>
          {s.running}/{s.desired}
        </span>
      </h1>
      <nav className="tabs">
        <NavLink to="." end>
          Overview
        </NavLink>
        <NavLink to="logs">Logs</NavLink>
        <NavLink to="deploys">Deploys</NavLink>
        {admin && <NavLink to="traefik">Traefik</NavLink>}
        {admin && <NavLink to="webhooks">Webhooks</NavLink>}
      </nav>
      <Outlet context={s} />
    </>
  );
}
