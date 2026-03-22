# Stripe Top-Up Flow

## ~`POST /wallet/topup`~

- [x] Validate request body (amount in piastres, min value)
- [x] Get wallet from DB using authenticated user ID
- [x] Create a pending transaction in DB (type: `credit`, status: `pending`)
- [x] Create Stripe Payment Intent (amount, currency: `egp`, metadata: `transaction_id`)
- [x] Return `client_secret` and `transaction_id` to client

## ~`POST /webhook/stripe`~

- [x] Read raw request body
- [x] Verify Stripe webhook signature using `stripe.ConstructEvent`
- [x] Switch on event type:
  - [x] `payment_intent.succeeded` → update transaction to `completed`, increment wallet balance
  - [x] `payment_intent.payment_failed` → update transaction to `failed`
- [x] Return 200 immediately (Stripe retries if it doesn't get 200)

## ~Queries Needed (sqlc)~

- [x] `CreateTransaction`
- [x] `UpdateTransactionStatus`
- [x] `GetTransactionByID`
- [x] `IncrementWalletBalance`

## Misc

- [x] Add `STRIPE_SECRET_KEY` and `STRIPE_WEBHOOK_SECRET` to `.env`
- [x] Store `transaction_id` in Stripe metadata so you can look it up in the webhook
- [x] Add time out to processPayment
- [ ] implement rest of stripe payment service
- [ ] Add an integration test that tests our transaction logic
