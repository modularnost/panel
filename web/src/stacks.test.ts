import { describe, expect, it } from "vitest";
import { validStackName } from "./stacks";

describe("validStackName", () => {
  it("accepts letters, digits, dot, dash and underscore", () => {
    expect(validStackName("shop")).toBe(true);
    expect(validStackName("my-app_2.prod")).toBe(true);
  });

  it("refuses what would change the request path or read as a flag", () => {
    expect(validStackName("")).toBe(false);
    expect(validStackName("a/b")).toBe(false);
    expect(validStackName("-rf")).toBe(false);
    expect(validStackName("a b")).toBe(false);
    expect(validStackName("..")).toBe(false);
  });

  it("stops at 63 characters", () => {
    expect(validStackName("a".repeat(63))).toBe(true);
    expect(validStackName("a".repeat(64))).toBe(false);
  });
});
