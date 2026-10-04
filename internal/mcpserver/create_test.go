package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lucaseufrasio/woovi-pix-mcp/internal/charge"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStdioClientCanCreateIdempotentChargeWithOptInAndPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	provider := httptest.NewServer(charge.NewWooviSimulatorWithCreate())
	defer provider.Close()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := charge.NewOperationStore(pool)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	binary := filepath.Join(t.TempDir(), "woovi-pix-mcp")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/woovi-pix-mcp")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build MCP server: %v\n%s", err, output)
	}
	command := exec.Command(binary)
	command.Env = append(command.Environ(), "WOOVI_API_BASE_URL="+provider.URL, "WOOVI_APP_ID=simulator", "WOOVI_ENABLE_CHARGE_CREATION=true", "DATABASE_URL="+dsn, "WOOVI_ACCOUNT_ID=mcp-test-account")
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 2 {
		t.Fatalf("expected read and opted-in create tools, got %+v", listed.Tools)
	}
	invalidAmount, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pix_create_charge", Arguments: map[string]any{"reference": "mcp-create-test-001", "amount_cents": 2500.5, "expires_in_seconds": float64(1800)}})
	if err != nil {
		t.Fatal(err)
	}
	if !invalidAmount.IsError {
		t.Fatalf("fractional cent value must be rejected: %+v", invalidAmount)
	}
	arguments := map[string]any{"reference": "mcp-create-test-001", "amount_cents": float64(2500), "expires_in_seconds": float64(1800)}
	first, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pix_create_charge", Arguments: arguments})
	if err != nil || first.IsError {
		t.Fatalf("create failed: %+v %v", first, err)
	}
	second, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pix_create_charge", Arguments: arguments})
	if err != nil || second.IsError {
		t.Fatalf("idempotent replay failed: %+v %v", second, err)
	}
	secondResult := second.StructuredContent.(map[string]any)
	if secondResult["replayed"] != true {
		t.Fatalf("expected replay of stored charge: %+v", secondResult)
	}
	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pix_charge_audit WHERE tenant_id='mcp-test-account' AND tool='pix_create_charge'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 2 {
		t.Fatalf("expected persisted start and outcome audit rows; got %d", auditCount)
	}
	conflict, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pix_create_charge", Arguments: map[string]any{"reference": "mcp-create-test-001", "amount_cents": float64(2600), "expires_in_seconds": float64(1800)}})
	if err != nil {
		t.Fatal(err)
	}
	if !conflict.IsError {
		t.Fatalf("same idempotency key with altered payload must conflict: %+v", conflict)
	}
}

func TestStdioClientDoesNotAdvertiseCreationByDefault(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer provider.Close()
	binary := filepath.Join(t.TempDir(), "woovi-pix-mcp")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/woovi-pix-mcp")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build MCP server: %v\n%s", err, output)
	}
	command := exec.Command(binary)
	command.Env = append(command.Environ(), "WOOVI_API_BASE_URL="+provider.URL, "WOOVI_APP_ID=simulator")
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "pix_get_charge" {
		t.Fatalf("write tool must be hidden by default: %+v", listed.Tools)
	}
}

func TestStdioClientReconcilesProviderCreatedChargeAfterLostResponse(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	createdOnce := atomic.Bool{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "simulator" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			createdOnce.Store(true)
			_, _ = w.Write([]byte(`{"charge":{"identifier":"after-timeout","correlationID":"mcp-unknown-001","status":"ACTIVE","value":3500,"brCode":"simulated"}}`))
			return
		}
		if r.Method == http.MethodGet && createdOnce.Load() {
			_, _ = w.Write([]byte(`{"charge":{"identifier":"after-timeout","correlationID":"mcp-unknown-001","status":"ACTIVE","value":3500,"brCode":"simulated"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer provider.Close()
	createdOnce.Store(true) // Simulates Woovi accepted the POST before the response was lost.
	lookup, lookupErr := charge.NewWooviClient(provider.URL, "simulator", provider.Client()).GetCharge(context.Background(), "mcp-unknown-001")
	if lookupErr != nil || lookup.ID != "after-timeout" {
		t.Fatalf("lookup should find the already-created provider charge: %+v %v", lookup, lookupErr)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := charge.NewOperationStore(pool)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := charge.CreateChargeRequest{CorrelationID: "mcp-unknown-001", AmountCents: 3500, ExpiresInSeconds: 1800}
	hash, err := charge.RequestHash(request)
	if err != nil {
		t.Fatal(err)
	}
	tenant := "unknown-account-" + time.Now().Format("150405.000000000")
	operation, _, err := store.Reserve(context.Background(), tenant, "pix_create_charge", request.CorrelationID, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUnknown(context.Background(), operation.ID); err != nil {
		t.Fatal(err)
	}

	binary := filepath.Join(t.TempDir(), "woovi-pix-mcp")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/woovi-pix-mcp")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build MCP server: %v\n%s", err, output)
	}
	command := exec.Command(binary)
	command.Env = append(command.Environ(), "WOOVI_API_BASE_URL="+provider.URL, "WOOVI_APP_ID=simulator", "WOOVI_ENABLE_CHARGE_CREATION=true", "DATABASE_URL="+dsn, "WOOVI_ACCOUNT_ID="+tenant)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pix_create_charge", Arguments: map[string]any{"reference": "mcp-unknown-001", "amount_cents": float64(3500), "expires_in_seconds": float64(1800)}})
	if err != nil || result.IsError {
		t.Fatalf("expected safe lookup reconciliation, got result=%+v err=%v", result, err)
	}
	reconciled := result.StructuredContent.(map[string]any)
	if reconciled["reconciled"] != true {
		t.Fatalf("expected reconciled outcome: %+v", reconciled)
	}
}

func TestProviderResponseMismatchIsNeverMarkedCompleted(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "simulator" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"charge":{"identifier":"wrong-charge","correlationID":"other-reference","status":"ACTIVE","value":9999}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer provider.Close()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := charge.NewOperationStore(pool)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	tenant := "mismatch-" + time.Now().Format("150405.000000000")
	api := charge.NewWooviClient(provider.URL, "simulator", provider.Client())
	server, err := NewWithWrites(api, api, store, tenant, true)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.MCP().Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "mismatch-test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	request := charge.CreateChargeRequest{CorrelationID: "wanted-reference", AmountCents: 2000, ExpiresInSeconds: 1800}
	hash, err := charge.RequestHash(request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pix_create_charge", Arguments: map[string]any{"reference": request.CorrelationID, "amount_cents": float64(request.AmountCents), "expires_in_seconds": float64(request.ExpiresInSeconds)}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("provider mismatch must not return success: %+v", result)
	}
	operation, created, err := store.Reserve(context.Background(), tenant, "pix_create_charge", request.CorrelationID, hash)
	if err != nil || created || operation.Status != charge.OperationUnknown {
		t.Fatalf("mismatched PSP response must remain UNKNOWN: %+v %v %v", operation, created, err)
	}
}
