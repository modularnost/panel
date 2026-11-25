import { useQueryClient } from "@tanstack/react-query";
import { lazy, Suspense, useEffect, useState } from "react";
import { NavLink, Navigate, Outlet, Route, Routes, useLocation } from "react-router";
import { ApiError, api, call, retryableQueryError } from "./api/client";
import { RefreshWarning } from "./components/RefreshWarning";
import { modulePath, useModules } from "./modules";
import { atLeast, useMe } from "./auth";
import { usePanelVersion, useUpdates } from "./updates";
import { Deploys } from "./pages/Deploys";
import { Login } from "./pages/Login";
import { ModuleInstall } from "./pages/ModuleInstall";
import { InstalledModules, Modules } from "./pages/Modules";
import { Stacks } from "./pages/Stacks";
import { ServiceLayout } from "./pages/service/Layout";
import { ServiceDeploys } from "./pages/service/Deploys";
import { Overview } from "./pages/service/Overview";
import { Traefik } from "./pages/service/Traefik";
import { ServiceWebhooks } from "./pages/service/Webhooks";
import { Services } from "./pages/Services";
import { Button } from "./components/Button";
import { DialogsProvider } from "./components/Dialogs";
import { Chip } from "./components/Chip";
import { Icon } from "./components/Icon";

const ModulePage = lazy(() => import("./pages/ModulePage").then((page) => ({ default: page.ModulePage })));
const ModuleCatalog = lazy(() => import("./pages/ModuleCatalog").then((page) => ({ default: page.ModuleCatalog })));
const Networks = lazy(() => import("./pages/Networks").then((page) => ({ default: page.Networks })));
const NetworkDetail = lazy(() => import("./pages/Networks").then((page) => ({ default: page.NetworkDetail })));
const NetworkNew = lazy(() => import("./pages/Networks").then((page) => ({ default: page.NetworkNew })));
const ServiceNetworks = lazy(() => import("./pages/service/Networks").then((page) => ({ default: page.ServiceNetworks })));
const Nodes = lazy(() => import("./pages/Nodes").then((page) => ({ default: page.Nodes })));
const Ports = lazy(() => import("./pages/Ports").then((page) => ({ default: page.Ports })));
const Resources = lazy(() => import("./pages/Resources").then((page) => ({ default: page.Resources })));
const Stack = lazy(() => import("./pages/Stack").then((page) => ({ default: page.Stack })));
const StackNew = lazy(() => import("./pages/StackNew").then((page) => ({ default: page.StackNew })));
const Settings = lazy(() => import("./pages/Settings").then((page) => ({ default: page.Settings })));
const Users = lazy(() => import("./pages/Users").then((page) => ({ default: page.Users })));
const Webhooks = lazy(() => import("./pages/Webhooks").then((page) => ({ default: page.Webhooks })));
const ModuleTab = lazy(() => import("./pages/service/ModuleTab").then((page) => ({ default: page.ModuleTab })));
const Logs = lazy(() => import("./pages/service/Logs").then((page) => ({ default: page.Logs })));

function Shell() {
  const me = useMe();
  const queryClient = useQueryClient();
  const location = useLocation();
  const modules = useModules(me.isSuccess, true);

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
  if (me.isError && (me.data === undefined || !retryableQueryError(me.error))) {
    return <p className="error">{me.error.message}</p>;
  }

  const logout = async () => {
    await call(api.logout());
    queryClient.clear();
    window.location.assign("/login");
  };

  return (
    <div className="app">
      <header className="bar">
        <Button variant="icon" aria-label="Menu" aria-expanded={drawer} onClick={() => setDrawer(true)}>
          <svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
            <path d="M3 6h18v2H3zm0 5h18v2H3zm0 5h18v2H3z" fill="currentColor" />
          </svg>
        </Button>
        <span className="brand">modularnost</span>
      </header>
      <div className={`scrim${drawer ? " open" : ""}`} onClick={() => setDrawer(false)} />
      <aside className={`side${drawer ? " open" : ""}`}>
        <span className="brand">modularnost</span>
        <NavLink to="/" end className="item">
          <Icon name="dns" />
          Services
        </NavLink>
        <NavLink to="/stacks" className="item">
          <Icon name="layers" />
          Stacks
        </NavLink>
        <NavLink to="/networks" className="item">
          <Icon name="hub" />
          Networks
        </NavLink>
        <NavLink to="/resources" className="item">
          <Icon name="monitoring" />
          Resources
        </NavLink>
        <NavLink to="/deploys" className="item">
          <Icon name="rocket_launch" />
          Deploys
        </NavLink>
        <NavLink to="/modules" end className="item">
          <Icon name="extension" />
          Modules
        </NavLink>
        {atLeast(me.data?.role, "admin") && (
          <>
            <NavLink to="/nodes" className="item">
              <Icon name="hub" />
              Nodes
            </NavLink>
            <NavLink to="/ports" className="item">
              <Icon name="dns" />
              Ports
            </NavLink>
            <NavLink to="/webhooks" className="item">
              <Icon name="webhook" />
              Webhooks
            </NavLink>
            <NavLink to="/users" className="item">
              <Icon name="group" />
              Users
            </NavLink>
          </>
        )}
        <NavLink to="/settings" className="item">
          <Icon name="settings" />
          Settings{pending > 0 && <Chip tone="ok"> {pending}</Chip>}
        </NavLink>
        {modules.data
          ?.filter((m) => m.ok)
          .flatMap((m) =>
            m.contributions
              .filter((c) => c.slot === "nav" && atLeast(me.data?.role, c.role))
              .map((c) => ({ key: `${m.name}${c.path}`, title: c.title, to: modulePath(m.name, c.path) })),
          )
          .map((e) => (
            <NavLink key={e.key} to={e.to} className="item sub">
              <Icon name="extension" size={20} />
              {e.title}
            </NavLink>
          ))}
        <div className="who">
          {me.data.username} · {me.data.role}
          <Button variant="text" onClick={logout}>
            Sign out
          </Button>
          {version.data && <div className="mono">{version.data}</div>}
        </div>
      </aside>
      <main className="main">
        <RefreshWarning error={me.error} />
        <Suspense fallback={<p role="status">Loading…</p>}>
          <Outlet />
        </Suspense>
      </main>
    </div>
  );
}

export function App() {
  return (
    <DialogsProvider>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<Shell />}>
          <Route index element={<Services />} />
          <Route path="services/:id" element={<ServiceLayout />}>
            <Route index element={<Overview />} />
            <Route path="logs" element={<Logs />} />
            <Route path="deploys" element={<ServiceDeploys />} />
            <Route path="networks" element={<ServiceNetworks />} />
            <Route path="traefik" element={<Traefik />} />
            <Route path="webhooks" element={<ServiceWebhooks />} />
            <Route path="module/:name/*" element={<ModuleTab />} />
          </Route>
          <Route path="stacks" element={<Stacks />} />
          <Route path="stacks/new" element={<StackNew />} />
          <Route path="stacks/:name" element={<Stack />} />
          <Route path="modules" element={<Modules />}>
            <Route index element={<InstalledModules />} />
            <Route path="catalog" element={<ModuleCatalog />} />
          </Route>
          <Route path="install-module" element={<ModuleInstall />} />
          <Route path="module/:name/*" element={<ModulePage />} />
          <Route path="networks" element={<Networks />} />
          <Route path="networks/new" element={<NetworkNew />} />
          <Route path="networks/:id" element={<NetworkDetail />} />
          <Route path="networks/node/:node/:id" element={<NetworkDetail />} />
          <Route path="nodes" element={<Nodes />} />
          <Route path="ports" element={<Ports />} />
          <Route path="webhooks" element={<Webhooks />} />
          <Route path="users" element={<Users />} />
          <Route path="settings" element={<Settings />} />
          <Route path="updates" element={<Navigate to="/settings" replace />} />
          <Route path="resources" element={<Resources />} />
          <Route path="deploys" element={<Deploys />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </DialogsProvider>
  );
}
