-- name: CreateUser :one
INSERT INTO users (email,
                   password,
                   full_name,
                   refresh_token,
                   token_expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING
    id,
    email,
    full_name,
    refresh_token,
    balance,
    created_at;

-- name: GetUserById :one
SELECT *
FROM users
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE email = $1
  AND deleted_at IS NULL;

-- name: GetUserByName :many
SELECT *
FROM users
WHERE full_name % $1
  AND deleted_at IS NULL
  AND NOT is_system
ORDER BY similarity(full_name, $1) DESC;

-- name: GetUserByRefreshToken :one
SELECT *
FROM users
WHERE refresh_token = $1
  AND deleted_at IS NULL;

-- name: GetBalance :one
SELECT balance
FROM users
WHERE id = $1
  AND deleted_at IS NULL;

-- name: DebitUser :one
-- Zero rows means insufficient funds. This guard, inside the money-moving
-- transaction, is the ONLY thing enforcing sufficient funds -- a read taken
-- outside the transaction protects nothing. The system-account carve-out lives
-- here so callers never have to branch on it.
UPDATE users
SET balance = balance - sqlc.arg(amount)
WHERE id = sqlc.arg(id)
  AND (balance >= sqlc.arg(amount) OR is_system)
  AND deleted_at IS NULL
RETURNING *;

-- name: CreditUser :one
UPDATE users
SET balance = balance + sqlc.arg(amount)
WHERE id = sqlc.arg(id)
  AND deleted_at IS NULL
RETURNING *;

-- name: UpdateUserDetails :one
UPDATE users
SET full_name = $2,
    email     = $3
WHERE id = $1
  AND deleted_at IS NULL
RETURNING *;

-- name: UpdateRefreshToken :exec
UPDATE users
SET refresh_token    = $2,
    token_expires_at = $3
WHERE id = $1
  AND deleted_at IS NULL;

-- name: RevokeRefreshToken :exec
UPDATE users
SET refresh_token    = NULL,
    token_expires_at = NULL
WHERE id = $1
  AND deleted_at IS NULL;

-- name: SoftDeleteUser :exec
UPDATE users
SET deleted_at = NOW()
WHERE id = $1
  AND deleted_at IS NULL;
