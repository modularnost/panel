import { describe, expect, it } from "vitest";
import { chartTop, formatValue, formDefaults, formFields, groupStats, linkTarget, nearest, strings } from "./document";

describe("formatValue", () => {
  it("formats a raw number by its unit", () => {
    expect(formatValue(3.14159, "percent")).toBe("3.1 %");
    expect(formatValue(1536, "bytes")).toBe("1.5 KiB");
    expect(formatValue(2048, "bytes/s")).toBe("2.0 KiB/s");
    expect(formatValue(30, "seconds")).toBe("30 s");
  });

  it("keeps a string as the module wrote it, and dashes what is not a number", () => {
    expect(formatValue("2 × 4 GiB", "bytes")).toBe("2 × 4 GiB");
    expect(formatValue(null)).toBe("–");
    expect(formatValue(Number.NaN, "percent")).toBe("–");
    expect(formatValue(undefined)).toBe("–");
  });
});

describe("linkTarget", () => {
  it("opens a page of the module inside the panel", () => {
    expect(linkTarget({ path: "/service?name=x" }, "metrics")).toEqual({
      kind: "route",
      to: "/module/metrics/service?name=x",
    });
  });

  it("opens a service by name", () => {
    expect(linkTarget({ service: "shop_web" }, "m")).toEqual({ kind: "route", to: "/services/shop_web" });
    expect(linkTarget({ service: "a b/c" }, "m")).toEqual({ kind: "route", to: "/services/a%20b%2Fc" });
  });

  it("allows https and nothing else outside the panel", () => {
    expect(linkTarget({ url: "https://example.com/x" }, "m")).toEqual({ kind: "external", url: "https://example.com/x" });
    for (const url of ["http://example.com", "javascript:alert(1)", "data:text/html,x", "//evil.example", "nope", ""]) {
      expect(linkTarget({ url }, "m")).toBeNull();
    }
  });

  it("refuses a path that climbs, one that is not a path and anything that is not an object", () => {
    expect(linkTarget({ path: "/a/../b" }, "m")).toBeNull();
    expect(linkTarget({ path: "a" }, "m")).toBeNull();
    expect(linkTarget({ service: "" }, "m")).toBeNull();
    expect(linkTarget("https://example.com", "m")).toBeNull();
    expect(linkTarget(null, "m")).toBeNull();
    expect(linkTarget({ unknown: "kind" }, "m")).toBeNull();
  });
});

describe("chartTop and nearest", () => {
  it("leaves room above the peak, and has a top for an empty chart", () => {
    expect(chartTop([{ points: [[1, 10], [2, 20]] }, { points: [[1, 5]] }])).toBeCloseTo(23);
    expect(chartTop([{ points: [] }])).toBe(1);
    expect(chartTop([{ points: [[1, 0]] }])).toBe(1);
  });

  it("finds the point closest in time", () => {
    const pts: [number, number][] = [[10, 1], [20, 2], [30, 3]];
    expect(nearest(pts, 22)).toEqual([20, 2]);
    expect(nearest(pts, 100)).toEqual([30, 3]);
    expect(nearest([], 5)).toBeUndefined();
  });
});

describe("groupStats", () => {
  it("puts a run of stats in one group and leaves the rest alone", () => {
    const text = { type: "text" };
    const a = { type: "stat" };
    const b = { type: "stat" };
    expect(groupStats([text, a, b, text, a])).toEqual([text, [a, b], text, [a]]);
  });
});

describe("forms", () => {
  it("starts a form from what the module said, and leaves out what cannot be sent", () => {
    const fields = formFields([
      { name: "tag", label: "Tag", value: "v1" },
      { name: "mode", type: "select", options: [{ value: "a" }, { value: "b", label: "B" }] },
      { name: "force", type: "switch", value: true },
      { name: "quiet", type: "switch" },
      { label: "no name" },
      "junk",
    ]);
    expect(formDefaults(fields)).toEqual({ tag: "v1", mode: "a", force: "true", quiet: "false" });
    expect(fields[1].options[0].label).toBe("a");
  });

  it("sends only strings for a button", () => {
    expect(strings({ a: "1", b: 2, c: null })).toEqual({ a: "1" });
    expect(strings("x")).toEqual({});
  });
});
