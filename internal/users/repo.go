package users

import (
	"context"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
)

type userRepo interface {
	GetUserByName(ctx context.Context, fullName string) ([]repo.User, error)
}

type UserRepo struct {
	db *db.DB
}

func NewUserRepo(database *db.DB) userRepo {
	return &UserRepo{db: database}
}

func (r *UserRepo) GetUserByName(ctx context.Context, fullName string) ([]repo.User, error) {
	return r.db.GetDBTX(ctx).GetUserByName(ctx, fullName)
}
