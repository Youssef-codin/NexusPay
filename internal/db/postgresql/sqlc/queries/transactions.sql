-- name: CreateTransaction :one
INSERT INTO transactions (sender_id,
                          receiver_id,
                          amount,
                          status,
                          note,
                          sender_category,
                          scheduled_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetTransactionById :one
SELECT *
FROM transactions
WHERE id = $1;

-- name: GetTransactionByIdWithUsers :one
SELECT t.*,
       s.full_name AS sender_name,
       r.full_name AS receiver_name
FROM transactions t
         JOIN users s ON s.id = t.sender_id
         JOIN users r ON r.id = t.receiver_id
WHERE t.id = $1;

-- name: GetTransactionsByUserId :many
SELECT t.*,
       s.full_name AS sender_name,
       r.full_name AS receiver_name
FROM transactions t
         JOIN users s ON s.id = t.sender_id
         JOIN users r ON r.id = t.receiver_id
WHERE t.sender_id = $1
   OR t.receiver_id = $1
ORDER BY t.created_at DESC;

-- name: GuardedSetStatus :one
-- The whole rewrite in one statement: the status guard makes the update claim
-- the row, and the row lock serializes concurrent callers. Zero rows
-- (pgx.ErrNoRows) means somebody else already did this work.
UPDATE transactions
SET status = sqlc.arg(to_status)
WHERE id = sqlc.arg(id)
  AND status = sqlc.arg(from_status)
RETURNING *;

-- name: ClaimDueTransactions :many
SELECT *
FROM transactions
WHERE status = 'scheduled'
  AND scheduled_at <= NOW()
ORDER BY scheduled_at
FOR UPDATE SKIP LOCKED
LIMIT $1;

-- name: ClaimStuckCrediting :many
SELECT *
FROM transactions
WHERE status = 'crediting'
  AND updated_at < NOW() - INTERVAL '5 minutes'
ORDER BY updated_at
FOR UPDATE SKIP LOCKED
LIMIT $1;

-- name: CancelScheduledTransaction :one
-- Races the scheduler correctly: this delete blocks on the scheduler's row
-- lock and then affects zero rows. Whichever lands first wins.
DELETE
FROM transactions
WHERE id = $1
  AND sender_id = $2
  AND status = 'scheduled'
RETURNING *;

-- name: SetSenderCategory :one
UPDATE transactions
SET sender_category = sqlc.arg(category)
WHERE id = sqlc.arg(id)
  AND sender_id = sqlc.arg(sender_id)
RETURNING *;

-- name: SetReceiverCategory :one
UPDATE transactions
SET receiver_category = sqlc.arg(category)
WHERE id = sqlc.arg(id)
  AND receiver_id = sqlc.arg(receiver_id)
RETURNING *;
