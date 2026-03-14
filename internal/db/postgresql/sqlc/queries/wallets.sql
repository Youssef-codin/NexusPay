-- name: CreateWallet :one
INSERT INTO wallets (id, user_id, balance, created_at, updated_at, deleted_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, user_id, balance, created_at;

-- name: GetWalletById :one
SELECT *
FROM wallets
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetWalletByUserId :one
SELECT *
FROM wallets
WHERE user_id = $1
  AND deleted_at IS NULL;

-- name: AddToBalance :one
UPDATE wallets
SET balance = balance + $2
WHERE user_id = $1
  AND deleted_at IS NULL
RETURNING *;

-- name: DeductFromBalance :one
UPDATE wallets
SET balance = balance - $2
WHERE user_id = $1
  AND deleted_at IS NULL
RETURNING *;
