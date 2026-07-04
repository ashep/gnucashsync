package source

import (
	"encoding/json"
	"os"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/model"
)

type jsonRecord struct {
	ID          string          `json:"id"`
	Date        string          `json:"date"`
	Description string          `json:"description"`
	Amount      decimal.Decimal `json:"amount"`
	Currency    string          `json:"currency"`
	AccountID   string          `json:"account_id"`
	Category    string          `json:"category"`
}

type jsonSource struct {
	path string
}

// NewJSON returns a Source that reads the custom JSON format from path.
func NewJSON(path string) Source {
	return &jsonSource{path: path}
}

func (s *jsonSource) Transactions() ([]model.Transaction, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}

	var records []jsonRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}

	txns := make([]model.Transaction, 0, len(records))
	for _, r := range records {
		date, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			return nil, err
		}
		txns = append(txns, model.Transaction{
			ID:          r.ID,
			Date:        date,
			Description: r.Description,
			Amount:      r.Amount,
			Currency:    r.Currency,
			AccountID:   r.AccountID,
			Category:    r.Category,
		})
	}
	return txns, nil
}
