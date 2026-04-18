package wallet

import (
	"time"

	"github.com/google/uuid"
)

type GetWalletRequest struct {
	ID uuid.UUID `json:"id" validate:"required,uuid"`
}

type CreateWalletRequest struct {
	UserID  uuid.UUID `json:"user_id"`
	Balance int64     `json:"balance_in_piastres"`
}

type TopUpRequest struct {
	Amount      int64  `json:"amount_in_piastres" validate:"min=1000"`
	Description string `json:"description"`
}

type DeductRequest struct {
	WalletID uuid.UUID `json:"wallet_id" validate:"required,uuid"`
	Amount   int64     `json:"amount_in_piastres" validate:"min=1"`
}

type AddToWalletRequest struct {
	WalletID uuid.UUID `json:"wallet_id"          validate:"required,uuid"`
	Amount   int64     `json:"amount_in_piastres" validate:"min=1000"`
}

type AddToWalletResponse struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GetWalletResponse struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	Balance   int64      `json:"balance"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}

type CreateWalletResponse struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
}

type TopUpResponse struct {
	ID                uuid.UUID `json:"id"`
	UserID            uuid.UUID `json:"user_id"`
	Status            string    `json:"status"`
	UpdatedAt         time.Time `json:"updated_at"`
	ProviderPaymentID string    `json:"provider_payment_id,omitempty"`
	ClientSecret      string    `json:"client_secret,omitempty"`
}

type DeductResponse struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}
