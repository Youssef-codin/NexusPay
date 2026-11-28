package users

import (
	"context"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type userRepo interface {
	GetUserByName(ctx context.Context, fullName string) ([]repo.User, error)
	GetUserById(ctx context.Context, id pgtype.UUID) (repo.User, error)
	GetBalance(ctx context.Context, id pgtype.UUID) (int64, error)
	DebitUser(ctx context.Context, arg repo.DebitUserParams) (repo.User, error)
	CreditUser(ctx context.Context, arg repo.CreditUserParams) (repo.User, error)
}

type UserRepo struct {
	db *db.DB
}

func NewRepo(database *db.DB) userRepo {
	return &UserRepo{db: database}
}

func (r *UserRepo) GetUserByName(ctx context.Context, fullName string) ([]repo.User, error) {
	return r.db.GetDBTX(ctx).GetUserByName(ctx, fullName)
}

func (r *UserRepo) GetUserById(ctx context.Context, id pgtype.UUID) (repo.User, error) {
	return r.db.GetDBTX(ctx).GetUserById(ctx, id)
}

func (r *UserRepo) GetBalance(ctx context.Context, id pgtype.UUID) (int64, error) {
	return r.db.GetDBTX(ctx).GetBalance(ctx, id)
}

func (r *UserRepo) DebitUser(ctx context.Context, arg repo.DebitUserParams) (repo.User, error) {
	return r.db.GetDBTX(ctx).DebitUser(ctx, arg)
}

func (r *UserRepo) CreditUser(ctx context.Context, arg repo.CreditUserParams) (repo.User, error) {
	return r.db.GetDBTX(ctx).CreditUser(ctx, arg)
}
