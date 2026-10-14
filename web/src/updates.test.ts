import { describe, expect, it } from "vitest";
import { healthzVersion, installCommand } from "./updates";

describe("installCommand", () => {
  it("passes the image tag, which has no leading v", () => {
    expect(installCommand("v0.2.0")).toContain("PANEL_VERSION=0.2.0 sh");
    expect(installCommand("0.2.0")).toContain("PANEL_VERSION=0.2.0 sh");
  });
});

describe("healthzVersion", () => {
  it("reads the version after ok", () => {
    expect(healthzVersion("ok v0.2.0
")).toBe("v0.2.0");
    expect(healthzVersion("ok v0.1.0-4-gabc1234")).toBe("v0.1.0-4-gabc1234");
  });
});
