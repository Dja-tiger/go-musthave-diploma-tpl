// Package accrual integrates with the external loyalty accrual service.
package accrual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/domain"
)

// Result is an accrual service response.
type Result struct {
	Order   string   `json:"order"`
	Status  string   `json:"status"`
	Accrual *float64 `json:"accrual,omitempty"`
}

// RetryError signals service-side rate limiting.
type RetryError struct {
	After time.Duration
}

// Error implements the error interface.
func (e RetryError) Error() string {
	return "accrual rate limit exceeded"
}

// Client fetches order accrual information.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates an accrual API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// GetOrder returns accrual information. A nil result means the order is not
// known by the accrual service yet.
func (c *Client) GetOrder(ctx context.Context, number string) (*Result, error) {
	if c.baseURL == "" {
		return nil, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/orders/"+number, http.NoBody)
	if err != nil {
		return nil, err
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		var result Result
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			return nil, err
		}
		if result.Status == "" {
			return nil, errors.New("empty accrual status")
		}
		return &result, nil
	case http.StatusNoContent:
		return nil, nil
	case http.StatusTooManyRequests:
		return nil, RetryError{After: retryAfter(response.Header.Get("Retry-After"))}
	default:
		return nil, fmt.Errorf("unexpected accrual status: %d", response.StatusCode)
	}
}

func retryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return time.Second
	}
	return time.Duration(seconds) * time.Second
}

// Store is the storage subset used by Worker.
type Store interface {
	PendingOrders(ctx context.Context, limit int) ([]string, error)
	TouchOrderAttempt(ctx context.Context, number string) error
	UpdateOrderAccrual(ctx context.Context, number, status string, accrual *float64) error
}

// Worker periodically updates pending orders from the accrual service.
type Worker struct {
	store  Store
	client *Client
}

// NewWorker creates a background accrual worker.
func NewWorker(store Store, client *Client) *Worker {
	return &Worker{store: store, client: client}
}

// Run starts polling until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	if w.client == nil || w.client.baseURL == "" {
		return
	}

	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			delay := w.process(ctx)
			timer.Reset(delay)
		}
	}
}

func (w *Worker) process(ctx context.Context) time.Duration {
	numbers, err := w.store.PendingOrders(ctx, 20)
	if err != nil {
		slog.Warn("failed to fetch pending accrual orders", "error", err)
		return time.Second
	}
	if len(numbers) == 0 {
		return time.Second
	}

	for _, number := range numbers {
		result, err := w.client.GetOrder(ctx, number)
		if err != nil {
			var retry RetryError
			if errors.As(err, &retry) {
				return retry.After
			}
			if touchErr := w.store.TouchOrderAttempt(ctx, number); touchErr != nil {
				slog.Warn("failed to touch order accrual attempt", "order", number, "error", touchErr)
			}
			slog.Warn("failed to fetch order accrual", "order", number, "error", err)
			continue
		}
		if result == nil {
			if touchErr := w.store.TouchOrderAttempt(ctx, number); touchErr != nil {
				slog.Warn("failed to touch order accrual attempt", "order", number, "error", touchErr)
			}
			continue
		}
		status := normalizeStatus(result.Status)
		if status == "" {
			slog.Warn("unknown accrual status", "order", number, "status", result.Status)
			continue
		}
		if err := w.store.UpdateOrderAccrual(ctx, number, status, result.Accrual); err != nil {
			slog.Warn("failed to update order accrual", "order", number, "status", status, "error", err)
		}
	}

	return time.Second
}

func normalizeStatus(status string) string {
	switch strings.ToUpper(status) {
	case domain.StatusRegistered:
		return domain.StatusRegistered
	case domain.StatusProcessing:
		return domain.StatusProcessing
	case domain.StatusInvalid:
		return domain.StatusInvalid
	case domain.StatusProcessed:
		return domain.StatusProcessed
	default:
		return ""
	}
}
