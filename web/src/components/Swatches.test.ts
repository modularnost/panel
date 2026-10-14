import { expect, test } from "vitest";
import { DEFAULT_SEED } from "../theme";
import { PRESETS } from "./Swatches";

test("presets are distinct lowercase hex colors and start with the default seed", () => {
  const hexes = PRESETS.map((p) => p.hex);
  expect(hexes[0]).toBe(DEFAULT_SEED);
  expect(new Set(hexes).size).toBe(hexes.length);
  for (const hex of hexes) expect(hex).toMatch(/^#[0-9a-f]{6}$/);
});
