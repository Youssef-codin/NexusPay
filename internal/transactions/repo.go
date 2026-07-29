package transactions

import (
	"context"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type transactionRepo interface {
	CreateTransaction(
		ctx context.Context,
		arg repo.CreateTransactionParams,
	) (repo.Transaction, error)
	GetTransactionById(ctx context.Context, id pgtype.UUID) (repo.Transaction, error)
	GetTransactionByIdWithUsers(
		ctx context.Context,
		id pgtype.UUID,
	) (repo.GetTransactionByIdWithUsersRow, error)
	GetTransactionsByUserId(
		ctx context.Context,
		userID pgtype.UUID,
	) ([]repo.GetTransactionsByUserIdRow, error)
	GuardedSetStatus(
		ctx context.Context,
		arg repo.GuardedSetStatusParams,
	) (repo.Transaction, error)
	ClaimDueTransactions(ctx context.Context, limit int32) ([]repo.Transaction, error)
	ClaimStuckCrediting(ctx context.Context, limit int32) ([]repo.Transaction, error)
	CancelScheduledTransaction(
		ctx context.Context,
		arg repo.CancelScheduledTransactionParams,
	) (repo.Transaction, error)
	SetSenderCategory(
		ctx context.Context,
		arg repo.SetSenderCategoryParams,
	) (repo.Transaction, error)
	SetReceiverCategory(
		ctx context.Context,
		arg repo.SetReceiverCategoryParams,
	) (repo.Transaction, error)
}

type TransactionRepo struct {
	db *db.DB
}

func NewRepo(database *db.DB) transactionRepo {
	return &TransactionRepo{db: database}
}

func (r *TransactionRepo) CreateTransaction(
	ctx context.Context,
	arg repo.CreateTransactionParams,
) (repo.Transaction, error) {
	return r.db.GetDBTX(ctx).CreateTransaction(ctx, arg)
}

func (r *TransactionRepo) GetTransactionById(
	ctx context.Context,
	id pgtype.UUID,
) (repo.Transaction, error) {
	return r.db.GetDBTX(ctx).GetTransactionById(ctx, id)
}

func (r *TransactionRepo) GetTransactionByIdWithUsers(
	ctx context.Context,
	id pgtype.UUID,
) (repo.GetTransactionByIdWithUsersRow, error) {
	return r.db.GetDBTX(ctx).GetTransactionByIdWithUsers(ctx, id)
}

func (r *TransactionRepo) GetTransactionsByUserId(
	ctx context.Context,
	userID pgtype.UUID,
) ([]repo.GetTransactionsByUserIdRow, error) {
	return r.db.GetDBTX(ctx).GetTransactionsByUserId(ctx, userID)
}

func (r *TransactionRepo) GuardedSetStatus(
	ctx context.Context,
	arg repo.GuardedSetStatusParams,
) (repo.Transaction, error) {
	return r.db.GetDBTX(ctx).GuardedSetStatus(ctx, arg)
}

func (r *TransactionRepo) ClaimDueTransactions(
	ctx context.Context,
	limit int32,
) ([]repo.Transaction, error) {
	return r.db.GetDBTX(ctx).ClaimDueTransactions(ctx, limit)
}

func (r *TransactionRepo) ClaimStuckCrediting(
	ctx context.Context,
	limit int32,
) ([]repo.Transaction, error) {
	return r.db.GetDBTX(ctx).ClaimStuckCrediting(ctx, limit)
}

func (r *TransactionRepo) CancelScheduledTransaction(
	ctx context.Context,
	arg repo.CancelScheduledTransactionParams,
) (repo.Transaction, error) {
	return r.db.GetDBTX(ctx).CancelScheduledTransaction(ctx, arg)
}

func (r *TransactionRepo) SetSenderCategory(
	ctx context.Context,
	arg repo.SetSenderCategoryParams,
) (repo.Transaction, error) {
	return r.db.GetDBTX(ctx).SetSenderCategory(ctx, arg)
}

func (r *TransactionRepo) SetReceiverCategory(
	ctx context.Context,
	arg repo.SetReceiverCategoryParams,
) (repo.Transaction, error) {
	return r.db.GetDBTX(ctx).SetReceiverCategory(ctx, arg)
}
