import { useQuery, useQueryClient } from "@tanstack/react-query";
import { NavLink, Navigate, Outlet, Route, Routes, useLocation } from "react-router";
import { ApiError, api, call } from "./api/client";
import { moduleURL } from "./modules";
import { useMe } from "./auth";
import { Deploys } from "./pages/Deploys";
import { Login } from "./pages/Login";
import { ModuleInstall } from "./pages/ModuleInstall";
import { ModulePage } from "./pages/ModulePage";
import { Modules } from "./pages/Modules";
import { Resources } from "./pages/Resources";
import { Stack } from "./pages/Stack";
import { StackNew } from "./pages/StackNew";
import { Stacks } from "./pages/Stacks";
import { ServiceLayout } from "./pages/service/Layout";
import { ServiceDeploys } from "./pages/service/Deploys";
import { Logs } from "./pages/service/Logs";
import { Overview } from "./pages/service/Overview";
import { Traefik } from "./pages/service/Traefik";
import { Webhooks } from "./pages/service/Webhooks";
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

  if (me.isPending) return null;
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }
  if (me.isError) return <p className="error">{me.error.message}</p>;

  const logout = async () => {
    await call(api.logout());
    queryClient.clear();
    window.location.assign("/app/login");
  };

  return (
    <div className="app">
      <aside className="side">
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
        {modules.data
          ?.filter((m) => m.ok)
          .flatMap((m) => m.nav_entries.map((e) => ({ key: `${m.name}${e.path}`, title: e.title, to: moduleURL(m.name, e.path) })))
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
          <Route path="webhooks" element={<Webhooks />} />
        </Route>
        <Route path="stacks" element={<Stacks />} />
        <Route path="stacks/new" element={<StackNew />} />
        <Route path="stacks/:name" element={<Stack />} />
        <Route path="modules" element={<Modules />} />
        <Route path="modules/install" element={<ModuleInstall />} />
        <Route path="modules/:name/*" element={<ModulePage />} />
        <Route path="resources" element={<Resources />} />
        <Route path="deploys" element={<Deploys />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
