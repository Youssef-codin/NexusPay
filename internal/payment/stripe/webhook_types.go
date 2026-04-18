package stripe

import "github.com/google/uuid"

type HandlePaymentSucceededRequest struct {
	TransactionID uuid.UUID
}

type HandlePaymentFailedRequest struct {
	TransactionID uuid.UUID
}

type HandlePaymentCanceledRequest struct {
	TransactionID uuid.UUID
}
