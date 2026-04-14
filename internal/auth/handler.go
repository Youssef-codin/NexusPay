package auth

import (
	"errors"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/go-chi/jwtauth/v5"
)

type handler struct {
	svc IService
}

func NewHandler(service IService) *handler {
	return &handler{
		svc: service,
	}
}

func (h *handler) TestAuth(w http.ResponseWriter, req *http.Request) error {
	_, claims, err := jwtauth.FromContext(req.Context())
	if err != nil {
		return api.WrappedError(http.StatusUnauthorized, "unauthorized")
	}
	api.Respond(w, claims, http.StatusOK)
	return nil
}

func (h *handler) LoginController(w http.ResponseWriter, req *http.Request) error {
	var dto loginRequest

	if err := api.Read(req, &dto); err != nil {
		return api.WrappedError(http.StatusBadRequest, "Invalid input")
	}

	response, err := h.svc.login(req.Context(), dto)
	if err != nil {
		switch {
		case errors.Is(err, ErrBadRequest):
			return api.WrappedError(http.StatusBadRequest, "Bad Request")
		case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrUserNotFound):
			return api.WrappedError(http.StatusUnauthorized, "Invalid credentials")
		default:
			return err
		}
	}

	api.Respond(w, response, http.StatusOK)
	return nil
}

func (h *handler) RegisterController(w http.ResponseWriter, req *http.Request) error {
	var dto registerRequest

	if err := api.Read(req, &dto); err != nil {
		return api.WrappedError(http.StatusBadRequest, "Invalid input")
	}

	response, err := h.svc.register(req.Context(), dto)
	if err != nil {
		switch {
		case errors.Is(err, ErrBadRequest), errors.Is(err, ErrPasswordTooLong):
			return api.WrappedError(http.StatusBadRequest, "Bad Request")
		case errors.Is(err, ErrUserAlreadyExists):
			return api.WrappedError(http.StatusConflict, "Already exists")
		default:
			return err
		}
	}

	api.Respond(w, response, http.StatusCreated)
	return nil
}

func (h *handler) RefreshController(w http.ResponseWriter, req *http.Request) error {
	var dto refreshRequest

	if err := api.Read(req, &dto); err != nil {
		return api.WrappedError(http.StatusBadRequest, "Invalid input")
	}

	response, err := h.svc.refreshToken(req.Context(), dto)
	if err != nil {
		switch {
		case errors.Is(err, ErrUserNotFound):
			return api.WrappedError(http.StatusNotFound, "User not found")
		case errors.Is(err, ErrTokenExpired):
			return api.WrappedError(http.StatusNotFound, "User not found")
		default:
			return err
		}
	}
	api.Respond(w, response, http.StatusOK)
	return nil
}

func (h *handler) LogoutController(w http.ResponseWriter, req *http.Request) error {
	err := h.svc.logout(req.Context())
	if err != nil {
		switch {
		case errors.Is(err, ErrUserNotFound):
			return api.WrappedError(http.StatusNotFound, "user not found")
		default:
			return err
		}
	}
	api.Respond(w, nil, http.StatusNoContent)
	return nil
}
