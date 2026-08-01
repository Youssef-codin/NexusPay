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

func createUser(ctx context.Context, q *repo.Queries, email, name string) uuid.UUID {
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
	ids := map[string]uuid.UUID{}

	people := []struct {
		email   string
		name    string
		balance int64
	}{
		{"user@gmail.com", "Youssef", 50000},
		{"rich@gmail.com", "Rich Guy", 100000000},
		{"netflix@email.com", "Netflix", 0},
		{"spotify@email.com", "Spotify", 0},
		{"amazon@email.com", "Amazon", 0},
		{"uber@email.com", "Uber", 0},
		{"electricity@provider.com", "Electricity Provider", 0},
		{"water@provider.com", "Water Provider", 0},
		{"landlord@email.com", "Landlord", 0},
		{"grocery@store.com", "Grocery Store", 0},
		{"restaurant@email.com", "Restaurant", 0},
		{"salary@company.com", "Employer Inc", 10000000},
		{"friend@email.com", "Ahmed Friend", 10000},
		{"ali@email.com", "Ali", 15000},
		{"mohamed@email.com", "Mohamed", 20000},
		{"john.smith@email.com", "John Smith", 5000},
		{"jane.doe@email.com", "Jane Doe", 7500},
		{"michael.johnson@email.com", "Michael Johnson", 3000},
		{"sarah.williams@email.com", "Sarah Williams", 6000},
		{"david.brown@email.com", "David Brown", 8000},
		{"emily.davis@email.com", "Emily Davis", 4500},
		{"james.wilson@email.com", "James Wilson", 9000},
		{"maria.garcia@email.com", "Maria Garcia", 5500},
		{"robert.miller@email.com", "Robert Miller", 12000},
		{"linda.martinez@email.com", "Linda Martinez", 4000},
	}

	for _, p := range people {
		ids[p.email] = createUser(ctx, q, p.email, p.name)
		if p.balance > 0 {
			topUp(ctx, q, ids[p.email], p.balance)
		}
	}
	logger.Info("Created users", "count", len(people))

	// Completed history for user@gmail.com, spread across the categories so the
	// spending breakdown has something to show.
	outgoing := []struct {
		to     string
		amount int64
		note   string
		cat    repo.ExpenseCategory
	}{
		{"netflix@email.com", 499, "subscription", repo.ExpenseCategoryBills},
		{"spotify@email.com", 199, "subscription", repo.ExpenseCategoryBills},
		{"amazon@email.com", 2500, "shopping", repo.ExpenseCategoryShopping},
		{"uber@email.com", 350, "ride", repo.ExpenseCategoryTransport},
		{"electricity@provider.com", 1200, "utilities", repo.ExpenseCategoryBills},
		{"water@provider.com", 400, "utilities", repo.ExpenseCategoryBills},
		{"landlord@email.com", 8000, "rent", repo.ExpenseCategoryBills},
		{"grocery@store.com", 1800, "groceries", repo.ExpenseCategoryShopping},
		{"restaurant@email.com", 750, "dinner", repo.ExpenseCategoryFood},
		{"friend@email.com", 500, "reimbursement", repo.ExpenseCategoryOther},
	}
	for _, tr := range outgoing {
		move(ctx, q, ids["user@gmail.com"], ids[tr.to], tr.amount, tr.note, tr.cat,
			repo.TransactionStatusCompleted, time.Now())
	}

	incoming := []struct {
		from   string
		amount int64
		note   string
	}{
		{"salary@company.com", 25000, "monthly salary"},
		{"friend@email.com", 500, "reimbursement"},
		{"ali@email.com", 2000, "dinner"},
		{"mohamed@email.com", 5000, "group trip"},
	}
	for _, tr := range incoming {
		move(ctx, q, ids[tr.from], ids["user@gmail.com"], tr.amount, tr.note,
			repo.ExpenseCategoryIncome, repo.TransactionStatusCompleted, time.Now())
	}

	// A couple of failed rows: inserted, never balanced.
	failed := []struct {
		to     string
		amount int64
		note   string
	}{
		{"electricity@provider.com", 1200, "utilities"},
		{"amazon@email.com", 3500, "shopping"},
	}
	for _, tr := range failed {
		move(ctx, q, ids["user@gmail.com"], ids[tr.to], tr.amount, tr.note,
			repo.ExpenseCategoryBills, repo.TransactionStatusFailed, time.Now())
	}

	// Future-dated rows for the scheduler to pick up.
	scheduled := []struct {
		from   string
		to     string
		amount int64
		note   string
		cat    repo.ExpenseCategory
		days   int
	}{
		{"user@gmail.com", "landlord@email.com", 8000, "rent", repo.ExpenseCategoryBills, 3},
		{"rich@gmail.com", "friend@email.com", 100000, "monthly allowance", repo.ExpenseCategoryOther, 7},
		{"rich@gmail.com", "landlord@email.com", 50000, "rent", repo.ExpenseCategoryBills, 14},
		{"rich@gmail.com", "grocery@store.com", 25000, "groceries", repo.ExpenseCategoryShopping, 21},
		{"rich@gmail.com", "netflix@email.com", 999, "subscription", repo.ExpenseCategoryBills, 30},
	}
	for _, tr := range scheduled {
		move(ctx, q, ids[tr.from], ids[tr.to], tr.amount, tr.note, tr.cat,
			repo.TransactionStatusScheduled, time.Now().AddDate(0, 0, tr.days))
	}

	// Peer-to-peer noise so search and history have breadth.
	peers := []struct {
		from   string
		to     string
		amount int64
		note   string
		cat    repo.ExpenseCategory
	}{
		{"john.smith@email.com", "jane.doe@email.com", 500, "dinner", repo.ExpenseCategoryFood},
		{"sarah.williams@email.com", "michael.johnson@email.com", 1000, "lend", repo.ExpenseCategoryOther},
		{"david.brown@email.com", "emily.davis@email.com", 750, "coffee", repo.ExpenseCategoryFood},
		{"james.wilson@email.com", "maria.garcia@email.com", 2000, "rent", repo.ExpenseCategoryBills},
		{"robert.miller@email.com", "linda.martinez@email.com", 1500, "group expense", repo.ExpenseCategoryOther},
		{"jane.doe@email.com", "john.smith@email.com", 300, "refund", repo.ExpenseCategoryOther},
	}
	for _, tr := range peers {
		move(ctx, q, ids[tr.from], ids[tr.to], tr.amount, tr.note, tr.cat,
			repo.TransactionStatusCompleted, time.Now())
	}

	logger.Info("Seed complete!",
		"users", len(people),
		"completed", len(outgoing)+len(incoming)+len(peers),
		"failed", len(failed),
		"scheduled", len(scheduled))
}
