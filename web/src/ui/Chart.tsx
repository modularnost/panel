import { createContext, useCallback, useContext, useEffect, useRef, useState, type PointerEvent, type ReactNode } from "react";
import { chartTop, formatUnix, formatValue, nearest, type Point } from "./document";

type Series = { name: string; points: Point[] };

const H = 170;
const PAD = { left: 62, right: 8, top: 10, bottom: 20 };
const PLOT_H = H - PAD.top - PAD.bottom;

const COLORS = [
  "var(--md-sys-color-primary)",
  "var(--md-sys-color-tertiary)",
  "var(--md-sys-color-secondary)",
  "var(--md-sys-color-error)",
  "var(--ok)",
  "var(--md-sys-color-outline)",
  "var(--md-sys-color-inverse-primary)",
  "var(--md-sys-color-on-surface-variant)",
];

type Cursors = { at: Record<string, number | null>; set: (key: string, t: number | null) => void };
const CursorContext = createContext<Cursors | null>(null);

// CursorProvider lets charts with the same sync key show one hovered time.
export function CursorProvider({ children }: { children: ReactNode }) {
  const [at, setAt] = useState<Record<string, number | null>>({});
  const set = useCallback((key: string, t: number | null) => setAt((a) => (a[key] === t ? a : { ...a, [key]: t })), []);
  return <CursorContext.Provider value={{ at, set }}>{children}</CursorContext.Provider>;
}

// seriesOf keeps what is drawable: a module's document is trusted for its text, not for its shape.
function seriesOf(raw: unknown): Series[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((s, i) => {
    const o = (s ?? {}) as { name?: unknown; points?: unknown };
    const points = Array.isArray(o.points)
      ? o.points.filter((p): p is Point => Array.isArray(p) && Number.isFinite(p[0]) && Number.isFinite(p[1]))
      : [];
    return { name: typeof o.name === "string" ? o.name : `series ${i + 1}`, points };
  });
}

export type ChartBlock = {
  title?: unknown;
  unit?: unknown;
  from?: unknown;
  to?: unknown;
  series?: unknown;
  sync?: unknown;
  empty?: unknown;
};

export function Chart({ block }: { block: ChartBlock }) {
  const ctx = useContext(CursorContext);
  const [local, setLocal] = useState<number | null>(null);
  // Drawn in real pixels, so the text is not stretched on a narrow screen.
  const wrap = useRef<HTMLDivElement>(null);
  const [W, setW] = useState(760);
  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const watch = new ResizeObserver(([e]) => setW(Math.max(280, Math.round(e.contentRect.width))));
    watch.observe(el);
    return () => watch.disconnect();
  }, []);
  const plotW = W - PAD.left - PAD.right;
  const key = typeof block.sync === "string" ? block.sync : "";
  const at = key && ctx ? (ctx.at[key] ?? null) : local;
  const setAt = (t: number | null) => (key && ctx ? ctx.set(key, t) : setLocal(t));

  const unit = typeof block.unit === "string" ? block.unit : undefined;
  const series = seriesOf(block.series);
  const all = series.flatMap((s) => s.points);
  const from = Number.isFinite(block.from) ? (block.from as number) : Math.min(...all.map((p) => p[0]));
  const to = Number.isFinite(block.to) ? (block.to as number) : Math.max(...all.map((p) => p[0]));
  const span = Math.max(to - from, 1);
  const top = chartTop(series);
  const x = (t: number) => PAD.left + (plotW * (t - from)) / span;
  const y = (v: number) => PAD.top + PLOT_H * (1 - v / top);
  const title = typeof block.title === "string" ? block.title : "";

  const move = (e: PointerEvent<SVGSVGElement>) => {
    const box = e.currentTarget.getBoundingClientRect();
    const px = ((e.clientX - box.left) / box.width) * W;
    setAt(Math.min(to, Math.max(from, from + ((px - PAD.left) / plotW) * span)));
  };

  // The cursor snaps to a sampled time, so every series is read at the same moment.
  const snapped = at === null ? undefined : nearest(all, at)?.[0];
  const reading =
    snapped === undefined
      ? []
      : series.flatMap((s, i) => {
          const p = nearest(s.points, snapped);
          return p ? [{ name: s.name, value: p[1], color: COLORS[i % COLORS.length] }] : [];
        });
  const label =
    title + (series.length === 1 && all.length ? `: ${formatValue(all[all.length - 1][1], unit)} now` : "");

  return (
    <figure className="chart-box">
      {title && <figcaption>{title}</figcaption>}
      <div className="chart-wrap" ref={wrap}>
        {all.length === 0 ? (
          <p className="muted">{typeof block.empty === "string" ? block.empty : "No data in this range."}</p>
        ) : (
          <>
          <svg
            className="chart"
            width={W}
            height={H}
            role="img"
            aria-label={label}
            onPointerMove={move}
            onPointerLeave={() => setAt(null)}
          >
            {[0, 0.5, 1].map((f) => (
              <g key={f}>
                <line className="grid" x1={PAD.left} x2={W - PAD.right} y1={y(top * f)} y2={y(top * f)} />
                <text className="tick" x={PAD.left - 6} y={y(top * f) + 4} textAnchor="end">
                  {formatValue(top * f, unit)}
                </text>
              </g>
            ))}
            {[from, from + span / 2, to].map((t, i) => (
              <text key={i} className="tick" x={x(t)} y={H - 6} textAnchor={(["start", "middle", "end"] as const)[i]}>
                {formatUnix(t)}
              </text>
            ))}
            {series.map((s, i) => {
              if (s.points.length === 0) return null;
              const color = COLORS[i % COLORS.length];
              const line = s.points.map(([t, v], j) => `${j ? "L" : "M"}${x(t).toFixed(1)},${y(v).toFixed(1)}`).join("");
              const first = s.points[0][0];
              const last = s.points[s.points.length - 1][0];
              return (
                <g key={i} style={{ color }}>
                  {series.length === 1 && (
                    <path className="area" d={`${line}L${x(last).toFixed(1)},${y(0)}L${x(first).toFixed(1)},${y(0)}Z`} />
                  )}
                  <path className="line" d={line} />
                </g>
              );
            })}
            {snapped !== undefined && (
              <g className="cursor">
                <line x1={x(snapped)} x2={x(snapped)} y1={PAD.top} y2={PAD.top + PLOT_H} />
                {reading.map((r) => (
                  <circle key={r.name} cx={x(snapped)} cy={y(r.value)} r={3.5} style={{ fill: r.color }} />
                ))}
              </g>
            )}
          </svg>
          {snapped !== undefined && (
            <div
              className={`chart-tip${x(snapped) > W * 0.6 ? " flip" : ""}`}
              style={{ left: `${(x(snapped) / W) * 100}%` }}
              aria-hidden="true"
            >
              <small>{formatUnix(snapped)}</small>
              {reading.map((r) => (
                <div key={r.name}>
                  {series.length > 1 && <span className="swatch-dot" style={{ background: r.color }} />}
                  {series.length > 1 ? `${r.name}: ` : ""}
                  {formatValue(r.value, unit)}
                </div>
              ))}
            </div>
          )}
          </>
        )}
      </div>
      {series.length > 1 && (
        <ul className="legend">
          {series.map((s, i) => (
            <li key={i}>
              <span className="swatch-dot" style={{ background: COLORS[i % COLORS.length] }} />
              {s.name}
            </li>
          ))}
        </ul>
      )}
    </figure>
  );
}
