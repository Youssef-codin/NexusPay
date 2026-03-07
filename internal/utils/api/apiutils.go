package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/httprate"
	httprateredis "github.com/go-chi/httprate-redis"
	"github.com/go-chi/jwtauth/v5"
	"github.com/go-playground/validator/v10"
)

type errorResponse struct {
	Error string `json:"error"`
}

var validate = validator.New()

func Validate(s any) error {
	return validate.Struct(s)
}

func Read(r *http.Request, data any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	return decoder.Decode(data)
}

func Respond(w http.ResponseWriter, obj any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(obj)
}

func Error(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse{Error: msg})
}

func NewUserLimiter(requestsPerMin int, host string, port uint16) func(http.Handler) http.Handler {
	return httprate.Limit(
		requestsPerMin, time.Minute,
		httprate.WithKeyFuncs(func(r *http.Request) (string, error) {
			_, claims, _ := jwtauth.FromContext(r.Context())
			sub, ok := claims["sub"].(string)
			if !ok {
				return "", fmt.Errorf("invalid sub claim")
			}
			return sub, nil
		}),
		httprateredis.WithRedisLimitCounter(&httprateredis.Config{
			Host: host, Port: port,
		}),
	)
}
