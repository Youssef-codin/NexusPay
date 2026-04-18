package transfers

import (
	"context"
	"time"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"
)

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
		return
	}
	s.cron.Start()
}

func (s *Scheduler) Stop() {
	<-s.stopCh
	s.cron.Stop()
}

func (s *Scheduler) processScheduledTransfers() {
	ctx := context.Background()
	now := pgtype.Timestamptz{
		Time:             time.Now().UTC(),
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}

	scheduledTransfers, err := s.transfersRepo.GetPendingScheduledTransfers(ctx, now)
	if err != nil {
		return
	}

	for _, st := range scheduledTransfers {
		transfer, err := s.transfersRepo.GetTransferById(ctx, st.TransferID)
		if err != nil {
			continue
		}

		if transfer.Status != repo.TransferStatusPending {
			continue
		}

		_, err = s.transfersSvc.ExecuteTransfer(ctx, transfer)
		if err != nil {
			continue
		}

		_, err = s.transfersRepo.MarkScheduledTransferExecuted(ctx, st.ID)
		if err != nil {
			continue
		}
	}
}
