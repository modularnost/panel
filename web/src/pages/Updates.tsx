import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { call, api, retryableQueryError } from "../api/client";
import { RefreshWarning } from "../components/RefreshWarning";
import { formatTime } from "../format";
import { installCommand, updateInProgress, useUpdates } from "../updates";
import { Button } from "../components/Button";
import { useDialogs } from "../components/Dialogs";
import { Chip } from "../components/Chip";

const rolloutMessages = {
  requested: "Update requested. Waiting for Swarm.",
  updating: "Swarm is replacing the panel task.",
  completed: "Update completed. The new version is running.",
  failed: "The update could not be submitted. Check the panel logs.",
  paused: "Swarm paused the update. Check the panel service tasks.",
  rolling_back: "Swarm is rolling back the update.",
  rollback_paused: "Swarm paused the rollback. Check the panel service tasks and the database backup.",
  rolled_back: "Swarm rolled back the service. Check the database before retrying.",
  unknown: "The update result could not be confirmed. Check the panel service tasks.",
};

export function Updates() {
  const queryClient = useQueryClient();
  const updates = useUpdates(true, true);
  const initialVersion = useRef<string | undefined>(undefined);
  const current = updates.data?.current;
  useEffect(() => {
    if (current === undefined) return;
    if (initialVersion.current !== undefined && initialVersion.current !== current) window.location.reload();
    initialVersion.current = current;
  }, [current]);
  const apply = useMutation({
    mutationFn: (version: string) => call(api.applyUpdate({ version })),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ["updates"] }),
  });
  const rollback = useMutation({
    mutationFn: () => call(api.rollbackUpdate()),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ["updates"] }),
  });
  const { confirm } = useDialogs();
  const update = async (tag: string) => {
    const ok = await confirm({
      title: `Update the panel to ${tag}?`,
      message: "It is unavailable for a few seconds while it restarts.",
      action: "Update",
    });
    if (ok) apply.mutate(tag);
  };
  const rollbackToPrevious = async () => {
    const ok = await confirm({
      title: "Roll back to previous version?",
      message: `The panel and agents will be restored to ${last?.original_image || "the previous image"}.`,
      action: "Roll back",
    });
    if (ok) rollback.mutate();
  };
  const check = useMutation({
    mutationFn: () => call(api.checkUpdates()),
    onSuccess: (fresh) => queryClient.setQueryData(["updates"], fresh),
  });

  if (updates.isPending) return <p>Loading…</p>;
  if (updates.isError && (updates.data === undefined || !retryableQueryError(updates.error))) {
    return <p className="error">{updates.error.message}</p>;
  }
  const u = updates.data;
  const newest = u.releases[0];
  const last = u.last_update;

  return (
    <>
      <RefreshWarning error={updates.error} />
      <h2>Updates</h2>
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
      <div role="status">
        {last && <p><strong>{last.version}</strong> · {rolloutMessages[last.state]}</p>}
      </div>
      {last?.backup && (
        <p className="muted">
          Database backup on the panel volume: <span className="mono">{last.backup}</span>
        </p>
      )}
      {apply.isError && <p className="error">{apply.error.message}</p>}
      {rollback.isError && <p className="error">{rollback.error.message}</p>}
      {last?.can_rollback && (
        <p>
          <Button
            onClick={rollbackToPrevious}
            disabled={rollback.isPending || apply.isPending || updateInProgress(last.state)}
          >
            {rollback.isPending ? "Rolling back…" : "Roll back to previous version"}
          </Button>
        </p>
      )}

      {newest && (
        <>
          <p>
            <Button onClick={() => update(newest.tag)} disabled={apply.isPending || updateInProgress(last?.state)}>
              Update to {newest.tag}
            </Button>
          </p>
          {!u.stack_file && (
            <p className="muted">
              The installer's stack file is not mounted into the panel, so it will still name the old image after the
              update: run the installer once to add the mount.
            </p>
          )}
          <p className="muted">Or on the manager:</p>
          <pre className="mono">{installCommand(newest.tag)}</pre>
          <p className="muted">
            Migrations only go forward: take a copy of the database volume first if you may need to roll back.
          </p>
          {u.releases.map((r) => (
            <section key={r.tag}>
              <h3>
                <a href={r.url} target="_blank" rel="noreferrer">
                  {r.tag}
                </a>{" "}
                {r.prerelease && <Chip>pre-release</Chip>}{" "}
                <span className="muted">{formatTime(r.published_at)}</span>
              </h3>
              <pre className="notes">{r.body || "No release notes."}</pre>
            </section>
          ))}
        </>
      )}
    </>
  );
}
