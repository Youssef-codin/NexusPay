# NexusPay two-table rewrite — implementation plan

## 1. Why

NexusPay spreads one money movement across six tables (`users`, `wallets`,
`transfers`, `transactions`, `scheduled_transfers`) and five services. Every
money-safety hole in `docs/PRD.md` §5 comes from that spread — the same logical
payment has three different "did this already happen?" mechanisms, and all three
are broken:

- **MS-1** — `HandlePaymentSucceeded` (`internal/payment/stripe/webhook.go:60`)
  reads the transaction status and rejects `processing`/`completed`, but the read
  is not what claims the row. Two concurrent Stripe deliveries both read
  `pending`, both credit. Double credit.
- **MS-2** — `UpdateTransferStatus` is `SET status=$2 WHERE id=$1` with no guard
  (`queries/transfers.sql`), and `ExecuteTransfer` never re-checks status inside
  its own tx (`internal/transfers/service.go:206`). A second call debits again.
- **MS-3** — `GetPendingScheduledTransfers` selects with no locking and
  `processOneTransfer` does check → execute → mark as three separate unlocked
  steps (`internal/transfers/scheduler.go:107`). Overlapping ticks pay twice.

The rewrite collapses the schema to **two tables** and replaces all three
mechanisms with **one**: a status update guarded on the current status, in the
same DB transaction as the two balance updates. The row lock serializes
concurrent callers; the status guard tells the loser the work is already done.

Stripe becomes a system user, so a top-up is an ordinary transaction and **every
row nets to zero** — which makes `SUM(balance) = 0` a global invariant every test
can assert.

## 2. Decisions already made

| Question | Decision |
|---|---|
| Expense categories | Two columns, `sender_category` / `receiver_category`, Postgres enum |
| Package collapse | `users` + `transactions`, plus `auth` and `payment/stripe`; `wallet` and `transfers` deleted |
| Migration | `00001_init.sql` rewritten in place — fresh DB, no data migration |
| Immediate transfer | Inserted as `crediting` and **committed** before the money moves, so a crash leaves a row the sweeper can finish |
| Transaction control | Go-side via `db.TxManager` only — no `BEGIN`/`COMMIT` in any `.sql` file |
| Unit test target | ~60% coverage, not exhaustive; integration tests carry the money-safety guarantees |
| Delivery | Three checkpoints, commit + review pause at each, PR at the end |

## 3. Repo orientation

```
cmd/app.go                     route table + dependency wiring (start here)
cmd/main.go                    config, pools, boot
cmd/seed/, cmd/seed_simple/    seed programs, both rewritten (they get shorter)
internal/db/db.go              TxManager: StartTx -> ctx carrying pgx.Tx; GetDBTX(ctx)
internal/db/postgresql/
  migrations/00001_init.sql    the single migration, rewritten in place
  sqlc/queries/*.sql           hand-written queries; `just sqlc-gen` regenerates
internal/{auth,users,wallet,transfers,transactions}/
                               each: types.go / repo.go / service.go / handler.go
internal/payment/stripe/       PaymentIntent creation + webhook
internal/utils/{api,validator,env}/
tests/integration/service_test.go   testcontainers + Stripe CLI harness (962 lines)
docs/PRD.md                    money-safety requirements MS-1..MS-7, test cases CT-1..CT-7
```

Conventions to match: `IService` interface + unexported `Service` struct +
`NewService(...) IService`; package-level `Err*` sentinels mapped to HTTP codes in
`handler.go` via `api.WrappedError`; repos are thin passthroughs over
`db.GetDBTX(ctx)`; DTOs in `types.go` with `json` + `validate` tags; unit tests use
testify mocks.

Tooling present: `sqlc` v1.31.1, `goose`, `docker` (daemon up), `stripe` CLI,
`just`, `air`. **`golangci-lint` is not installed** — use `go vet` for the lint
gate.

## 4. Schema — `internal/db/postgresql/migrations/00001_init.sql`

```sql
CREATE TYPE transaction_status AS ENUM
    ('awaiting_payment', 'crediting', 'scheduled', 'completed', 'failed');

CREATE TYPE expense_category AS ENUM
    ('food','transport','bills','shopping','income','topup','other');

CREATE TABLE users (
    id, email, password, full_name, refresh_token, token_expires_at,
    created_at, updated_at, deleted_at,          -- unchanged from today
    balance   BIGINT  NOT NULL DEFAULT 0,
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    CONSTRAINT balance_non_negative CHECK (balance >= 0 OR is_system)
);

CREATE TABLE transactions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_id         UUID NOT NULL REFERENCES users(id),
    receiver_id       UUID NOT NULL REFERENCES users(id),
    amount            BIGINT NOT NULL,
    status            transaction_status NOT NULL,
    note              TEXT,
    sender_category   expense_category,
    receiver_category expense_category,
    scheduled_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT sender_ne_receiver CHECK (sender_id <> receiver_id),
    CONSTRAINT amount_positive    CHECK (amount > 0)
);
```

- `CHECK (balance >= 0 OR is_system)` **is** the "Stripe is a clearing account"
  rule — row-level, no cross-table logic. Stripe's balance goes negative by
  design.
- Keep `note` (the current API exposes it) and the `pg_trgm` name indexes.
- Partial indexes for the two workers: `(scheduled_at) WHERE status='scheduled'`
  and `(updated_at) WHERE status='crediting'`.
- `set_updated_at()` trigger on both tables — the sweeper's five-minute threshold
  reads `updated_at`, so entering `crediting` must stamp it.
- Seed the Stripe system user in the migration at a fixed UUID
  (`00000000-0000-0000-0000-000000000001`, `is_system = TRUE`, empty password so
  no bcrypt compare can ever succeed), exposed in Go as `users.SystemStripeID`.

## 5. The one function that moves money

All transaction control is Go-side through `db.TxManager` — `StartTx` /
`tx.Commit(txCtx)` / `defer tx.Rollback(txCtx)`, exactly as
`internal/transfers/service.go:226` does today. Every sqlc query stays a single
statement; repos call `db.GetDBTX(ctx)` and pick the tx up from context.

```go
// complete is the whole rewrite. The webhook, immediate transfers, the
// scheduler and the sweeper all call this, and nothing else moves money.
func (svc *Service) complete(
    ctx  context.Context,
    id   uuid.UUID,
    from repo.TransactionStatus,
) (repo.Transaction, error) {
    txCtx, tx, err := svc.txManager.StartTx(ctx)
    if err != nil {
        return repo.Transaction{}, err
    }
    defer tx.Rollback(txCtx)

    // Guarded status update first: it takes the row lock that serializes
    // concurrent callers. Zero rows (pgx.ErrNoRows) means another worker got
    // here first -- return early WITHOUT committing; nothing has moved.
    t, err := svc.repo.GuardedSetStatus(txCtx, GuardedSetStatusParams{
        ID: id, From: from, To: repo.TransactionStatusCompleted,
    })
    if errors.Is(err, pgx.ErrNoRows) {
        return repo.Transaction{}, ErrAlreadyProcessed
    }
    if err != nil {
        return repo.Transaction{}, err
    }

    // Both balance updates, applied in ascending user-id order so simultaneous
    // A->B and B->A transfers cannot deadlock on each other's row locks.
    if err := svc.moveBalance(txCtx, t); err != nil {
        return repo.Transaction{}, err
    }

    return t, tx.Commit(txCtx)
}
```

`moveBalance` runs two single-statement updates:

```sql
-- DebitUser: zero rows => insufficient funds. The system-account carve-out
-- lives here, so callers never branch on it.
UPDATE users SET balance = balance - $2
 WHERE id = $1 AND (balance >= $2 OR is_system) AND deleted_at IS NULL
RETURNING *;

-- CreditUser
UPDATE users SET balance = balance + $2
 WHERE id = $1 AND deleted_at IS NULL
RETURNING *;
```

Rules that follow from this and must not be softened:

- Sufficient funds are enforced **only** by `(balance >= $2 OR is_system)` inside
  the transaction. A read outside the transaction protects nothing and must never
  be load-bearing — a pre-read is a UX nicety at most.
- Zero rows on the debit ⇒ `ErrInsufficientFunds`, the tx rolls back (nothing
  moved), then a separate guarded update takes the row to `failed`.
- `ErrAlreadyProcessed` is **success** for every caller. That single fact is what
  makes the webhook idempotent, `complete` idempotent, and scheduled transfers
  claim-once — three broken mechanisms replaced by one.

## 6. Lifecycles

| Flow | Statuses |
|---|---|
| Top-up | `awaiting_payment` → `crediting` → `completed` (or → `failed`) |
| Immediate transfer | insert `crediting` (**committed**) → `completed` / `failed` |
| Scheduled transfer | insert `scheduled` → `completed` / `failed`; cancel deletes the row |

**Webhook** (`payment_intent.succeeded`) is two transactions on purpose: tx1
claims `awaiting_payment → crediting` and commits; tx2 is `complete()`. So a row
sitting in `crediting` means the credit definitively did not commit. Zero rows on
the claim ⇒ return nil ⇒ **2xx**, so Stripe stops retrying.
`payment_intent.payment_failed` / `.canceled` are a single guarded
`awaiting_payment → failed` and never touch a balance.

**Immediate transfer** has the same shape: insert `crediting`, commit, then
`complete()`. Crash in between and the sweeper finishes it — an accepted transfer
is never silently lost.

**Sweeper** (every minute): claims `status='crediting' AND updated_at < NOW() -
INTERVAL '5 minutes'` with `FOR UPDATE SKIP LOCKED` and calls the same
`complete()`. A live worker that beat it simply yields zero rows.

**Scheduler** (every minute): claims `status='scheduled' AND scheduled_at <=
NOW()` with `FOR UPDATE SKIP LOCKED LIMIT n` — Postgres is the queue, no Redis —
then calls `complete(id, 'scheduled')` per row, each in its own `TxManager`
transaction. Keeping the claim and the execution in separate transactions is what
lets both workers reuse one unmodified `complete()`; overlapping ticks are
harmless because the loser's guarded update matches zero rows. Sequential within
a tick, which drops the goroutine pool in today's `scheduler.go`. Both ticks are
exported so they are testable outside cron: `RunOnce(ctx) error` and
`SweepOnce(ctx) error`.

**Cancel** is one statement:

```sql
DELETE FROM transactions
 WHERE id = $1 AND sender_id = $2 AND status = 'scheduled'
RETURNING *;
```

It races the scheduler correctly: the delete blocks on the scheduler's row lock,
then affects zero rows. Whichever lands first wins. A follow-up read only
classifies the error (404 vs 409) and is not load-bearing.

## 7. Packages

```
internal/
  auth/            unchanged shape; drops the wallet.IService dependency
                   (balance now starts at 0 on the users row itself)
  users/           users table: search, profile + balance, debit/credit
  transactions/    transactions table: transfers, top-ups, scheduler, sweeper
  payment/stripe/  PaymentIntent (unchanged) + webhook, now guarded
  db/              TxManager / GetDBTX — unchanged, already the right plumbing
```

Deleted: `internal/wallet/`, `internal/transfers/`, and the four dead Redis caches
(`redisDb/{wallets,transfers,transactions,scheduledTransfers}Cache.go` — only
`usersCache.go` is wired into anything). `internal/transactions/` is rewritten
from scratch at the same path. Keep the uncommitted `Refund`/`CancelPayment`
removal in `internal/payment/` — it is consistent with this change.

`transactions.Service` depends on `users.IService` for the balance updates — the
same service→service shape `transfers` → `wallet` used before.

## 8. API

Wallets are gone, so `wallet_id` becomes `user_id` throughout.

```
POST   /auth/register|login|refresh     unchanged
GET    /users/test, POST /users/logout  unchanged
GET    /users                           search by name (system users hidden)
GET    /users/me                        profile + balance     (was GET /wallet/{userId})
POST   /transactions                    transfer; scheduled_at => scheduled
GET    /transactions                    mine, both directions, with counterparty
GET    /transactions/{id}
DELETE /transactions/{id}               cancel while still scheduled
PATCH  /transactions/{id}/category      set my side's expense category
POST   /transactions/topup              creates the PaymentIntent  (was PATCH /wallet)
POST   /webhook/stripe                  unchanged path
```

`PATCH /transactions/{id}/category` exists because the receiver otherwise has no
way to set `receiver_category` and the column would be dead weight; the service
picks the column from which side the caller is on. Minimum top-up stays 1000
piastres. The webhook handler starts returning 500 on genuine errors (so Stripe
retries) while keeping 2xx for the already-done case — today it swallows every
error as 200.

---

## Checkpoint 1 — schema, API surface, skeletons

Everything structural, service bodies stubbed as `ErrNotImplemented`. Goal: the
schema, the routes and every signature are reviewable before any logic exists.

1. Rewrite `00001_init.sql`. Rewrite `queries/users.sql` and
   `queries/transactions.sql`; delete
   `queries/{wallets,transfers,scheduled_transfers}.sql`; run `just sqlc-gen`.
   Queries: `CreateTransaction`, `GetTransactionById`, `GetTransactionsByUserId`
   (joined to both users for counterparty names), `GuardedSetStatus(id, from, to)`,
   `ClaimDueTransactions` / `ClaimStuckCrediting` (both `FOR UPDATE SKIP LOCKED
   LIMIT`), `CancelScheduledTransaction`, `SetSenderCategory` /
   `SetReceiverCategory`, `DebitUser` / `CreditUser`, `GetBalance`, plus the
   existing auth/user queries with `is_system` excluded from search. All
   `:one`/`:many` so zero rows surface as `pgx.ErrNoRows`.
2. Delete `internal/wallet/`, `internal/transfers/`, the four dead Redis caches,
   and the unit tests belonging to them.
3. New `internal/transactions/`: `types.go` (real DTOs), `repo.go` (real — thin
   `db.GetDBTX(ctx)` passthroughs), `service.go` (`IService` + stub bodies),
   `handler.go` (real — parsing and error mapping), `scheduler.go` (struct,
   `Start`/`Stop` carried over from today's cron wiring, `RunOnce`/`SweepOnce`
   stubs).
4. `internal/users/` gains balance, `GetMe`, `Debit`/`Credit`; `internal/auth/`
   drops the wallet dependency; `validator.go` swaps `transaction_type` /
   `transfer_status` for the new status + category validators.
5. `cmd/app.go` rewired to the new routes; both seed programs updated.
6. `tests/integration/service_test.go` harness rewired to the new services so the
   package compiles; old wallet/transfer test bodies removed (new ones land in
   checkpoint 2).
7. Bruno + spec: `docs/bruno/NexusPay/apispec/api.yaml`, `Wallet/` folder → top-up
   under `Transactions/`, `Transfers/` → `Transactions/`, new requests for
   `/users/me` and the category PATCH.

**Gate:** `go build ./...`, `go vet ./...`, `just migrate-up` on a fresh DB,
`just seed`, `just run` boots and serves the new routes. Commit, pause for review.

## Checkpoint 2 — integration tests for the race conditions

Written against the skeletons, so they are **expected to fail**; the check is that
each fails on `ErrNotImplemented` or a wrong balance, never on a compile error.
Reuses the existing testcontainers + Stripe CLI harness. All run with `-race`.

- **CT-1** duplicate `payment_intent.succeeded`, sequential *and* N-concurrent,
  credits exactly once — the double-payment case, highest priority
- **CT-2** `complete()` twice on one transfer debits once
- **CT-3** two concurrent `RunOnce` on one due transfer execute it once
- **CT-4** K concurrent transfers each equal to the full balance: at most one
  succeeds, balance ≥ 0 (extends `TestConcurrentTransfers_ExceedBalance_Integration`)
- **CT-5** `POST /transactions/topup` does not change the balance
- **CT-7** concurrent self-transfers all rejected, balance unchanged
- sweeper: a row parked in `crediting` is finished by `SweepOnce`, exactly once
- cancel racing `RunOnce`: either the row is gone or it completed — never both
  effects, never a double debit
- `TestConcurrentTransfers_Integration` ported, not dropped
- every test asserts the global invariant `SELECT SUM(balance) FROM users = 0` as
  a post-condition

**Gate:** the suite compiles and every new test fails for the documented reason.
Commit, pause for review.

## Checkpoint 3 — implementation + PR

1. Fill in `transactions.Service` (`complete`, `moveBalance` with id-ordered
   updates, create / schedule / cancel / category / top-up), `scheduler.go`
   (`RunOnce`, `SweepOnce`), and the guarded `payment/stripe/webhook.go` plus
   handler status codes.
2. Unit tests — **target ~60% coverage, not exhaustive**; the integration suite
   carries the money-safety guarantees. Follow the existing testify-mock style
   (`internal/transfers/service_test.go` is the template: `MockTxManager`,
   `MockTx`, `withUserID`). Priority order: zero-row guard ⇒ no-op not error;
   insufficient funds ⇒ rollback then `failed`; cancel of a non-scheduled row;
   top-up never credits at request time; `RunOnce`/`SweepOnce` against a mocked
   repo. Check with `go test ./internal/... -cover`.
3. Docs: `docs/PRD.md` §4–§6 restated against the two-table model, each "current
   gap" replaced by how the rewrite closes that MS-n; `CLAUDE.md` + `AGENTS.md`
   gain a short guarded-update section beside the TxManager note; `TODO.md` ticks
   "Rewrite"; delete this file or fold it into the PR body.
4. Everything green, then push `rewrite` and open the PR against `main` with
   `gh pr create` — body summarising the schema collapse, the single
   guarded-update pattern, and which MS-n each test guards.

**Gate:**

```bash
just sqlc-gen && go build ./... && go vet ./...   # golangci-lint is not installed
just test-unit
go test ./internal/... -cover                     # ~60% target
go test ./... -tags=integration -race -run '_Integration$' -p 1
```

Needs docker running, `stripe login`, and `STRIPE_SECRET_KEY` set.

Manual smoke via `just run`: register two users, `POST /transactions/topup`,
confirm the PaymentIntent with the Stripe CLI, watch the balance move only after
the webhook, transfer between the two, then re-POST the same signed webhook
payload and confirm the balance does not move again.

---

Out of scope, left in `TODO.md`: `Idempotency-Key` on money-moving POSTs (MS-7 /
CT-6), pagination, AI analytics, login caching, Paymob.
