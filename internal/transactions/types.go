package transactions

import (
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/google/uuid"
)

type GetByIdRequest struct {
	ID uuid.UUID `json:"id"`
}

type CreateTransactionRequest struct {
	WalletID    uuid.UUID            `json:"wallet_id"`
	Amount      int64                `json:"amount_in_piastres" validate:"min=1000"`
	Type        repo.TransactionType `json:"transaction_type"   validate:"transaction_type"`
	Description string               `json:"description"`
}

type GetTransactionResponse struct {
	ID          uuid.UUID              `json:"id"`
	WalletID    uuid.UUID              `json:"wallet_id"`
	Amount      int64                  `json:"amount_in_piastres"`
	Type        repo.TransactionType   `json:"transaction_type"`
	Status      repo.TransactionStatus `json:"transaction_status"`
	TransferID  *uuid.UUID             `json:"transfer_id"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	DeletedAt   *time.Time             `json:"deleted_at"`
	Description string                 `json:"description"`
}

type CreateTransactionResponse struct {
	ID          uuid.UUID              `json:"id"`
	WalletID    uuid.UUID              `json:"wallet_id"`
	Amount      int64                  `json:"amount_in_piastres"`
	Type        repo.TransactionType   `json:"transaction_type"`
	Status      repo.TransactionStatus `json:"transaction_status"`
	TransferID  *uuid.UUID             `json:"transfer_id"`
	CreatedAt   time.Time              `json:"created_at"`
	Description string                 `json:"description"`
}

type UpdateTransactionRequest struct {
	ID     uuid.UUID              `json:"id"`
	Status repo.TransactionStatus `json:"transaction_status" validate:"transaction_status"`
}
