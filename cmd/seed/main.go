package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/utils/env"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type dbConfig struct {
	dsn string
}

func main() {
	ctx := context.Background()

	cfg := dbConfig{
		dsn: env.GetEnvVar(
			"GOOSE_DBSTRING",
			"host=localhost user=joe-arch password=password port=5433 dbname=nexuspay sslmode=disable",
		),
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
	}))
	slog.SetDefault(logger)

	pool, err := pgxpool.New(ctx, cfg.dsn)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("Connection to db FAILED", "error", err)
		os.Exit(1)
	}

	logger.Info("Connected to db")

	queries := repo.New(pool)

	Seed(ctx, queries, logger)
}

func Seed(ctx context.Context, queries *repo.Queries, logger *slog.Logger) {
	targetEmail := "user@gmail.com"

	existingUser, err := queries.GetUserByEmail(ctx, targetEmail)
	if err == nil {
		logger.Info("User already exists", "email", targetEmail, "id", existingUser.ID)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}

	user, err := queries.CreateUser(ctx, repo.CreateUserParams{
		Email:    targetEmail,
		Password: string(hashedPassword),
		FullName: "Youssef",
	})
	if err != nil {
		panic(err)
	}

	logger.Info("Created user", "email", user.Email, "id", user.ID)

	wallet, err := queries.CreateWallet(ctx, repo.CreateWalletParams{
		UserID:  user.ID,
		Balance: 50000,
	})
	if err != nil {
		panic(err)
	}

	logger.Info("Created wallet", "id", wallet.ID, "balance", wallet.Balance)

	otherUsers := []struct {
		email   string
		name    string
		balance int64
	}{
		{"netflix@email.com", "Netflix", 0},
		{"spotify@email.com", "Spotify", 0},
		{"amazon@email.com", "Amazon", 0},
		{"uber@email.com", "Uber", 0},
		{"electricity@provider.com", "Electricity Provider", 0},
		{"water@provider.com", "Water Provider", 0},
		{"landlord@email.com", "Landlord", 0},
		{"grocery@store.com", "Grocery Store", 0},
		{"restaurant@email.com", "Restaurant", 0},
		{"friend@email.com", "Ahmed Friend", 10000},
		{"ali@email.com", "Ali", 15000},
		{"mohamed@email.com", "Mohamed", 20000},
	}

	otherWallets := make(map[string]pgtype.UUID)

	for _, ou := range otherUsers {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
		if err != nil {
			panic(err)
		}

		otherUser, err := queries.CreateUser(ctx, repo.CreateUserParams{
			Email:    ou.email,
			Password: string(hashedPassword),
			FullName: ou.name,
		})
		if err != nil {
			panic(err)
		}

		otherWallet, err := queries.CreateWallet(ctx, repo.CreateWalletParams{
			UserID:  otherUser.ID,
			Balance: ou.balance,
		})
		if err != nil {
			panic(err)
		}

		otherWallets[ou.email] = otherWallet.ID
		logger.Debug("Created other user/wallet", "email", ou.email, "wallet_id", otherWallet.ID)
	}

	transfers := []struct {
		toEmail string
		amount int64
		note  string
	}{
		{"netflix@email.com", 499, "subscription"},
		{"spotify@email.com", 199, "subscription"},
		{"amazon@email.com", 2500, "shopping"},
		{"uber@email.com", 350, "transport"},
		{"electricity@provider.com", 1200, "utilities"},
		{"water@provider.com", 400, "utilities"},
		{"landlord@email.com", 8000, "rent"},
		{"grocery@store.com", 1800, "shopping"},
		{"restaurant@email.com", 750, "food"},
		{"friend@email.com", 500, "other"},
		{"ali@email.com", 3000, "other"},
		{"mohamed@email.com", 4500, "other"},
	}

	var totalTransferred int64
	for _, tr := range transfers {
		totalTransferred += tr.amount
	}

	rnd := time.Now().AddDate(0, 0, -30)

	for i, tr := range transfers {
		toWalletID := otherWallets[tr.toEmail]

		randomTime := rnd.Add(time.Duration(i) * 24 * time.Hour)
		if i > 6 {
			randomTime = randomTime.Add(12 * time.Hour)
		}
		_ = randomTime

		note := pgtype.Text{String: tr.note, Valid: true}

		debitTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID: wallet.ID,
			Amount:  tr.amount,
			Type:    repo.TransactionTypeDebit,
			Status:  repo.TransactionStatusCompleted,
		})
		if err != nil {
			panic(err)
		}

		creditTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID: toWalletID,
			Amount:  tr.amount,
			Type:    repo.TransactionTypeCredit,
			Status:  repo.TransactionStatusCompleted,
		})
		if err != nil {
			panic(err)
		}

		_, err = queries.CreateTransfer(ctx, repo.CreateTransferParams{
			FromWalletID:        wallet.ID,
			ToWalletID:          toWalletID,
			Amount:             tr.amount,
			Status:             repo.TransferStatusCompleted,
			Note:               note,
			DebitTransactionID:  debitTx.ID,
			CreditTransactionID: creditTx.ID,
		})
		if err != nil {
			panic(err)
		}

		logger.Debug("Created transfer",
			"from", user.Email,
			"to", tr.toEmail,
			"amount", tr.amount,
			"note", tr.note,
		)
	}

	finalBalance := wallet.Balance - totalTransferred

	logger.Info("Final wallet balance", "balance", finalBalance, "initial", wallet.Balance, "transferred", totalTransferred)
	logger.Info("Seed complete!", "email", targetEmail)
}