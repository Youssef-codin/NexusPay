# Default: list all recipes
default:
    @just --list

# ── Dev ────────────────────────────────────────────────────────────────────────

[group('dev')]
run: up
    air

[group('dev')]
stripe:
    stripe listen --forward-to localhost:3000/webhook/stripe

# ── Build ──────────────────────────────────────────────────────────────────────

[group('build')]
build:
    go build -o bin/app ./cmd

# ── Test ───────────────────────────────────────────────────────────────────────

[group('test')]
test:
    go test ./...

[group('test')]
testv:
    go test -v ./...

[group('test')]
test-unit:
    go test ./... -run '^Test[^_]+$' -v

[group('test')]
test-integration:
    go test ./... tests/... -tags=integration -run '_Integration$' -v -p 1

[group('test')]
coverage:
    mkdir -p docs
    go test -tags=integration -coverprofile=coverage.out ./... tests/...
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
    goose up

[group('db')]
migrate-down:
    goose down

[group('db')]
migrate-status:
    goose status

[group('db')]
migrate-create NAME="migration":
    goose create {{NAME}} sql

[group('db')]
seed-simple:
    go run ./cmd/seed_simple

[group('db')]
seed:
    go run ./cmd/seed

# ── Setup ─────────────────────────────────────────────────────────────────────

[group('setup')]
setup: tidy sqlc-gen
    @echo "Setup complete!"

# ── Lambda ────────────────────────────────────────────────────────────────────

# Static arm64 binary named `bootstrap`, which is what provided.al2023 runs.
[group('lambda')]
lambda-build:
    mkdir -p bin/lambda
    GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -trimpath -ldflags="-s -w" -o bin/lambda/bootstrap ./cmd

# First deploy: prompts for the parameters and saves them to samconfig.toml (gitignored).
[group('lambda')]
lambda-deploy-guided: lambda-build
    sam deploy --guided --stack-name nexuspay --capabilities CAPABILITY_IAM

[group('lambda')]
lambda-deploy: lambda-build
    sam deploy

[group('lambda')]
lambda-logs:
    sam logs --stack-name nexuspay --tail
