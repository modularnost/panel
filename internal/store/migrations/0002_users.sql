CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL CHECK (role IN ('admin','operator','viewer')),
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- One table for both browser sessions and CI tokens: same mechanism, the only
-- difference is who created it and how long it lives.
CREATE TABLE api_tokens (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL DEFAULT '',
  token_hash TEXT NOT NULL UNIQUE,
  role       TEXT NOT NULL DEFAULT '' CHECK (role IN ('','admin','operator','viewer')),
  expires_at DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX api_tokens_user ON api_tokens (user_id);
