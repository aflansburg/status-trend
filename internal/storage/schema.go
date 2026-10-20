package storage

// schemaDDL creates the metrics tables if they do not exist.
// Times are stored as RFC3339 UTC text, NULL when zero or invalid.
const schemaDDL = `
CREATE TABLE IF NOT EXISTS incidents (
    vendor            TEXT NOT NULL,
    incident_id       TEXT NOT NULL,
    name              TEXT NOT NULL,
    status            TEXT NOT NULL,
    impact            TEXT NOT NULL,
    region_code       TEXT NOT NULL DEFAULT '',
    shortlink         TEXT NOT NULL DEFAULT '',
    created_at        TEXT,
    started_at        TEXT,
    resolved_at       TEXT,
    monitoring_at     TEXT,
    updated_at        TEXT,
    first_recorded_at TEXT NOT NULL,
    last_recorded_at  TEXT NOT NULL,
    PRIMARY KEY (vendor, incident_id)
);

CREATE INDEX IF NOT EXISTS idx_incidents_vendor_status
    ON incidents(vendor, status);

CREATE TABLE IF NOT EXISTS fetches (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    vendor              TEXT NOT NULL,
    fetched_at          TEXT NOT NULL,
    overall_indicator   TEXT NOT NULL DEFAULT '',
    overall_description TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_fetches_vendor_time
    ON fetches(vendor, fetched_at);

CREATE TABLE IF NOT EXISTS component_status (
    fetch_id     INTEGER NOT NULL REFERENCES fetches(id) ON DELETE CASCADE,
    vendor       TEXT NOT NULL,
    component_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    status       TEXT NOT NULL,
    is_group     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (fetch_id, component_id)
);
`
