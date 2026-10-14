import type { ReactNode } from "react";

export function Banner({ tone = "info", children }: { tone?: "info" | "warning" | "error"; children: ReactNode }) {
  return (
    <div className={`banner ${tone}`} role="status">
      {children}
    </div>
  );
}
