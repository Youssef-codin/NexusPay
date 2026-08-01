package transactions

import (
	"context"
	"errors"
	"testing"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/payment"
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type fixture struct {
	svc     *Service
	txMgr   *MockTxManager
	repo    *MockRepo
	users   *MockUserService
	payment *MockPaymentService
}

func newFixture() *fixture {
	f := &fixture{
		txMgr:   newMockTxManager(),
		repo:    &MockRepo{},
		users:   &MockUserService{},
		payment: &MockPaymentService{},
	}
	f.svc = &Service{
		txManager:  f.txMgr,
		repo:       f.repo,
		userSvc:    f.users,
		paymentSvc: f.payment,
	}
	return f
}

func txRow(id, sender, receiver uuid.UUID, amount int64, status repo.TransactionStatus) repo.Transaction {
	return repo.Transaction{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		SenderID:    pgtype.UUID{Bytes: sender, Valid: true},
		ReceiverID:  pgtype.UUID{Bytes: receiver, Valid: true},
		Amount:      amount,
		Status:      status,
		ScheduledAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

// ---------------------------------------------------------------------------
// Complete -- the guarded update
// ---------------------------------------------------------------------------

// The zero-row guard is the whole rewrite: losing the race is a no-op that the
// caller treats as success, not an error condition.
func TestComplete_ZeroRowGuard_IsNoOp(t *testing.T) {
	f := newFixture()
	id := uuid.New()

	f.repo.On("GuardedSetStatus", mock.Anything, mock.Anything).
		Return(repo.Transaction{}, pgx.ErrNoRows)

	_, err := f.svc.Complete(context.Background(), id, repo.TransactionStatusScheduled)

	assert.ErrorIs(t, err, ErrAlreadyProcessed)
	assert.False(t, f.txMgr.tx.commitCalled, "must not commit when the guard matched nothing")
	f.users.AssertNotCalled(t, "Debit", mock.Anything, mock.Anything)
	f.users.AssertNotCalled(t, "Credit", mock.Anything, mock.Anything)
}

func TestComplete_MovesMoneyAndCommits(t *testing.T) {
	f := newFixture()
	id, sender, receiver := uuid.New(), uuid.New(), uuid.New()
	row := txRow(id, sender, receiver, 3000, repo.TransactionStatusCompleted)

	f.repo.On("GuardedSetStatus", mock.Anything, repo.GuardedSetStatusParams{
		ID:         pgtype.UUID{Bytes: id, Valid: true},
		FromStatus: repo.TransactionStatusScheduled,
		ToStatus:   repo.TransactionStatusCompleted,
	}).Return(row, nil)

	f.users.On("Debit", mock.Anything, users.BalanceRequest{UserID: sender, Amount: 3000}).
		Return(users.BalanceResponse{}, nil)
	f.users.On("Credit", mock.Anything, users.BalanceRequest{UserID: receiver, Amount: 3000}).
		Return(users.BalanceResponse{}, nil)

	got, err := f.svc.Complete(context.Background(), id, repo.TransactionStatusScheduled)

	assert.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)
	assert.True(t, f.txMgr.tx.commitCalled)
	f.users.AssertExpectations(t)
}

// Insufficient funds must roll the whole thing back -- so nothing moved and the
// status guard is undone -- and then park the row as failed in its own update.
func TestComplete_InsufficientFunds_RollsBackThenMarksFailed(t *testing.T) {
	f := newFixture()
	id, sender, receiver := uuid.New(), uuid.New(), uuid.New()
	row := txRow(id, sender, receiver, 3000, repo.TransactionStatusCompleted)

	f.repo.On("GuardedSetStatus", mock.Anything, repo.GuardedSetStatusParams{
		ID:         pgtype.UUID{Bytes: id, Valid: true},
		FromStatus: repo.TransactionStatusScheduled,
		ToStatus:   repo.TransactionStatusCompleted,
	}).Return(row, nil).Once()

	f.users.On("Debit", mock.Anything, mock.Anything).
		Return(users.BalanceResponse{}, users.ErrInsufficientFunds)
	f.users.On("Credit", mock.Anything, mock.Anything).
		Return(users.BalanceResponse{}, nil).Maybe()

	// The separate guarded update that parks the row.
	failParams := repo.GuardedSetStatusParams{
		ID:         pgtype.UUID{Bytes: id, Valid: true},
		FromStatus: repo.TransactionStatusScheduled,
		ToStatus:   repo.TransactionStatusFailed,
	}
	f.repo.On("GuardedSetStatus", mock.Anything, failParams).
		Return(txRow(id, sender, receiver, 3000, repo.TransactionStatusFailed), nil).Once()

	_, err := f.svc.Complete(context.Background(), id, repo.TransactionStatusScheduled)

	assert.ErrorIs(t, err, ErrInsufficientFunds)
	assert.False(t, f.txMgr.tx.commitCalled, "money-moving tx must not commit")
	assert.True(t, f.txMgr.tx.rollbackCalled)
	f.repo.AssertCalled(t, "GuardedSetStatus", mock.Anything, failParams)
}

// moveBalance orders the two updates by user id so that simultaneous A->B and
// B->A transfers cannot take each other's row locks in opposite orders.
func TestComplete_AppliesBalanceUpdatesInIDOrder(t *testing.T) {
	low := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	high := uuid.MustParse("ffffffff-0000-0000-0000-000000000000")

	tests := []struct {
		name             string
		sender, receiver uuid.UUID
		wantOrder        []string
	}{
		{"sender sorts first", low, high, []string{"debit", "credit"}},
		{"receiver sorts first", high, low, []string{"credit", "debit"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			id := uuid.New()

			f.repo.On("GuardedSetStatus", mock.Anything, mock.Anything).
				Return(txRow(id, tc.sender, tc.receiver, 1000, repo.TransactionStatusCompleted), nil)
			f.users.On("Debit", mock.Anything, mock.Anything).
				Return(users.BalanceResponse{}, nil)
			f.users.On("Credit", mock.Anything, mock.Anything).
				Return(users.BalanceResponse{}, nil)

			_, err := f.svc.Complete(context.Background(), id, repo.TransactionStatusCrediting)

			assert.NoError(t, err)
			assert.Equal(t, tc.wantOrder, f.users.calls)
		})
	}
}

// ---------------------------------------------------------------------------
// Transition -- guarded, no money
// ---------------------------------------------------------------------------

func TestTransition_ZeroRows_IsAlreadyProcessed(t *testing.T) {
	f := newFixture()

	f.repo.On("GuardedSetStatus", mock.Anything, mock.Anything).
		Return(repo.Transaction{}, pgx.ErrNoRows)

	_, err := f.svc.Transition(
		context.Background(),
		uuid.New(),
		repo.TransactionStatusAwaitingPayment,
		repo.TransactionStatusCrediting,
	)

	assert.ErrorIs(t, err, ErrAlreadyProcessed)
	f.users.AssertNotCalled(t, "Debit", mock.Anything, mock.Anything)
	f.users.AssertNotCalled(t, "Credit", mock.Anything, mock.Anything)
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestCreate_SelfTransferRejected(t *testing.T) {
	f := newFixture()
	me := uuid.New()
	ctx := withUserID(context.Background(), me.String())

	_, err := f.svc.Create(ctx, CreateTransactionRequest{ReceiverID: me, Amount: 5000})

	assert.ErrorIs(t, err, ErrSelfTransfer)
	f.repo.AssertNotCalled(t, "CreateTransaction", mock.Anything, mock.Anything)
}

func TestCreate_AmountBelowMinimumRejected(t *testing.T) {
	f := newFixture()
	ctx := withUserID(context.Background(), uuid.New().String())

	_, err := f.svc.Create(ctx, CreateTransactionRequest{
		ReceiverID: uuid.New(),
		Amount:     MinAmount - 1,
	})

	assert.ErrorIs(t, err, ErrAmountIsTooLow)
	f.repo.AssertNotCalled(t, "CreateTransaction", mock.Anything, mock.Anything)
}

// A scheduled transfer is inserted and left alone: no balance may move until the
// scheduler picks it up.
func TestCreate_Scheduled_DoesNotMoveMoney(t *testing.T) {
	f := newFixture()
	me, receiver, id := uuid.New(), uuid.New(), uuid.New()
	ctx := withUserID(context.Background(), me.String())
	at := time.Now().Add(time.Hour)

	f.users.On("GetByID", mock.Anything, receiver).Return(users.GetMeResponse{ID: receiver}, nil)
	f.repo.On("CreateTransaction", mock.Anything, mock.MatchedBy(
		func(arg repo.CreateTransactionParams) bool {
			return arg.Status == repo.TransactionStatusScheduled
		},
	)).Return(txRow(id, me, receiver, 5000, repo.TransactionStatusScheduled), nil)

	f.repo.On("GetTransactionByIdWithUsers", mock.Anything, mock.Anything).
		Return(repo.GetTransactionByIdWithUsersRow{
			ID:         pgtype.UUID{Bytes: id, Valid: true},
			SenderID:   pgtype.UUID{Bytes: me, Valid: true},
			ReceiverID: pgtype.UUID{Bytes: receiver, Valid: true},
			Amount:     5000,
			Status:     repo.TransactionStatusScheduled,
		}, nil)

	got, err := f.svc.Create(ctx, CreateTransactionRequest{
		ReceiverID:  receiver,
		Amount:      5000,
		ScheduledAt: &at,
	})

	assert.NoError(t, err)
	assert.Equal(t, repo.TransactionStatusScheduled, got.Status)
	f.users.AssertNotCalled(t, "Debit", mock.Anything, mock.Anything)
	f.users.AssertNotCalled(t, "Credit", mock.Anything, mock.Anything)
}

// The immediate path inserts 'crediting' and commits before moving money, so a
// crash leaves a row the sweeper can finish.
func TestCreate_Immediate_InsertsCreditingThenCompletes(t *testing.T) {
	f := newFixture()
	me, receiver, id := uuid.New(), uuid.New(), uuid.New()
	ctx := withUserID(context.Background(), me.String())

	f.users.On("GetByID", mock.Anything, receiver).Return(users.GetMeResponse{ID: receiver}, nil)
	f.repo.On("CreateTransaction", mock.Anything, mock.MatchedBy(
		func(arg repo.CreateTransactionParams) bool {
			return arg.Status == repo.TransactionStatusCrediting
		},
	)).Return(txRow(id, me, receiver, 5000, repo.TransactionStatusCrediting), nil)

	f.repo.On("GuardedSetStatus", mock.Anything, repo.GuardedSetStatusParams{
		ID:         pgtype.UUID{Bytes: id, Valid: true},
		FromStatus: repo.TransactionStatusCrediting,
		ToStatus:   repo.TransactionStatusCompleted,
	}).Return(txRow(id, me, receiver, 5000, repo.TransactionStatusCompleted), nil)

	f.users.On("Debit", mock.Anything, mock.Anything).Return(users.BalanceResponse{}, nil)
	f.users.On("Credit", mock.Anything, mock.Anything).Return(users.BalanceResponse{}, nil)

	f.repo.On("GetTransactionByIdWithUsers", mock.Anything, mock.Anything).
		Return(repo.GetTransactionByIdWithUsersRow{
			ID:         pgtype.UUID{Bytes: id, Valid: true},
			SenderID:   pgtype.UUID{Bytes: me, Valid: true},
			ReceiverID: pgtype.UUID{Bytes: receiver, Valid: true},
			Amount:     5000,
			Status:     repo.TransactionStatusCompleted,
		}, nil)

	got, err := f.svc.Create(ctx, CreateTransactionRequest{ReceiverID: receiver, Amount: 5000})

	assert.NoError(t, err)
	assert.Equal(t, repo.TransactionStatusCompleted, got.Status)
	assert.True(t, f.txMgr.tx.commitCalled)
	f.users.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

func TestCancel_Scheduled_Deletes(t *testing.T) {
	f := newFixture()
	me, id := uuid.New(), uuid.New()
	ctx := withUserID(context.Background(), me.String())

	f.repo.On("CancelScheduledTransaction", mock.Anything, repo.CancelScheduledTransactionParams{
		ID:       pgtype.UUID{Bytes: id, Valid: true},
		SenderID: pgtype.UUID{Bytes: me, Valid: true},
	}).Return(txRow(id, me, uuid.New(), 5000, repo.TransactionStatusScheduled), nil)

	got, err := f.svc.Cancel(ctx, id)

	assert.NoError(t, err)
	assert.Equal(t, id, got.CancelledID)
}

// A delete that matched nothing is classified by a follow-up read. The read only
// picks the error code; it is never load-bearing for the race itself.
func TestCancel_AlreadyExecuted_IsNotScheduled(t *testing.T) {
	f := newFixture()
	me, id := uuid.New(), uuid.New()
	ctx := withUserID(context.Background(), me.String())

	f.repo.On("CancelScheduledTransaction", mock.Anything, mock.Anything).
		Return(repo.Transaction{}, pgx.ErrNoRows)
	f.repo.On("GetTransactionById", mock.Anything, pgtype.UUID{Bytes: id, Valid: true}).
		Return(txRow(id, me, uuid.New(), 5000, repo.TransactionStatusCompleted), nil)

	_, err := f.svc.Cancel(ctx, id)

	assert.ErrorIs(t, err, ErrNotScheduled)
}

func TestCancel_SomebodyElsesTransaction_IsWrongOwnership(t *testing.T) {
	f := newFixture()
	me, owner, id := uuid.New(), uuid.New(), uuid.New()
	ctx := withUserID(context.Background(), me.String())

	f.repo.On("CancelScheduledTransaction", mock.Anything, mock.Anything).
		Return(repo.Transaction{}, pgx.ErrNoRows)
	f.repo.On("GetTransactionById", mock.Anything, mock.Anything).
		Return(txRow(id, owner, uuid.New(), 5000, repo.TransactionStatusScheduled), nil)

	_, err := f.svc.Cancel(ctx, id)

	assert.ErrorIs(t, err, ErrWrongOwnership)
}

func TestCancel_MissingTransaction_IsNotFound(t *testing.T) {
	f := newFixture()
	ctx := withUserID(context.Background(), uuid.New().String())

	f.repo.On("CancelScheduledTransaction", mock.Anything, mock.Anything).
		Return(repo.Transaction{}, pgx.ErrNoRows)
	f.repo.On("GetTransactionById", mock.Anything, mock.Anything).
		Return(repo.Transaction{}, pgx.ErrNoRows)

	_, err := f.svc.Cancel(ctx, uuid.New())

	assert.ErrorIs(t, err, ErrTransactionNotFound)
}

// ---------------------------------------------------------------------------
// SetCategory
// ---------------------------------------------------------------------------

func TestSetCategory_PicksColumnFromCallersSide(t *testing.T) {
	sender, receiver, id := uuid.New(), uuid.New(), uuid.New()

	tests := []struct {
		name       string
		caller     uuid.UUID
		wantMethod string
	}{
		{"sender writes sender_category", sender, "SetSenderCategory"},
		{"receiver writes receiver_category", receiver, "SetReceiverCategory"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			ctx := withUserID(context.Background(), tc.caller.String())

			f.repo.On("GetTransactionById", mock.Anything, mock.Anything).
				Return(txRow(id, sender, receiver, 5000, repo.TransactionStatusCompleted), nil)
			f.repo.On(tc.wantMethod, mock.Anything, mock.Anything).
				Return(txRow(id, sender, receiver, 5000, repo.TransactionStatusCompleted), nil)
			f.repo.On("GetTransactionByIdWithUsers", mock.Anything, mock.Anything).
				Return(repo.GetTransactionByIdWithUsersRow{
					ID:         pgtype.UUID{Bytes: id, Valid: true},
					SenderID:   pgtype.UUID{Bytes: sender, Valid: true},
					ReceiverID: pgtype.UUID{Bytes: receiver, Valid: true},
				}, nil)

			_, err := f.svc.SetCategory(ctx, SetCategoryRequest{
				ID:       id,
				Category: repo.ExpenseCategoryFood,
			})

			assert.NoError(t, err)
			f.repo.AssertCalled(t, tc.wantMethod, mock.Anything, mock.Anything)
		})
	}
}

func TestSetCategory_ThirdParty_IsWrongOwnership(t *testing.T) {
	f := newFixture()
	id := uuid.New()
	ctx := withUserID(context.Background(), uuid.New().String())

	f.repo.On("GetTransactionById", mock.Anything, mock.Anything).
		Return(txRow(id, uuid.New(), uuid.New(), 5000, repo.TransactionStatusCompleted), nil)

	_, err := f.svc.SetCategory(ctx, SetCategoryRequest{
		ID:       id,
		Category: repo.ExpenseCategoryFood,
	})

	assert.ErrorIs(t, err, ErrWrongOwnership)
}

// ---------------------------------------------------------------------------
// TopUp
// ---------------------------------------------------------------------------

// Creating the PaymentIntent must not credit anything. The balance moves only
// when the webhook lands.
func TestTopUp_DoesNotCreditAtRequestTime(t *testing.T) {
	f := newFixture()
	me, id := uuid.New(), uuid.New()
	ctx := withUserID(context.Background(), me.String())

	f.repo.On("CreateTransaction", mock.Anything, mock.MatchedBy(
		func(arg repo.CreateTransactionParams) bool {
			return arg.Status == repo.TransactionStatusAwaitingPayment &&
				uuid.UUID(arg.SenderID.Bytes) == users.SystemStripeID
		},
	)).Return(txRow(id, users.SystemStripeID, me, 5000, repo.TransactionStatusAwaitingPayment), nil)

	f.payment.On("ProcessPayment", mock.Anything, mock.Anything).
		Return(payment.ProcessPaymentResponse{
			ProviderPaymentID: "pi_123",
			ClientSecret:      "secret",
		}, nil)

	got, err := f.svc.TopUp(ctx, TopUpRequest{Amount: 5000})

	assert.NoError(t, err)
	assert.Equal(t, repo.TransactionStatusAwaitingPayment, got.Status)
	assert.Equal(t, "pi_123", got.ProviderPaymentID)
	f.users.AssertNotCalled(t, "Credit", mock.Anything, mock.Anything)
	f.users.AssertNotCalled(t, "Debit", mock.Anything, mock.Anything)
}

// No PaymentIntent means no webhook will ever arrive, so the row must not be
// left waiting for a payment that cannot happen.
func TestTopUp_PaymentProviderFails_MarksFailed(t *testing.T) {
	f := newFixture()
	me, id := uuid.New(), uuid.New()
	ctx := withUserID(context.Background(), me.String())

	f.repo.On("CreateTransaction", mock.Anything, mock.Anything).
		Return(txRow(id, users.SystemStripeID, me, 5000, repo.TransactionStatusAwaitingPayment), nil)
	f.payment.On("ProcessPayment", mock.Anything, mock.Anything).
		Return(payment.ProcessPaymentResponse{}, errors.New("stripe is down"))

	failParams := repo.GuardedSetStatusParams{
		ID:         pgtype.UUID{Bytes: id, Valid: true},
		FromStatus: repo.TransactionStatusAwaitingPayment,
		ToStatus:   repo.TransactionStatusFailed,
	}
	f.repo.On("GuardedSetStatus", mock.Anything, failParams).
		Return(txRow(id, users.SystemStripeID, me, 5000, repo.TransactionStatusFailed), nil)

	_, err := f.svc.TopUp(ctx, TopUpRequest{Amount: 5000})

	assert.Error(t, err)
	f.repo.AssertCalled(t, "GuardedSetStatus", mock.Anything, failParams)
}

func TestTopUp_BelowMinimumRejected(t *testing.T) {
	f := newFixture()
	ctx := withUserID(context.Background(), uuid.New().String())

	_, err := f.svc.TopUp(ctx, TopUpRequest{Amount: MinAmount - 1})

	assert.ErrorIs(t, err, ErrAmountIsTooLow)
	f.repo.AssertNotCalled(t, "CreateTransaction", mock.Anything, mock.Anything)
}

// ---------------------------------------------------------------------------
// GetByID / List
// ---------------------------------------------------------------------------

func TestGetByID_ThirdParty_IsWrongOwnership(t *testing.T) {
	f := newFixture()
	id := uuid.New()
	ctx := withUserID(context.Background(), uuid.New().String())

	f.repo.On("GetTransactionByIdWithUsers", mock.Anything, mock.Anything).
		Return(repo.GetTransactionByIdWithUsersRow{
			ID:         pgtype.UUID{Bytes: id, Valid: true},
			SenderID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
			ReceiverID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
		}, nil)

	_, err := f.svc.GetByID(ctx, id)

	assert.ErrorIs(t, err, ErrWrongOwnership)
}

func TestList_MarksDirectionFromViewer(t *testing.T) {
	f := newFixture()
	me, other := uuid.New(), uuid.New()
	ctx := withUserID(context.Background(), me.String())

	f.repo.On("GetTransactionsByUserId", mock.Anything, pgtype.UUID{Bytes: me, Valid: true}).
		Return([]repo.GetTransactionsByUserIdRow{
			{
				ID:         pgtype.UUID{Bytes: uuid.New(), Valid: true},
				SenderID:   pgtype.UUID{Bytes: me, Valid: true},
				ReceiverID: pgtype.UUID{Bytes: other, Valid: true},
			},
			{
				ID:         pgtype.UUID{Bytes: uuid.New(), Valid: true},
				SenderID:   pgtype.UUID{Bytes: other, Valid: true},
				ReceiverID: pgtype.UUID{Bytes: me, Valid: true},
			},
		}, nil)

	got, err := f.svc.List(ctx)

	assert.NoError(t, err)
	assert.Len(t, got.Transactions, 2)
	assert.Equal(t, "debit", got.Transactions[0].Direction)
	assert.Equal(t, "credit", got.Transactions[1].Direction)
}
