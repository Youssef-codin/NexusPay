package transfers

import (
	"errors"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
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
		default:
			return err
		}
	}

	api.Respond(w, transfer, http.StatusOK)
	return nil
}
