package transactions

import (
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/google/uuid"
)

// MinAmount is the floor for both transfers and top-ups, in piastres.
const MinAmount = 1000

type CreateTransactionRequest struct {
	ReceiverID  uuid.UUID            `json:"receiver_id"        validate:"required"`
	Amount      int64                `json:"amount_in_piastres" validate:"min=1000"`
	Note        string               `json:"note"`
	Category    repo.ExpenseCategory `json:"category"           validate:"omitempty,expense_category"`
	ScheduledAt *time.Time           `json:"scheduled_at"       validate:"omitempty,future"`
}

type SetCategoryRequest struct {
	ID       uuid.UUID            `json:"id"`
	Category repo.ExpenseCategory `json:"category" validate:"required,expense_category"`
}

type TopUpRequest struct {
	Amount int64 `json:"amount_in_piastres" validate:"min=1000"`
}

type UserMini struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"full_name"`
}

type TransactionResponse struct {
	ID       uuid.UUID `json:"id"`
	Sender   UserMini  `json:"sender"`
	Receiver UserMini  `json:"receiver"`
	Amount   int64     `json:"amount_in_piastres"`
	// Direction is "debit" when the caller is the sender, "credit" otherwise.
	Direction        string                 `json:"direction,omitempty"`
	Status           repo.TransactionStatus `json:"status"`
	Note             string                 `json:"note,omitempty"`
	SenderCategory   repo.ExpenseCategory   `json:"sender_category,omitempty"`
	ReceiverCategory repo.ExpenseCategory   `json:"receiver_category,omitempty"`
	ScheduledAt      time.Time              `json:"scheduled_at"`
	CreatedAt        time.Time              `json:"created_at"`
}

type GetTransactionResponse struct {
	Transaction TransactionResponse `json:"transaction"`
}

type ListTransactionsResponse struct {
	Transactions []TransactionResponse `json:"transactions"`
}

type CancelTransactionResponse struct {
	CancelledID uuid.UUID `json:"cancelled_id"`
}

// TopUpResponse carries the Stripe PaymentIntent back to the client. No money
// has moved at this point -- the balance only changes when the webhook lands.
type TopUpResponse struct {
	TransactionID     uuid.UUID              `json:"transaction_id"`
	Amount            int64                  `json:"amount_in_piastres"`
	Status            repo.TransactionStatus `json:"status"`
	ProviderPaymentID string                 `json:"provider_payment_id,omitempty"`
	ClientSecret      string                 `json:"client_secret,omitempty"`
}
