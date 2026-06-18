// Package storage contains PostgreSQL persistence for Gophermart.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/domain"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Store persists users, orders, and withdrawals in PostgreSQL.
type Store struct {
	db *sql.DB
}

// New opens a PostgreSQL store and creates required tables.
func New(ctx context.Context, databaseURI string) (*Store, error) {
	db, err := sql.Open("pgx", databaseURI)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

// Close closes the database connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	const query = `
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    login TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders (
    number TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    accrual NUMERIC(12, 2),
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS orders_user_uploaded_idx ON orders(user_id, uploaded_at DESC);
CREATE INDEX IF NOT EXISTS orders_status_idx ON orders(status);

CREATE TABLE IF NOT EXISTS withdrawals (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_number TEXT NOT NULL,
    sum NUMERIC(12, 2) NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS withdrawals_user_processed_idx ON withdrawals(user_id, processed_at DESC);
`
	_, err := s.db.ExecContext(ctx, query)
	return err
}

// CreateUser stores a new user.
func (s *Store) CreateUser(ctx context.Context, login, passwordHash string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
INSERT INTO users (login, password_hash)
VALUES ($1, $2)
RETURNING id`, login, passwordHash).Scan(&id)
	if err != nil {
		if isDuplicate(err) {
			return 0, domain.ErrLoginTaken
		}
		return 0, err
	}
	return id, nil
}

// UserByLogin returns a user by login.
func (s *Store) UserByLogin(ctx context.Context, login string) (domain.User, error) {
	var user domain.User
	err := s.db.QueryRowContext(ctx, `
SELECT id, login, password_hash
FROM users
WHERE login = $1`, login).Scan(&user.ID, &user.Login, &user.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return user, err
}

// SaveOrder stores a user order or reports ownership conflicts.
func (s *Store) SaveOrder(ctx context.Context, userID int64, number string) error {
	var savedNumber string
	err := s.db.QueryRowContext(ctx, `
INSERT INTO orders (number, user_id, status)
VALUES ($1, $2, $3)
ON CONFLICT (number) DO NOTHING
RETURNING number`, number, userID, domain.StatusNew).Scan(&savedNumber)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var ownerID int64
	err = s.db.QueryRowContext(ctx, `
SELECT user_id
FROM orders
WHERE number = $1`, number).Scan(&ownerID)
	if err != nil {
		return err
	}
	if ownerID == userID {
		return domain.ErrOrderSameUser
	}
	return domain.ErrOrderOtherUser
}

// ListOrders returns user's orders from newest to oldest.
func (s *Store) ListOrders(ctx context.Context, userID int64) ([]domain.StoredOrder, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT number, status, accrual, uploaded_at
FROM orders
WHERE user_id = $1
ORDER BY uploaded_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []domain.StoredOrder
	for rows.Next() {
		var order domain.StoredOrder
		var accrual sql.NullFloat64
		if err := rows.Scan(&order.Number, &order.Status, &accrual, &order.UploadedAt); err != nil {
			return nil, err
		}
		if accrual.Valid {
			value := accrual.Float64
			order.Accrual = &value
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

// Balance returns current and withdrawn loyalty point sums for a user.
func (s *Store) Balance(ctx context.Context, userID int64) (domain.Balance, error) {
	var balance domain.Balance
	err := s.db.QueryRowContext(ctx, `
SELECT
    COALESCE((SELECT SUM(accrual) FROM orders WHERE user_id = $1 AND status = $2), 0) -
    COALESCE((SELECT SUM(sum) FROM withdrawals WHERE user_id = $1), 0) AS current,
    COALESCE((SELECT SUM(sum) FROM withdrawals WHERE user_id = $1), 0) AS withdrawn`,
		userID, domain.StatusProcessed).Scan(&balance.Current, &balance.Withdrawn)
	return balance, err
}

// Withdraw stores a withdrawal if the user has enough points.
func (s *Store) Withdraw(ctx context.Context, userID int64, order string, sum float64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, userID); err != nil {
		return err
	}

	var current float64
	err = tx.QueryRowContext(ctx, `
SELECT
    COALESCE((SELECT SUM(accrual) FROM orders WHERE user_id = $1 AND status = $2), 0) -
    COALESCE((SELECT SUM(sum) FROM withdrawals WHERE user_id = $1), 0)`,
		userID, domain.StatusProcessed).Scan(&current)
	if err != nil {
		return err
	}
	if current+1e-9 < sum {
		err = domain.ErrInsufficientFund
		return err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO withdrawals (user_id, order_number, sum)
VALUES ($1, $2, $3)`, userID, order, sum)
	if err != nil {
		return err
	}

	err = tx.Commit()
	return err
}

// ListWithdrawals returns user's withdrawals from newest to oldest.
func (s *Store) ListWithdrawals(ctx context.Context, userID int64) ([]domain.StoredWithdrawal, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT order_number, sum, processed_at
FROM withdrawals
WHERE user_id = $1
ORDER BY processed_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var withdrawals []domain.StoredWithdrawal
	for rows.Next() {
		var withdrawal domain.StoredWithdrawal
		if err := rows.Scan(&withdrawal.Order, &withdrawal.Sum, &withdrawal.ProcessedAt); err != nil {
			return nil, err
		}
		withdrawals = append(withdrawals, withdrawal)
	}
	return withdrawals, rows.Err()
}

// PendingOrders returns orders that still need accrual status updates.
func (s *Store) PendingOrders(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT number
FROM orders
WHERE status IN ($1, $2)
ORDER BY updated_at, uploaded_at
LIMIT $3`, domain.StatusNew, domain.StatusProcessing, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var numbers []string
	for rows.Next() {
		var number string
		if err := rows.Scan(&number); err != nil {
			return nil, err
		}
		numbers = append(numbers, number)
	}
	return numbers, rows.Err()
}

// TouchOrderAttempt records that the accrual worker tried to refresh an order.
func (s *Store) TouchOrderAttempt(ctx context.Context, number string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE orders
SET updated_at = NOW()
WHERE number = $1
  AND status NOT IN ($2, $3)`,
		number, domain.StatusInvalid, domain.StatusProcessed)
	return err
}

// UpdateOrderAccrual applies an accrual-system status to an order.
func (s *Store) UpdateOrderAccrual(ctx context.Context, number, status string, accrual *float64) error {
	if status == domain.StatusRegistered {
		status = domain.StatusNew
	}

	_, err := s.db.ExecContext(ctx, `
UPDATE orders
SET status = $2, accrual = $3, updated_at = NOW()
WHERE number = $1
  AND status NOT IN ($4, $5)`,
		number, status, accrual, domain.StatusInvalid, domain.StatusProcessed)
	return err
}

func isDuplicate(err error) bool {
	text := strings.ToLower(fmt.Sprint(err))
	return strings.Contains(text, "duplicate key") || strings.Contains(text, "unique constraint")
}
