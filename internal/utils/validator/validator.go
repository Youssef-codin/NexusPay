package validator

import (
	"errors"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

var (
	ErrAmountIsTooLow = errors.New(
		"amount is too low, must be at least 10 EGP (1000 piastres)",
	)
	ErrInvalidTransactionStatus = errors.New("invalid transaction status")
	ErrInvalidExpenseCategory   = errors.New("invalid expense category")
	ErrScheduledAtMustBeFuture  = errors.New("scheduled_at must be in the future")
)

func init() {
	validate.RegisterValidation("transaction_status", func(fl validator.FieldLevel) bool {
		switch repo.TransactionStatus(fl.Field().String()) {
		case repo.TransactionStatusAwaitingPayment,
			repo.TransactionStatusCrediting,
			repo.TransactionStatusScheduled,
			repo.TransactionStatusCompleted,
			repo.TransactionStatusFailed:
			return true
		}
		return false
	})

	validate.RegisterValidation("expense_category", func(fl validator.FieldLevel) bool {
		switch repo.ExpenseCategory(fl.Field().String()) {
		case repo.ExpenseCategoryFood,
			repo.ExpenseCategoryTransport,
			repo.ExpenseCategoryBills,
			repo.ExpenseCategoryShopping,
			repo.ExpenseCategoryIncome,
			repo.ExpenseCategoryTopup,
			repo.ExpenseCategoryOther:
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
			case "expense_category":
				return ErrInvalidExpenseCategory
			case "future":
				return ErrScheduledAtMustBeFuture
			}
		}
		return err
	}
	return nil
}
