# Transfers

## Queries (sqlc)

- [x] Create transfer
- [x] Update transfer status
- [x] Update transfer with transaction IDs
- [x] Get transfer by ID (with wallet ownership check)
- [x] Get transfers by wallet ID (sent + received)

## Repository

- [x] Done

## Service

- [ ] CreateTransfer (validate receiver exists, sufficient balance, not sending to self)
- [ ] GetTransferByID (ownership check)
- [ ] ExecuteTransfer (DB transaction — debit, credit, update statuses)
- [ ] GetTransfers

## Handlers

- [ ] POST /transfers
- [ ] GET /transfers
- [ ] GET /transfers/:id

## Wiring

- [ ] Register routes in router
- [ ] Apply httprate middleware to POST /transfers (10 req/min per user)
