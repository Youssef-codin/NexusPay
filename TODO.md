# Transfers

- [ ] add Idempotency for transfer
- [ ] add Pagination 

## Queries (sqlc)

- [x] Create transfer
- [x] Update transfer status
- [x] Update transfer with transaction IDs
- [x] Get transfer by ID (with wallet ownership check)
- [x] Get transfers by wallet ID (sent + received)

## Repository

- [x] Done

## Service

- [x] CreateTransfer (validate receiver exists, sufficient balance, not sending to self)
~- [ ] GetTransferByID (ownership check)~
- [x] ExecuteTransfer (DB transaction — debit, credit, update statuses)
- [x] GetTransfers

## Handlers

- [x] POST /transfers
- [x] GET /transfers
- [ ] GET /transfers/:id

## Wiring

- [x] Register routes in router
- [x] Apply httprate middleware to POST /transfers (10 req/min per user)
