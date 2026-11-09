-- projects carries a hard tenant column (tenant_id NOT NULL) and a nullable
-- context tenant column (workspace_id), like echopoint's webhooks: a row
-- with no workspace belongs to the whole tenant.
CREATE TABLE projects (
    id           text PRIMARY KEY,
    tenant_id    uuid NOT NULL REFERENCES tenants (id),
    workspace_id text,
    name         text NOT NULL
);
