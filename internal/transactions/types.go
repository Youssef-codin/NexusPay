package transactions

import (
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
)

type GetByIdRequest struct {
	ID string `json:"id"`
}

type CreateTransactionRequest struct {
	WalletID    string                 `json:"wallet_id"`
	Amount      int64                  `json:"amount_in_piastres" validate:"min=1000"`
	Type        repo.TransactionType   `json:"transaction_type"   validate:"transaction_type"`
	Status      repo.TransactionStatus `json:"transaction_status" validate:"transaction_status"`
	Description string                 `json:"description"`
}

// type idk repo.Transaction

type GetTransactionResponse struct {
	ID          string                 `json:"id"`
	WalletID    string                 `json:"wallet_id"`
	Amount      int64                  `json:"amount_in_piastres"`
	Type        repo.TransactionType   `json:"transaction_type"`
	Status      repo.TransactionStatus `json:"transaction_status"`
	TransferID  *string                `json:"transfer_id"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	DeletedAt   *time.Time             `json:"deleted_at"`
	Description string                 `json:"description"`
}

type CreateTransactionResponse struct {
	ID          string                 `json:"id"`
	WalletID    string                 `json:"wallet_id"`
	Amount      int64                  `json:"amount_in_piastres"`
	Type        repo.TransactionType   `json:"transaction_type"`
	Status      repo.TransactionStatus `json:"transaction_status"`
	TransferID  *string                `json:"transfer_id"`
	CreatedAt   time.Time              `json:"created_at"`
	Description string                 `json:"description"`
}

type UpdateTransactionRequest struct {
	ID     string                 `json:"id"`
	Status repo.TransactionStatus `json:"transaction_status" validate:"transaction_status"`
}
