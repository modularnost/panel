import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { NavLink, Navigate, Outlet, Route, Routes, useLocation } from "react-router";
import { ApiError, api, call } from "./api/client";
import { modulePath } from "./modules";
import { atLeast, useMe } from "./auth";
import { usePanelVersion, useUpdates } from "./updates";
import { Deploys } from "./pages/Deploys";
import { Login } from "./pages/Login";
import { ModuleInstall } from "./pages/ModuleInstall";
import { ModulePage } from "./pages/ModulePage";
import { Modules } from "./pages/Modules";
import { Nodes } from "./pages/Nodes";
import { Resources } from "./pages/Resources";
import { Stack } from "./pages/Stack";
import { StackNew } from "./pages/StackNew";
import { Stacks } from "./pages/Stacks";
import { Updates } from "./pages/Updates";
import { Users } from "./pages/Users";
import { Webhooks } from "./pages/Webhooks";
import { ServiceLayout } from "./pages/service/Layout";
import { ModuleTab } from "./pages/service/ModuleTab";
import { ServiceDeploys } from "./pages/service/Deploys";
import { Logs } from "./pages/service/Logs";
import { Overview } from "./pages/service/Overview";
import { Traefik } from "./pages/service/Traefik";
import { ServiceWebhooks } from "./pages/service/Webhooks";
import { Services } from "./pages/Services";

function Shell() {
  const me = useMe();
  const queryClient = useQueryClient();
  const location = useLocation();
  const modules = useQuery({
    queryKey: ["modules"],
    queryFn: () => call(api.listModules()),
    refetchInterval: 10_000,
    enabled: me.isSuccess,
  });

  const isAdmin = atLeast(me.data?.role, "admin");
  const updates = useUpdates(isAdmin);
  const pending = updates.data?.releases.length ?? 0;
  const version = usePanelVersion();
  const [drawer, setDrawer] = useState(false);

  useEffect(() => setDrawer(false), [location.pathname]);
  useEffect(() => {
    if (!drawer) return;
    const close = (e: KeyboardEvent) => e.key === "Escape" && setDrawer(false);
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [drawer]);

  if (me.isPending) return null;
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }
  if (me.isError) return <p className="error">{me.error.message}</p>;

  const logout = async () => {
    await call(api.logout());
    queryClient.clear();
    window.location.assign("/login");
  };

  return (
    <div className="app">
      <header className="bar">
        <button className="menu" aria-label="Menu" aria-expanded={drawer} onClick={() => setDrawer(true)}>
          <svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
            <path d="M3 6h18v2H3zm0 5h18v2H3zm0 5h18v2H3z" fill="currentColor" />
          </svg>
        </button>
        <span className="brand">modularnost</span>
      </header>
      <div className={`scrim${drawer ? " open" : ""}`} onClick={() => setDrawer(false)} />
      <aside className={`side${drawer ? " open" : ""}`}>
        <span className="brand">modularnost</span>
        <NavLink to="/" end className="item">
          Services
        </NavLink>
        <NavLink to="/stacks" className="item">
          Stacks
        </NavLink>
        <NavLink to="/resources" className="item">
          Resources
        </NavLink>
        <NavLink to="/deploys" className="item">
          Deploys
        </NavLink>
        <NavLink to="/modules" end className="item">
          Modules
        </NavLink>
        {atLeast(me.data?.role, "admin") && (
          <>
            <NavLink to="/nodes" className="item">
              Nodes
            </NavLink>
            <NavLink to="/webhooks" className="item">
              Webhooks
            </NavLink>
            <NavLink to="/users" className="item">
              Users
            </NavLink>
            <NavLink to="/updates" className="item">
              Updates{pending > 0 && <span className="pill ok"> {pending}</span>}
            </NavLink>
          </>
        )}
        {modules.data
          ?.filter((m) => m.ok)
          .flatMap((m) =>
            m.contributions
              .filter((c) => c.slot === "nav" && atLeast(me.data?.role, c.role))
              .map((c) => ({ key: `${m.name}${c.path}`, title: c.title, to: modulePath(m.name, c.path) })),
          )
          .map((e) => (
            <NavLink key={e.key} to={e.to} className="item sub">
              {e.title}
            </NavLink>
          ))}
        <div className="who">
          {me.data.username} · {me.data.role}
          <button className="link" onClick={logout}>
            Sign out
          </button>
          {version.data && <div className="mono">{version.data}</div>}
        </div>
      </aside>
      <main className="main">
        <Outlet />
      </main>
    </div>
  );
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<Shell />}>
        <Route index element={<Services />} />
        <Route path="services/:id" element={<ServiceLayout />}>
          <Route index element={<Overview />} />
          <Route path="logs" element={<Logs />} />
          <Route path="deploys" element={<ServiceDeploys />} />
          <Route path="traefik" element={<Traefik />} />
          <Route path="webhooks" element={<ServiceWebhooks />} />
          <Route path="module/:name/*" element={<ModuleTab />} />
        </Route>
        <Route path="stacks" element={<Stacks />} />
        <Route path="stacks/new" element={<StackNew />} />
        <Route path="stacks/:name" element={<Stack />} />
        <Route path="modules" element={<Modules />} />
        <Route path="install-module" element={<ModuleInstall />} />
        <Route path="module/:name/*" element={<ModulePage />} />
        <Route path="nodes" element={<Nodes />} />
        <Route path="webhooks" element={<Webhooks />} />
        <Route path="users" element={<Users />} />
        <Route path="updates" element={<Updates />} />
        <Route path="resources" element={<Resources />} />
        <Route path="deploys" element={<Deploys />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
