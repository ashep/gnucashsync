package source

import (
	"errors"

	"github.com/ashep/gnucashsync/internal/model"
)

type monobankSource struct {
	token string
}

// NewMonobank returns a Source that fetches transactions from the Monobank API.
// Not yet implemented.
func NewMonobank(token string) Source {
	return &monobankSource{token: token}
}

func (s *monobankSource) Transactions() ([]model.Transaction, error) {
	return nil, errors.New("monobank source not yet implemented")
}
