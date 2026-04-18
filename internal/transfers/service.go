package transfers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/utils/validator"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrTransferNotFound = errors.New("transfer not found")
	ErrSelfTransfer     = errors.New("can not transfer to self")
	ErrBadRequest       = errors.New("bad request")
	ErrWrongOwnership   = errors.New("wallet belongs to somebody else")
	ErrAlreadyExecuted  = errors.New("transfer already executed")
	ErrTooLateToCancel  = errors.New(
		"too late to cancel transfer",
	)
)

type IService interface {
	CreateTransfer(
		ctx context.Context,
		req CreateTransferRequest,
	) (res CreateTransferResponse, err error)
	GetTransfers(ctx context.Context) (res GetTransfersByIDResponse, err error)
	GetTransferByID(
		ctx context.Context,
		req GetTransferByIDRequest,
	) (res GetTransferByIDResponse, err error)
	ListScheduledTransfers(
		ctx context.Context,
		userID uuid.UUID,
	) (res ListScheduledTransfersResponse, err error)
	CancelScheduledTransfers(
		ctx context.Context,
		req CancelScheduledTransfersRequest,
	) (res CancelScheduledTransfersResponse, err error)
	ExecuteTransfer(
		ctx context.Context,
		t repo.Transfer,
	) (transfer repo.Transfer, err error)
}

type Service struct {
	txManager      db.TxManager
	repo           transfersRepo
	walletSvc      wallet.IService
	transactionSvc transactions.IService
}

func NewService(
	txManager db.TxManager,
	repo transfersRepo,
	walletService wallet.IService,
	transactionsService transactions.IService,
) IService {
	return &Service{
		txManager:      txManager,
		repo:           repo,
		walletSvc:      walletService,
		transactionSvc: transactionsService,
	}
}

func (svc *Service) CreateTransfer(
	ctx context.Context,
	req CreateTransferRequest,
) (res CreateTransferResponse, err error) {
	if err := validator.Validate(&req); err != nil {
		return CreateTransferResponse{}, ErrBadRequest
	}

	wallet, err := svc.walletSvc.GetByUserId(ctx)
	if err != nil {
		return CreateTransferResponse{}, err
	}

	if req.ToWalletID == wallet.ID {
		return CreateTransferResponse{}, ErrSelfTransfer
	}

	txCtx, tx, err := svc.txManager.StartTx(ctx)
	if err != nil {
		return CreateTransferResponse{}, err
	}
	defer tx.Rollback(txCtx)

	sender, err := svc.transactionSvc.CreateTransaction(
		txCtx,
		transactions.CreateTransactionRequest{
			WalletID: wallet.ID,
			Amount:   req.Amount,
			Type:     repo.TransactionTypeDebit,
		},
	)

	if err != nil {
		return CreateTransferResponse{}, err
	}

	receiver, err := svc.transactionSvc.CreateTransaction(
		txCtx,
		transactions.CreateTransactionRequest{
			WalletID: req.ToWalletID,
			Amount:   req.Amount,
			Type:     repo.TransactionTypeCredit,
		},
	)

	if err != nil {
		return CreateTransferResponse{}, err
	}

	senderWalletID := sender.WalletID
	senderID := sender.ID
	receiverWalletID := receiver.WalletID
	receiverID := receiver.ID

	transfer, err := svc.repo.CreateTransfer(txCtx, repo.CreateTransferParams{
		FromWalletID: pgtype.UUID{
			Bytes: senderWalletID,
			Valid: true,
		},
		ToWalletID: pgtype.UUID{
			Bytes: receiverWalletID,
			Valid: true,
		},
		Amount: req.Amount,
		Status: repo.TransferStatusPending,
		Note: pgtype.Text{
			String: req.Note,
			Valid:  true,
		},
		DebitTransactionID: pgtype.UUID{
			Bytes: senderID,
			Valid: true,
		},
		CreditTransactionID: pgtype.UUID{
			Bytes: receiverID,
			Valid: true,
		},
	})

	if err != nil {
		return CreateTransferResponse{}, err
	}

	if req.ScheduledAt != nil {
		done := false
		attempts := 0
		var attemptErr error

		for !done && attempts < 3 {
			_, attemptErr = svc.repo.CreateScheduledTransfer(
				txCtx,
				repo.CreateScheduledTransferParams{
					TransferID: transfer.ID,
					ScheduledAt: pgtype.Timestamptz{
						Time:             *req.ScheduledAt,
						InfinityModifier: pgtype.Finite,
						Valid:            true,
					},
				},
			)
			if attemptErr == nil {
				done = true
			}
		}
		if attemptErr != nil {
			return CreateTransferResponse{}, attemptErr
		}

	} else {
		transfer, err = svc.ExecuteTransfer(txCtx, transfer)
		if err != nil {
			return CreateTransferResponse{}, err
		}
	}

	tx.Commit(txCtx)
	return CreateTransferResponse{
		ID:           uuid.UUID(transfer.ID.Bytes),
		FromWalletID: uuid.UUID(transfer.FromWalletID.Bytes),
		ToWalletID:   uuid.UUID(transfer.ToWalletID.Bytes),
		Amount:       transfer.Amount,
		Status:       transfer.Status,
		Note:         transfer.Note.String,
		CreatedAt:    transfer.CreatedAt.Time,
	}, nil
}

func (svc *Service) ExecuteTransfer(
	ctx context.Context,
	t repo.Transfer,
) (transfer repo.Transfer, err error) {
	sender, err := svc.walletSvc.GetById(ctx, wallet.GetWalletRequest{
		ID: uuid.UUID(t.FromWalletID.Bytes),
	})

	if err != nil {
		origErr := err
		err = svc.setBothTransactions(ctx, t, repo.TransactionStatusFailed)
		if err != nil {
			return t, err
		}
		t, err = svc.repo.UpdateTransferStatus(ctx, repo.UpdateTransferStatusParams{
			ID:     t.ID,
			Status: repo.TransferStatusFailed,
		})
		if err != nil {
			return t, fmt.Errorf("original error: %w; status update error: %w", origErr, err)
		}
		return t, origErr
	}

	receiver, err := svc.walletSvc.GetById(ctx, wallet.GetWalletRequest{
		ID: uuid.UUID(t.ToWalletID.Bytes),
	})

	if err != nil {
		origErr := err
		err = svc.setBothTransactions(ctx, t, repo.TransactionStatusFailed)
		if err != nil {
			return t, err
		}
		t, err = svc.repo.UpdateTransferStatus(ctx, repo.UpdateTransferStatusParams{
			ID:     t.ID,
			Status: repo.TransferStatusFailed,
		})
		if err != nil {
			return t, fmt.Errorf("original error: %w; status update error: %w", origErr, err)
		}
		return t, origErr
	}

	_, err = svc.walletSvc.DeductFromBalance(ctx, wallet.DeductRequest{
		WalletID: sender.ID,
		Amount:   t.Amount,
	})

	if err != nil {
		origErr := err
		err = svc.setBothTransactions(ctx, t, repo.TransactionStatusFailed)
		if err != nil {
			return t, err
		}
		t, err = svc.repo.UpdateTransferStatus(ctx, repo.UpdateTransferStatusParams{
			ID:     t.ID,
			Status: repo.TransferStatusFailed,
		})
		if err != nil {
			return t, fmt.Errorf("original error: %w; status update error: %w", origErr, err)
		}
		return t, origErr
	}

	_, err = svc.walletSvc.AddToWallet(ctx, wallet.AddToWalletRequest{
		WalletID: receiver.ID,
		Amount:   t.Amount,
	})

	if err != nil {
		origErr := err
		err = svc.setBothTransactions(ctx, t, repo.TransactionStatusFailed)
		if err != nil {
			return t, err
		}
		t, err = svc.repo.UpdateTransferStatus(ctx, repo.UpdateTransferStatusParams{
			ID:     t.ID,
			Status: repo.TransferStatusFailed,
		})
		if err != nil {
			return t, fmt.Errorf("original error: %w; status update error: %w", origErr, err)
		}
		return t, origErr
	}

	t, err = svc.repo.UpdateTransferStatus(ctx, repo.UpdateTransferStatusParams{
		ID:     t.ID,
		Status: repo.TransferStatusCompleted,
	})

	if err != nil {
		origErr := err
		err = svc.setBothTransactions(ctx, t, repo.TransactionStatusFailed)
		if err != nil {
			return t, err
		}
		t, err = svc.repo.UpdateTransferStatus(ctx, repo.UpdateTransferStatusParams{
			ID:     t.ID,
			Status: repo.TransferStatusFailed,
		})
		if err != nil {
			return t, fmt.Errorf("original error: %w; status update error: %w", origErr, err)
		}
		return t, origErr
	}

	return t, nil
}

func (svc *Service) setBothTransactions(
	ctx context.Context,
	t repo.Transfer,
	status repo.TransactionStatus,
) error {
	err := svc.transactionSvc.UpdateStatus(ctx, transactions.UpdateTransactionRequest{
		ID:     uuid.UUID(t.DebitTransactionID.Bytes),
		Status: status,
	})
	if err != nil {
		return err
	}
	err = svc.transactionSvc.UpdateStatus(ctx, transactions.UpdateTransactionRequest{
		ID:     uuid.UUID(t.CreditTransactionID.Bytes),
		Status: status,
	})
	if err != nil {
		return err
	}
	return nil
}

func (svc *Service) GetTransfers(ctx context.Context) (res GetTransfersByIDResponse, err error) {
	wallet, err := svc.walletSvc.GetByUserId(ctx)
	if err != nil {
		return GetTransfersByIDResponse{}, err
	}

	transfers, err := svc.repo.GetTransfersByWalletId(ctx, pgtype.UUID{
		Bytes: wallet.ID,
	})

	if err != nil {
		return GetTransfersByIDResponse{}, err
	}

	dtoTransfers := []TransferResponse{}

	for _, t := range transfers {
		dtoTransfers = append(dtoTransfers, TransferResponse{
			ID:        uuid.UUID(t.ID.Bytes),
			Amount:    t.Amount,
			Status:    t.Status,
			Note:      t.Note.String,
			CreatedAt: t.CreatedAt.Time,
		})
	}

	return GetTransfersByIDResponse{
		FromWalletID: wallet.ID,
		Transfers:    dtoTransfers,
	}, nil
}

func (svc *Service) GetTransferByID(
	ctx context.Context,
	req GetTransferByIDRequest,
) (res GetTransferByIDResponse, err error) {
	if err := validator.Validate(&req); err != nil {
		return GetTransferByIDResponse{}, ErrBadRequest
	}

	transfer, err := svc.repo.GetTransferById(ctx, pgtype.UUID{Bytes: req.ID, Valid: true})
	if err != nil {
		return GetTransferByIDResponse{}, ErrTransferNotFound
	}

	return GetTransferByIDResponse{
		Transfer: TransferResponse{
			ID:        uuid.UUID(transfer.ID.Bytes),
			Amount:    transfer.Amount,
			Status:    transfer.Status,
			Note:      transfer.Note.String,
			CreatedAt: transfer.CreatedAt.Time,
		},
	}, nil
}

func (svc *Service) ListScheduledTransfers(
	ctx context.Context,
	userID uuid.UUID,
) (res ListScheduledTransfersResponse, err error) {
	scheduledTransfers, err := svc.repo.GetScheduledTransfersByUserId(
		ctx,
		pgtype.UUID{Bytes: userID, Valid: true},
	)
	if err != nil {
		return ListScheduledTransfersResponse{}, err
	}

	var dtoScheduledTransfers []ScheduledTransferResponse
	for _, t := range scheduledTransfers {
		dtoScheduledTransfers = append(dtoScheduledTransfers, ScheduledTransferResponse{
			ID:          uuid.UUID(t.ID.Bytes),
			TransferID:  uuid.UUID(t.TransferID.Bytes),
			ScheduledAt: t.ScheduledAt.Time,
			ExecutedAt:  t.ExecutedAt.Time,
			CreatedAt:   t.CreatedAt.Time,
		})
	}

	return ListScheduledTransfersResponse{
		ScheduledTransfers: dtoScheduledTransfers,
	}, nil
}

func (svc *Service) CancelScheduledTransfers(
	ctx context.Context,
	req CancelScheduledTransfersRequest,
) (res CancelScheduledTransfersResponse, err error) {
	txCtx, tx, err := svc.txManager.StartTx(ctx)
	if err != nil {
		return CancelScheduledTransfersResponse{}, err
	}
	defer tx.Rollback(txCtx)

	st, err := svc.repo.GetScheduledTransferByTransferId(txCtx, pgtype.UUID{
		Bytes: req.TransferID,
		Valid: true,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CancelScheduledTransfersResponse{}, ErrTransferNotFound
		}
		return CancelScheduledTransfersResponse{}, err
	}

	t, err := svc.repo.GetTransferById(txCtx, pgtype.UUID{
		Bytes: st.TransferID.Bytes,
		Valid: true,
	})
	if err != nil {
		return CancelScheduledTransfersResponse{}, err
	}

	w, err := svc.walletSvc.GetByUserId(txCtx)
	if err != nil {
		return CancelScheduledTransfersResponse{}, err
	}

	if uuid.UUID(t.FromWalletID.Bytes) != w.ID {
		return CancelScheduledTransfersResponse{}, ErrWrongOwnership
	}

	if st.ExecutedAt.Valid {
		return CancelScheduledTransfersResponse{}, ErrAlreadyExecuted
	}

	now := time.Now().UTC().Truncate(time.Minute)
	scheduledAt := st.ScheduledAt.Time.UTC().Truncate(time.Minute)

	if now.After(scheduledAt) {
		return CancelScheduledTransfersResponse{}, ErrTooLateToCancel
	}

	st, err = svc.repo.CancelScheduledTransfer(txCtx, pgtype.UUID{
		Bytes: st.ID.Bytes,
	})
	if err != nil {
		return CancelScheduledTransfersResponse{}, err
	}

	_, err = svc.repo.UpdateTransferStatus(txCtx, repo.UpdateTransferStatusParams{
		ID:     t.ID,
		Status: repo.TransferStatusCancelled,
	})
	if err != nil {
		return CancelScheduledTransfersResponse{}, err
	}

	err = svc.setBothTransactions(txCtx, t, repo.TransactionStatusCancelled)
	if err != nil {
		return CancelScheduledTransfersResponse{}, err
	}

	tx.Commit(txCtx)
	return CancelScheduledTransfersResponse{
		CancelledID: uuid.UUID(st.ID.Bytes),
	}, nil
}
