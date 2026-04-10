package transfers

import (
	"context"
	"errors"

	"github.com/Youssef-codin/NexusPay/internal/db"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserNotFound       = errors.New("user not found")
	ErrUsernameTaken      = errors.New("username taken")
	ErrBadRequest         = errors.New("Bad request")
	ErrUserAlreadyExists  = errors.New("User already exists")
	ErrPasswordTooLong    = errors.New("Password is too long")
	ErrTokenExpired       = errors.New("Token Expired")
)

type IService interface {
	CreateTransfer(
		ctx context.Context,
		req CreateTransferRequest,
	) (res CreateTransferResponse, err error)
	GetTransfers(ctx context.Context) (res GetTransfersByIDResponse, err error)
}

type Service struct {
	txManager db.TxManager
	repo      transfersRepo
	wallet    wallet.IService
}

func NewService(
	txManager db.TxManager,
	repo transfersRepo,
	wallet wallet.IService,
) IService {
	return &Service{
		txManager: txManager,
		repo:      repo,
		wallet:    wallet,
	}
}

func (s *Service) CreateTransfer(
	ctx context.Context,
	req CreateTransferRequest,
) (res CreateTransferResponse, err error) {

	return res, err
}

func (svc *Service) GetTransfers(ctx context.Context) (res GetTransfersByIDResponse, err error) {
	id, err := api.GetTokenUserID(ctx)
	if err != nil {
		return GetTransfersByIDResponse{}, err
	}
	ctxId, _ := uuid.Parse(id)

	transfers, err := svc.repo.GetTransfersByWalletId(ctx, pgtype.UUID{
		Bytes: ctxId,
		Valid: true,
	})

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
		FromWalletID: ctxId.String(),
		Transfers:    dtoTransfers,
	}, nil
}
