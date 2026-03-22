package stripe

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Youssef-codin/NexusPay/internal/payment"
	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/paymentintent"
)

var (
	ErrPaymentFailed    = errors.New("payment failed")
	ErrRefundFailed     = errors.New("refund failed")
	ErrPaymentCancelled = errors.New("payment cancelled")
	ErrPaymentTimeout   = errors.New("timeout reached")
	ErrUnimplemented    = errors.New("unimplemented for now")
)

type Service struct{}

func NewService(apiKey string) payment.IService {
	stripe.Key = apiKey
	return &Service{}
}

func (svc *Service) ProcessPayment(
	ctx context.Context,
	req payment.ProcessPaymentRequest,
) (payment.ProcessPaymentResponse, error) {
	params := &stripe.PaymentIntentParams{
		Params: stripe.Params{
			IdempotencyKey: stripe.String(req.TransactionID),
		},
		Amount:      new(req.Amount),
		Currency:    new("egp"),
		Description: new(req.Description),
		Metadata: map[string]string{
			"transaction_id": req.TransactionID,
		},
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: new(true),
		},
	}

	var pi *stripe.PaymentIntent
	var err error

	for attempts := range 3 {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		params.Context = callCtx
		pi, err = paymentintent.New(params)
		cancel()

		if err == nil {
			break
		}

		if callCtx.Err() == context.DeadlineExceeded {
			slog.Warn(
				"stripe timed out, retrying...",
				"attempt",
				attempts+1,
				"transaction_id",
				req.TransactionID,
			)
			continue // safe to retry because of idempotency key
		}

		// non-timeout error, don't retry
		slog.Error("stripe payment failed", "error", err)
		return payment.ProcessPaymentResponse{}, ErrPaymentFailed
	}

	if err != nil {
		return payment.ProcessPaymentResponse{}, ErrPaymentTimeout
	}

	return payment.ProcessPaymentResponse{
		ProviderPaymentID: pi.ID,
		Status:            FromStripePaymentIntentStatus(pi.Status),
		ClientSecret:      pi.ClientSecret,
	}, nil
}

// TODO: future feature hopefully
func (svc *Service) Refund(
	ctx context.Context,
	req payment.RefundRequest,
) (payment.RefundResponse, error) {
	// params := &stripe.RefundParams{
	// 	PaymentIntent: new(req.ProviderPaymentID),
	// 	Amount:        req.Amount,
	// }
	//
	// var re *stripe.Refund
	// for attempts := range 3 {
	// 	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	// 	params.Context = callCtx
	// 	re, err := refund.New(params)
	// 	cancel()
	//
	// 	if err == nil {
	// 		break
	// 	}
	//
	// 	if callCtx.Err() == context.DeadlineExceeded {
	// 		slog.Warn(
	// 			"stripe timed out, retrying...",
	// 			"attempt",
	// 			attempts+1,
	// 			"transaction_id",
	// 			req.TransactionID,
	// 		)
	// 		continue
	// 		return payment.RefundResponse{}, ErrRefundFailed
	// 	}
	//
	// 	slog.Error("stripe payment failed", "error", err)
	// 	return payment.RefundResponse{}, ErrPaymentFailed
	// }
	//
	// return payment.RefundResponse{
	// 	RefundID: re.ID,
	// 	Status:   FromStripeRefundStatus(re.Status),
	// 	Amount:   re.Amount,
	// }, nil

	return payment.RefundResponse{}, ErrPaymentFailed
}

// TODO: future feature hopefully
func (svc *Service) CancelPayment(
	ctx context.Context,
	req payment.CancelPaymentRequest,
) (payment.CancelPaymentResponse, error) {
	// pi, err := paymentintent.Cancel(req.ProviderPaymentID, nil)
	// if err != nil {
	// 	slog.Error("stripe payment failed", "error", err)
	// 	return payment.CancelPaymentResponse{}, ErrPaymentCancelled
	// }
	//
	// return payment.CancelPaymentResponse{
	// 	ProviderPaymentID: pi.ID,
	// 	Status:            FromStripePaymentIntentStatus(pi.Status),
	// }, nil

	return payment.CancelPaymentResponse{}, ErrUnimplemented
}

func FromStripePaymentIntentStatus(s stripe.PaymentIntentStatus) payment.PaymentStatus {
	switch s {
	case stripe.PaymentIntentStatusSucceeded:
		return payment.PaymentStatusCompleted
	case stripe.PaymentIntentStatusCanceled,
		stripe.PaymentIntentStatusRequiresPaymentMethod:
		return payment.PaymentStatusFailed
	case stripe.PaymentIntentStatusProcessing,
		stripe.PaymentIntentStatusRequiresAction,
		stripe.PaymentIntentStatusRequiresCapture,
		stripe.PaymentIntentStatusRequiresConfirmation:
		return payment.PaymentStatusPending
	default:
		return payment.PaymentStatusPending
	}
}

func FromStripeRefundStatus(s stripe.RefundStatus) payment.PaymentStatus {
	switch s {
	case stripe.RefundStatusSucceeded:
		return payment.PaymentStatusCompleted
	case stripe.RefundStatusFailed,
		stripe.RefundStatusCanceled:
		return payment.PaymentStatusFailed
	case stripe.RefundStatusPending,
		stripe.RefundStatusRequiresAction:
		return payment.PaymentStatusPending
	default:
		return payment.PaymentStatusPending
	}
}
