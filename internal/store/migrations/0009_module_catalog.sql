CREATE TABLE module_catalog (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    body TEXT NOT NULL,
    checked_at INTEGER NOT NULL
);
