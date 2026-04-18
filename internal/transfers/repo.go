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
	CreateScheduledTransfer(ctx context.Context, arg repo.CreateScheduledTransferParams) (repo.ScheduledTransfer, error)
	GetScheduledTransferById(ctx context.Context, id pgtype.UUID) (repo.ScheduledTransfer, error)
	GetScheduledTransferByTransferId(ctx context.Context, transferID pgtype.UUID) (repo.ScheduledTransfer, error)
	GetPendingScheduledTransfers(ctx context.Context, at pgtype.Timestamptz) ([]repo.ScheduledTransfer, error)
	MarkScheduledTransferExecuted(ctx context.Context, id pgtype.UUID) (repo.ScheduledTransfer, error)
	CancelScheduledTransfer(ctx context.Context, id pgtype.UUID) (repo.ScheduledTransfer, error)
	GetScheduledTransfersByUserId(ctx context.Context, userID pgtype.UUID) ([]repo.ScheduledTransfer, error)
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

func (r *TransfersRepo) CreateScheduledTransfer(
	ctx context.Context,
	arg repo.CreateScheduledTransferParams,
) (repo.ScheduledTransfer, error) {
	return r.db.GetDBTX(ctx).CreateScheduledTransfer(ctx, arg)
}

func (r *TransfersRepo) GetScheduledTransferById(
	ctx context.Context,
	id pgtype.UUID,
) (repo.ScheduledTransfer, error) {
	return r.db.GetDBTX(ctx).GetScheduledTransferById(ctx, id)
}

func (r *TransfersRepo) GetScheduledTransferByTransferId(
	ctx context.Context,
	transferID pgtype.UUID,
) (repo.ScheduledTransfer, error) {
	return r.db.GetDBTX(ctx).GetScheduledTransferByTransferId(ctx, transferID)
}

func (r *TransfersRepo) GetPendingScheduledTransfers(
	ctx context.Context,
	at pgtype.Timestamptz,
) ([]repo.ScheduledTransfer, error) {
	return r.db.GetDBTX(ctx).GetPendingScheduledTransfers(ctx, at)
}

func (r *TransfersRepo) MarkScheduledTransferExecuted(
	ctx context.Context,
	id pgtype.UUID,
) (repo.ScheduledTransfer, error) {
	return r.db.GetDBTX(ctx).MarkScheduledTransferExecuted(ctx, id)
}

func (r *TransfersRepo) CancelScheduledTransfer(
	ctx context.Context,
	id pgtype.UUID,
) (repo.ScheduledTransfer, error) {
	return r.db.GetDBTX(ctx).CancelScheduledTransfer(ctx, id)
}

func (r *TransfersRepo) GetScheduledTransfersByUserId(
	ctx context.Context,
	userID pgtype.UUID,
) ([]repo.ScheduledTransfer, error) {
	return r.db.GetDBTX(ctx).GetScheduledTransfersByUserId(ctx, userID)
}
