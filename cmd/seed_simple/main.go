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
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

type dbConfig struct {
	dsn string
}

func main() {
	for _, f := range []string{".env.local", "../.env.local", ".env.prod", "../.env.prod"} {
		if godotenv.Load(f) == nil {
			break
		}
	}
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

	ClearDB(ctx, pool, logger)

	queries := repo.New(pool)

	Seed(ctx, queries, logger)
}

func ClearDB(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	tables := []string{
		"scheduled_transfers",
		"transfers",
		"transactions",
		"wallets",
		"users",
	}

	for _, table := range tables {
		_, err := pool.Exec(ctx, "DELETE FROM "+table)
		if err != nil {
			logger.Warn("Failed to clear table", "table", table, "error", err)
		} else {
			logger.Info("Cleared table", "table", table)
		}
	}
}

func Seed(ctx context.Context, queries *repo.Queries, logger *slog.Logger) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}

	user, err := queries.CreateUser(ctx, repo.CreateUserParams{
		Email:    "user@gmail.com",
		Password: string(hashedPassword),
		FullName: "User",
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

	_, err = queries.CreateTransaction(ctx, repo.CreateTransactionParams{
		WalletID:    wallet.ID,
		Amount:    50000,
		Type:      repo.TransactionTypeCredit,
		Status:    repo.TransactionStatusCompleted,
		Description: pgtype.Text{String: "Initial deposit", Valid: true},
	})
	if err != nil {
		panic(err)
	}

	logger.Debug("Created initial deposit for user", "amount", 50000)

	hashedPassword, err = bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}

	richUser, err := queries.CreateUser(ctx, repo.CreateUserParams{
		Email:    "rich@gmail.com",
		Password: string(hashedPassword),
		FullName: "Rich Guy",
	})
	if err != nil {
		panic(err)
	}

	richWallet, err := queries.CreateWallet(ctx, repo.CreateWalletParams{
		UserID:  richUser.ID,
		Balance: 100000000,
	})
	if err != nil {
		panic(err)
	}

	logger.Info("Created rich user", "email", richUser.Email, "id", richUser.ID, "balance", richWallet.Balance)

	otherUsers := []struct {
		email   string
		name    string
		balance int64
	}{
		{"friend@email.com", "Friend", 10000},
		{"landlord@email.com", "Landlord", 0},
		{"grocery@store.com", "Grocery Store", 0},
		{"netflix@email.com", "Netflix", 0},
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

	richTopUps := []struct {
		amount    int64
		desc      string
		dayOffset int
	}{
		{10000000, "Initial deposit", 0},
		{5000000, "Wire transfer", 7},
		{2000000, "Crypto conversion", 20},
		{1500000, "Stock sale", 35},
	}

	for _, tu := range richTopUps {
		_, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID:    richWallet.ID,
			Amount:      tu.amount,
			Type:        repo.TransactionTypeCredit,
			Status:      repo.TransactionStatusCompleted,
			Description: pgtype.Text{String: tu.desc, Valid: true},
		})
		if err != nil {
			panic(err)
		}
		logger.Debug("Created rich top-up", "amount", tu.amount)
	}

	richOutgoingTransfers := []struct {
		toEmail   string
		amount    int64
		note      string
		dayOffset int
	}{
		{"friend@email.com", 50000, "gift", 5},
		{"netflix@email.com", 999, "subscription", 45},
	}

	for _, tr := range richOutgoingTransfers {
		toWalletID := otherWallets[tr.toEmail]
		note := pgtype.Text{String: tr.note, Valid: true}

		debitTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID:    richWallet.ID,
			Amount:      tr.amount,
			Type:        repo.TransactionTypeDebit,
			Status:      repo.TransactionStatusCompleted,
			Description: note,
		})
		if err != nil {
			panic(err)
		}

		creditTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID: toWalletID,
			Amount:   tr.amount,
			Type:     repo.TransactionTypeCredit,
			Status:   repo.TransactionStatusCompleted,
		})
		if err != nil {
			panic(err)
		}

		_, err = queries.CreateTransfer(ctx, repo.CreateTransferParams{
			FromWalletID:        richWallet.ID,
			ToWalletID:          toWalletID,
			Amount:              tr.amount,
			Status:              repo.TransferStatusCompleted,
			Note:                note,
			DebitTransactionID:  debitTx.ID,
			CreditTransactionID: creditTx.ID,
		})
		if err != nil {
			panic(err)
		}

		logger.Debug("Created rich outgoing transfer", "to", tr.toEmail, "amount", tr.amount)
	}

	richScheduledTransfers := []struct {
		toEmail string
		amount  int64
		note    string
		month   int
		day     int
	}{
		{"friend@email.com", 100000, "monthly allowance", 6, 15},
		{"landlord@email.com", 50000, "rent", 7, 1},
		{"grocery@store.com", 25000, "groceries", 8, 1},
		{"netflix@email.com", 999, "subscription", 10, 1},
	}

	for _, tr := range richScheduledTransfers {
		toWalletID := otherWallets[tr.toEmail]
		note := pgtype.Text{String: tr.note, Valid: true}
		scheduledAt := time.Date(2026, time.Month(tr.month), tr.day, 9, 0, 0, 0, time.UTC)

		debitTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID:    richWallet.ID,
			Amount:      tr.amount,
			Type:        repo.TransactionTypeDebit,
			Status:      repo.TransactionStatusPending,
			Description: note,
		})
		if err != nil {
			panic(err)
		}

		creditTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID: toWalletID,
			Amount:   tr.amount,
			Type:     repo.TransactionTypeCredit,
			Status:   repo.TransactionStatusPending,
		})
		if err != nil {
			panic(err)
		}

		transfer, err := queries.CreateTransfer(ctx, repo.CreateTransferParams{
			FromWalletID:        richWallet.ID,
			ToWalletID:          toWalletID,
			Amount:              tr.amount,
			Status:              repo.TransferStatusPending,
			Note:                note,
			DebitTransactionID:  debitTx.ID,
			CreditTransactionID: creditTx.ID,
		})
		if err != nil {
			panic(err)
		}

		_, err = queries.CreateScheduledTransfer(ctx, repo.CreateScheduledTransferParams{
			TransferID:  transfer.ID,
			ScheduledAt: pgtype.Timestamptz{Time: scheduledAt, Valid: true},
		})
		if err != nil {
			panic(err)
		}

		logger.Debug("Created rich scheduled transfer", "to", tr.toEmail, "scheduled_at", scheduledAt)
	}

	logger.Info("Seed complete!", "user_email", "user@gmail.com", "rich_email", "rich@gmail.com",
		"scheduled_transfers", len(richScheduledTransfers))
}