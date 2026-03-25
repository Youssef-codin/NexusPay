package stripe

import (
	"context"
	"errors"
	"testing"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockWalletSvc struct {
	mock.Mock
}

func (m *MockWalletSvc) GetById(ctx context.Context, req wallet.GetWalletRequest) (wallet.GetWalletResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(wallet.GetWalletResponse), args.Error(1)
}

func (m *MockWalletSvc) GetByUserId(ctx context.Context) (wallet.GetWalletResponse, error) {
	args := m.Called(ctx)
	return args.Get(0).(wallet.GetWalletResponse), args.Error(1)
}

func (m *MockWalletSvc) CreateWallet(ctx context.Context, req wallet.CreateWalletRequest) (wallet.CreateWalletResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(wallet.CreateWalletResponse), args.Error(1)
}

func (m *MockWalletSvc) TopUp(ctx context.Context, req wallet.TopUpRequest) (wallet.TopUpResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(wallet.TopUpResponse), args.Error(1)
}

func (m *MockWalletSvc) DeductFromBalance(ctx context.Context, req wallet.DeductRequest) (wallet.DeductResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(wallet.DeductResponse), args.Error(1)
}

func (m *MockWalletSvc) AddToWallet(ctx context.Context, req wallet.AddToWalletRequest) (wallet.AddToWalletResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(wallet.AddToWalletResponse), args.Error(1)
}

type MockTransactionSvc struct {
	mock.Mock
}

func (m *MockTransactionSvc) GetById(ctx context.Context, req transactions.GetByIdRequest) (transactions.GetTransactionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(transactions.GetTransactionResponse), args.Error(1)
}

func (m *MockTransactionSvc) CreateTransaction(ctx context.Context, req transactions.CreateTransactionRequest) (transactions.CreateTransactionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(transactions.CreateTransactionResponse), args.Error(1)
}

func (m *MockTransactionSvc) UpdateStatus(ctx context.Context, req transactions.UpdateTransactionRequest) error {
	args := m.Called(ctx, req)
	return args.Error(0)
}

type MockTxManager struct {
	mock.Mock
}

func (m *MockTxManager) StartTx(ctx context.Context) (context.Context, pgx.Tx, error) {
	args := m.Called(ctx)
	return args.Get(0).(context.Context), args.Get(1).(pgx.Tx), args.Error(2)
}

type MockTx struct {
	mock.Mock
	commitCalled   bool
	rollbackCalled bool
	commitErr      error
}

func (m *MockTx) Commit(ctx context.Context) error {
	m.commitCalled = true
	return m.commitErr
}

func (m *MockTx) Rollback(ctx context.Context) error {
	m.rollbackCalled = true
	return nil
}

func (m *MockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return m, nil
}

func (m *MockTx) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	return m, nil
}

func (m *MockTx) Close(ctx context.Context) error {
	return nil
}

func (m *MockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (m *MockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (m *MockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

func (m *MockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (m *MockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return nil
}

func (m *MockTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (m *MockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (m *MockTx) Conn() *pgx.Conn {
	return &pgx.Conn{}
}

func TestHandlePaymentSucceeded(t *testing.T) {
	ctx := context.Background()
	transactionID := uuid.New()
	walletID := uuid.New()

	t.Run("happy_path", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)
		mockTx := &MockTx{}

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockTxSvc.On("GetById", mock.Anything, mock.Anything).Return(transactions.GetTransactionResponse{
			ID:       transactionID.String(),
			WalletID: walletID.String(),
			Amount:   1000,
			Status:   repo.TransactionStatusPending,
		}, nil)
		mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil).Twice()
		mockWalletSvc.On("AddToWallet", mock.Anything, mock.Anything).Return(wallet.AddToWalletResponse{
			ID:      walletID.String(),
			Balance: 1000,
		}, nil)

		svc := &WebhookService{
			txManager:      mockTxManager,
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentSucceeded(ctx, HandlePaymentSucceededRequest{
			TransactionID: transactionID.String(),
		})

		assert.NoError(t, err)
		assert.True(t, mockTx.commitCalled)
		mockTxManager.AssertExpectations(t)
		mockWalletSvc.AssertExpectations(t)
		mockTxSvc.AssertExpectations(t)
	})

	t.Run("transaction_not_found", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)
		mockTx := &MockTx{}

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockTxSvc.On("GetById", mock.Anything, mock.Anything).Return(transactions.GetTransactionResponse{}, errors.New("transaction not found"))

		svc := &WebhookService{
			txManager:      mockTxManager,
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentSucceeded(ctx, HandlePaymentSucceededRequest{
			TransactionID: transactionID.String(),
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "transaction not found")
		mockTxSvc.AssertExpectations(t)
	})

	t.Run("already_processing", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)
		mockTx := &MockTx{}

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockTxSvc.On("GetById", mock.Anything, mock.Anything).Return(transactions.GetTransactionResponse{
			ID:     transactionID.String(),
			Status: repo.TransactionStatusProcessing,
		}, nil)

		svc := &WebhookService{
			txManager:      mockTxManager,
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentSucceeded(ctx, HandlePaymentSucceededRequest{
			TransactionID: transactionID.String(),
		})

		assert.ErrorIs(t, err, ErrAlreadyProcessing)
		mockTxSvc.AssertExpectations(t)
	})

	t.Run("update_status_to_processing_fails", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)
		mockTx := &MockTx{}

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockTxSvc.On("GetById", mock.Anything, mock.Anything).Return(transactions.GetTransactionResponse{
			ID:     transactionID.String(),
			Status: repo.TransactionStatusPending,
		}, nil)
		mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(errors.New("db error"))

		svc := &WebhookService{
			txManager:      mockTxManager,
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentSucceeded(ctx, HandlePaymentSucceededRequest{
			TransactionID: transactionID.String(),
		})

		assert.Error(t, err)
		mockTxSvc.AssertExpectations(t)
	})

	t.Run("add_to_wallet_fails", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)
		mockTx := &MockTx{}

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockTxSvc.On("GetById", mock.Anything, mock.Anything).Return(transactions.GetTransactionResponse{
			ID:       transactionID.String(),
			WalletID: walletID.String(),
			Amount:   1000,
			Status:   repo.TransactionStatusPending,
		}, nil)
		mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil)
		mockWalletSvc.On("AddToWallet", mock.Anything, mock.Anything).Return(wallet.AddToWalletResponse{}, errors.New("db error"))

		svc := &WebhookService{
			txManager:      mockTxManager,
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentSucceeded(ctx, HandlePaymentSucceededRequest{
			TransactionID: transactionID.String(),
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockWalletSvc.AssertExpectations(t)
		mockTxSvc.AssertExpectations(t)
	})

	t.Run("commit_fails", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)
		mockTx := &MockTx{commitErr: errors.New("commit failed")}

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockTxSvc.On("GetById", mock.Anything, mock.Anything).Return(transactions.GetTransactionResponse{
			ID:       transactionID.String(),
			WalletID: walletID.String(),
			Amount:   1000,
			Status:   repo.TransactionStatusPending,
		}, nil)
		mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil).Twice()
		mockWalletSvc.On("AddToWallet", mock.Anything, mock.Anything).Return(wallet.AddToWalletResponse{
			ID:      walletID.String(),
			Balance: 1000,
		}, nil)

		svc := &WebhookService{
			txManager:      mockTxManager,
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentSucceeded(ctx, HandlePaymentSucceededRequest{
			TransactionID: transactionID.String(),
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "commit failed")
		mockWalletSvc.AssertExpectations(t)
		mockTxSvc.AssertExpectations(t)
	})
}

func TestHandlePaymentFailed(t *testing.T) {
	ctx := context.Background()
	transactionID := uuid.New()

	t.Run("success", func(t *testing.T) {
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)

		mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil)

		svc := &WebhookService{
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentFailed(ctx, HandlePaymentFailedRequest{
			TransactionID: transactionID.String(),
		})

		assert.NoError(t, err)
		mockTxSvc.AssertExpectations(t)
	})

	t.Run("transaction_not_found", func(t *testing.T) {
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)

		mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(transactions.ErrTransactionNotFound)

		svc := &WebhookService{
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentFailed(ctx, HandlePaymentFailedRequest{
			TransactionID: transactionID.String(),
		})

		assert.ErrorIs(t, err, transactions.ErrTransactionNotFound)
		mockTxSvc.AssertExpectations(t)
	})
}

func TestHandlePaymentCanceled(t *testing.T) {
	ctx := context.Background()
	transactionID := uuid.New()

	t.Run("success", func(t *testing.T) {
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)

		mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil)

		svc := &WebhookService{
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentCanceled(ctx, HandlePaymentCanceledRequest{
			TransactionID: transactionID.String(),
		})

		assert.NoError(t, err)
		mockTxSvc.AssertExpectations(t)
	})

	t.Run("transaction_not_found", func(t *testing.T) {
		mockWalletSvc := new(MockWalletSvc)
		mockTxSvc := new(MockTransactionSvc)

		mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(transactions.ErrTransactionNotFound)

		svc := &WebhookService{
			walletSvc:      mockWalletSvc,
			transactionSvc: mockTxSvc,
		}

		err := svc.HandlePaymentCanceled(ctx, HandlePaymentCanceledRequest{
			TransactionID: transactionID.String(),
		})

		assert.ErrorIs(t, err, transactions.ErrTransactionNotFound)
		mockTxSvc.AssertExpectations(t)
	})
}
