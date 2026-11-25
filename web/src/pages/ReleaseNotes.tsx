import type { ReactNode } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

const components = {
  h1: "h4",
  h2: "h4",
  h3: "h5",
  h4: "h5",
  h5: "h6",
  table: ({ children }: { children?: ReactNode }) => (
    <div className="table"><table>{children}</table></div>
  ),
  img: ({ alt, src }: { alt?: string; src?: string }) => (
    <a href={src || undefined}>{alt || "View image"}</a>
  ),
} as const;

export function ReleaseNotes({ body }: { body: string }) {
  return (
    <div className="release-notes">
      <Markdown components={components} remarkPlugins={[remarkGfm]} skipHtml>
        {body.trim() ? body : "No release notes."}
      </Markdown>
    </div>
  );
}
