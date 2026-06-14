# NexusPay — Product Requirements Document

> Status: Draft for the rewrite
> Owner: Youssef
> Last updated: 2026-06-14

NexusPay is a digital wallet API (inspired by Telda). Users hold a balance in
**piastres** (1 EGP = 100 piastres), top up via Stripe, send money to other
users (immediately or on a schedule), and view their transaction history.

This document specifies what the rewrite must do, with particular emphasis on
**money-safety under concurrency** — the class of bugs where the same logical
payment is applied more than once, or a balance is allowed to go wrong because
two requests raced. Those requirements and their test cases are the core of
this PRD (sections 5–7).

---

## 1. Goals

- Move money correctly. **Every piastre debited from one wallet is credited to
  exactly one other place, exactly once — never zero times, never twice.**
- Survive duplicate and out-of-order external events (Stripe delivers webhooks
  **at least once**, and retries on any non-2xx or timeout).
- Survive concurrent requests against the same wallet (double-clicks, retries,
  multiple devices) and concurrent server instances (horizontal scaling).
- Be testable: every money-safety invariant below has at least one automated
  test that fails if the invariant is violated.

## 2. Non-goals (this version)

- Multi-currency / FX.
- Refunds and chargebacks beyond marking a transaction failed/reversed.
- Ledger double-entry accounting reporting (we track transactions, not a full
  GL). Internal balances must still always reconcile (§6).
- The AI analytics, login caching, and Paymob items from `TODO.md` are out of
  scope here and tracked separately.

## 3. Users

- **Wallet holder** — registers, tops up, sends and schedules transfers, views
  history.
- **Stripe (system actor)** — sends webhook events about payment intents.
- **Scheduler (system actor)** — an internal job that executes due scheduled
  transfers.

## 4. Functional requirements

### 4.1 Auth
- JWT-based auth with refresh tokens. Every wallet-mutating endpoint requires a
  valid token; the acting user is derived from the token, never the request body.

### 4.2 Wallet
- Each user has exactly one wallet, created at registration, balance starts at 0.
- Balance is a non-negative integer count of piastres. **Balance must never go
  negative** under any interleaving of requests.

### 4.3 Top-up (Stripe)
- `PATCH /wallet` creates a `credit` transaction in `pending` and a Stripe
  PaymentIntent carrying the transaction id in metadata. Minimum 1000 piastres.
- The wallet is credited **only** when the `payment_intent.succeeded` webhook is
  processed — never at request time.
- `payment_intent.payment_failed` / `payment_intent.canceled` mark the
  transaction `failed` and never touch the balance.

### 4.4 P2P transfer
- `POST /transfers` moves piastres from the caller's wallet to a target wallet.
- Cannot transfer to self. Cannot transfer more than the current balance.
- A transfer is atomic: debit + credit + status change either all happen or
  none do.

### 4.5 Scheduled transfer
- A transfer may carry a `scheduled_at`. It is stored `pending` and executed by
  the scheduler at/after that time. It may be cancelled before execution.
- A scheduled transfer executes **exactly once**.

---

## 5. Money-safety & concurrency requirements (normative)

These are the requirements the rewrite exists to get right. Each has an ID
(`MS-n`) referenced by the test cases in §7. The "Current gap" notes describe
what the existing implementation does today so the rewrite doesn't reintroduce
the same hole.

### MS-1 — Wallet credit from a payment is idempotent per payment
Processing the **same** `payment_intent.succeeded` event N times credits the
wallet exactly once. Stripe delivers at least once and retries on any non-2xx
or slow response, and may deliver duplicates concurrently.

Required mechanism (at least one, preferably both):
- A **processed-events** table keyed by Stripe `event.ID` (or PaymentIntent id),
  written in the **same transaction** as the credit, with a `UNIQUE` constraint.
  A duplicate insert ⇒ skip the credit and return 2xx.
- The credit + transaction-status transition guarded by `SELECT … FOR UPDATE`
  on the transaction row so the status check and the credit are one atomic
  critical section.

> **Current gap.** `HandlePaymentSucceeded` reads the transaction and rejects if
> its status is already `processing`/`completed`, but the read has no
> `FOR UPDATE`, the tx runs at default Read Committed, and there is no event-id
> dedup. Two concurrent deliveries can both read `pending`, both pass the guard,
> and both call `AddToWallet` → **double credit**. `webhook.go:42`.

### MS-2 — A transfer executes at most once (idempotent execution)
`ExecuteTransfer` applied twice to the same transfer must debit/credit only
once. The completing status update must be a **conditional** update that only
fires when the transfer is still `pending`, in the same transaction as the
balance moves; if zero rows are affected, the money moves must roll back.

> **Current gap.** `UpdateTransferStatus` is `SET status=$2 WHERE id=$1` with no
> status guard (`transfers.sql.go:261`), and `ExecuteTransfer` never re-checks
> the transfer status inside its own tx (`service.go:206`). A second call
> deducts again.

### MS-3 — A scheduled transfer is claimed by exactly one worker
When multiple scheduler ticks overlap, or multiple server replicas run the
scheduler, a due scheduled transfer is executed by exactly one of them.

Required mechanism:
- Fetch due rows with `FOR UPDATE SKIP LOCKED`, **and/or** claim each row with a
  conditional `UPDATE … SET executed_at = NOW() WHERE id = $1 AND executed_at IS
  NULL RETURNING …` and only execute if a row was returned — all in one tx with
  the execution.

> **Current gap.** `GetPendingScheduledTransfers` selects `executed_at IS NULL`
> with no locking (`scheduled_transfers.sql.go:61`); `processOneTransfer` does
> check-status → execute → mark as three separate, unlocked steps
> (`scheduler.go:107`). Overlap or multi-replica ⇒ same transfer paid twice.

### MS-4 — Balance never goes negative
No interleaving of concurrent debits (transfers, future withdrawals) may drive a
balance below zero. The decrement must be a single conditional statement
(`UPDATE … SET balance = balance - $amt WHERE id = $1 AND balance >= $amt`) and
the caller must treat "0 rows affected" as insufficient funds.

> **Current state: OK.** `DeductFromBalance` already has `AND balance >= $2`
> (`wallets.sql.go:72`); concurrent debits serialize on the row lock. Keep this
> guarantee in the rewrite and do **not** rely on the service-layer pre-read
> (`service.go:268`) for correctness — it is a TOCTOU and only a UX nicety.

### MS-5 — A transfer is atomic across debit, credit, and status
All side effects of a transfer (sender debit, receiver credit, transfer row
status, both transaction rows) commit together or not at all. A failure after
the debit must not leave money destroyed or duplicated. Sum of all wallet
balances changes by exactly zero across an internal transfer.

### MS-6 — Top-up does not credit at request time
`PATCH /wallet` must never change the balance. Only the succeeded webhook does.
A failed/canceled payment leaves the balance untouched.

### MS-7 — Idempotent client retries (recommended)
Money-moving POSTs (`/transfers`, `/wallet` top-up) accept an
`Idempotency-Key`. Replaying the same key returns the original result without
creating a second transfer/PaymentIntent. (`TODO.md`: "add Idempotency for
transfer".)

---

## 6. Data-integrity invariants (always true)

- `I1` — every wallet balance ≥ 0.
- `I2` — for any internal transfer, `Σ balances` is unchanged (money is
  conserved); the debit amount equals the credit amount.
- `I3` — a `completed` transfer has exactly one debit and one credit transaction,
  both `completed`.
- `I4` — a `pending`/`failed`/`cancelled` transfer has moved no money.
- `I5` — a `scheduled_transfer` has `executed_at` set at most once.
- `I6` — a given Stripe payment credits its wallet at most once.

These can be asserted as post-conditions in tests and as periodic reconciliation
checks in production.

---

## 7. Test cases

Tests live under `tests/integration/` (build tag `integration`, testcontainers
Postgres + Redis + Stripe CLI; see existing `service_test.go` for the harness)
and as unit tests in the owning package. Each case names the requirement it
guards.

Legend: **CT** = concurrency test. Every CT must be run with `-race`.

### Existing coverage (keep)
- `TestConcurrentTransfers_Integration` — guards MS-5 / I2 (money in == money out
  for two simultaneous transfers).
- `TestConcurrentTransfers_ExceedBalance_Integration` — guards MS-4 (two
  simultaneous transfers exceeding balance; no negative, no over-credit).

### New / required cases

#### CT-1 — Duplicate `payment_intent.succeeded` credits once (MS-1, I6) — **highest priority**
The double-payment case. Deliver the same succeeded event twice (sequentially
and concurrently) for one top-up and assert the wallet is credited once.

```
GIVEN a funded top-up whose transaction is `pending`
WHEN  the payment_intent.succeeded webhook for that transaction is delivered twice
THEN  the wallet balance increases by the top-up amount exactly once
AND   the transaction ends `completed`
AND   both deliveries return 2xx (so Stripe stops retrying)
```

Two variants:
- **Sequential replay** — send event, wait for credit, resend the *identical*
  signed payload. Balance must not change on the second.
- **Concurrent replay** — fire N identical deliveries in parallel (`-race`).
  Exactly one applies the credit.

Sketch (service-level, deterministic — preferred for CI because it doesn't
depend on Stripe redelivery timing):

```go
//go:build integration

func TestWebhook_DuplicateSucceeded_CreditsOnce(t *testing.T) {
	if err := setup(); err != nil { t.Fatalf("setup: %v", err) }
	defer teardown()

	ctx := context.Background()
	// Arrange: a wallet + a pending credit transaction of 50_000 piastres.
	walletID, txID := seedPendingTopUp(t, ctx, 50_000)
	before := walletBalance(t, ctx, walletID)

	// Build the webhook service directly so we control concurrency precisely.
	svc := stripe.NewWebhookService(database, walletService, transactionService)
	req := stripe.HandlePaymentSucceededRequest{TransactionID: txID}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errs[i] = svc.HandlePaymentSucceeded(ctx, req) }(i)
	}
	wg.Wait()

	after := walletBalance(t, ctx, walletID)
	if after-before != 50_000 {
		t.Fatalf("MS-1 violated: credited %d, want 50000 (double-credit?)", after-before)
	}
	// Exactly one call should have applied the credit; the rest must be
	// no-ops (idempotent), not hard errors that would make Stripe retry.
	applied := 0
	for _, e := range errs {
		if e == nil { applied++ } else if !errors.Is(e, stripe.ErrAlreadyProcessing) {
			t.Errorf("unexpected error from duplicate delivery: %v", e)
		}
	}
	if applied != 1 {
		t.Fatalf("MS-1: expected exactly 1 applying call, got %d", applied)
	}
}
```

End-to-end variant through the HTTP webhook endpoint (uses the real signed
payload from the happy-path test, then re-POSTs the same body+signature):

```go
func TestWebhook_HTTPReplay_CreditsOnce(t *testing.T) {
	// ... do the happy-path top-up + confirm, capture (rawBody, sigHeader)
	//     of the payment_intent.succeeded delivery ...
	// Re-POST the identical payload to /webhook/stripe.
	resp := postWebhook(t, rawBody, sigHeader)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("replay must return 2xx so Stripe stops retrying, got %d", resp.StatusCode)
	}
	if got := app.getWalletBalance(t, token); got != expectedAfterFirstCredit {
		t.Fatalf("MS-1: replay changed balance to %d", got)
	}
}
```

#### CT-2 — `ExecuteTransfer` is idempotent (MS-2, I2)
Call `ExecuteTransfer` twice on the same `pending` transfer (sequentially and
concurrently) and assert the sender is debited once.

```
GIVEN a pending transfer of 3000 from A (balance 10000) to B
WHEN  ExecuteTransfer is invoked twice for that transfer
THEN  A is debited exactly 3000 and B credited exactly 3000
AND   the transfer ends `completed`
AND   the second invocation is a no-op (returns the completed transfer or
      ErrAlreadyExecuted), not a second debit
```

```go
func TestExecuteTransfer_DoubleCall_AppliesOnce(t *testing.T) {
	// ... seed A=10000, B=0, a pending transfer t of 3000 ...
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = transfersService.ExecuteTransfer(ctx, transfer) }()
	}
	wg.Wait()
	if a := walletBalance(t, ctx, aWallet); a != 7000 {
		t.Fatalf("MS-2 violated: A=%d, want 7000 (double debit?)", a)
	}
	if b := walletBalance(t, ctx, bWallet); b != 3000 {
		t.Fatalf("MS-2 violated: B=%d, want 3000", b)
	}
}
```

#### CT-3 — Scheduled transfer executes exactly once under overlapping workers (MS-3, I5)
Simulate two scheduler runs hitting the same due transfer at the same time.

```
GIVEN a due scheduled transfer of 2000 from A (balance 5000) to B, still pending
WHEN  two scheduler passes run processScheduledTransfers concurrently
THEN  A is debited exactly 2000, B credited exactly 2000
AND   scheduled_transfers.executed_at is set exactly once
AND   the transfer is `completed` exactly once
```

```go
func TestScheduler_ConcurrentTicks_ExecuteOnce(t *testing.T) {
	// ... seed A=5000, B=0, a scheduled transfer due now, status pending ...
	sched := transfers.NewScheduler(transfersService, database, transfersRepo)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); sched.RunOnce(ctx) }() // expose the tick for testing
	}
	wg.Wait()
	if a := walletBalance(t, ctx, aWallet); a != 3000 {
		t.Fatalf("MS-3 violated: A=%d, want 3000 (paid twice?)", a)
	}
}
```

> Note for the rewrite: expose the tick body (e.g. `RunOnce(ctx) error`) so it is
> directly testable instead of only reachable through cron. Today
> `processScheduledTransfers` is private and time-driven (`scheduler.go:58`).

#### CT-4 — Concurrent debits never go negative (MS-4) — *covered, keep & strengthen*
Extend `TestConcurrentTransfers_ExceedBalance_Integration`: fire **K**
simultaneous transfers each equal to the full balance; assert at most one
succeeds, balance ≥ 0, and `money_out == money_in`. Run with `-race`.

#### CT-5 — Top-up request does not credit (MS-6)
`PATCH /wallet` returns 200 with a client secret but the balance is unchanged
until a succeeded webhook arrives. Assert balance == initial immediately after
the call and after a failed-payment webhook. (Partly covered by
`TestTopUp_PaymentFailed_Integration`; add the "no credit at request time"
assertion explicitly.)

#### CT-6 — Idempotency-Key replay on `/transfers` (MS-7) *(once implemented)*
Two identical `POST /transfers` with the same `Idempotency-Key` create one
transfer and move money once; the second returns the first result.

#### CT-7 — Self-transfer and concurrent self-transfer rejected (MS-5)
Concurrent transfers where `to_wallet == from_wallet` are all rejected and never
change the balance.

### Test hygiene
- All CT-* run under `go test -race`.
- Each CT asserts a **balance delta**, not an absolute value, so it is robust to
  seed changes.
- Each CT asserts the relevant invariant from §6 as a post-condition
  (e.g. `money_out == money_in`, `balance >= 0`, `executed_at` count == 1).
- Prefer service-level concurrency tests (deterministic, fast) for MS-1/2/3 and
  keep one HTTP/Stripe end-to-end test per flow as a smoke check.

---

## 8. Open questions

1. Dedup strategy for MS-1: processed-events table keyed on Stripe `event.ID`,
   on PaymentIntent id, or both? (event.ID is the most general — covers any
   event type we later handle.)
2. Do we need SERIALIZABLE isolation anywhere, or are conditional updates +
   `FOR UPDATE [SKIP LOCKED]` sufficient? (Current default is Read Committed via
   empty `pgx.TxOptions{}` in `db.go:26`.)
3. Scheduler ownership when running multiple replicas — DB-claim via SKIP LOCKED
   (preferred, no extra infra) vs. an advisory lock / leader election.
4. Idempotency-Key scope and TTL for MS-7.
