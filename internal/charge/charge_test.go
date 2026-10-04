package charge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"golang.org/x/time/rate"
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

func TestWooviClientHonorsCancelledContextBeforeProviderCall(t *testing.T) {
	called := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true

		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewWooviClient(server.URL, "test", server.Client())
	client.limiter = rate.NewLimiter(0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.GetCharge(ctx, "charge-1"); err == nil {
		t.Fatal("expected cancelled request")
	}

	if called {
		t.Fatal("provider request was sent after context cancellation")
	}
}

func TestWooviProviderHTTPFailuresAreBoundedAndSanitized(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"secret":"private provider detail"}`, status)
			}))
			defer server.Close()

			client := NewWooviClient(server.URL, "test-secret", server.Client())

			_, err := client.GetCharge(context.Background(), "charge-1")
			if err == nil || err.Error() != "provider returned HTTP "+strconv.Itoa(status) || contains(err.Error(), "private") || contains(err.Error(), "test-secret") {
				t.Fatalf("unsanitized provider error: %v", err)
			}

			_, err = client.CreateCharge(context.Background(), CreateChargeRequest{CorrelationID: "order", AmountCents: 100, ExpiresInSeconds: 300})
			if err == nil || err.Error() != "provider returned HTTP "+strconv.Itoa(status) || contains(err.Error(), "private") || contains(err.Error(), "test-secret") {
				t.Fatalf("unsanitized create error: %v", err)
			}
		})
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
