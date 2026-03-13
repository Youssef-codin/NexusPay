package users

import (
	"context"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
)

type userRepository interface {
	GetUserByName(ctx context.Context, fullName string) ([]repo.User, error)
}
