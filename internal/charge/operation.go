package charge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")

type OperationStatus string

const (
	OperationPending   OperationStatus = "PENDING"
	OperationUnknown   OperationStatus = "UNKNOWN"
	OperationCompleted OperationStatus = "COMPLETED"
	OperationFailed    OperationStatus = "FAILED"
)

type Operation struct {
	ID          string
	Status      OperationStatus
	Charge      Charge
	PayloadHash string
	CreatedAt   time.Time
}

type OperationStore struct{ pool *pgxpool.Pool }

func NewOperationStore(pool *pgxpool.Pool) *OperationStore { return &OperationStore{pool: pool} }

func (s *OperationStore) Migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('woovi_pix_mcp_schema'))`); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS pix_charge_operations (
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
	)`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS pix_charge_audit (
		id BIGSERIAL PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		operation_id TEXT,
		tool TEXT NOT NULL,
		outcome TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *OperationStore) Audit(ctx context.Context, tenant, operationID, tool, outcome string) error {
	if tenant == "" || tool == "" || outcome == "" {
		return errors.New("audit tenant, tool and outcome are required")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO pix_charge_audit(tenant_id,operation_id,tool,outcome) VALUES($1,$2,$3,$4)`, tenant, operationID, tool, outcome)
	if err != nil {
		return fmt.Errorf("persist operation audit: %w", err)
	}
	return nil
}

func (s *OperationStore) Reserve(ctx context.Context, tenant, operation, key, payloadHash string) (Operation, bool, error) {
	if tenant == "" || operation == "" || key == "" || payloadHash == "" {
		return Operation{}, false, errors.New("tenant, operation, idempotency key and payload hash are required")
	}
	id, err := newOperationID()
	if err != nil {
		return Operation{}, false, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Operation{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var insertedID string
	err = tx.QueryRow(ctx, `INSERT INTO pix_charge_operations(id,tenant_id,operation,idempotency_key,payload_hash,status)
		VALUES($1,$2,$3,$4,$5,'PENDING') ON CONFLICT(tenant_id,operation,idempotency_key) DO NOTHING RETURNING id`, id, tenant, operation, key, payloadHash).Scan(&insertedID)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return Operation{}, false, err
		}
		return Operation{ID: insertedID, Status: OperationPending, PayloadHash: payloadHash, CreatedAt: time.Now().UTC()}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Operation{}, false, err
	}
	var existing Operation
	var existingHash string
	var chargeJSON []byte
	err = tx.QueryRow(ctx, `SELECT id,payload_hash,status,charge,created_at FROM pix_charge_operations
		WHERE tenant_id=$1 AND operation=$2 AND idempotency_key=$3 FOR UPDATE`, tenant, operation, key).
		Scan(&existing.ID, &existingHash, &existing.Status, &chargeJSON, &existing.CreatedAt)
	if err != nil {
		return Operation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Operation{}, false, err
	}
	if existingHash != payloadHash {
		return Operation{}, false, ErrIdempotencyConflict
	}
	existing.PayloadHash = existingHash
	if len(chargeJSON) != 0 {
		if err := json.Unmarshal(chargeJSON, &existing.Charge); err != nil {
			return Operation{}, false, errors.New("stored charge result is invalid")
		}
	}
	return existing, false, nil
}

func (s *OperationStore) Complete(ctx context.Context, id string, charge Charge) error {
	encoded, err := json.Marshal(charge)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `UPDATE pix_charge_operations SET status='COMPLETED',charge=$2::jsonb,updated_at=now()
		WHERE id=$1 AND status='PENDING'`, id, encoded)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("operation is not available for completion")
	}
	return nil
}

func (s *OperationStore) MarkUnknown(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `UPDATE pix_charge_operations SET status='UNKNOWN',updated_at=now()
		WHERE id=$1 AND status='PENDING'`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("operation is not pending")
	}
	return nil
}

func (s *OperationStore) MarkFailed(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `UPDATE pix_charge_operations SET status='FAILED',updated_at=now() WHERE id=$1 AND status='PENDING'`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("operation is not pending")
	}
	return nil
}

func (s *OperationStore) Reconcile(ctx context.Context, id string, charge Charge) error {
	encoded, err := json.Marshal(charge)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `UPDATE pix_charge_operations SET status='COMPLETED',charge=$2::jsonb,updated_at=now()
		WHERE id=$1 AND status='UNKNOWN'`, id, encoded)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("operation is not awaiting reconciliation")
	}
	return nil
}

func newOperationID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func RequestHash(input CreateChargeRequest) (string, error) {
	canonical, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
