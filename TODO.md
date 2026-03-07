# NexusPay Setup Todo

## Core Infrastructure

- [x] Project config (env loading, config struct)
- [x] Database connection (PostgreSQL pool)
- [x] Redis client setup

## Router & Middleware

- [x] Chi router init
- [x] CORS
- [x] Logger
- [x] Recoverer
- [x] Auth middleware (JWT validation)
- [x] Rate limiting with httprate-redis

## Server

- [x] Graceful shutdown
- [x] Health check endpoint (`GET /health`)

## Database

- [x] sqlc setup + sqlc.yaml
- [x] goose migrations wired (Makefile commands)
- [x] setup redis client api
