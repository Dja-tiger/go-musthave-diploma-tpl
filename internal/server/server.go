// Package server exposes the Gophermart HTTP API.
package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/auth"
	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/domain"
	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/luhn"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const userIDKey contextKey = "user_id"

// Store is the persistence contract used by HTTP handlers.
type Store interface {
	CreateUser(ctx context.Context, login, passwordHash string) (int64, error)
	UserByLogin(ctx context.Context, login string) (domain.User, error)
	SaveOrder(ctx context.Context, userID int64, number string) error
	ListOrders(ctx context.Context, userID int64) ([]domain.StoredOrder, error)
	Balance(ctx context.Context, userID int64) (domain.Balance, error)
	Withdraw(ctx context.Context, userID int64, order string, sum float64) error
	ListWithdrawals(ctx context.Context, userID int64) ([]domain.StoredWithdrawal, error)
}

// Server routes requests to Gophermart handlers.
type Server struct {
	store  Store
	tokens *auth.Manager
}

// New creates a Server.
func New(store Store, tokens *auth.Manager) *Server {
	return &Server{store: store, tokens: tokens}
}

// Handler returns the root HTTP handler with gzip support.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/user/register", s.register)
	mux.HandleFunc("POST /api/user/login", s.login)
	mux.HandleFunc("POST /api/user/orders", s.withAuth(s.uploadOrder))
	mux.HandleFunc("GET /api/user/orders", s.withAuth(s.listOrders))
	mux.HandleFunc("GET /api/user/balance", s.withAuth(s.balance))
	mux.HandleFunc("POST /api/user/balance/withdraw", s.withAuth(s.withdraw))
	mux.HandleFunc("GET /api/user/withdrawals", s.withAuth(s.withdrawals))

	return gzipMiddleware(mux)
}

type credentials struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var request credentials
	if !decodeJSON(w, r, &request) || request.Login == "" || request.Password == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	userID, err := s.store.CreateUser(r.Context(), request.Login, string(hash))
	if err != nil {
		if errors.Is(err, domain.ErrLoginTaken) {
			http.Error(w, "login already exists", http.StatusConflict)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.writeAuth(w, userID)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request credentials
	if !decodeJSON(w, r, &request) || request.Login == "" || request.Password == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	user, err := s.store.UserByLogin(r.Context(), request.Login)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)) != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.writeAuth(w, user.ID)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) uploadOrder(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	number := strings.TrimSpace(string(body))
	if number == "" || !onlyDigits(number) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !luhn.Valid(number) {
		http.Error(w, "invalid order number", http.StatusUnprocessableEntity)
		return
	}

	err = s.store.SaveOrder(r.Context(), userIDFromContext(r.Context()), number)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, domain.ErrOrderSameUser):
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, domain.ErrOrderOtherUser):
		http.Error(w, "order already uploaded by another user", http.StatusConflict)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := s.store.ListOrders(r.Context(), userIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	response := make([]domain.Order, 0, len(orders))
	for _, order := range orders {
		response = append(response, domain.Order{
			Number:     order.Number,
			Status:     order.Status,
			Accrual:    order.Accrual,
			UploadedAt: order.UploadedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) balance(w http.ResponseWriter, r *http.Request) {
	balance, err := s.store.Balance(r.Context(), userIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, balance)
}

type withdrawRequest struct {
	Order string  `json:"order"`
	Sum   float64 `json:"sum"`
}

func (s *Server) withdraw(w http.ResponseWriter, r *http.Request) {
	var request withdrawRequest
	if !decodeJSON(w, r, &request) || request.Order == "" || request.Sum <= 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !luhn.Valid(request.Order) {
		http.Error(w, "invalid order number", http.StatusUnprocessableEntity)
		return
	}

	err := s.store.Withdraw(r.Context(), userIDFromContext(r.Context()), request.Order, request.Sum)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, domain.ErrInsufficientFund):
		http.Error(w, "insufficient funds", http.StatusPaymentRequired)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) withdrawals(w http.ResponseWriter, r *http.Request) {
	withdrawals, err := s.store.ListWithdrawals(r.Context(), userIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	response := make([]domain.Withdrawal, 0, len(withdrawals))
	for _, withdrawal := range withdrawals {
		response = append(response, domain.Withdrawal{
			Order:       withdrawal.Order,
			Sum:         withdrawal.Sum,
			ProcessedAt: withdrawal.ProcessedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		userID, ok := s.tokens.Verify(token)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) writeAuth(w http.ResponseWriter, userID int64) {
	token := s.tokens.Issue(userID)
	w.Header().Set("Authorization", "Bearer "+token)
	http.SetCookie(w, &http.Cookie{
		Name:     "Authorization",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func bearerToken(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if value == "" {
		if cookie, err := r.Cookie("Authorization"); err == nil {
			value = cookie.Value
		}
	}
	value = strings.TrimSpace(value)
	if token, ok := strings.CutPrefix(value, "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	return value
}

func userIDFromContext(ctx context.Context) int64 {
	userID, _ := ctx.Value(userIDKey).(int64)
	return userID
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(value); err != nil {
		return false
	}
	var extra struct{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func onlyDigits(value string) bool {
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, "bad gzip body", http.StatusBadRequest)
				return
			}
			defer reader.Close()
			r.Body = reader
		}

		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		defer writer.Close()
		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, Writer: writer}, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	Writer io.Writer
}

func (w gzipResponseWriter) Write(data []byte) (int, error) {
	return w.Writer.Write(data)
}
