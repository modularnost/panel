import type { ReactNode } from "react";

export type Tone = "ok" | "bad" | "";

export function Chip({ tone = "", children }: { tone?: Tone; children: ReactNode }) {
  return <span className={`chip ${tone}`.trim()}>{children}</span>;
}
