import { renderToStaticMarkup } from "react-dom/server";
import { expect, test } from "vitest";
import { ReleaseNotes } from "./ReleaseNotes";

test("renders GitHub release headings, lists, bare links, and tables", () => {
  const html = renderToStaticMarkup(<ReleaseNotes body={`## What's Changed

* Fix the **update** screen in https://github.com/modularnost/panel/pull/180

| Version | Status |
| --- | --- |
| v1.0.0 | Ready |
`} />);

  expect(html).toContain("<h4>What&#x27;s Changed</h4>");
  expect(html).toContain("<ul>");
  expect(html).toContain("<strong>update</strong>");
  expect(html).toContain('href="https://github.com/modularnost/panel/pull/180"');
  expect(html).toContain('<div class="table"><table>');
});

test("ignores raw HTML and unsafe link schemes in release notes", () => {
  const html = renderToStaticMarkup(<ReleaseNotes body={"<img src=x onerror=example>\n\n[Unsafe](javascript:example)"} />);

  expect(html).not.toContain("<img");
  expect(html).not.toContain("javascript:");
  expect(html).toContain("Unsafe");
});
