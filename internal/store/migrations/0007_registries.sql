CREATE TABLE registries (
  host TEXT PRIMARY KEY,
  username TEXT NOT NULL,
  encrypted_token BLOB NOT NULL
);

ALTER TABLE stacks ADD COLUMN registry_host TEXT NOT NULL DEFAULT '';
