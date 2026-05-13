package transfers

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"
)

const maxConcurrent = 10

type Scheduler struct {
	cron          *cron.Cron
	transfersSvc  IService
	txManager     db.TxManager
	transfersRepo transfersRepo
	stopCh        chan struct{}
}

func NewScheduler(
	transfersSvc IService,
	txManager db.TxManager,
	transfersRepo transfersRepo,
) *Scheduler {
	return &Scheduler{
		cron:          cron.New(),
		transfersSvc:  transfersSvc,
		txManager:     txManager,
		transfersRepo: transfersRepo,
		stopCh:        make(chan struct{}),
	}
}

func (s *Scheduler) Start() {
	_, err := s.cron.AddFunc("0,30 * * * *", s.processScheduledTransfers)
	if err != nil {
		slog.Error("Failed to add cron job", "error", err)
		return
	}
	s.cron.Start()
	slog.Info("Transfers scheduler started")
}

func (s *Scheduler) Stop() error {
	ctx := s.cron.Stop()
	<-ctx.Done()
	close(s.stopCh)
	slog.Info("Transfers scheduler stopped")
	return ctx.Err()
}

func (s *Scheduler) processScheduledTransfers() {
	ctx := context.Background()
	now := pgtype.Timestamptz{
		Time:             time.Now().UTC(),
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}

	slog.Info("Scheduler tick", "time", now.Time)

	scheduledTransfers, err := s.transfersRepo.GetPendingScheduledTransfers(ctx, now)
	if err != nil {
		slog.Error("Failed to get pending scheduled transfers", "error", err)
		return
	}

	if len(scheduledTransfers) == 0 {
		slog.Info("No pending scheduled transfers")
		return
	}

	slog.Info("Processing scheduled transfers", "count", len(scheduledTransfers))

	var wg sync.WaitGroup
	sem := make(chan int, maxConcurrent)
	var successCount, failedCount atomic.Int32

	for _, st := range scheduledTransfers {
		wg.Go(func() {
			sem <- 1
			defer func() {
				<-sem
			}()

			if err := s.processOneTransfer(ctx, st); err != nil {
				failedCount.Add(1)
			} else {
				successCount.Add(1)
			}
		})
	}

	wg.Wait()
	slog.Info("Scheduled transfers batch complete",
		"total", len(scheduledTransfers),
		"success", successCount.Load(),
		"failed", failedCount.Load())
}

func (s *Scheduler) processOneTransfer(ctx context.Context, st repo.ScheduledTransfer) error {
	transfer, err := s.transfersRepo.GetTransferById(ctx, st.TransferID)
	if err != nil {
		return err
	}

	if transfer.Status != repo.TransferStatusPending {
		return nil
	}

	_, tErr := s.transfersSvc.ExecuteTransfer(ctx, transfer)
	_, err = s.transfersRepo.MarkScheduledTransferExecuted(ctx, st.ID)

	if tErr != nil {
		return tErr
	}

	if err != nil {
		return err
	}

	return nil
}
