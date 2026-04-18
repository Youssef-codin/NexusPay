# Scheduled Transfers Todo

## Database
- [x] Write sqlc queries for scheduled transfers
  - [x] `CreateScheduledTransfer`
  - [x] `GetScheduledTransferByID`
  - [x] `GetScheduledTransferByTransferID`
  - [x] `GetPendingScheduledTransfers`
  - [x] `MarkScheduledTransferExecuted`
  - [x] `CancelScheduledTransfer` (soft delete)
  - [x] `GetScheduledTransfersByUserId`

## Repository
- [x] `CreateScheduledTransfer(ctx, params)`
- [x] `GetScheduledTransferByID(ctx, id)`
- [x] `GetScheduledTransferByTransferID(ctx, transferID)`
- [x] `GetPendingScheduledTransfers(ctx, at)`
- [x] `MarkScheduledTransferExecuted(ctx, id)`
- [x] `CancelScheduledTransfer(ctx, id)`
- [x] `GetScheduledTransfersByUserId(ctx, userID)`

## Service
- [x] `ListScheduledTransfers(ctx, userID)`
- [x] `CancelScheduledTransfer(ctx, id)` — only allowed if not executed

## Cron Job
- [x] Set up cron runner (standard `time.Ticker` or a cron lib)
- [x] Runs every 30 mins
- [x] Fetch pending due transfers (scheduled_at <= NOW() AND executed_at IS NULL)
- [x] Execute transfer logic for each
- [x] Set executed_at on success
- [x] Log stuck records (scheduled_at far in the past)

## Handlers
- [x] `GET /scheduled` — list user's scheduled transfers
- [x] `DELETE /scheduled/:id` — cancel if not executed

## Wiring
- [x] Register routes in router
- [x] Start cron job in `main.go` alongside server
- [x] Ensure cron shuts down cleanly on app shutdown signal

## Misc 
- [ ] add Idempotency for transfer
- [ ] add Pagination 
