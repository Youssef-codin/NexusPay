package users

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/utils/api"
)

type controller struct {
	service IService
}

func NewController(service IService) *controller {
	return &controller{
		service: service,
	}
}

func (c *controller) LoginController(w http.ResponseWriter, req *http.Request) error {
	var loginReq LoginRequest

	if err := api.Read(req, &loginReq); err != nil {
		return api.Errorf(http.StatusBadRequest, "Invalid input")
	}

	user, err := c.service.Login(req.Context(), loginReq)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			return api.Errorf(http.StatusUnauthorized, "invalid credentials")
		case errors.Is(err, ErrUserNotFound):
			return api.Errorf(http.StatusNotFound, "user not found")
		default:
			slog.Error("login failed", "error", err)
			return api.Errorf(http.StatusInternalServerError, "something went wrong")
		}
	}

	api.Respond(w, user, http.StatusOK)
	return nil
}

func (c *controller) RegisterController(w http.ResponseWriter, req *http.Request) error {
	return nil
}
