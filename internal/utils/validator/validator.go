package validator

import (
	"errors"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

var (
	ErrAmountIsTooLow       = errors.New("amount is too low, must be at least 10 EGP (1000 piastres)")
	ErrInvalidTransactionStatus = errors.New("invalid transaction status")
	ErrInvalidTransactionType   = errors.New("invalid transaction type")
	ErrInvalidTransferStatus    = errors.New("invalid transfer status")
	ErrScheduledAtMustBeFuture  = errors.New("scheduled_at must be in the future")
)

func init() {
	validate.RegisterValidation("transaction_status", func(fl validator.FieldLevel) bool {
		switch repo.TransactionStatus(fl.Field().String()) {
		case repo.TransactionStatusPending,
			repo.TransactionStatusProcessing,
			repo.TransactionStatusCompleted,
			repo.TransactionStatusFailed,
			repo.TransactionStatusCancelled,
			repo.TransactionStatusReversed,
			repo.TransactionStatusReversing:
			return true
		}
		return false
	})

	validate.RegisterValidation("transaction_type", func(fl validator.FieldLevel) bool {
		switch repo.TransactionType(fl.Field().String()) {
		case repo.TransactionTypeDebit, repo.TransactionTypeCredit:
			return true
		}
		return false
	})

	validate.RegisterValidation("transfer_status", func(fl validator.FieldLevel) bool {
		switch repo.TransferStatus(fl.Field().String()) {
		case repo.TransferStatusPending,
			repo.TransferStatusCompleted,
			repo.TransferStatusFailed,
			repo.TransferStatusCancelled:
			return true
		}
		return false
	})

	validate.RegisterValidation("future", func(fl validator.FieldLevel) bool {
		t, ok := fl.Field().Interface().(time.Time)
		if !ok {
			return false
		}
		return t.After(time.Now())
	})
}

func Validate(s any) error {
	if err := validate.Struct(s); err != nil {
		for _, e := range err.(validator.ValidationErrors) {
			switch e.Tag() {
			case "min":
				if e.Field() == "Amount" {
					return ErrAmountIsTooLow
				}
			case "transaction_status":
				return ErrInvalidTransactionStatus
			case "transaction_type":
				return ErrInvalidTransactionType
			case "transfer_status":
				return ErrInvalidTransferStatus
			case "future":
				return ErrScheduledAtMustBeFuture
			}
		}
		return err
	}
	return nil
}
