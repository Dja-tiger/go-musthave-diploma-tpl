package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/auth"
	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/domain"
)

func TestRegisterUploadAndListOrders(t *testing.T) {
	store := newFakeStore()
	handler := newTestServer(t, store)

	register := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{"login":"alice","password":"secret"}`))
	register.Header.Set("Content-Type", "application/json")
	registerResponse := httptest.NewRecorder()
	handler.ServeHTTP(registerResponse, register)
	if registerResponse.Code != http.StatusOK {
		t.Fatalf("register status = %d", registerResponse.Code)
	}

	authHeader := registerResponse.Header().Get("Authorization")
	upload := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678903"))
	upload.Header.Set("Authorization", authHeader)
	uploadResponse := httptest.NewRecorder()
	handler.ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusAccepted {
		t.Fatalf("upload status = %d", uploadResponse.Code)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/user/orders", http.NoBody)
	list.Header.Set("Authorization", authHeader)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d", listResponse.Code)
	}
	if !bytes.Contains(listResponse.Body.Bytes(), []byte(`"number":"12345678903"`)) {
		t.Fatalf("list body = %s", listResponse.Body.String())
	}
}

func TestProtectedEndpointRequiresAuth(t *testing.T) {
	handler := newTestServer(t, newFakeStore())
	request := httptest.NewRequest(http.MethodGet, "/api/user/balance", http.NoBody)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestUploadRejectsBadOrderNumber(t *testing.T) {
	store := newFakeStore()
	handler := newTestServer(t, store)
	token := registerAndToken(t, handler)

	request := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678904"))
	request.Header.Set("Authorization", token)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.Code)
	}
}

func TestUploadReadsLongOrderBody(t *testing.T) {
	store := newFakeStore()
	handler := newTestServer(t, store)
	token := registerAndToken(t, handler)
	number := strings.Repeat("0", 1200)

	request := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader(number))
	request.Header.Set("Authorization", token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.Code)
	}
	if _, ok := store.orders[number]; !ok {
		t.Fatal("long order number was not stored intact")
	}
}

func TestRegisterRejectsTrailingJSON(t *testing.T) {
	handler := newTestServer(t, newFakeStore())
	request := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{"login":"alice","password":"secret"} trailing`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestUploadOrderConflicts(t *testing.T) {
	store := newFakeStore()
	handler := newTestServer(t, store)
	firstToken := registerAndTokenWithLogin(t, handler, "first")

	firstUpload := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678903"))
	firstUpload.Header.Set("Authorization", firstToken)
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, firstUpload)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first upload status = %d", firstResponse.Code)
	}

	sameUpload := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678903"))
	sameUpload.Header.Set("Authorization", firstToken)
	sameResponse := httptest.NewRecorder()
	handler.ServeHTTP(sameResponse, sameUpload)
	if sameResponse.Code != http.StatusOK {
		t.Fatalf("same upload status = %d", sameResponse.Code)
	}

	secondToken := registerAndTokenWithLogin(t, handler, "second")
	otherUpload := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678903"))
	otherUpload.Header.Set("Authorization", secondToken)
	otherResponse := httptest.NewRecorder()
	handler.ServeHTTP(otherResponse, otherUpload)
	if otherResponse.Code != http.StatusConflict {
		t.Fatalf("other upload status = %d", otherResponse.Code)
	}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	handler := newTestServer(t, newFakeStore())
	_ = registerAndTokenWithLogin(t, handler, "alice")

	request := httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(`{"login":"alice","password":"bad"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestBalanceWithdrawAndWithdrawals(t *testing.T) {
	store := newFakeStore()
	handler := newTestServer(t, store)
	token := registerAndToken(t, handler)
	store.balances[1] = domain.Balance{Current: 100, Withdrawn: 10}

	balanceRequest := httptest.NewRequest(http.MethodGet, "/api/user/balance", http.NoBody)
	balanceRequest.Header.Set("Authorization", token)
	balanceResponse := httptest.NewRecorder()
	handler.ServeHTTP(balanceResponse, balanceRequest)
	if balanceResponse.Code != http.StatusOK {
		t.Fatalf("balance status = %d", balanceResponse.Code)
	}
	if !bytes.Contains(balanceResponse.Body.Bytes(), []byte(`"current":100`)) {
		t.Fatalf("balance body = %s", balanceResponse.Body.String())
	}

	withdrawRequest := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(`{"order":"2377225624","sum":50}`))
	withdrawRequest.Header.Set("Authorization", token)
	withdrawResponse := httptest.NewRecorder()
	handler.ServeHTTP(withdrawResponse, withdrawRequest)
	if withdrawResponse.Code != http.StatusOK {
		t.Fatalf("withdraw status = %d", withdrawResponse.Code)
	}

	withdrawalsRequest := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", http.NoBody)
	withdrawalsRequest.Header.Set("Authorization", token)
	withdrawalsResponse := httptest.NewRecorder()
	handler.ServeHTTP(withdrawalsResponse, withdrawalsRequest)
	if withdrawalsResponse.Code != http.StatusOK {
		t.Fatalf("withdrawals status = %d", withdrawalsResponse.Code)
	}
	if !bytes.Contains(withdrawalsResponse.Body.Bytes(), []byte(`"order":"2377225624"`)) {
		t.Fatalf("withdrawals body = %s", withdrawalsResponse.Body.String())
	}
}

func TestListOrdersNoContent(t *testing.T) {
	handler := newTestServer(t, newFakeStore())
	token := registerAndToken(t, handler)

	request := httptest.NewRequest(http.MethodGet, "/api/user/orders", http.NoBody)
	request.Header.Set("Authorization", token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
}

func newTestServer(t *testing.T, store *fakeStore) http.Handler {
	t.Helper()
	tokens, err := auth.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	return New(store, tokens).Handler()
}

func registerAndToken(t *testing.T, handler http.Handler) string {
	t.Helper()
	return registerAndTokenWithLogin(t, handler, "bob")
}

func registerAndTokenWithLogin(t *testing.T, handler http.Handler, login string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{"login":"`+login+`","password":"secret"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("register status = %d", response.Code)
	}
	return response.Header().Get("Authorization")
}

type fakeStore struct {
	mu          sync.Mutex
	nextUserID  int64
	users       map[string]domain.User
	orders      map[string]domain.StoredOrder
	orderOwners map[string]int64
	withdrawals map[int64][]domain.StoredWithdrawal
	balances    map[int64]domain.Balance
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		nextUserID:  1,
		users:       make(map[string]domain.User),
		orders:      make(map[string]domain.StoredOrder),
		orderOwners: make(map[string]int64),
		withdrawals: make(map[int64][]domain.StoredWithdrawal),
		balances:    make(map[int64]domain.Balance),
	}
}

func (s *fakeStore) CreateUser(ctx context.Context, login, passwordHash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[login]; ok {
		return 0, domain.ErrLoginTaken
	}
	id := s.nextUserID
	s.nextUserID++
	s.users[login] = domain.User{ID: id, Login: login, PasswordHash: passwordHash}
	return id, nil
}

func (s *fakeStore) UserByLogin(ctx context.Context, login string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[login]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}

func (s *fakeStore) SaveOrder(ctx context.Context, userID int64, number string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ownerID, ok := s.orderOwners[number]; ok {
		if ownerID == userID {
			return domain.ErrOrderSameUser
		}
		return domain.ErrOrderOtherUser
	}
	s.orderOwners[number] = userID
	s.orders[number] = domain.StoredOrder{
		Number:     number,
		Status:     domain.StatusNew,
		UploadedAt: time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC),
	}
	return nil
}

func (s *fakeStore) ListOrders(ctx context.Context, userID int64) ([]domain.StoredOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var orders []domain.StoredOrder
	for number, ownerID := range s.orderOwners {
		if ownerID == userID {
			orders = append(orders, s.orders[number])
		}
	}
	return orders, nil
}

func (s *fakeStore) Balance(ctx context.Context, userID int64) (domain.Balance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.balances[userID], nil
}

func (s *fakeStore) Withdraw(ctx context.Context, userID int64, order string, sum float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	balance := s.balances[userID]
	if balance.Current < sum {
		return domain.ErrInsufficientFund
	}
	balance.Current -= sum
	balance.Withdrawn += sum
	s.balances[userID] = balance
	s.withdrawals[userID] = append(s.withdrawals[userID], domain.StoredWithdrawal{Order: order, Sum: sum, ProcessedAt: time.Now()})
	return nil
}

func (s *fakeStore) ListWithdrawals(ctx context.Context, userID int64) ([]domain.StoredWithdrawal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	withdrawals, ok := s.withdrawals[userID]
	if !ok {
		return nil, nil
	}
	return withdrawals, nil
}

var _ Store = (*fakeStore)(nil)
