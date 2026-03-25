# Transfers

## Queries (sqlc)

- [ ] Create transfer
- [ ] Update transfer status
- [ ] Update transfer with transaction IDs
- [ ] Get transfer by ID (with wallet ownership check)
- [ ] Get transfers by wallet ID (sent + received)

## Repository

- [ ] CreateTransfer
- [ ] ExecuteTransfer (DB transaction — debit, credit, update statuses)
- [ ] GetTransferByID
- [ ] GetTransfers

## Service

- [ ] CreateTransfer (validate receiver exists, sufficient balance, not sending to self)
- [ ] GetTransferByID (ownership check)
- [ ] GetTransfers

## Handlers

- [ ] POST /transfers
- [ ] GET /transfers
- [ ] GET /transfers/:id

## Wiring

- [ ] Register routes in router
- [ ] Apply httprate middleware to POST /transfers (10 req/min per user)
