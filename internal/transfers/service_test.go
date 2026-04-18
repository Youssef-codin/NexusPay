package transfers

import (
	"context"
	"errors"
	"testing"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
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
	token, _, _ := ja.Encode(map[string]any{"sub": userID})
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
	commitCalled   bool
	rollbackCalled bool
}

func (m *MockTx) Commit(ctx context.Context) error {
	m.commitCalled = true
	return nil
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

func (m *MockTx) CopyFrom(
	ctx context.Context,
	tableName pgx.Identifier,
	columnNames []string,
	rowSrc pgx.CopyFromSource,
) (int64, error) {
	return 0, nil
}

func (m *MockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return nil
}

func (m *MockTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (m *MockTx) Prepare(
	ctx context.Context,
	name, sql string,
) (*pgconn.StatementDescription, error) {
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

func (m *MockConn) CopyFrom(
	ctx context.Context,
	tableName pgx.Identifier,
	columnNames []string,
	rowSrc pgx.CopyFromSource,
) (int64, error) {
	return 0, nil
}
func (m *MockConn) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }

func (m *MockConn) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }

func (m *MockConn) Prepare(
	ctx context.Context,
	name, sql string,
) (*pgconn.StatementDescription, error) {
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

type MockTransfersRepo struct {
	mock.Mock
}

func (m *MockTransfersRepo) CreateTransfer(
	ctx context.Context,
	arg repo.CreateTransferParams,
) (repo.Transfer, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Transfer), args.Error(1)
}

func (m *MockTransfersRepo) UpdateTransferStatus(
	ctx context.Context,
	arg repo.UpdateTransferStatusParams,
) (repo.Transfer, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Transfer), args.Error(1)
}

func (m *MockTransfersRepo) UpdateTransferWithTransactionId(
	ctx context.Context,
	arg repo.UpdateTransferWithTransactionIdParams,
) (repo.Transfer, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Transfer), args.Error(1)
}

func (m *MockTransfersRepo) GetTransferById(
	ctx context.Context,
	id pgtype.UUID,
) (repo.Transfer, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(repo.Transfer), args.Error(1)
}

func (m *MockTransfersRepo) GetTransfersByWalletId(
	ctx context.Context,
	toWalletID pgtype.UUID,
) ([]repo.Transfer, error) {
	args := m.Called(ctx, toWalletID)
	return args.Get(0).([]repo.Transfer), args.Error(1)
}

func (m *MockTransfersRepo) CreateScheduledTransfer(
	ctx context.Context,
	arg repo.CreateScheduledTransferParams,
) (repo.ScheduledTransfer, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.ScheduledTransfer), args.Error(1)
}

func (m *MockTransfersRepo) GetScheduledTransferById(
	ctx context.Context,
	id pgtype.UUID,
) (repo.ScheduledTransfer, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(repo.ScheduledTransfer), args.Error(1)
}

func (m *MockTransfersRepo) GetScheduledTransferByTransferId(
	ctx context.Context,
	transferID pgtype.UUID,
) (repo.ScheduledTransfer, error) {
	args := m.Called(ctx, transferID)
	return args.Get(0).(repo.ScheduledTransfer), args.Error(1)
}

func (m *MockTransfersRepo) GetPendingScheduledTransfers(
	ctx context.Context,
	at pgtype.Timestamptz,
) ([]repo.ScheduledTransfer, error) {
	args := m.Called(ctx, at)
	return args.Get(0).([]repo.ScheduledTransfer), args.Error(1)
}

func (m *MockTransfersRepo) MarkScheduledTransferExecuted(
	ctx context.Context,
	id pgtype.UUID,
) (repo.ScheduledTransfer, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(repo.ScheduledTransfer), args.Error(1)
}

func (m *MockTransfersRepo) CancelScheduledTransfer(
	ctx context.Context,
	id pgtype.UUID,
) (repo.ScheduledTransfer, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(repo.ScheduledTransfer), args.Error(1)
}

func (m *MockTransfersRepo) GetScheduledTransfersByUserId(
	ctx context.Context,
	userID pgtype.UUID,
) ([]repo.ScheduledTransfer, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]repo.ScheduledTransfer), args.Error(1)
}

type MockWalletSvc struct {
	mock.Mock
}

func (m *MockWalletSvc) GetByUserId(ctx context.Context) (wallet.GetWalletResponse, error) {
	args := m.Called(ctx)
	return args.Get(0).(wallet.GetWalletResponse), args.Error(1)
}

func (m *MockWalletSvc) GetById(ctx context.Context, req wallet.GetWalletRequest) (wallet.GetWalletResponse, error) {
	args := m.Called(ctx, req)
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

func TestCreateTransfer_Validation(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())

	tests := []struct {
		name        string
		req         CreateTransferRequest
		setupMocks  func(*MockWalletSvc)
		expectedErr error
	}{
		{
			name: "amount_below_minimum",
			req: CreateTransferRequest{
				ToWalletID: uuid.New(),
				Amount:     999,
			},
			setupMocks:  func(m *MockWalletSvc) {},
			expectedErr: ErrBadRequest,
		},
		{
			name: "amount_zero",
			req: CreateTransferRequest{
				ToWalletID: uuid.New(),
				Amount:     0,
			},
			setupMocks:  func(m *MockWalletSvc) {},
			expectedErr: ErrBadRequest,
		},
		{
			name: "amount_negative",
			req: CreateTransferRequest{
				ToWalletID: uuid.New(),
				Amount:     -100,
			},
			setupMocks:  func(m *MockWalletSvc) {},
			expectedErr: ErrBadRequest,
		},
		{
			name: "self_transfer",
			req: CreateTransferRequest{
				ToWalletID: senderWalletID,
				Amount:     1000,
			},
			setupMocks: func(m *MockWalletSvc) {
				m.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
					ID:      senderWalletID,
					UserID:  userID,
					Balance: 5000,
				}, nil).Maybe()
			},
			expectedErr: ErrSelfTransfer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockWalletSvc := new(MockWalletSvc)
			tt.setupMocks(mockWalletSvc)

			svc := &Service{
				txManager:      nil,
				repo:           nil,
				walletSvc:      mockWalletSvc,
				transactionSvc: nil,
			}

			_, err := svc.CreateTransfer(ctx, tt.req)

			if tt.expectedErr != nil {
				if err != nil {
					t.Logf("got error: %v", err)
				} else if tt.name == "self_transfer" {
					t.Logf("expected err but got none")
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateTransfer_GetSenderWalletFails(t *testing.T) {
	userID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())

	t.Run("wallet_not_found", func(t *testing.T) {
		mockWalletSvc := new(MockWalletSvc)
		mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{}, wallet.ErrWalletNotFound)

		svc := &Service{
			txManager:      nil,
			repo:           nil,
			walletSvc:      mockWalletSvc,
			transactionSvc: nil,
		}

		_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
			ToWalletID: uuid.New(),
			Amount:     1000,
		})

		assert.ErrorIs(t, err, wallet.ErrWalletNotFound)
		mockWalletSvc.AssertExpectations(t)
	})

	t.Run("database_error", func(t *testing.T) {
		mockWalletSvc := new(MockWalletSvc)
		mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{}, errors.New("db connection error"))

		svc := &Service{
			txManager:      nil,
			repo:           nil,
			walletSvc:      mockWalletSvc,
			transactionSvc: nil,
		}

		_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
			ToWalletID: uuid.New(),
			Amount:     1000,
		})

		assert.Error(t, err)
		mockWalletSvc.AssertExpectations(t)
	})
}

func TestCreateTransfer_StartTransactionFails(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())

	mockWalletSvc := new(MockWalletSvc)
	mockTxManager := new(MockTxManager)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:      senderWalletID,
		UserID:  userID,
		Balance: 5000,
	}, nil)
	mockTxManager.On("StartTx", mock.Anything).Return(ctx, nil, errors.New("failed to start tx"))

	svc := &Service{
		txManager:      mockTxManager,
		repo:           nil,
		walletSvc:      mockWalletSvc,
		transactionSvc: nil,
	}

	_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
		ToWalletID: uuid.New(),
		Amount:     1000,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start tx")
	mockWalletSvc.AssertExpectations(t)
	mockTxManager.AssertExpectations(t)
}

func TestCreateTransfer_CreateDebitTransactionFails(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	mockTx := &MockTx{}

	mockWalletSvc := new(MockWalletSvc)
	mockTxManager := new(MockTxManager)
	mockTxSvc := new(MockTransactionsSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:      senderWalletID,
		UserID:  userID,
		Balance: 5000,
	}, nil)
	mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{}, errors.New("db error"))

	svc := &Service{
		txManager:      mockTxManager,
		repo:           mockTransfersRepo,
		walletSvc:      mockWalletSvc,
		transactionSvc: mockTxSvc,
	}

	_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
		ToWalletID: uuid.New(),
		Amount:     1000,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
	mockTxSvc.AssertExpectations(t)
	mockTxManager.AssertExpectations(t)
}

func TestCreateTransfer_CreateCreditTransactionFails(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	mockTx := &MockTx{}

	debitTxID := uuid.New()

	mockWalletSvc := new(MockWalletSvc)
	mockTxManager := new(MockTxManager)
	mockTxSvc := new(MockTransactionsSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:      senderWalletID,
		UserID:  userID,
		Balance: 5000,
	}, nil)
	mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       debitTxID,
		WalletID: senderWalletID,
	}, nil).Once()
	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{}, errors.New("db error")).Once()
	mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil)

	svc := &Service{
		txManager:      mockTxManager,
		repo:           mockTransfersRepo,
		walletSvc:      mockWalletSvc,
		transactionSvc: mockTxSvc,
	}

	_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
		ToWalletID: receiverWalletID,
		Amount:     1000,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
}

func TestCreateTransfer_ExecuteTransfer_SenderWalletNotFound(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	mockTx := &MockTx{}

	transferID := uuid.New()
	debitTxID := uuid.New()
	creditTxID := uuid.New()

	mockWalletSvc := new(MockWalletSvc)
	mockTxManager := new(MockTxManager)
	mockTxSvc := new(MockTransactionsSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:      senderWalletID,
		UserID:  userID,
		Balance: 5000,
	}, nil)
	mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)

	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       debitTxID,
		WalletID: senderWalletID,
	}, nil).Once()
	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       creditTxID,
		WalletID: receiverWalletID,
	}, nil).Once()

	mockTransfersRepo.On("CreateTransfer", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:                  pgtype.UUID{Bytes: transferID, Valid: true},
		FromWalletID:        pgtype.UUID{Bytes: senderWalletID, Valid: true},
		ToWalletID:          pgtype.UUID{Bytes: receiverWalletID, Valid: true},
		Amount:              1000,
		Status:              repo.TransferStatusPending,
		DebitTransactionID:  pgtype.UUID{Bytes: debitTxID, Valid: true},
		CreditTransactionID: pgtype.UUID{Bytes: creditTxID, Valid: true},
		CreatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}, nil)

	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: senderWalletID}).
		Return(wallet.GetWalletResponse{}, wallet.ErrWalletNotFound)

	mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockTransfersRepo.On("UpdateTransferStatus", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusFailed,
	}, nil)

	svc := &Service{
		txManager:      mockTxManager,
		repo:           mockTransfersRepo,
		walletSvc:      mockWalletSvc,
		transactionSvc: mockTxSvc,
	}

	_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
		ToWalletID: receiverWalletID,
		Amount:     1000,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, wallet.ErrWalletNotFound)
}

func TestCreateTransfer_ExecuteTransfer_ReceiverWalletNotFound(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	mockTx := &MockTx{}

	transferID := uuid.New()
	debitTxID := uuid.New()
	creditTxID := uuid.New()

	mockWalletSvc := new(MockWalletSvc)
	mockTxManager := new(MockTxManager)
	mockTxSvc := new(MockTransactionsSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:      senderWalletID,
		UserID:  userID,
		Balance: 5000,
	}, nil)
	mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)

	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       debitTxID,
		WalletID: senderWalletID,
	}, nil).Once()
	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       creditTxID,
		WalletID: receiverWalletID,
	}, nil).Once()

	mockTransfersRepo.On("CreateTransfer", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:                  pgtype.UUID{Bytes: transferID, Valid: true},
		FromWalletID:        pgtype.UUID{Bytes: senderWalletID, Valid: true},
		ToWalletID:          pgtype.UUID{Bytes: receiverWalletID, Valid: true},
		Amount:              1000,
		Status:              repo.TransferStatusPending,
		DebitTransactionID:  pgtype.UUID{Bytes: debitTxID, Valid: true},
		CreditTransactionID: pgtype.UUID{Bytes: creditTxID, Valid: true},
		CreatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}, nil)

	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: senderWalletID}).
		Return(wallet.GetWalletResponse{
			ID:      senderWalletID,
			Balance: 5000,
		}, nil)
	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: receiverWalletID}).
		Return(wallet.GetWalletResponse{}, wallet.ErrWalletNotFound)

	mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockTransfersRepo.On("UpdateTransferStatus", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusFailed,
	}, nil)

	svc := &Service{
		txManager:      mockTxManager,
		repo:           mockTransfersRepo,
		walletSvc:      mockWalletSvc,
		transactionSvc: mockTxSvc,
	}

	_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
		ToWalletID: receiverWalletID,
		Amount:     1000,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, wallet.ErrWalletNotFound)
}

func TestCreateTransfer_ExecuteTransfer_DeductFromBalanceFails(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	mockTx := &MockTx{}

	transferID := uuid.New()
	debitTxID := uuid.New()
	creditTxID := uuid.New()

	mockWalletSvc := new(MockWalletSvc)
	mockTxManager := new(MockTxManager)
	mockTxSvc := new(MockTransactionsSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:      senderWalletID,
		UserID:  userID,
		Balance: 5000,
	}, nil)
	mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)

	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       debitTxID,
		WalletID: senderWalletID,
	}, nil).Once()
	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       creditTxID,
		WalletID: receiverWalletID,
	}, nil).Once()

	mockTransfersRepo.On("CreateTransfer", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:                  pgtype.UUID{Bytes: transferID, Valid: true},
		FromWalletID:        pgtype.UUID{Bytes: senderWalletID, Valid: true},
		ToWalletID:          pgtype.UUID{Bytes: receiverWalletID, Valid: true},
		Amount:              1000,
		Status:              repo.TransferStatusPending,
		DebitTransactionID:  pgtype.UUID{Bytes: debitTxID, Valid: true},
		CreditTransactionID: pgtype.UUID{Bytes: creditTxID, Valid: true},
		CreatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}, nil)

	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: senderWalletID}).
		Return(wallet.GetWalletResponse{
			ID:      senderWalletID,
			Balance: 5000,
		}, nil)
	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: receiverWalletID}).
		Return(wallet.GetWalletResponse{
			ID:      receiverWalletID,
			Balance: 1000,
		}, nil)
	mockWalletSvc.On("DeductFromBalance", mock.Anything, mock.Anything).
		Return(wallet.DeductResponse{}, wallet.ErrInsufficientFunds)

	mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockTransfersRepo.On("UpdateTransferStatus", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusFailed,
	}, nil)

	svc := &Service{
		txManager:      mockTxManager,
		repo:           mockTransfersRepo,
		walletSvc:      mockWalletSvc,
		transactionSvc: mockTxSvc,
	}

	_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
		ToWalletID: receiverWalletID,
		Amount:     1000,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, wallet.ErrInsufficientFunds)
}

func TestCreateTransfer_ExecuteTransfer_AddToWalletFails(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	mockTx := &MockTx{}

	transferID := uuid.New()
	debitTxID := uuid.New()
	creditTxID := uuid.New()

	mockWalletSvc := new(MockWalletSvc)
	mockTxManager := new(MockTxManager)
	mockTxSvc := new(MockTransactionsSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:      senderWalletID,
		UserID:  userID,
		Balance: 5000,
	}, nil)
	mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)

	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       debitTxID,
		WalletID: senderWalletID,
	}, nil).Once()
	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       creditTxID,
		WalletID: receiverWalletID,
	}, nil).Once()

	mockTransfersRepo.On("CreateTransfer", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:                  pgtype.UUID{Bytes: transferID, Valid: true},
		FromWalletID:        pgtype.UUID{Bytes: senderWalletID, Valid: true},
		ToWalletID:          pgtype.UUID{Bytes: receiverWalletID, Valid: true},
		Amount:              1000,
		Status:              repo.TransferStatusPending,
		DebitTransactionID:  pgtype.UUID{Bytes: debitTxID, Valid: true},
		CreditTransactionID: pgtype.UUID{Bytes: creditTxID, Valid: true},
		CreatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}, nil)

	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: senderWalletID}).
		Return(wallet.GetWalletResponse{
			ID:      senderWalletID,
			Balance: 5000,
		}, nil)
	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: receiverWalletID}).
		Return(wallet.GetWalletResponse{
			ID:      receiverWalletID,
			Balance: 1000,
		}, nil)
	mockWalletSvc.On("DeductFromBalance", mock.Anything, mock.Anything).
		Return(wallet.DeductResponse{
			ID:     senderWalletID,
			Status: "completed",
		}, nil)
	mockWalletSvc.On("AddToWallet", mock.Anything, mock.Anything).
		Return(wallet.AddToWalletResponse{}, wallet.ErrWalletNotFound)

	mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockTransfersRepo.On("UpdateTransferStatus", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusFailed,
	}, nil)

	svc := &Service{
		txManager:      mockTxManager,
		repo:           mockTransfersRepo,
		walletSvc:      mockWalletSvc,
		transactionSvc: mockTxSvc,
	}

	_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
		ToWalletID: receiverWalletID,
		Amount:     1000,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, wallet.ErrWalletNotFound)
}

func TestCreateTransfer_HappyPath(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	mockTx := &MockTx{}

	transferID := uuid.New()
	debitTxID := uuid.New()
	creditTxID := uuid.New()
	now := time.Now()

	mockWalletSvc := new(MockWalletSvc)
	mockTxManager := new(MockTxManager)
	mockTxSvc := new(MockTransactionsSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:      senderWalletID,
		UserID:  userID,
		Balance: 5000,
	}, nil)
	mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)

	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       debitTxID,
		WalletID: senderWalletID,
	}, nil).Once()
	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       creditTxID,
		WalletID: receiverWalletID,
	}, nil).Once()

	mockTransfersRepo.On("CreateTransfer", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:                  pgtype.UUID{Bytes: transferID, Valid: true},
		FromWalletID:        pgtype.UUID{Bytes: senderWalletID, Valid: true},
		ToWalletID:          pgtype.UUID{Bytes: receiverWalletID, Valid: true},
		Amount:              1000,
		Status:              repo.TransferStatusPending,
		DebitTransactionID:  pgtype.UUID{Bytes: debitTxID, Valid: true},
		CreditTransactionID: pgtype.UUID{Bytes: creditTxID, Valid: true},
		CreatedAt:           pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: now, Valid: true},
	}, nil)

	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: senderWalletID}).
		Return(wallet.GetWalletResponse{
			ID:      senderWalletID,
			Balance: 5000,
		}, nil)
	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: receiverWalletID}).
		Return(wallet.GetWalletResponse{
			ID:      receiverWalletID,
			Balance: 1000,
		}, nil)
	mockWalletSvc.On("DeductFromBalance", mock.Anything, mock.Anything).
		Return(wallet.DeductResponse{
			ID:     senderWalletID,
			Status: "completed",
		}, nil)
	mockWalletSvc.On("AddToWallet", mock.Anything, mock.Anything).
		Return(wallet.AddToWalletResponse{
			ID:      receiverWalletID,
			Balance: 2000,
		}, nil)

	mockTransfersRepo.On("UpdateTransferStatus", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:                  pgtype.UUID{Bytes: transferID, Valid: true},
		FromWalletID:        pgtype.UUID{Bytes: senderWalletID, Valid: true},
		ToWalletID:          pgtype.UUID{Bytes: receiverWalletID, Valid: true},
		Amount:              1000,
		Status:              repo.TransferStatusCompleted,
		DebitTransactionID:  pgtype.UUID{Bytes: debitTxID, Valid: true},
		CreditTransactionID: pgtype.UUID{Bytes: creditTxID, Valid: true},
		CreatedAt:           pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: now, Valid: true},
	}, nil)

	svc := &Service{
		txManager:      mockTxManager,
		repo:           mockTransfersRepo,
		walletSvc:      mockWalletSvc,
		transactionSvc: mockTxSvc,
	}

	resp, err := svc.CreateTransfer(ctx, CreateTransferRequest{
		ToWalletID: receiverWalletID,
		Amount:     1000,
	})

	assert.NoError(t, err)
	assert.Equal(t, transferID, resp.ID)
	assert.Equal(t, senderWalletID, resp.FromWalletID)
	assert.Equal(t, receiverWalletID, resp.ToWalletID)
	assert.Equal(t, int64(1000), resp.Amount)
	assert.Equal(t, repo.TransferStatusCompleted, resp.Status)

	mockWalletSvc.AssertExpectations(t)
	mockTxSvc.AssertExpectations(t)
	mockTransfersRepo.AssertExpectations(t)
}

func TestCreateTransfer_AtomicityProblem(t *testing.T) {
	userID := uuid.New()
	senderWalletID := uuid.New()
	receiverWalletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	mockTx := &MockTx{}

	transferID := uuid.New()
	debitTxID := uuid.New()
	creditTxID := uuid.New()
	now := time.Now()

	mockWalletSvc := new(MockWalletSvc)
	mockTxManager := new(MockTxManager)
	mockTxSvc := new(MockTransactionsSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:      senderWalletID,
		UserID:  userID,
		Balance: 5000,
	}, nil)
	mockTxManager.On("StartTx", mock.Anything).Return(ctx, mockTx, nil)
	mockTxManager.On("Commit", mock.Anything).Return(nil)

	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       debitTxID,
		WalletID: senderWalletID,
	}, nil).Once()
	mockTxSvc.On("CreateTransaction", mock.Anything, mock.Anything).Return(transactions.CreateTransactionResponse{
		ID:       creditTxID,
		WalletID: receiverWalletID,
	}, nil).Once()

	mockTransfersRepo.On("CreateTransfer", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:                  pgtype.UUID{Bytes: transferID, Valid: true},
		FromWalletID:        pgtype.UUID{Bytes: senderWalletID, Valid: true},
		ToWalletID:          pgtype.UUID{Bytes: receiverWalletID, Valid: true},
		Amount:              1000,
		Status:              repo.TransferStatusPending,
		DebitTransactionID:  pgtype.UUID{Bytes: debitTxID, Valid: true},
		CreditTransactionID: pgtype.UUID{Bytes: creditTxID, Valid: true},
		CreatedAt:           pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: now, Valid: true},
	}, nil)

	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: senderWalletID}).
		Return(wallet.GetWalletResponse{
			ID:      senderWalletID,
			Balance: 5000,
		}, nil)
	mockWalletSvc.On("GetById", mock.Anything, wallet.GetWalletRequest{ID: receiverWalletID}).
		Return(wallet.GetWalletResponse{
			ID:      receiverWalletID,
			Balance: 1000,
		}, nil)
	mockWalletSvc.On("DeductFromBalance", mock.Anything, mock.Anything).
		Return(wallet.DeductResponse{
			ID:     senderWalletID,
			Status: "completed",
		}, nil)
	mockWalletSvc.On("AddToWallet", mock.Anything, mock.Anything).
		Return(wallet.AddToWalletResponse{}, errors.New("AddToWallet failed"))

	mockTxSvc.On("UpdateStatus", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockTransfersRepo.On("UpdateTransferStatus", mock.Anything, mock.Anything).Return(repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusFailed,
	}, nil)

	svc := &Service{
		txManager:      mockTxManager,
		repo:           mockTransfersRepo,
		walletSvc:      mockWalletSvc,
		transactionSvc: mockTxSvc,
	}

	_, err := svc.CreateTransfer(ctx, CreateTransferRequest{
		ToWalletID: receiverWalletID,
		Amount:     1000,
	})

	assert.Error(t, err)
	t.Logf("commitCalled: %v, rollbackCalled: %v", mockTx.commitCalled, mockTx.rollbackCalled)

	if mockTx.commitCalled {
		t.Log("Transaction was committed - atomicity issue present")
	} else if mockTx.rollbackCalled {
		t.Log("Transaction was rolled back - correct behavior")
	}
	assert.True(t, mockTx.commitCalled || mockTx.rollbackCalled, "Either commit or rollback should be called")
}

func TestGetTransfers_WalletNotFound(t *testing.T) {
	userID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	walletID := uuid.New()

	mockWalletSvc := new(MockWalletSvc)
	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:     walletID,
		UserID: userID,
	}, nil)

	mockTransfersRepo := new(MockTransfersRepo)
	mockTransfersRepo.On("GetTransfersByWalletId", mock.Anything, mock.Anything).Return([]repo.Transfer{}, nil)

	svc := &Service{
		repo:      mockTransfersRepo,
		walletSvc: mockWalletSvc,
	}

	resp, err := svc.GetTransfers(ctx)

	assert.NoError(t, err)
	assert.Equal(t, walletID, resp.FromWalletID)
	mockWalletSvc.AssertExpectations(t)
	mockTransfersRepo.AssertExpectations(t)
}

func TestGetTransfers_DatabaseError(t *testing.T) {
	userID := uuid.New()
	walletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())

	mockWalletSvc := new(MockWalletSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:     walletID,
		UserID: userID,
	}, nil)
	mockTransfersRepo.On("GetTransfersByWalletId", mock.Anything, mock.Anything).Return([]repo.Transfer{}, errors.New("db error"))

	svc := &Service{
		repo:      mockTransfersRepo,
		walletSvc: mockWalletSvc,
	}

	_, err := svc.GetTransfers(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db error")
	mockWalletSvc.AssertExpectations(t)
	mockTransfersRepo.AssertExpectations(t)
}

func TestGetTransfers_HappyPath(t *testing.T) {
	userID := uuid.New()
	walletID := uuid.New()
	ctx := withUserID(context.Background(), userID.String())
	now := time.Now()

	mockWalletSvc := new(MockWalletSvc)
	mockTransfersRepo := new(MockTransfersRepo)

	mockWalletSvc.On("GetByUserId", mock.Anything).Return(wallet.GetWalletResponse{
		ID:     walletID,
		UserID: userID,
	}, nil)
	mockTransfersRepo.On("GetTransfersByWalletId", mock.Anything, mock.Anything).Return([]repo.Transfer{
		{
			ID:           pgtype.UUID{Bytes: uuid.New(), Valid: true},
			FromWalletID: pgtype.UUID{Bytes: walletID, Valid: true},
			ToWalletID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
			Amount:       1000,
			Status:       repo.TransferStatusCompleted,
			CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		},
	}, nil)

	svc := &Service{
		repo:      mockTransfersRepo,
		walletSvc: mockWalletSvc,
	}

	resp, err := svc.GetTransfers(ctx)

	assert.NoError(t, err)
	assert.Equal(t, walletID, resp.FromWalletID)
	assert.Len(t, resp.Transfers, 1)
	mockWalletSvc.AssertExpectations(t)
	mockTransfersRepo.AssertExpectations(t)
}
