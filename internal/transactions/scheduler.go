package transactions

import (
	"context"
	"errors"
	"log/slog"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// batchSize caps how many rows one tick claims. Postgres is the queue -- there
// is no Redis involved.
const batchSize = 100

// Scheduler runs the two background workers. Both claim rows with
// FOR UPDATE SKIP LOCKED and then hand each one to the same unmodified
// Complete(); overlapping ticks are harmless because the loser's guarded
// update matches zero rows.
type Scheduler struct {
	cron *cron.Cron
	svc  IService
	repo transactionRepo
}

func NewScheduler(svc IService, repo transactionRepo) *Scheduler {
	return &Scheduler{
		cron: cron.New(),
		svc:  svc,
		repo: repo,
	}
}

func (s *Scheduler) Start() {
	if _, err := s.cron.AddFunc("* * * * *", s.runTick); err != nil {
		slog.Error("Failed to add scheduler cron job", "error", err)
		return
	}
	if _, err := s.cron.AddFunc("* * * * *", s.sweepTick); err != nil {
		slog.Error("Failed to add sweeper cron job", "error", err)
		return
	}
	s.cron.Start()
	slog.Info("Transactions scheduler started")
}

func (s *Scheduler) Stop() error {
	ctx := s.cron.Stop()
	<-ctx.Done()
	slog.Info("Transactions scheduler stopped")
	return ctx.Err()
}

func (s *Scheduler) runTick() {
	if err := s.RunOnce(context.Background()); err != nil {
		slog.Error("Scheduler tick failed", "error", err)
	}
}

func (s *Scheduler) sweepTick() {
	if err := s.SweepOnce(context.Background()); err != nil {
		slog.Error("Sweeper tick failed", "error", err)
	}
}

// RunOnce executes every scheduled transaction that is now due. Exported so it
// is testable outside cron.
func (s *Scheduler) RunOnce(ctx context.Context) error {
	return s.drain(ctx, s.repo.ClaimDueTransactions, repo.TransactionStatusScheduled)
}

// SweepOnce finishes transactions left parked in 'crediting' by a crashed
// worker. A live worker that beat it simply yields zero rows.
func (s *Scheduler) SweepOnce(ctx context.Context) error {
	return s.drain(ctx, s.repo.ClaimStuckCrediting, repo.TransactionStatusCrediting)
}

// drain claims a batch and hands each row to the same unmodified Complete.
//
// The claim and the execution are deliberately separate transactions: that is
// what lets both workers share one Complete, and it is safe because losing a
// row is not an error. Whatever happens to one row, the rest of the batch still
// runs -- a tick that aborted early would strand due transactions behind a
// single bad one.
func (s *Scheduler) drain(
	ctx context.Context,
	claim func(context.Context, int32) ([]repo.Transaction, error),
	from repo.TransactionStatus,
) error {
	rows, err := claim(ctx, batchSize)
	if err != nil {
		return err
	}

	for _, row := range rows {
		id := uuid.UUID(row.ID.Bytes)

		_, err := s.svc.Complete(ctx, id, from)
		switch {
		case err == nil, errors.Is(err, ErrAlreadyProcessed):
			// Nothing to do: either we moved it or somebody else already had.
		case errors.Is(err, ErrInsufficientFunds):
			// Complete has already parked the row as 'failed'.
			slog.Warn("Transaction failed for insufficient funds", "transaction_id", id)
		default:
			slog.Error("Failed to complete transaction", "error", err, "transaction_id", id)
		}
	}

	return nil
}
