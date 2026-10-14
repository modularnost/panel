import { describe, expect, it } from "vitest";
import { atLeast } from "./auth";

describe("atLeast", () => {
  it("orders viewer below operator below admin", () => {
    expect(atLeast("viewer", "viewer")).toBe(true);
    expect(atLeast("viewer", "operator")).toBe(false);
    expect(atLeast("operator", "operator")).toBe(true);
    expect(atLeast("operator", "admin")).toBe(false);
    expect(atLeast("admin", "viewer")).toBe(true);
  });

  it("treats an unknown or missing role as no rights", () => {
    expect(atLeast(undefined, "viewer")).toBe(false);
    expect(atLeast("root", "viewer")).toBe(false);
  });
});
