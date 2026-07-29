package payment

import (
	"context"
)

type IService interface {
	ProcessPayment(
		ctx context.Context,
		req ProcessPaymentRequest,
	) (ProcessPaymentResponse, error)
}
