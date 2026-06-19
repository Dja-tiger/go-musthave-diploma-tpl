package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCreateUser(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectQuery("INSERT INTO users").
		WithArgs("alice", "hash").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))

	id, err := store.CreateUser(context.Background(), "alice", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if id != 7 {
		t.Fatalf("id = %d, want 7", id)
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectQuery("INSERT INTO users").
		WithArgs("alice", "hash").
		WillReturnError(&pgconn.PgError{Code: "23505"})

	_, err := store.CreateUser(context.Background(), "alice", "hash")
	if !errors.Is(err, domain.ErrLoginTaken) {
		t.Fatalf("err = %v, want ErrLoginTaken", err)
	}
}

func TestUserByLogin(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectQuery("SELECT id, login, password_hash").
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "login", "password_hash"}).AddRow(7, "alice", "hash"))

	user, err := store.UserByLogin(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != 7 || user.Login != "alice" || user.PasswordHash != "hash" {
		t.Fatalf("unexpected user: %#v", user)
	}
}

func TestUserByLoginNotFound(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectQuery("SELECT id, login, password_hash").
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, err := store.UserByLogin(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSaveOrderNew(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectQuery("INSERT INTO orders").
		WithArgs("12345678903", int64(7), domain.StatusNew).
		WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow("12345678903"))

	if err := store.SaveOrder(context.Background(), 7, "12345678903"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveOrderSameUser(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectQuery("INSERT INTO orders").
		WithArgs("12345678903", int64(7), domain.StatusNew).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT user_id").
		WithArgs("12345678903").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(7))

	err := store.SaveOrder(context.Background(), 7, "12345678903")
	if !errors.Is(err, domain.ErrOrderSameUser) {
		t.Fatalf("err = %v, want ErrOrderSameUser", err)
	}
}

func TestSaveOrderOtherUser(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectQuery("INSERT INTO orders").
		WithArgs("12345678903", int64(7), domain.StatusNew).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT user_id").
		WithArgs("12345678903").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(9))

	err := store.SaveOrder(context.Background(), 7, "12345678903")
	if !errors.Is(err, domain.ErrOrderOtherUser) {
		t.Fatalf("err = %v, want ErrOrderOtherUser", err)
	}
}

func TestListOrders(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()
	uploadedAt := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT number, status, accrual, uploaded_at").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"number", "status", "accrual", "uploaded_at"}).
			AddRow("12345678903", domain.StatusProcessed, 10.5, uploadedAt))

	orders, err := store.ListOrders(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].Accrual == nil || *orders[0].Accrual != 10.5 {
		t.Fatalf("orders = %#v", orders)
	}
}

func TestBalance(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectQuery("SELECT").
		WithArgs(int64(7), domain.StatusProcessed).
		WillReturnRows(sqlmock.NewRows([]string{"current", "withdrawn"}).AddRow(90.5, 10.0))

	balance, err := store.Balance(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if balance.Current != 90.5 || balance.Withdrawn != 10 {
		t.Fatalf("balance = %#v", balance)
	}
}

func TestWithdrawSuccess(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT").
		WithArgs(int64(7), domain.StatusProcessed).
		WillReturnRows(sqlmock.NewRows([]string{"current"}).AddRow(100.0))
	mock.ExpectExec("INSERT INTO withdrawals").
		WithArgs(int64(7), "2377225624", 50.0).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := store.Withdraw(context.Background(), 7, "2377225624", 50); err != nil {
		t.Fatal(err)
	}
}

func TestWithdrawInsufficientFunds(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT").
		WithArgs(int64(7), domain.StatusProcessed).
		WillReturnRows(sqlmock.NewRows([]string{"current"}).AddRow(10.0))
	mock.ExpectRollback()

	err := store.Withdraw(context.Background(), 7, "2377225624", 50)
	if !errors.Is(err, domain.ErrInsufficientFund) {
		t.Fatalf("err = %v, want ErrInsufficientFund", err)
	}
}

func TestListWithdrawals(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()
	processedAt := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT order_number, sum, processed_at").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"order_number", "sum", "processed_at"}).
			AddRow("2377225624", 50.0, processedAt))

	withdrawals, err := store.ListWithdrawals(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(withdrawals) != 1 || withdrawals[0].Order != "2377225624" || withdrawals[0].Sum != 50 {
		t.Fatalf("withdrawals = %#v", withdrawals)
	}
}

func TestPendingOrders(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectQuery("SELECT number").
		WithArgs(domain.StatusNew, domain.StatusProcessing, 20).
		WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow("12345678903"))

	numbers, err := store.PendingOrders(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(numbers) != 1 || numbers[0] != "12345678903" {
		t.Fatalf("numbers = %#v", numbers)
	}
}

func TestTouchOrderAttempt(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectExec("UPDATE orders").
		WithArgs("12345678903", domain.StatusInvalid, domain.StatusProcessed).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.TouchOrderAttempt(context.Background(), "12345678903"); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateOrderAccrual(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()
	accrual := 10.5

	mock.ExpectExec("UPDATE orders").
		WithArgs("12345678903", domain.StatusProcessed, &accrual, domain.StatusInvalid, domain.StatusProcessed).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.UpdateOrderAccrual(context.Background(), "12345678903", domain.StatusProcessed, &accrual); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateOrderAccrualMapsRegisteredToNew(t *testing.T) {
	store, mock, closeStore := newMockStore(t)
	defer closeStore()

	mock.ExpectExec("UPDATE orders").
		WithArgs("12345678903", domain.StatusNew, nil, domain.StatusInvalid, domain.StatusProcessed).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.UpdateOrderAccrual(context.Background(), "12345678903", domain.StatusRegistered, nil); err != nil {
		t.Fatal(err)
	}
}

func newMockStore(t *testing.T) (*Store, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}

	return &Store{db: db}, mock, func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		_ = db.Close()
	}
}
