package transactions

import (
	"context"
	"errors"
	"fmt"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/utils/validator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrTransactionNotFound     = errors.New("transaction not found")
	ErrBadRequest              = errors.New("bad request")
	ErrInsufficientFunds       = errors.New("insufficient funds")
	ErrAlreadySameStatus       = errors.New("transaction is already in the same state")
	ErrInvalidStatusTransition = errors.New("invalid state change")
)

type IService interface {
	GetById(ctx context.Context, req GetByIdRequest) (GetTransactionResponse, error)
	CreateTransaction(
		ctx context.Context,
		req CreateTransactionRequest,
	) (CreateTransactionResponse, error)
	UpdateStatus(ctx context.Context, req UpdateTransactionRequest) error
}

type Service struct {
	repo transactionRepo
}

func NewService(
	repo transactionRepo,
) IService {
	return &Service{
		repo: repo,
	}
}

func (svc *Service) GetById(
	ctx context.Context,
	req GetByIdRequest,
) (GetTransactionResponse, error) {
	if err := validator.Validate(&req); err != nil {
		return GetTransactionResponse{}, err
	}

	parsedId, _ := uuid.Parse(req.ID)

	transaction, err := svc.repo.GetTransactionById(ctx, pgtype.UUID{
		Bytes: parsedId,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetTransactionResponse{}, ErrTransactionNotFound
		}

		return GetTransactionResponse{}, err
	}

	return GetTransactionResponse{
		ID:          transaction.ID.String(),
		WalletID:    transaction.WalletID.String(),
		Amount:      transaction.Amount,
		Type:        transaction.Type,
		Status:      transaction.Status,
		TransferID:  new(transaction.TransferID.String()),
		CreatedAt:   transaction.CreatedAt.Time,
		UpdatedAt:   transaction.UpdatedAt.Time,
		DeletedAt:   &transaction.DeletedAt.Time,
		Description: transaction.Description.String,
	}, nil
}

func (svc *Service) CreateTransaction(
	ctx context.Context,
	req CreateTransactionRequest,
) (CreateTransactionResponse, error) {
	if err := validator.Validate(&req); err != nil {
		return CreateTransactionResponse{}, err
	}

	parsedWalletID, _ := uuid.Parse(req.WalletID)

	transaction, err := svc.repo.CreateTransaction(ctx, repo.CreateTransactionParams{
		WalletID: pgtype.UUID{
			Bytes: parsedWalletID,
			Valid: true,
		},
		Amount:     req.Amount,
		Type:       req.Type,
		Status:     req.Status,
		TransferID: pgtype.UUID{Valid: false},
		Description: pgtype.Text{
			String: req.Description,
			Valid:  true,
		},
	})

	if err != nil {
		return CreateTransactionResponse{}, err
	}

	return CreateTransactionResponse{
		ID:          transaction.ID.String(),
		WalletID:    transaction.WalletID.String(),
		Amount:      transaction.Amount,
		Type:        transaction.Type,
		Status:      transaction.Status,
		CreatedAt:   transaction.CreatedAt.Time,
		Description: transaction.Description.String,
	}, nil
}

func (svc *Service) UpdateStatus(
	ctx context.Context,
	req UpdateTransactionRequest,
) error {
	if err := validator.Validate(&req); err != nil {
		return err
	}

	parsedId, _ := uuid.Parse(req.ID)

	transaction, err := svc.repo.GetTransactionById(ctx, pgtype.UUID{
		Bytes: parsedId,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTransactionNotFound
		}
		return err
	}

	from := transaction.Status
	to := req.Status

	if from == to {
		return ErrAlreadySameStatus
	}

	switch from {
	case repo.TransactionStatusPending:
		if to != repo.TransactionStatusProcessing &&
			to != repo.TransactionStatusReversed &&
			to != repo.TransactionStatusFailed {
			return errInvalidTransition(from, to)
		}
	case repo.TransactionStatusProcessing:
		if to != repo.TransactionStatusCompleted &&
			to != repo.TransactionStatusFailed {
			return errInvalidTransition(from, to)
		}
	case repo.TransactionStatusCompleted:
		if to != repo.TransactionStatusReversing {
			return errInvalidTransition(from, to)
		}
	case repo.TransactionStatusFailed, repo.TransactionStatusReversed:
		return errInvalidTransition(from, to)

	case repo.TransactionStatusReversing:
		if to != repo.TransactionStatusReversed &&
			to != repo.TransactionStatusFailed {
			return errInvalidTransition(from, to)
		}
	default:
		return ErrBadRequest
	}

	_, err = svc.repo.UpdateTransactionStatus(ctx, repo.UpdateTransactionStatusParams{
		ID: pgtype.UUID{
			Bytes: parsedId,
			Valid: true,
		},
		Status: req.Status,
	})

	if err != nil {
		return err
	}

	return nil
}

func errInvalidTransition(from, to repo.TransactionStatus) error {
	return fmt.Errorf("cannot transition from %s to %s: %w",
		from,
		to,
		ErrInvalidStatusTransition,
	)

}
