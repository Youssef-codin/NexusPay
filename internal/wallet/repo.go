package wallet

import (
	"context"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type walletRepo interface {
	CreateWallet(ctx context.Context, arg repo.CreateWalletParams) (repo.CreateWalletRow, error)
	GetWalletById(ctx context.Context, id pgtype.UUID) (repo.Wallet, error)
	GetWalletByUserId(ctx context.Context, userID pgtype.UUID) (repo.Wallet, error)
	DeductFromBalance(ctx context.Context, arg repo.DeductFromBalanceParams) (repo.Wallet, error)
	AddToBalance(ctx context.Context, arg repo.AddToBalanceParams) (repo.Wallet, error)
}

type WalletRepo struct {
	db *db.DB
}

func NewRepo(database *db.DB) walletRepo {
	return &WalletRepo{db: database}
}

func (r *WalletRepo) CreateWallet(
	ctx context.Context,
	arg repo.CreateWalletParams,
) (repo.CreateWalletRow, error) {
	return r.db.GetDBTX(ctx).CreateWallet(ctx, arg)
}

func (r *WalletRepo) GetWalletById(ctx context.Context, id pgtype.UUID) (repo.Wallet, error) {
	return r.db.GetDBTX(ctx).GetWalletById(ctx, id)
}

func (r *WalletRepo) GetWalletByUserId(
	ctx context.Context,
	userID pgtype.UUID,
) (repo.Wallet, error) {
	return r.db.GetDBTX(ctx).GetWalletByUserId(ctx, userID)
}

func (r *WalletRepo) DeductFromBalance(
	ctx context.Context,
	arg repo.DeductFromBalanceParams,
) (repo.Wallet, error) {
	return r.db.GetDBTX(ctx).DeductFromBalance(ctx, arg)
}

func (r *WalletRepo) AddToBalance(
	ctx context.Context,
	arg repo.AddToBalanceParams,
) (repo.Wallet, error) {
	return r.db.GetDBTX(ctx).AddToBalance(ctx, arg)
}
