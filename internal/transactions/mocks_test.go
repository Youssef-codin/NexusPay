package transactions

import (
	"context"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/payment"
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/go-chi/jwtauth/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

// withUserID puts a signed token carrying sub=userID into the context, which is
// where callerID reads the authenticated user from.
func withUserID(ctx context.Context, userID string) context.Context {
	ja := jwtauth.New("HS256", []byte("test-secret"), nil)
	token, _, _ := ja.Encode(map[string]any{"sub": userID})
	return jwtauth.NewContext(ctx, token, nil)
}

// ---------------------------------------------------------------------------
// Transaction manager
// ---------------------------------------------------------------------------

// MockTxManager hands out a MockTx and records it, so a test can assert whether
// the transaction committed or rolled back.
type MockTxManager struct {
	tx  *MockTx
	err error
}

func newMockTxManager() *MockTxManager {
	return &MockTxManager{tx: &MockTx{}}
}

func (m *MockTxManager) StartTx(ctx context.Context) (context.Context, pgx.Tx, error) {
	if m.err != nil {
		return ctx, nil, m.err
	}
	return ctx, m.tx, nil
}

type MockTx struct {
	commitCalled   bool
	rollbackCalled bool
}

func (m *MockTx) Commit(ctx context.Context) error {
	m.commitCalled = true
	return nil
}

func (m *MockTx) Rollback(ctx context.Context) error {
	if !m.commitCalled {
		m.rollbackCalled = true
	}
	return nil
}

func (m *MockTx) Begin(ctx context.Context) (pgx.Tx, error) { return m, nil }

func (m *MockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (m *MockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (m *MockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }

func (m *MockTx) CopyFrom(
	ctx context.Context,
	tableName pgx.Identifier,
	columnNames []string,
	rowSrc pgx.CopyFromSource,
) (int64, error) {
	return 0, nil
}

func (m *MockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }

func (m *MockTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }

func (m *MockTx) Prepare(
	ctx context.Context,
	name, sql string,
) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (m *MockTx) Conn() *pgx.Conn { return &pgx.Conn{} }

// ---------------------------------------------------------------------------
// Repository
// ---------------------------------------------------------------------------

type MockRepo struct {
	mock.Mock
}

func (m *MockRepo) CreateTransaction(
	ctx context.Context,
	arg repo.CreateTransactionParams,
) (repo.Transaction, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func (m *MockRepo) GetTransactionById(
	ctx context.Context,
	id pgtype.UUID,
) (repo.Transaction, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func (m *MockRepo) GetTransactionByIdWithUsers(
	ctx context.Context,
	id pgtype.UUID,
) (repo.GetTransactionByIdWithUsersRow, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(repo.GetTransactionByIdWithUsersRow), args.Error(1)
}

func (m *MockRepo) GetTransactionsByUserId(
	ctx context.Context,
	userID pgtype.UUID,
) ([]repo.GetTransactionsByUserIdRow, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]repo.GetTransactionsByUserIdRow), args.Error(1)
}

func (m *MockRepo) GuardedSetStatus(
	ctx context.Context,
	arg repo.GuardedSetStatusParams,
) (repo.Transaction, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func (m *MockRepo) ClaimDueTransactions(
	ctx context.Context,
	limit int32,
) ([]repo.Transaction, error) {
	args := m.Called(ctx, limit)
	return args.Get(0).([]repo.Transaction), args.Error(1)
}

func (m *MockRepo) ClaimStuckCrediting(
	ctx context.Context,
	limit int32,
) ([]repo.Transaction, error) {
	args := m.Called(ctx, limit)
	return args.Get(0).([]repo.Transaction), args.Error(1)
}

func (m *MockRepo) CancelScheduledTransaction(
	ctx context.Context,
	arg repo.CancelScheduledTransactionParams,
) (repo.Transaction, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func (m *MockRepo) SetSenderCategory(
	ctx context.Context,
	arg repo.SetSenderCategoryParams,
) (repo.Transaction, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func (m *MockRepo) SetReceiverCategory(
	ctx context.Context,
	arg repo.SetReceiverCategoryParams,
) (repo.Transaction, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

// ---------------------------------------------------------------------------
// Users service
// ---------------------------------------------------------------------------

// MockUserService also records the order Debit and Credit were called in, which
// is how the deadlock-ordering test observes moveBalance.
type MockUserService struct {
	mock.Mock
	calls []string
}

func (m *MockUserService) FindByName(
	ctx context.Context,
	req users.FindUserRequest,
) (users.FindUserResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(users.FindUserResponse), args.Error(1)
}

func (m *MockUserService) GetMe(ctx context.Context) (users.GetMeResponse, error) {
	args := m.Called(ctx)
	return args.Get(0).(users.GetMeResponse), args.Error(1)
}

func (m *MockUserService) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (users.GetMeResponse, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(users.GetMeResponse), args.Error(1)
}

func (m *MockUserService) Debit(
	ctx context.Context,
	req users.BalanceRequest,
) (users.BalanceResponse, error) {
	m.calls = append(m.calls, "debit")
	args := m.Called(ctx, req)
	return args.Get(0).(users.BalanceResponse), args.Error(1)
}

func (m *MockUserService) Credit(
	ctx context.Context,
	req users.BalanceRequest,
) (users.BalanceResponse, error) {
	m.calls = append(m.calls, "credit")
	args := m.Called(ctx, req)
	return args.Get(0).(users.BalanceResponse), args.Error(1)
}

// ---------------------------------------------------------------------------
// Payment service
// ---------------------------------------------------------------------------

type MockPaymentService struct {
	mock.Mock
}

func (m *MockPaymentService) ProcessPayment(
	ctx context.Context,
	req payment.ProcessPaymentRequest,
) (payment.ProcessPaymentResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(payment.ProcessPaymentResponse), args.Error(1)
}
