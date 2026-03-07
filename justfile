# Default: list all recipes
default:
    @just --list

# ── Dev ────────────────────────────────────────────────────────────────────────

[group('dev')]
run: up
    air

# ── Build ──────────────────────────────────────────────────────────────────────

[group('build')]
build:
    go build -o bin/app ./...

# ── Test ───────────────────────────────────────────────────────────────────────

[group('test')]
test:
    go test ./...

[group('test')]
coverage:
    mkdir -p docs
    go test -tags=integration -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o docs/coverage.html

# ── Code Quality ───────────────────────────────────────────────────────────────

[group('quality')]
fmt:
    gofmt -w .

[group('quality')]
lint:
    golangci-lint run

[group('quality')]
tidy:
    go mod tidy

# ── Docker ─────────────────────────────────────────────────────────────────────

[group('docker')]
up:
    docker compose up -d db redis

[group('docker')]
down:
    docker compose down db redis

[group('docker')]
logs *args:
    docker compose logs -f {{args}}

# ── Clean ──────────────────────────────────────────────────────────────────────

[group('clean')]
clean:
    rm -rf bin/ coverage.out tmp/

# ── SQL / Database ───────────────────────────────────────────────────────────

[group('db')]
sqlc-gen:
    sqlc generate

[group('db')]
migrate-up:
    goose -dir {{env_var('GOOSE_MIGRATION_DIR')}} {{env_var('GOOSE_DRIVER')}} "{{env_var('GOOSE_DBSTRING')}}" up

[group('db')]
migrate-down:
    goose -dir {{env_var('GOOSE_MIGRATION_DIR')}} {{env_var('GOOSE_DRIVER')}} "{{env_var('GOOSE_DBSTRING')}}" down

[group('db')]
migrate-status:
    goose -dir {{env_var('GOOSE_MIGRATION_DIR')}} {{env_var('GOOSE_DRIVER')}} "{{env_var('GOOSE_DBSTRING')}}" status

[group('db')]
migrate-create NAME="migration":
    goose -dir {{env_var('GOOSE_MIGRATION_DIR')}} create {{NAME}} sql

# ── Setup ─────────────────────────────────────────────────────────────────────

[group('setup')]
setup: tidy sqlc-gen
    @echo "Setup complete!"
