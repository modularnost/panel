import type { ReactNode } from "react";
import { NavLink, type NavLinkProps } from "react-router";

export function Tabs({ label, children }: { label: string; children: ReactNode }) {
  return (
    <nav className="tabs" aria-label={label}>
      {children}
    </nav>
  );
}

export function Tab(props: Omit<NavLinkProps, "className">) {
  return <NavLink className="tab-item" {...props} />;
}
