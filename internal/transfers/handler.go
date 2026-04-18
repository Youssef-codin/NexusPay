package transfers

import (
	"errors"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/Youssef-codin/NexusPay/internal/utils/validator"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
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

func (h *handler) GetTransfers(w http.ResponseWriter, req *http.Request) error {
	transfers, err := h.svc.GetTransfers(req.Context())

	if err != nil {
		return err
	}

	api.Respond(w, transfers, 200)
	return nil
}

func (h *handler) CreateTransfer(w http.ResponseWriter, req *http.Request) error {
	var dto CreateTransferRequest

	if err := api.Read(req, &dto); err != nil {
		return err
	}

	transfer, err := h.svc.CreateTransfer(req.Context(), dto)
	if err != nil {
		switch {
		case errors.Is(err, ErrTransferNotFound):
			return api.WrappedError(http.StatusNotFound, "Transfer was not found")
		case errors.Is(err, transactions.ErrBadRequest):
			return api.WrappedError(http.StatusBadRequest, "Bad transaction request")
		case errors.Is(err, wallet.ErrWalletNotFound):
			return api.WrappedError(http.StatusNotFound, "Wallet not found")
		case errors.Is(err, wallet.ErrInsufficientFunds):
			return api.WrappedError(http.StatusBadRequest, "Insufficient funds")
		case errors.Is(err, ErrSelfTransfer):
			return api.WrappedError(http.StatusBadRequest, "Can not transfer to self")
		case errors.Is(err, ErrBadRequest):
			return api.WrappedError(http.StatusBadRequest, "Bad transfer request")
		case errors.Is(err, validator.ErrScheduledAtMustBeFuture):
			return api.WrappedError(http.StatusBadRequest, "Scheduled time must be in the future")
		default:
			return err
		}
	}

	api.Respond(w, transfer, http.StatusOK)
	return nil
}

func (h *handler) GetTransferByID(w http.ResponseWriter, req *http.Request) error {
	var dto GetTransferByIDRequest

	if err := api.Read(req, &dto); err != nil {
		return err
	}

	transfer, err := h.svc.GetTransferByID(req.Context(), dto)
	if err != nil {
		switch {
		case errors.Is(err, ErrTransferNotFound):
			return api.WrappedError(http.StatusNotFound, "Transfer was not found")
		case errors.Is(err, ErrBadRequest):
			return api.WrappedError(http.StatusBadRequest, "Bad transfer request")
		default:
			return err
		}
	}

	api.Respond(w, transfer, http.StatusOK)
	return nil
}

func (h *handler) GetScheduledTransfers(w http.ResponseWriter, req *http.Request) error {
	userIDStr, err := api.GetTokenUserID(req.Context())
	if err != nil {
		return api.WrappedError(http.StatusUnauthorized, "Unauthorized")
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return api.WrappedError(http.StatusUnauthorized, "Invalid user ID")
	}

	result, err := h.svc.ListScheduledTransfers(req.Context(), userID)
	if err != nil {
		return err
	}

	api.Respond(w, result, http.StatusOK)
	return nil
}

func (h *handler) DeleteScheduledTransfer(w http.ResponseWriter, req *http.Request) error {
	var dto CancelScheduledTransfersRequest

	if err := api.Read(req, &dto); err != nil {
		return err
	}

	result, err := h.svc.CancelScheduledTransfers(req.Context(), dto)
	if err != nil {
		switch {
		case errors.Is(err, ErrTransferNotFound):
			return api.WrappedError(http.StatusNotFound, "Transfer was not found")
		case errors.Is(err, ErrWrongOwnership):
			return api.WrappedError(http.StatusForbidden, "Transfer belongs to another user")
		case errors.Is(err, ErrAlreadyExecuted):
			return api.WrappedError(http.StatusBadRequest, "Transfer already executed")
		case errors.Is(err, ErrTooLateToCancel):
			return api.WrappedError(http.StatusBadRequest, "Too late to cancel transfer")
		default:
			return err
		}
	}

	api.Respond(w, result, http.StatusOK)
	return nil
}
