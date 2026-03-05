package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Youssef-codin/NexusPay/internal/utils/env"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()

	cfg := config{
		addr: ":3000",
		db: dbConfig{
			dsn: env.GetEnvVar(
				"GOOSE_DBSTRING",
				"host=localhost user=joe-arch password=password port=5433 dbname=todo sslmode=disable",
			),
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	conn, err := pgx.Connect(ctx, cfg.db.dsn)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	logger.Info("Connected to db", "dsn", cfg.db.dsn)

	api := application{
		config: cfg,
		db:     conn,
	}

	handler := api.mount()
	if err := api.run(handler); err != nil {
		slog.Error("Server has failed to start, err: %s", err)
		os.Exit(1)
	}
}
