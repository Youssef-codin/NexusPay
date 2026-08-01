package stripe

import (
	"context"
	"errors"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/google/uuid"
)

var ErrParseEvent = errors.New("failed to parse event")

type IService interface {
	HandlePaymentSucceeded(ctx context.Context, req HandlePaymentSucceededRequest) error
	HandlePaymentFailed(ctx context.Context, req HandlePaymentFailedRequest) error
	HandlePaymentCanceled(ctx context.Context, req HandlePaymentCanceledRequest) error
}

type WebhookService struct {
	transactionSvc transactions.IService
}

func NewWebhookService(transactionSvc transactions.IService) IService {
	return &WebhookService{
		transactionSvc: transactionSvc,
	}
}

// HandlePaymentSucceeded is two transactions on purpose. Tx1 claims
// awaiting_payment -> crediting and commits, so a row sitting in 'crediting'
// means the credit definitively did not commit and the sweeper can finish it.
// Tx2 is Complete(), which moves the money.
//
// A zero-row claim means another delivery of the same event already did this,
// so we return nil -- Stripe gets a 2xx and stops retrying.
func (svc *WebhookService) HandlePaymentSucceeded(
	ctx context.Context,
	req HandlePaymentSucceededRequest,
) error {
	_, err := svc.transactionSvc.Transition(
		ctx,
		req.TransactionID,
		repo.TransactionStatusAwaitingPayment,
		repo.TransactionStatusCrediting,
	)
	if errors.Is(err, transactions.ErrAlreadyProcessed) {
		return nil
	}
	if err != nil {
		return err
	}

	_, err = svc.transactionSvc.Complete(ctx, req.TransactionID, repo.TransactionStatusCrediting)
	if errors.Is(err, transactions.ErrAlreadyProcessed) {
		return nil
	}
	return err
}

func (svc *WebhookService) HandlePaymentFailed(
	ctx context.Context,
	req HandlePaymentFailedRequest,
) error {
	return svc.markFailed(ctx, req.TransactionID)
}

func (svc *WebhookService) HandlePaymentCanceled(
	ctx context.Context,
	req HandlePaymentCanceledRequest,
) error {
	return svc.markFailed(ctx, req.TransactionID)
}

// markFailed is a single guarded awaiting_payment -> failed and never touches a
// balance. If the row already moved on, that is somebody else's correct work.
func (svc *WebhookService) markFailed(ctx context.Context, id uuid.UUID) error {
	_, err := svc.transactionSvc.Transition(
		ctx,
		id,
		repo.TransactionStatusAwaitingPayment,
		repo.TransactionStatusFailed,
	)
	if errors.Is(err, transactions.ErrAlreadyProcessed) {
		return nil
	}
	return err
}
