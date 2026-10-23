package charge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSimulatorRequiresItsLocalTestCredential(t *testing.T) {
	simulator := NewSimulator()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/charge/demo-charge", nil)
	response := httptest.NewRecorder()
	simulator.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without simulator credential, got %d", response.Code)
	}
}

func TestSimulatorCancelsSyntheticSubscriptionIdempotently(t *testing.T) {
	simulator := NewSimulator()

	response := subscriptionRequest(simulator, http.MethodPut, "/api/v1/subscriptions/demo-subscription/cancel", "simulator")
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected cancellation status %d, got %d", http.StatusNoContent, response.Code)
	}

	response = subscriptionRequest(simulator, http.MethodPut, "/api/v1/subscriptions/demo-subscription/cancel", "simulator")
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected repeated cancellation status %d, got %d", http.StatusNoContent, response.Code)
	}

	response = subscriptionRequest(simulator, http.MethodGet, "/api/v1/subscriptions/demo-subscription", "simulator")
	if response.Code != http.StatusOK {
		t.Fatalf("expected subscription read status %d, got %d", http.StatusOK, response.Code)
	}

	var body struct {
		Subscription struct {
			GlobalID string `json:"globalID"`
			Status   string `json:"status"`
		} `json:"subscription"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode subscription response: %v", err)
	}

	if body.Subscription.GlobalID != "demo-subscription" || body.Subscription.Status != "CANCELED" {
		t.Fatalf("unexpected subscription after cancellation: %+v", body.Subscription)
	}
}

func TestSimulatorSubscriptionCancellationRequiresLocalTestCredential(t *testing.T) {
	simulator := NewSimulator()

	response := subscriptionRequest(simulator, http.MethodPut, "/api/v1/subscriptions/demo-subscription/cancel", "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized cancellation status %d, got %d", http.StatusUnauthorized, response.Code)
	}

	response = subscriptionRequest(simulator, http.MethodGet, "/api/v1/subscriptions/demo-subscription", "simulator")
	if response.Code != http.StatusOK {
		t.Fatalf("expected subscription read status %d, got %d", http.StatusOK, response.Code)
	}

	var body struct {
		Subscription struct {
			Status string `json:"status"`
		} `json:"subscription"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode subscription response: %v", err)
	}

	if body.Subscription.Status != "APPROVED" {
		t.Fatalf("unauthorized cancellation changed status to %q", body.Subscription.Status)
	}
}

func TestSimulatorReturnsNotFoundForUnknownSubscription(t *testing.T) {
	simulator := NewSimulator()
	response := subscriptionRequest(simulator, http.MethodPut, "/api/v1/subscriptions/unknown/cancel", "simulator")

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected unknown subscription status %d, got %d", http.StatusNotFound, response.Code)
	}
}

func subscriptionRequest(simulator *Simulator, method, path, appID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if appID != "" {
		request.Header.Set("Authorization", appID)
	}

	response := httptest.NewRecorder()
	simulator.ServeHTTP(response, request)

	return response
}
