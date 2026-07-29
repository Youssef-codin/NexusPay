package users

import (
	"context"
	"errors"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/db/redisDb"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrBadRequest        = errors.New("bad request")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

type IService interface {
	FindByName(ctx context.Context, req FindUserRequest) (FindUserResponse, error)
	GetMe(ctx context.Context) (GetMeResponse, error)
	GetByID(ctx context.Context, id uuid.UUID) (GetMeResponse, error)
	Debit(ctx context.Context, req BalanceRequest) (BalanceResponse, error)
	Credit(ctx context.Context, req BalanceRequest) (BalanceResponse, error)
}

type Service struct {
	repo  userRepo
	users *redisDb.Users
}

func NewService(repo userRepo, users *redisDb.Users) IService {
	return &Service{
		repo:  repo,
		users: users,
	}
}

func (svc *Service) FindByName(
	ctx context.Context,
	req FindUserRequest,
) (FindUserResponse, error) {
	users, err := svc.repo.GetUserByName(ctx, req.FullName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return FindUserResponse{Users: []UserType{}}, nil
		}
		return FindUserResponse{}, err
	}

	cleanUsers := make([]UserType, 0, len(users))
	for _, user := range users {
		cleanUsers = append(cleanUsers, UserType{
			ID:       uuid.UUID(user.ID.Bytes),
			FullName: user.FullName,
		})
	}

	return FindUserResponse{
		Users: cleanUsers,
	}, nil
}

func (svc *Service) GetMe(ctx context.Context) (GetMeResponse, error) {
	userIDStr, err := api.GetTokenUserID(ctx)
	if err != nil {
		return GetMeResponse{}, err
	}
	userUUID, err := uuid.Parse(userIDStr)
	if err != nil {
		return GetMeResponse{}, ErrBadRequest
	}

	return svc.GetByID(ctx, userUUID)
}

func (svc *Service) GetByID(ctx context.Context, id uuid.UUID) (GetMeResponse, error) {
	user, err := svc.repo.GetUserById(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetMeResponse{}, ErrUserNotFound
		}
		return GetMeResponse{}, err
	}

	return GetMeResponse{
		ID:        uuid.UUID(user.ID.Bytes),
		Email:     user.Email,
		FullName:  user.FullName,
		Balance:   user.Balance,
		CreatedAt: user.CreatedAt.Time,
	}, nil
}

// Debit subtracts from a balance. Zero rows back from the guarded update means
// the funds were not there -- that guard, running inside the caller's
// transaction, is the only real protection against overdraft.
func (svc *Service) Debit(ctx context.Context, req BalanceRequest) (BalanceResponse, error) {
	user, err := svc.repo.DebitUser(ctx, repo.DebitUserParams{
		ID:     pgtype.UUID{Bytes: req.UserID, Valid: true},
		Amount: req.Amount,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BalanceResponse{}, ErrInsufficientFunds
		}
		return BalanceResponse{}, err
	}

	return BalanceResponse{
		UserID:  uuid.UUID(user.ID.Bytes),
		Balance: user.Balance,
	}, nil
}

func (svc *Service) Credit(ctx context.Context, req BalanceRequest) (BalanceResponse, error) {
	user, err := svc.repo.CreditUser(ctx, repo.CreditUserParams{
		ID:     pgtype.UUID{Bytes: req.UserID, Valid: true},
		Amount: req.Amount,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BalanceResponse{}, ErrUserNotFound
		}
		return BalanceResponse{}, err
	}

	return BalanceResponse{
		UserID:  uuid.UUID(user.ID.Bytes),
		Balance: user.Balance,
	}, nil
}
