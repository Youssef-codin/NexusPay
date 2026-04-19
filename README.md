# NexusPay

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat&logo=go)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-336791?style=flat&logo=postgresql)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D?style=flat&logo=redis)](https://redis.io/)
[![Docker](https://img.shields.io/badge/Docker-24+-2496ED?style=flat&logo=docker)](https://www.docker.com/)

A production-ready digital wallet API built in Go, inspired by platforms like Telda.

---

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Scripts](#scripts)
- [Testing](#testing)
- [Engineering Highlights](#engineering-highlights)
- [Architecture](#architecture)
- [API Documentation](#api-documentation)
- [Tech Stack](#tech-stack)

---

## Features

- **User authentication** — Secure JWT-based auth with refresh tokens
- **Wallet management** — Every user gets a wallet with balance tracked in piastres (1 EGP = 100 piastres)
- **Stripe top-ups** — Add money via Stripe Payment Intents with webhook-driven fulfillment
- **P2P transfers** — Send money to other users with full transaction history
- **Scheduled transfers** — Set up future transfers via a goroutine-powered scheduler that runs every minute
- **Rate limiting** — Per-user and per-IP rate limiting on sensitive endpoints
- **Redis caching** — Wallet balances and user profiles cached with smart invalidation

---

## Prerequisites

- **Docker** — Required to run PostgreSQL and Redis
- **Go** (1.22+) — If running without Docker
- **just** — Optional, for running the scripts below
- **Stripe CLI** — Optional, for testing webhook integration

---

## Getting Started

```bash
# Clone and start dependencies
just up
just migrate-up

# Run the server
just run
```

API runs on `http://localhost:3000`

---

## Environment Variables

Copy `.env.example` to `.env` and fill in the values:

| Variable | Description |
| -------- | ----------- |
| `DB_URL` | PostgreSQL connection string |
| `REDIS_URL` | Redis connection string |
| `GOOSE_DBSTRING` | PostgreSQL connection string for migrations |
| `GOOSE_DRIVER` | Database driver for migrations |
| `GOOSE_MIGRATION_DIR` | Path to migration files |
| `JWT_SECRET` | Secret key for signing JWT tokens |
| `STRIPE_SECRET_KEY` | Stripe API key |

---

## Scripts

| Command | Description |
| ------- | ----------- |
| `just run` | Start the development server with hot reload |
| `just build` | Build the Go binary to `bin/` |
| `just test` | Run all tests |
| `just test-unit` | Run unit tests only |
| `just test-integration` | Run integration tests (requires Stripe CLI) |
| `just coverage` | Generate test coverage report |
| `just up` | Start PostgreSQL and Redis containers |
| `just down` | Stop containers |
| `just migrate-up` | Run database migrations |
| `just migrate-create <name>` | Create a new migration |
| `just sqlc-gen` | Generate type-safe SQL queries |
| `just lint` | Run linter |
| `just fmt` | Format code |

---

## Testing

Run unit tests for fast feedback:

```bash
just test-unit
```

Run full integration tests (requires Stripe CLI running with webhook forwarding):

```bash
stripe listen --forward-to localhost:3000/webhook/stripe
just test-integration
```

Generate coverage report:

```bash
just coverage
```

---

## Engineering Highlights

- **Goroutine-powered scheduler** — Scheduled transfers execute asynchronously using Go's concurrency model. A cron job polls the database every minute and dispatches transfers using goroutines for parallel execution
- **Double-entry bookkeeping** — Every transfer creates paired debit and credit transactions, ensuring full auditability and data consistency
- **Redis caching** — Wallet balances and user profiles cached in Redis with automatic invalidation on writes to keep data fresh
- **Rate limiting** — Per-user rate limiting on transfers (10 req/min) and top-ups (5 req/min), plus per-IP limiting on login to prevent abuse
- **Crash-recovery guards** — Scheduled transfers use `SELECT ... FOR UPDATE SKIP LOCKED` to prevent duplicate execution across multiple instances, with a `processing` status that catches stale jobs for retry
- **Soft deletes** — All major entities use soft deletes to preserve historical data
- **Stripe webhook security** — All webhook events validated using Stripe's signature verification
- **Idiomatic Go** — Clean layered architecture with handlers → services → repositories, generated type-safe SQL with sqlc, and structured logging with slog

---

## Architecture

```
Nexus/
├── cmd/                    # Application entrypoints
├── internal/
│   ├── auth/            # JWT authentication
│   ├── db/
│   │   ├── postgresql/  # PostgreSQL + sqlc
│   │   └── redisDb/    # Redis caching
│   ├── payment/        # Stripe integration
│   ├── security/      # Password hashing, JWT
│   ├── transactions/  # Transaction logic
│   ├── transfers/     # P2P + scheduled transfers
│   ├── users/         # User management
│   ├── utils/         # Helpers, validation
│   └── wallet/        # Wallet management
└── docs/
    └── bruno/         # API documentation
```

---

## API Documentation

The API is fully documented using:

- **Swagger UI** — Open `docs/NexusPay API-documentation.html` in a browser
- **Bruno** — Import the collection from `docs/bruno/NexusPay/`

Endpoints include:
- **Auth** — Register, login, refresh token, profile update
- **Wallet** — Get balance, initiate top-up, Stripe webhook
- **Transfers** — Create transfer, get history, get single transfer
- **Scheduled** — Create, list, update, cancel scheduled transfers
- **Users** — Search users by name or email

---

## Tech Stack

| Layer | Technology |
| ----- | ---------- |
| Language | Go |
| Router | Chi |
| Database | PostgreSQL |
| Cache & Rate Limiting | Redis |
| Query Generation | sqlc |
| Migrations | Goose |
| Authentication | JWT |
| Payments | Stripe |
| Testing | Go test |
| HTTP Client | net/http |

---
