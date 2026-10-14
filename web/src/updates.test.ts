import { describe, expect, it } from "vitest";
import { installCommand } from "./updates";

describe("installCommand", () => {
  it("passes the image tag, which has no leading v", () => {
    expect(installCommand("v0.2.0")).toContain("PANEL_VERSION=0.2.0 sh");
    expect(installCommand("0.2.0")).toContain("PANEL_VERSION=0.2.0 sh");
  });
});
