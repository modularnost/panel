import type { TableHTMLAttributes } from "react";

// The wrapper scrolls on a narrow screen, so the page does not and the table keeps its layout.
export function Table({ scrollLabel, ...props }: TableHTMLAttributes<HTMLTableElement> & { scrollLabel?: string }) {
  return (
    <div className="table" role={scrollLabel ? "region" : undefined} aria-label={scrollLabel} tabIndex={scrollLabel ? 0 : undefined}>
      <table {...props} />
    </div>
  );
}
