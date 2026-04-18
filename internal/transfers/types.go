package transfers

import (
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/google/uuid"
)

type CreateTransferRequest struct {
	ToWalletID  uuid.UUID  `json:"to_wallet_id"`
	Amount      int64      `json:"amount_in_piastres" validate:"min=1000"`
	Note        string     `json:"note"`
	ScheduledAt *time.Time `json:"scheduled_at"`
}

type GetTransferByIDRequest struct {
	ID uuid.UUID `json:"id" validate:"uuid"`
}

type UpdateTransferStatusRequest struct {
	ID     uuid.UUID           `json:"id"     validate:"uuid"`
	Status repo.TransferStatus `json:"status" validate:"required,transfer_status"`
}

type UpdateTransferWithTxnIDRequest struct {
	TransactionID uuid.UUID           `json:"transaction_id" validate:"uuid"`
	Status        repo.TransferStatus `json:"status"         validate:"required,transfer_status"`
}

type CreateTransferResponse struct {
	ID           uuid.UUID           `json:"id"`
	FromWalletID uuid.UUID           `json:"from_wallet_id"`
	ToWalletID   uuid.UUID           `json:"to_wallet_id"`
	Amount       int64               `json:"amount_in_piastres"`
	Status       repo.TransferStatus `json:"status"`
	Note         string              `json:"note"`
	CreatedAt    time.Time           `json:"created_at"`
}

type TransferResponse struct {
	ID        uuid.UUID           `json:"id"`
	Amount    int64               `json:"amount_in_piastres"`
	Status    repo.TransferStatus `json:"status"`
	Note      string              `json:"note"`
	CreatedAt time.Time           `json:"created_at"`
}

type GetTransfersByIDResponse struct {
	FromWalletID uuid.UUID          `json:"from_wallet_id"`
	Transfers    []TransferResponse `json:"transfers"`
}

type ProcessTransferRequest struct {
	ID uuid.UUID `json:"id" validate:"uuid"`
}

type ProcessTransferResponse struct {
	ID           uuid.UUID           `json:"id"`
	FromWalletID uuid.UUID           `json:"from_wallet_id"`
	ToWalletID   uuid.UUID           `json:"to_wallet_id"`
	Amount       int64               `json:"amount_in_piastres"`
	Status       repo.TransferStatus `json:"status"`
	Note         string              `json:"note"`
	CreatedAt    time.Time           `json:"created_at"`
}

type GetTransferByIDResponse struct {
	Transfer TransferResponse `json:"transfer"`
}

type ListScheduledTransfersRequest struct {
	UserID uuid.UUID `json:"user_id" validate:"uuid"`
}

type ScheduledTransferResponse struct {
	ID          uuid.UUID `json:"id"`
	TransferID  uuid.UUID `json:"transfer_id"`
	ScheduledAt time.Time `json:"scheduled_at"`
	ExecutedAt  time.Time `json:"executed_at"`
	CreatedAt   time.Time `json:"created_at"`
}

type ListScheduledTransfersResponse struct {
	ScheduledTransfers []ScheduledTransferResponse `json:"scheduled_transfers"`
}

type CancelScheduledTransfersRequest struct {
	TransferID uuid.UUID `json:"transfer_id" validate:"uuid"`
}

type CancelScheduledTransfersResponse struct {
	CancelledID uuid.UUID `json:"cancelled_id"`
}
