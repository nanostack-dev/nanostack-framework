-- notes is the table of the derived-write tests: a hard tenant column
-- (tenant_id), a NOT NULL context tenant column (workspace_id) that a
-- Create stamps, database-owned timestamps, a unique key that holds the
-- tenant (tenant_id, slug), and a case-insensitive unique index that
-- OnUnique maps.
CREATE TABLE notes (
    id           text PRIMARY KEY,
    tenant_id    uuid NOT NULL REFERENCES tenants (id),
    workspace_id text NOT NULL,
    slug         text NOT NULL,
    body         text NOT NULL,
    pinned       boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notes_tenant_slug_key UNIQUE (tenant_id, slug)
);

CREATE UNIQUE INDEX notes_tenant_lower_slug_idx ON notes (tenant_id, lower(slug));
