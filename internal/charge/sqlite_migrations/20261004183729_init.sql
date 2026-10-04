-- +goose Up
CREATE TABLE pix_charge_operations (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING','UNKNOWN','COMPLETED','FAILED')),
    charge TEXT CHECK (charge IS NULL OR json_valid(charge)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (tenant_id, operation, idempotency_key)
);
CREATE TABLE pix_charge_audit (
    id INTEGER PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    operation_id TEXT,
    tool TEXT NOT NULL,
    outcome TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- +goose Down
DROP TABLE pix_charge_audit;
DROP TABLE pix_charge_operations;
