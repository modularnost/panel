CREATE INDEX deploy_events_successful_by_target
ON deploy_events (service_name, stack_name, unixepoch(finished_at) DESC)
WHERE status = 'success' AND finished_at IS NOT NULL;
