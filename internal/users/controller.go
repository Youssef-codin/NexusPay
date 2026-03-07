package users

import (
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

func (c *controller) LoginController(w http.ResponseWriter, req *http.Request) {
	var loginReq LoginRequest

	if err := api.Read(req, &loginReq); err != nil {
		api.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	user, err := c.service.Login(req.Context(), loginReq)
	if err != nil {
		api.Error(w, "Something went wrong...", http.StatusBadRequest)
		slog.Error(err.Error())
		return
	}

	api.Respond(w, user, http.StatusOK)
}

func (c *controller) RegisterController(w http.ResponseWriter, req *http.Request) {

}
