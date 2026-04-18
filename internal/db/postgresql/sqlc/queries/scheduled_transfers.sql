-- name: CreateScheduledTransfer :one
INSERT INTO scheduled_transfers
(transfer_id, scheduled_at)
VALUES ($1, $2)
RETURNING *;

-- name: GetScheduledTransferById :one
SELECT *
FROM scheduled_transfers
WHERE id = $1;

-- name: GetScheduledTransferByTransferId :one
SELECT *
FROM scheduled_transfers
WHERE transfer_id = $1;

-- name: GetPendingScheduledTransfers :many
SELECT *
FROM scheduled_transfers
WHERE scheduled_at <= $1
  AND executed_at IS NULL;

-- name: MarkScheduledTransferExecuted :one
UPDATE scheduled_transfers
SET executed_at = NOW()
WHERE id = $1
RETURNING *;

-- name: CancelScheduledTransfer :one
UPDATE scheduled_transfers
SET deleted_at = NOW()
WHERE id = $1
  AND deleted_at IS NULL
  AND executed_at IS NULL
RETURNING *;

-- name: GetScheduledTransfersByUserId :many
SELECT st.*
FROM scheduled_transfers st
JOIN transfers t ON t.id = st.transfer_id
JOIN wallets w ON w.id = t.from_wallet_id
WHERE w.user_id = $1
  AND st.deleted_at IS NULL
ORDER BY st.scheduled_at DESC;
