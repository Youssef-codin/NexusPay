package transactions

import (
	"context"
	"errors"
	"testing"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockService stands in for IService so the scheduler can be tested without a
// database. It records every Complete call so tests can assert which rows the
// tick handed over and in which state.
type MockService struct {
	mock.Mock
	IService
	completed []completeCall
}

type completeCall struct {
	id   uuid.UUID
	from repo.TransactionStatus
}

func (m *MockService) Complete(
	ctx context.Context,
	id uuid.UUID,
	from repo.TransactionStatus,
) (repo.Transaction, error) {
	m.completed = append(m.completed, completeCall{id: id, from: from})
	args := m.Called(ctx, id, from)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func newScheduler() (*Scheduler, *MockService, *MockRepo) {
	svc := &MockService{}
	r := &MockRepo{}
	return NewScheduler(svc, r), svc, r
}

func idRow(id uuid.UUID) repo.Transaction {
	return repo.Transaction{ID: pgtype.UUID{Bytes: id, Valid: true}}
}

func TestRunOnce_CompletesEachDueRowFromScheduled(t *testing.T) {
	s, svc, r := newScheduler()
	first, second := uuid.New(), uuid.New()

	r.On("ClaimDueTransactions", mock.Anything, int32(batchSize)).
		Return([]repo.Transaction{idRow(first), idRow(second)}, nil)
	svc.On("Complete", mock.Anything, mock.Anything, repo.TransactionStatusScheduled).
		Return(repo.Transaction{}, nil)

	err := s.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, []completeCall{
		{id: first, from: repo.TransactionStatusScheduled},
		{id: second, from: repo.TransactionStatusScheduled},
	}, svc.completed)
}

func TestSweepOnce_CompletesStuckRowsFromCrediting(t *testing.T) {
	s, svc, r := newScheduler()
	id := uuid.New()

	r.On("ClaimStuckCrediting", mock.Anything, int32(batchSize)).
		Return([]repo.Transaction{idRow(id)}, nil)
	svc.On("Complete", mock.Anything, id, repo.TransactionStatusCrediting).
		Return(repo.Transaction{}, nil)

	err := s.SweepOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, []completeCall{{id: id, from: repo.TransactionStatusCrediting}}, svc.completed)
}

// Losing a row to another tick is normal, not a failure. A tick that surfaced it
// as an error would make overlapping schedulers look broken.
func TestRunOnce_AlreadyProcessedIsNotAnError(t *testing.T) {
	s, svc, r := newScheduler()

	r.On("ClaimDueTransactions", mock.Anything, mock.Anything).
		Return([]repo.Transaction{idRow(uuid.New())}, nil)
	svc.On("Complete", mock.Anything, mock.Anything, mock.Anything).
		Return(repo.Transaction{}, ErrAlreadyProcessed)

	assert.NoError(t, s.RunOnce(context.Background()))
}

// One bad row must not strand the rest of the batch behind it.
func TestRunOnce_ContinuesPastAFailingRow(t *testing.T) {
	s, svc, r := newScheduler()
	bad, good := uuid.New(), uuid.New()

	r.On("ClaimDueTransactions", mock.Anything, mock.Anything).
		Return([]repo.Transaction{idRow(bad), idRow(good)}, nil)
	svc.On("Complete", mock.Anything, bad, mock.Anything).
		Return(repo.Transaction{}, ErrInsufficientFunds)
	svc.On("Complete", mock.Anything, good, mock.Anything).
		Return(repo.Transaction{}, nil)

	err := s.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Len(t, svc.completed, 2, "the row after the failure must still be attempted")
}

// A claim that cannot run is a real error: there is nothing to fall back on.
func TestRunOnce_ClaimFailureIsReturned(t *testing.T) {
	s, _, r := newScheduler()
	claimErr := errors.New("connection refused")

	r.On("ClaimDueTransactions", mock.Anything, mock.Anything).
		Return([]repo.Transaction{}, claimErr)

	assert.ErrorIs(t, s.RunOnce(context.Background()), claimErr)
}

func TestRunOnce_EmptyBatchDoesNothing(t *testing.T) {
	s, svc, r := newScheduler()

	r.On("ClaimDueTransactions", mock.Anything, mock.Anything).
		Return([]repo.Transaction{}, nil)

	assert.NoError(t, s.RunOnce(context.Background()))
	assert.Empty(t, svc.completed)
	svc.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything)
}
