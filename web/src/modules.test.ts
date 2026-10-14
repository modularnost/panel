import { describe, expect, it } from "vitest";
import type { Module, Service } from "./api/generated";
import { envLines, matchesLabels, modulePath, moduleURL, serviceContext, serviceTabs, slot, uiContribution } from "./modules";

const mod = (name: string, ok: boolean, contributions: Module["contributions"]): Module => ({
  name,
  ok,
  contributions,
  permissions: [],
  service: name,
  state: ok ? "ready" : "starting",
  version: "1",
});
const tab = (title: string, role: "viewer" | "operator" | "admin", match: Record<string, string> = {}) => ({
  slot: "service.tab" as const,
  title,
  path: "/service",
  role,
  match,
  render: "frame" as const,
});
const svc = (labels?: Record<string, string>, stack?: string): Service =>
  ({ id: "abc", name: "shop_web", stack, labels, image: "", mode: "replicated", running: 1, desired: 1, healthy: true }) as Service;

describe("slot and serviceTabs", () => {
  const modules = [
    mod("metrics", true, [tab("Metrics", "viewer")]),
    mod("build", true, [tab("Build", "admin", { "build.module": "on" })]),
    mod("down", false, [tab("Hidden", "viewer")]),
  ];

  it("hides what the role cannot use, and modules that are not ready", () => {
    expect(slot(modules, "service.tab", "viewer").map((s) => s.contribution.title)).toEqual(["Metrics"]);
    expect(slot(modules, "service.tab", "admin").map((s) => s.contribution.title)).toEqual(["Metrics", "Build"]);
    expect(slot(modules, "service.tab", undefined)).toEqual([]);
  });

  it("shows a tab only on the services that carry its labels", () => {
    expect(serviceTabs(modules, "admin", svc()).map((s) => s.contribution.title)).toEqual(["Metrics"]);
    expect(serviceTabs(modules, "admin", svc({ "build.module": "on" })).map((s) => s.contribution.title)).toEqual([
      "Metrics",
      "Build",
    ]);
    expect(serviceTabs(modules, "admin", svc({ "build.module": "off" })).length).toBe(1);
  });
});

describe("matchesLabels", () => {
  it("is true for an empty match and needs every label", () => {
    expect(matchesLabels({}, undefined)).toBe(true);
    expect(matchesLabels({ a: "1", b: "2" }, { a: "1" })).toBe(false);
    expect(matchesLabels({ a: "1" }, { a: "1", c: "3" })).toBe(true);
  });
});

describe("serviceContext", () => {
  it("names the service, its id and its stack", () => {
    expect(serviceContext(svc(undefined, "shop"))).toBe("?service=shop_web&id=abc&stack=shop");
    expect(serviceContext(svc())).toBe("?service=shop_web&id=abc");
  });
});

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

describe("uiContribution", () => {
  const page = (path: string, render: "frame" | "ui") => ({ ...tab("T", "viewer"), path, render });
  const modules = [mod("metrics", true, [page("/", "ui"), page("/service/", "ui"), page("/old", "frame")])];

  it("finds the page the module declared as a document, ignoring slashes at the ends", () => {
    expect(uiContribution(modules, "metrics", "")?.path).toBe("/");
    expect(uiContribution(modules, "metrics", "/")?.path).toBe("/");
    expect(uiContribution(modules, "metrics", "service")?.path).toBe("/service/");
    expect(uiContribution(modules, "metrics", "/service/")?.path).toBe("/service/");
  });

  it("leaves a frame, a path nobody declared and another module to the frame", () => {
    expect(uiContribution(modules, "metrics", "old")).toBeUndefined();
    expect(uiContribution(modules, "metrics", "service/deep")).toBeUndefined();
    expect(uiContribution(modules, "other", "")).toBeUndefined();
    expect(uiContribution(undefined, "metrics", "")).toBeUndefined();
  });
});