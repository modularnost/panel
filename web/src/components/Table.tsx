import type { TableHTMLAttributes } from "react";

// The wrapper scrolls on a narrow screen, so the page does not and the table keeps its layout.
export function Table(props: TableHTMLAttributes<HTMLTableElement>) {
  return (
    <div className="table">
      <table {...props} />
    </div>
  );
}
