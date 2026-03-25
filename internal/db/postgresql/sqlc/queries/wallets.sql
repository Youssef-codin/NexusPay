-- name: CreateWallet :one
INSERT INTO wallets (user_id, balance)
VALUES ($1, $2)
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
  AND $2 > 0
  AND deleted_at IS NULL
RETURNING *;

-- name: DeductFromBalance :one
UPDATE wallets
SET balance = balance - $2
WHERE user_id = $1
  AND $2 > 0
  AND balance >= $2
  AND deleted_at IS NULL
RETURNING *;
