package transactions

import (
	"context"
	"errors"
	"testing"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockTransactionRepo struct {
	mock.Mock
}

func (m *MockTransactionRepo) CreateTransaction(ctx context.Context, arg repo.CreateTransactionParams) (repo.CreateTransactionRow, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.CreateTransactionRow), args.Error(1)
}

func (m *MockTransactionRepo) GetTransactionById(ctx context.Context, id pgtype.UUID) (repo.Transaction, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func (m *MockTransactionRepo) GetTransactionByTransferId(ctx context.Context, transferID pgtype.UUID) (repo.Transaction, error) {
	args := m.Called(ctx, transferID)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func (m *MockTransactionRepo) GetTransactionsByWalletId(ctx context.Context, walletID pgtype.UUID) ([]repo.Transaction, error) {
	args := m.Called(ctx, walletID)
	return args.Get(0).([]repo.Transaction), args.Error(1)
}

func (m *MockTransactionRepo) UpdateTransactionStatus(ctx context.Context, arg repo.UpdateTransactionStatusParams) (repo.Transaction, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func TestCreateTransaction(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	transactionID := uuid.New()
	now := time.Now()

	t.Run("success", func(t *testing.T) {
		mockRepo := new(MockTransactionRepo)

		mockRepo.On("CreateTransaction", mock.Anything, mock.Anything).Return(repo.CreateTransactionRow{
			ID:          pgtype.UUID{Bytes: transactionID, Valid: true},
			WalletID:    pgtype.UUID{Bytes: walletID, Valid: true},
			Amount:      1000,
			Type:        repo.TransactionTypeCredit,
			Status:      repo.TransactionStatusPending,
			CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
			Description: pgtype.Text{String: "Test transaction", Valid: true},
		}, nil)

		svc := &Service{repo: mockRepo}
		resp, err := svc.CreateTransaction(ctx, CreateTransactionRequest{
			WalletID:    walletID.String(),
			Amount:      1000,
			Type:        repo.TransactionTypeCredit,
			Status:      repo.TransactionStatusPending,
			Description: "Test transaction",
		})

		assert.NoError(t, err)
		assert.Equal(t, transactionID.String(), resp.ID)
		assert.Equal(t, walletID.String(), resp.WalletID)
		assert.Equal(t, int64(1000), resp.Amount)
		assert.Equal(t, repo.TransactionTypeCredit, resp.Type)
		mockRepo.AssertExpectations(t)
	})

	t.Run("database_error", func(t *testing.T) {
		mockRepo := new(MockTransactionRepo)
		mockRepo.On("CreateTransaction", mock.Anything, mock.Anything).Return(repo.CreateTransactionRow{}, errors.New("db error"))

		svc := &Service{repo: mockRepo}
		_, err := svc.CreateTransaction(ctx, CreateTransactionRequest{
			WalletID:    walletID.String(),
			Amount:      1000,
			Type:        repo.TransactionTypeCredit,
			Status:      repo.TransactionStatusPending,
			Description: "Test transaction",
		})

		assert.Error(t, err)
		mockRepo.AssertExpectations(t)
	})
}

func TestGetById(t *testing.T) {
	ctx := context.Background()
	transactionID := uuid.New()
	walletID := uuid.New()
	now := time.Now()

	t.Run("transaction_exists", func(t *testing.T) {
		mockRepo := new(MockTransactionRepo)

		mockRepo.On("GetTransactionById", mock.Anything, mock.Anything).Return(repo.Transaction{
			ID:        pgtype.UUID{Bytes: transactionID, Valid: true},
			WalletID:  pgtype.UUID{Bytes: walletID, Valid: true},
			Amount:    1000,
			Type:      repo.TransactionTypeCredit,
			Status:    repo.TransactionStatusPending,
			CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
			UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}, nil)

		svc := &Service{repo: mockRepo}
		resp, err := svc.GetById(ctx, GetByIdRequest{ID: transactionID.String()})

		assert.NoError(t, err)
		assert.Equal(t, transactionID.String(), resp.ID)
		assert.Equal(t, walletID.String(), resp.WalletID)
		mockRepo.AssertExpectations(t)
	})

	t.Run("transaction_not_found", func(t *testing.T) {
		mockRepo := new(MockTransactionRepo)
		mockRepo.On("GetTransactionById", mock.Anything, mock.Anything).Return(repo.Transaction{}, pgx.ErrNoRows)

		svc := &Service{repo: mockRepo}
		_, err := svc.GetById(ctx, GetByIdRequest{ID: uuid.New().String()})

		assert.ErrorIs(t, err, ErrTransactionNotFound)
		mockRepo.AssertExpectations(t)
	})

	t.Run("database_error", func(t *testing.T) {
		mockRepo := new(MockTransactionRepo)
		mockRepo.On("GetTransactionById", mock.Anything, mock.Anything).Return(repo.Transaction{}, errors.New("db error"))

		svc := &Service{repo: mockRepo}
		_, err := svc.GetById(ctx, GetByIdRequest{ID: uuid.New().String()})

		assert.Error(t, err)
		assert.NotEqual(t, ErrTransactionNotFound, err)
		mockRepo.AssertExpectations(t)
	})
}

func TestUpdateStatus(t *testing.T) {
	ctx := context.Background()
	transactionID := uuid.New()

	validTransitions := []struct {
		name string
		from repo.TransactionStatus
		to   repo.TransactionStatus
	}{
		{"pending_to_processing", repo.TransactionStatusPending, repo.TransactionStatusProcessing},
		{"pending_to_reversed", repo.TransactionStatusPending, repo.TransactionStatusReversed},
		{"processing_to_completed", repo.TransactionStatusProcessing, repo.TransactionStatusCompleted},
		{"processing_to_failed", repo.TransactionStatusProcessing, repo.TransactionStatusFailed},
		{"completed_to_reversing", repo.TransactionStatusCompleted, repo.TransactionStatusReversing},
		{"reversing_to_reversed", repo.TransactionStatusReversing, repo.TransactionStatusReversed},
		{"reversing_to_failed", repo.TransactionStatusReversing, repo.TransactionStatusFailed},
	}

	for _, tt := range validTransitions {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockTransactionRepo)

			mockRepo.On("GetTransactionById", mock.Anything, mock.Anything).Return(repo.Transaction{
				ID:     pgtype.UUID{Bytes: transactionID, Valid: true},
				Status: tt.from,
			}, nil)
			mockRepo.On("UpdateTransactionStatus", mock.Anything, mock.Anything).Return(repo.Transaction{
				ID:     pgtype.UUID{Bytes: transactionID, Valid: true},
				Status: tt.to,
			}, nil)

			svc := &Service{repo: mockRepo}
			err := svc.UpdateStatus(ctx, UpdateTransactionRequest{
				ID:     transactionID.String(),
				Status: tt.to,
			})

			assert.NoError(t, err)
			mockRepo.AssertExpectations(t)
		})
	}

	invalidTransitions := []struct {
		name string
		from repo.TransactionStatus
		to   repo.TransactionStatus
	}{
		{"pending_to_completed", repo.TransactionStatusPending, repo.TransactionStatusCompleted},
		{"pending_to_failed", repo.TransactionStatusPending, repo.TransactionStatusFailed},
		{"processing_to_pending", repo.TransactionStatusProcessing, repo.TransactionStatusPending},
		{"completed_to_processing", repo.TransactionStatusCompleted, repo.TransactionStatusProcessing},
		{"failed_to_pending", repo.TransactionStatusFailed, repo.TransactionStatusPending},
		{"failed_to_processing", repo.TransactionStatusFailed, repo.TransactionStatusProcessing},
		{"reversed_to_pending", repo.TransactionStatusReversed, repo.TransactionStatusPending},
		{"reversed_to_processing", repo.TransactionStatusReversed, repo.TransactionStatusProcessing},
	}

	for _, tt := range invalidTransitions {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockTransactionRepo)

			mockRepo.On("GetTransactionById", mock.Anything, mock.Anything).Return(repo.Transaction{
				ID:     pgtype.UUID{Bytes: transactionID, Valid: true},
				Status: tt.from,
			}, nil)

			svc := &Service{repo: mockRepo}
			err := svc.UpdateStatus(ctx, UpdateTransactionRequest{
				ID:     transactionID.String(),
				Status: tt.to,
			})

			assert.ErrorIs(t, err, ErrInvalidStatusTransition)
			mockRepo.AssertExpectations(t)
		})
	}

	t.Run("same_status", func(t *testing.T) {
		mockRepo := new(MockTransactionRepo)

		mockRepo.On("GetTransactionById", mock.Anything, mock.Anything).Return(repo.Transaction{
			ID:     pgtype.UUID{Bytes: transactionID, Valid: true},
			Status: repo.TransactionStatusPending,
		}, nil)

		svc := &Service{repo: mockRepo}
		err := svc.UpdateStatus(ctx, UpdateTransactionRequest{
			ID:     transactionID.String(),
			Status: repo.TransactionStatusPending,
		})

		assert.ErrorIs(t, err, ErrAlreadySameStatus)
		mockRepo.AssertExpectations(t)
	})

	t.Run("transaction_not_found", func(t *testing.T) {
		mockRepo := new(MockTransactionRepo)
		mockRepo.On("GetTransactionById", mock.Anything, mock.Anything).Return(repo.Transaction{}, pgx.ErrNoRows)

		svc := &Service{repo: mockRepo}
		err := svc.UpdateStatus(ctx, UpdateTransactionRequest{
			ID:     uuid.New().String(),
			Status: repo.TransactionStatusProcessing,
		})

		assert.ErrorIs(t, err, ErrTransactionNotFound)
		mockRepo.AssertExpectations(t)
	})

	t.Run("database_error_on_update", func(t *testing.T) {
		mockRepo := new(MockTransactionRepo)

		mockRepo.On("GetTransactionById", mock.Anything, mock.Anything).Return(repo.Transaction{
			ID:     pgtype.UUID{Bytes: transactionID, Valid: true},
			Status: repo.TransactionStatusPending,
		}, nil)
		mockRepo.On("UpdateTransactionStatus", mock.Anything, mock.Anything).Return(repo.Transaction{}, errors.New("db error"))

		svc := &Service{repo: mockRepo}
		err := svc.UpdateStatus(ctx, UpdateTransactionRequest{
			ID:     transactionID.String(),
			Status: repo.TransactionStatusProcessing,
		})

		assert.Error(t, err)
		mockRepo.AssertExpectations(t)
	})
}
