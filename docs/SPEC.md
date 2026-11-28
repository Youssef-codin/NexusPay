# NexusPay architecture spec

_Rewrite design as of 28 Nov 2026. Source: https://claude.ai/artifact/MATjwK6sqPLyasbSBS2cEE_

A modular Go monolith on Lambda behind a Hono gateway. Modules own jobs, not tables. Only the ledger writes balances, and every money move is one Postgres transaction.

## 1. System map

```
Client app (web/mobile) ──HTTPS──▶ Hono gateway (VPS; better-auth, rate limit)
Client app ──pays with client_secret──▶ Stripe

Hono ──JWT──▶ API Lambda (private, checks JWT) ──JWKS──▶ Upstash (JWKS cache)
Stripe ──payment_intent.*──▶ Webhook Lambda (public URL)
EventBridge rate(1 minute) ──tick──▶ Scheduler Lambda (RunDue())

All three Lambdas import the same Go modules (same binary):
  Accounts · Transfers · Top-ups · Providers (Stripe now, Paymob later) · Ledger (only balance writer)

Go modules ──one tx per money move──▶ Postgres (accounts, ledger_entries, transfers, topups)
```

- Three Lambdas share one Go codebase.
- The API Lambda is reachable only from Hono.
- The webhook Lambda is public; Stripe's signature check guards it.
- Only the ledger writes `accounts.balance` and `ledger_entries`. Every other module owns its own table and uses its own narrow set of sqlc queries.

## 2. Top-up flow

| # | From → To | Action |
|---|-----------|--------|
| 1 | Client → Hono | `POST /topups {id, amount}` |
| 2 | Hono → API Lambda | forward + JWT |
| 3 | API Lambda → Postgres | `INSERT` pending top-up |
| 4 | API Lambda → Stripe | create PaymentIntent, `Idempotency-Key` = top-up id |
| 5 | Stripe → API Lambda | intent id + `client_secret` |
| 6 | API Lambda → Postgres | save intent id |
| 7 | API Lambda → Client | `client_secret` |
| 8 | Client → Stripe | confirm payment (card) |
| 9 | Stripe → Webhook Lambda | `succeeded`; verify signature, `ParseEvent` |
| 10 | Webhook Lambda → Postgres | guarded `UPDATE` (**tx start**) |
| 11 | Webhook Lambda → Postgres | `Ledger.Move(clearing → user)` |
| 12 | Webhook Lambda → Postgres | `COMMIT` (**tx end**) |
| 13 | Webhook Lambda → Stripe | `200 OK` |

- The top-up id is the client's UUIDv7 and doubles as Stripe's Idempotency-Key, so a retried request returns the same row and PaymentIntent.
- The credit (steps 10–12) is one transaction.
- A guarded update that matches zero rows means it's already credited: return 200 and do nothing.
- The amount comes from our row, not the webhook.

## 3. Transfers: send now, schedule, cancel

```
POST /transfers ─▶ Insert (ON CONFLICT (id) → replay) ─▶ scheduled_at?
                     │                                    ├─ empty ─▶ Execute(id): claim pending → Ledger.Move → completed | failed
                     │  [all of the above is ONE tx]      └─ set ───▶ stays pending
DELETE /transfers/{id} ─▶ cancel pending → cancelled
EventBridge tick ─▶ RunDue(): claim 1 due row (FOR UPDATE SKIP LOCKED) ─▶ same Execute, one tx per transfer
```

- Send-now inserts and executes in one transaction, so a row never sits unattempted.
- Scheduled transfers stay pending until a tick claims them.
- Cancel and Execute race safely: the row lock decides and the loser updates zero rows.
- Insufficient funds marks the transfer `failed` in the same transaction.
- Replaying the same id returns the stored result.

## 4. Status lifecycles

### Transfers

| From | To | Trigger | Moves money |
|------|----|---------|-------------|
| pending | completed | Execute, Move succeeded | yes |
| pending | failed | Execute, `insufficient_funds` (now or when due) | no |
| pending | cancelled | Cancel by sender, scheduled only | no |

### Top-ups

| From | To | Trigger | Moves money |
|------|----|---------|-------------|
| pending | processing | provider reports processing | no |
| processing | succeeded | credit: `Move(clearing → user)` | yes |
| pending | succeeded | credit: `Move(clearing → user)` | yes |
| pending | canceled | canceled | no |
| processing | canceled | canceled | no |
| pending | expired | never paid | no |
| pending | pending | `payment_failed` keeps it pending | no |

- Every transition is a guarded `UPDATE … WHERE status = …`; only `completed` / `succeeded` move money.
- `processing` only mirrors what Stripe or Paymob reports, for the user's benefit. It never moves money.

## 5. Decisions (locked)

- **Shape** — Modular monolith. Modules own jobs, not tables. Rules in Go; database constraints as the safety net.
- **Auth** — Hono + better-auth (shared identity service) handles routing and rate limiting. Go verifies the JWT and reads `sub`. JWKS cached in memory, then Upstash.
- **Network** — API Lambda is private, reachable only from Hono. Webhook Lambda has a public URL. EventBridge invokes the scheduler Lambda every minute.
- **accounts** — `id` (v7), `user_id` (identity, unique), `username` (copied once, immutable), `balance` (`CHECK ≥ 0`, piastres), `created_at`. One system row: `stripe_clearing`, which may go negative.
- **Open account** — Idempotent through `ON CONFLICT (user_id)`. Hono then adds the `nexuspay` product in identity.
- **Search** — By username prefix over NexusPay accounts only. No email search.
- **Ledger** — `Move(txCtx, from, to, amount, ref)` is the only balance writer. Guarded debit, then credit, then entries that sum to zero. Never starts its own transaction.
- **Transactions** — Via `txCtx`. Rewrite `StartTx` so it doesn't nest, use a `WithTx` helper, and always check commit errors.
- **Top-ups** — Minimum 1000 piastres. Client UUIDv7 serves as both the row id and the Stripe Idempotency-Key. Guarded status update, no `processed_events` table for now. `payment_failed` is not final.
- **Transfers** — One table with `scheduled_at` and one `Execute`. Client UUIDv7 key, scoped to the sender; a replay returns the stored result. Insufficient funds stores the transfer as `failed`, both now and when due. Cancel only applies to pending scheduled transfers.
- **Pagination** — Cursor-based on `GET /transfers`, `GET /topups` and account search. UUIDv7 ids sort by creation time, so the cursor is just the last id: `WHERE id < $cursor ORDER BY id DESC LIMIT $n`. Default 20, max 100. Responses are `{items, next_cursor}`. Transfers lists sent and received (`from_account = me OR to_account = me`), with an index on each column paired with `id`. Search pages by `username`, since that's the order it sorts in.
- **Out of scope** — Paymob, AI analytics, editing scheduled transfers, notifications, withdrawals.
