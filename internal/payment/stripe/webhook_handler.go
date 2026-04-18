package stripe

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"
)

var (
	ErrReadBodyFailed   = errors.New("failed to read request body")
	ErrInvalidSignature = errors.New("invalid webhook signature")
)

type handler struct {
	endpointSecret string
	service        IService
}

func NewWebhookHandler(endpointSecret string, service IService) *handler {
	return &handler{
		endpointSecret: endpointSecret,
		service:        service,
	}
}

func (h *handler) Handle(w http.ResponseWriter, req *http.Request) error {
	slog.Debug("Webhook received", "method", req.Method, "path", req.URL.Path)

	const maxBodyBytes = int64(65536)

	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)

	payload, err := io.ReadAll(req.Body)
	if err != nil {
		slog.Error("Error reading request body", "error", err)
		return api.WrappedError(http.StatusServiceUnavailable, "Failed to read request body")
	}

	slog.Debug("Webhook payload received", "size", len(payload), "signature_header", req.Header.Get("Stripe-Signature")[:min(20, len(req.Header.Get("Stripe-Signature")))])

	skipSignature := os.Getenv("STRIPE_WEBHOOK_SKIP_SIGNATURE") == "true"

	if !skipSignature {
		event, err := webhook.ConstructEvent(
			payload,
			req.Header.Get("Stripe-Signature"),
			h.endpointSecret,
		)
		if err != nil {
			slog.Error("Webhook signature verification failed", "error", err)
			return api.WrappedError(http.StatusBadRequest, "Invalid webhook signature")
		}

		return h.handleEvent(w, req, payload, event)
	}

	var event map[string]interface{}
	if err := json.Unmarshal(payload, &event); err != nil {
		slog.Error("Error parsing webhook payload", "error", err)
		return api.WrappedError(http.StatusBadRequest, "Failed to parse webhook payload")
	}

	return h.handleEvent(w, req, payload, event)
}

func (h *handler) handleEvent(w http.ResponseWriter, req *http.Request, payload []byte, eventData interface{}) error {
	var eventType string
	var rawData []byte

	switch v := eventData.(type) {
	case stripe.Event:
		eventType = string(v.Type)
		rawData = v.Data.Raw
	case map[string]interface{}:
		var ok bool
		eventType, ok = v["type"].(string)
		if !ok {
			slog.Error("Missing event type in webhook")
			return api.WrappedError(http.StatusBadRequest, "Missing event type")
		}
		rawData, _ = json.Marshal(v)
	default:
		slog.Error("Unknown event data type")
		return api.WrappedError(http.StatusBadRequest, "Unknown event data")
	}

	slog.Debug("Parsed event", "eventType", eventType, "rawDataLen", len(rawData))

	var paymentIntent stripe.PaymentIntent
	if err := json.Unmarshal(rawData, &paymentIntent); err != nil {
		slog.Error("Error parsing payment intent event", "error", err)
	}

	var transactionID string
	if paymentIntent.Metadata != nil {
		transactionID = paymentIntent.Metadata["transaction_id"]
	}

	if transactionID == "" {
		if eventDataMap, ok := eventData.(map[string]interface{}); ok {
			if dataObj, ok := eventDataMap["data"].(map[string]interface{}); ok {
				if obj, ok := dataObj["object"].(map[string]interface{}); ok {
					if meta, ok := obj["metadata"].(map[string]interface{}); ok {
						transactionID, _ = meta["transaction_id"].(string)
					}
				}
			}
		}
	}

	slog.Debug("Final transaction_id", "transaction_id", transactionID, "eventType", eventType)

	if transactionID == "" {
		slog.Error("Missing transaction_id in webhook metadata", "metadata_keys", func() []string {
			keys := []string{}
			if paymentIntent.Metadata != nil {
				for k := range paymentIntent.Metadata {
					keys = append(keys, k)
				}
			}
			return keys
		}())
		api.Respond(w, "missing transaction_id", http.StatusBadRequest)
		return nil
	}

	slog.Info("Processing webhook", "eventType", eventType, "transactionID", transactionID)

	switch eventType {
	case "payment_intent.succeeded":
		txUUID, _ := uuid.Parse(transactionID)
		err := h.service.HandlePaymentSucceeded(req.Context(), HandlePaymentSucceededRequest{
			TransactionID: txUUID,
		})
		if err != nil {
			slog.Error(
				"Failed to handle payment succeeded",
				"error",
				err,
				"transaction_id",
				transactionID,
			)
		}
	case "payment_intent.payment_failed":
		txUUID, _ := uuid.Parse(transactionID)
		err := h.service.HandlePaymentFailed(req.Context(), HandlePaymentFailedRequest{
			TransactionID: txUUID,
		})
		if err != nil {
			slog.Error(
				"Failed to handle payment failed",
				"error",
				err,
				"transaction_id",
				transactionID,
			)
		}
	case "payment_intent.canceled":
		txUUID, _ := uuid.Parse(transactionID)
		err := h.service.HandlePaymentCanceled(req.Context(), HandlePaymentCanceledRequest{
			TransactionID: txUUID,
		})
		if err != nil {
			slog.Error(
				"Failed to handle payment canceled",
				"error",
				err,
				"transaction_id",
				transactionID,
			)
		}
	default:
		slog.Debug("Unhandled event type", "type", eventType)
	}

	api.Respond(w, nil, http.StatusOK)
	return nil
}
