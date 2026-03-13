package auth

import (
	"errors"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/go-chi/jwtauth/v5"
)

type controller struct {
	svc IService
}

func NewController(service IService) *controller {
	return &controller{
		svc: service,
	}
}

func (c *controller) TestAuth(w http.ResponseWriter, req *http.Request) error {
	_, claims, err := jwtauth.FromContext(req.Context())
	if err != nil {
		return api.WrappedError(http.StatusUnauthorized, "unauthorized")
	}
	api.Respond(w, claims, http.StatusOK)
	return nil
}

func (c *controller) LoginController(w http.ResponseWriter, req *http.Request) error {
	var loginReq loginRequest

	if err := api.Read(req, &loginReq); err != nil {
		return api.WrappedError(http.StatusBadRequest, "Invalid input")
	}

	response, err := c.svc.login(req.Context(), loginReq)
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

func (c *controller) RegisterController(w http.ResponseWriter, req *http.Request) error {
	var registerReq registerRequest

	if err := api.Read(req, &registerReq); err != nil {
		return api.WrappedError(http.StatusBadRequest, "Invalid input")
	}

	response, err := c.svc.register(req.Context(), registerReq)
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

func (c *controller) RefreshController(w http.ResponseWriter, req *http.Request) error {
	var refreshReq refreshRequest

	if err := api.Read(req, &refreshReq); err != nil {
		return api.WrappedError(http.StatusBadRequest, "Invalid input")
	}

	response, err := c.svc.refreshToken(req.Context(), refreshReq)
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

func (c *controller) LogoutController(w http.ResponseWriter, req *http.Request) error {
	err := c.svc.logout(req.Context())
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
