package payment

type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "pending"
	PaymentStatusCompleted PaymentStatus = "completed"
	PaymentStatusFailed    PaymentStatus = "failed"
)

type ProcessPaymentRequest struct {
	Amount        int64
	TransactionID string
	Description   string
}

type ProcessPaymentResponse struct {
	ProviderPaymentID string
	Status            PaymentStatus
	ClientSecret      string
}

type RefundRequest struct {
	ProviderPaymentID string
	Amount            *int64
}

type RefundResponse struct {
	RefundID string
	Status   PaymentStatus
	Amount   int64
}

type CancelPaymentRequest struct {
	ProviderPaymentID string
}

type CancelPaymentResponse struct {
	ProviderPaymentID string
	Status            PaymentStatus
}
