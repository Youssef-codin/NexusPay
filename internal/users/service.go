package users

import (
	"context"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/db/redisDb"
	"github.com/jackc/pgx/v5"
)

type IService interface {
	Login(ctx context.Context, req LoginRequest) (repo.User, error)
	Register(ctx context.Context, req RegisterRequest) (repo.User, error)
}

type Service struct {
	repo  *repo.Queries
	db    *pgx.Conn
	users *redisDb.Users
}

func NewService(repo *repo.Queries, db *pgx.Conn, users *redisDb.Users) IService {
	return &Service{
		repo:  repo,
		db:    db,
		users: users,
	}
}

func (svc *Service) Login(ctx context.Context, req LoginRequest) (repo.User, error) {
	return repo.User{}, nil
}

func (svc *Service) Register(ctx context.Context, req RegisterRequest) (repo.User, error) {
	return repo.User{}, nil
}
