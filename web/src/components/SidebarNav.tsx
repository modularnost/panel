import { useEffect, useState, type ReactNode } from "react";
import { NavLink, useLocation } from "react-router";
import type { Module } from "../api/generated";
import { atLeast } from "../auth";
import { modulePath, slot } from "../modules";
import { Chip } from "./Chip";
import { Icon, type IconName } from "./Icon";

type NavItem = {
  to: string;
  label: string;
  icon: IconName;
  admin?: boolean;
  end?: boolean;
};

const EVERYDAY_ITEMS: NavItem[] = [
  { to: "/", label: "Overview", icon: "info", end: true },
  { to: "/services", label: "Services", icon: "dns" },
  { to: "/stacks", label: "Stacks", icon: "layers" },
  { to: "/deploys", label: "Deploys", icon: "rocket_launch" },
];

const CLUSTER_ITEMS: NavItem[] = [
  { to: "/resources", label: "Resources", icon: "monitoring" },
  { to: "/nodes", label: "Nodes", icon: "hub", admin: true },
  { to: "/networks", label: "Networks", icon: "lan" },
  { to: "/volumes", label: "Volumes", icon: "hard_drive" },
  { to: "/secrets", label: "Secrets", icon: "lock", admin: true },
  { to: "/configs", label: "Configs", icon: "description", admin: true },
  { to: "/ports", label: "Ports", icon: "dns", admin: true },
];

const SETTINGS_ITEMS: NavItem[] = [
  { to: "/settings", label: "Settings", icon: "settings" },
  { to: "/webhooks", label: "Webhooks", icon: "webhook" },
  { to: "/users", label: "Users", icon: "group" },
];

function matchesPrefix(pathname: string, prefix: string): boolean {
  return pathname === prefix || pathname.startsWith(`${prefix}/`);
}

function NavGroup({
  active,
  pathname,
  icon,
  label,
  badge,
  children,
}: {
  active: boolean;
  pathname: string;
  icon: IconName;
  label: string;
  badge?: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(active);

  // Open on navigation into a deep link while preserving manual toggle state otherwise.
  useEffect(() => {
    if (active) setOpen(true);
  }, [active, pathname]);

  return (
    <details
      className="side-group"
      data-active={active || undefined}
      open={open}
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary className="item group-trigger">
        <Icon name={icon} />
        <span>{label}</span>
        {badge}
        <span className="group-chevron" aria-hidden="true">
          <Icon name="chevron_right" size={18} />
        </span>
      </summary>
      <div className="group-links">{children}</div>
    </details>
  );
}

export function SidebarNav({
  role,
  modules,
  pendingUpdates,
}: {
  role: string | undefined;
  modules: Module[] | undefined;
  pendingUpdates: number;
}) {
  const { pathname } = useLocation();
  const isAdmin = atLeast(role, "admin");
  const clusterItems = CLUSTER_ITEMS.filter((item) => !item.admin || isAdmin);
  const navModules = slot(modules, "nav", role);

  const clusterActive = clusterItems.some((item) => matchesPrefix(pathname, item.to));
  const modulesActive =
    matchesPrefix(pathname, "/modules") ||
    matchesPrefix(pathname, "/install-module") ||
    matchesPrefix(pathname, "/module");
  const settingsActive = SETTINGS_ITEMS.some((item) => matchesPrefix(pathname, item.to));

  return (
    <nav className="side-nav" aria-label="Primary">
      {EVERYDAY_ITEMS.map((item) => (
        <NavLink key={item.to} to={item.to} end={item.end} className="item">
          <Icon name={item.icon} />
          {item.label}
        </NavLink>
      ))}

      <NavGroup active={clusterActive} pathname={pathname} icon="hub" label="Cluster">
        {clusterItems.map((item) => (
          <NavLink key={item.to} to={item.to} className="item sub">
            <Icon name={item.icon} size={20} />
            {item.label}
          </NavLink>
        ))}
      </NavGroup>

      <NavGroup active={modulesActive} pathname={pathname} icon="extension" label="Modules">
        <NavLink to="/modules" className="item sub">
          <Icon name="extension" size={20} />
          All modules
        </NavLink>
        {navModules.map(({ module, contribution }) => (
          <NavLink
            key={`${module}${contribution.path}`}
            to={modulePath(module, contribution.path)}
            className="item sub"
            end
          >
            <Icon name="extension" size={20} />
            {contribution.title}
          </NavLink>
        ))}
      </NavGroup>

      {isAdmin ? (
        <NavGroup
          active={settingsActive}
          pathname={pathname}
          icon="settings"
          label="Settings"
          badge={pendingUpdates > 0 ? <Chip tone="ok">{pendingUpdates}</Chip> : undefined}
        >
          {SETTINGS_ITEMS.map((item) => (
            <NavLink key={item.to} to={item.to} className="item sub">
              <Icon name={item.icon} size={20} />
              {item.label}
            </NavLink>
          ))}
        </NavGroup>
      ) : (
        <NavLink to="/settings" className="item">
          <Icon name="settings" />
          Settings
        </NavLink>
      )}
    </nav>
  );
}
