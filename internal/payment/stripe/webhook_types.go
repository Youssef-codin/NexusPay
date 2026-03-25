package stripe

type HandlePaymentSucceededRequest struct {
	TransactionID string
}

type HandlePaymentFailedRequest struct {
	TransactionID string
}

type HandlePaymentCanceledRequest struct {
	TransactionID string
}
