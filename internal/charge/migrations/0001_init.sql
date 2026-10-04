CREATE TABLE pix_charge_operations (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING','UNKNOWN','COMPLETED','FAILED')),
    charge JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, operation, idempotency_key)
);

CREATE TABLE pix_charge_audit (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    operation_id TEXT,
    tool TEXT NOT NULL,
    outcome TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
