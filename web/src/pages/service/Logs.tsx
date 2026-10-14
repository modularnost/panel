import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { api, call } from "../../api/client";
import { MAX_LINES, capLines, parseLine } from "../../logs";
import { useService } from "./Layout";
import { Select } from "../../components/Select";
import { Button } from "../../components/Button";

const TAILS = [100, 200, 500, 1000];

export function Logs() {
  const s = useService();
  const [task, setTask] = useState("");
  const [tail, setTail] = useState(200);
  const [paused, setPaused] = useState(false);
  const [view, setView] = useState<string[]>([]);
  const [problem, setProblem] = useState("");
  const [connected, setConnected] = useState(false);

  const buffer = useRef<string[]>([]);
  const pausedRef = useRef(false);
  const box = useRef<HTMLPreElement>(null);
  const stick = useRef(true);
  pausedRef.current = paused;

  const tasks = useQuery({
    queryKey: ["tasks", s.id],
    queryFn: () => call(api.listServiceTasks(s.id)),
    refetchInterval: 10_000,
  });

  useEffect(() => {
    buffer.current = [];
    setView([]);
    setProblem("");
    setConnected(false);

    const params = new URLSearchParams({ tail: String(tail) });
    if (task) params.set("task", task);
    const source = new EventSource(`/api/v1/services/${s.id}/logs?${params}`);

    source.onopen = () => setConnected(true);
    source.addEventListener("line", (e) => {
      buffer.current = capLines([...buffer.current, parseLine((e as MessageEvent).data)]);
    });
    source.addEventListener("problem", (e) => {
      try {
        setProblem(JSON.parse((e as MessageEvent).data).error.message);
      } catch {
        setProblem("The log stream failed.");
      }
      source.close();
      setConnected(false);
    });
    source.onerror = () => {
      // A refused request closes the stream for good; a dropped connection is
      // retried by the browser, so only a closed source is worth reporting.
      if (source.readyState === EventSource.CLOSED) {
        setProblem((p) => p || "Could not open the log stream.");
        setConnected(false);
      }
    };

    // Lines arrive in bursts; painting a few times a second is plenty.
    const paint = setInterval(() => {
      if (!pausedRef.current) setView([...buffer.current]);
    }, 250);

    return () => {
      clearInterval(paint);
      source.close();
    };
  }, [s.id, task, tail]);

  useEffect(() => {
    const el = box.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [view]);

  const onScroll = () => {
    const el = box.current;
    if (el) stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
  };

  return (
    <>
      <div className="toolbar">
        <label>
          Replica
          <Select value={task} onChange={(v) => setTask(v)}>
            <option value="">All</option>
            {tasks.data?.map((t) => (
              <option key={t.id} value={t.id}>
                {t.slot ? `#${t.slot}` : t.id.slice(0, 8)} · {t.state}
              </option>
            ))}
          </Select>
        </label>
        <label>
          History
          <Select value={tail} onChange={(v) => setTail(Number(v))}>
            {TAILS.map((n) => (
              <option key={n} value={n}>
                {n} lines
              </option>
            ))}
          </Select>
        </label>
        <Button variant="tonal" onClick={() => setPaused((p) => !p)}>{paused ? "Resume" : "Pause"}</Button>
        <span className="muted">{connected ? "live" : problem ? "stopped" : "connecting…"}</span>
      </div>
      {problem && <p className="error">{problem}</p>}
      <pre className="logbox" ref={box} onScroll={onScroll}>
        {view.length === 0 && !problem ? "Waiting for output…" : view.join("\n")}
      </pre>
      <p className="muted">Showing the last {MAX_LINES} lines at most.</p>
    </>
  );
}
