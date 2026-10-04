package charge

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Simulator is a local, deterministic HTTP stand-in for Woovi's GET charge API.
type Simulator struct {
	mu      sync.RWMutex
	charges map[string]Charge
}

func NewSimulator() *Simulator {
	return &Simulator{charges: map[string]Charge{
		"demo-charge": {ID: "demo-charge", Reference: "demo-order-001", Status: "ACTIVE", AmountCents: 1250, Currency: "BRL", PixCode: "000201-DEMO-PIX-COPY-CODE"},
	}}
}

func NewWooviSimulatorWithCreate() *Simulator {
	return &Simulator{charges: map[string]Charge{
		"demo-charge": {ID: "demo-charge", Reference: "demo-order-001", Status: "ACTIVE", AmountCents: 1250, Currency: "BRL", PixCode: "000201-DEMO-PIX-COPY-CODE"},
	}}
}

func (s *Simulator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == "/api/v1/charge" {
		s.createCharge(w, r)
		return
	}
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/v1/charge/") {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Authorization") != "simulator" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/charge/")
	s.mu.RLock()
	charge, ok := s.charges[id]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, `{"error":"charge not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"charge": map[string]any{
		"identifier": charge.ID, "correlationID": charge.Reference, "status": charge.Status,
		"value": charge.AmountCents, "brCode": charge.PixCode,
	}})
}

func (s *Simulator) createCharge(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "simulator" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var request struct {
		CorrelationID string `json:"correlationID"`
		Value         int64  `json:"value"`
		ExpiresIn     int64  `json:"expiresIn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.CorrelationID == "" || request.Value < 1 || request.ExpiresIn < 300 {
		http.Error(w, `{"error":"invalid charge request"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.charges {
		if existing.Reference == request.CorrelationID {
			writeCharge(w, existing)
			return
		}
	}
	id := "sim-" + strconv.FormatInt(int64(len(s.charges)+1), 10)
	created := Charge{ID: id, Reference: request.CorrelationID, Status: "ACTIVE", AmountCents: request.Value, Currency: "BRL", ExpiresAt: time.Now().Add(time.Duration(request.ExpiresIn) * time.Second), PixCode: "000201-SIMULATED-PIX-CODE"}
	s.charges[id] = created
	writeCharge(w, created)
}

func writeCharge(w http.ResponseWriter, item Charge) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"charge": map[string]any{
		"identifier": item.ID, "correlationID": item.Reference, "status": item.Status,
		"value": item.AmountCents, "expiresDate": item.ExpiresAt.Format(time.RFC3339Nano), "brCode": item.PixCode,
	}})
}
