package transactions

import (
	"context"
	"errors"

	"github.com/Youssef-codin/NexusPay/internal/db"
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/payment"
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/google/uuid"
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

func (svc *Service) Create(
	ctx context.Context,
	req CreateTransactionRequest,
) (TransactionResponse, error) {
	return TransactionResponse{}, ErrNotImplemented
}

func (svc *Service) List(ctx context.Context) (ListTransactionsResponse, error) {
	return ListTransactionsResponse{}, ErrNotImplemented
}

func (svc *Service) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (GetTransactionResponse, error) {
	return GetTransactionResponse{}, ErrNotImplemented
}

func (svc *Service) Cancel(
	ctx context.Context,
	id uuid.UUID,
) (CancelTransactionResponse, error) {
	return CancelTransactionResponse{}, ErrNotImplemented
}

func (svc *Service) SetCategory(
	ctx context.Context,
	req SetCategoryRequest,
) (GetTransactionResponse, error) {
	return GetTransactionResponse{}, ErrNotImplemented
}

func (svc *Service) TopUp(ctx context.Context, req TopUpRequest) (TopUpResponse, error) {
	return TopUpResponse{}, ErrNotImplemented
}

func (svc *Service) Complete(
	ctx context.Context,
	id uuid.UUID,
	from repo.TransactionStatus,
) (repo.Transaction, error) {
	return repo.Transaction{}, ErrNotImplemented
}

func (svc *Service) Transition(
	ctx context.Context,
	id uuid.UUID,
	from, to repo.TransactionStatus,
) (repo.Transaction, error) {
	return repo.Transaction{}, ErrNotImplemented
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
