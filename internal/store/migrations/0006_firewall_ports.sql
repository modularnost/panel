-- Ports that are open to everyone, besides SSH, 80 and 443. The installer's
-- list of what listened before the install is copied in once; after that this
-- table is the only place they are kept.
CREATE TABLE firewall_ports (
  port       INTEGER NOT NULL,
  proto      TEXT NOT NULL,
  note       TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (port, proto)
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
