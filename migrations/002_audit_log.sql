-- 002_audit_log.sql
-- NetVantage Control Plane: audit log for tracking all mutations.
-- Idempotent: safe to re-run.

CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id    TEXT NOT NULL DEFAULT '',
    actor_role  TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    resource    TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    source_ip   TEXT NOT NULL DEFAULT '',
    change_diff JSONB
);

CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_resource ON audit_log(resource, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_actor ON audit_log(actor_id);

-- Partition hint: for production, consider range-partitioning by timestamp
-- (monthly partitions) once the table exceeds ~10M rows.
COMMENT ON TABLE audit_log IS 'Immutable audit trail of all control plane mutations (M9).';
