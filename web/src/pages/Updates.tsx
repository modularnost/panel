import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { call, api } from "../api/client";
import { formatTime } from "../format";
import { installCommand, useUpdates, waitForVersion } from "../updates";
import { Button } from "../components/Button";
import { Chip } from "../components/Chip";

type Progress = "idle" | "waiting" | "late";

export function Updates() {
  const queryClient = useQueryClient();
  const updates = useUpdates(true);
  const [progress, setProgress] = useState<Progress>("idle");
  const apply = useMutation({
    mutationFn: (version: string) => call(api.applyUpdate({ version })),
    onSuccess: async (started) => {
      setProgress("waiting");
      if (await waitForVersion(started.version)) {
        window.location.reload();
      } else {
        setProgress("late");
      }
    },
  });
  const update = (tag: string) => {
    if (window.confirm(`Update the panel to ${tag}? It is unavailable for a few seconds while it restarts.`)) {
      apply.mutate(tag);
    }
  };
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
          <Button onClick={() => check.mutate()} disabled={check.isPending}>
            {check.isPending ? "Checking…" : "Check now"}
          </Button>
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
          <p>
            <Button onClick={() => update(newest.tag)} disabled={apply.isPending || progress !== "idle"}>
              Update to {newest.tag}
            </Button>
          </p>
          {apply.isError && <p className="error">{apply.error.message}</p>}
          {apply.isSuccess && (
            <p className="muted">
              A copy of the database is at <span className="mono">{apply.data.backup}</span> on the panel's volume.
            </p>
          )}
          {!u.stack_file && (
            <p className="muted">
              The installer's stack file is not mounted into the panel, so it will still name the old image after the
              update: run the installer once to add the mount.
            </p>
          )}
          {progress === "waiting" && <p>Updating… this page reloads when {newest.tag} answers.</p>}
          {progress === "late" && (
            <p className="error">
              {newest.tag} did not answer within three minutes. Swarm rolls a failing update back on its own; look at{" "}
              <span className="mono">docker service ps --no-trunc</span> for the panel service.
            </p>
          )}
          <p className="muted">Or on the manager:</p>
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
                {r.prerelease && <Chip>pre-release</Chip>}{" "}
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
