//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/google/uuid"
	stripeapi "github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"
)

// ---------------------------------------------------------------------------
// Helpers specific to the money-safety tests.
// ---------------------------------------------------------------------------

// balanceOf reads a balance straight from the database. Unlike getBalance it
// needs no token, so it works for receivers and for the Stripe system user.
func balanceOf(t *testing.T, userID uuid.UUID) int64 {
	t.Helper()

	var balance int64
	err := pgPool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).
		Scan(&balance)
	if err != nil {
		t.Fatalf("read balance of %s: %v", userID, err)
	}
	return balance
}

// seedBalance funds a user the way a completed top-up would: one transaction
// row from Stripe plus the two matching balance updates, all in one database
// transaction. It keeps SUM(balance) = 0 intact, so tests that only need a
// funded sender do not have to round-trip through the Stripe API.
func seedBalance(t *testing.T, userID uuid.UUID, amount int64) {
	t.Helper()

	ctx := context.Background()
	tx, err := pgPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed tx: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (sender_id, receiver_id, amount, status, receiver_category)
		VALUES ($1, $2, $3, 'completed', 'topup')`,
		users.SystemStripeID, userID, amount)
	if err != nil {
		t.Fatalf("seed transaction row: %v", err)
	}

	if _, err := tx.Exec(ctx,
		"UPDATE users SET balance = balance - $1 WHERE id = $2",
		amount, users.SystemStripeID,
	); err != nil {
		t.Fatalf("seed debit stripe: %v", err)
	}

	if _, err := tx.Exec(ctx,
		"UPDATE users SET balance = balance + $1 WHERE id = $2",
		amount, userID,
	); err != nil {
		t.Fatalf("seed credit user: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed tx: %v", err)
	}
}

// countCompletedBetween counts completed transactions running either way
// between two users, ignoring the seeded top-ups from the Stripe account.
func countCompletedBetween(t *testing.T, a, b uuid.UUID) int {
	t.Helper()

	var count int
	err := pgPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM transactions
		WHERE status = 'completed'
		  AND ((sender_id = $1 AND receiver_id = $2)
		    OR (sender_id = $2 AND receiver_id = $1))`,
		a, b,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count transactions between %s and %s: %v", a, b, err)
	}
	return count
}

// insertTransaction writes a transaction row in an arbitrary state so a test can
// stage a race without depending on the service that is being tested.
//
// updatedAt is set on INSERT deliberately: set_updated_at is a BEFORE UPDATE
// trigger, so an UPDATE can never backdate updated_at. Staging a stuck
// 'crediting' row for the sweeper has to happen at insert time.
func insertTransaction(
	t *testing.T,
	senderID, receiverID uuid.UUID,
	amount int64,
	status repo.TransactionStatus,
	scheduledAt, updatedAt time.Time,
) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pgPool.QueryRow(context.Background(), `
		INSERT INTO transactions
			(sender_id, receiver_id, amount, status, scheduled_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		RETURNING id`,
		senderID, receiverID, amount, status, scheduledAt, updatedAt,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	return id
}

// postStripeEvent delivers a correctly signed webhook event to the app. Tests
// synthesise events rather than confirming a real PaymentIntent so they control
// exactly how many deliveries happen -- which is the whole point of CT-1.
func (app *testApp) postStripeEvent(t *testing.T, eventType string, txID uuid.UUID) (int, string) {
	t.Helper()

	// api_version must be the one stripe-go was generated against: ConstructEvent
	// rejects a mismatch outright, and the handler reports that as a bad signature.
	payload := fmt.Sprintf(
		`{"id":"evt_test_%s","object":"event","api_version":"%s","type":"%s",`+
			`"data":{"object":{"id":"pi_test_%s","object":"payment_intent",`+
			`"metadata":{"transaction_id":"%s"}}}}`,
		uuid.New(), stripeapi.APIVersion, eventType, uuid.New(), txID,
	)

	now := time.Now()
	sig := webhook.ComputeSignature(now, []byte(payload), webhookSecret)
	header := fmt.Sprintf("t=%d,v1=%x", now.Unix(), sig)

	req, err := http.NewRequest(
		http.MethodPost,
		app.addr+"/webhook/stripe",
		strings.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("build webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", header)

	resp, err := app.httpClient.Do(req)
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	defer resp.Body.Close()

	body := make([]byte, 512)
	n, _ := resp.Body.Read(body)

	return resp.StatusCode, string(body[:n])
}

// startTopUp calls POST /transactions/topup and returns the pending
// transaction id. No money has moved when it returns.
func (app *testApp) startTopUp(t *testing.T, token string, amount int64) uuid.UUID {
	t.Helper()

	result, status, raw := app.topUp(t, token, amount)
	if status != http.StatusOK {
		t.Fatalf("POST /transactions/topup returned %d: %s", status, raw)
	}

	idStr, _ := result["transaction_id"].(string)
	id, err := uuid.Parse(idStr)
	if err != nil {
		t.Fatalf("topup returned no usable transaction_id: %s", raw)
	}
	return id
}

// pendingTopUp stages the row POST /transactions/topup would have written,
// without calling the Stripe API. Tests about what happens *after* the payment
// succeeds should not depend on a live Stripe account.
func pendingTopUp(t *testing.T, userID uuid.UUID, amount int64) uuid.UUID {
	t.Helper()

	now := time.Now()
	return insertTransaction(t, users.SystemStripeID, userID, amount,
		repo.TransactionStatusAwaitingPayment, now, now)
}

// requireStripe skips a test when the Stripe CLI or key is unusable. Only the
// two tests that create a real PaymentIntent need them.
func requireStripe(t *testing.T) {
	t.Helper()

	if err := setup(); err != nil {
		t.Skipf("needs a live Stripe test account: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CT-1 -- duplicate payment_intent.succeeded credits exactly once.
// This is the double-payment case: the highest-priority bug in the old code.
// ---------------------------------------------------------------------------

func TestCT1_DuplicateWebhook_Sequential_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance
	token := app.newUser(t, "ct1-seq")

	const amount = 5000
	txID := pendingTopUp(t, userIDFromToken(token), amount)

	// Three identical deliveries, exactly as Stripe would retry them.
	for i := range 3 {
		status, body := app.postStripeEvent(t, "payment_intent.succeeded", txID)
		if status < 200 || status > 299 {
			t.Fatalf("delivery %d returned %d, want 2xx: %s", i+1, status, body)
		}
	}

	if got := app.getBalance(t, token); got != amount {
		t.Errorf("DOUBLE CREDIT: balance = %d after 3 deliveries, want %d", got, amount)
	}
	if got := statusOf(t, txID); got != string(repo.TransactionStatusCompleted) {
		t.Errorf("status = %q, want %q", got, repo.TransactionStatusCompleted)
	}

	assertZeroSum(t)
}

func TestCT1_DuplicateWebhook_Concurrent_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance
	token := app.newUser(t, "ct1-conc")

	const (
		amount     = 5000
		deliveries = 8
	)
	txID := pendingTopUp(t, userIDFromToken(token), amount)

	// All deliveries released at once, so they contend on the guarded update.
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, deliveries)

	for i := range deliveries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes[i], _ = app.postStripeEvent(t, "payment_intent.succeeded", txID)
		}()
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code < 200 || code > 299 {
			t.Errorf("delivery %d returned %d, want 2xx", i, code)
		}
	}

	if got := app.getBalance(t, token); got != amount {
		t.Errorf("DOUBLE CREDIT: balance = %d after %d concurrent deliveries, want %d",
			got, deliveries, amount)
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// CT-2 -- Complete() twice on one transaction debits once.
// ---------------------------------------------------------------------------

func TestCT2_CompleteTwice_DebitsOnce_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	senderToken := app.newUser(t, "ct2-sender")
	receiverToken := app.newUser(t, "ct2-receiver")
	senderID := userIDFromToken(senderToken)
	receiverID := userIDFromToken(receiverToken)

	const (
		funded = 5000
		amount = 3000
	)
	seedBalance(t, senderID, funded)

	now := time.Now()
	txID := insertTransaction(t, senderID, receiverID, amount,
		repo.TransactionStatusScheduled, now, now)

	ctx := context.Background()

	if _, err := app.txService.Complete(ctx, txID, repo.TransactionStatusScheduled); err != nil {
		t.Fatalf("first Complete: %v", err)
	}

	// The second call finds zero rows to guard on. That is success, not failure.
	_, err := app.txService.Complete(ctx, txID, repo.TransactionStatusScheduled)
	if err == nil {
		t.Error("second Complete returned nil, want ErrAlreadyProcessed")
	} else if !errors.Is(err, transactions.ErrAlreadyProcessed) {
		t.Errorf("second Complete returned %v, want ErrAlreadyProcessed", err)
	}

	if got := balanceOf(t, senderID); got != funded-amount {
		t.Errorf("DOUBLE DEBIT: sender balance = %d, want %d", got, funded-amount)
	}
	if got := balanceOf(t, receiverID); got != amount {
		t.Errorf("DOUBLE CREDIT: receiver balance = %d, want %d", got, amount)
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// CT-3 -- two concurrent RunOnce execute one due transaction once.
// ---------------------------------------------------------------------------

func TestCT3_ConcurrentRunOnce_ExecutesOnce_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	senderToken := app.newUser(t, "ct3-sender")
	receiverToken := app.newUser(t, "ct3-receiver")
	senderID := userIDFromToken(senderToken)
	receiverID := userIDFromToken(receiverToken)

	const (
		funded = 5000
		amount = 3000
	)
	seedBalance(t, senderID, funded)

	due := time.Now().Add(-time.Minute)
	txID := insertTransaction(t, senderID, receiverID, amount,
		repo.TransactionStatusScheduled, due, due)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)

	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = app.scheduler.RunOnce(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	// Losing the claim is not an error: the loser's guarded update matches zero
	// rows and it moves on. Both ticks must report success.
	for i, err := range errs {
		if err != nil {
			t.Errorf("RunOnce %d returned %v, want nil", i, err)
		}
	}

	if got := statusOf(t, txID); got != string(repo.TransactionStatusCompleted) {
		t.Errorf("status = %q, want %q", got, repo.TransactionStatusCompleted)
	}
	if got := balanceOf(t, senderID); got != funded-amount {
		t.Errorf("DOUBLE EXECUTION: sender balance = %d, want %d", got, funded-amount)
	}
	if got := balanceOf(t, receiverID); got != amount {
		t.Errorf("DOUBLE EXECUTION: receiver balance = %d, want %d", got, amount)
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// CT-4 -- K concurrent transfers each equal to the full balance.
// ---------------------------------------------------------------------------

func TestCT4_ConcurrentFullBalanceTransfers_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	senderToken := app.newUser(t, "ct4-sender")
	senderID := userIDFromToken(senderToken)

	const (
		funded = 5000
		k      = 8
	)
	seedBalance(t, senderID, funded)

	receivers := make([]uuid.UUID, k)
	for i := range k {
		receivers[i] = userIDFromToken(app.newUser(t, fmt.Sprintf("ct4-receiver-%d", i)))
	}

	// Every request spends the entire balance, so only one can legally win.
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, k)

	for i := range k {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, status, _ := app.transfer(t, senderToken, receivers[i], funded)
			codes[i] = status
		}()
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for _, code := range codes {
		if code >= 200 && code <= 299 {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Errorf("OVERSPEND: %d of %d full-balance transfers succeeded, want exactly 1",
			succeeded, k)
	}

	senderBalance := balanceOf(t, senderID)
	if senderBalance < 0 {
		t.Errorf("NEGATIVE BALANCE: sender = %d", senderBalance)
	}

	var credited int64
	for _, id := range receivers {
		credited += balanceOf(t, id)
	}
	if debited := funded - senderBalance; debited != credited {
		t.Errorf("ATOMICITY BUG: debited %d but credited %d", debited, credited)
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// Two immediate transfers in opposite directions at the same instant.
//
// This is the deadlock case, and it is the only test that can catch a
// moveBalance which locks rows in whatever order the transfer happens to name
// them. A->B locks A then B while B->A locks B then A; each holds what the
// other wants and Postgres kills one with SQLSTATE 40P01. Applying the two
// balance updates in ascending user-id order makes the cycle impossible.
// ---------------------------------------------------------------------------

func TestConcurrentOppositeTransfers_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	aToken := app.newUser(t, "pingpong-a")
	bToken := app.newUser(t, "pingpong-b")
	aID := userIDFromToken(aToken)
	bID := userIDFromToken(bToken)

	const (
		funded = 10000
		amount = 1000
		rounds = 4 // 4 each way, so the balances must come back to where they started
	)
	seedBalance(t, aID, funded)
	seedBalance(t, bID, funded)

	// Half the goroutines push A->B and half push B->A, all released together so
	// the two lock orders genuinely interleave.
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, 2*rounds)

	for i := range 2 * rounds {
		wg.Add(1)
		go func() {
			defer wg.Done()

			token, to := aToken, bID
			if i%2 == 1 {
				token, to = bToken, aID
			}

			<-start
			_, status, _ := app.transfer(t, token, to, amount)
			codes[i] = status
		}()
	}
	close(start)
	wg.Wait()

	// A deadlock surfaces as a 500, so a non-2xx here is the failure signal.
	for i, code := range codes {
		if code < 200 || code > 299 {
			t.Errorf("DEADLOCK OR LOST TRANSFER: transfer %d returned %d, want 2xx", i, code)
		}
	}

	// Equal traffic both ways nets out to the starting balances.
	if got := balanceOf(t, aID); got != funded {
		t.Errorf("A balance = %d, want %d", got, funded)
	}
	if got := balanceOf(t, bID); got != funded {
		t.Errorf("B balance = %d, want %d", got, funded)
	}

	// Balances netting out is not enough on its own -- two transfers that both
	// silently failed in opposite directions would look identical. Count the rows.
	if got := countCompletedBetween(t, aID, bID); got != 2*rounds {
		t.Errorf("completed transactions = %d, want %d", got, 2*rounds)
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// Many immediate transfers landing on one receiver at the same instant.
// Catches a credit implemented as read-modify-write instead of
// balance = balance + n, which loses updates under contention.
// ---------------------------------------------------------------------------

func TestConcurrentTransfersToSameReceiver_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	receiverID := userIDFromToken(app.newUser(t, "fanin-receiver"))

	const (
		senders = 8
		amount  = 2000
	)

	tokens := make([]string, senders)
	for i := range senders {
		tokens[i] = app.newUser(t, fmt.Sprintf("fanin-sender-%d", i))
		seedBalance(t, userIDFromToken(tokens[i]), amount)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, senders)

	for i := range senders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, status, _ := app.transfer(t, tokens[i], receiverID, amount)
			codes[i] = status
		}()
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code < 200 || code > 299 {
			t.Errorf("transfer %d returned %d, want 2xx", i, code)
		}
	}

	if got := balanceOf(t, receiverID); got != senders*amount {
		t.Errorf("LOST UPDATE: receiver balance = %d, want %d", got, senders*amount)
	}
	for i, token := range tokens {
		if got := balanceOf(t, userIDFromToken(token)); got != 0 {
			t.Errorf("sender %d balance = %d, want 0", i, got)
		}
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// CT-5 -- creating a top-up moves no money. The balance changes only when the
// webhook lands.
// ---------------------------------------------------------------------------

func TestCT5_TopUpDoesNotCredit_Integration(t *testing.T) {
	requireStripe(t)
	defer teardown()

	app := testAppInstance
	token := app.newUser(t, "ct5")

	before := app.getBalance(t, token)
	txID := app.startTopUp(t, token, 5000)

	if after := app.getBalance(t, token); after != before {
		t.Errorf("PREMATURE CREDIT: balance moved from %d to %d on top-up creation",
			before, after)
	}
	if got := statusOf(t, txID); got != string(repo.TransactionStatusAwaitingPayment) {
		t.Errorf("status = %q, want %q", got, repo.TransactionStatusAwaitingPayment)
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// CT-7 -- concurrent self-transfers are all rejected and move nothing.
// ---------------------------------------------------------------------------

func TestCT7_ConcurrentSelfTransfers_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	token := app.newUser(t, "ct7")
	userID := userIDFromToken(token)

	const (
		funded = 5000
		k      = 8
	)
	seedBalance(t, userID, funded)

	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, k)

	for i := range k {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, status, _ := app.transfer(t, token, userID, 1000)
			codes[i] = status
		}()
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code < 400 || code > 499 {
			t.Errorf("self-transfer %d returned %d, want 4xx", i, code)
		}
	}

	if got := balanceOf(t, userID); got != funded {
		t.Errorf("SELF-TRANSFER MOVED MONEY: balance = %d, want %d", got, funded)
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// Sweeper -- a row abandoned in 'crediting' is finished exactly once.
// ---------------------------------------------------------------------------

func TestSweeper_FinishesStuckCrediting_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	token := app.newUser(t, "sweep")
	userID := userIDFromToken(token)

	// A top-up whose worker died between claiming 'crediting' and committing the
	// credit. Ten minutes old, so it is past the sweeper's five-minute threshold.
	const amount = 4000
	stale := time.Now().Add(-10 * time.Minute)
	txID := insertTransaction(t, users.SystemStripeID, userID, amount,
		repo.TransactionStatusCrediting, stale, stale)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)

	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = app.scheduler.SweepOnce(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("SweepOnce %d returned %v, want nil", i, err)
		}
	}

	if got := statusOf(t, txID); got != string(repo.TransactionStatusCompleted) {
		t.Errorf("status = %q, want %q", got, repo.TransactionStatusCompleted)
	}
	if got := balanceOf(t, userID); got != amount {
		t.Errorf("DOUBLE SWEEP: balance = %d, want %d", got, amount)
	}

	assertZeroSum(t)
}

// TestSweeper_LeavesFreshCrediting_Integration guards the other direction: a row
// that entered 'crediting' a moment ago belongs to a live worker and must not be
// stolen. It only passes because set_updated_at stamps updated_at on the status
// change -- without the trigger every fresh row looks stale.
func TestSweeper_LeavesFreshCrediting_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	token := app.newUser(t, "sweep-fresh")
	userID := userIDFromToken(token)

	// Created long ago but moved into 'crediting' just now.
	old := time.Now().Add(-time.Hour)
	txID := insertTransaction(t, users.SystemStripeID, userID, 4000,
		repo.TransactionStatusAwaitingPayment, old, old)

	if _, err := app.txService.Transition(
		context.Background(),
		txID,
		repo.TransactionStatusAwaitingPayment,
		repo.TransactionStatusCrediting,
	); err != nil {
		t.Fatalf("transition to crediting: %v", err)
	}

	if err := app.scheduler.SweepOnce(context.Background()); err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}

	if got := statusOf(t, txID); got != string(repo.TransactionStatusCrediting) {
		t.Errorf("STOLEN FROM LIVE WORKER: status = %q, want %q",
			got, repo.TransactionStatusCrediting)
	}
	if got := balanceOf(t, userID); got != 0 {
		t.Errorf("STOLEN FROM LIVE WORKER: balance = %d, want 0", got)
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// Cancel racing the scheduler -- either the row is gone or it completed, never
// both effects and never a double debit.
// ---------------------------------------------------------------------------

func TestCancelRacesRunOnce_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	senderToken := app.newUser(t, "cancel-sender")
	receiverToken := app.newUser(t, "cancel-receiver")
	senderID := userIDFromToken(senderToken)
	receiverID := userIDFromToken(receiverToken)

	const (
		funded = 5000
		amount = 3000
	)
	seedBalance(t, senderID, funded)

	due := time.Now().Add(-time.Minute)
	txID := insertTransaction(t, senderID, receiverID, amount,
		repo.TransactionStatusScheduled, due, due)

	var wg sync.WaitGroup
	start := make(chan struct{})

	var cancelStatus int
	var runErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, status, _ := app.do(t, http.MethodDelete, "/transactions/"+txID.String(), senderToken, nil)
		cancelStatus = status
	}()
	go func() {
		defer wg.Done()
		<-start
		runErr = app.scheduler.RunOnce(context.Background())
	}()
	close(start)
	wg.Wait()

	if runErr != nil {
		t.Errorf("RunOnce returned %v, want nil", runErr)
	}

	var exists bool
	if err := pgPool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM transactions WHERE id = $1)", txID,
	).Scan(&exists); err != nil {
		t.Fatalf("check row existence: %v", err)
	}

	senderBalance := balanceOf(t, senderID)
	receiverBalance := balanceOf(t, receiverID)

	switch {
	case !exists:
		// Cancel won: the row is gone and no money may have moved.
		if cancelStatus < 200 || cancelStatus > 299 {
			t.Errorf("row was deleted but DELETE returned %d", cancelStatus)
		}
		if senderBalance != funded || receiverBalance != 0 {
			t.Errorf("CANCEL LEAKED MONEY: sender = %d (want %d), receiver = %d (want 0)",
				senderBalance, funded, receiverBalance)
		}
	default:
		// The scheduler won: the transfer executed exactly once and the cancel
		// must have been refused.
		if got := statusOf(t, txID); got != string(repo.TransactionStatusCompleted) {
			t.Errorf("row survived with status %q, want %q",
				got, repo.TransactionStatusCompleted)
		}
		if cancelStatus >= 200 && cancelStatus <= 299 {
			t.Errorf("BOTH EFFECTS: transfer completed but DELETE also returned %d",
				cancelStatus)
		}
		if senderBalance != funded-amount || receiverBalance != amount {
			t.Errorf("sender = %d (want %d), receiver = %d (want %d)",
				senderBalance, funded-amount, receiverBalance, amount)
		}
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// Ported from the old suite: concurrent transfers to distinct receivers must
// stay atomic. Now also asserts the global zero-sum invariant.
// ---------------------------------------------------------------------------

func TestConcurrentTransfers_Integration(t *testing.T) {
	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	senderToken := app.newUser(t, "conc-sender")
	receiver1Token := app.newUser(t, "conc-receiver1")
	receiver2Token := app.newUser(t, "conc-receiver2")

	senderID := userIDFromToken(senderToken)
	receiver1ID := userIDFromToken(receiver1Token)
	receiver2ID := userIDFromToken(receiver2Token)

	const (
		funded = 20000
		amount = 3000
	)
	seedBalance(t, senderID, funded)

	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, 2)

	for i, to := range []uuid.UUID{receiver1ID, receiver2ID} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, status, _ := app.transfer(t, senderToken, to, amount)
			codes[i] = status
		}()
	}
	close(start)
	wg.Wait()

	// The sender can afford both, so both must land. Without this the atomicity
	// check below passes vacuously when no transfer succeeds at all.
	for i, code := range codes {
		if code < 200 || code > 299 {
			t.Errorf("transfer %d returned %d, want 2xx", i, code)
		}
	}

	senderBalance := balanceOf(t, senderID)
	totalOut := funded - senderBalance
	totalIn := balanceOf(t, receiver1ID) + balanceOf(t, receiver2ID)

	if totalOut != 2*amount {
		t.Errorf("total moved = %d, want %d", totalOut, 2*amount)
	}

	if totalOut != totalIn {
		t.Errorf("ATOMICITY BUG: money out (%d) != money in (%d)", totalOut, totalIn)
	}
	if senderBalance < 0 {
		t.Errorf("NEGATIVE BALANCE: sender = %d", senderBalance)
	}

	assertZeroSum(t)
}

// ---------------------------------------------------------------------------
// End-to-end top-up through the real Stripe CLI, kept as the one test that
// exercises the whole payment path rather than a synthesised event.
// ---------------------------------------------------------------------------

func TestTopUp_HappyPath_Integration(t *testing.T) {
	requireStripe(t)
	defer teardown()

	app := testAppInstance
	token := app.newUser(t, "topup-happy")

	const amount = 5000
	app.fund(t, token, amount)

	if got := app.getBalance(t, token); got != amount {
		t.Errorf("balance = %d, want %d", got, amount)
	}
	if got := balanceOf(t, users.SystemStripeID); got != -amount {
		t.Errorf("stripe clearing balance = %d, want %d", got, -amount)
	}

	assertZeroSum(t)
}
