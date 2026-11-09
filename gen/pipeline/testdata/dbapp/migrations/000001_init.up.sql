CREATE TABLE tenants (
    id   text PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE users (
    id          text NOT NULL PRIMARY KEY,
    tenant_id   text NOT NULL REFERENCES tenants (id),
    email       text NOT NULL,
    external_id text,
    CONSTRAINT users_tenant_email_key UNIQUE (tenant_id, email)
);
