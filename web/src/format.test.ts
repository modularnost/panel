import { describe, expect, it } from "vitest";
import { formatBytes, memoryUse, shortDigest } from "./format";

describe("formatBytes", () => {
  it("keeps whole bytes plain", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1023)).toBe("1023 B");
  });

  it("moves to the next unit at 1024", () => {
    expect(formatBytes(1024)).toBe("1.0 KiB");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(5 * 1024 ** 3)).toBe("5.0 GiB");
  });

  it("stops at the largest unit", () => {
    expect(formatBytes(2048 * 1024 ** 4)).toBe("2048.0 TiB");
  });
});

describe("shortDigest", () => {
  it("drops the algorithm and cuts to twelve characters", () => {
    expect(shortDigest("sha256:0123456789abcdef0123")).toBe("0123456789ab");
  });

  it("copes with a missing digest", () => {
    expect(shortDigest(undefined)).toBe("");
  });
});

describe("memoryUse", () => {
  it("shows the limit only when there is one", () => {
    expect(memoryUse(1024, 0)).toBe("1.0 KiB");
    expect(memoryUse(1024, 2048)).toBe("1.0 KiB / 2.0 KiB");
  });
});
