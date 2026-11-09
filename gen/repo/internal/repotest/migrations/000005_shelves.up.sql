-- shelves has two hard tenant columns (tenant_id and store_id, both in
-- Postgres.Scopes) and a context one (workspace_id), like anchor's tables
-- under platform_tenant_id, product_id and organization_id. Its key
-- (store_id, name) holds one hard scope only: an upsert guards the other.
CREATE TABLE shelves (
    id           text PRIMARY KEY,
    tenant_id    uuid NOT NULL REFERENCES tenants (id),
    store_id     text NOT NULL,
    workspace_id text NOT NULL,
    name         text NOT NULL,
    capacity     integer NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT shelves_store_name_key UNIQUE (store_id, name)
);
