package stripe

import (
	"context"
	"errors"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
)

var (
	ErrParseEvent        = errors.New("failed to parse event")
	ErrAlreadyProcessing = errors.New("already processing transaction")
)

type IService interface {
	HandlePaymentSucceeded(ctx context.Context, req HandlePaymentSucceededRequest) error
	HandlePaymentFailed(ctx context.Context, req HandlePaymentFailedRequest) error
	HandlePaymentCanceled(ctx context.Context, req HandlePaymentCanceledRequest) error
}

type WebhookService struct {
	txManager      db.TxManager
	walletSvc      wallet.IService
	transactionSvc transactions.IService
}

func NewWebhookService(
	txManager db.TxManager,
	walletSvc wallet.IService,
	transactionSvc transactions.IService,
) IService {
	return &WebhookService{
		txManager:      txManager,
		walletSvc:      walletSvc,
		transactionSvc: transactionSvc,
	}
}

func (svc *WebhookService) HandlePaymentSucceeded(
	ctx context.Context,
	req HandlePaymentSucceededRequest,
) error {
	txCtx, tx, err := svc.txManager.StartTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	transaction, err := svc.transactionSvc.GetById(txCtx, transactions.GetByIdRequest{
		ID: req.TransactionID,
	})

	if err != nil {
		return err
	}

	if transaction.Status == repo.TransactionStatusProcessing {
		return ErrAlreadyProcessing
	}

	err = svc.transactionSvc.UpdateStatus(txCtx, transactions.UpdateTransactionRequest{
		ID:     transaction.ID,
		Status: repo.TransactionStatusProcessing,
	})

	if err != nil {
		return err
	}

	_, err = svc.walletSvc.AddToWallet(txCtx, wallet.AddToWalletRequest{
		WalletID: transaction.WalletID,
		Amount:   transaction.Amount,
	})

	if err != nil {
		return err
	}

	err = svc.transactionSvc.UpdateStatus(
		txCtx,
		transactions.UpdateTransactionRequest{
			ID:     transaction.ID,
			Status: repo.TransactionStatusCompleted,
		},
	)

	if err != nil {
		return err
	}

	return tx.Commit(txCtx)
}

func (svc *WebhookService) HandlePaymentFailed(
	ctx context.Context,
	req HandlePaymentFailedRequest,
) error {
	return svc.transactionSvc.UpdateStatus(ctx, transactions.UpdateTransactionRequest{
		ID:     req.TransactionID,
		Status: repo.TransactionStatusFailed,
	})
}

func (svc *WebhookService) HandlePaymentCanceled(
	ctx context.Context,
	req HandlePaymentCanceledRequest,
) error {
	return svc.transactionSvc.UpdateStatus(ctx, transactions.UpdateTransactionRequest{
		ID:     req.TransactionID,
		Status: repo.TransactionStatusFailed,
	})
}
