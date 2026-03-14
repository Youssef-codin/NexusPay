package wallet

import (
	"context"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type walletRepository interface {
	CreateWallet(ctx context.Context, arg repo.CreateWalletParams) (repo.CreateWalletRow, error)
	GetWalletById(ctx context.Context, id pgtype.UUID) (repo.Wallet, error)
	GetWalletByUserId(ctx context.Context, userID pgtype.UUID) (repo.Wallet, error)
	DeductFromBalance(ctx context.Context, arg repo.DeductFromBalanceParams) (repo.Wallet, error)
	AddToBalance(ctx context.Context, arg repo.AddToBalanceParams) (repo.Wallet, error)
}
