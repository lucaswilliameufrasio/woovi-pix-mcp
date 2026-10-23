package charge

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Simulator is a local, deterministic HTTP stand-in for a small Woovi API subset.
type Simulator struct {
	mu            sync.RWMutex
	charges       map[string]Charge
	subscriptions map[string]simulatedSubscription
}

type simulatedSubscription struct {
	GlobalID        string
	CorrelationID   string
	Status          string
	RecurringStatus string
	Installments    []simulatedInstallment
}

type simulatedInstallment struct {
	GlobalID           string
	Status             string
	InstallmentNumber  int
	Value              int64
	DateGenerateCharge string
	ChargeID           string
}

func NewSimulator() *Simulator {
	return &Simulator{
		charges: map[string]Charge{
			"demo-charge": {ID: "demo-charge", Reference: "demo-order-001", Status: "ACTIVE", AmountCents: 1250, Currency: "BRL", PixCode: "000201-DEMO-PIX-COPY-CODE"},
		},
		subscriptions: map[string]simulatedSubscription{
			"demo-subscription": {
				GlobalID:        "demo-subscription",
				CorrelationID:   "demo-subscription-correlation",
				Status:          "APPROVED",
				RecurringStatus: "APPROVED",
				Installments: []simulatedInstallment{
					{
						GlobalID:           "demo-installment-001",
						Status:             "COMPLETED",
						InstallmentNumber:  1,
						Value:              1_250,
						DateGenerateCharge: "2026-01-01T00:00:00Z",
						ChargeID:           "demo-charge-001",
					},
					{
						GlobalID:           "demo-installment-002",
						Status:             "SCHEDULED",
						InstallmentNumber:  2,
						Value:              1_250,
						DateGenerateCharge: "2026-02-01T00:00:00Z",
					},
				},
			},
		},
	}
}

func NewWooviSimulatorWithCreate() *Simulator {
	return NewSimulator()
}

func (s *Simulator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/v1/subscriptions/") {
		s.serveSubscription(w, r)
		return
	}

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

func (s *Simulator) serveSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "simulator" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	const prefix = "/api/v1/subscriptions/"

	resource := strings.TrimPrefix(r.URL.Path, prefix)
	if r.Method == http.MethodPut && strings.HasSuffix(resource, "/cancel") {
		id := strings.TrimSuffix(resource, "/cancel")
		s.cancelSubscription(w, id)

		return
	}

	if r.Method == http.MethodGet {
		if strings.HasSuffix(resource, "/installments") {
			id := strings.TrimSuffix(resource, "/installments")
			s.getInstallments(w, r, id)

			return
		}

		s.getSubscription(w, resource)

		return
	}

	http.NotFound(w, r)
}

func (s *Simulator) cancelSubscription(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscription, ok := s.subscriptions[id]
	if !ok {
		http.Error(w, `{"error":"subscription not found"}`, http.StatusNotFound)
		return
	}

	subscription.Status = "CANCELED"
	subscription.RecurringStatus = "CANCELED"
	s.subscriptions[id] = subscription

	w.WriteHeader(http.StatusNoContent)
}

func (s *Simulator) getSubscription(w http.ResponseWriter, id string) {
	s.mu.RLock()
	subscription, ok := s.subscriptions[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, `{"error":"subscription not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"subscription": map[string]any{
		"globalID":      subscription.GlobalID,
		"correlationID": subscription.CorrelationID,
		"status":        subscription.Status,
		"pixRecurring": map[string]string{
			"status": subscription.RecurringStatus,
		},
	}})
}

func (s *Simulator) getInstallments(w http.ResponseWriter, r *http.Request, id string) {
	query := r.URL.Query()

	skip, err := strconv.Atoi(query.Get("skip"))
	if err != nil || skip < 0 {
		http.Error(w, `{"error":"invalid pagination"}`, http.StatusBadRequest)
		return
	}

	limit, err := strconv.Atoi(query.Get("limit"))
	if err != nil || limit < 1 || limit > 100 {
		http.Error(w, `{"error":"invalid pagination"}`, http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	subscription, ok := s.subscriptions[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, `{"error":"subscription not found"}`, http.StatusNotFound)
		return
	}

	start := min(skip, len(subscription.Installments))
	end := min(start+limit, len(subscription.Installments))
	rows := make([]map[string]any, 0, end-start)

	for _, installment := range subscription.Installments[start:end] {
		row := map[string]any{
			"globalID":           installment.GlobalID,
			"status":             installment.Status,
			"installmentNumber":  installment.InstallmentNumber,
			"value":              installment.Value,
			"dateGenerateCharge": installment.DateGenerateCharge,
		}

		if installment.ChargeID != "" {
			row["cobr"] = map[string]string{"identifierId": installment.ChargeID}
		}

		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"installments": rows,
		"pageInfo": map[string]any{
			"skip":        skip,
			"limit":       limit,
			"hasNextPage": end < len(subscription.Installments),
		},
	})
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
