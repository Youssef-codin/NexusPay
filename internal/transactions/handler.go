package transactions

import (
	"errors"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/Youssef-codin/NexusPay/internal/utils/validator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type handler struct {
	svc IService
}

func NewHandler(service IService) *handler {
	return &handler{
		svc: service,
	}
}

// mapError turns the package's sentinels into HTTP codes. Every handler funnels
// through here so the mapping lives in exactly one place.
func mapError(err error) error {
	switch {
	case errors.Is(err, ErrTransactionNotFound):
		return api.WrappedError(http.StatusNotFound, "Transaction was not found")
	case errors.Is(err, users.ErrUserNotFound):
		return api.WrappedError(http.StatusNotFound, "User was not found")
	case errors.Is(err, ErrWrongOwnership):
		return api.WrappedError(http.StatusForbidden, "Transaction belongs to another user")
	case errors.Is(err, ErrNotScheduled):
		return api.WrappedError(http.StatusConflict, "Transaction is no longer scheduled")
	case errors.Is(err, ErrSelfTransfer):
		return api.WrappedError(http.StatusBadRequest, "Can not transfer to self")
	case errors.Is(err, ErrInsufficientFunds), errors.Is(err, users.ErrInsufficientFunds):
		return api.WrappedError(http.StatusBadRequest, "Insufficient funds")
	case errors.Is(err, ErrAmountIsTooLow), errors.Is(err, validator.ErrAmountIsTooLow):
		return api.WrappedError(http.StatusBadRequest, "%s", ErrAmountIsTooLow.Error())
	case errors.Is(err, validator.ErrScheduledAtMustBeFuture):
		return api.WrappedError(http.StatusBadRequest, "Scheduled time must be in the future")
	case errors.Is(err, validator.ErrInvalidExpenseCategory):
		return api.WrappedError(http.StatusBadRequest, "Invalid expense category")
	case errors.Is(err, ErrBadRequest):
		return api.WrappedError(http.StatusBadRequest, "Bad Request")
	case errors.Is(err, ErrNotImplemented):
		return api.WrappedError(http.StatusNotImplemented, "Not implemented")
	default:
		return err
	}
}

func (h *handler) Create(w http.ResponseWriter, req *http.Request) error {
	var dto CreateTransactionRequest

	if err := api.Read(req, &dto); err != nil {
		return mapError(err)
	}

	transaction, err := h.svc.Create(req.Context(), dto)
	if err != nil {
		return mapError(err)
	}

	api.Respond(w, transaction, http.StatusCreated)
	return nil
}

func (h *handler) List(w http.ResponseWriter, req *http.Request) error {
	transactions, err := h.svc.List(req.Context())
	if err != nil {
		return mapError(err)
	}

	api.Respond(w, transactions, http.StatusOK)
	return nil
}

func (h *handler) GetByID(w http.ResponseWriter, req *http.Request) error {
	id, err := uuid.Parse(chi.URLParam(req, "id"))
	if err != nil {
		return api.WrappedError(http.StatusBadRequest, "Invalid id")
	}

	transaction, err := h.svc.GetByID(req.Context(), id)
	if err != nil {
		return mapError(err)
	}

	api.Respond(w, transaction, http.StatusOK)
	return nil
}

func (h *handler) Cancel(w http.ResponseWriter, req *http.Request) error {
	id, err := uuid.Parse(chi.URLParam(req, "id"))
	if err != nil {
		return api.WrappedError(http.StatusBadRequest, "Invalid id")
	}

	result, err := h.svc.Cancel(req.Context(), id)
	if err != nil {
		return mapError(err)
	}

	api.Respond(w, result, http.StatusOK)
	return nil
}

func (h *handler) SetCategory(w http.ResponseWriter, req *http.Request) error {
	id, err := uuid.Parse(chi.URLParam(req, "id"))
	if err != nil {
		return api.WrappedError(http.StatusBadRequest, "Invalid id")
	}

	var dto SetCategoryRequest
	if err := api.Read(req, &dto); err != nil {
		return mapError(err)
	}
	dto.ID = id

	transaction, err := h.svc.SetCategory(req.Context(), dto)
	if err != nil {
		return mapError(err)
	}

	api.Respond(w, transaction, http.StatusOK)
	return nil
}

func (h *handler) TopUp(w http.ResponseWriter, req *http.Request) error {
	var dto TopUpRequest

	if err := api.Read(req, &dto); err != nil {
		return mapError(err)
	}

	result, err := h.svc.TopUp(req.Context(), dto)
	if err != nil {
		return mapError(err)
	}

	api.Respond(w, result, http.StatusOK)
	return nil
}
