package charge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")

type OperationStatus string

const (
	OperationPending   OperationStatus = "PENDING"
	OperationUnknown   OperationStatus = "UNKNOWN"
	OperationCompleted OperationStatus = "COMPLETED"
	OperationFailed    OperationStatus = "FAILED"
)

type Operation struct {
	ID          string
	Status      OperationStatus
	Charge      Charge
	PayloadHash string
	CreatedAt   time.Time
}

func newOperationID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func RequestHash(input CreateChargeRequest) (string, error) {
	canonical, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
