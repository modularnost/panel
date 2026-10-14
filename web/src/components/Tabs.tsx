import type { ReactNode } from "react";
import { NavLink, type NavLinkProps } from "react-router";
import { Icon, type IconName } from "./Icon";

export function Tabs({ label, children }: { label: string; children: ReactNode }) {
  return (
    <nav className="tabs" aria-label={label}>
      {children}
    </nav>
  );
}

export function Tab({ icon, children, ...rest }: Omit<NavLinkProps, "className" | "children"> & { icon?: IconName; children: ReactNode }) {
  return (
    <NavLink className="tab-item" {...rest}>
      {icon && <Icon name={icon} size={18} />}
      {children}
    </NavLink>
  );
}
