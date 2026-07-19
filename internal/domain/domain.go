// Package domain contains shared application models and sentinel errors.
package domain

import (
	"errors"
	"time"
)

// User is a registered Gophermart user.
type User struct {
	ID           int64
	Login        string
	PasswordHash string
}

// Order is an order uploaded by a user.
type Order struct {
	Number     string   `json:"number"`
	Status     string   `json:"status"`
	Accrual    *float64 `json:"accrual,omitempty"`
	UploadedAt string   `json:"uploaded_at"`
}

// Withdrawal is a user loyalty points withdrawal.
type Withdrawal struct {
	Order       string  `json:"order"`
	Sum         float64 `json:"sum"`
	ProcessedAt string  `json:"processed_at"`
}

// Balance is the current user loyalty account state.
type Balance struct {
	Current   float64 `json:"current"`
	Withdrawn float64 `json:"withdrawn"`
}

// StoredWithdrawal represents a withdrawal row before JSON formatting.
type StoredWithdrawal struct {
	Order       string
	Sum         float64
	ProcessedAt time.Time
}

// StoredOrder represents an order row before JSON formatting.
type StoredOrder struct {
	Number     string
	Status     string
	Accrual    *float64
	UploadedAt time.Time
}

// Order and accrual statuses used by the service.
const (
	StatusNew        = "NEW"
	StatusRegistered = "REGISTERED"
	StatusProcessing = "PROCESSING"
	StatusInvalid    = "INVALID"
	StatusProcessed  = "PROCESSED"
)

// Sentinel errors returned by service dependencies.
var (
	ErrLoginTaken       = errors.New("login is already taken")
	ErrInvalidAuth      = errors.New("invalid login or password")
	ErrOrderSameUser    = errors.New("order was already uploaded by this user")
	ErrOrderOtherUser   = errors.New("order was already uploaded by another user")
	ErrInsufficientFund = errors.New("insufficient funds")
	ErrNotFound         = errors.New("not found")
)
