package users

import (
	"errors"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/Youssef-codin/NexusPay/internal/utils/validator"
)

type controller struct {
	svc IService
}

func NewController(service IService) *controller {
	return &controller{
		svc: service,
	}
}

func (c *controller) SearchByNameController(w http.ResponseWriter, req *http.Request) error {
	nameReq := FindUserRequest{
		FullName: req.URL.Query().Get("name"),
	}

	if err := validator.Validate(nameReq); err != nil {
		return api.WrappedError(http.StatusBadRequest, "Bad Request")
	}

	usersRes, err := c.svc.findByName(req.Context(), nameReq)
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
