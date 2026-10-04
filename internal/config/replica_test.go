package config

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucaseufrasio/woovi-pix-mcp/internal/charge"
)

func TestReplicaConfigurationAndValidation(t *testing.T) {
	s := Profiles{Root: filepath.Join(t.TempDir(), "config")}

	p := Profile{Name: "sandbox", Environment: "sandbox", Account: "test", SecretFile: true}
	if err := s.Save(p, "appid"); err != nil {
		t.Fatal(err)
	}

	r := Replica{URL: "s3://test-bucket/account-prefix", Endpoint: "http://127.0.0.1:9000", Region: "us-east-1", SecretFile: true}

	c := ReplicaCredential{AccessKey: "access-sentinel", SecretKey: "secret-sentinel"}
	if err := s.SaveReplica(p, r, c); err != nil {
		t.Fatal(err)
	}

	got, cred, err := s.LoadReplica(p)
	if err != nil || got != r || cred != c {
		t.Fatalf("load %+v %v", got, err)
	}

	if err := s.SaveReplica(p, r, c); err == nil {
		t.Fatal("replica overwritten")
	}

	for _, r := range []Replica{{URL: "s3://user:secret@bucket/prefix"}, {URL: "s3://bucket"}, {URL: "file://relative"}, {URL: "file:///"}, {URL: "s3://bucket/prefix", Endpoint: "http://example.com"}, {URL: "s3://bucket/prefix?secret=x"}} {
		if r.Validate() == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
}

// This test uses the real optional Litestream binary and local file storage,
// not a protocol fake. CI provides the pinned binary explicitly.
func TestLitestreamRealReplicaAndProtectedRestore(t *testing.T) {
	testRealReplica(t, Replica{URL: "file://" + filepath.Join(t.TempDir(), "replica")}, ReplicaCredential{})
}

func TestLitestreamRealS3CompatibleReplica(t *testing.T) {
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT required for real S3-compatible integration")
	}

	testRealReplica(t, Replica{URL: fmt.Sprintf("s3://woovi-test/run-%d", time.Now().UnixNano()), Endpoint: endpoint, Region: "us-east-1", SecretFile: true}, ReplicaCredential{AccessKey: os.Getenv("TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("TEST_S3_SECRET_KEY")})
}

func testRealReplica(t *testing.T, r Replica, c ReplicaCredential) {
	t.Helper()

	binary := os.Getenv("TEST_LITESTREAM_BINARY")
	if binary == "" {
		t.Skip("TEST_LITESTREAM_BINARY required for optional Litestream integration")
	}

	s := Profiles{Root: filepath.Join(t.TempDir(), "config")}

	p := Profile{Name: "test", Environment: "simulator", Account: "test", SecretFile: true}
	if err := s.Save(p, "appid"); err != nil {
		t.Fatal(err)
	}

	if err := s.SaveReplica(p, r, c); err != nil {
		t.Fatal(err)
	}

	db := s.DatabasePath(p)

	store, err := charge.OpenSQLiteStore(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}

	operation, _, err := store.Reserve(context.Background(), "test", "create", "reference", "hash")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.MarkUnknown(context.Background(), operation.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.RunReplica(context.Background(), p, binary, "restore", true); err == nil {
		t.Fatal("restore accepted active MCP")
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- s.RunReplica(ctx, p, binary, "replicate", false) }()
	// Wait for an LTX file (file backend) or a bounded replication interval
	// (S3), then verify the restored contents, not merely process startup.
	deadline := time.Now().Add(20 * time.Second)
	restored := false

	for time.Now().Before(deadline) {
		if r.Endpoint != "" {
			time.Sleep(4 * time.Second)

			restored = true

			break
		}

		_ = filepath.WalkDir(r.URL[len("file://"):], func(path string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && filepath.Ext(path) == ".ltx" {
				restored = true
			}

			return nil
		})

		if restored {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if !restored {
		cancel()

		err := <-done
		_ = store.Close()

		t.Fatalf("replication did not produce files: %v", err)
	}

	if err := s.RunReplica(context.Background(), p, binary, "status", false); err != nil {
		cancel()
		<-done

		_ = store.Close()

		t.Fatal(err)
	}

	cancel()
	<-done

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := s.RunReplica(context.Background(), p, binary, "restore", true); err == nil {
		t.Fatal("restore overwrote existing database")
	}

	if err := os.Rename(db, db+".archived"); err != nil {
		t.Fatal(err)
	}

	if err := s.RunReplica(context.Background(), p, binary, "restore", false); err == nil {
		t.Fatal("restore did not require acknowledgement")
	}

	if err := s.RunReplica(context.Background(), p, binary, "restore", true); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(db + ".recovered"); err != nil {
		t.Fatal("recovery safety marker missing")
	}

	conn, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = conn.Close() }()

	var status string
	if err := conn.QueryRow("SELECT status FROM pix_charge_operations WHERE id=?", operation.ID).Scan(&status); err != nil || status != "UNKNOWN" {
		t.Fatalf("restored state: %s %v", status, err)
	}
}
