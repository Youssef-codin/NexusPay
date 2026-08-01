# AGENTS.md

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
go test ./internal/transactions/... -run TestComplete_ZeroRowGuard_IsNoOp -v
```

### Transactions / database

`db.TxManager` (`internal/db/db.go`) starts a pgx transaction and stores it in context via `db.NewTxContext`. Repositories call `db.GetDBTX(ctx)` — if a transaction is in context it uses that, otherwise it uses the pool. This means transactional code just passes `txCtx` through; repos don't need to know.

### Money movement: one guarded update

There are exactly two tables, `users` and `transactions`, and exactly one
function that moves a balance: `transactions.Service.Complete`. The webhook,
immediate transfers, the scheduler and the sweeper all call it and nothing else
touches a balance.

`Complete` opens a transaction and issues the status guard *first*:

```sql
UPDATE transactions SET status = @to_status
 WHERE id = @id AND status = @from_status
RETURNING *;
```

That single statement is the whole concurrency design. It claims the row and
takes the row lock that serializes concurrent callers, so both balance updates
ride in the same transaction behind it. Zero rows come back as `pgx.ErrNoRows`,
which becomes `ErrAlreadyProcessed`.

Rules that must not be softened:

- **`ErrAlreadyProcessed` is success for every caller.** A duplicate Stripe
  delivery, an overlapping scheduler tick and a sweeper racing a live worker all
  end here, and all of them return 2xx / nil. Never map it to an error status.
- **Sufficient funds are enforced only by `(balance >= @amount OR is_system)`
  inside the transaction**, in `DebitUser`. A balance read outside the
  transaction protects nothing and must never be load-bearing.
- **Balance updates go in ascending user-id order** (`moveBalance`). Simultaneous
  A->B and B->A transfers would otherwise grab each other's row locks in opposite
  orders and deadlock.
- **Stripe is a system user** (`users.SystemStripeID`), so a top-up is an ordinary
  transaction and every row nets to zero. `SUM(balance) = 0` is a global
  invariant; the integration suite asserts it after every test.
- **`set_updated_at` on `transactions` is load-bearing.** The sweeper finds
  abandoned work with `updated_at < NOW() - INTERVAL '5 minutes'`, and the guarded
  update only writes `status` — without the trigger every fresh `crediting` row
  looks stale and live work gets stolen.

Both workers are exported as `RunOnce(ctx)` / `SweepOnce(ctx)` so they are
testable outside cron.

### Testing conventions

Integration tests are in `tests/integration/` and require real Docker-backed Postgres and Redis (via testcontainers). They are gated by the `integration` build tag and should be run with `-race`.

Only the two tests that create a real PaymentIntent need Stripe; they call `requireStripe(t)` and skip when the CLI or key is unusable. Everything else calls `setupOffline()`, which builds the app with a fixed webhook signing secret so the test can sign its own events. Those tests fund users with `seedBalance` — one transaction row from the Stripe system user plus both balance updates in a single database transaction, which is what keeps `SUM(balance) = 0` true even though the service was bypassed.

Synthesised webhook events must carry `stripe.APIVersion`; `webhook.ConstructEvent` rejects a mismatch and the handler reports that as `"Invalid webhook signature"`.
