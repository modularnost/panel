import { describe, expect, it } from "vitest";
import { envLines, modulePath, moduleURL } from "./modules";

describe("moduleURL", () => {
  it("joins the module name and its path with one slash", () => {
    expect(moduleURL("metrics", "/")).toBe("/modules/metrics/");
    expect(moduleURL("metrics", "page")).toBe("/modules/metrics/page");
    expect(moduleURL("metrics", "//a/b")).toBe("/modules/metrics/a/b");
  });
});

describe("modulePath", () => {
  it("is the framing route, not the proxied URL", () => {
    expect(modulePath("metrics", "/page")).toBe("/module/metrics/page");
  });
});

describe("envLines", () => {
  it("trims entries and drops blank lines", () => {
    expect(envLines("A=1\n\n  B=two  \n")).toEqual(["A=1", "B=two"]);
  });

  it("is empty for empty input", () => {
    expect(envLines("")).toEqual([]);
  });
});
