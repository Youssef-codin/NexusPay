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
		{"salary@company.com", "Employer Inc", 0},
		{"refund@store.com", "Store Refund", 0},
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

	outgoingTransfers := []struct {
		toEmail    string
		amount     int64
		note       string
		dayOffset  int
		status     repo.TransferStatus
	}{
		{"netflix@email.com", 499, "subscription", 1, repo.TransferStatusCompleted},
		{"spotify@email.com", 199, "subscription", 2, repo.TransferStatusCompleted},
		{"amazon@email.com", 2500, "shopping", 3, repo.TransferStatusCompleted},
		{"uber@email.com", 350, "transport", 5, repo.TransferStatusCompleted},
		{"electricity@provider.com", 1200, "utilities", 7, repo.TransferStatusCompleted},
		{"water@provider.com", 400, "utilities", 8, repo.TransferStatusCompleted},
		{"landlord@email.com", 8000, "rent", 10, repo.TransferStatusCompleted},
		{"grocery@store.com", 1800, "shopping", 12, repo.TransferStatusCompleted},
		{"restaurant@email.com", 750, "food", 14, repo.TransferStatusCompleted},
		{"friend@email.com", 500, "reimbursement", 15, repo.TransferStatusCompleted},
		{"ali@email.com", 3000, "lend", 18, repo.TransferStatusCompleted},
		{"mohamed@email.com", 4500, "group expense", 20, repo.TransferStatusCompleted},
		{"landlord@email.com", 8000, "rent", 35, repo.TransferStatusPending},
		{"electricity@provider.com", 1200, "utilities", 38, repo.TransferStatusFailed},
		{"amazon@email.com", 3500, "shopping", 25, repo.TransferStatusFailed},
	}

	var totalOutgoing int64
	for _, tr := range outgoingTransfers {
		if tr.status == repo.TransferStatusCompleted {
			totalOutgoing += tr.amount
		}
	}

	for _, tr := range outgoingTransfers {
		toWalletID := otherWallets[tr.toEmail]

		note := pgtype.Text{String: tr.note, Valid: true}
		txStatus := repo.TransactionStatusCompleted
		if tr.status == repo.TransferStatusPending {
			txStatus = repo.TransactionStatusPending
		} else if tr.status == repo.TransferStatusFailed {
			txStatus = repo.TransactionStatusFailed
		}

		debitTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID:    wallet.ID,
			Amount:      tr.amount,
			Type:        repo.TransactionTypeDebit,
			Status:      txStatus,
			Description: pgtype.Text{String: tr.note, Valid: true},
		})
		if err != nil {
			panic(err)
		}

		creditTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID:     toWalletID,
			Amount:       tr.amount,
			Type:         repo.TransactionTypeCredit,
			Status:       txStatus,
			
		})
		if err != nil {
			panic(err)
		}

		transfer, err := queries.CreateTransfer(ctx, repo.CreateTransferParams{
			FromWalletID:        wallet.ID,
			ToWalletID:          toWalletID,
			Amount:              tr.amount,
			Status:              tr.status,
			Note:                note,
			DebitTransactionID:  debitTx.ID,
			CreditTransactionID: creditTx.ID,
		})
		if err != nil {
			panic(err)
		}

		logger.Debug("Created outgoing transfer",
			"from", user.Email,
			"to", tr.toEmail,
			"amount", tr.amount,
			"status", tr.status,
		)

		if tr.status == repo.TransferStatusPending {
			scheduledTime := time.Now().Add(3 * 24 * time.Hour)
			_, err = queries.CreateScheduledTransfer(ctx, repo.CreateScheduledTransferParams{
				TransferID:  transfer.ID,
				ScheduledAt: pgtype.Timestamptz{Time: scheduledTime, Valid: true},
			})
			if err != nil {
				panic(err)
			}
			logger.Debug("Created scheduled transfer", "execute_at", scheduledTime)
		}
	}

	incomingTransfers := []struct {
		fromEmail   string
		amount      int64
		note        string
		dayOffset   int
	}{
		{"friend@email.com", 500, "reimbursement", 4},
		{"ali@email.com", 2000, "dinner", 9},
		{"mohamed@email.com", 5000, "group trip", 16},
		{"salary@company.com", 25000, "monthly salary", 22},
		{"friend@email.com", 250, "coffee", 28},
		{"salary@company.com", 25000, "monthly salary", 50},
	}

	var totalIncoming int64
	for _, tr := range incomingTransfers {
		totalIncoming += tr.amount
	}

	for _, tr := range incomingTransfers {
		fromWalletID := otherWallets[tr.fromEmail]

		note := pgtype.Text{String: tr.note, Valid: true}

		creditTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID:     wallet.ID,
			Amount:       tr.amount,
			Type:         repo.TransactionTypeCredit,
			Status:       repo.TransactionStatusCompleted,
			Description:  pgtype.Text{String: tr.note, Valid: true},
			
		})
		if err != nil {
			panic(err)
		}

		debitTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID:  fromWalletID,
			Amount:    tr.amount,
			Type:      repo.TransactionTypeDebit,
			Status:    repo.TransactionStatusCompleted,
			
		})
		if err != nil {
			panic(err)
		}

		_, err = queries.CreateTransfer(ctx, repo.CreateTransferParams{
			FromWalletID:        fromWalletID,
			ToWalletID:          wallet.ID,
			Amount:              tr.amount,
			Status:              repo.TransferStatusCompleted,
			Note:                note,
			DebitTransactionID:  debitTx.ID,
			CreditTransactionID: creditTx.ID,
		})
		if err != nil {
			panic(err)
		}

		logger.Debug("Created incoming transfer",
			"from", tr.fromEmail,
			"to", user.Email,
			"amount", tr.amount,
		)
	}

	topUps := []struct {
		amount    int64
		desc      string
		dayOffset int
	}{
		{5000, "Bank deposit", 0},
		{10000, "Stripe top-up", 11},
		{7500, "Bank transfer", 30},
	}

	for _, tu := range topUps {
		tx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
			WalletID:     wallet.ID,
			Amount:        tu.amount,
			Type:          repo.TransactionTypeCredit,
			Status:        repo.TransactionStatusCompleted,
			Description:   pgtype.Text{String: tu.desc, Valid: true},
		})
		if err != nil {
			panic(err)
		}
		totalIncoming += tu.amount

		logger.Debug("Created top-up", "amount", tu.amount, "desc", tu.desc)
		_ = tx
	}

	refundTx, err := queries.CreateTransaction(ctx, repo.CreateTransactionParams{
		WalletID:    wallet.ID,
		Amount:      2500,
		Type:        repo.TransactionTypeCredit,
		Status:      repo.TransactionStatusReversed,
		Description: pgtype.Text{String: "Amazon refund (reversed)", Valid: true},
	})
	if err != nil {
		panic(err)
	}
	totalIncoming += 2500
	_ = refundTx
	logger.Debug("Created reversed refund", "amount", 2500)

	finalBalance := wallet.Balance - totalOutgoing + totalIncoming

	logger.Info("Final wallet balance", "balance", finalBalance,
		"initial", wallet.Balance,
		"outgoing", totalOutgoing,
		"incoming", totalIncoming)
	logger.Info("Seed complete!", "email", targetEmail,
		"total_transfers", len(outgoingTransfers)+len(incomingTransfers),
		"top_ups", len(topUps),
		"scheduled", 1,
		"failed", 2)
}

	