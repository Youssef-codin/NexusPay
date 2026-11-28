package users

import (
	"time"

	"github.com/google/uuid"
)

// SystemStripeID is the Stripe clearing account seeded by 00001_init.sql.
// Top-ups are ordinary transactions sent by this user, which is why every
// transaction row nets to zero and SUM(balance) is always 0.
var SystemStripeID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

type FindUserRequest struct {
	FullName string `json:"full_name" validate:"required,min=2,max=100"`
}

type FindUserResponse struct {
	Users []UserType `json:"users"`
}

type UserType struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"full_name"`
}

type GetMeResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
}

// BalanceRequest is the input to Debit/Credit. Both are called from inside the
// caller's transaction, so they never open one of their own.
type BalanceRequest struct {
	UserID uuid.UUID `json:"user_id"`
	Amount int64     `json:"amount_in_piastres"`
}

type BalanceResponse struct {
	UserID  uuid.UUID `json:"user_id"`
	Balance int64     `json:"balance"`
}
