package transfers

import (
	"context"
	"errors"
	"testing"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockTransfersSvc struct {
	mock.Mock
}

func (m *MockTransfersSvc) CreateTransfer(
	ctx context.Context,
	req CreateTransferRequest,
) (res CreateTransferResponse, err error) {
	args := m.Called(ctx, req)
	return args.Get(0).(CreateTransferResponse), args.Error(1)
}

func (m *MockTransfersSvc) GetTransfers(ctx context.Context) (res GetTransfersByIDResponse, err error) {
	args := m.Called(ctx)
	return args.Get(0).(GetTransfersByIDResponse), args.Error(1)
}

func (m *MockTransfersSvc) GetTransferByID(
	ctx context.Context,
	req GetTransferByIDRequest,
) (res GetTransferByIDResponse, err error) {
	args := m.Called(ctx, req)
	return args.Get(0).(GetTransferByIDResponse), args.Error(1)
}

func (m *MockTransfersSvc) ListScheduledTransfers(
	ctx context.Context,
	userID uuid.UUID,
) (res ListScheduledTransfersResponse, err error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(ListScheduledTransfersResponse), args.Error(1)
}

func (m *MockTransfersSvc) CancelScheduledTransfers(
	ctx context.Context,
	req CancelScheduledTransfersRequest,
) (res CancelScheduledTransfersResponse, err error) {
	args := m.Called(ctx, req)
	return args.Get(0).(CancelScheduledTransfersResponse), args.Error(1)
}

func (m *MockTransfersSvc) ExecuteTransfer(
	ctx context.Context,
	t repo.Transfer,
) (transfer repo.Transfer, err error) {
	args := m.Called(ctx, t)
	return args.Get(0).(repo.Transfer), args.Error(1)
}

func TestProcessScheduledTransfers_NoPendingTransfers(t *testing.T) {
	mockTransfersRepo := new(MockTransfersRepo)
	mockTransfersSvc := new(MockTransfersSvc)

	mockTransfersRepo.On("GetPendingScheduledTransfers", mock.Anything, mock.Anything).Return([]repo.ScheduledTransfer{}, nil)

	svc := &Scheduler{
		transfersRepo: mockTransfersRepo,
		transfersSvc:  mockTransfersSvc,
	}

	svc.processScheduledTransfers()

	mockTransfersRepo.AssertExpectations(t)
	mockTransfersSvc.AssertExpectations(t)
}

func TestProcessScheduledTransfers_GetPendingFails(t *testing.T) {
	mockTransfersRepo := new(MockTransfersRepo)
	mockTransfersSvc := new(MockTransfersSvc)

	mockTransfersRepo.On("GetPendingScheduledTransfers", mock.Anything, mock.Anything).Return([]repo.ScheduledTransfer{}, errors.New("db error"))

	svc := &Scheduler{
		transfersRepo: mockTransfersRepo,
		transfersSvc:  mockTransfersSvc,
	}

	svc.processScheduledTransfers()

	mockTransfersRepo.AssertExpectations(t)
}

func TestProcessScheduledTransfers_GetTransferFails(t *testing.T) {
	mockTransfersRepo := new(MockTransfersRepo)
	mockTransfersSvc := new(MockTransfersSvc)

	scheduledTransferID := uuid.New()
	transferID := uuid.New()

	scheduledTransfer := repo.ScheduledTransfer{
		ID:         pgtype.UUID{Bytes: scheduledTransferID, Valid: true},
		TransferID: pgtype.UUID{Bytes: transferID, Valid: true},
		ExecutedAt: pgtype.Timestamptz{Valid: false},
	}

	mockTransfersRepo.On("GetPendingScheduledTransfers", mock.Anything, mock.Anything).Return([]repo.ScheduledTransfer{scheduledTransfer}, nil)
	mockTransfersRepo.On("GetTransferById", mock.Anything, mock.Anything).Return(repo.Transfer{}, errors.New("db error"))

	svc := &Scheduler{
		transfersRepo: mockTransfersRepo,
		transfersSvc:  mockTransfersSvc,
	}

	svc.processScheduledTransfers()

	mockTransfersRepo.AssertExpectations(t)
	mockTransfersSvc.AssertExpectations(t)
}

func TestProcessScheduledTransfers_TransferNotPending(t *testing.T) {
	mockTransfersRepo := new(MockTransfersRepo)
	mockTransfersSvc := new(MockTransfersSvc)

	scheduledTransferID := uuid.New()
	transferID := uuid.New()

	scheduledTransfer := repo.ScheduledTransfer{
		ID:         pgtype.UUID{Bytes: scheduledTransferID, Valid: true},
		TransferID: pgtype.UUID{Bytes: transferID, Valid: true},
		ExecutedAt: pgtype.Timestamptz{Valid: false},
	}

	transfer := repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusCompleted,
	}

	mockTransfersRepo.On("GetPendingScheduledTransfers", mock.Anything, mock.Anything).Return([]repo.ScheduledTransfer{scheduledTransfer}, nil)
	mockTransfersRepo.On("GetTransferById", mock.Anything, mock.Anything).Return(transfer, nil)

	svc := &Scheduler{
		transfersRepo: mockTransfersRepo,
		transfersSvc:  mockTransfersSvc,
	}

	svc.processScheduledTransfers()

	mockTransfersRepo.AssertExpectations(t)
	mockTransfersSvc.AssertExpectations(t)
}

func TestProcessScheduledTransfers_ExecuteFails(t *testing.T) {
	mockTransfersRepo := new(MockTransfersRepo)
	mockTransfersSvc := new(MockTransfersSvc)

	scheduledTransferID := uuid.New()
	transferID := uuid.New()

	scheduledTransfer := repo.ScheduledTransfer{
		ID:         pgtype.UUID{Bytes: scheduledTransferID, Valid: true},
		TransferID: pgtype.UUID{Bytes: transferID, Valid: true},
		ExecutedAt: pgtype.Timestamptz{Valid: false},
	}

	transfer := repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusPending,
	}

	mockTransfersRepo.On("GetPendingScheduledTransfers", mock.Anything, mock.Anything).Return([]repo.ScheduledTransfer{scheduledTransfer}, nil)
	mockTransfersRepo.On("GetTransferById", mock.Anything, mock.Anything).Return(transfer, nil)
	mockTransfersSvc.On("ExecuteTransfer", mock.Anything, mock.Anything).Return(repo.Transfer{}, errors.New("insufficient funds"))

	svc := &Scheduler{
		transfersRepo: mockTransfersRepo,
		transfersSvc:  mockTransfersSvc,
	}

	svc.processScheduledTransfers()

	mockTransfersRepo.AssertExpectations(t)
	mockTransfersSvc.AssertExpectations(t)
}

func TestProcessScheduledTransfers_MarkExecutedFails(t *testing.T) {
	mockTransfersRepo := new(MockTransfersRepo)
	mockTransfersSvc := new(MockTransfersSvc)

	scheduledTransferID := uuid.New()
	transferID := uuid.New()

	scheduledTransfer := repo.ScheduledTransfer{
		ID:         pgtype.UUID{Bytes: scheduledTransferID, Valid: true},
		TransferID: pgtype.UUID{Bytes: transferID, Valid: true},
		ExecutedAt: pgtype.Timestamptz{Valid: false},
	}

	transfer := repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusPending,
	}

	mockTransfersRepo.On("GetPendingScheduledTransfers", mock.Anything, mock.Anything).Return([]repo.ScheduledTransfer{scheduledTransfer}, nil)
	mockTransfersRepo.On("GetTransferById", mock.Anything, mock.Anything).Return(transfer, nil)
	mockTransfersSvc.On("ExecuteTransfer", mock.Anything, mock.Anything).Return(transfer, nil)
	mockTransfersRepo.On("MarkScheduledTransferExecuted", mock.Anything, mock.Anything).Return(repo.ScheduledTransfer{}, errors.New("db error"))

	svc := &Scheduler{
		transfersRepo: mockTransfersRepo,
		transfersSvc:  mockTransfersSvc,
	}

	svc.processScheduledTransfers()

	mockTransfersRepo.AssertExpectations(t)
	mockTransfersSvc.AssertExpectations(t)
}

func TestProcessScheduledTransfers_HappyPath(t *testing.T) {
	mockTransfersRepo := new(MockTransfersRepo)
	mockTransfersSvc := new(MockTransfersSvc)

	scheduledTransferID := uuid.New()
	transferID := uuid.New()

	scheduledTransfer := repo.ScheduledTransfer{
		ID:         pgtype.UUID{Bytes: scheduledTransferID, Valid: true},
		TransferID: pgtype.UUID{Bytes: transferID, Valid: true},
		ExecutedAt: pgtype.Timestamptz{Valid: false},
	}

	transfer := repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusPending,
	}

	completedTransfer := repo.Transfer{
		ID:     pgtype.UUID{Bytes: transferID, Valid: true},
		Status: repo.TransferStatusCompleted,
	}

	executedScheduledTransfer := repo.ScheduledTransfer{
		ID:         pgtype.UUID{Bytes: scheduledTransferID, Valid: true},
		TransferID: pgtype.UUID{Bytes: transferID, Valid: true},
		ExecutedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	mockTransfersRepo.On("GetPendingScheduledTransfers", mock.Anything, mock.Anything).Return([]repo.ScheduledTransfer{scheduledTransfer}, nil)
	mockTransfersRepo.On("GetTransferById", mock.Anything, mock.Anything).Return(transfer, nil)
	mockTransfersSvc.On("ExecuteTransfer", mock.Anything, mock.Anything).Return(completedTransfer, nil)
	mockTransfersRepo.On("MarkScheduledTransferExecuted", mock.Anything, mock.Anything).Return(executedScheduledTransfer, nil)

	svc := &Scheduler{
		transfersRepo: mockTransfersRepo,
		transfersSvc:  mockTransfersSvc,
	}

	svc.processScheduledTransfers()

	mockTransfersRepo.AssertExpectations(t)
	mockTransfersSvc.AssertExpectations(t)
}

func TestCancelAtExactScheduledTime(t *testing.T) {
	scheduledAt := time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC)
	now := scheduledAt

	scheduledAtTruncated := scheduledAt.UTC().Truncate(time.Minute)
	nowTruncated := now.UTC().Truncate(time.Minute)

	assert.False(t, nowTruncated.After(scheduledAtTruncated), "now.After(scheduledAt) should be false when now == scheduledAt")
	assert.Equal(t, nowTruncated, scheduledAtTruncated, "times should be equal")
}
