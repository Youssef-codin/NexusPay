# CLAUDE.md

## Commands

```bash
just run              # start dev server with hot reload (starts Docker deps first)
just build            
just test             
just test-unit        # run unit tests only (no integration tag)
just test-integration # run integration tests (requires `stripe listen --forward-to localhost:3000/webhook/stripe`)
just lint             
just fmt             
just migrate-up       # run goose migrations
just migrate-create NAME=foo  # create new migration
just sqlc-gen         # regenerate sqlc types after SQL changes
just seed             # seed the database
```

Run a single test:
```bash
go test ./internal/wallet/... -run TestGetByUserId -v
```

### Transactions / database

`db.TxManager` (`internal/db/db.go`) starts a pgx transaction and stores it in context via `db.NewTxContext`. Repositories call `db.GetDBTX(ctx)` — if a transaction is in context it uses that, otherwise it uses the pool. This means transactional code just passes `txCtx` through; repos don't need to know.

### Testing conventions

Integration tests are in `tests/integration/` and require real Docker-backed Postgres and Redis (via testcontainers) plus a running Stripe CLI webhook forwarder. They are gated by the `integration` build tag.
