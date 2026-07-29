package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/Youssef-codin/NexusPay/internal/utils/env"
	"github.com/google/uuid"
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

	Seed(ctx, repo.New(pool), logger)
}

// ClearDB wipes everything except the Stripe system user, which the migration
// seeds and every top-up references.
func ClearDB(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	for _, stmt := range []string{
		"DELETE FROM transactions",
		"DELETE FROM users WHERE NOT is_system",
		"UPDATE users SET balance = 0 WHERE is_system",
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			logger.Warn("Failed to clear", "stmt", stmt, "error", err)
		} else {
			logger.Info("Cleared", "stmt", stmt)
		}
	}
}

func uid(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func text(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func category(c repo.ExpenseCategory) repo.NullExpenseCategory {
	return repo.NullExpenseCategory{ExpenseCategory: c, Valid: c != ""}
}

func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func createUser(
	ctx context.Context,
	q *repo.Queries,
	email, name string,
) uuid.UUID {
	hashed, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}

	user, err := q.CreateUser(ctx, repo.CreateUserParams{
		Email:    email,
		Password: string(hashed),
		FullName: name,
	})
	if err != nil {
		panic(err)
	}

	return uuid.UUID(user.ID.Bytes)
}

// topUp seeds a completed top-up: an ordinary transaction sent by the Stripe
// system user, so the row nets to zero like every other one.
func topUp(ctx context.Context, q *repo.Queries, userID uuid.UUID, amount int64) {
	move(ctx, q, users.SystemStripeID, userID, amount, "Top up", repo.ExpenseCategoryTopup,
		repo.TransactionStatusCompleted, time.Now())
}

// move inserts a transaction and, when it is completed, applies both balance
// updates so the seeded database satisfies SUM(balance) = 0.
func move(
	ctx context.Context,
	q *repo.Queries,
	from, to uuid.UUID,
	amount int64,
	note string,
	cat repo.ExpenseCategory,
	status repo.TransactionStatus,
	scheduledAt time.Time,
) uuid.UUID {
	t, err := q.CreateTransaction(ctx, repo.CreateTransactionParams{
		SenderID:       uid(from),
		ReceiverID:     uid(to),
		Amount:         amount,
		Status:         status,
		Note:           text(note),
		SenderCategory: category(cat),
		ScheduledAt:    stamp(scheduledAt),
	})
	if err != nil {
		panic(err)
	}

	if status == repo.TransactionStatusCompleted {
		if _, err := q.DebitUser(ctx, repo.DebitUserParams{ID: uid(from), Amount: amount}); err != nil {
			panic(err)
		}
		if _, err := q.CreditUser(ctx, repo.CreditUserParams{ID: uid(to), Amount: amount}); err != nil {
			panic(err)
		}
	}

	return uuid.UUID(t.ID.Bytes)
}

func Seed(ctx context.Context, q *repo.Queries, logger *slog.Logger) {
	user := createUser(ctx, q, "user@gmail.com", "User")
	topUp(ctx, q, user, 50000)
	logger.Info("Created user", "email", "user@gmail.com", "balance", 50000)

	rich := createUser(ctx, q, "rich@gmail.com", "Rich Guy")
	topUp(ctx, q, rich, 100000000)
	logger.Info("Created rich user", "email", "rich@gmail.com", "balance", 100000000)

	counterparties := map[string]uuid.UUID{}
	for _, c := range []struct{ email, name string }{
		{"friend@email.com", "Friend"},
		{"landlord@email.com", "Landlord"},
		{"grocery@store.com", "Grocery Store"},
		{"netflix@email.com", "Netflix"},
	} {
		counterparties[c.email] = createUser(ctx, q, c.email, c.name)
	}
	topUp(ctx, q, counterparties["friend@email.com"], 10000)

	completed := []struct {
		to     string
		amount int64
		note   string
		cat    repo.ExpenseCategory
	}{
		{"friend@email.com", 50000, "gift", repo.ExpenseCategoryOther},
		{"netflix@email.com", 999, "subscription", repo.ExpenseCategoryBills},
	}
	for _, tr := range completed {
		move(ctx, q, rich, counterparties[tr.to], tr.amount, tr.note, tr.cat,
			repo.TransactionStatusCompleted, time.Now())
		logger.Debug("Created completed transfer", "to", tr.to, "amount", tr.amount)
	}

	scheduled := []struct {
		to     string
		amount int64
		note   string
		cat    repo.ExpenseCategory
		days   int
	}{
		{"friend@email.com", 100000, "monthly allowance", repo.ExpenseCategoryOther, 7},
		{"landlord@email.com", 50000, "rent", repo.ExpenseCategoryBills, 14},
		{"grocery@store.com", 25000, "groceries", repo.ExpenseCategoryShopping, 21},
		{"netflix@email.com", 999, "subscription", repo.ExpenseCategoryBills, 30},
	}
	for _, tr := range scheduled {
		move(ctx, q, rich, counterparties[tr.to], tr.amount, tr.note, tr.cat,
			repo.TransactionStatusScheduled, time.Now().AddDate(0, 0, tr.days))
		logger.Debug("Created scheduled transfer", "to", tr.to, "in_days", tr.days)
	}

	logger.Info("Seed complete!",
		"user_email", "user@gmail.com",
		"rich_email", "rich@gmail.com",
		"scheduled", len(scheduled))
}
