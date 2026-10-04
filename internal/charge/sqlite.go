package charge

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// OperationRepository is the durable boundary used by the MCP write tools.
type OperationRepository interface {
	Reserve(context.Context, string, string, string, string) (Operation, bool, error)
	Audit(context.Context, string, string, string, string) error
	Complete(context.Context, string, Charge) error
	MarkUnknown(context.Context, string) error
	Reconcile(context.Context, string, Charge) error
}

//go:embed sqlite_migrations/*.sql
var sqliteMigrations embed.FS

type SQLiteStore struct {
	db    *sql.DB
	guard *flock.Flock
}

// OpenSQLiteStore opens a private file-backed store, never an in-memory database.
// Callers must supply a private, profile-specific data directory.
func OpenSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	if path == "" || path == ":memory:" {
		return nil, errors.New("a persistent SQLite path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("invalid SQLite path")
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, errors.New("unable to create SQLite directory")
	}
	guard := flock.New(abs+".runtime.lock", flock.SetPermissions(0600))
	locked, err := guard.TryRLock()
	if err != nil || !locked {
		return nil, errors.New("database is locked for recovery")
	}
	success := false
	defer func() {
		if !success {
			_ = guard.Unlock()
		}
	}()
	if info, err := os.Lstat(abs); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("SQLite file must be private and regular")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("unable to inspect SQLite file")
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		_ = f.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return nil, errors.New("unable to create SQLite file")
	}
	u := url.URL{Scheme: "file", Path: abs}
	query := u.Query()
	query.Add("_pragma", "busy_timeout(10000)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("unable to open SQLite")
	}
	db.SetMaxOpenConns(1)
	s := &SQLiteStore{db: db, guard: guard}
	if err := s.migrate(ctx, abs+".migration.lock"); err != nil {
		_ = db.Close()
		return nil, errors.New("unable to initialize SQLite operation store")
	}
	success = true
	return s, nil
}

func (s *SQLiteStore) Close() error {
	err := s.db.Close()
	lockErr := s.guard.Unlock()
	if err != nil {
		return err
	}
	return lockErr
}

func (s *SQLiteStore) migrate(ctx context.Context, lockPath string) error {
	// Serialize Goose bootstrap across processes. OS locks are released on crash.
	guard := flock.New(lockPath, flock.SetPermissions(0600))
	locked, err := guard.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("migration lock unavailable")
	}
	defer func() { _ = guard.Unlock() }()
	migrations, err := fs.Sub(sqliteMigrations, "sqlite_migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, s.db, migrations)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

func (s *SQLiteStore) Reserve(ctx context.Context, tenant, operation, key, hash string) (Operation, bool, error) {
	if tenant == "" || operation == "" || key == "" || hash == "" {
		return Operation{}, false, errors.New("tenant, operation, idempotency key and payload hash are required")
	}
	id, err := newOperationID()
	if err != nil {
		return Operation{}, false, err
	}
	var createdAt string
	err = s.db.QueryRowContext(ctx, `INSERT INTO pix_charge_operations(id,tenant_id,operation,idempotency_key,payload_hash,status)
		VALUES(?,?,?,?,?,'PENDING') ON CONFLICT(tenant_id,operation,idempotency_key) DO NOTHING RETURNING created_at`, id, tenant, operation, key, hash).Scan(&createdAt)
	if err == nil {
		at, parseErr := time.Parse(time.RFC3339Nano, createdAt)
		return Operation{ID: id, Status: OperationPending, PayloadHash: hash, CreatedAt: at}, true, parseErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, false, errors.New("unable to reserve SQLite operation")
	}
	var existing Operation
	var encoded sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT id,status,payload_hash,charge,created_at FROM pix_charge_operations
		WHERE tenant_id=? AND operation=? AND idempotency_key=?`, tenant, operation, key).Scan(&existing.ID, &existing.Status, &existing.PayloadHash, &encoded, &createdAt)
	if err != nil {
		return Operation{}, false, errors.New("unable to read SQLite operation")
	}
	if existing.PayloadHash != hash {
		return Operation{}, false, ErrIdempotencyConflict
	}
	existing.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Operation{}, false, errors.New("stored operation timestamp is invalid")
	}
	if encoded.Valid {
		if err := json.Unmarshal([]byte(encoded.String), &existing.Charge); err != nil {
			return Operation{}, false, errors.New("stored charge result is invalid")
		}
	}
	return existing, false, nil
}

func (s *SQLiteStore) Audit(ctx context.Context, tenant, id, tool, outcome string) error {
	if tenant == "" || tool == "" || outcome == "" {
		return errors.New("audit tenant, tool and outcome are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO pix_charge_audit(tenant_id,operation_id,tool,outcome) VALUES(?,?,?,?)`, tenant, id, tool, outcome)
	if err != nil {
		return errors.New("unable to persist SQLite audit")
	}
	return nil
}

func (s *SQLiteStore) transition(ctx context.Context, id string, from, to OperationStatus, result *Charge) error {
	var encoded any
	if result != nil {
		body, err := json.Marshal(result)
		if err != nil {
			return err
		}
		encoded = string(body)
	}
	r, err := s.db.ExecContext(ctx, `UPDATE pix_charge_operations SET status=?,charge=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=? AND status=?`, to, encoded, id, from)
	if err != nil {
		return errors.New("unable to persist SQLite operation status")
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		return errors.New("operation is not available for transition")
	}
	return nil
}

func (s *SQLiteStore) Complete(ctx context.Context, id string, c Charge) error {
	return s.transition(ctx, id, OperationPending, OperationCompleted, &c)
}
func (s *SQLiteStore) MarkUnknown(ctx context.Context, id string) error {
	return s.transition(ctx, id, OperationPending, OperationUnknown, nil)
}
func (s *SQLiteStore) Reconcile(ctx context.Context, id string, c Charge) error {
	return s.transition(ctx, id, OperationUnknown, OperationCompleted, &c)
}
