package accrual

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/domain"
)

func TestClientGetOrderOK(t *testing.T) {
	client := NewClient("http://accrual.test")
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/orders/12345678903" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		return response(http.StatusOK, `{"order":"12345678903","status":"PROCESSED","accrual":10.5}`, nil), nil
	})

	result, err := client.GetOrder(context.Background(), "12345678903")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Status != "PROCESSED" || result.Accrual == nil || *result.Accrual != 10.5 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestClientGetOrderNoContent(t *testing.T) {
	client := NewClient("http://accrual.test")
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(http.StatusNoContent, "", nil), nil
	})

	result, err := client.GetOrder(context.Background(), "12345678903")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
}

func TestClientGetOrderRateLimited(t *testing.T) {
	client := NewClient("http://accrual.test")
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(http.StatusTooManyRequests, "", http.Header{"Retry-After": []string{"3"}}), nil
	})

	_, err := client.GetOrder(context.Background(), "12345678903")
	var retry RetryError
	if !errors.As(err, &retry) {
		t.Fatalf("err = %v, want RetryError", err)
	}
	if retry.After != 3*time.Second {
		t.Fatalf("retry.After = %s, want 3s", retry.After)
	}
}

func TestWorkerTouchesUnknownOrder(t *testing.T) {
	store := &workerStore{pending: []string{"12345678903"}}
	client := NewClient("http://accrual.test")
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(http.StatusNoContent, "", nil), nil
	})

	delay := NewWorker(store, client).process(context.Background())

	if delay != time.Second {
		t.Fatalf("delay = %s, want 1s", delay)
	}
	if len(store.touched) != 1 || store.touched[0] != "12345678903" {
		t.Fatalf("touched = %#v", store.touched)
	}
	if len(store.updated) != 0 {
		t.Fatalf("updated = %#v, want empty", store.updated)
	}
}

func TestWorkerUpdatesKnownOrder(t *testing.T) {
	accrual := 15.5
	store := &workerStore{pending: []string{"12345678903"}}
	client := NewClient("http://accrual.test")
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"order":"12345678903","status":"PROCESSED","accrual":15.5}`, nil), nil
	})

	_ = NewWorker(store, client).process(context.Background())

	if len(store.updated) != 1 {
		t.Fatalf("updated = %#v", store.updated)
	}
	update := store.updated[0]
	if update.number != "12345678903" || update.status != domain.StatusProcessed || update.accrual == nil || *update.accrual != accrual {
		t.Fatalf("unexpected update: %#v", update)
	}
}

func TestWorkerHonorsRetryAfter(t *testing.T) {
	store := &workerStore{pending: []string{"12345678903", "79927398713"}}
	client := NewClient("http://accrual.test")
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(http.StatusTooManyRequests, "", http.Header{"Retry-After": []string{"7"}}), nil
	})

	delay := NewWorker(store, client).process(context.Background())

	if delay != 7*time.Second {
		t.Fatalf("delay = %s, want 7s", delay)
	}
	if len(store.touched) != 0 {
		t.Fatalf("touched = %#v, want empty on retry", store.touched)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func response(status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type workerStore struct {
	pending []string
	touched []string
	updated []workerUpdate
}

type workerUpdate struct {
	number  string
	status  string
	accrual *float64
}

func (s *workerStore) PendingOrders(ctx context.Context, limit int) ([]string, error) {
	if len(s.pending) > limit {
		return s.pending[:limit], nil
	}
	return s.pending, nil
}

func (s *workerStore) TouchOrderAttempt(ctx context.Context, number string) error {
	s.touched = append(s.touched, number)
	return nil
}

func (s *workerStore) UpdateOrderAccrual(ctx context.Context, number, status string, accrual *float64) error {
	s.updated = append(s.updated, workerUpdate{number: number, status: status, accrual: accrual})
	return nil
}
