package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Youssef-codin/NexusPay/internal/utils/validator"
	"github.com/go-chi/httprate"
	httprateredis "github.com/go-chi/httprate-redis"
	"github.com/go-chi/jwtauth/v5"
	"github.com/redis/go-redis/v9"
)

type errorResponse struct {
	Error string `json:"error"`
}

func Read[T any](r *http.Request, data *T) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(data); err != nil {
		return err
	}
	return validator.Validate(data)
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

func NewUserLimiter(
	requestsPerMin int,
	client redis.UniversalClient,
) func(http.Handler) http.Handler {
	return httprate.Limit(
		requestsPerMin, time.Minute,
		httprate.WithKeyFuncs(func(r *http.Request) (string, error) {
			_, claims, _ := jwtauth.FromContext(r.Context())
			sub, ok := claims["sub"].(string)
			if !ok {
				slog.Error("invalid sub claim in rate limiter")
				return "", fmt.Errorf("invalid sub claim")
			}
			return sub, nil
		}),
		httprateredis.WithRedisLimitCounter(&httprateredis.Config{
			Client: client,
		}),
	)
}

func GetTokenUserID(ctx context.Context) (string, error) {
	_, claims, err := jwtauth.FromContext(ctx)
	if err != nil {
		slog.Error("failed to get claims from context", "error", err)
		return "", fmt.Errorf("failed to get claims from context: %w", err)
	}

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		slog.Error("invalid or missing sub claim in context")
		return "", fmt.Errorf("invalid or missing sub claim")
	}

	return sub, nil
}
