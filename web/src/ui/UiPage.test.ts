import { expect, it } from "vitest";
import { queryFor } from "./UiPage";

it("asks the module what the URL carries, with the tab's context on top of it", () => {
  expect(queryFor("", new URLSearchParams("range=24h"))).toBe("range=24h");
  expect(queryFor("?service=shop_web&id=abc", new URLSearchParams("range=24h"))).toBe("range=24h&service=shop_web&id=abc");
  // A tab's context is the panel's: the address bar cannot say which service it is on.
  expect(queryFor("?service=shop_web", new URLSearchParams("service=other"))).toBe("service=shop_web");
  expect(queryFor("", new URLSearchParams())).toBe("");
});
