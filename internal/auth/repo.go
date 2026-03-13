package auth

import (
	"context"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type authRepository interface {
	GetUserByEmail(ctx context.Context, email string) (repo.User, error)
	CreateUser(ctx context.Context, arg repo.CreateUserParams) (repo.CreateUserRow, error)
	GetUserByRefreshToken(ctx context.Context, token pgtype.Text) (repo.User, error)
	UpdateRefreshToken(ctx context.Context, arg repo.UpdateRefreshTokenParams) error
	GetUserById(ctx context.Context, id pgtype.UUID) (repo.User, error)
	RevokeRefreshToken(ctx context.Context, id pgtype.UUID) error
}
