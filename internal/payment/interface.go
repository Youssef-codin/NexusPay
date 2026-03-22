package payment

import (
	"context"
)

type IService interface {
	ProcessPayment(
		ctx context.Context,
		req ProcessPaymentRequest,
	) (ProcessPaymentResponse, error)

	Refund(ctx context.Context, req RefundRequest) (RefundResponse, error)
	CancelPayment(
		ctx context.Context,
		req CancelPaymentRequest,
	) (CancelPaymentResponse, error)
}
