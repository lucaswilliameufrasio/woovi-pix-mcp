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

func TestSimulatorReturnsSubscriptionAndPaginatedInstallments(t *testing.T) {
	simulator := NewSimulator()

	response := subscriptionRequest(simulator, http.MethodGet, "/api/v1/subscriptions/demo-subscription", "simulator")
	if response.Code != http.StatusOK {
		t.Fatalf("expected subscription status %d, got %d", http.StatusOK, response.Code)
	}

	var subscriptionBody struct {
		Subscription struct {
			GlobalID      string `json:"globalID"`
			CorrelationID string `json:"correlationID"`
			PixRecurring  struct {
				Status string `json:"status"`
			} `json:"pixRecurring"`
		} `json:"subscription"`
	}
	if err := json.NewDecoder(response.Body).Decode(&subscriptionBody); err != nil {
		t.Fatalf("decode subscription response: %v", err)
	}

	if subscriptionBody.Subscription.GlobalID != "demo-subscription" ||
		subscriptionBody.Subscription.CorrelationID != "demo-subscription-correlation" ||
		subscriptionBody.Subscription.PixRecurring.Status != "APPROVED" {
		t.Fatalf("unexpected subscription snapshot: %+v", subscriptionBody.Subscription)
	}

	response = subscriptionRequest(
		simulator,
		http.MethodGet,
		"/api/v1/subscriptions/demo-subscription/installments?skip=0&limit=1",
		"simulator",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected first installment page status %d, got %d", http.StatusOK, response.Code)
	}

	var firstPage struct {
		Installments []struct {
			GlobalID           string         `json:"globalID"`
			Status             string         `json:"status"`
			InstallmentNumber  int            `json:"installmentNumber"`
			Value              int64          `json:"value"`
			DateGenerateCharge string         `json:"dateGenerateCharge"`
			Cobr               map[string]any `json:"cobr"`
		} `json:"installments"`
		PageInfo struct {
			Skip        int  `json:"skip"`
			Limit       int  `json:"limit"`
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
	}
	if err := json.NewDecoder(response.Body).Decode(&firstPage); err != nil {
		t.Fatalf("decode first installment page: %v", err)
	}

	if len(firstPage.Installments) != 1 || firstPage.Installments[0].GlobalID != "demo-installment-001" ||
		firstPage.Installments[0].Status != "COMPLETED" || firstPage.Installments[0].Value != 1_250 ||
		firstPage.Installments[0].Cobr["identifierId"] != "demo-charge-001" ||
		firstPage.PageInfo.Skip != 0 || firstPage.PageInfo.Limit != 1 || !firstPage.PageInfo.HasNextPage {
		t.Fatalf("unexpected first installment page: %+v", firstPage)
	}

	response = subscriptionRequest(
		simulator,
		http.MethodGet,
		"/api/v1/subscriptions/demo-subscription/installments?skip=1&limit=1",
		"simulator",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected second installment page status %d, got %d", http.StatusOK, response.Code)
	}

	var secondPage struct {
		Installments []struct {
			GlobalID string `json:"globalID"`
			Status   string `json:"status"`
		} `json:"installments"`
		PageInfo struct {
			Skip        int  `json:"skip"`
			Limit       int  `json:"limit"`
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
	}
	if err := json.NewDecoder(response.Body).Decode(&secondPage); err != nil {
		t.Fatalf("decode second installment page: %v", err)
	}

	if len(secondPage.Installments) != 1 || secondPage.Installments[0].GlobalID != "demo-installment-002" ||
		secondPage.Installments[0].Status != "SCHEDULED" || secondPage.PageInfo.Skip != 1 ||
		secondPage.PageInfo.Limit != 1 || secondPage.PageInfo.HasNextPage {
		t.Fatalf("unexpected second installment page: %+v", secondPage)
	}
}

func TestSimulatorInstallmentReadRequiresCredentialAndBoundedPagination(t *testing.T) {
	simulator := NewSimulator()

	response := subscriptionRequest(
		simulator,
		http.MethodGet,
		"/api/v1/subscriptions/demo-subscription/installments?skip=0&limit=1",
		"",
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized installment status %d, got %d", http.StatusUnauthorized, response.Code)
	}

	for _, query := range []string{"skip=-1&limit=1", "skip=0&limit=101", "skip=0&limit=0"} {
		response = subscriptionRequest(
			simulator,
			http.MethodGet,
			"/api/v1/subscriptions/demo-subscription/installments?"+query,
			"simulator",
		)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid pagination status %d for %q, got %d", http.StatusBadRequest, query, response.Code)
		}
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
