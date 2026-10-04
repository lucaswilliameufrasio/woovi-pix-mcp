package charge

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresOperationStoreReservesAndCompletesIdempotently(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	store := NewOperationStore(pool)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := "test-" + time.Now().Format("20060102150405.000000000")

	first, created, err := store.Reserve(ctx, tenant, "charge", "key-1", "payload-a")
	if err != nil || !created || first.Status != OperationPending {
		t.Fatalf("first reserve: %+v %v %v", first, created, err)
	}
	repeated, created, err := store.Reserve(ctx, tenant, "charge", "key-1", "payload-a")
	if err != nil || created || repeated.ID != first.ID {
		t.Fatalf("idempotent reserve: %+v %v %v", repeated, created, err)
	}
	if _, _, err := store.Reserve(ctx, tenant, "charge", "key-1", "payload-b"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected payload conflict, got %v", err)
	}

	charge := Charge{ID: "provider-id-1", Reference: "key-1", Status: "ACTIVE", AmountCents: 2500, Currency: "BRL"}
	if err := store.Complete(ctx, first.ID, charge); err != nil {
		t.Fatal(err)
	}
	completed, created, err := store.Reserve(ctx, tenant, "charge", "key-1", "payload-a")
	if err != nil || created || completed.Status != OperationCompleted || completed.Charge.ID != "provider-id-1" {
		t.Fatalf("completed retry: %+v %v %v", completed, created, err)
	}
}

func TestPostgresOperationStoreConcurrentReservationsConverge(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewOperationStore(pool)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := "race-" + time.Now().Format("150405.000000000")
	start := make(chan struct{})
	results := make(chan Operation, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			operation, _, err := store.Reserve(ctx, tenant, "charge", "same-key", "same-payload")
			if err != nil {
				errs <- err
				return
			}
			results <- operation
		}()
	}
	close(start)
	one, two := <-results, <-results
	if one.ID != two.ID {
		t.Fatalf("concurrent calls returned different operation IDs: %s, %s", one.ID, two.ID)
	}
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}

func TestPostgresOperationStoreAppliesMigrationsExactlyOnce(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewOperationStore(pool)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pix_charge_schema_migrations WHERE version='0001_init'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one applied versioned migration, got %d", count)
	}
}

func TestPostgresOperationStorePreservesUnknownOutcome(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewOperationStore(pool)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	key := "unknown-" + time.Now().Format("150405.000000000")
	reserved, _, err := store.Reserve(ctx, "unknown-test", "charge", key, "payload")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUnknown(ctx, reserved.ID); err != nil {
		t.Fatal(err)
	}
	again, created, err := store.Reserve(ctx, "unknown-test", "charge", key, "payload")
	if err != nil || created || again.Status != OperationUnknown {
		t.Fatalf("unknown result must not be retried blindly: %+v %v %v", again, created, err)
	}
	if err := store.MarkFailed(ctx, reserved.ID); err == nil {
		t.Fatal("unknown operation must not be marked failed without reconciliation")
	}
	if err := store.Reconcile(ctx, reserved.ID, Charge{ID: "found-charge", Reference: key, Status: "ACTIVE", AmountCents: 1000, Currency: "BRL"}); err != nil {
		t.Fatal(err)
	}
	completed, created, err := store.Reserve(ctx, "unknown-test", "charge", key, "payload")
	if err != nil || created || completed.Status != OperationCompleted || completed.Charge.ID != "found-charge" {
		t.Fatalf("reconciliation did not persist: %+v %v %v", completed, created, err)
	}
}

func TestPostgresOperationStoreSerializesConcurrentReservations(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewOperationStore(pool)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := "concurrent-" + time.Now().Format("150405.000000000")
	results := make(chan Operation, 2)
	errors := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			operation, _, err := store.Reserve(ctx, tenant, "charge", "same-key", "same-payload")
			if err != nil {
				errors <- err
				return
			}
			results <- operation
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.ID != second.ID || first.Status != OperationPending || second.Status != OperationPending {
		t.Fatalf("concurrent reservations did not converge: %+v %+v", first, second)
	}
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
}
