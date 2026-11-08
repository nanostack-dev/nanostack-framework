-- One table per unique-index shape that nano gen db must classify.
CREATE COLLATION case_insensitive (provider = icu, locale = 'und-u-ks-level2', deterministic = false);

CREATE TYPE user_status AS ENUM ('ACTIVE', 'DISABLED');

CREATE TABLE tenants (
    id   text PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE users (
    id          text PRIMARY KEY,
    tenant_id   text        NOT NULL REFERENCES tenants (id),
    email       text        NOT NULL,
    external_id text,
    name        text        NOT NULL,
    status      user_status NOT NULL DEFAULT 'ACTIVE',
    handle      text COLLATE case_insensitive NOT NULL,
    tags        text[],
    deleted_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_tenant_email_key UNIQUE (tenant_id, email),
    CONSTRAINT users_tenant_name_deferrable UNIQUE (tenant_id, name) DEFERRABLE INITIALLY IMMEDIATE
);

-- A key: the predicate only says a key column is not NULL.
CREATE UNIQUE INDEX idx_users_external ON users (tenant_id, external_id) WHERE external_id IS NOT NULL;
-- A key: the INCLUDE column is not part of the key.
CREATE UNIQUE INDEX idx_users_email_include ON users (email) INCLUDE (name);
-- A key: the index uses the column's own case-insensitive collation.
CREATE UNIQUE INDEX idx_users_handle ON users (tenant_id, handle);
-- Not a key: another predicate.
CREATE UNIQUE INDEX idx_users_active_name ON users (tenant_id, name) WHERE status = 'ACTIVE';
-- Not a key: IS NOT NULL and another predicate.
CREATE UNIQUE INDEX idx_users_live_external ON users (tenant_id, external_id)
    WHERE external_id IS NOT NULL AND deleted_at IS NULL;
-- Not a key: an expression.
CREATE UNIQUE INDEX idx_users_lower_email ON users (tenant_id, lower(email));
-- Not a key: "C" is unique per byte string; handle = $1 compares case-insensitively.
CREATE UNIQUE INDEX idx_users_handle_c ON users (tenant_id, handle COLLATE "C");

-- No unique index at all.
CREATE TABLE events (
    at      timestamptz NOT NULL,
    payload jsonb
);

CREATE VIEW active_users AS SELECT id, tenant_id FROM users WHERE status = 'ACTIVE';
