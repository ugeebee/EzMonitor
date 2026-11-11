-- =====================================================================
-- EzMonitor schema v1 (PostgreSQL 16+)
-- Owner: Data Infrastructure (Harsha)
-- Applied by golang-migrate. Never edit an applied migration: add a new one.
-- All timestamps are TIMESTAMPTZ; partition boundaries are UTC days.
-- =====================================================================
BEGIN;

CREATE EXTENSION IF NOT EXISTS citext;    -- case-insensitive email
CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- gen_random_bytes / crypt for tokens + seed

-- ---------------------------------------------------------------------
-- Domains (enums replace VARCHAR + CHECK: 4 B, typo-proof, self-documenting)
-- ---------------------------------------------------------------------
CREATE TYPE instance_status AS ENUM ('unknown', 'up', 'down');
CREATE TYPE metric_kind     AS ENUM ('cpu_util', 'gpu_util', 'ram_util', 'uptime',
                                     'net_up_kbps', 'net_down_kbps');
CREATE TYPE cmp_op          AS ENUM ('>', '>=', '<', '<=');
CREATE TYPE alert_status    AS ENUM ('active', 'resolved');
CREATE TYPE webhook_status  AS ENUM ('active', 'disabled');

-- ---------------------------------------------------------------------
-- Identity & RBAC
-- ---------------------------------------------------------------------
-- Fixed IDs so Go code can use constants (RoleAdmin = 1, ...).
CREATE TABLE roles (
    role_id   SMALLINT    PRIMARY KEY,
    role_name VARCHAR(20) NOT NULL UNIQUE
);
INSERT INTO roles (role_id, role_name) VALUES (1, 'admin'), (2, 'editor'), (3, 'viewer');

CREATE TABLE users (
    u_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         VARCHAR(100) NOT NULL CHECK (btrim(name) <> ''),
    email        CITEXT       NOT NULL UNIQUE                              -- BR1
                 CHECK (email ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$'),           -- BR1
    hash_pass    TEXT         NOT NULL,            -- bcrypt/argon2id string produced by Go
    date_created TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE groups (
    g_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    label        VARCHAR(100) NOT NULL CHECK (btrim(label) <> ''),
    date_created TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Associative entity for Users M:N Groups, with relationship attribute role_id.
CREATE TABLE user_group_roles (
    u_id    BIGINT   NOT NULL REFERENCES users  ON DELETE CASCADE,
    g_id    BIGINT   NOT NULL REFERENCES groups ON DELETE CASCADE,       -- BR8
    role_id SMALLINT NOT NULL REFERENCES roles,
    PRIMARY KEY (u_id, g_id)                                             -- BR2
);
CREATE INDEX user_group_roles_g_id_idx ON user_group_roles (g_id);

-- ---------------------------------------------------------------------
-- Monitored instances
-- ---------------------------------------------------------------------
CREATE TABLE instances (
    i_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    g_id         BIGINT          NOT NULL REFERENCES groups ON DELETE CASCADE,  -- BR8
    label        VARCHAR(100)    NOT NULL CHECK (btrim(label) <> ''),
    -- 16-hex hardware fingerprint produced by the agent (collector/fingerprint.go)
    fingerprint  VARCHAR(16)     NOT NULL UNIQUE CHECK (fingerprint ~ '^[0-9a-f]{16}$'),
    -- hex SHA-256 of the agent's secret token; the plaintext token is never stored
        -- hardware metadata, sent by the agent in the pairing payload (nullable: may be unknown)
    cpu_model    VARCHAR(150),
    ram_config   VARCHAR(50),
    gpu_model    VARCHAR(150),
    token_hash   VARCHAR(64)     NOT NULL UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    enabled      BOOLEAN         NOT NULL DEFAULT true,     -- admin switch (BR5 / T1)
    status       instance_status NOT NULL DEFAULT 'unknown',-- health, set by backend
    date_created TIMESTAMPTZ     NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ,
    UNIQUE (g_id, label),                                    -- BR3
    UNIQUE (i_id, g_id)          -- target for tenant-safe composite FKs (alert_logs)
);

-- Weak entity of instances: identified by (owner, key).
CREATE TABLE instance_labels (
    i_id        BIGINT       NOT NULL REFERENCES instances ON DELETE CASCADE,
    label_key   VARCHAR(63)  NOT NULL CHECK (label_key ~ '^[a-z][a-z0-9_]{0,62}$'),
    label_value VARCHAR(100) NOT NULL,
    PRIMARY KEY (i_id, label_key)
);
CREATE INDEX instance_labels_kv_idx ON instance_labels (label_key, label_value);

-- ---------------------------------------------------------------------
-- Time-series samples: weak entity of instances, range-partitioned by UTC day.
-- Column names match the agent JSON (internal/models/metrics.go) except the
-- network pair, which is up_speed/down_speed (KB/s) in the agent.
-- ---------------------------------------------------------------------
CREATE TABLE metrics_data (
    i_id          BIGINT      NOT NULL REFERENCES instances ON DELETE CASCADE,
    recorded_at   TIMESTAMPTZ NOT NULL,
    cpu_util      REAL        NOT NULL CHECK (cpu_util BETWEEN 0 AND 100),
    gpu_util      REAL                 CHECK (gpu_util BETWEEN 0 AND 100), -- NULL = no GPU
    ram_util      REAL        NOT NULL CHECK (ram_util BETWEEN 0 AND 100),
    uptime        BIGINT      NOT NULL CHECK (uptime >= 0),                -- seconds
    net_up_kbps   REAL        NOT NULL CHECK (net_up_kbps   >= 0),
    net_down_kbps REAL        NOT NULL CHECK (net_down_kbps >= 0),
    PRIMARY KEY (i_id, recorded_at)                                        -- BR4
) PARTITION BY RANGE (recorded_at);

-- PK index serves "one instance, time range"; BRIN serves "all instances, last N min".
CREATE INDEX metrics_data_recorded_brin ON metrics_data USING brin (recorded_at);

CREATE FUNCTION create_metrics_partition(p_day date)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS %I PARTITION OF metrics_data FOR VALUES FROM (%L) TO (%L)',
        'metrics_data_' || to_char(p_day, 'YYYYMMDD'),
        p_day::timestamp       AT TIME ZONE 'UTC',
        (p_day + 1)::timestamp AT TIME ZONE 'UTC');
END $$;

CREATE FUNCTION drop_old_metrics_partitions(keep_days int)
RETURNS int LANGUAGE plpgsql AS $$
DECLARE
    r       record;
    cutoff  text := to_char((now() AT TIME ZONE 'UTC')::date - keep_days, 'YYYYMMDD');
    dropped int  := 0;
BEGIN
    FOR r IN
        SELECT c.relname
        FROM pg_inherits i
        JOIN pg_class c ON c.oid = i.inhrelid
        WHERE i.inhparent = 'metrics_data'::regclass
          AND c.relname ~ '^metrics_data_\d{8}$'
          AND substring(c.relname FROM '\d{8}$') < cutoff
    LOOP
        EXECUTE format('DROP TABLE %I', r.relname);
        dropped := dropped + 1;
    END LOOP;
    RETURN dropped;
END $$;

-- Called hourly by the db-maint container: pre-creates yesterday..+7 days, enforces retention.
CREATE FUNCTION run_maintenance(keep_days int DEFAULT 30)
RETURNS text LANGUAGE plpgsql AS $$
DECLARE
    today   date := (now() AT TIME ZONE 'UTC')::date;
    dropped int;
BEGIN
    PERFORM create_metrics_partition(today + n) FROM generate_series(-1, 7) AS n;
    dropped := drop_old_metrics_partitions(keep_days);
    RETURN format('partitions ensured %s..%s, dropped %s', today - 1, today + 7, dropped);
END $$;

SELECT run_maintenance();

-- T1 / BR5: reject samples for disabled instances.
-- Statement-level with a transition table: one join per batch instead of one
-- lookup per row. Two triggers because a transition-table trigger may have only
-- one event, and BR4 upserts arrive as UPDATEs on conflict.
CREATE FUNCTION t1_reject_disabled_instances()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    bad_id BIGINT;
BEGIN
    SELECT n.i_id INTO bad_id
    FROM new_rows n
    JOIN instances i USING (i_id)
    WHERE NOT i.enabled
    LIMIT 1;

    IF FOUND THEN
        RAISE EXCEPTION 'instance % is disabled; samples rejected', bad_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END $$;

CREATE TRIGGER t1_reject_disabled_on_insert
AFTER INSERT ON metrics_data
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT EXECUTE FUNCTION t1_reject_disabled_instances();

CREATE TRIGGER t1_reject_disabled_on_update
AFTER UPDATE ON metrics_data
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT EXECUTE FUNCTION t1_reject_disabled_instances();

-- ---------------------------------------------------------------------
-- Alerting tables (trigger logic lives in 000003_alerting, owned by Aayush)
-- ---------------------------------------------------------------------
CREATE TABLE alert_rules (
    rule_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    g_id            BIGINT           NOT NULL REFERENCES groups ON DELETE CASCADE, -- BR8
    metric_name     metric_kind      NOT NULL,
    operator        cmp_op           NOT NULL,
    threshold_value DOUBLE PRECISION NOT NULL CHECK (threshold_value >= 0),
    enabled         BOOLEAN          NOT NULL DEFAULT true,
    date_created    TIMESTAMPTZ      NOT NULL DEFAULT now(),
    UNIQUE (rule_id, g_id)
);
CREATE INDEX alert_rules_eval_idx ON alert_rules (g_id, metric_name) WHERE enabled;

-- g_id is deliberately carried here (controlled redundancy, rule_id -> g_id):
-- the two composite FKs make it impossible for a rule of group A to log an
-- alert against an instance of group B, and the redundant copy can never disagree.
CREATE TABLE alert_logs (
    a_id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    rule_id      BIGINT           NOT NULL,
    i_id         BIGINT           NOT NULL,
    g_id         BIGINT           NOT NULL,
    alert_value  DOUBLE PRECISION NOT NULL,
    triggered_at TIMESTAMPTZ      NOT NULL,
    resolved_at  TIMESTAMPTZ,
    status       alert_status     NOT NULL DEFAULT 'active',
    FOREIGN KEY (rule_id, g_id) REFERENCES alert_rules (rule_id, g_id) ON DELETE CASCADE,
    FOREIGN KEY (i_id,    g_id) REFERENCES instances   (i_id,    g_id) ON DELETE CASCADE,
    CHECK ((status = 'resolved') = (resolved_at IS NOT NULL)),
    CHECK (resolved_at IS NULL OR resolved_at >= triggered_at)
);
-- BR7: at most one active alert per (rule, instance).
CREATE UNIQUE INDEX alert_logs_one_active ON alert_logs (rule_id, i_id) WHERE status = 'active';
CREATE INDEX alert_logs_group_time_idx ON alert_logs (g_id, triggered_at DESC);
CREATE INDEX alert_logs_i_id_idx       ON alert_logs (i_id);

-- BR7: resolved alerts are history and are never re-opened or edited.
CREATE FUNCTION br7_freeze_resolved_alerts()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = 'resolved' THEN
        RAISE EXCEPTION 'alert % is resolved and cannot be modified', OLD.a_id
            USING ERRCODE = 'check_violation';
    END IF;
    IF (NEW.rule_id, NEW.i_id, NEW.g_id, NEW.alert_value, NEW.triggered_at)
       IS DISTINCT FROM
       (OLD.rule_id, OLD.i_id, OLD.g_id, OLD.alert_value, OLD.triggered_at) THEN
        RAISE EXCEPTION 'alert %: only status/resolved_at may change', OLD.a_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER br7_freeze_resolved
BEFORE UPDATE ON alert_logs
FOR EACH ROW EXECUTE FUNCTION br7_freeze_resolved_alerts();

CREATE TABLE webhooks (
    web_id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    g_id         BIGINT         NOT NULL REFERENCES groups ON DELETE CASCADE,  -- BR8
    webhook_url  VARCHAR(2048)  NOT NULL CHECK (webhook_url ~ '^https://'),
    secret       TEXT           NOT NULL,   -- HMAC-SHA256 signing key for payloads
    status       webhook_status NOT NULL DEFAULT 'active',
    date_created TIMESTAMPTZ    NOT NULL DEFAULT now(),
    UNIQUE (web_id, g_id)
);

-- BR6: a rule may only notify webhooks of its own group, enforced by composite FKs.
CREATE TABLE alert_rule_webhooks (
    rule_id BIGINT NOT NULL,
    web_id  BIGINT NOT NULL,
    g_id    BIGINT NOT NULL,
    PRIMARY KEY (rule_id, web_id),
    FOREIGN KEY (rule_id, g_id) REFERENCES alert_rules (rule_id, g_id) ON DELETE CASCADE,
    FOREIGN KEY (web_id,  g_id) REFERENCES webhooks    (web_id,  g_id) ON DELETE CASCADE
);
CREATE INDEX alert_rule_webhooks_web_id_idx ON alert_rule_webhooks (web_id);

COMMIT;
