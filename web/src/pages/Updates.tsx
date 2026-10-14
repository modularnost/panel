import { useMutation, useQueryClient } from "@tanstack/react-query";
import { call, api } from "../api/client";
import { formatTime } from "../format";
import { installCommand, useUpdates } from "../updates";

export function Updates() {
  const queryClient = useQueryClient();
  const updates = useUpdates(true);
  const check = useMutation({
    mutationFn: () => call(api.checkUpdates()),
    onSuccess: (fresh) => queryClient.setQueryData(["updates"], fresh),
  });

  if (updates.isPending) return <p>Loading…</p>;
  if (updates.isError) return <p className="error">{updates.error.message}</p>;
  const u = updates.data;
  const newest = u.releases[0];

  return (
    <>
      <h1>Updates</h1>
      <p>
        Running <span className="mono">{u.current}</span>
        {u.checked_at && <span className="muted"> · checked {formatTime(u.checked_at)}</span>}{" "}
        {u.enabled && u.release_build && (
          <button onClick={() => check.mutate()} disabled={check.isPending}>
            {check.isPending ? "Checking…" : "Check now"}
          </button>
        )}
      </p>
      {check.isError && <p className="error">{check.error.message}</p>}
      {u.error && <p className="error">The last check failed: {u.error}</p>}
      {!u.enabled && <p className="muted">The release check is off (PANEL_UPDATE_CHECK=off).</p>}
      {u.enabled && !u.release_build && (
        <p className="muted">This is not a release build, so there is nothing to update to from here.</p>
      )}
      {u.enabled && u.release_build && !newest && !u.error && <p>You are on the latest release.</p>}

      {newest && (
        <>
          <p>To update to {newest.tag}, run this on the manager:</p>
          <pre className="mono">{installCommand(newest.tag)}</pre>
          <p className="muted">
            Migrations only go forward: take a copy of the database volume first if you may need to roll back.
          </p>
          {u.releases.map((r) => (
            <section key={r.tag}>
              <h2>
                <a href={r.url} target="_blank" rel="noreferrer">
                  {r.tag}
                </a>{" "}
                {r.prerelease && <span className="pill">pre-release</span>}{" "}
                <span className="muted">{formatTime(r.published_at)}</span>
              </h2>
              <pre className="notes">{r.body || "No release notes."}</pre>
            </section>
          ))}
        </>
      )}
    </>
  );
}
