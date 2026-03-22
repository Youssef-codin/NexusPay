# NexusPay — Project Spec

## Overview

A digital wallet API built in Go, inspired by Telda. Supports user authentication,
Stripe-powered top-ups, user-to-user transfers, transaction history, and scheduled
transfers. EGP only (stored in piastres).

---

## Tech Stack

| Layer            | Choice     |
| ---------------- | ---------- |
| Language         | Go         |
| Router           | Chi        |
| Database         | PostgreSQL |
| Cache            | Redis      |
| Migrations       | Goose      |
| Query Generation | sqlc       |
| Payments         | Stripe     |
| Docs             | Swaggo     |

## Libraries

- `go-chi/chi` — router
- `go-chi/httprate` — rate limiting
- `golang-jwt/jwt` — auth
- `sqlc-dev/sqlc` — query generation
- `pressly/goose` — migrations
- `swaggo/swag` — swagger
- `redis/go-redis` — Redis client
- `joho/godotenv` — config
- `go-playground/validator` — request validation
- `stripe/stripe-go` — Stripe SDK

## Project Structure

```
Nexus/
├── cmd/                    # Application entrypoints
│   ├── main.go            # Main entrypoint
│   └── app.go             # App initialization & routing
├── internal/              # Private application code
│   ├── db/
│   │   ├── postgresql/    # PostgreSQL related
│   │   │   ├── migrations/   # DB migrations (Goose)
│   │   │   └── sqlc/      # Generated SQL queries
│   │   │       └── queries/  # SQL query files
│   │   └── redisDb/       # Redis caching layer
│   ├── security/          # Auth, JWT, password hashing
│   ├── users/             # User handlers, services, types
│   └── utils/             # Utilities
│       ├── api/           # API helpers
│       └── env/           # Environment config
├── docs/                  # Swagger documentation
├── compose.yaml           # Docker Compose config
├── Dockerfile             # Container image
├── go.mod / go.sum        # Dependencies
├── sqlc.yaml              # sqlc config
├── justfile               # Task runner
└── .air.toml              # Hot reload config
```

---

## Database Schema

```sql
CREATE TYPE transaction_type AS ENUM ('debit', 'credit');
CREATE TYPE transaction_status AS ENUM ('pending', 'processing', 'completed', 'failed', 'reversed');
CREATE TYPE transfer_status AS ENUM ('pending', 'completed', 'failed');

CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               TEXT NOT NULL UNIQUE,
    password            TEXT NOT NULL,
    full_name           TEXT NOT NULL,
    refresh_token       TEXT,
    token_expires_at    TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ
);

CREATE TABLE wallets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL UNIQUE REFERENCES users(id),
    balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE TABLE transactions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id       UUID NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    type            transaction_type NOT NULL,
    status          transaction_status NOT NULL DEFAULT 'pending',
    transfer_id     UUID REFERENCES transfers(id) DEFERRABLE INITIALLY DEFERRED,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE TABLE transfers (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id          UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id            UUID NOT NULL REFERENCES wallets(id),
    amount                  BIGINT NOT NULL CHECK (amount > 0),
    status                  transfer_status NOT NULL DEFAULT 'pending',
    note                    TEXT,
    debit_transaction_id    UUID UNIQUE REFERENCES transactions(id) DEFERRABLE INITIALLY DEFERRED,
    credit_transaction_id   UUID UNIQUE REFERENCES transactions(id) DEFERRABLE INITIALLY DEFERRED,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);

CREATE TABLE scheduled_transfers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id UUID NOT NULL UNIQUE REFERENCES transfers(id),
    scheduled_at TIMESTAMPTZ NOT NULL,
    executed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
NEW.updated_at = NOW();
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_set_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER wallets_set_updated_at
BEFORE UPDATE ON wallets
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER transfers_set_updated_at
BEFORE UPDATE ON transfers
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER scheduled_transfers_set_updated_at
BEFORE UPDATE ON scheduled_transfers
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
```

---

## Endpoints

### Auth

| Method | Endpoint         | Auth | Description                  |
| ------ | ---------------- | ---- | ---------------------------- |
| POST   | `/auth/register` | ❌   | Register a new user          |
| POST   | `/auth/login`    | ❌   | Login and get JWT            |
| POST   | `/auth/refresh`  | ❌   | Refresh JWT token            |
| PATCH  | `/auth/profile`  | ✅   | Update name, email, password |

### Wallet

| Method | Endpoint          | Auth | Description            |
| ------ | ----------------- | ---- | ---------------------- |
| GET    | `/wallet`         | ✅   | Get wallet + balance   |
| POST   | `/wallet/topup`   | ✅   | Initiate Stripe top-up |
| POST   | `/webhook/stripe` | ❌   | Stripe webhook handler |

### Transfers

| Method | Endpoint         | Auth | Description                   |
| ------ | ---------------- | ---- | ----------------------------- |
| POST   | `/transfers`     | ✅   | Send money to another user    |
| GET    | `/transfers`     | ✅   | Get sent and received history |
| GET    | `/transfers/:id` | ✅   | Get single transfer           |

### Scheduled Transfers

| Method | Endpoint         | Auth | Description                         |
| ------ | ---------------- | ---- | ----------------------------------- |
| POST   | `/scheduled`     | ✅   | Create a scheduled transfer         |
| GET    | `/scheduled`     | ✅   | List scheduled transfers            |
| GET    | `/scheduled/:id` | ✅   | Get single scheduled transfer       |
| PATCH  | `/scheduled/:id` | ✅   | Update a pending scheduled transfer |
| DELETE | `/scheduled/:id` | ✅   | Cancel a scheduled transfer         |

### Users

| Method | Endpoint           | Auth | Description                   |
| ------ | ------------------ | ---- | ----------------------------- |
| GET    | `/users/search?q=` | ✅   | Search users by name or email |

---

## Redis

### Caching

| Key                          | Description            | Invalidated On    |
| ---------------------------- | ---------------------- | ----------------- |
| `wallet:balance:{wallet_id}` | User's current balance | Every transaction |
| `user:{user_id}`             | User profile           | Profile update    |

### Rate Limiting

| Endpoint        | Limit      | Per  |
| --------------- | ---------- | ---- |
| `/transfers`    | 10 req/min | User |
| `/wallet/topup` | 5 req/min  | User |
| `/auth/login`   | 5 req/min  | IP   |
| Everything else | 60 req/min | User |

---

## Transaction Types

- `debit` — money sent out
- `credit` — money received or topped up

## Transaction Statuses

- `pending` — initiated
- `processing` — being handled
- `completed` — successful
- `failed` — something went wrong
- `reversed` — rolled back

## Transfer Statuses

- `pending` — initiated
- `completed` — successful
- `failed` — something went wrong

## Scheduled Transfer Statuses

- `pending` — waiting for execution
- `processing` — cron picked it up
- `completed` — executed successfully
- `failed` — cron tried but failed
- `cancelled` — user cancelled

---

## Stripe Top-Up Flow

1. Client calls `POST /wallet/topup`
2. Server creates a Stripe Payment Intent
3. Transaction created with status `pending`
4. Stripe processes payment
5. Stripe fires webhook to `POST /webhook/stripe`
6. Server updates transaction to `completed` or `failed`
7. On `completed`, wallet balance is incremented

---

## Transfer Flow

1. Client calls `POST /transfers`
2. Server creates a `transfers` row with status `pending`, nullable transaction IDs
3. Server inserts debit transaction (`pending`) and credit transaction (`pending`)
4. Server updates the transfer with both transaction IDs
5. Execute balance changes within a DB transaction:
   - Debit sender's wallet
   - Credit receiver's wallet
   - Mark both transactions as `completed`
   - Mark transfer as `completed`
6. On any failure, mark transactions and transfer as `failed`

---

## Cron Job — Scheduled Transfers

Runs every minute:

1. Query `scheduled_transfers` where `status = pending` AND `scheduled_at <= NOW()`
2. Set status to `processing`
3. Execute transfer logic (same as regular transfer)
4. Set status to `completed` or `failed`

> Note: `processing` status acts as a crash recovery guard.
> Stuck `processing` records can be detected and retried or flagged.

---

## Notes

- All money is stored in **piastres** (1 EGP = 100 piastres) as BIGINT
- JWT auth will be replaced by shared auth service in Project 2
- Soft deletes used on `users`, `transactions`, `transfers`, `scheduled_transfers`
- Double-entry bookkeeping: every transfer creates two transaction records
- Webhook signature must be verified using `stripe.ConstructEvent`
- `/users/search` requires a minimum query length of 3 characters and returns only non-sensitive fields (id, full_name, email)
- Use `SELECT ... FOR UPDATE SKIP LOCKED` in the cron job to prevent concurrent duplicate execution across multiple instances
