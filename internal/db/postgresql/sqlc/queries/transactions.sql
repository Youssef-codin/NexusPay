-- name: CreateTransaction :one
INSERT INTO transactions
    (wallet_id, amount, type, status, transfer_id, description)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, wallet_id, amount, type, status, transfer_id, created_at, description;

-- name: GetTransactionById :one
SELECT *
FROM transactions
WHERE id = $1
  AND deleted_at IS NULL
    FOR UPDATE;

-- name: GetTransactionsByWalletId :many
SELECT *
FROM transactions
WHERE wallet_id = $1
  AND deleted_at IS NULL;

-- name: GetTransactionByTransferId :one
SELECT *
FROM transactions
WHERE transfer_id = $1
  AND deleted_at IS NULL;

-- name: UpdateTransactionStatus :one
UPDATE transactions
SET status = $2
WHERE id = $1
  AND deleted_at IS NULL
RETURNING *;


