# NexusPay Setup Todo

## Core Infrastructure

- [x] Project config (env loading, config struct)
- [x] Database connection (PostgreSQL pool)
- [ ] Redis client setup

## Router & Middleware

- [x] Chi router init
- [ ] CORS
- [x] Logger
- [x] Recoverer
- [ ] Auth middleware (JWT validation)
- [ ] Rate limiting with httprate-redis

## Server

- [ ] Graceful shutdown
- [ ] Health check endpoint (`GET /health`)

## Database

- [x] sqlc setup + sqlc.yaml
- [ ] goose migrations wired (Makefile commands)
