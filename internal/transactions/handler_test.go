package transactions

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/Youssef-codin/NexusPay/internal/utils/validator"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mapError is the single place a sentinel becomes an HTTP status, so every
// sentinel the service can return needs to land on a deliberate code rather
// than falling through to a 500.
func TestMapError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"not found", ErrTransactionNotFound, http.StatusNotFound},
		{"unknown user", users.ErrUserNotFound, http.StatusNotFound},
		{"wrong owner", ErrWrongOwnership, http.StatusForbidden},
		{"no longer scheduled", ErrNotScheduled, http.StatusConflict},
		{"self transfer", ErrSelfTransfer, http.StatusBadRequest},
		{"insufficient funds", ErrInsufficientFunds, http.StatusBadRequest},
		{"user-level insufficient funds", users.ErrInsufficientFunds, http.StatusBadRequest},
		{"amount too low", ErrAmountIsTooLow, http.StatusBadRequest},
		{"scheduled in the past", validator.ErrScheduledAtMustBeFuture, http.StatusBadRequest},
		{"bad category", validator.ErrInvalidExpenseCategory, http.StatusBadRequest},
		{"bad request", ErrBadRequest, http.StatusBadRequest},
		{"not implemented", ErrNotImplemented, http.StatusNotImplemented},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, api.ErrorCode(mapError(tc.err)))
		})
	}
}

// An error nobody classified must not be dressed up as a client error.
func TestMapError_UnknownErrorIsPassedThrough(t *testing.T) {
	boom := errors.New("connection refused")
	assert.Equal(t, boom, mapError(boom))
}

// ErrAlreadyProcessed is success everywhere, so it must never reach mapError as
// a client-visible code.
func TestMapError_AlreadyProcessedIsNotClassified(t *testing.T) {
	assert.Equal(t, ErrAlreadyProcessed, mapError(ErrAlreadyProcessed))
}

// ---------------------------------------------------------------------------
// Handler wiring
// ---------------------------------------------------------------------------

type handlerMockService struct {
	mock.Mock
	IService
}

func (m *handlerMockService) Create(
	ctx context.Context,
	req CreateTransactionRequest,
) (TransactionResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(TransactionResponse), args.Error(1)
}

func (m *handlerMockService) List(ctx context.Context) (ListTransactionsResponse, error) {
	args := m.Called(ctx)
	return args.Get(0).(ListTransactionsResponse), args.Error(1)
}

func (m *handlerMockService) TopUp(ctx context.Context, req TopUpRequest) (TopUpResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(TopUpResponse), args.Error(1)
}

func TestCreateHandler_Returns201(t *testing.T) {
	svc := &handlerMockService{}
	h := NewHandler(svc)

	svc.On("Create", mock.Anything, mock.Anything).
		Return(TransactionResponse{ID: uuid.New(), Amount: 5000}, nil)

	body := `{"receiver_id":"` + uuid.New().String() + `","amount_in_piastres":5000}`
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(body))
	w := httptest.NewRecorder()

	assert.NoError(t, h.Create(w, req))
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestCreateHandler_ServiceErrorBecomesStatus(t *testing.T) {
	svc := &handlerMockService{}
	h := NewHandler(svc)

	svc.On("Create", mock.Anything, mock.Anything).
		Return(TransactionResponse{}, ErrInsufficientFunds)

	body := `{"receiver_id":"` + uuid.New().String() + `","amount_in_piastres":5000}`
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(body))

	err := h.Create(httptest.NewRecorder(), req)

	assert.Equal(t, http.StatusBadRequest, api.ErrorCode(err))
}

func TestListHandler_Returns200(t *testing.T) {
	svc := &handlerMockService{}
	h := NewHandler(svc)

	svc.On("List", mock.Anything).Return(ListTransactionsResponse{
		Transactions: []TransactionResponse{{ID: uuid.New()}},
	}, nil)

	w := httptest.NewRecorder()

	assert.NoError(t, h.List(w, httptest.NewRequest(http.MethodGet, "/transactions", nil)))
	assert.Equal(t, http.StatusOK, w.Code)
}

// A top-up answers 200, not 201: nothing has been created from the caller's
// point of view until the payment succeeds.
func TestTopUpHandler_Returns200(t *testing.T) {
	svc := &handlerMockService{}
	h := NewHandler(svc)

	svc.On("TopUp", mock.Anything, TopUpRequest{Amount: 5000}).
		Return(TopUpResponse{TransactionID: uuid.New(), Amount: 5000}, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/transactions/topup",
		strings.NewReader(`{"amount_in_piastres":5000}`),
	)
	w := httptest.NewRecorder()

	assert.NoError(t, h.TopUp(w, req))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetByIDHandler_RejectsUnparseableID(t *testing.T) {
	h := NewHandler(&handlerMockService{})

	req := httptest.NewRequest(http.MethodGet, "/transactions/not-a-uuid", nil)
	err := h.GetByID(httptest.NewRecorder(), req)

	assert.Equal(t, http.StatusBadRequest, api.ErrorCode(err))
}
