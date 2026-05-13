package auth

import (
	"errors"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/security"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/go-chi/jwtauth/v5"
)

const refreshCookieName = "refresh_token"

type handler struct {
	svc  IService
	auth *security.Authenticator
}

func NewHandler(service IService, auth *security.Authenticator) *handler {
	return &handler{
		svc:  service,
		auth: auth,
	}
}

func setRefreshCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		HttpOnly: true,
		Secure:   true, //NOTE: set to true in production behind HTTPS
		SameSite: http.SameSiteNoneMode,
		Path:     "/",
		MaxAge:   maxAge,
	})
}

func clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		HttpOnly: true,
		SameSite: http.SameSiteNoneMode,
		Path:     "/",
		MaxAge:   -1,
	})
}

func (h *handler) TestAuth(w http.ResponseWriter, req *http.Request) error {
	_, claims, err := jwtauth.FromContext(req.Context())
	if err != nil {
		return api.WrappedError(http.StatusUnauthorized, "unauthorized")
	}
	api.Respond(w, claims, http.StatusOK)
	return nil
}

func (h *handler) LoginHandler(w http.ResponseWriter, req *http.Request) error {
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

	setRefreshCookie(w, response.RefreshToken, int(h.auth.RefreshTokenDuration.Seconds()))
	api.Respond(w, response, http.StatusOK)
	return nil
}

func (h *handler) RegisterHandler(w http.ResponseWriter, req *http.Request) error {
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

	setRefreshCookie(w, response.RefreshToken, int(h.auth.RefreshTokenDuration.Seconds()))
	api.Respond(w, response, http.StatusCreated)
	return nil
}

func (h *handler) RefreshHandler(w http.ResponseWriter, req *http.Request) error {
	cookie, err := req.Cookie(refreshCookieName)
	if err != nil {
		return api.WrappedError(http.StatusUnauthorized, "missing refresh token")
	}

	response, err := h.svc.refreshToken(req.Context(), refreshRequest{RefreshToken: cookie.Value})
	if err != nil {
		switch {
		case errors.Is(err, ErrUserNotFound):
			return api.WrappedError(http.StatusNotFound, "User not found")
		case errors.Is(err, ErrTokenExpired):
			return api.WrappedError(http.StatusUnauthorized, "Token expired")
		default:
			return err
		}
	}

	setRefreshCookie(w, response.RefreshToken, int(h.auth.RefreshTokenDuration.Seconds()))
	api.Respond(w, response, http.StatusOK)
	return nil
}

func (h *handler) LogoutHandler(w http.ResponseWriter, req *http.Request) error {
	err := h.svc.logout(req.Context())
	if err != nil {
		switch {
		case errors.Is(err, ErrUserNotFound):
			return api.WrappedError(http.StatusNotFound, "user not found")
		default:
			return err
		}
	}

	clearRefreshCookie(w)
	api.Respond(w, nil, http.StatusNoContent)
	return nil
}
