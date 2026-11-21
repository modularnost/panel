import { expect, test } from "vitest";
import { sortRows } from "./TableSort";

test("sorts dates or counts by value, leaving unknowns last and ties by name", () => {
  const rows = [{ name: "z", n: 2 }, { name: "b" }, { name: "a", n: 2 }, { name: "c", n: 10 }];
  expect(sortRows(rows, (row) => row.n, false).map((row) => row.name)).toEqual(["a", "z", "c", "b"]);
  expect(sortRows(rows, (row) => row.n, true).map((row) => row.name)).toEqual(["c", "a", "z", "b"]);
});
