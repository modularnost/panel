import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { call, api, retryableQueryError } from "../api/client";
import { RefreshWarning } from "../components/RefreshWarning";
import { formatTime } from "../format";
import { installCommand, updateInProgress, useUpdates } from "../updates";
import { Button } from "../components/Button";
import { useDialogs } from "../components/Dialogs";
import { Chip } from "../components/Chip";
import { Banner } from "../components/Banner";
import { Icon } from "../components/Icon";
import { ReleaseNotes } from "./ReleaseNotes";

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
      message: "The panel restarts during the update. Back up the database volume first if you may need to roll back after a migration.",
      action: "Update",
    });
    if (ok) apply.mutate(tag);
  };
  const rollbackToPrevious = async () => {
    const ok = await confirm({
      title: "Roll back to previous version?",
      message: `The panel and agents will be restored to ${last?.original_image || "the previous image"}. Restore the database separately if required after a migration.`,
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
    return <Banner tone="error">{updates.error.message}</Banner>;
  }
  const u = updates.data;
  const newest = u.releases[0];
  const last = u.last_update;
  const busy = apply.isPending || rollback.isPending || updateInProgress(last?.state);

  return (
    <>
      <RefreshWarning error={updates.error} />
      <div className="settings-row">
        <div>
          <p className="settings-version">{u.current}</p>
          <p className="muted">Installed version{u.checked_at && ` · Checked ${formatTime(u.checked_at)}`}</p>
        </div>
        {u.enabled && u.release_build && (
          <Button variant="tonal" onClick={() => check.mutate()} disabled={check.isPending}>
            <Icon name="refresh" size={18} />
            {check.isPending ? "Checking…" : "Check now"}
          </Button>
        )}
      </div>
      {check.isError && <Banner tone="error">{check.error.message}</Banner>}
      {u.error && !check.isError && <Banner tone="error">The last check failed: {u.error}</Banner>}
      {!u.enabled && <p className="muted">The release check is off (PANEL_UPDATE_CHECK=off).</p>}
      {u.enabled && !u.release_build && (
        <p className="muted">Automatic updates are available for release builds.</p>
      )}
      {u.enabled && u.release_build && !newest && !u.error && u.checked_at && <Chip tone="ok">Up to date</Chip>}
      {apply.isError && <Banner tone="error">{apply.error.message}</Banner>}
      {rollback.isError && <Banner tone="error">{rollback.error.message}</Banner>}
      {newest && (
        <div className="settings-row settings-update">
          <div>
            <h3>Update available</h3>
            <div className="settings-inline">
              <span className="settings-version">{newest.tag}</span>
              {newest.prerelease && <Chip>Pre-release</Chip>}
            </div>
          </div>
          <Button onClick={() => update(newest.tag)} disabled={busy}>
            <Icon name="arrow_downward" size={18} />
            {apply.isPending ? "Starting update…" : `Update to ${newest.tag}`}
          </Button>
        </div>
      )}
      {last && (
        <div className="settings-attempt">
          <div className="settings-row">
            <div role="status">
              <h3>Last update · {last.version}</h3>
              <p className="muted">{rolloutMessages[last.state]}</p>
            </div>
            {last.can_rollback && (
              <Button variant="outlined" onClick={rollbackToPrevious} disabled={busy}>
                {rollback.isPending ? "Rolling back…" : "Roll back"}
              </Button>
            )}
          </div>
          {last.backup && (
            <details className="settings-disclosure">
              <summary><Icon name="chevron_right" size={18} /> Database backup</summary>
              <p className="settings-disclosure-body"><code>{last.backup}</code></p>
            </details>
          )}
        </div>
      )}
      {newest && (
        <>
          {!u.stack_file && (
            <Banner tone="warning">
              The stack file will still point to the old image after the update.
              Run the installer on the Swarm manager to add the missing mount.
            </Banner>
          )}
          <details className="settings-disclosure">
            <summary><Icon name="chevron_right" size={18} /> Manual update</summary>
            <div className="settings-disclosure-body">
              <p className="muted">
                Back up the database volume before updating. Rolling back after a migration requires restoring that backup.
              </p>
              <p>Run on the Swarm manager:</p>
              <pre><code>{installCommand(newest.tag)}</code></pre>
            </div>
          </details>
          <section className="settings-releases" aria-labelledby="settings-release-notes">
            <h3 id="settings-release-notes">Release notes</h3>
            {u.releases.map((r) => (
              <details className="settings-disclosure" key={r.tag}>
                <summary>
                  <Icon name="chevron_right" size={18} />
                  <span className="settings-inline">
                    <strong>{r.tag}</strong>
                    {r.prerelease && <Chip>Pre-release</Chip>}
                    <time className="muted" dateTime={r.published_at}>{formatTime(r.published_at)}</time>
                  </span>
                </summary>
                <div className="settings-disclosure-body">
                  <ReleaseNotes body={r.body} />
                  <a href={r.url} target="_blank" rel="noreferrer">View release on GitHub</a>
                </div>
              </details>
            ))}
          </section>
        </>
      )}
    </>
  );
}
