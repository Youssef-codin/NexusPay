package transfers

import (
	"context"
	"errors"
	"fmt"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/utils/validator"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrTransferNotFound = errors.New("transfer not found")
	ErrSelfTransfer     = errors.New("can not transfer to self")
	ErrBadRequest       = errors.New("bad request")
)

type IService interface {
	CreateTransfer(
		ctx context.Context,
		req CreateTransferRequest,
	) (res CreateTransferResponse, err error)
	GetTransfers(ctx context.Context) (res GetTransfersByIDResponse, err error)
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

	senderWalletID, _ := uuid.Parse(sender.WalletID)
	senderID, _ := uuid.Parse(sender.ID)
	receiverWalletID, _ := uuid.Parse(receiver.WalletID)
	receiverID, _ := uuid.Parse(receiver.ID)

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

	transfer, err = svc.ExecuteTransfer(txCtx, transfer)
	if err != nil {
		return CreateTransferResponse{}, err
	}

	tx.Commit(txCtx)
	return CreateTransferResponse{
		ID:           transfer.ID.String(),
		FromWalletID: transfer.FromWalletID.String(),
		ToWalletID:   transfer.ToWalletID.String(),
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
		ID: t.FromWalletID.String(),
	})

	if err != nil {
		origErr := err
		svc.setBothTransactionsFailed(ctx, t)
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
		ID: t.ToWalletID.String(),
	})

	if err != nil {
		origErr := err
		svc.setBothTransactionsFailed(ctx, t)
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
		svc.setBothTransactionsFailed(ctx, t)
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
		svc.setBothTransactionsFailed(ctx, t)
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
		svc.setBothTransactionsFailed(ctx, t)
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

func (svc *Service) setBothTransactionsFailed(ctx context.Context, t repo.Transfer) {
	svc.transactionSvc.UpdateStatus(ctx, transactions.UpdateTransactionRequest{
		ID:     t.DebitTransactionID.String(),
		Status: repo.TransactionStatusFailed,
	})
	svc.transactionSvc.UpdateStatus(ctx, transactions.UpdateTransactionRequest{
		ID:     t.CreditTransactionID.String(),
		Status: repo.TransactionStatusFailed,
	})
}

func (svc *Service) GetTransfers(ctx context.Context) (res GetTransfersByIDResponse, err error) {
	wallet, err := svc.walletSvc.GetByUserId(ctx)

	walletID, _ := uuid.Parse(wallet.ID)
	transfers, err := svc.repo.GetTransfersByWalletId(ctx, pgtype.UUID{
		Bytes: walletID,
		Valid: true,
	})

	if err != nil {
		return GetTransfersByIDResponse{}, err
	}

	dtoTransfers := []TransferResponse{}

	for _, t := range transfers {
		dtoTransfers = append(dtoTransfers, TransferResponse{
			ID:        t.ID.String(),
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
