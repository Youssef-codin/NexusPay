# Default: list all recipes
default:
    @just --list

# ── Dev ────────────────────────────────────────────────────────────────────────

[group('dev')]
dev: up
    air

# ── Build ──────────────────────────────────────────────────────────────────────

[group('build')]
build:
    go build -o bin/app ./...

# ── Test ───────────────────────────────────────────────────────────────────────

[group('test')]
test filter="":
    go test -v -run {{filter}} ./...

[group('test')]
test-integration filter="":
    go test -tags=integration -v -run {{filter}} ./integration/...

[group('test')]
test-all filter="":
    go test -tags=integration -v -run {{filter}} ./...

[group('test')]
coverage:
    mkdir -p docs
    go test -coverprofile=coverage.out ./...
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
