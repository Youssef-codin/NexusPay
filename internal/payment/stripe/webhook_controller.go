package stripe

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"
)

var (
	ErrReadBodyFailed   = errors.New("failed to read request body")
	ErrInvalidSignature = errors.New("invalid webhook signature")
)

type controller struct {
	endpointSecret string
	service        IService
}

func NewWebhookController(endpointSecret string, service IService) *controller {
	return &controller{
		endpointSecret: endpointSecret,
		service:        service,
	}
}

func (c *controller) Handle(w http.ResponseWriter, req *http.Request) error {
	const maxBodyBytes = int64(65536)

	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)

	payload, err := io.ReadAll(req.Body)
	if err != nil {
		slog.Error("Error reading request body", "error", err)
		return api.WrappedError(http.StatusServiceUnavailable, "Failed to read request body")
	}

	event, err := webhook.ConstructEvent(
		payload,
		req.Header.Get("Stripe-Signature"),
		c.endpointSecret,
	)
	if err != nil {
		slog.Error("Webhook signature verification failed", "error", err)
		return api.WrappedError(http.StatusBadRequest, "Invalid webhook signature")
	}

	var paymentIntent stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &paymentIntent); err != nil {
		slog.Error("Error parsing payment intent event", "error", err)
		return api.WrappedError(http.StatusBadRequest, "Failed to parse event")
	}

	transactionID := paymentIntent.Metadata["transaction_id"]

	switch event.Type {
	case "payment_intent.succeeded":
		err := c.service.HandlePaymentSucceeded(req.Context(), HandlePaymentSucceededRequest{
			TransactionID: transactionID,
		})
		if err != nil {
			slog.Error("Failed to handle payment succeeded", "error", err, "transaction_id", transactionID)
		}
	case "payment_intent.payment_failed":
		err := c.service.HandlePaymentFailed(req.Context(), HandlePaymentFailedRequest{
			TransactionID: transactionID,
		})
		if err != nil {
			slog.Error("Failed to handle payment failed", "error", err, "transaction_id", transactionID)
		}
	case "payment_intent.canceled":
		err := c.service.HandlePaymentCanceled(req.Context(), HandlePaymentCanceledRequest{
			TransactionID: transactionID,
		})
		if err != nil {
			slog.Error("Failed to handle payment canceled", "error", err, "transaction_id", transactionID)
		}
	default:
		slog.Debug("Unhandled event type", "type", event.Type)
	}

	api.Respond(w, nil, http.StatusOK)
	return nil
}
