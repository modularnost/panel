CREATE TABLE webhooks (
  id               INTEGER PRIMARY KEY,
  service_name     TEXT NOT NULL DEFAULT '',
  stack_name       TEXT NOT NULL DEFAULT '',
  secret_token     TEXT NOT NULL UNIQUE,
  action_type      TEXT NOT NULL CHECK (action_type IN ('force_update','stack_redeploy')),
  image_pattern    TEXT NOT NULL DEFAULT '',
  created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_triggered_at DATETIME
);

CREATE TABLE deploy_events (
  id                INTEGER PRIMARY KEY,
  service_name      TEXT NOT NULL DEFAULT '',
  stack_name        TEXT NOT NULL DEFAULT '',
  trigger_source    TEXT NOT NULL,
  old_image_digest  TEXT NOT NULL DEFAULT '',
  new_image_digest  TEXT NOT NULL DEFAULT '',
  status            TEXT NOT NULL CHECK (status IN ('pending','success','failed')),
  triggered_by      TEXT NOT NULL DEFAULT '',
  started_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at       DATETIME,
  error_message     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX deploy_events_started ON deploy_events (started_at DESC);
