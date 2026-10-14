import { describe, expect, it } from "vitest";
import { capLines, parseLine } from "./logs";

describe("parseLine", () => {
  it("takes the text of a line event", () => {
    expect(parseLine('{"text":"2026-10-14T03:00:00Z [1] started"}')).toBe("2026-10-14T03:00:00Z [1] started");
  });

  it("shows what it cannot parse instead of dropping it", () => {
    expect(parseLine("plain text")).toBe("plain text");
    expect(parseLine('{"other":1}')).toBe('{"other":1}');
  });
});

describe("capLines", () => {
  it("keeps the newest lines", () => {
    expect(capLines(["a", "b", "c", "d"], 2)).toEqual(["c", "d"]);
  });

  it("leaves a short list alone", () => {
    const lines = ["a", "b"];
    expect(capLines(lines, 5)).toBe(lines);
  });
});
