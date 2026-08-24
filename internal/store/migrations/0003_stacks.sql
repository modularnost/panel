-- Compose files. Swarm does not keep them: `docker stack deploy` is a
-- client-side concept, the daemon only ever sees the resulting services.
-- Storing the file is what makes a real redeploy (and editing) possible.
CREATE TABLE stacks (
  name       TEXT PRIMARY KEY,
  compose    TEXT NOT NULL,
  updated_by TEXT NOT NULL DEFAULT '',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
