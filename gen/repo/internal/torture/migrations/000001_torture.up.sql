-- The torture fixture of nanostack gen repo: strange tables and columns, to
-- run every derived method on PostgreSQL. Tenant columns are org_id,
-- product_id and site_id (hard scopes) and workspace_id (a context scope).
CREATE EXTENSION IF NOT EXISTS citext;
CREATE TYPE mood AS ENUM ('HAPPY', 'SAD', 'needs-quote');

-- No scope column: an unscoped table. A bigint identity key, a nullable
-- unique column, reserved words as table and column names.
CREATE TABLE "group" (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text    NOT NULL,
    name_key   text,
    slug       text UNIQUE,
    "count"    integer NOT NULL DEFAULT 0,
    "by"       text,
    "and"      text,
    is_internal boolean NOT NULL DEFAULT false,
    deleted_at timestamptz
);

-- One hard scope (org_id). Partial and expression unique indexes, which are
-- not keys; a unique key on a nullable column; a composite key; a quoted
-- mixed-case column; a column named internal.
CREATE TABLE "user" (
    id           uuid PRIMARY KEY,
    org_id       uuid    NOT NULL,
    email        text    NOT NULL,
    handle       text    NOT NULL,
    external_id  text,
    name         text    NOT NULL,
    name_key     text    NOT NULL,
    internal     boolean NOT NULL DEFAULT false,
    "MixedCase"  text,
    deleted_at   timestamptz,
    UNIQUE (org_id, external_id),
    UNIQUE (org_id, name, name_key)
);
CREATE UNIQUE INDEX user_handle_live ON "user" (org_id, handle) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX user_email_lower ON "user" (org_id, lower(email));

-- Two hard scopes and a nullable context scope. Columns whose names are
-- reserved words or contain And, Internal, Order.
CREATE TABLE "order" (
    id              uuid PRIMARY KEY,
    org_id          uuid    NOT NULL,
    product_id      uuid    NOT NULL,
    workspace_id    text,
    "by"            text    NOT NULL,
    "and"           text    NOT NULL,
    "count"         integer NOT NULL DEFAULT 0,
    internal        boolean NOT NULL DEFAULT false,
    order_by        text    NOT NULL,
    is_internal     boolean NOT NULL DEFAULT false,
    brand_and_model text    NOT NULL,
    id_and_name     text    NOT NULL,
    name            text    NOT NULL,
    name_key        text    NOT NULL,
    deleted_at      timestamptz,
    UNIQUE (org_id, product_id, "by"),
    UNIQUE (org_id, product_id, workspace_id, name)
);

-- Three hard scopes and a NOT NULL context scope, all in the primary key.
CREATE TABLE ledger (
    org_id       uuid   NOT NULL,
    product_id   uuid   NOT NULL,
    site_id      uuid   NOT NULL,
    workspace_id text   NOT NULL,
    seq          bigint NOT NULL,
    memo         text   NOT NULL,
    deleted_at   timestamptz,
    PRIMARY KEY (org_id, product_id, site_id, workspace_id, seq)
);

-- Only a context scope, NOT NULL.
CREATE TABLE note (
    id           text PRIMARY KEY,
    workspace_id text NOT NULL,
    title        text NOT NULL
);

-- Only a context scope, nullable.
CREATE TABLE draft (
    id           text PRIMARY KEY,
    workspace_id text,
    title        text NOT NULL
);

-- A tenant column that is nullable and not a context scope: the table has no
-- NOT NULL scope column, so the scope rule does not apply to it.
CREATE TABLE shared_doc (
    id     text PRIMARY KEY,
    org_id uuid,
    title  text NOT NULL
);

-- A scope column and no key at all.
CREATE TABLE log_line (
    org_id uuid NOT NULL,
    msg    text NOT NULL
);

-- One column of each type, and a nullable twin.
CREATE TABLE kitchen (
    id        uuid PRIMARY KEY,
    org_id    uuid NOT NULL,
    c_uuid    uuid             NOT NULL,
    c_text    text             NOT NULL,
    c_varchar varchar(8)       NOT NULL,
    c_citext  citext           NOT NULL,
    c_small   smallint         NOT NULL,
    c_int     integer          NOT NULL,
    c_big     bigint           NOT NULL,
    c_real    real             NOT NULL,
    c_double  double precision NOT NULL,
    c_bool    boolean          NOT NULL,
    c_mood    mood             NOT NULL,
    c_inet    inet             NOT NULL,
    c_json    jsonb            NOT NULL,
    c_bytes   bytea            NOT NULL,
    c_date    date             NOT NULL,
    c_ts      timestamp        NOT NULL,
    c_tstz    timestamptz      NOT NULL,
    c_time    time             NOT NULL,
    c_timetz  timetz           NOT NULL,
    c_texts   text[]           NOT NULL,
    c_ints    integer[]        NOT NULL,
    n_uuid    uuid,
    n_text    text,
    n_varchar varchar(8),
    n_citext  citext,
    n_small   smallint,
    n_int     integer,
    n_big     bigint,
    n_real    real,
    n_double  double precision,
    n_bool    boolean,
    n_mood    mood,
    n_inet    inet,
    n_json    jsonb,
    n_bytes   bytea,
    n_date    date,
    n_ts      timestamp,
    n_tstz    timestamptz,
    n_time    time,
    n_timetz  timetz,
    n_texts   text[],
    n_ints    integer[]
);

-- Two context scopes (workspace_id nullable, region_id NOT NULL) and a hard one.
CREATE TABLE zone (
    id           text PRIMARY KEY,
    org_id       uuid NOT NULL,
    workspace_id text,
    region_id    text NOT NULL,
    name         text NOT NULL
);

-- A NOT NULL and a nullable hard scope on one table.
CREATE TABLE mixed (
    id         text PRIMARY KEY,
    org_id     uuid NOT NULL,
    product_id uuid,
    name       text NOT NULL
);

-- Products and sites: the tables that prove, with a foreign key, that a site
-- has one product and a product one org (Postgres.ScopeImplies).
CREATE TABLE tproduct (
    id     uuid PRIMARY KEY,
    org_id uuid NOT NULL,
    UNIQUE (id, org_id)
);
CREATE TABLE tsite (
    id         uuid PRIMARY KEY,
    product_id uuid NOT NULL,
    UNIQUE (id, product_id)
);

-- Three hard scopes that determine each other: the foreign keys prove that a
-- site belongs to one product, which belongs to one org (Postgres.ScopeImplies
-- site_id -> product_id -> org_id).
CREATE TABLE bin (
    id         text PRIMARY KEY,
    org_id     uuid NOT NULL,
    product_id uuid NOT NULL,
    site_id    uuid NOT NULL,
    name       text NOT NULL,
    FOREIGN KEY (product_id, org_id) REFERENCES tproduct (id, org_id),
    FOREIGN KEY (site_id, product_id) REFERENCES tsite (id, product_id)
);

-- Two hard scopes with no foreign key between them: Postgres.ScopeImplies
-- says product_id -> org_id, and this table does not prove it, so a name
-- must give both.
CREATE TABLE shelf (
    id         text PRIMARY KEY,
    org_id     uuid NOT NULL,
    product_id uuid NOT NULL,
    name       text NOT NULL
);

-- A join table: the tenant column is in its composite key.
CREATE TABLE member (
    org_id   uuid   NOT NULL,
    group_id bigint NOT NULL,
    user_id  uuid   NOT NULL,
    role     text   NOT NULL,
    PRIMARY KEY (org_id, group_id, user_id)
);

-- Writes: hard scopes org_id and product_id and the context scope workspace_id
-- (stamped on a new row), every column type of the kitchen as NOT NULL and as
-- nullable, and the columns the database writes (Postgres.DatabaseColumns).
CREATE TABLE wide (
    id           uuid PRIMARY KEY,
    org_id       uuid        NOT NULL,
    product_id   uuid        NOT NULL,
    workspace_id text        NOT NULL,
    slug         text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    c_uuid    uuid             NOT NULL,
    c_text    text             NOT NULL,
    c_varchar varchar(8)       NOT NULL,
    c_citext  citext           NOT NULL,
    c_small   smallint         NOT NULL,
    c_int     integer          NOT NULL,
    c_big     bigint           NOT NULL,
    c_real    real             NOT NULL,
    c_double  double precision NOT NULL,
    c_bool    boolean          NOT NULL,
    c_mood    mood             NOT NULL,
    c_inet    inet             NOT NULL,
    c_json    jsonb            NOT NULL,
    c_bytes   bytea            NOT NULL,
    c_date    date             NOT NULL,
    c_ts      timestamp        NOT NULL,
    c_tstz    timestamptz      NOT NULL,
    c_time    time             NOT NULL,
    c_timetz  timetz           NOT NULL,
    c_texts   text[]           NOT NULL,
    c_ints    integer[]        NOT NULL,
    n_uuid    uuid,
    n_text    text,
    n_varchar varchar(8),
    n_citext  citext,
    n_small   smallint,
    n_int     integer,
    n_big     bigint,
    n_real    real,
    n_double  double precision,
    n_bool    boolean,
    n_mood    mood,
    n_inet    inet,
    n_json    jsonb,
    n_bytes   bytea,
    n_date    date,
    n_ts      timestamp,
    n_tstz    timestamptz,
    n_time    time,
    n_timetz  timetz,
    n_texts   text[],
    n_ints    integer[],
    UNIQUE (org_id, product_id, slug)
);

-- A unique key that holds product_id only: an upsert conflicts on it, and
-- guards org_id and workspace_id in its DO UPDATE.
CREATE TABLE wshelf (
    id           text PRIMARY KEY,
    org_id       uuid NOT NULL,
    product_id   uuid NOT NULL,
    workspace_id text NOT NULL,
    slug         text NOT NULL,
    body         text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (product_id, slug)
);

-- A unique key with no scope column: no derived upsert is safe on it.
CREATE TABLE wglobal (
    id         text PRIMARY KEY,
    org_id     uuid NOT NULL,
    product_id uuid NOT NULL,
    slug       text NOT NULL UNIQUE,
    body       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- A view: every column of a view is nullable to the generator.
CREATE VIEW order_live AS
    SELECT id, org_id, product_id, name FROM "order" WHERE deleted_at IS NULL;

-- Columns that collide with the short scope names Org, Product, Workspace.
CREATE TABLE collide (
    id           text PRIMARY KEY,
    org_id       uuid NOT NULL,
    org          text NOT NULL
);
CREATE TABLE collide_ws (
    id           text PRIMARY KEY,
    workspace_id text,
    workspace    text NOT NULL
);
CREATE TABLE collide_product (
    id         text PRIMARY KEY,
    org_id     uuid NOT NULL,
    product_id uuid NOT NULL,
    product    text NOT NULL
);
