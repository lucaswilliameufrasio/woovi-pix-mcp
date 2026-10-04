package charge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWooviCreateChargeUsesCorrelationIDForIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/charge" || r.URL.Query().Get("return_existing") != "true" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}

		if r.Header.Get("Authorization") != "test-secret" {
			t.Fatal("authorization header missing")
		}

		var request struct {
			Value         int64  `json:"value"`
			CorrelationID string `json:"correlationID"`
			ExpiresIn     int64  `json:"expiresIn"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}

		if request.Value != 1200 || request.CorrelationID != "order-123" || request.ExpiresIn != 1800 {
			t.Fatalf("unexpected request payload: %+v", request)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"charge":{"identifier":"charge-1","correlationID":"order-123","status":"ACTIVE","value":1200,"brCode":"safe-pix"}}`))
	}))
	defer server.Close()

	got, err := NewWooviClient(server.URL, "test-secret", server.Client()).CreateCharge(context.Background(), CreateChargeRequest{CorrelationID: "order-123", AmountCents: 1200, ExpiresInSeconds: 1800})
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != "charge-1" || got.Reference != "order-123" || got.AmountCents != 1200 {
		t.Fatalf("unexpected created charge: %+v", got)
	}
}

func TestWooviCreateTimeoutIsNotSafeToRetryBlindly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()

		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}

		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}

		_ = conn.Close()
	}))
	defer server.Close()

	_, err := NewWooviClient(server.URL, "test-secret", server.Client()).CreateCharge(context.Background(), CreateChargeRequest{CorrelationID: "order-timeout", AmountCents: 1000, ExpiresInSeconds: 300})
	if err == nil || !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("expected unknown outcome requiring reconciliation, got %v", err)
	}
}

func TestWooviCreateRejectsMalformedProviderResponse(t *testing.T) {
	for name, body := range map[string]string{
		"missing charge":         `{}`,
		"fractional cent amount": `{"charge":{"identifier":"id","correlationID":"order","status":"ACTIVE","value":12.5}}`,
		"missing identifier":     `{"charge":{"correlationID":"order","status":"ACTIVE","value":1200}}`,
		"trailing JSON value":    `{"charge":{"identifier":"id","correlationID":"order","status":"ACTIVE","value":1200}} {"unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()

			_, err := NewWooviClient(server.URL, "test-secret", server.Client()).CreateCharge(context.Background(), CreateChargeRequest{CorrelationID: "order", AmountCents: 1200, ExpiresInSeconds: 300})
			if err == nil {
				t.Fatal("expected malformed response error")
			}
		})
	}
}

func TestCreateChargeRejectsInvalidInputBeforeCallingProvider(t *testing.T) {
	called := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true

		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	for _, input := range []CreateChargeRequest{
		{CorrelationID: "order-1", AmountCents: 0, ExpiresInSeconds: 300},
		{CorrelationID: "order-1", AmountCents: 101, ExpiresInSeconds: 299},
		{CorrelationID: "  ", AmountCents: 100, ExpiresInSeconds: 300},
	} {
		if _, err := NewWooviClient(server.URL, "test", server.Client()).CreateCharge(context.Background(), input); err == nil {
			t.Fatalf("expected validation error for %+v", input)
		}
	}

	if called {
		t.Fatal("provider was called for invalid input")
	}
}
