import { expect, test } from "vitest";
import { ICONS } from "./Icon";

test("every icon is a path that starts with a moveto and closes its shapes", () => {
  for (const [name, d] of Object.entries(ICONS)) {
    expect(d, name).toMatch(/^[Mm][\d.\-]/);
    expect(d, name).toMatch(/Z$/);
  }
});
