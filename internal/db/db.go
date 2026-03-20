package db

import (
	"context"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ctxKeyTx struct{}

type TxManager interface {
	StartTx(ctx context.Context) (context.Context, pgx.Tx, error)
}

type DB struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *DB {
	return &DB{pool: pool}
}

func (db *DB) StartTx(ctx context.Context) (context.Context, pgx.Tx, error) {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{})
	txCtx := NewTxContext(ctx, tx)
	return txCtx, tx, err
}

func (db *DB) GetDBTX(ctx context.Context) *repo.Queries {
	if tx, ok := ctx.Value(ctxKeyTx{}).(pgx.Tx); ok {
		return repo.New(tx)
	}
	return repo.New(db.pool)
}

func NewTxContext(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, ctxKeyTx{}, tx)
}
