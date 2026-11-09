-- Tables for the search tenant checks and the generated search method.
-- labels holds rows shared by every tenant (tenant_id NULL) next to the
-- rows of one tenant; label_uses is a child with no tenant column, for
-- FilterExists; item_licenses is LEFT-joined with the tenant in the ON
-- clause; item_events is counted by a computed sort key; boards carries a
-- context tenant column (workspace_id) only. audit_events reaches a tenant
-- through the item it names.
ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_item_id_fkey FOREIGN KEY (item_id) REFERENCES items (id);

CREATE TABLE labels (
    id        text PRIMARY KEY,
    tenant_id uuid REFERENCES tenants (id),
    name      text NOT NULL
);

CREATE TABLE label_uses (
    id       bigserial PRIMARY KEY,
    label_id text NOT NULL REFERENCES labels (id),
    kind     text NOT NULL
);

CREATE TABLE item_licenses (
    item_id   text PRIMARY KEY REFERENCES items (id),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    plan      text NOT NULL
);

CREATE TABLE item_events (
    id      bigserial PRIMARY KEY,
    item_id text        NOT NULL REFERENCES items (id),
    at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE boards (
    id           text PRIMARY KEY,
    workspace_id text NOT NULL,
    title        text NOT NULL
);
