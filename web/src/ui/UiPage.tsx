import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { Link, useSearchParams } from "react-router";
import { api, call } from "../api/client";
import { Banner } from "../components/Banner";
import { Chip, type Tone } from "../components/Chip";
import { Table } from "../components/Table";
import { ButtonBlock, FormBlock } from "./Action";
import { Chart, CursorProvider, type ChartBlock } from "./Chart";
import { type Block, formatUnix, formatValue, groupStats, linkTarget } from "./document";

// queryFor is what the module is asked: what the panel's own URL carries (a
// chart's range), with the context of a tab on top, which the URL cannot set.
export function queryFor(context: string, search: URLSearchParams): string {
  const q = new URLSearchParams(search);
  for (const [k, v] of new URLSearchParams(context)) q.set(k, v);
  return q.toString();
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const list = (v: unknown): Record<string, unknown>[] =>
  Array.isArray(v) ? v.filter((x): x is Record<string, unknown> => typeof x === "object" && x !== null) : [];

// UiPage draws a module's document. It asks again when the module says the page
// changed, and when the query (a chart's range) does.
export function UiPage({ module, path, context = "" }: { module: string; path: string; context?: string }) {
  const [search] = useSearchParams();
  const queryClient = useQueryClient();
  const query = queryFor(context, search);

  const page = useQuery({
    queryKey: ["ui", module, path, query],
    queryFn: () => call(api.getModulePage(module, { path, query })),
    placeholderData: keepPreviousData,
    retry: 1,
  });

  useEffect(() => {
    const events = new EventSource(`/api/v1/modules/${encodeURIComponent(module)}/events?path=${encodeURIComponent(path)}`);
    const refetch = () => queryClient.invalidateQueries({ queryKey: ["ui", module, path] });
    // The first open has just been fetched; a later one is a reconnect, and
    // events may have been missed in between.
    let opened = false;
    events.onopen = () => {
      if (opened) refetch();
      opened = true;
    };
    events.addEventListener("changed", refetch);
    return () => events.close();
  }, [module, path, queryClient]);

  if (page.isPending) return <p>Loading…</p>;
  if (page.isError) return <p className="error">{page.error.message}</p>;

  return (
    <CursorProvider>
      <div className="ui-page">
        {groupStats(page.data.blocks as Block[]).map((b, i) =>
          Array.isArray(b) ? (
            <div className="stats" key={i}>
              {b.map((s, j) => (
                <Stat key={j} block={s} />
              ))}
            </div>
          ) : (
            <BlockView key={i} block={b} module={module} page={path} />
          ),
        )}
      </div>
    </CursorProvider>
  );
}

function BlockView({ block, module, page }: { block: Block; module: string; page: string }) {
  switch (block.type) {
    case "heading":
      return block.level === 1 ? <h2>{str(block.text)}</h2> : <h3>{str(block.text)}</h3>;
    case "text":
      return <p className={block.tone === "muted" ? "muted" : undefined}>{str(block.text)}</p>;
    case "banner":
      return <Banner tone={block.tone === "warning" || block.tone === "error" ? block.tone : "info"}>{str(block.text)}</Banner>;
    case "facts":
      return (
        <dl className="facts">
          {list(block.items).map((it, i) => (
            <div key={i} className="fact">
              <dt>{str(it.label)}</dt>
              <dd>{formatValue(it.value, str(it.unit))}</dd>
            </div>
          ))}
        </dl>
      );
    case "table":
      return <TableBlock block={block} module={module} />;
    case "meter":
      return <Meter block={block} />;
    case "chart":
      return <Chart block={block as ChartBlock} />;
    case "choice":
      return <Choice block={block} />;
    case "button":
      return <ButtonBlock block={block} module={module} page={page} />;
    case "form":
      return <FormBlock block={block} module={module} page={page} />;
    case "link":
      return (
        <p>
          <Target href={block.href} module={module}>
            {str(block.text)}
          </Target>
        </p>
      );
    default:
      return <p className="muted">This page has a block this panel does not know: {block.type}.</p>;
  }
}

function Stat({ block }: { block: Block }) {
  return (
    <div className="stat">
      <span className="stat-label">{str(block.label)}</span>
      <span className="stat-value">{formatValue(block.value, str(block.unit))}</span>
      {block.caption !== undefined && <span className="muted">{str(block.caption)}</span>}
    </div>
  );
}

function Meter({ block }: { block: Block }) {
  const value = Number(block.value);
  const max = Number(block.max);
  const share = max > 0 && Number.isFinite(value) ? Math.min(1, Math.max(0, value / max)) : 0;
  const unit = str(block.unit);
  return (
    <div className="meter-row">
      <span>{str(block.label)}</span>
      <div
        className={`meter${block.tone === "bad" ? " bad" : ""}`}
        role="meter"
        aria-label={str(block.label)}
        aria-valuemin={0}
        aria-valuemax={max}
        aria-valuenow={value}
      >
        <span style={{ width: `${share * 100}%` }} />
      </div>
      <span className="muted">
        {formatValue(value, unit)} / {formatValue(max, unit)}
      </span>
    </div>
  );
}

function Choice({ block }: { block: Block }) {
  const [, setSearch] = useSearchParams();
  const param = str(block.param);
  const current = str(block.value);
  if (!param) return null;
  return (
    <div className="segmented" role="radiogroup" aria-label={param}>
      {list(block.options).map((o) => (
        <label key={str(o.value)} className={str(o.value) === current ? "selected" : undefined}>
          <input
            type="radio"
            name={`choice-${param}`}
            checked={str(o.value) === current}
            onChange={() =>
              setSearch((prev) => {
                const next = new URLSearchParams(prev);
                next.set(param, str(o.value));
                return next;
              })
            }
          />
          {str(o.label) || str(o.value)}
        </label>
      ))}
    </div>
  );
}

function Target({ href, module, children }: { href: unknown; module: string; children: React.ReactNode }) {
  const target = linkTarget(href, module);
  if (!target) return <>{children}</>;
  return target.kind === "route" ? (
    <Link to={target.to}>{children}</Link>
  ) : (
    <a href={target.url} target="_blank" rel="noreferrer noopener">
      {children}
    </a>
  );
}

const NUMERIC = ["number", "bytes", "percent", "time"];

function cellText(value: unknown, type: string): string {
  switch (type) {
    case "bytes":
      return formatValue(value, "bytes");
    case "percent":
      return formatValue(value, "percent");
    case "time":
      return typeof value === "number" ? formatUnix(value) : "–";
    case "number":
      return formatValue(value);
    default:
      return value === null || value === undefined ? "" : String(value);
  }
}

function TableBlock({ block, module }: { block: Block; module: string }) {
  const columns = list(block.columns);
  const rows = list(block.rows);
  if (rows.length === 0) return <p className="muted">{str(block.empty) || "Nothing here."}</p>;
  return (
    <Table>
      <thead>
        <tr>
          {columns.map((c, i) => (
            <th key={i} className={NUMERIC.includes(str(c.type)) || c.align === "right" ? "num" : undefined}>
              {str(c.title)}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, r) => (
          <tr key={r}>
            {columns.map((c, i) => {
              const type = str(c.type);
              const raw = row[str(c.key)];
              const cell = typeof raw === "object" && raw !== null ? (raw as Record<string, unknown>) : { text: raw };
              const tone: Tone = cell.tone === "ok" || cell.tone === "bad" ? cell.tone : "";
              const text = cellText(cell.text, type);
              return (
                <td key={i} className={NUMERIC.includes(type) || c.align === "right" ? "num" : undefined}>
                  <Target href={cell.href} module={module}>
                    {type === "chip" || tone ? <Chip tone={tone}>{text}</Chip> : text}
                  </Target>
                </td>
              );
            })}
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
