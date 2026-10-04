package charge

import (
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
