package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Youssef-codin/NexusPay/internal/db"
	"github.com/Youssef-codin/NexusPay/internal/utils/env"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	cfg := config{
		addr: ":3000",
		db: dbConfig{
			dsn: env.GetEnvVar(
				"GOOSE_DBSTRING",
				"host=localhost user=joe-arch password=password port=5433 dbname=wrongdblol sslmode=disable",
			),
		},
		redis: dbConfig{
			dsn: env.GetEnvVar(
				"REDIS_URL",
				"redis://localhost:6379",
			),
		},
		secret: env.GetEnvVar("JWT_SECRET", "secretlol"),
		stripe: stripeConfig{
			apiKey:        env.GetEnvVar("STRIPE_SECRET_KEY", ""),
			webhookSecret: env.GetEnvVar("STRIPE_WEBHOOK_SECRET", ""),
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
	}))
	slog.SetDefault(logger)

	pool, err := pgxpool.New(ctx, cfg.db.dsn)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("Connection to db FAILED", "error", err)
		os.Exit(1)
	}

	logger.Info("Connected to db")

	database := db.New(pool)

	redisOpt, err := redis.ParseURL(cfg.redis.dsn)
	if err != nil {
		panic(err)
	}

	rdb := redis.NewClient(redisOpt)
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Error("Connection to redis FAILED", "dsn", cfg.redis.dsn)
	}

	logger.Info("Connected to redis", "dsn", cfg.redis.dsn)

	api := application{
		config:    cfg,
		db:        database,
		redis:     rdb,
		redisOpts: redisOpt,
	}

	handler := api.mount()
	if err := api.run(handler); err != nil {
		slog.Error("Server has failed to start, err", "err", err)
		os.Exit(1)
	}
}
