package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Youssef-codin/NexusPay/internal/utils/env"
	"github.com/jackc/pgx/v5"
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
	}

	redisOpt, err := redis.ParseURL(cfg.redis.dsn)
	if err != nil {
		panic(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	conn, err := pgx.Connect(ctx, cfg.db.dsn)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	logger.Info("Connected to db", "dsn", cfg.db.dsn)

	rdb := redis.NewClient(redisOpt)
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Error("Connection to redis FAILED", "dsn", cfg.redis.dsn)
	}

	logger.Info("Connected to redis", "dsn", cfg.redis.dsn)

	api := application{
		config:    cfg,
		db:        conn,
		redis:     rdb,
		redisOpts: redisOpt,
	}

	handler := api.mount()
	if err := api.run(handler); err != nil {
		slog.Error("Server has failed to start, err", "err", err)
		os.Exit(1)
	}
}
