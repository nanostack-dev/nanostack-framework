-- The schema of the nanostack gen repo fixture: one column per kind the base's
-- predicates branch on, a tenant column for the scope check, and a unique
-- key for the OnUnique test. audit_events also has columns that the model
-- reads as a string but that are not text (inet, jsonb), which get no
-- Contains, and a varchar column, which does.
CREATE TYPE item_status AS ENUM ('ACTIVE', 'ARCHIVED');

CREATE TABLE tenants (
    id   uuid PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE items (
    id          text PRIMARY KEY,
    tenant_id   uuid             NOT NULL REFERENCES tenants (id),
    sku         text             NOT NULL,
    name        text             NOT NULL,
    status      item_status      NOT NULL DEFAULT 'ACTIVE',
    stock       integer          NOT NULL DEFAULT 0,
    sold        bigint           NOT NULL DEFAULT 0,
    rank        smallint         NOT NULL DEFAULT 0,
    price       double precision NOT NULL DEFAULT 0,
    weight      real,
    active      boolean          NOT NULL DEFAULT true,
    released_on date,
    local_at    timestamp,
    opens_at    time,
    created_at  timestamptz      NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    external_id text,
    tags        text[],
    CONSTRAINT items_tenant_sku_key UNIQUE (tenant_id, sku)
);

CREATE TABLE audit_events (
    id      bigserial PRIMARY KEY,
    item_id text,
    action  text        NOT NULL,
    at      timestamptz NOT NULL DEFAULT now(),
    note    varchar(200),
    client  inet,
    payload jsonb
);
