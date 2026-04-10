-- name: CreateTransfer :one
INSERT INTO transfers
(id,
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
        $7,
        $8)
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

-- name: GetTransferByWalletId :one
SELECT *
FROM transfers
WHERE to_wallet_id = $1
   OR from_wallet_id = $1;