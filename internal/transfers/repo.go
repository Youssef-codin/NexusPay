package transfers

import (
	"context"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type transfersRepo interface {
	CreateTransfer(ctx context.Context, arg repo.CreateTransferParams) (repo.Transfer, error)
	UpdateTransferStatus(
		ctx context.Context,
		arg repo.UpdateTransferStatusParams,
	) (repo.Transfer, error)
	UpdateTransferWithTransactionId(
		ctx context.Context,
		arg repo.UpdateTransferWithTransactionIdParams,
	) (repo.Transfer, error)
	GetTransferById(ctx context.Context, id pgtype.UUID) (repo.Transfer, error)
	GetTransfersByWalletId(ctx context.Context, toWalletID pgtype.UUID) ([]repo.Transfer, error)
}

type TransfersRepo struct {
	db *db.DB
}

func NewRepo(database *db.DB) transfersRepo {
	return &TransfersRepo{db: database}
}

func (r *TransfersRepo) CreateTransfer(
	ctx context.Context,
	arg repo.CreateTransferParams,
) (repo.Transfer, error) {
	return r.db.GetDBTX(ctx).CreateTransfer(ctx, arg)
}

func (r *TransfersRepo) UpdateTransferStatus(
	ctx context.Context,
	arg repo.UpdateTransferStatusParams,
) (repo.Transfer, error) {

	return r.db.GetDBTX(ctx).UpdateTransferStatus(ctx, arg)
}

func (r *TransfersRepo) UpdateTransferWithTransactionId(
	ctx context.Context,
	arg repo.UpdateTransferWithTransactionIdParams,
) (repo.Transfer, error) {
	return r.db.GetDBTX(ctx).UpdateTransferWithTransactionId(ctx, arg)
}

func (r *TransfersRepo) GetTransferById(
	ctx context.Context,
	id pgtype.UUID,
) (repo.Transfer, error) {
	return r.db.GetDBTX(ctx).GetTransferById(ctx, id)
}

func (r *TransfersRepo) GetTransfersByWalletId(
	ctx context.Context,
	walletID pgtype.UUID,
) ([]repo.Transfer, error) {
	return r.db.GetDBTX(ctx).GetTransfersByWalletId(ctx, walletID)
}
