package transfers

import (
	"context"
	"errors"

	"github.com/Youssef-codin/NexusPay/internal/db"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
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

}

func (s *Service) GetTransfers(ctx context.Context) (res GetTransfersByIDResponse, err error) {
	id, err := api.GetTokenUserID(ctx)
	if err != nil {
		return GetTransfersByIDResponse{}, err
	}

}
