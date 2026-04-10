package transfers

import (
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
)

type CreateTransferRequest struct {
	FromWalletID string `json:"from_wallet_id"     validate:"required,uuid"`
	ToWalletID   string `json:"to_wallet_id"       validate:"required,uuid"`
	Amount       int64  `json:"amount_in_piastres" validate:"min=1000"`
	Note         string `json:"note"`
}

type GetTransferByIDRequest struct {
	FromWalletID string `json:"from_wallet_id" validate:"required,uuid"`
}

type UpdateTransferStatusRequest struct {
	ID     string              `json:"id"     validate:"required,uuid"`
	Status repo.TransferStatus `json:"status" validate:"required,transfer_status"`
}

type UpdateTransferWithTxnIDRequest struct {
	TransactionID string              `json:"transaction_id" validate:"required,uuid"`
	Status        repo.TransferStatus `json:"status"         validate:"required,transfer_status"`
}

type CreateTransferResponse struct {
	ID           string              `json:"id"`
	FromWalletID string              `json:"from_wallet_id"`
	ToWalletID   string              `json:"to_wallet_id"`
	Amount       int64               `json:"amount_in_piastres"`
	Status       repo.TransferStatus `json:"status"`
	Note         string              `json:"note"`
	CreatedAt    time.Time           `json:"created_at"`
}

type TransferResponse struct {
	ID        string              `json:"id"`
	Amount    int64               `json:"amount_in_piastres"`
	Status    repo.TransferStatus `json:"status"`
	Note      string              `json:"note"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
	DeletedAt *time.Time          `json:"deleted_at"`
}

type GetTransfersByIDResponse struct {
	FromWalletID string             `json:"from_wallet_id"`
	Transfers    []TransferResponse `json:"transfers"`
}
