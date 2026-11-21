import { useState } from "react";
import { Button } from "./Button";
import { Icon } from "./Icon";

type SortValue = string | number | undefined;

export function sortRows<T extends { name: string }>(
  rows: T[], value: (row: T) => SortValue, descending: boolean,
): T[] {
  return [...rows].sort((a, b) => {
    const left = value(a);
    const right = value(b);
    const byName = a.name.localeCompare(b.name);
    if (left === undefined) return right === undefined ? byName : 1;
    if (right === undefined) return -1;
    const order = typeof left === "number" && typeof right === "number"
      ? left - right : String(left).localeCompare(String(right));
    return (descending ? -order : order) || byName;
  });
}

export function useTableSort<T extends { name: string }>(columns: Record<string, (row: T) => SortValue>) {
  const [sort, setSort] = useState({ key: "name", descending: false });
  return {
    rows: (items: T[]) => sortRows(items, columns[sort.key], sort.descending),
    header: (key: string, label: string) => {
      const active = sort.key === key;
      return (
        <th className="sort-header" scope="col" aria-sort={active ? (sort.descending ? "descending" : "ascending") : undefined}>
          <Button type="button" variant="text" onClick={() => setSort({ key, descending: active && !sort.descending })}>
            <span>{label}</span>
            <span className="sort-arrows">
              <Icon name="arrow_downward" size={16} />
              <Icon name="arrow_upward" size={16} />
            </span>
          </Button>
        </th>
      );
    },
  };
}
