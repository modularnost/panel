import { formatBytes } from "../format";
import { modulePath } from "../modules";

export type Block = { type: string } & Record<string, unknown>;
export type Point = [number, number];

// formatValue is how the panel shows a number a module sent raw. A string is
// shown as it is: the module may have a better word for it than a number.
export function formatValue(value: unknown, unit?: string): string {
  if (typeof value === "string") return value;
  if (typeof value !== "number" || !Number.isFinite(value)) return "–";
  switch (unit) {
    case "percent":
      return `${value.toFixed(1)} %`;
    case "bytes":
      return formatBytes(value);
    case "bytes/s":
      return `${formatBytes(value)}/s`;
    case "seconds":
      return `${value} s`;
    default:
      return value.toLocaleString();
  }
}

// formatUnix shows unix seconds in the viewer's own time zone: the module runs
// in whatever zone its container has, the reader does not.
export function formatUnix(seconds: number): string {
  return new Date(seconds * 1000).toLocaleString([], {
    day: "2-digit",
    month: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export type Target = { kind: "route"; to: string } | { kind: "external"; url: string };

// linkTarget turns what a module wrote as a link into where the panel will go,
// or nothing. Only three shapes are links; a module's own string never becomes
// an href of its own.
export function linkTarget(href: unknown, module: string): Target | null {
  if (typeof href !== "object" || href === null) return null;
  const h = href as Record<string, unknown>;

  if (typeof h.path === "string") {
    const [pathname] = h.path.split("?");
    if (!h.path.startsWith("/") || pathname.split("/").includes("..")) return null;
    return { kind: "route", to: modulePath(module, h.path) };
  }
  if (typeof h.service === "string" && h.service !== "") {
    return { kind: "route", to: `/services/${encodeURIComponent(h.service)}` };
  }
  if (typeof h.url === "string") {
    try {
      const u = new URL(h.url);
      if (u.protocol === "https:") return { kind: "external", url: u.href };
    } catch {
      // not a URL
    }
  }
  return null;
}

// chartTop is the top of the y axis: the largest value with a little room, so a
// peak is not glued to the frame.
export function chartTop(series: { points: Point[] }[]): number {
  let max = 0;
  for (const s of series) for (const [, v] of s.points) if (v > max) max = v;
  return max > 0 ? max * 1.15 : 1;
}

// nearest is the point of a series closest in time to t.
export function nearest(points: Point[], t: number): Point | undefined {
  let best: Point | undefined;
  for (const p of points) if (best === undefined || Math.abs(p[0] - t) < Math.abs(best[0] - t)) best = p;
  return best;
}

// groupStats lets a run of stat blocks sit in one row instead of one per line.
export function groupStats(blocks: Block[]): (Block | Block[])[] {
  const out: (Block | Block[])[] = [];
  for (const b of blocks) {
    const last = out[out.length - 1];
    if (b.type === "stat" && Array.isArray(last)) last.push(b);
    else if (b.type === "stat") out.push([b]);
    else out.push(b);
  }
  return out;
}
