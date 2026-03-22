package wallet

import (
	"context"
	"errors"
	"testing"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/payment"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/go-chi/jwtauth/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func withUserID(ctx context.Context, userID string) context.Context {
	ja := jwtauth.New("HS256", []byte("test-secret"), nil)
	token, _, _ := ja.Encode(map[string]interface{}{"sub": userID})
	return jwtauth.NewContext(ctx, token, nil)
}

type MockTxManager struct {
	mock.Mock
}

func (m *MockTxManager) StartTx(ctx context.Context) (context.Context, pgx.Tx, error) {
	args := m.Called(ctx)
	var tx pgx.Tx
	if args.Get(1) != nil {
		tx = args.Get(1).(pgx.Tx)
	}
	return args.Get(0).(context.Context), tx, args.Error(2)
}

type MockTx struct {
	mock.Mock
	commitCalled bool
}

func (m *MockTx) Commit(ctx context.Context) error {
	m.commitCalled = true
	return nil
}

func (m *MockTx) Rollback(ctx context.Context) error {
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

type MockConn struct{}

func (m *MockConn) Close(ctx context.Context) error { return nil }
func (m *MockConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *MockConn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (m *MockConn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }
func (m *MockConn) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (m *MockConn) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (m *MockConn) LargeObjects() pgx.LargeObjects                               { return pgx.LargeObjects{} }
func (m *MockConn) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (m *MockConn) IsClosed() bool          { return false }
func (m *MockConn) Config() *pgx.ConnConfig { return nil }
func (m *MockConn) PgConn() *pgconn.PgConn  { return nil }
func (m *MockConn) WaitForNotification(ctx context.Context) (*pgconn.Notification, error) {
	return nil, nil
}
func (m *MockConn) Ping(ctx context.Context) error { return nil }
func (m *MockConn) LoadType(ctx context.Context, typeName string) (*pgtype.Type, error) {
	return nil, nil
}
func (m *MockConn) LoadTypes(ctx context.Context, typeNames []string) ([]*pgtype.Type, error) {
	return nil, nil
}
func (m *MockConn) TypeMap() *pgtype.Map                              { return nil }
func (m *MockConn) Deallocate(ctx context.Context, name string) error { return nil }
func (m *MockConn) DeallocateAll(ctx context.Context) error           { return nil }

func (m *MockTx) Conn() *pgx.Conn {
	conn := &pgx.Conn{}
	return conn
}

type MockwalletRepo struct {
	mock.Mock
}

func (m *MockwalletRepo) CreateWallet(ctx context.Context, arg repo.CreateWalletParams) (repo.CreateWalletRow, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.CreateWalletRow), args.Error(1)
}

func (m *MockwalletRepo) GetWalletById(ctx context.Context, id pgtype.UUID) (repo.Wallet, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(repo.Wallet), args.Error(1)
}

func (m *MockwalletRepo) GetWalletByUserId(ctx context.Context, userID pgtype.UUID) (repo.Wallet, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(repo.Wallet), args.Error(1)
}

func (m *MockwalletRepo) DeductFromBalance(ctx context.Context, arg repo.DeductFromBalanceParams) (repo.Wallet, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Wallet), args.Error(1)
}

func (m *MockwalletRepo) AddToBalance(ctx context.Context, arg repo.AddToBalanceParams) (repo.Wallet, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Wallet), args.Error(1)
}

type MockTransactionsSvc struct {
	mock.Mock
}

func (m *MockTransactionsSvc) GetById(ctx context.Context, req transactions.GetByIdRequest) (transactions.GetTransactionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(transactions.GetTransactionResponse), args.Error(1)
}

func (m *MockTransactionsSvc) CreateTransaction(ctx context.Context, req transactions.CreateTransactionRequest) (transactions.CreateTransactionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(transactions.CreateTransactionResponse), args.Error(1)
}

func (m *MockTransactionsSvc) UpdateStatus(ctx context.Context, req transactions.UpdateTransactionRequest) error {
	args := m.Called(ctx, req)
	return args.Error(0)
}

type MockPaymentSvc struct {
	mock.Mock
}

func (m *MockPaymentSvc) ProcessPayment(ctx context.Context, req payment.ProcessPaymentRequest) (payment.ProcessPaymentResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(payment.ProcessPaymentResponse), args.Error(1)
}

func (m *MockPaymentSvc) Refund(ctx context.Context, req payment.RefundRequest) (payment.RefundResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(payment.RefundResponse), args.Error(1)
}

func (m *MockPaymentSvc) CancelPayment(ctx context.Context, req payment.CancelPaymentRequest) (payment.CancelPaymentResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(payment.CancelPaymentResponse), args.Error(1)
}

func TestTopUp(t *testing.T) {
	userID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	walletID := uuid.New()

	tests := []struct {
		name         string
		amount       int64
		setupMocks   func(*MockwalletRepo, *MockTransactionsSvc, *MockPaymentSvc)
		expectedErr  error
		expectStatus string
	}{
		{
			name:         "amount_below_minimum",
			amount:       999,
			setupMocks:   func(mr *MockwalletRepo, mt *MockTransactionsSvc, mp *MockPaymentSvc) {},
			expectedErr:  ErrAmountIsTooLow,
			expectStatus: "",
		},
		{
			name:         "amount_zero",
			amount:       0,
			setupMocks:   func(mr *MockwalletRepo, mt *MockTransactionsSvc, mp *MockPaymentSvc) {},
			expectedErr:  ErrAmountIsTooLow,
			expectStatus: "",
		},
		{
			name:         "amount_negative",
			amount:       -100,
			setupMocks:   func(mr *MockwalletRepo, mt *MockTransactionsSvc, mp *MockPaymentSvc) {},
			expectedErr:  ErrAmountIsTooLow,
			expectStatus: "",
		},
		{
			name:   "wallet_not_found",
			amount: 1000,
			setupMocks: func(mr *MockwalletRepo, mt *MockTransactionsSvc, mp *MockPaymentSvc) {
				mr.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{}, pgx.ErrNoRows)
			},
			expectedErr:  ErrWalletNotFound,
			expectStatus: "",
		},
		{
			name:   "transaction_creation_fails",
			amount: 1000,
			setupMocks: func(mr *MockwalletRepo, mt *MockTransactionsSvc, mp *MockPaymentSvc) {
				mr.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{
					ID:        pgtype.UUID{Bytes: walletID, Valid: true},
					UserID:    pgtype.UUID{Bytes: userID, Valid: true},
					Balance:   0,
					CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
					UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
				}, nil)
				mt.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{}, errors.New("db error"))
			},
			expectedErr:  errors.New("db error"),
			expectStatus: "",
		},
		{
			name:   "payment_processing_fails",
			amount: 1000,
			setupMocks: func(mr *MockwalletRepo, mt *MockTransactionsSvc, mp *MockPaymentSvc) {
				mr.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{
					ID:        pgtype.UUID{Bytes: walletID, Valid: true},
					UserID:    pgtype.UUID{Bytes: userID, Valid: true},
					Balance:   0,
					CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
					UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
				}, nil)
				mt.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
					ID:     uuid.New().String(),
					Status: repo.TransactionStatusPending,
				}, nil)
				mp.On("ProcessPayment", mock.Anything, mock.Anything).Return(payment.ProcessPaymentResponse{}, errors.New("payment failed"))
				mt.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil)
			},
			expectedErr:  errors.New("payment failed"),
			expectStatus: string(repo.TransactionStatusFailed),
		},
		{
			name:   "happy_path",
			amount: 1000,
			setupMocks: func(mr *MockwalletRepo, mt *MockTransactionsSvc, mp *MockPaymentSvc) {
				mr.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{
					ID:        pgtype.UUID{Bytes: walletID, Valid: true},
					UserID:    pgtype.UUID{Bytes: userID, Valid: true},
					Balance:   0,
					CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
					UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
				}, nil)
				mt.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
					ID:     uuid.New().String(),
					Status: repo.TransactionStatusPending,
				}, nil)
				mp.On("ProcessPayment", mock.Anything, mock.Anything).Return(payment.ProcessPaymentResponse{
					ProviderPaymentID: "pi_test123",
					ClientSecret:      "pi_test123_secret",
					Status:            payment.PaymentStatusPending,
				}, nil)
			},
			expectedErr:  nil,
			expectStatus: string(payment.PaymentStatusPending),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockwalletRepo)
			mockTxSvc := new(MockTransactionsSvc)
			mockPaySvc := new(MockPaymentSvc)

			tt.setupMocks(mockRepo, mockTxSvc, mockPaySvc)

			svc := &Service{
				txManager:       nil,
				repo:            mockRepo,
				transactionsSvc: mockTxSvc,
				paymentSvc:      mockPaySvc,
			}

			resp, err := svc.TopUp(ctx, TopUpRequest{
				Amount:      tt.amount,
				Description: "test topup",
			})

			if tt.expectedErr != nil {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectStatus, resp.Status)
			}

			mockRepo.AssertExpectations(t)
			mockTxSvc.AssertExpectations(t)
			mockPaySvc.AssertExpectations(t)
		})
	}
}

func TestGetById(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	t.Run("wallet_exists", func(t *testing.T) {
		mockRepo := new(MockwalletRepo)
		now := time.Now()

		mockRepo.On("GetWalletById", mock.Anything, mock.Anything).Return(repo.Wallet{
			ID:        pgtype.UUID{Bytes: walletID, Valid: true},
			UserID:    pgtype.UUID{Bytes: userID, Valid: true},
			Balance:   5000,
			CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
			UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}, nil)

		svc := &Service{repo: mockRepo}
		resp, err := svc.GetById(ctx, GetWalletRequest{ID: walletID.String()})

		assert.NoError(t, err)
		assert.Equal(t, walletID.String(), resp.ID)
		assert.Equal(t, userID.String(), resp.UserID)
		assert.Equal(t, int64(5000), resp.Balance)
		mockRepo.AssertExpectations(t)
	})

	t.Run("wallet_not_found", func(t *testing.T) {
		mockRepo := new(MockwalletRepo)
		mockRepo.On("GetWalletById", mock.Anything, mock.Anything).Return(repo.Wallet{}, pgx.ErrNoRows)

		svc := &Service{repo: mockRepo}
		_, err := svc.GetById(ctx, GetWalletRequest{ID: uuid.New().String()})

		assert.ErrorIs(t, err, ErrWalletNotFound)
		mockRepo.AssertExpectations(t)
	})

	t.Run("database_error", func(t *testing.T) {
		mockRepo := new(MockwalletRepo)
		mockRepo.On("GetWalletById", mock.Anything, mock.Anything).Return(repo.Wallet{}, errors.New("connection error"))

		svc := &Service{repo: mockRepo}
		_, err := svc.GetById(ctx, GetWalletRequest{ID: uuid.New().String()})

		assert.Error(t, err)
		assert.NotEqual(t, ErrWalletNotFound, err)
		mockRepo.AssertExpectations(t)
	})
}

func TestGetByUserId(t *testing.T) {
	userID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	walletID := uuid.New()
	now := time.Now()

	t.Run("wallet_exists", func(t *testing.T) {
		mockRepo := new(MockwalletRepo)
		mockRepo.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{
			ID:        pgtype.UUID{Bytes: walletID, Valid: true},
			UserID:    pgtype.UUID{Bytes: userID, Valid: true},
			Balance:   5000,
			CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
			UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}, nil)

		svc := &Service{repo: mockRepo}
		resp, err := svc.GetByUserId(ctx)

		assert.NoError(t, err)
		assert.Equal(t, walletID.String(), resp.ID)
		assert.Equal(t, userID.String(), resp.UserID)
		mockRepo.AssertExpectations(t)
	})

	t.Run("wallet_not_found", func(t *testing.T) {
		mockRepo := new(MockwalletRepo)
		mockRepo.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{}, pgx.ErrNoRows)

		svc := &Service{repo: mockRepo}
		_, err := svc.GetByUserId(ctx)

		assert.ErrorIs(t, err, ErrWalletNotFound)
		mockRepo.AssertExpectations(t)
	})
}

func TestCreateWallet(t *testing.T) {
	userID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	walletID := uuid.New()
	now := time.Now()

	t.Run("success", func(t *testing.T) {
		mockRepo := new(MockwalletRepo)

		mockRepo.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{}, pgx.ErrNoRows)
		mockRepo.On("CreateWallet", mock.Anything, mock.Anything).Return(repo.CreateWalletRow{
			ID:        pgtype.UUID{Bytes: walletID, Valid: true},
			UserID:    pgtype.UUID{Bytes: userID, Valid: true},
			Balance:   0,
			CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}, nil)

		svc := &Service{repo: mockRepo}
		resp, err := svc.CreateWallet(ctx, CreateWalletRequest{})

		assert.NoError(t, err)
		assert.Equal(t, walletID.String(), resp.ID)
		assert.Equal(t, userID.String(), resp.UserID)
		assert.Equal(t, int64(0), resp.Balance)
		mockRepo.AssertExpectations(t)
	})

	t.Run("wallet_already_exists", func(t *testing.T) {
		mockRepo := new(MockwalletRepo)
		mockRepo.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{
			ID:      pgtype.UUID{Bytes: walletID, Valid: true},
			UserID:  pgtype.UUID{Bytes: userID, Valid: true},
			Balance: 1000,
		}, nil)

		svc := &Service{repo: mockRepo}
		_, err := svc.CreateWallet(ctx, CreateWalletRequest{})

		assert.ErrorIs(t, err, ErrWalletAlreadyExists)
		mockRepo.AssertExpectations(t)
	})

	t.Run("create_database_error", func(t *testing.T) {
		mockRepo := new(MockwalletRepo)
		mockRepo.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{}, pgx.ErrNoRows)
		mockRepo.On("CreateWallet", mock.Anything, mock.Anything).Return(repo.CreateWalletRow{}, errors.New("db error"))

		svc := &Service{repo: mockRepo}
		_, err := svc.CreateWallet(ctx, CreateWalletRequest{})

		assert.Error(t, err)
		mockRepo.AssertExpectations(t)
	})
}

func TestDeductFromBalance(t *testing.T) {
	userID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	walletID := uuid.New()
	now := time.Now()

	t.Run("success", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockRepo := new(MockwalletRepo)
		mockTx := &MockTx{}

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockRepo.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{
			ID:      pgtype.UUID{Bytes: walletID, Valid: true},
			UserID:  pgtype.UUID{Bytes: userID, Valid: true},
			Balance: 5000,
		}, nil)
		mockRepo.On("DeductFromBalance", mock.Anything, mock.Anything).Return(repo.Wallet{
			ID:        pgtype.UUID{Bytes: walletID, Valid: true},
			UserID:    pgtype.UUID{Bytes: userID, Valid: true},
			Balance:   3000,
			UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}, nil)

		svc := &Service{
			txManager: mockTxManager,
			repo:      mockRepo,
		}
		resp, err := svc.DeductFromBalance(ctx, DeductRequest{Amount: 2000})

		assert.NoError(t, err)
		assert.Equal(t, walletID.String(), resp.ID)
		assert.Equal(t, userID.String(), resp.UserID)
		mockTxManager.AssertExpectations(t)
		mockRepo.AssertExpectations(t)
	})

	t.Run("insufficient_funds", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockRepo := new(MockwalletRepo)
		mockTx := &MockTx{}

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockRepo.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{
			ID:      pgtype.UUID{Bytes: walletID, Valid: true},
			UserID:  pgtype.UUID{Bytes: userID, Valid: true},
			Balance: 100,
		}, nil)

		svc := &Service{
			txManager: mockTxManager,
			repo:      mockRepo,
		}
		_, err := svc.DeductFromBalance(ctx, DeductRequest{Amount: 2000})

		assert.ErrorIs(t, err, ErrInsufficientFunds)
		mockTxManager.AssertExpectations(t)
		mockRepo.AssertExpectations(t)
	})

	t.Run("wallet_not_found", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockRepo := new(MockwalletRepo)
		mockTx := &MockTx{}

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockRepo.On("GetWalletByUserId", mock.Anything, mock.Anything).Return(repo.Wallet{}, pgx.ErrNoRows)

		svc := &Service{
			txManager: mockTxManager,
			repo:      mockRepo,
		}
		_, err := svc.DeductFromBalance(ctx, DeductRequest{Amount: 1000})

		assert.ErrorIs(t, err, ErrWalletNotFound)
		mockTxManager.AssertExpectations(t)
		mockRepo.AssertExpectations(t)
	})

	t.Run("start_tx_fails", func(t *testing.T) {
		mockTxManager := new(MockTxManager)

		mockTxManager.On("StartTx", mock.Anything).Return(ctx, nil, errors.New("failed to start tx"))

		svc := &Service{
			txManager: mockTxManager,
			repo:      nil,
		}
		_, err := svc.DeductFromBalance(ctx, DeductRequest{Amount: 1000})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to start tx")
		mockTxManager.AssertExpectations(t)
	})
}

func TestAddToWallet(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()
	now := time.Now()

	t.Run("success", func(t *testing.T) {
		mockTxManager := new(MockTxManager)
		mockRepo := new(MockwalletRepo)
		mockTx := &MockTx{}

		mockRepo.On("GetWalletById", mock.Anything, mock.Anything).Return(repo.Wallet{
			ID:     pgtype.UUID{Bytes: walletID, Valid: true},
			UserID: pgtype.UUID{Bytes: userID, Valid: true},
		}, nil)
		mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
		mockRepo.On("AddToBalance", mock.Anything, mock.Anything).Return(repo.Wallet{
			ID:        pgtype.UUID{Bytes: walletID, Valid: true},
			UserID:    pgtype.UUID{Bytes: userID, Valid: true},
			Balance:   3000,
			UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}, nil)

		svc := &Service{
			txManager: mockTxManager,
			repo:      mockRepo,
		}
		resp, err := svc.AddToWallet(ctx, AddToWalletRequest{
			WalletID: walletID.String(),
			Amount:   1000,
		})

		assert.NoError(t, err)
		assert.Equal(t, walletID.String(), resp.ID)
		assert.Equal(t, int64(3000), resp.Balance)
		mockRepo.AssertExpectations(t)
	})

	t.Run("wallet_not_found", func(t *testing.T) {
		mockRepo := new(MockwalletRepo)
		mockRepo.On("GetWalletById", mock.Anything, mock.Anything).Return(repo.Wallet{}, pgx.ErrNoRows)

		svc := &Service{repo: mockRepo}
		_, err := svc.AddToWallet(ctx, AddToWalletRequest{
			WalletID: walletID.String(),
			Amount:   1000,
		})

		assert.ErrorIs(t, err, ErrWalletNotFound)
		mockRepo.AssertExpectations(t)
	})
}
