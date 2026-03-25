package users

import (
	"context"
	"errors"

	"github.com/Youssef-codin/NexusPay/internal/db/redisDb"
	"github.com/jackc/pgx/v5"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrBadRequest   = errors.New("Bad request")
)

type IService interface {
	findByName(ctx context.Context, req FindUserRequest) (FindUserResponse, error)
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

func (svc *Service) findByName(ctx context.Context, req FindUserRequest) (FindUserResponse, error) {
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
			ID:       user.ID.String(),
			FullName: user.FullName,
		})
	}

	return FindUserResponse{
		Users: cleanUsers,
	}, nil
}
