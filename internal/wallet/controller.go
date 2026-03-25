package wallet

import (
	"errors"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/utils/api"
)

type controller struct {
	svc IService
}

func NewController(service IService) *controller {
	return &controller{
		svc: service,
	}
}

func (c *controller) GetByUserId(w http.ResponseWriter, req *http.Request) error {
	res, err := c.svc.GetByUserId(req.Context())
	if err != nil {
		switch {
		case errors.Is(err, ErrWalletNotFound):
			return api.WrappedError(http.StatusNotFound, "Wallet for this user was not found")
		default:
			return err
		}
	}
	api.Respond(w, res, http.StatusOK)
	return nil
}

func (c *controller) TopUp(w http.ResponseWriter, req *http.Request) error {
	var walletReq TopUpRequest

	if err := api.Read(req, &walletReq); err != nil {
		return api.WrappedError(http.StatusBadRequest, "Bad Request")
	}

	wallet, err := c.svc.TopUp(req.Context(), walletReq)
	if err != nil {
		switch {
		case errors.Is(err, ErrWalletNotFound):
			return api.WrappedError(http.StatusNotFound, "Wallet for this user was not found")
		default:
			return err
		}
	}

	api.Respond(w, wallet, http.StatusOK)
	return nil
}
