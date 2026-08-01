package users

import (
	"errors"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/Youssef-codin/NexusPay/internal/utils/validator"
)

type handler struct {
	svc IService
}

func NewHandler(service IService) *handler {
	return &handler{
		svc: service,
	}
}

func (h *handler) SearchByName(w http.ResponseWriter, req *http.Request) error {
	nameReq := FindUserRequest{
		FullName: req.URL.Query().Get("name"),
	}

	if err := validator.Validate(nameReq); err != nil {
		return api.WrappedError(http.StatusBadRequest, "Bad Request")
	}

	usersRes, err := h.svc.FindByName(req.Context(), nameReq)
	if err != nil {
		switch {
		case errors.Is(err, ErrBadRequest):
			return api.WrappedError(http.StatusBadRequest, "Bad Request")
		case errors.Is(err, ErrUserNotFound):
			return api.WrappedError(http.StatusNotFound, "User(s) not found")
		default:
			return err
		}
	}

	api.Respond(w, usersRes, http.StatusOK)
	return nil
}

func (h *handler) GetMe(w http.ResponseWriter, req *http.Request) error {
	me, err := h.svc.GetMe(req.Context())
	if err != nil {
		switch {
		case errors.Is(err, ErrBadRequest):
			return api.WrappedError(http.StatusBadRequest, "Bad Request")
		case errors.Is(err, ErrUserNotFound):
			return api.WrappedError(http.StatusNotFound, "User not found")
		default:
			return err
		}
	}

	api.Respond(w, me, http.StatusOK)
	return nil
}
