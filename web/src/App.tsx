import { useQueryClient } from "@tanstack/react-query";
import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { Navigate, Outlet, Route, Routes, useLocation } from "react-router";
import { ApiError, api, call, retryableQueryError } from "./api/client";
import { RefreshWarning } from "./components/RefreshWarning";
import { useModules } from "./modules";
import { atLeast, useMe } from "./auth";
import { usePanelVersion, useUpdates } from "./updates";
import { Dashboard } from "./pages/Dashboard";
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
import { Icon } from "./components/Icon";
import { SidebarNav } from "./components/SidebarNav";

const ModulePage = lazy(() => import("./pages/ModulePage").then((page) => ({ default: page.ModulePage })));
const ModuleCatalog = lazy(() => import("./pages/ModuleCatalog").then((page) => ({ default: page.ModuleCatalog })));
const Networks = lazy(() => import("./pages/Networks").then((page) => ({ default: page.Networks })));
const NetworkDetail = lazy(() => import("./pages/Networks").then((page) => ({ default: page.NetworkDetail })));
const NetworkNew = lazy(() => import("./pages/Networks").then((page) => ({ default: page.NetworkNew })));
const Volumes = lazy(() => import("./pages/Volumes").then((page) => ({ default: page.Volumes })));
const VolumeDetail = lazy(() => import("./pages/Volumes").then((page) => ({ default: page.VolumeDetail })));
const Secrets = lazy(() => import("./pages/Secrets").then((page) => ({ default: page.Secrets })));
const Configs = lazy(() => import("./pages/Configs").then((page) => ({ default: page.Configs })));
const ServiceNetworks = lazy(() => import("./pages/service/Networks").then((page) => ({ default: page.ServiceNetworks })));
const ServiceVolumes = lazy(() => import("./pages/service/Volumes").then((page) => ({ default: page.ServiceVolumes })));
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
  const mobileNav = useRef<HTMLDialogElement>(null);

  useEffect(() => mobileNav.current?.close(), [location.pathname]);
  useEffect(() => {
    const compact = window.matchMedia("(max-width: 840px)");
    const resize = () => { if (!compact.matches) mobileNav.current?.close(); };
    compact.addEventListener("change", resize);
    return () => compact.removeEventListener("change", resize);
  }, []);

  if (me.isPending) return null;
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/login" state={{ from: location.pathname + location.search + location.hash }} replace />;
  }
  if (me.isError && (me.data === undefined || !retryableQueryError(me.error))) {
    return <p className="error">{me.error.message}</p>;
  }

  const logout = async () => {
    await call(api.logout());
    queryClient.clear();
    window.location.assign("/login");
  };

  const sidebar = (
    <>
      <SidebarNav role={me.data.role} modules={modules.data} pendingUpdates={pending} />
      <div className="who">
        <div className="who-account">
          <span className="who-avatar" aria-hidden="true">{me.data.username.slice(0, 1).toUpperCase()}</span>
          <div className="who-meta">
            <span className="who-name">{me.data.username}</span>
            <span className="who-role">{me.data.role}</span>
          </div>
          <Button variant="icon" aria-label="Sign out" title="Sign out" onClick={logout}>
            <Icon name="logout" size={20} />
          </Button>
        </div>
        {version.data && <div className="who-version mono">{version.data}</div>}
      </div>
    </>
  );

  return (
    <div className="app">
      <header className="bar">
        <Button variant="icon" aria-label="Menu" aria-controls="mobile-nav" aria-expanded={drawer} onClick={() => {
          mobileNav.current?.showModal();
          setDrawer(true);
        }}>
          <svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
            <path d="M3 6h18v2H3zm0 5h18v2H3zm0 5h18v2H3z" fill="currentColor" />
          </svg>
        </Button>
        <span className="brand">modularnost</span>
      </header>
      <aside className="side desktop-side">
        <span className="brand">modularnost</span>
        {sidebar}
      </aside>
      <dialog className="side-dialog" id="mobile-nav" aria-label="Navigation" ref={mobileNav} onClose={() => setDrawer(false)} onClick={(e) => {
        if (e.target === e.currentTarget || (e.target instanceof Element && e.target.closest("a"))) e.currentTarget.close();
      }}>
        <div className="side">
          <div className="side-heading">
            <span className="brand">modularnost</span>
            <Button variant="icon" aria-label="Close navigation" onClick={() => mobileNav.current?.close()}>
              <svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
                <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2" />
              </svg>
            </Button>
          </div>
          {sidebar}
        </div>
      </dialog>
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
          <Route index element={<Dashboard />} />
          <Route path="services" element={<Services />} />
          <Route path="services/:id" element={<ServiceLayout />}>
            <Route index element={<Overview />} />
            <Route path="logs" element={<Logs />} />
            <Route path="deploys" element={<ServiceDeploys />} />
            <Route path="networks" element={<ServiceNetworks />} />
            <Route path="volumes" element={<ServiceVolumes />} />
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
          <Route path="volumes" element={<Volumes />} />
          <Route path="volumes/:node/:name" element={<VolumeDetail />} />
          <Route path="secrets/:id?" element={<Secrets />} />
          <Route path="configs/:id?" element={<Configs />} />
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
