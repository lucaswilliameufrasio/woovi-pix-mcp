package charge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestSQLitePersistenceAndTransitions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "operations.db")
	s, err := OpenSQLiteStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := s.Reserve(ctx, "account", "create", "reference", "hash")
	if err != nil || !created {
		t.Fatalf("reserve: %+v %v %v", first, created, err)
	}
	if err := s.Audit(ctx, "account", first.ID, "create", "STARTED"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkUnknown(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, first.ID, Charge{}); err == nil {
		t.Fatal("UNKNOWN must not complete through PENDING transition")
	}
	want := Charge{ID: "provider", Reference: "reference", AmountCents: 123, Currency: "BRL"}
	if err := s.Reconcile(ctx, first.ID, want); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSQLiteStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	got, created, err := s.Reserve(ctx, "account", "create", "reference", "hash")
	if err != nil || created || got.ID != first.ID || got.Charge != want || got.Status != OperationCompleted {
		t.Fatalf("reopen: %+v %v %v", got, created, err)
	}
	if _, _, err := s.Reserve(ctx, "account", "create", "reference", "different"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict: %v", err)
	}
	if _, created, err := s.Reserve(ctx, "other-account", "create", "reference", "different"); err != nil || !created {
		t.Fatalf("scope isolation: %v %v", created, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM pix_charge_audit WHERE operation_id=?", first.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit: %d %v", count, err)
	}
	if err := s.Audit(ctx, "", first.ID, "create", "STARTED"); err == nil {
		t.Fatal("empty tenant accepted")
	}
	if err := s.MarkUnknown(ctx, "missing"); err == nil {
		t.Fatal("missing operation accepted")
	}
}

func TestSQLiteConcurrentProcesses(t *testing.T) {
	if os.Getenv("WOOVI_SQLITE_TEST_HELPER") == "1" {
		s, err := OpenSQLiteStore(context.Background(), os.Getenv("WOOVI_SQLITE_TEST_PATH"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()
		_, created, err := s.Reserve(context.Background(), "account", "create", "key", "hash")
		if err != nil {
			t.Fatal(err)
		}
		if created {
			if err := s.Audit(context.Background(), "account", "", "create", "WINNER"); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	path := filepath.Join(t.TempDir(), "shared.db")
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 6 {
		wg.Go(func() {
			<-start
			cmd := exec.Command(os.Args[0], "-test.run=^TestSQLiteConcurrentProcesses$")
			cmd.Env = append(os.Environ(), "WOOVI_SQLITE_TEST_HELPER=1", "WOOVI_SQLITE_TEST_PATH="+path)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("child: %v %s", err, out)
			}
		})
	}
	close(start)
	wg.Wait()
	s, err := OpenSQLiteStore(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM pix_charge_audit WHERE outcome='WINNER'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("winners: %d %v", count, err)
	}
}

func TestSQLiteRejectsUnsafePaths(t *testing.T) {
	ctx := context.Background()
	for _, path := range []string{"", ":memory:"} {
		if s, err := OpenSQLiteStore(ctx, path); err == nil {
			_ = s.Close()
			t.Fatalf("accepted %q", path)
		}
	}
	path := filepath.Join(t.TempDir(), "public.db")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenSQLiteStore(ctx, path); err == nil {
		_ = s.Close()
		t.Fatal("accepted public file")
	}
}
