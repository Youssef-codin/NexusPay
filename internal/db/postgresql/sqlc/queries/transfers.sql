-- name: CreateTransfer :one
INSERT INTO transfers
(
  from_wallet_id,
  to_wallet_id,
  amount,
  status,
  note,
  debit_transaction_id,
  credit_transaction_id)
VALUES ($1,
        $2,
        $3,
        $4,
        $5,
        $6,
        $7)
RETURNING *;

-- name: UpdateTransferStatus :one
UPDATE transfers
SET status = $2
WHERE id = $1
RETURNING *;

-- name: UpdateTransferWithTransactionId :one
UPDATE transfers
SET status = $2
WHERE credit_transaction_id = $1
   OR debit_transaction_id = $1
RETURNING *;

-- name: GetTransferById :one
SELECT *
FROM transfers
WHERE id = $1;

-- name: GetTransferByIdWithUser :one
SELECT 
  t.id, t.from_wallet_id, t.to_wallet_id, t.amount, t.status, t.note,
  t.created_at, t.updated_at, t.deleted_at,
  t.debit_transaction_id, t.credit_transaction_id,
  u.id as to_user_id, u.full_name as to_user_full_name
FROM transfers t
LEFT JOIN wallets w ON t.to_wallet_id = w.id
LEFT JOIN users u ON w.user_id = u.id
WHERE t.id = $1;

-- name: GetTransfersByWalletId :many
SELECT *
FROM transfers
WHERE to_wallet_id = $1
   OR from_wallet_id = $1;

-- name: GetTransfersByWalletIdWithUser :many
SELECT 
  t.id, t.from_wallet_id, t.to_wallet_id, t.amount, t.status, t.note,
  t.created_at, t.updated_at, t.deleted_at,
  t.debit_transaction_id, t.credit_transaction_id,
  u.id as to_user_id, u.full_name as to_user_full_name
FROM transfers t
LEFT JOIN wallets w ON t.to_wallet_id = w.id
LEFT JOIN users u ON w.user_id = u.id
WHERE t.to_wallet_id = $1
   OR t.from_wallet_id = $1;
