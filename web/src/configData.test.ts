import { describe, expect, it } from "vitest";
import { configTarget, readConfigData } from "./configData";

describe("config targets", () => {
  it.each(["/app.conf", "/etc/app/config file.json", "/.app.conf"])("accepts %s", (target) => {
    expect(configTarget(target)).toBe(true);
  });
  it.each(["", "/", "app.conf", "/etc//app.conf", "/etc/../app.conf", "/etc/./app.conf",
    "/etc/app.conf/", "/etc/a:b", "/etc/a\\b", "/app\nconf", "/app\0conf"])("rejects %j", (target) => {
    expect(configTarget(target)).toBe(false);
  });
});

describe("config contents", () => {
  it("decodes UTF-8 text and preserves a BOM for replacement", () => {
    const config = readConfigData("77u/0YQK");
    expect(config.text).toBe("\uFEFFф\n");
  });
  it("keeps binary content available as bytes", () => {
    expect(readConfigData("AGH/")).toEqual({ bytes: Uint8Array.of(0, 97, 255), text: undefined });
    expect(readConfigData("/w==").text).toBeUndefined();
  });
  it("rejects values outside the JSON base64 contract", () => {
    expect(() => readConfigData({})).toThrow();
    expect(() => readConfigData("invalid!")).toThrow();
  });
});
