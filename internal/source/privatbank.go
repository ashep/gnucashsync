package source

import (
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ashep/gnucashsync/internal/model"
)

type privatBankSource struct {
	path string
}

// NewPrivatBank returns a Source that parses a PrivatBank CSV export.
func NewPrivatBank(path string) Source {
	return &privatBankSource{path: path}
}

func (s *privatBankSource) Transactions() ([]model.Transaction, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comma = ';'
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	// Skip header row.
	if len(records) < 2 {
		return nil, nil
	}

	var txns []model.Transaction
	for _, row := range records[1:] {
		if len(row) < 7 {
			continue
		}
		dateStr := strings.TrimSpace(row[0]) // DD.MM.YYYY
		timeStr := strings.TrimSpace(row[1]) // HH:MM:SS
		card := strings.TrimSpace(row[3])
		description := strings.TrimSpace(row[4])
		amountStr := strings.TrimSpace(row[5])
		currency := strings.TrimSpace(row[6])

		date, err := time.Parse("02.01.2006", dateStr)
		if err != nil {
			return nil, fmt.Errorf("parsing date %q: %w", dateStr, err)
		}

		// Normalize: remove spaces, replace comma decimal separator.
		amountStr = strings.ReplaceAll(amountStr, " ", "")
		amountStr = strings.ReplaceAll(amountStr, ",", ".")
		amount, err := decimal.NewFromString(amountStr)
		if err != nil {
			return nil, fmt.Errorf("parsing amount %q: %w", amountStr, err)
		}

		id := syntheticID(dateStr, timeStr, card, amountStr, description)

		txns = append(txns, model.Transaction{
			ID:          id,
			Date:        date,
			Description: description,
			Amount:      amount,
			Currency:    currency,
			AccountID:   card,
		})
	}
	return txns, nil
}

// syntheticID builds a deterministic 16-char hex ID from transaction fields.
func syntheticID(fields ...string) string {
	h := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return fmt.Sprintf("%x", h[:8])
}
