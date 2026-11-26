import { describe, expect, it } from "vitest";
import { encodeSecret, isStandaloneService, secretTarget } from "./Secrets";

describe("manual secret changes", () => {
  it("requires a known standalone service", () => {
    expect(isStandaloneService({})).toBe(true);
    expect(isStandaloneService({ stack: "" })).toBe(true);
    expect(isStandaloneService({ stack: "demo-stack" })).toBe(false);
    expect(isStandaloneService()).toBe(false);
  });
});

describe("secretTarget", () => {
  it.each([
    ["api-key", "api-key"],
    ["/run/secrets/api-key", "api-key"],
    ["", undefined],
    ["../api-key", undefined],
    ["/etc/api-key", undefined],
  ])("normalizes %j to %j", (input, expected) => {
    expect(secretTarget(input)).toBe(expected);
  });
});

describe("encodeSecret", () => {
  it("encodes UTF-8 bytes", () => {
    const bytes = new TextEncoder().encode("🔐");
    expect(encodeSecret(bytes)).toBe("8J+UkA==");
  });

  it("round-trips binary data across chunk boundaries", () => {
    const bytes = new Uint8Array(8192 * 2 + 123);
    for (let i = 0; i < bytes.length; i++) {
      bytes[i] = (i * 31 + 7) & 0xff;
    }
    const decoded = Uint8Array.from(atob(encodeSecret(bytes)), (char) => char.charCodeAt(0));
    expect(decoded).toEqual(bytes);
  });
});
