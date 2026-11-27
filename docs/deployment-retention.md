# Deployment history retention proposal

Keep completed deployment details for 90 days after `finished_at` in UTC.
This gives operators a quarter of history for investigating failures and
comparing rollouts. Keep pending rows until the deployment finishes. Handle
abandoned pending rows separately after defining how to distinguish them from
deployments still in progress.

## Last successful deployment

Preserve the time of the latest successful deployment for each service name
and each stack name even after the corresponding event ages out. Preserve the
current meaning of these timestamps. A successful direct service deployment
updates the service timestamp, even if the service belongs to a stack. A
successful stack-wide deployment updates the stack timestamp. The time shown
for a service is the newer of its direct deployment and its current stack's
deployment, when available. Failed and pending events advance neither timestamp.

Before any pruning, backfill these timestamps from the full history and verify
that the last-deployment values match the existing query. Update the timestamp
in the same SQLite transaction as a successful event, keeping the greatest
completion time when deployments finish out of order. The retained timestamp
is a summary. The history list contains only detailed events still available.

## Backup and upgrade

Roll out the summary and backfill before shipping automatic deletion. An
upgrade must preserve all existing detailed events, and pruning should start
only when an administrator explicitly enables it for that database. Provide
a preview count, take a consistent SQLite backup before the first prune, and
keep that backup in a location outside the panel's automatic update backup
rotation. A restore from before the prune recovers the event details present
in that backup. Use a panel version compatible with the restored schema.

The first cleanup implementation should delete completed rows in bounded
transactions. Verify an upgrade from an existing database, backups and restore,
preservation of service and stack timestamps, expiry at the cutoff, and writes
that overlap pruning before enabling it on a live database.
