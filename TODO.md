# Scheduled Transfers Todo

## Database
- [ ] Write sqlc queries for scheduled transfers
  - [ ] `CreateScheduledTransfer`
  - [ ] `GetScheduledTransferByID`
  - [ ] `ListScheduledTransfersByWalletID`
  - [ ] `UpdateScheduledTransferStatus`
  - [ ] `GetPendingScheduledTransfers` (status = pending AND scheduled_at <= NOW(), SKIP LOCKED)
  - [ ] `CancelScheduledTransfer` (soft delete + status = cancelled)

## Repository
- [ ] `CreateScheduledTransfer(ctx, params)`
- [ ] `GetScheduledTransferByID(ctx, id)`
- [ ] `ListScheduledTransfers(ctx, walletID)`
- [ ] `UpdateScheduledTransferStatus(ctx, id, status)`
- [ ] `CancelScheduledTransfer(ctx, id)`
- [ ] `GetPendingDueTransfers(ctx)` — used by cron

## Service
- [ ] `CreateScheduledTransfer(ctx, params)` — validate scheduled_at is in the future
- [ ] `GetScheduledTransfer(ctx, id)` — ownership check
- [ ] `ListScheduledTransfers(ctx, userID)`
- [ ] `UpdateScheduledTransfer(ctx, id, params)` — only allowed if status = pending
- [ ] `CancelScheduledTransfer(ctx, id)` — only allowed if status = pending
- [ ] `ExecuteScheduledTransfer(ctx, st)` — reuses transfer execution logic

## Cron Job
- [ ] Set up cron runner (standard `time.Ticker` or a cron lib)
- [ ] Runs every minute
- [ ] Fetch pending due transfers
- [ ] Set status to `processing`
- [ ] Execute transfer logic for each
- [ ] Set status to `completed` or `failed`
- [ ] Log stuck `processing` records (scheduled_at far in the past)

## Handlers
- [ ] `POST /scheduled` — create scheduled transfer
- [ ] `GET /scheduled` — list user's scheduled transfers
- [ ] `GET /scheduled/:id` — get single (ownership check)
- [ ] `PATCH /scheduled/:id` — update if pending
- [ ] `DELETE /scheduled/:id` — cancel if pending

## Wiring
- [ ] Register routes in router
- [ ] Start cron job in `main.go` alongside server
- [ ] Ensure cron shuts down cleanly on app shutdown signal

## Misc 
- [ ] add Idempotency for transfer
- [ ] add Pagination 
