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
	wallet, err := svc.repo.GetWalletById(ctx, pgtype.UUID{
		Bytes: req.ID,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetWalletResponse{}, ErrWalletNotFound
		}
		return GetWalletResponse{}, err
	}

	return GetWalletResponse{
		ID:        uuid.UUID(wallet.ID.Bytes),
		UserID:    uuid.UUID(wallet.UserID.Bytes),
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
		ID:        uuid.UUID(wallet.ID.Bytes),
		UserID:    uuid.UUID(wallet.UserID.Bytes),
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
	_, err := svc.repo.GetWalletByUserId(ctx, pgtype.UUID{
		Bytes: req.UserID,
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
			Bytes: req.UserID,
			Valid: true,
		},
		Balance: 0,
	})

	if err != nil {
		return CreateWalletResponse{}, err
	}

	return CreateWalletResponse{
		ID:        uuid.UUID(wallet.ID.Bytes),
		UserID:    uuid.UUID(wallet.UserID.Bytes),
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
	userUUID, _ := uuid.Parse(id)

	wallet, err := svc.repo.GetWalletByUserId(ctx, pgtype.UUID{
		Bytes: userUUID,
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
			WalletID:    uuid.UUID(wallet.ID.Bytes),
			Amount:      req.Amount,
			Type:        repo.TransactionTypeCredit,
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
		ID:                uuid.UUID(wallet.ID.Bytes),
		UserID:            userUUID,
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
	wallet, err := svc.repo.GetWalletById(ctx, pgtype.UUID{
		Bytes: req.WalletID,
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

	newWallet, err := svc.repo.DeductFromBalance(ctx, repo.DeductFromBalanceParams{
		UserID:  wallet.UserID,
		Balance: req.Amount,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeductResponse{}, ErrWalletNotFound
		}
		return DeductResponse{}, err
	}

	return DeductResponse{
		ID:        uuid.UUID(newWallet.ID.Bytes),
		UserID:    uuid.UUID(wallet.UserID.Bytes),
		Status:    string(repo.TransactionStatusCompleted),
		UpdatedAt: newWallet.UpdatedAt.Time,
	}, nil
}

func (svc *Service) AddToWallet(
	ctx context.Context,
	req AddToWalletRequest,
) (AddToWalletResponse, error) {
	wallet, err := svc.repo.GetWalletById(ctx, pgtype.UUID{
		Bytes: req.WalletID,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AddToWalletResponse{}, ErrWalletNotFound
		}
		return AddToWalletResponse{}, err
	}

	updatedWallet, err := svc.repo.AddToBalance(ctx, repo.AddToBalanceParams{
		UserID:  wallet.UserID,
		Balance: req.Amount,
	})

	if err != nil {
		return AddToWalletResponse{}, err
	}

	return AddToWalletResponse{
		ID:        uuid.UUID(updatedWallet.ID.Bytes),
		UserID:    uuid.UUID(wallet.UserID.Bytes),
		Balance:   updatedWallet.Balance,
		UpdatedAt: updatedWallet.UpdatedAt.Time,
	}, nil
}
