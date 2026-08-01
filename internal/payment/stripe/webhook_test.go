package stripe

import (
	"context"
	"errors"
	"testing"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockTransactionService embeds the interface so only the two methods the
// webhook actually calls need implementing.
type MockTransactionService struct {
	mock.Mock
	transactions.IService
}

func (m *MockTransactionService) Complete(
	ctx context.Context,
	id uuid.UUID,
	from repo.TransactionStatus,
) (repo.Transaction, error) {
	args := m.Called(ctx, id, from)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func (m *MockTransactionService) Transition(
	ctx context.Context,
	id uuid.UUID,
	from, to repo.TransactionStatus,
) (repo.Transaction, error) {
	args := m.Called(ctx, id, from, to)
	return args.Get(0).(repo.Transaction), args.Error(1)
}

func TestHandlePaymentSucceeded_ClaimsThenCompletes(t *testing.T) {
	svc := &MockTransactionService{}
	id := uuid.New()

	svc.On("Transition", mock.Anything, id,
		repo.TransactionStatusAwaitingPayment, repo.TransactionStatusCrediting).
		Return(repo.Transaction{}, nil)
	svc.On("Complete", mock.Anything, id, repo.TransactionStatusCrediting).
		Return(repo.Transaction{}, nil)

	err := NewWebhookService(svc).HandlePaymentSucceeded(
		context.Background(),
		HandlePaymentSucceededRequest{TransactionID: id},
	)

	assert.NoError(t, err)
	svc.AssertExpectations(t)
}

// A redelivered event loses the claim. That is not an error -- returning nil
// gives Stripe a 2xx so it stops retrying, and nothing is credited twice.
func TestHandlePaymentSucceeded_RedeliveryIsSuccessAndDoesNotComplete(t *testing.T) {
	svc := &MockTransactionService{}
	id := uuid.New()

	svc.On("Transition", mock.Anything, id,
		repo.TransactionStatusAwaitingPayment, repo.TransactionStatusCrediting).
		Return(repo.Transaction{}, transactions.ErrAlreadyProcessed)

	err := NewWebhookService(svc).HandlePaymentSucceeded(
		context.Background(),
		HandlePaymentSucceededRequest{TransactionID: id},
	)

	assert.NoError(t, err)
	svc.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything)
}

// The claim can succeed while the credit loses to the sweeper. Still a 2xx.
func TestHandlePaymentSucceeded_CompleteAlreadyProcessedIsSuccess(t *testing.T) {
	svc := &MockTransactionService{}
	id := uuid.New()

	svc.On("Transition", mock.Anything, id, mock.Anything, mock.Anything).
		Return(repo.Transaction{}, nil)
	svc.On("Complete", mock.Anything, id, repo.TransactionStatusCrediting).
		Return(repo.Transaction{}, transactions.ErrAlreadyProcessed)

	err := NewWebhookService(svc).HandlePaymentSucceeded(
		context.Background(),
		HandlePaymentSucceededRequest{TransactionID: id},
	)

	assert.NoError(t, err)
}

// A genuine failure must propagate so the handler answers 500 and Stripe retries.
func TestHandlePaymentSucceeded_RealErrorPropagates(t *testing.T) {
	svc := &MockTransactionService{}
	id := uuid.New()
	dbErr := errors.New("connection refused")

	svc.On("Transition", mock.Anything, id, mock.Anything, mock.Anything).
		Return(repo.Transaction{}, dbErr)

	err := NewWebhookService(svc).HandlePaymentSucceeded(
		context.Background(),
		HandlePaymentSucceededRequest{TransactionID: id},
	)

	assert.ErrorIs(t, err, dbErr)
}

func TestHandlePaymentFailedAndCanceled_MarkFailedWithoutTouchingBalance(t *testing.T) {
	tests := []struct {
		name string
		call func(IService, context.Context, uuid.UUID) error
	}{
		{
			name: "payment_failed",
			call: func(s IService, ctx context.Context, id uuid.UUID) error {
				return s.HandlePaymentFailed(ctx, HandlePaymentFailedRequest{TransactionID: id})
			},
		},
		{
			name: "canceled",
			call: func(s IService, ctx context.Context, id uuid.UUID) error {
				return s.HandlePaymentCanceled(ctx, HandlePaymentCanceledRequest{TransactionID: id})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &MockTransactionService{}
			id := uuid.New()

			svc.On("Transition", mock.Anything, id,
				repo.TransactionStatusAwaitingPayment, repo.TransactionStatusFailed).
				Return(repo.Transaction{}, nil)

			err := tc.call(NewWebhookService(svc), context.Background(), id)

			assert.NoError(t, err)
			svc.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

// A row that already moved on was somebody else's correct work, not a failure.
func TestHandlePaymentFailed_AlreadyMovedOnIsSuccess(t *testing.T) {
	svc := &MockTransactionService{}
	id := uuid.New()

	svc.On("Transition", mock.Anything, id, mock.Anything, mock.Anything).
		Return(repo.Transaction{}, transactions.ErrAlreadyProcessed)

	err := NewWebhookService(svc).HandlePaymentFailed(
		context.Background(),
		HandlePaymentFailedRequest{TransactionID: id},
	)

	assert.NoError(t, err)
}
