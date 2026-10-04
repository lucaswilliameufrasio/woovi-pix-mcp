package charge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWooviClientGetsAndMinimizesCharge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/charge/correlation-123" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "test-secret" {
			t.Fatalf("authorization header missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"charge":{"identifier":"charge-1","correlationID":"correlation-123","status":"COMPLETED","value":1234,"expiresDate":"2026-10-03T12:00:00Z","brCode":"pix-copy-code","customer":{"name":"Sensitive Name","email":"person@example.com"}}}`))
	}))
	defer server.Close()

	got, err := NewWooviClient(server.URL, "test-secret", server.Client()).GetCharge(context.Background(), "correlation-123")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "charge-1" || got.Reference != "correlation-123" || got.Status != "COMPLETED" || got.AmountCents != 1234 || got.Currency != "BRL" || got.PixCode != "pix-copy-code" || !got.ExpiresAt.Equal(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected charge: %+v", got)
	}
	encoded, _ := json.Marshal(got)
	if string(encoded) == "" || contains(string(encoded), "Sensitive Name") || contains(string(encoded), "person@example.com") {
		t.Fatalf("PII leaked into result: %s", encoded)
	}
}

func TestWooviClientSanitizesProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"secret provider detail"}`, http.StatusTooManyRequests)
	}))
	defer server.Close()
	_, err := NewWooviClient(server.URL, "secret-app-id", server.Client()).GetCharge(context.Background(), "charge-1")
	if err == nil || err.Error() != "provider returned HTTP 429" || contains(err.Error(), "secret") {
		t.Fatalf("provider error was not sanitized: %v", err)
	}
}

func TestWooviClientEscapesChargeReferenceAsOnePathSegment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v1/charge/order%2Fwith%23reserved" {
			t.Fatalf("reference was not escaped as one path segment: %q", r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"charge":{"identifier":"id-1","correlationID":"order/with#reserved","status":"ACTIVE","value":100}}`))
	}))
	defer server.Close()
	if _, err := NewWooviClient(server.URL, "test", server.Client()).GetCharge(context.Background(), "order/with#reserved"); err != nil {
		t.Fatal(err)
	}
}

func contains(s, part string) bool {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
