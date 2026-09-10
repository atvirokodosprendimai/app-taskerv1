-- +goose Up
-- +goose StatementBegin

-- Accounts. Registration is open: anyone with an e-mail address may create one.
-- The auth service stores the address already lower-cased, so UNIQUE is the
-- whole "one account per address" rule and no collation has to agree with it.
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    email         TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    -- The IANA zone captured from the browser at registration. History day,
    -- month and year boundaries are computed in it, so "today" is the user's
    -- today rather than the server's.
    timezone      TEXT    NOT NULL DEFAULT 'UTC',
    created_at    INTEGER NOT NULL
) STRICT;

-- Session records for alexedwards/scs. The column names and types are the
-- store's, not ours.
CREATE TABLE sessions (
    token  TEXT PRIMARY KEY,
    data   BLOB NOT NULL,
    expiry REAL NOT NULL
);
CREATE INDEX sessions_expiry_idx ON sessions (expiry);

-- The companies a person tracks time for. Each belongs to exactly one user.
CREATE TABLE companies (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    -- The parent key of the composite foreign key on time_entries below.
    UNIQUE (id, user_id)
) STRICT;
-- One company per name per user; NOCASE so "acme" and "Acme" are one company.
CREATE UNIQUE INDEX companies_user_name_idx ON companies (user_id, name COLLATE NOCASE);

-- One row per timer. A running timer has no stopped_at, and several may run at
-- once. Times are unix seconds, UTC.
CREATE TABLE time_entries (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL,
    company_id INTEGER NOT NULL,
    task       TEXT    NOT NULL DEFAULT '',
    started_at INTEGER NOT NULL,
    stopped_at INTEGER,
    CHECK (stopped_at IS NULL OR stopped_at >= started_at),
    -- Composite on purpose: an entry's owner must be its company's owner. The
    -- tracking service already writes it that way; this makes the database
    -- refuse any other write path that does not.
    FOREIGN KEY (company_id, user_id) REFERENCES companies (id, user_id) ON DELETE CASCADE
) STRICT;
-- The running-timers list.
CREATE INDEX time_entries_running_idx ON time_entries (user_id, started_at) WHERE stopped_at IS NULL;
-- History: a user's entries overlapping a period.
CREATE INDEX time_entries_user_started_idx ON time_entries (user_id, started_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE time_entries;
DROP TABLE companies;
DROP INDEX IF EXISTS sessions_expiry_idx;
DROP TABLE sessions;
DROP TABLE users;
-- +goose StatementEnd
