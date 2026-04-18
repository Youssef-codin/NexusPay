package auth

import (
	"context"
	"errors"
	"time"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/db/redisDb"
	"github.com/Youssef-codin/NexusPay/internal/security"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
	"github.com/go-chi/jwtauth/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	login(ctx context.Context, req loginRequest) (loginResponse, error)
	register(ctx context.Context, req registerRequest) (registerResponse, error)
	refreshToken(ctx context.Context, req refreshRequest) (refreshResponse, error)
	logout(ctx context.Context) error
}

type Service struct {
	txManager db.TxManager
	repo      authRepo
	users     *redisDb.Users
	auth      *security.Authenticator
	wallet    wallet.IService
}

func NewService(
	txManager db.TxManager,
	repo authRepo,
	users *redisDb.Users,
	auth *security.Authenticator,
	wallet wallet.IService,
) IService {
	return &Service{
		txManager: txManager,
		repo:      repo,
		users:     users,
		auth:      auth,
		wallet:    wallet,
	}
}

func (svc *Service) login(ctx context.Context, req loginRequest) (loginResponse, error) {
	user, err := svc.repo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return loginResponse{}, ErrUserNotFound
		}
		return loginResponse{}, err
	}

	if validPass, _ := security.ComparePass(req.Password, user.Password); !validPass {
		return loginResponse{}, ErrInvalidCredentials
	}

	rawRefreshToken := svc.auth.MakeRawRefreshToken()
	hashedRefreshToken := svc.auth.HashRefreshToken(rawRefreshToken)

	err = svc.repo.UpdateRefreshToken(ctx, repo.UpdateRefreshTokenParams{
		ID: user.ID,
		RefreshToken: pgtype.Text{
			String: hashedRefreshToken,
			Valid:  true,
		},
		TokenExpiresAt: pgtype.Timestamptz{
			Time:  time.Now().Add(svc.auth.RefreshTokenDuration),
			Valid: true,
		},
	})

	if err != nil {
		return loginResponse{}, err
	}

	jwtToken := svc.auth.MakeJWTToken(security.Claims{
		ID: user.ID.String(),
	})

	return loginResponse{
		Email:        user.Email,
		FullName:     user.FullName,
		JwtToken:     jwtToken,
		RefreshToken: rawRefreshToken,
	}, nil
}

func (svc *Service) register(ctx context.Context, req registerRequest) (registerResponse, error) {
	txCtx, tx, err := svc.txManager.StartTx(ctx)
	if err != nil {
		return registerResponse{}, err
	}
	defer tx.Rollback(txCtx)

	if _, err := svc.repo.GetUserByEmail(txCtx, req.Email); err == nil {
		return registerResponse{}, ErrUserAlreadyExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return registerResponse{}, err
	}

	hashedPass, err := security.HashPass(req.Password)
	if err != nil {
		return registerResponse{}, ErrPasswordTooLong
	}

	rawRefreshToken := svc.auth.MakeRawRefreshToken()
	hashedRefreshToken := svc.auth.HashRefreshToken(rawRefreshToken)

	user, err := svc.repo.CreateUser(txCtx, repo.CreateUserParams{
		Email:    req.Email,
		Password: hashedPass,
		FullName: req.FullName,
		RefreshToken: pgtype.Text{
			String: hashedRefreshToken,
			Valid:  true,
		},
		TokenExpiresAt: pgtype.Timestamptz{
			Time:  time.Now().Add(svc.auth.RefreshTokenDuration),
			Valid: true,
		},
	})

	if err != nil {
		return registerResponse{}, err
	}

	_, err = svc.wallet.CreateWallet(txCtx, wallet.CreateWalletRequest{
		UserID: uuid.UUID(user.ID.Bytes),
	})

	if err != nil {
		return registerResponse{}, err
	}

	jwtToken := svc.auth.MakeJWTToken(security.Claims{
		ID: user.ID.String(),
	})

	if err := tx.Commit(txCtx); err != nil {
		return registerResponse{}, err
	}

	return registerResponse{
		FullName:     user.FullName,
		Email:        user.Email,
		JwtToken:     jwtToken,
		RefreshToken: rawRefreshToken,
	}, nil
}

func (svc *Service) refreshToken(ctx context.Context, req refreshRequest) (refreshResponse, error) {
	hashedReqToken := svc.auth.HashRefreshToken(req.RefreshToken)

	user, err := svc.repo.GetUserByRefreshToken(ctx, pgtype.Text{
		String: hashedReqToken,
		Valid:  true,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return refreshResponse{}, ErrUserNotFound
		}
		return refreshResponse{}, err
	}

	if user.TokenExpiresAt.Time.Before(time.Now()) {
		return refreshResponse{}, ErrTokenExpired
	}

	rawRefreshToken := svc.auth.MakeRawRefreshToken()
	hashedRefreshToken := svc.auth.HashRefreshToken(rawRefreshToken)

	err = svc.repo.UpdateRefreshToken(ctx, repo.UpdateRefreshTokenParams{
		ID: user.ID,
		RefreshToken: pgtype.Text{
			String: hashedRefreshToken,
			Valid:  true,
		},
		TokenExpiresAt: pgtype.Timestamptz{
			Time:  time.Now().Add(svc.auth.RefreshTokenDuration),
			Valid: true,
		},
	})

	if err != nil {
		return refreshResponse{}, err
	}

	jwtToken := svc.auth.MakeJWTToken(security.Claims{
		ID: user.ID.String(),
	})

	return refreshResponse{
		JwtToken:     jwtToken,
		RefreshToken: rawRefreshToken,
	}, nil
}

func (svc *Service) logout(ctx context.Context) error {
	_, claims, _ := jwtauth.FromContext(ctx)
	id := claims["sub"].(string)
	parsedId, _ := uuid.Parse(id)

	_, err := svc.repo.GetUserById(ctx, pgtype.UUID{
		Bytes: parsedId,
		Valid: true,
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}

	err = svc.repo.RevokeRefreshToken(ctx, pgtype.UUID{
		Bytes: parsedId,
		Valid: true,
	})

	if err != nil {
		return err
	}

	return nil
}
