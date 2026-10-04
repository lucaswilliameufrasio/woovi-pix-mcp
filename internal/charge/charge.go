package charge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Charge is the deliberately minimal public view of a Woovi charge.
type Charge struct {
	ID          string    `json:"id"`
	Reference   string    `json:"reference"`
	Status      string    `json:"status"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	PixCode     string    `json:"pix_code,omitempty"`
}

type Client interface {
	GetCharge(context.Context, string) (Charge, error)
}

type CreateChargeRequest struct {
	CorrelationID    string
	AmountCents      int64
	ExpiresInSeconds int64
}

type ChargeCreator interface {
	CreateCharge(context.Context, CreateChargeRequest) (Charge, error)
}

type WooviClient struct {
	baseURL string
	appID   string
	http    *http.Client
	limiter *rate.Limiter
}

func NewWooviClient(baseURL, appID string, httpClient *http.Client) *WooviClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &WooviClient{baseURL: strings.TrimRight(baseURL, "/"), appID: appID, http: httpClient, limiter: rate.NewLimiter(10, 1)}
}

func (c *WooviClient) GetCharge(ctx context.Context, id string) (Charge, error) {
	if strings.TrimSpace(id) == "" || len(id) > 256 {
		return Charge{}, errors.New("invalid charge identifier")
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return Charge{}, errors.New("provider request was cancelled before dispatch")
	}

	base, err := url.Parse(c.baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return Charge{}, errors.New("invalid provider base URL configuration")
	}

	escapedPath := strings.TrimRight(base.EscapedPath(), "/") + "/api/v1/charge/" + url.PathEscape(id)

	base.Path, err = url.PathUnescape(escapedPath)
	if err != nil {
		return Charge{}, errors.New("invalid charge identifier")
	}

	base.RawPath = escapedPath
	endpoint := base.String()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Charge{}, errors.New("unable to prepare provider request")
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", c.appID)

	resp, err := c.http.Do(req)
	if err != nil {
		return Charge{}, errors.New("provider request failed")
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Charge{}, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}

	return decodeCharge(resp.Body)
}

type createChargeRequest struct {
	CorrelationID string `json:"correlationID"`
	Value         int64  `json:"value"`
	ExpiresIn     int64  `json:"expiresIn,omitempty"`
}

func (c *WooviClient) CreateCharge(ctx context.Context, input CreateChargeRequest) (Charge, error) {
	if err := ValidateCreateCharge(input); err != nil {
		return Charge{}, err
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return Charge{}, errors.New("provider request was cancelled before dispatch")
	}

	base, err := url.Parse(c.baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return Charge{}, errors.New("invalid provider base URL configuration")
	}

	base.Path = strings.TrimRight(base.Path, "/") + "/api/v1/charge"
	query := base.Query()
	query.Set("return_existing", "true")
	base.RawQuery = query.Encode()

	body, err := json.Marshal(createChargeRequest{CorrelationID: input.CorrelationID, Value: input.AmountCents, ExpiresIn: input.ExpiresInSeconds})
	if err != nil {
		return Charge{}, errors.New("unable to encode charge request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), strings.NewReader(string(body)))
	if err != nil {
		return Charge{}, errors.New("unable to prepare provider request")
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.appID)

	resp, err := c.http.Do(req)
	if err != nil {
		return Charge{}, errors.New("provider request outcome is unknown; reconcile by correlation ID before retrying")
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Charge{}, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}

	return decodeCharge(resp.Body)
}

func ValidateCreateCharge(input CreateChargeRequest) error {
	if strings.TrimSpace(input.CorrelationID) == "" || len(input.CorrelationID) > 256 {
		return errors.New("invalid charge reference")
	}

	if input.AmountCents < 1 || input.AmountCents > 100_000_00 {
		return errors.New("charge amount is outside the configured limit")
	}

	if input.ExpiresInSeconds < 300 || input.ExpiresInSeconds > 2_592_000 {
		return errors.New("charge expiration is outside the allowed range")
	}

	return nil
}

func decodeCharge(body io.Reader) (Charge, error) {
	var payload struct {
		Charge struct {
			Identifier    string      `json:"identifier"`
			CorrelationID string      `json:"correlationID"`
			Status        string      `json:"status"`
			Value         json.Number `json:"value"`
			ExpiresDate   string      `json:"expiresDate"`
			BrCode        string      `json:"brCode"`
		} `json:"charge"`
	}

	decoder := json.NewDecoder(io.LimitReader(body, 1<<20))
	decoder.UseNumber()

	if err := decoder.Decode(&payload); err != nil {
		return Charge{}, errors.New("provider returned an invalid charge response")
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Charge{}, errors.New("provider returned an invalid charge response")
	}

	p := payload.Charge

	value, err := strconv.ParseInt(string(p.Value), 10, 64)
	if p.Identifier == "" || p.Status == "" || err != nil || value < 0 {
		return Charge{}, errors.New("provider returned an incomplete charge")
	}

	var expiresAt time.Time
	if p.ExpiresDate != "" {
		expiresAt, err = time.Parse(time.RFC3339Nano, p.ExpiresDate)
		if err != nil {
			return Charge{}, errors.New("provider returned an invalid expiration date")
		}
	}

	return Charge{ID: p.Identifier, Reference: p.CorrelationID, Status: p.Status, AmountCents: value, Currency: "BRL", ExpiresAt: expiresAt, PixCode: p.BrCode}, nil
}
