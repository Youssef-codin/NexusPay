package transactions

import (
	"context"
	"log/slog"

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
	return ErrNotImplemented
}

// SweepOnce finishes transactions left parked in 'crediting' by a crashed
// worker. A live worker that beat it simply yields zero rows.
func (s *Scheduler) SweepOnce(ctx context.Context) error {
	return ErrNotImplemented
}
