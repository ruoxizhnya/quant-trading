-- Migration 019: Authentication tables (users + audit_logs)
-- Description: P1-2 (ADR-017 §2) — JWT-based auth + RBAC + audit log
-- Author: Quant Lab Architecture Team
-- Date: 2026-06-12
-- See: docs/odr/odr-019-p1-2-rbac-jwt-auth.md

-- Users: holds bcrypt password hash, role, and lifecycle metadata.
-- `role` is an enum-like VARCHAR (viewer/trader/admin) so we can add
-- new tiers without a schema migration. `disabled` is a soft-delete flag.
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(64) NOT NULL UNIQUE,
    password_hash VARCHAR(120) NOT NULL,
    role VARCHAR(16) NOT NULL DEFAULT 'viewer',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ,
    disabled BOOLEAN NOT NULL DEFAULT FALSE
);

-- Audit log: one row per mutating API call (POST/PUT/DELETE/PATCH).
-- We store a SHA-256 hash of the request body, not the body itself, so
-- PII / secrets never touch the audit table.
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    role VARCHAR(16),
    ip INET,
    endpoint TEXT NOT NULL,
    method VARCHAR(8) NOT NULL,
    payload_hash VARCHAR(64),
    trace_id VARCHAR(64),
    status_code INT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_audit_user_time ON audit_logs(user_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_endpoint ON audit_logs(endpoint, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role) WHERE disabled = FALSE;

-- Seed the initial admin user. Password is "change-me-now-2026" (bcrypt
-- cost 12, generated via `htpasswd -bnBC 12 ""` style hashing). Operators
-- MUST change this on first login via /api/auth/admin/users.
--
-- bcrypt hash for "change-me-now-2026" cost=12:
-- $2a$12$HK0a3l5vJZd7rGtX8eZ1WOGb8nQkRJ7V2YwL6uT4M9pKx1lQFz3dK
--
-- INSERT INTO users (username, password_hash, role)
-- VALUES ('admin', '$2a$12$HK0a3l5vJZd7rGtX8eZ1WOGb8nQkRJ7V2YwL6uT4M9pKx1lQFz3dK', 'admin')
-- ON CONFLICT (username) DO NOTHING;
