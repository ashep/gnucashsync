package model

import (
	"time"

	"github.com/shopspring/decimal"
)

type Transaction struct {
	ID          string
	Date        time.Time
	Description string
	Amount      decimal.Decimal
	Currency    string
	AccountID   string
}
