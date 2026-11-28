package transactions

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/payment"
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrNotImplemented      = errors.New("not implemented")
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrBadRequest          = errors.New("bad request")
	ErrSelfTransfer        = errors.New("can not transfer to self")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrWrongOwnership      = errors.New("transaction belongs to somebody else")
	ErrNotScheduled        = errors.New("transaction is no longer scheduled")
	ErrAmountIsTooLow      = errors.New(
		"amount is too low, must be at least 10 EGP (1000 piastres)",
	)
	// ErrAlreadyProcessed means a guarded update matched zero rows: another
	// worker got there first. It is SUCCESS for every caller -- that single
	// fact is what makes the webhook idempotent, Complete idempotent and
	// scheduled transfers claim-once.
	ErrAlreadyProcessed = errors.New("transaction already processed")
)

type IService interface {
	Create(ctx context.Context, req CreateTransactionRequest) (TransactionResponse, error)
	List(ctx context.Context) (ListTransactionsResponse, error)
	GetByID(ctx context.Context, id uuid.UUID) (GetTransactionResponse, error)
	Cancel(ctx context.Context, id uuid.UUID) (CancelTransactionResponse, error)
	SetCategory(ctx context.Context, req SetCategoryRequest) (GetTransactionResponse, error)
	TopUp(ctx context.Context, req TopUpRequest) (TopUpResponse, error)

	// Complete is the one function that moves money. The webhook, immediate
	// transfers, the scheduler and the sweeper all call this and nothing else
	// moves a balance.
	Complete(
		ctx context.Context,
		id uuid.UUID,
		from repo.TransactionStatus,
	) (repo.Transaction, error)
	// Transition is a guarded status change that touches no balance: the
	// webhook's awaiting_payment -> crediting claim and the failure paths.
	Transition(
		ctx context.Context,
		id uuid.UUID,
		from, to repo.TransactionStatus,
	) (repo.Transaction, error)
}

type Service struct {
	txManager  db.TxManager
	repo       transactionRepo
	userSvc    users.IService
	paymentSvc payment.IService
}

func NewService(
	txManager db.TxManager,
	repo transactionRepo,
	userSvc users.IService,
	paymentSvc payment.IService,
) IService {
	return &Service{
		txManager:  txManager,
		repo:       repo,
		userSvc:    userSvc,
		paymentSvc: paymentSvc,
	}
}

// callerID pulls the authenticated user out of the JWT claims.
func callerID(ctx context.Context) (uuid.UUID, error) {
	sub, err := api.GetTokenUserID(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	id, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, ErrBadRequest
	}
	return id, nil
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// Create either schedules a transfer or performs it now. The immediate path
// inserts the row as 'crediting' and commits before moving any money, so a
// crash between the two leaves a row the sweeper can finish -- an accepted
// transfer is never silently lost.
func (svc *Service) Create(
	ctx context.Context,
	req CreateTransactionRequest,
) (TransactionResponse, error) {
	senderID, err := callerID(ctx)
	if err != nil {
		return TransactionResponse{}, err
	}

	if req.ReceiverID == senderID {
		return TransactionResponse{}, ErrSelfTransfer
	}
	if req.Amount < MinAmount {
		return TransactionResponse{}, ErrAmountIsTooLow
	}

	// Fail with a clean 404 rather than a foreign-key violation.
	if _, err := svc.userSvc.GetByID(ctx, req.ReceiverID); err != nil {
		return TransactionResponse{}, err
	}

	scheduled := req.ScheduledAt != nil && req.ScheduledAt.After(time.Now())

	status := repo.TransactionStatusCrediting
	scheduledAt := time.Now()
	if scheduled {
		status = repo.TransactionStatusScheduled
		scheduledAt = *req.ScheduledAt
	}

	created, err := svc.repo.CreateTransaction(ctx, repo.CreateTransactionParams{
		SenderID:    pgUUID(senderID),
		ReceiverID:  pgUUID(req.ReceiverID),
		Amount:      req.Amount,
		Status:      status,
		Note:        pgtype.Text{String: req.Note, Valid: req.Note != ""},
		ScheduledAt: pgtype.Timestamptz{Time: scheduledAt, Valid: true},
		SenderCategory: repo.NullExpenseCategory{
			ExpenseCategory: req.Category,
			Valid:           req.Category != "",
		},
	})
	if err != nil {
		return TransactionResponse{}, err
	}

	id := uuid.UUID(created.ID.Bytes)

	if !scheduled {
		if _, err := svc.Complete(ctx, id, repo.TransactionStatusCrediting); err != nil &&
			!errors.Is(err, ErrAlreadyProcessed) {
			return TransactionResponse{}, err
		}
	}

	got, err := svc.readOne(ctx, id, senderID)
	if err != nil {
		return TransactionResponse{}, err
	}
	return got.Transaction, nil
}

func (svc *Service) List(ctx context.Context) (ListTransactionsResponse, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return ListTransactionsResponse{}, err
	}

	rows, err := svc.repo.GetTransactionsByUserId(ctx, pgUUID(userID))
	if err != nil {
		return ListTransactionsResponse{}, err
	}

	out := make([]TransactionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowToResponse(row, userID))
	}

	return ListTransactionsResponse{Transactions: out}, nil
}

func (svc *Service) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (GetTransactionResponse, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return GetTransactionResponse{}, err
	}

	return svc.readOne(ctx, id, userID)
}

// readOne fetches a transaction with both counterparty names and refuses it to
// anyone who is neither side of it.
func (svc *Service) readOne(
	ctx context.Context,
	id, viewer uuid.UUID,
) (GetTransactionResponse, error) {
	row, err := svc.repo.GetTransactionByIdWithUsers(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetTransactionResponse{}, ErrTransactionNotFound
		}
		return GetTransactionResponse{}, err
	}

	if uuid.UUID(row.SenderID.Bytes) != viewer && uuid.UUID(row.ReceiverID.Bytes) != viewer {
		return GetTransactionResponse{}, ErrWrongOwnership
	}

	return GetTransactionResponse{Transaction: singleRowToResponse(row, viewer)}, nil
}

// Cancel deletes a still-scheduled transaction. The delete is the whole race
// resolution: it blocks on the scheduler's row lock and then affects zero rows,
// so whichever landed first wins. The follow-up read only decides which error to
// report and is not load-bearing.
func (svc *Service) Cancel(
	ctx context.Context,
	id uuid.UUID,
) (CancelTransactionResponse, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return CancelTransactionResponse{}, err
	}

	deleted, err := svc.repo.CancelScheduledTransaction(ctx, repo.CancelScheduledTransactionParams{
		ID:       pgUUID(id),
		SenderID: pgUUID(userID),
	})
	if err == nil {
		return CancelTransactionResponse{CancelledID: uuid.UUID(deleted.ID.Bytes)}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CancelTransactionResponse{}, err
	}

	return CancelTransactionResponse{}, svc.explainFailedCancel(ctx, id, userID)
}

func (svc *Service) explainFailedCancel(ctx context.Context, id, userID uuid.UUID) error {
	existing, err := svc.repo.GetTransactionById(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTransactionNotFound
		}
		return err
	}

	if uuid.UUID(existing.SenderID.Bytes) != userID {
		return ErrWrongOwnership
	}
	return ErrNotScheduled
}

// SetCategory writes the caller's own side of the transaction. Which column that
// is follows from which side they are on, so the receiver can categorise money
// coming in without being able to touch the sender's label.
func (svc *Service) SetCategory(
	ctx context.Context,
	req SetCategoryRequest,
) (GetTransactionResponse, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return GetTransactionResponse{}, err
	}

	existing, err := svc.repo.GetTransactionById(ctx, pgUUID(req.ID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetTransactionResponse{}, ErrTransactionNotFound
		}
		return GetTransactionResponse{}, err
	}

	category := repo.NullExpenseCategory{ExpenseCategory: req.Category, Valid: true}

	switch userID {
	case uuid.UUID(existing.SenderID.Bytes):
		_, err = svc.repo.SetSenderCategory(ctx, repo.SetSenderCategoryParams{
			Category: category,
			ID:       pgUUID(req.ID),
			SenderID: pgUUID(userID),
		})
	case uuid.UUID(existing.ReceiverID.Bytes):
		_, err = svc.repo.SetReceiverCategory(ctx, repo.SetReceiverCategoryParams{
			Category:   category,
			ID:         pgUUID(req.ID),
			ReceiverID: pgUUID(userID),
		})
	default:
		return GetTransactionResponse{}, ErrWrongOwnership
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetTransactionResponse{}, ErrTransactionNotFound
		}
		return GetTransactionResponse{}, err
	}

	return svc.readOne(ctx, req.ID, userID)
}

// TopUp creates the transaction and the PaymentIntent, and moves nothing. The
// balance changes only when the webhook lands, which is what makes a top-up an
// ordinary transaction sent by the Stripe system user.
func (svc *Service) TopUp(ctx context.Context, req TopUpRequest) (TopUpResponse, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return TopUpResponse{}, err
	}

	if req.Amount < MinAmount {
		return TopUpResponse{}, ErrAmountIsTooLow
	}

	created, err := svc.repo.CreateTransaction(ctx, repo.CreateTransactionParams{
		SenderID:    pgUUID(users.SystemStripeID),
		ReceiverID:  pgUUID(userID),
		Amount:      req.Amount,
		Status:      repo.TransactionStatusAwaitingPayment,
		ScheduledAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		return TopUpResponse{}, err
	}

	id := uuid.UUID(created.ID.Bytes)

	payment, err := svc.paymentSvc.ProcessPayment(ctx, payment.ProcessPaymentRequest{
		Amount:        req.Amount,
		TransactionID: id,
		Description:   "NexusPay top-up",
	})
	if err != nil {
		// No PaymentIntent means no webhook will ever arrive, so park the row
		// instead of leaving it awaiting a payment that cannot happen.
		if _, ferr := svc.Transition(
			ctx, id,
			repo.TransactionStatusAwaitingPayment,
			repo.TransactionStatusFailed,
		); ferr != nil && !errors.Is(ferr, ErrAlreadyProcessed) {
			slog.Error("Failed to mark top-up failed", "error", ferr, "transaction_id", id)
		}
		return TopUpResponse{}, err
	}

	return TopUpResponse{
		TransactionID:     id,
		Amount:            req.Amount,
		Status:            repo.TransactionStatusAwaitingPayment,
		ProviderPaymentID: payment.ProviderPaymentID,
		ClientSecret:      payment.ClientSecret,
	}, nil
}

// Complete is the one function that moves money. On insufficient funds the
// transaction has already rolled back, so a second guarded update parks the row
// as 'failed' rather than leaving it to be retried forever.
func (svc *Service) Complete(
	ctx context.Context,
	id uuid.UUID,
	from repo.TransactionStatus,
) (repo.Transaction, error) {
	t, err := svc.complete(ctx, id, from)
	if !errors.Is(err, ErrInsufficientFunds) {
		return t, err
	}

	if _, ferr := svc.Transition(ctx, id, from, repo.TransactionStatusFailed); ferr != nil &&
		!errors.Is(ferr, ErrAlreadyProcessed) {
		slog.Error("Failed to mark transaction failed", "error", ferr, "transaction_id", id)
	}

	return repo.Transaction{}, ErrInsufficientFunds
}

func (svc *Service) complete(
	ctx context.Context,
	id uuid.UUID,
	from repo.TransactionStatus,
) (repo.Transaction, error) {
	txCtx, tx, err := svc.txManager.StartTx(ctx)
	if err != nil {
		return repo.Transaction{}, err
	}
	defer tx.Rollback(txCtx)

	// The guarded status update comes first because it takes the row lock that
	// serializes concurrent callers. Zero rows means another worker got here
	// first: return without committing, nothing has moved.
	t, err := svc.repo.GuardedSetStatus(txCtx, repo.GuardedSetStatusParams{
		ID:         pgUUID(id),
		FromStatus: from,
		ToStatus:   repo.TransactionStatusCompleted,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.Transaction{}, ErrAlreadyProcessed
		}
		return repo.Transaction{}, err
	}

	if err := svc.moveBalance(txCtx, t); err != nil {
		return repo.Transaction{}, err
	}

	return t, tx.Commit(txCtx)
}

// moveBalance applies the debit and the credit in ascending user-id order.
// Simultaneous A->B and B->A transfers would otherwise grab each other's row
// locks in opposite orders and deadlock.
func (svc *Service) moveBalance(ctx context.Context, t repo.Transaction) error {
	sender := uuid.UUID(t.SenderID.Bytes)
	receiver := uuid.UUID(t.ReceiverID.Bytes)

	debit := func() error {
		_, err := svc.userSvc.Debit(ctx, users.BalanceRequest{UserID: sender, Amount: t.Amount})
		if errors.Is(err, users.ErrInsufficientFunds) {
			return ErrInsufficientFunds
		}
		return err
	}
	credit := func() error {
		_, err := svc.userSvc.Credit(ctx, users.BalanceRequest{UserID: receiver, Amount: t.Amount})
		return err
	}

	first, second := debit, credit
	if bytes.Compare(sender[:], receiver[:]) > 0 {
		first, second = credit, debit
	}

	if err := first(); err != nil {
		return err
	}
	return second()
}

// Transition is a guarded status change that touches no balance. A single
// statement is already atomic, so it needs no explicit transaction.
func (svc *Service) Transition(
	ctx context.Context,
	id uuid.UUID,
	from, to repo.TransactionStatus,
) (repo.Transaction, error) {
	t, err := svc.repo.GuardedSetStatus(ctx, repo.GuardedSetStatusParams{
		ID:         pgUUID(id),
		FromStatus: from,
		ToStatus:   to,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.Transaction{}, ErrAlreadyProcessed
		}
		return repo.Transaction{}, err
	}

	return t, nil
}

// toResponse maps a joined row to the API shape. viewer decides the direction
// field; pass uuid.Nil to leave it blank.
func toResponse(
	id, senderID, receiverID uuid.UUID,
	senderName, receiverName string,
	t repo.Transaction,
	viewer uuid.UUID,
) TransactionResponse {
	direction := ""
	switch viewer {
	case senderID:
		direction = "debit"
	case receiverID:
		direction = "credit"
	}

	return TransactionResponse{
		ID:               id,
		Sender:           UserMini{ID: senderID, FullName: senderName},
		Receiver:         UserMini{ID: receiverID, FullName: receiverName},
		Amount:           t.Amount,
		Direction:        direction,
		Status:           t.Status,
		Note:             t.Note.String,
		SenderCategory:   t.SenderCategory.ExpenseCategory,
		ReceiverCategory: t.ReceiverCategory.ExpenseCategory,
		ScheduledAt:      t.ScheduledAt.Time,
		CreatedAt:        t.CreatedAt.Time,
	}
}

func rowToResponse(r repo.GetTransactionsByUserIdRow, viewer uuid.UUID) TransactionResponse {
	return toResponse(
		uuid.UUID(r.ID.Bytes),
		uuid.UUID(r.SenderID.Bytes),
		uuid.UUID(r.ReceiverID.Bytes),
		r.SenderName,
		r.ReceiverName,
		repo.Transaction{
			Amount:           r.Amount,
			Status:           r.Status,
			Note:             r.Note,
			SenderCategory:   r.SenderCategory,
			ReceiverCategory: r.ReceiverCategory,
			ScheduledAt:      r.ScheduledAt,
			CreatedAt:        r.CreatedAt,
		},
		viewer,
	)
}

func singleRowToResponse(
	r repo.GetTransactionByIdWithUsersRow,
	viewer uuid.UUID,
) TransactionResponse {
	return toResponse(
		uuid.UUID(r.ID.Bytes),
		uuid.UUID(r.SenderID.Bytes),
		uuid.UUID(r.ReceiverID.Bytes),
		r.SenderName,
		r.ReceiverName,
		repo.Transaction{
			Amount:           r.Amount,
			Status:           r.Status,
			Note:             r.Note,
			SenderCategory:   r.SenderCategory,
			ReceiverCategory: r.ReceiverCategory,
			ScheduledAt:      r.ScheduledAt,
			CreatedAt:        r.CreatedAt,
		},
		viewer,
	)
}
