package wallet

import "time"

type GetWalletRequest struct {
	ID string `json:"id" validate:"required,uuid"`
}

type CreateWalletRequest struct{}

type AddToBalanceRequest struct {
	Amount int64 `json:"amount" validate:"min=0"`
}

type DeductFromBalanceRequest struct {
	Amount int64 `json:"amount" validate:"min=0"`
}

type GetWalletResponse struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Balance   int64      `json:"balance"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}

type CreateWalletResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
}

type UpdateBalanceResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updated_at"`
}

// type baseStruct struct {
// 	ID        string     `json:"id"         validate:"required,uuid"`
// 	UserID    string     `json:"user_id"    validate:"required,uuid"`
// 	Balance   int64      `json:"balance"    validate:"min=0"`
// 	CreatedAt time.Time  `json:"created_at" validate:"required"`
// 	UpdatedAt time.Time  `json:"updated_at" validate:"required"`
// 	DeletedAt *time.Time `json:"deleted_at" validate:"omitempty"`
// }
