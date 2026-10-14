import { expect, test } from "vitest";
import { choicesOf } from "./Select";

test("choicesOf reads options, fragments and mapped lists", () => {
  const choices = choicesOf(
    <>
      <option value="">All</option>
      {[1, 2].map((n) => (
        <option key={n} value={n}>
          {n} lines
        </option>
      ))}
      <option>plain</option>
    </>,
  );
  expect(choices).toEqual([
    { value: "", label: "All" },
    { value: "1", label: "1 lines" },
    { value: "2", label: "2 lines" },
    { value: "plain", label: "plain" },
  ]);
});
