package source

import "github.com/ashep/gnucashsync/internal/model"

type sliceSource struct {
	txns []model.Transaction
}

// NewSlice returns a Source backed by an in-memory slice of transactions.
func NewSlice(txns []model.Transaction) Source {
	return &sliceSource{txns: txns}
}

func (s *sliceSource) Transactions() ([]model.Transaction, error) {
	return s.txns, nil
}
