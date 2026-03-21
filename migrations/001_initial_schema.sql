-- 001_initial_schema.sql
-- NetVantage Control Plane: initial schema for agents, tests, and POPs.
-- Idempotent: safe to re-run.

-- POPs (Points of Presence) — logical grouping for agents.
CREATE TABLE IF NOT EXISTS pops (
    name        TEXT PRIMARY KEY,
    provider    TEXT NOT NULL DEFAULT '',
    city        TEXT NOT NULL DEFAULT '',
    country     TEXT NOT NULL DEFAULT '',
    latitude    DOUBLE PRECISION,
    longitude   DOUBLE PRECISION,
    labels      JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Agents — registered canary agents.
CREATE TABLE IF NOT EXISTS agents (
    id              TEXT PRIMARY KEY,
    pop_name        TEXT NOT NULL REFERENCES pops(name),
    version         TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'online',
    capabilities    TEXT[] NOT NULL DEFAULT '{}',
    labels          JSONB NOT NULL DEFAULT '{}',
    last_heartbeat  TIMESTAMPTZ NOT NULL DEFAULT now(),
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agents_pop_name ON agents(pop_name);
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);

-- Test definitions — centrally managed synthetic tests.
CREATE TABLE IF NOT EXISTS test_definitions (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    test_type   TEXT NOT NULL,
    target      TEXT NOT NULL,
    interval_ms BIGINT NOT NULL DEFAULT 60000,
    timeout_ms  BIGINT NOT NULL DEFAULT 10000,
    config      JSONB NOT NULL DEFAULT '{}',
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_test_definitions_type ON test_definitions(test_type);
CREATE INDEX IF NOT EXISTS idx_test_definitions_enabled ON test_definitions(enabled);

-- Test assignments — which tests run on which POPs.
-- NULL pop_name means "all POPs" (global test).
CREATE TABLE IF NOT EXISTS test_assignments (
    id              BIGSERIAL PRIMARY KEY,
    test_id         TEXT NOT NULL REFERENCES test_definitions(id) ON DELETE CASCADE,
    pop_name        TEXT REFERENCES pops(name) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(test_id, pop_name)
);

CREATE INDEX IF NOT EXISTS idx_test_assignments_pop ON test_assignments(pop_name);
CREATE INDEX IF NOT EXISTS idx_test_assignments_test ON test_assignments(test_id);

-- API keys for authentication.
CREATE TABLE IF NOT EXISTS api_keys (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    key_hash    TEXT NOT NULL UNIQUE,
    role        TEXT NOT NULL DEFAULT 'agent',
    scopes      TEXT[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ,
    last_used   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);

-- Updated-at trigger function.
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply updated_at triggers.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_pops_updated_at') THEN
        CREATE TRIGGER trg_pops_updated_at BEFORE UPDATE ON pops
        FOR EACH ROW EXECUTE FUNCTION update_updated_at();
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_agents_updated_at') THEN
        CREATE TRIGGER trg_agents_updated_at BEFORE UPDATE ON agents
        FOR EACH ROW EXECUTE FUNCTION update_updated_at();
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_test_definitions_updated_at') THEN
        CREATE TRIGGER trg_test_definitions_updated_at BEFORE UPDATE ON test_definitions
        FOR EACH ROW EXECUTE FUNCTION update_updated_at();
    END IF;
END $$;
