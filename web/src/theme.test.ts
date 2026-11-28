import { expect, test } from "vitest";
import { DEFAULT_SEED, themeCss } from "./theme";

test("themeCss has every role in both schemes, as hex", () => {
  const css = themeCss(DEFAULT_SEED);
  const [light, dark] = css.split("@media (prefers-color-scheme: dark)");
  for (const part of [light, dark]) {
    expect(part).toMatch(/--md-sys-color-primary:#[0-9a-f]{6};/);
    expect(part).toMatch(/--md-sys-color-surface-container-high:#[0-9a-f]{6};/);
  }
  expect(light).not.toEqual(dark);
});

test("themeCss supports explicit light and dark overrides", () => {
  const light = themeCss(DEFAULT_SEED, "light");
  const dark = themeCss(DEFAULT_SEED, "dark");
  expect(light).toContain("color-scheme:light;");
  expect(light).not.toContain("prefers-color-scheme");
  expect(dark).toContain("color-scheme:dark;");
  expect(dark).not.toContain("prefers-color-scheme");
  expect(light).not.toEqual(dark);
});
