package wallet

import (
	"context"
	"errors"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/payment"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrWalletNotFound      = errors.New("Wallet not found")
	ErrBadRequest          = errors.New("Bad request")
	ErrWalletAlreadyExists = errors.New("User already has a wallet")
	ErrInsufficientFunds   = errors.New("Insufficient funds")
	ErrAmountIsTooLow      = errors.New(
		"Amount is too low, must be at least 10 EGP (1000 Piastres)",
	)
)

type IService interface {
	GetById(ctx context.Context, req GetWalletRequest) (GetWalletResponse, error)
	GetByUserId(ctx context.Context) (GetWalletResponse, error)
	CreateWallet(ctx context.Context, req CreateWalletRequest) (CreateWalletResponse, error)
	TopUp(ctx context.Context, req TopUpRequest) (TopUpResponse, error)
	DeductFromBalance(
		ctx context.Context,
		req DeductRequest,
	) (DeductResponse, error)
	AddToWallet(ctx context.Context, req AddToWalletRequest) (AddToWalletResponse, error)
}

type Service struct {
	txManager       db.TxManager
	repo            walletRepo
	transactionsSvc transactions.IService
	paymentSvc      payment.IService
}

func NewService(
	txManager db.TxManager,
	repo walletRepo,
	transactionsSvc transactions.IService,
	paymentSvc payment.IService,
) IService {
	return &Service{
		txManager:       txManager,
		repo:            repo,
		transactionsSvc: transactionsSvc,
		paymentSvc:      paymentSvc,
	}
}

func (svc *Service) GetById(ctx context.Context, req GetWalletRequest) (GetWalletResponse, error) {
	parsedId, _ := uuid.Parse(req.ID)

	wallet, err := svc.repo.GetWalletById(ctx, pgtype.UUID{
		Bytes: parsedId,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetWalletResponse{}, ErrWalletNotFound
		}
		return GetWalletResponse{}, err
	}

	return GetWalletResponse{
		ID:        wallet.ID.String(),
		UserID:    wallet.UserID.String(),
		Balance:   wallet.Balance,
		CreatedAt: wallet.CreatedAt.Time,
		UpdatedAt: wallet.UpdatedAt.Time,
		DeletedAt: &wallet.DeletedAt.Time,
	}, nil
}

func (svc *Service) GetByUserId(ctx context.Context) (GetWalletResponse, error) {
	id, err := api.GetTokenUserID(ctx)
	if err != nil {
		return GetWalletResponse{}, err
	}
	ctxId, _ := uuid.Parse(id)

	wallet, err := svc.repo.GetWalletByUserId(ctx, pgtype.UUID{
		Bytes: ctxId,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetWalletResponse{}, ErrWalletNotFound
		}
		return GetWalletResponse{}, err
	}

	return GetWalletResponse{
		ID:        wallet.ID.String(),
		UserID:    wallet.UserID.String(),
		Balance:   wallet.Balance,
		CreatedAt: wallet.CreatedAt.Time,
		UpdatedAt: wallet.UpdatedAt.Time,
		DeletedAt: &wallet.DeletedAt.Time,
	}, nil
}

func (svc *Service) CreateWallet(
	ctx context.Context,
	req CreateWalletRequest,
) (CreateWalletResponse, error) {
	id, err := api.GetTokenUserID(ctx)
	if err != nil {
		return CreateWalletResponse{}, err
	}
	parsedId, _ := uuid.Parse(id)

	_, err = svc.repo.GetWalletByUserId(ctx, pgtype.UUID{
		Bytes: parsedId,
		Valid: true,
	})

	if err == nil {
		return CreateWalletResponse{}, ErrWalletAlreadyExists
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return CreateWalletResponse{}, err
	}

	wallet, err := svc.repo.CreateWallet(ctx, repo.CreateWalletParams{
		UserID: pgtype.UUID{
			Bytes: parsedId,
			Valid: true,
		},
	})

	if err != nil {
		return CreateWalletResponse{}, err
	}

	return CreateWalletResponse{
		ID:        wallet.ID.String(),
		UserID:    wallet.UserID.String(),
		Balance:   wallet.Balance,
		CreatedAt: wallet.CreatedAt.Time,
	}, nil
}

func (svc *Service) TopUp(
	ctx context.Context,
	req TopUpRequest,
) (TopUpResponse, error) {

	if req.Amount < 1000 {
		return TopUpResponse{}, ErrAmountIsTooLow
	}

	id, err := api.GetTokenUserID(ctx)
	if err != nil {
		return TopUpResponse{}, err
	}

	parsedId, _ := uuid.Parse(id)

	wallet, err := svc.repo.GetWalletByUserId(ctx, pgtype.UUID{
		Bytes: parsedId,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TopUpResponse{}, ErrWalletNotFound
		}
		return TopUpResponse{}, err
	}

	transaction, err := svc.transactionsSvc.CreateTransaction(
		ctx,
		transactions.CreateTransactionRequest{
			WalletID:    wallet.ID.String(),
			Amount:      req.Amount,
			Type:        repo.TransactionTypeCredit,
			Status:      repo.TransactionStatusPending,
			Description: req.Description,
		},
	)

	if err != nil {
		return TopUpResponse{}, err
	}

	paymentRes, err := svc.paymentSvc.ProcessPayment(ctx, payment.ProcessPaymentRequest{
		Amount:        req.Amount,
		TransactionID: transaction.ID,
		Description:   req.Description,
	})

	if err != nil {
		svc.transactionsSvc.UpdateStatus(ctx, transactions.UpdateTransactionRequest{
			ID:     transaction.ID,
			Status: repo.TransactionStatusFailed,
		})
		return TopUpResponse{}, err
	}

	return TopUpResponse{
		ID:                wallet.ID.String(),
		UserID:            parsedId.String(),
		Status:            string(paymentRes.Status),
		UpdatedAt:         wallet.UpdatedAt.Time,
		ProviderPaymentID: paymentRes.ProviderPaymentID,
		ClientSecret:      paymentRes.ClientSecret,
	}, nil
}

func (svc *Service) DeductFromBalance(
	ctx context.Context,
	req DeductRequest,
) (DeductResponse, error) {
	id, err := api.GetTokenUserID(ctx)
	if err != nil {
		return DeductResponse{}, err
	}
	parsedId, _ := uuid.Parse(id)

	txCtx, tx, err := svc.txManager.StartTx(ctx)
	if err != nil {
		return DeductResponse{}, err
	}
	defer tx.Rollback(ctx)

	wallet, err := svc.repo.GetWalletByUserId(txCtx, pgtype.UUID{
		Bytes: parsedId,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeductResponse{}, ErrWalletNotFound
		}
		return DeductResponse{}, err
	}

	if wallet.Balance < req.Amount {
		return DeductResponse{}, ErrInsufficientFunds
	}

	newWallet, err := svc.repo.DeductFromBalance(txCtx, repo.DeductFromBalanceParams{
		UserID: pgtype.UUID{
			Bytes: parsedId,
			Valid: true,
		},
		Balance: req.Amount,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeductResponse{}, ErrWalletNotFound
		}
		return DeductResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return DeductResponse{}, err
	}

	return DeductResponse{
		ID:        newWallet.ID.String(),
		UserID:    parsedId.String(),
		Status:    string(repo.TransactionStatusCompleted),
		UpdatedAt: newWallet.UpdatedAt.Time,
	}, nil
}

func (svc *Service) AddToWallet(
	ctx context.Context,
	req AddToWalletRequest,
) (AddToWalletResponse, error) {
	parsedId, _ := uuid.Parse(req.WalletID)

	wallet, err := svc.repo.GetWalletById(ctx, pgtype.UUID{
		Bytes: parsedId,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AddToWalletResponse{}, ErrWalletNotFound
		}
		return AddToWalletResponse{}, err
	}

	txCtx, tx, err := svc.txManager.StartTx(ctx)
	if err != nil {
		return AddToWalletResponse{}, err
	}
	defer tx.Rollback(txCtx)

	updatedWallet, err := svc.repo.AddToBalance(txCtx, repo.AddToBalanceParams{
		UserID:  wallet.UserID,
		Balance: req.Amount,
	})

	if err != nil {
		return AddToWalletResponse{}, err
	}

	if err := tx.Commit(txCtx); err != nil {
		return AddToWalletResponse{}, err
	}

	return AddToWalletResponse{
		ID:        updatedWallet.ID.String(),
		UserID:    uuid.UUID(wallet.UserID.Bytes).String(),
		Balance:   updatedWallet.Balance,
		UpdatedAt: updatedWallet.UpdatedAt.Time,
	}, nil
}
