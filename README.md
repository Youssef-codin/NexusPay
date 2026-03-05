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

---

## Database Schema

```sql
CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT NOT NULL UNIQUE,
    password    TEXT NOT NULL,
    full_name   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE TABLE wallets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL UNIQUE REFERENCES users(id),
    balance     BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transactions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id       UUID NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL,
    type            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    reference_id    UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE TABLE transfers (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id          UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id            UUID NOT NULL REFERENCES wallets(id),
    amount                  BIGINT NOT NULL,
    note                    TEXT,
    debit_transaction_id    UUID NOT NULL REFERENCES transactions(id),
    credit_transaction_id   UUID NOT NULL REFERENCES transactions(id),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);

CREATE TABLE scheduled_transfers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id  UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id    UUID NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL,
    note            TEXT,
    scheduled_at    TIMESTAMPTZ NOT NULL,
    executed_at     TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);
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
| DELETE | `/transfers/:id` | ✅   | Reverse/cancel a transfer     |

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

- `top_up` — Stripe added money to wallet
- `transfer_debit` — money sent out
- `transfer_credit` — money received
- `reversal` — transaction reversed

## Transaction Statuses

- `pending` — initiated
- `processing` — being handled
- `completed` — successful
- `failed` — something went wrong
- `reversed` — rolled back

## Scheduled Transfer Statuses

- `pending` — waiting for execution
- `processing` — cron picked it up
- `executed` — completed successfully
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

## Cron Job — Scheduled Transfers

Runs every minute:

1. Query `scheduled_transfers` where `status = pending` AND `scheduled_at <= NOW()`
2. Set status to `processing`
3. Execute transfer logic (same as regular transfer)
4. Set status to `executed` or `failed`

> Note: `processing` status acts as a crash recovery guard.
> Stuck `processing` records can be detected and retried or flagged.

---

## Notes

- All money is stored in **piastres** (1 EGP = 100 piastres) as BIGINT
- JWT auth will be replaced by shared auth service in Project 2
- Soft deletes used on `users`, `transactions`, `transfers`, `scheduled_transfers`
- Double-entry bookkeeping: every transfer creates two transaction records
