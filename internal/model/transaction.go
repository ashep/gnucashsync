package model

import (
	"time"

	"github.com/shopspring/decimal"
)

type Transaction struct {
	ID                string
	Date              time.Time
	Description       string
	Amount            decimal.Decimal
	Currency          string
	OperationAmount   decimal.Decimal // non-zero when operation currency differs from account currency
	OperationCurrency string          // non-empty when operation currency differs from account currency
	AccountID         string
	Category          string
	CategoryLabel     string
}
