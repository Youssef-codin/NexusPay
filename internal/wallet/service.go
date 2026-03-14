package wallet

import (
	"context"
	"errors"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
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
)

type IService interface {
	GetById(ctx context.Context, req GetWalletRequest) (GetWalletResponse, error)
	GetByUserId(ctx context.Context) (GetWalletResponse, error)
	CreateWallet(ctx context.Context, req CreateWalletRequest) (CreateWalletResponse, error)
	AddToBalance(ctx context.Context, req AddToBalanceRequest) (UpdateBalanceResponse, error)
	DeductFromBalance(
		ctx context.Context,
		req DeductFromBalanceRequest,
	) (UpdateBalanceResponse, error)
}

type Service struct {
	repo walletRepository
}

func NewService(
	repo walletRepository,
) IService {
	return &Service{
		repo: repo,
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

func (svc *Service) AddToBalance(
	ctx context.Context,
	req AddToBalanceRequest,
) (UpdateBalanceResponse, error) {
	id, err := api.GetTokenUserID(ctx)
	if err != nil {
		return UpdateBalanceResponse{}, err
	}
	parsedId, _ := uuid.Parse(id)

	wallet, err := svc.repo.AddToBalance(ctx, repo.AddToBalanceParams{
		UserID: pgtype.UUID{
			Bytes: parsedId,
			Valid: true,
		},
		Balance: req.Amount,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpdateBalanceResponse{}, ErrWalletNotFound
		}

		return UpdateBalanceResponse{}, err
	}

	return UpdateBalanceResponse{
		ID:        wallet.ID.String(),
		UserID:    parsedId.String(),
		Balance:   wallet.Balance,
		UpdatedAt: wallet.UpdatedAt.Time,
	}, nil
}

func (svc *Service) DeductFromBalance(
	ctx context.Context,
	req DeductFromBalanceRequest,
) (UpdateBalanceResponse, error) {
	id, err := api.GetTokenUserID(ctx)
	if err != nil {
		return UpdateBalanceResponse{}, err
	}
	parsedId, _ := uuid.Parse(id)

	wallet, err := svc.GetByUserId(ctx)

	if err != nil {
		return UpdateBalanceResponse{}, err
	}

	if wallet.Balance < req.Amount {
		return UpdateBalanceResponse{}, ErrInsufficientFunds
	}

	newWallet, err := svc.repo.DeductFromBalance(ctx, repo.DeductFromBalanceParams{
		UserID: pgtype.UUID{
			Bytes: parsedId,
			Valid: true,
		},
		Balance: req.Amount,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpdateBalanceResponse{}, ErrWalletNotFound
		}

		return UpdateBalanceResponse{}, err
	}

	return UpdateBalanceResponse{
		ID:        newWallet.ID.String(),
		UserID:    parsedId.String(),
		Balance:   newWallet.Balance,
		UpdatedAt: newWallet.UpdatedAt.Time,
	}, nil
}
